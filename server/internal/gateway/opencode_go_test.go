package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestClassifyOpenCodeGoUsageLimitPreservesOfficialReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:       uuid.New(),
			SiteType: "opencode_go",
		},
		Model: routeengine.CandidateModel{
			SiteModelID:  uuid.New(),
			UpstreamName: "kimi-k3",
		},
	}
	credentialID := uuid.New()
	result := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		retryAfterSeconds:  7 * 24 * 60 * 60,
		credentialID:       credentialID,
	}
	body := []byte(`{"type":"error","error":{"type":"GoUsageLimitError","message":"Weekly Go usage limit reached"},"metadata":{"workspace":"wrk_test","limitName":"weekly"}}`)

	classified := classifyGatewayUpstreamError(candidate, result, body, now)
	if classified.errorType != "opencode_go_usage_limit" || classified.cooldownScope != "credential" {
		t.Fatalf("classified result = %#v", classified)
	}
	if classified.cooldownDuration != 7*24*time.Hour || classified.retryAfterSeconds != 7*24*60*60 {
		t.Fatalf("classified duration = %s, retryAfter = %d", classified.cooldownDuration, classified.retryAfterSeconds)
	}
	if classified.cooldownMetadata["workspace"] != "wrk_test" || classified.cooldownMetadata["limit_name"] != "weekly" {
		t.Fatalf("classified metadata = %#v", classified.cooldownMetadata)
	}
	if classified.cooldownMetadata["reset_at"] != now.Add(7*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("reset_at = %#v", classified.cooldownMetadata["reset_at"])
	}

	input, ok := cooldownInputForFailure(candidate, classified)
	if !ok {
		t.Fatal("OpenCode Go usage limit did not create a cooldown")
	}
	if input.Scope != "credential" || input.Reason != store.CooldownReasonOpenCodeGoUsageLimitReached || input.Duration != 7*24*time.Hour {
		t.Fatalf("cooldown input = %#v", input)
	}
	if input.Metadata["workspace"] != "wrk_test" || input.Metadata["retry_after_seconds"] != int64(7*24*60*60) {
		t.Fatalf("cooldown metadata = %#v", input.Metadata)
	}
}

func TestClassifyOpenCodeGoLeavesOrdinaryRateLimitGeneric(t *testing.T) {
	t.Parallel()

	result := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		retryAfterSeconds:  30,
	}
	got := classifyGatewayUpstreamError(
		resolverCooldownCandidate("opencode_go"),
		result,
		[]byte(`{"error":{"type":"rate_limit_exceeded","message":"slow down"}}`),
		time.Now(),
	)
	if got.errorType != "upstream_credential_limited" {
		t.Fatalf("ordinary rate limit errorType = %q", got.errorType)
	}
}

func TestWriteUpstreamFailureForwardsRetryAfter(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeUpstreamFailure(recorder, gatewayAttemptResult{
		statusCode:        http.StatusTooManyRequests,
		contentType:       "application/json",
		body:              []byte(`{"error":{"type":"GoUsageLimitError"}}`),
		retryAfterSeconds: 86400,
	}, "request-test")
	if got := recorder.Header().Get("Retry-After"); got != "86400" {
		t.Fatalf("Retry-After = %q, want 86400", got)
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
}
