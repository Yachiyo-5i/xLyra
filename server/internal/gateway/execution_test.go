package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/httpclient"
	routeengine "xlyra/server/internal/router"
)

func TestUpstreamResponseForMetadataParsesProtocolUsage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		prompt     int
		completion int
		total      int
		cached     int
	}{
		{
			name:       "chat completions",
			body:       `{"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`,
			prompt:     11,
			completion: 7,
			total:      18,
		},
		{
			name:       "responses",
			body:       `{"usage":{"input_tokens":5091,"output_tokens":5,"total_tokens":5096,"input_tokens_details":{"cached_tokens":3}}}`,
			prompt:     5091,
			completion: 5,
			total:      5096,
			cached:     3,
		},
		{
			name:       "anthropic messages",
			body:       `{"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":4}}`,
			prompt:     14,
			completion: 2,
			total:      16,
			cached:     4,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw := upstreamResponseForMetadata(true, []byte(tt.body))
			metadata, ok := raw.(map[string]any)
			if !ok {
				t.Fatalf("metadata = %T, want map[string]any", raw)
			}
			encoded, err := json.Marshal(metadata["usage"])
			if err != nil {
				t.Fatalf("marshal usage: %v", err)
			}
			var usage gatewayUsage
			if err := json.Unmarshal(encoded, &usage); err != nil {
				t.Fatalf("unmarshal usage: %v", err)
			}
			if usage.PromptTokens != tt.prompt || usage.CompletionTokens != tt.completion || usage.TotalTokens != tt.total || usage.CachedPromptTokens != tt.cached {
				t.Fatalf("usage = %+v, want prompt=%d completion=%d total=%d cached=%d", usage, tt.prompt, tt.completion, tt.total, tt.cached)
			}
		})
	}
}

func TestNewUpstreamHTTPClientConfiguresConnectionPool(t *testing.T) {
	t.Parallel()

	manager := httpclient.NewManager(nil)
	client, err := manager.Client(httpclient.DefaultProfile())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if client.Timeout == 0 {
		t.Fatal("expected client timeout to be configured")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.MaxIdleConns <= 0 {
		t.Fatalf("expected MaxIdleConns > 0, got %d", transport.MaxIdleConns)
	}
	if transport.MaxConnsPerHost <= 0 {
		t.Fatalf("expected MaxConnsPerHost > 0, got %d", transport.MaxConnsPerHost)
	}
}

func TestDefaultUpstreamHTTPClientAllowsLongImageGeneration(t *testing.T) {
	t.Parallel()

	manager := httpclient.NewManager(nil)
	client, err := manager.Client(httpclient.DefaultProfile())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if client.Timeout != 300*time.Second {
		t.Fatalf("client timeout = %s, want 300s", client.Timeout)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.ResponseHeaderTimeout != 300*time.Second {
		t.Fatalf("response header timeout = %s, want 300s", transport.ResponseHeaderTimeout)
	}
}

func TestCooldownInputForCodexModelCapacityUsesModelScope(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	credentialID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	input, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID, SiteType: "codex"},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-5.3-codex"},
	}, gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusBadRequest,
		errorType:          "codex_model_capacity",
		credentialID:       credentialID,
		cooldownReason:     "codex_model_capacity",
		cooldownScope:      "model",
		cooldownDuration:   2 * time.Minute,
	})
	if !ok {
		t.Fatal("expected cooldown input")
	}
	if input.Scope != "model" {
		t.Fatalf("expected model scope, got %q", input.Scope)
	}
	if input.SiteModelID == nil || *input.SiteModelID != siteModelID {
		t.Fatalf("expected site model id %s, got %#v", siteModelID, input.SiteModelID)
	}
	if input.SiteCredentialID != nil {
		t.Fatalf("expected no credential scope, got %#v", input.SiteCredentialID)
	}
	if input.Duration != 2*time.Minute {
		t.Fatalf("duration = %v, want 2m", input.Duration)
	}
}

