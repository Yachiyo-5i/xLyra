package gateway

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestSemanticFailureFromJSONRecognizesNestedResponsesFailure(t *testing.T) {
	t.Parallel()

	failure, ok := semanticFailureFromJSON([]byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"Concurrency limit exceeded"}}}`))
	if !ok || failure.Code != "rate_limit_exceeded" || failure.Message != "Concurrency limit exceeded" {
		t.Fatalf("failure = %#v, ok=%v", failure, ok)
	}
	if !strings.Contains(failure.Error(), "Concurrency limit exceeded") {
		t.Fatalf("failure error = %q", failure.Error())
	}
}

func TestSemanticFailureFromJSONRecognizesTopLevelErrorEnvelope(t *testing.T) {
	t.Parallel()

	failure, ok := semanticFailureFromJSON([]byte(`{"error":{"code":"invalid_api_key","message":"credential rejected"}}`))
	if !ok || failure.Code != "invalid_api_key" || failure.Message != "credential rejected" {
		t.Fatalf("top-level error = %#v, ok=%v", failure, ok)
	}
}

func TestSemanticFailureClassificationBodyFlattensNestedResponsesError(t *testing.T) {
	t.Parallel()

	failure, ok := semanticFailureFromJSON([]byte(`{"type":"response.failed","response":{"status":"failed","error":{"code":"invalid_api_key","message":"credential rejected"}}}`))
	if !ok {
		t.Fatal("expected semantic failure")
	}
	flattened, ok := semanticFailureFromJSON(semanticFailureClassificationBody(failure))
	if !ok || flattened.Code != "invalid_api_key" || flattened.Message != "credential rejected" {
		t.Fatalf("flattened failure = %#v, ok=%v", flattened, ok)
	}
}

func TestSemanticFailureFromJSONIgnoresFailedOutputItem(t *testing.T) {
	t.Parallel()

	if failure, ok := semanticFailureFromJSON([]byte(`{"type":"response.output_item.done","item":{"status":"failed"}}`)); ok || failure != nil {
		t.Fatalf("output item failure = %#v, ok=%v", failure, ok)
	}
	if failure, ok := semanticFailureFromJSON([]byte(`{"type":"response.completed","response":{"status":"completed"}}`)); ok || failure != nil {
		t.Fatalf("completed response = %#v, ok=%v", failure, ok)
	}
}

func TestStreamSucceededRejectsSemanticErrorAfterCompletion(t *testing.T) {
	t.Parallel()

	if streamSucceeded(streamCaptureState{streamCompleted: true, endReason: "upstream_stream_error"}) {
		t.Fatal("explicit upstream stream error must override completed flag")
	}
}

func TestHandleBufferedResponseMarksHTTP200SemanticFailure(t *testing.T) {
	t.Parallel()

	body := `{"status":"failed","error":{"code":"upstream_failed","message":"provider rejected the request"}}`
	result := Handler{logger: gatewayDiscardLogger()}.handleBufferedResponse(
		context.Background(),
		"req-semantic-failure",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "openai"}},
		&testGatewayProtocol{},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		},
		gatewayAttemptResult{upstreamStatusCode: http.StatusOK},
		time.Now(),
	)
	if result.success || result.statusCode != http.StatusBadGateway || result.upstreamStatusCode != http.StatusOK {
		t.Fatalf("semantic failure result = %#v", result)
	}
	if result.errorType != "upstream_response_failed" || !strings.Contains(result.errorMessage, "provider rejected the request") {
		t.Fatalf("semantic failure error = %q %q", result.errorType, result.errorMessage)
	}
	if string(result.body) != body || result.failureResponse == nil {
		t.Fatalf("semantic failure payload = %q %#v", string(result.body), result.failureResponse)
	}
}

func TestHandleBufferedResponseClassifiesNestedRateLimitFailure(t *testing.T) {
	t.Parallel()

	result := Handler{logger: gatewayDiscardLogger()}.handleBufferedResponse(
		context.Background(),
		"req-semantic-rate-limit",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "openai"}},
		&testGatewayProtocol{},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(gatewaySSEEvent("response.failed",
				`{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"Concurrency limit exceeded"}}}`,
			))),
		},
		gatewayAttemptResult{upstreamStatusCode: http.StatusOK},
		time.Now(),
	)
	if result.success || result.statusCode != http.StatusBadGateway || result.upstreamStatusCode != http.StatusOK {
		t.Fatalf("semantic rate-limit result = %#v", result)
	}
	if result.errorType != "upstream_credential_limited" || result.cooldownScope != "credential" {
		t.Fatalf("semantic rate-limit classification = %#v", result)
	}
	if result.contentType != "application/json" {
		t.Fatalf("semantic rate-limit content type = %q, want application/json", result.contentType)
	}
	if !strings.Contains(result.errorMessage, "Concurrency limit exceeded") {
		t.Fatalf("semantic rate-limit message = %q", result.errorMessage)
	}
}

func TestHandleBufferedResponseClassifiesSemanticCredentialFailure(t *testing.T) {
	t.Parallel()

	result := Handler{logger: gatewayDiscardLogger()}.handleBufferedResponse(
		context.Background(),
		"req-semantic-auth",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "codex"}},
		&testGatewayProtocol{},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_api_key","message":"credential rejected"}}`)),
		},
		gatewayAttemptResult{upstreamStatusCode: http.StatusOK},
		time.Now(),
	)
	if result.success || result.statusCode != http.StatusBadGateway || result.upstreamStatusCode != http.StatusOK {
		t.Fatalf("semantic credential failure result = %#v", result)
	}
	if result.errorType != "upstream_credential_invalid" || result.cooldownScope != "credential" {
		t.Fatalf("semantic credential classification = %#v", result)
	}
	if !shouldTryNextCredential(result) {
		t.Fatal("semantic credential failure should rotate to the next credential")
	}
}

func TestHandleBufferedResponseDoesNotTreatNon2xxErrorBodyAsSemanticFailure(t *testing.T) {
	t.Parallel()

	result := Handler{logger: gatewayDiscardLogger()}.handleBufferedResponse(
		context.Background(),
		"req-http-error",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "openai"}},
		openAIChatProtocolAdapter{},
		&http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"bad_request","message":"invalid parameter"}}`)),
		},
		gatewayAttemptResult{upstreamStatusCode: http.StatusBadRequest},
		time.Now(),
	)
	if result.success {
		t.Fatal("HTTP 400 with error body must not be treated as success")
	}
	if result.statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d; HTTP 400 must not be overridden to 502 by semantic failure detection", result.statusCode, http.StatusBadRequest)
	}
	if result.errorType == "upstream_response_failed" {
		t.Fatal("HTTP 400 must not enter the semantic failure path; got upstream_response_failed")
	}
}
