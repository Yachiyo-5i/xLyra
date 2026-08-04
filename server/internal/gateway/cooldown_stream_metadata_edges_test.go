package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestNoResponseStreakSkipsNonGatewayAndStopsOnDifferentFailure(t *testing.T) {
	t.Parallel()

	attempts := []store.RequestLog{
		gatewayRequestLogFixture(t, true, 503, "upstream_timeout", map[string]any{"scope": "admin"}),
		gatewayRequestLogFixture(t, false, 503, "upstream_timeout", map[string]any{"no_upstream_response": true}),
		gatewayRequestLogFixture(t, false, 503, "upstream_transport_error", nil),
	}
	if !upstreamNoResponseFailureStreakReached(attempts, 2) {
		t.Fatal("expected two gateway no-response failures to reach threshold")
	}

	attempts = append([]store.RequestLog{
		gatewayRequestLogFixture(t, false, 503, "upstream_timeout", map[string]any{"upstream_status_code": float64(502)}),
	}, attempts...)
	if upstreamNoResponseFailureStreakReached(attempts, 2) {
		t.Fatal("different gateway failure should break no-response streak")
	}

	if !upstreamNoResponseFailureStreakReached(nil, 0) {
		t.Fatal("non-positive threshold should be treated as reached")
	}
}

func TestAttemptMetadataCapturesDiagnosticMappingAndStreamConversion(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	canonicalModelID := uuid.New()
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:       uuid.New(),
			Name:     "Codex Site",
			Slug:     "codex-site",
			SiteType: "codex",
			BaseURL:  "https://upstream.example.test",
		},
		Model: routeengine.CandidateModel{
			SiteModelID:  uuid.New(),
			UpstreamName: "gpt-upstream",
			DisplayName:  "GPT Display",
		},
	}
	result := gatewayAttemptResult{
		diagnostic:         true,
		downstreamPath:     gatewayEndpointResponses,
		upstreamProtocol:   string(canonicalProtocolAnthropicMessages),
		stream:             true,
		responseStarted:    true,
		streamEndReason:    "upstream_stream_read_failed",
		statusCode:         http.StatusBadGateway,
		errorType:          "upstream_transport_error",
		credentialAttempt:  1,
		credentialTotal:    2,
		firstByteLatencyMS: 12,
	}
	ctx := withModelMapping(context.Background(), "public-model", "mapped-model", "hard")

	meta := attemptMetadata(ctx, "attempt-1", "parent-1", apiKeyID, canonicalModelID, candidate, result)

	if meta["scope"] != "site_model_test" || meta["test"] != true || meta["downstream_api_key"] != nil {
		t.Fatalf("diagnostic metadata = %#v, want site_model_test with test marker", meta)
	}
	if meta["original_model"] != "public-model" || meta["mapped_model"] != "mapped-model" {
		t.Fatalf("model mapping metadata = %#v", meta)
	}
	if meta["stream_incomplete"] != true || meta["stream_failure_scope"] != "upstream" {
		t.Fatalf("stream metadata = %#v", meta)
	}
	conversion, ok := meta["protocol_conversion"].(map[string]any)
	if !ok || conversion["mode"] != "canonical" || conversion["downstream_protocol"] != string(canonicalProtocolOpenAIResponses) {
		t.Fatalf("protocol conversion metadata = %#v", meta["protocol_conversion"])
	}
}

func TestProxyStreamMissingBodyReturnsSemanticCapture(t *testing.T) {
	t.Parallel()

	capture, started, err := proxyUpstreamStreamWithInspector(
		context.Background(),
		&discardResponseWriter{},
		&http.Response{StatusCode: http.StatusOK},
		time.Now(),
		nil,
	)
	assertMissingBodyStreamCapture(t, "proxyUpstreamStreamWithInspector", capture, started, err)
}

func gatewayRequestLogFixture(t *testing.T, success bool, statusCode int, errorType string, metadata map[string]any) store.RequestLog {
	t.Helper()

	var encoded store.JSON
	if metadata != nil {
		raw, err := json.Marshal(metadata)
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		encoded = store.JSON(raw)
	}
	return store.RequestLog{
		Success:    success,
		StatusCode: statusCode,
		ErrorType:  sql.NullString{String: errorType, Valid: errorType != ""},
		Metadata:   encoded,
	}
}