func TestCooldownInputForCodexUsageLimitUsesCredentialRetryAfter(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	credentialID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	input, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID, SiteType: "codex"},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-5.3-codex"},
	}, gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "codex_usage_limit_reached",
		credentialID:       credentialID,
		cooldownReason:     "codex_usage_limit_reached",
		cooldownScope:      "credential",
		cooldownDuration:   123 * time.Second,
		retryAfterSeconds:  123,
	})
	if !ok {
		t.Fatal("expected cooldown input")
	}
	if input.Scope != "credential" {
		t.Fatalf("expected credential scope, got %q", input.Scope)
	}
	if input.SiteCredentialID == nil || *input.SiteCredentialID != credentialID {
		t.Fatalf("expected credential id %s, got %#v", credentialID, input.SiteCredentialID)
	}
	if input.Duration != 123*time.Second {
		t.Fatalf("duration = %v, want 123s", input.Duration)
	}
	if input.Metadata["retry_after_seconds"] != int64(123) {
		t.Fatalf("metadata retry_after_seconds = %#v, want 123", input.Metadata["retry_after_seconds"])
	}
}

func TestCooldownInputForFailureUsesCredentialScopeForUnauthorized(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	credentialID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	input, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-4o-mini"},
	}, gatewayAttemptResult{
		statusCode:   http.StatusUnauthorized,
		errorType:    "upstream_http_error",
		credentialID: credentialID,
	})
	if !ok {
		t.Fatal("expected cooldown input")
	}
	if input.Scope != "credential" {
		t.Fatalf("expected credential scope, got %q", input.Scope)
	}
	if input.SiteCredentialID == nil || *input.SiteCredentialID != credentialID {
		t.Fatalf("expected credential id %s, got %#v", credentialID, input.SiteCredentialID)
	}
}

func TestCooldownInputForNoUpstreamResponseUsesShortModelScope(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	credentialID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	input, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-5.5"},
	}, gatewayAttemptResult{
		statusCode:   http.StatusBadGateway,
		errorType:    "upstream_timeout",
		credentialID: credentialID,
	})
	if !ok {
		t.Fatal("expected cooldown input")
	}
	if input.Scope != "model" {
		t.Fatalf("expected model scope, got %q", input.Scope)
	}
	if input.SiteModelID == nil || *input.SiteModelID != siteModelID {
		t.Fatalf("expected site model id %s, got %#v", siteModelID, input.SiteModelID)
	}
	if input.SiteCredentialID != nil {
		t.Fatalf("expected no credential scope, got %#v", input.SiteCredentialID)
	}
	if input.Reason != "upstream_no_response" {
		t.Fatalf("reason = %q, want upstream_no_response", input.Reason)
	}
	if input.Duration != upstreamNoResponseCooldownDuration {
		t.Fatalf("duration = %v, want %v", input.Duration, upstreamNoResponseCooldownDuration)
	}
	if input.Metadata["no_upstream_response"] != true {
		t.Fatalf("metadata no_upstream_response = %#v, want true", input.Metadata["no_upstream_response"])
	}
}

func TestCooldownInputForDownstreamCancelDoesNotCooldownUpstream(t *testing.T) {
	t.Parallel()

	_, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		Model: routeengine.CandidateModel{SiteModelID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), UpstreamName: "gpt-5.5"},
	}, gatewayAttemptResult{
		statusCode: 499,
		errorType:  "downstream_client_cancelled",
	})
	if ok {
		t.Fatal("expected downstream cancellation to skip upstream cooldown")
	}
}

func TestTransportFailureClassificationForDownstreamCancel(t *testing.T) {
	t.Parallel()

	if got := transportErrorType(context.Canceled); got != "downstream_client_cancelled" {
		t.Fatalf("transport error type = %q, want downstream_client_cancelled", got)
	}
	if got := transportFailureStatusCode(context.Canceled); got != 499 {
		t.Fatalf("transport failure status = %d, want 499", got)
	}
}

type timeoutTransportError struct{}

func (timeoutTransportError) Error() string   { return "timeout" }
func (timeoutTransportError) Timeout() bool   { return true }
func (timeoutTransportError) Temporary() bool { return false }

type plainTransportError string

func (err plainTransportError) Error() string { return string(err) }

func TestTransportFailureClassificationForUpstreamErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: "upstream_timeout"},
		{name: "net timeout", err: timeoutTransportError{}, want: "upstream_timeout"},
		{name: "ordinary error", err: plainTransportError("connection refused"), want: "upstream_transport_error"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := transportErrorType(tt.err); got != tt.want {
				t.Fatalf("transport error type = %q, want %q", got, tt.want)
			}
			if got := transportFailureStatusCode(tt.err); got != http.StatusBadGateway {
				t.Fatalf("transport failure status = %d, want %d", got, http.StatusBadGateway)
			}
		})
	}
}

func TestStreamSucceededTreatsProtocolIncompleteAsSuccess(t *testing.T) {
	t.Parallel()

	capture := streamCaptureState{endReason: "response_incomplete"}
	if !streamSucceeded(capture) {
		t.Fatal("expected response_incomplete to be treated as stream success")
	}
	if got := streamMetadataEnvelope(capture)["stream_end_reason"]; got != "response_incomplete" {
		t.Fatalf("stream end reason metadata = %#v, want response_incomplete", got)
	}
	if got := streamMetadataEnvelope(capture)["stream_incomplete"]; got != false {
		t.Fatalf("stream incomplete metadata = %#v, want false", got)
	}
}

func TestStreamSucceededRejectsUpstreamReadFailure(t *testing.T) {
	t.Parallel()

	capture := streamCaptureState{endReason: "upstream_stream_read_failed"}
	if streamSucceeded(capture) {
		t.Fatal("expected upstream read failure to remain unsuccessful")
	}
	if got := streamErrorTypeFromEndReason(capture.endReason); got != "upstream_stream_read_failed" {
		t.Fatalf("stream error type = %q, want upstream_stream_read_failed", got)
	}
	if got := streamMetadataEnvelope(capture)["stream_end_reason"]; got != "upstream_stream_read_failed" {
		t.Fatalf("stream end reason metadata = %#v, want upstream_stream_read_failed", got)
	}
	if got := streamMetadataEnvelope(capture)["stream_incomplete"]; got != true {
		t.Fatalf("stream incomplete metadata = %#v, want true", got)
	}
}

func TestStreamSucceededAcceptsCompletedStreamAfterLateReadFailure(t *testing.T) {
	t.Parallel()

	capture := streamCaptureState{streamCompleted: true, endReason: "done"}
	if !streamSucceeded(capture) {
		t.Fatal("a stream with a terminal completion event must remain successful")
	}
}

func TestStreamCompletedAfterReadErrorNormalizesToSuccess(t *testing.T) {
	t.Parallel()

	if !streamCompletedAfterReadError(streamCaptureState{streamCompleted: true, endReason: "upstream_stream_read_failed"}, io.ErrUnexpectedEOF) {
		t.Fatal("completed stream with a late read error should be normalized")
	}
	if streamCompletedAfterReadError(streamCaptureState{streamCompleted: true, endReason: "upstream_stream_error"}, io.ErrUnexpectedEOF) {
		t.Fatal("explicit semantic stream failure must not be normalized")
	}
}

func TestStreamErrorMessageFromEndReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		endReason string
		want      string
	}{
		{endReason: " response_incomplete ", want: "upstream stream ended with response.incomplete"},
		{endReason: "upstream_stream_error", want: "upstream stream returned an error event"},
		{endReason: "tool_call_arguments_invalid_json", want: "upstream stream returned invalid tool call arguments"},
		{endReason: "upstream_stream_eof", want: "upstream stream ended before completion"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(strings.TrimSpace(tt.endReason), func(t *testing.T) {
			t.Parallel()

			if got := streamErrorMessageFromEndReason(tt.endReason); got != tt.want {
				t.Fatalf("stream error message = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpstreamResponseForMetadataFailureBodies(t *testing.T) {
	t.Parallel()

	if got := upstreamResponseForMetadata(false, nil); got != nil {
		t.Fatalf("empty failure body metadata = %#v, want nil", got)
	}

	raw := upstreamResponseForMetadata(false, []byte(`{"error":{"message":"bad upstream","code":"bad_request"}}`))
	metadata, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("json failure metadata = %T, want map[string]any", raw)
	}
	errorValue, ok := metadata["error"].(map[string]any)
	if !ok {
		t.Fatalf("error metadata = %T, want map[string]any", metadata["error"])
	}
	if errorValue["message"] != "bad upstream" || errorValue["code"] != "bad_request" {
		t.Fatalf("error metadata = %#v, want decoded error object", errorValue)
	}

	longBody := strings.Repeat("x", 5000)
	truncated, ok := upstreamResponseForMetadata(false, []byte(longBody)).(string)
	if !ok {
		t.Fatalf("plain failure metadata = %T, want string", truncated)
	}
	if len(truncated) != 4096 {
		t.Fatalf("truncated metadata length = %d, want 4096", len(truncated))
	}
}

func TestUpstreamResponseForMetadataSuccessWithoutUsage(t *testing.T) {
	t.Parallel()

	raw := upstreamResponseForMetadata(true, []byte(`{}`))
	metadata, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("success metadata = %T, want map[string]any", raw)
	}
	usage, ok := metadata["usage"].(gatewayUsage)
	if !ok {
		t.Fatalf("usage metadata = %T, want gatewayUsage", metadata["usage"])
	}
	if usage != (gatewayUsage{}) {
		t.Fatalf("usage = %+v, want zero value", usage)
	}
}

func TestEmptySliceToNil(t *testing.T) {
	t.Parallel()

	if got := emptySliceToNil([]string{}); got != nil {
		t.Fatalf("empty slice metadata = %#v, want nil", got)
	}
	values, ok := emptySliceToNil([]string{"annotation"}).([]string)
	if !ok {
		t.Fatalf("non-empty slice metadata type mismatch")
	}
	if len(values) != 1 || values[0] != "annotation" {
		t.Fatalf("non-empty slice metadata = %#v, want annotation", values)
	}
}

func TestOAuthModelRequestAuthFailureOnlyMatchesOAuthUnauthorized(t *testing.T) {
	t.Parallel()

	if !oauthModelRequestAuthFailure("codex", gatewayAttemptResult{upstreamStatusCode: http.StatusUnauthorized}) {
		t.Fatal("expected codex upstream 401 to mark oauth connection unavailable")
	}
	if !oauthModelRequestAuthFailure("antigravity", gatewayAttemptResult{statusCode: http.StatusUnauthorized}) {
		t.Fatal("expected antigravity 401 to mark oauth connection unavailable")
	}
	if oauthModelRequestAuthFailure("openai", gatewayAttemptResult{upstreamStatusCode: http.StatusUnauthorized}) {
		t.Fatal("non-oauth site should not mark oauth connection unavailable")
	}
	if oauthModelRequestAuthFailure("codex", gatewayAttemptResult{upstreamStatusCode: http.StatusTooManyRequests}) {
		t.Fatal("non-401 oauth failure should not mark oauth connection unavailable")
	}
}

func TestOAuthModelRequestAuthFailureMessageIncludesRequestContext(t *testing.T) {
	t.Parallel()

	message := oauthModelRequestAuthFailureMessage(routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.5"},
	}, gatewayAttemptResult{
		upstreamStatusCode: http.StatusUnauthorized,
		upstreamPath:       "/backend-api/codex/responses",
		errorMessage:       "upstream returned HTTP 401",
	})

	for _, want := range []string{"codex model request returned HTTP 401", "/backend-api/codex/responses", "gpt-5.5", "upstream returned HTTP 401"} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected message to contain %q, got %q", want, message)
		}
	}
}
