package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestAntigravityBuildUpstreamPayloadRejectsMissingProjectID(t *testing.T) {
	t.Parallel()

	_, err := (antigravityProtocolAdapter{}).BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model": "gemini-test",
			"input": "hello",
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gemini-upstream"},
	})

	if err == nil || !strings.Contains(err.Error(), "project_id is missing") {
		t.Fatalf("BuildUpstreamPayload error = %v, want missing project_id", err)
	}
}

func TestAntigravityProjectIDGuardsNilStoreAndNilSite(t *testing.T) {
	t.Parallel()

	adapter := antigravityProtocolAdapter{}
	if got := adapter.projectID(gatewayRequest{}, routeengine.Candidate{}); got != "" {
		t.Fatalf("projectID without store = %q, want empty", got)
	}
	if got := adapter.projectID(gatewayRequest{}, routeengine.Candidate{Site: routeengine.CandidateSite{ID: uuid.Nil}}); got != "" {
		t.Fatalf("projectID with nil site = %q, want empty", got)
	}
}

func TestAntigravityProxyStreamAsRejectsMissingBody(t *testing.T) {
	t.Parallel()

	capture, started, err := (antigravityProtocolAdapter{}).proxyStreamAs(context.Background(), httptest.NewRecorder(), gatewayStreamTestResponseWithoutBody(), time.Now(), canonicalProtocolOpenAIResponses, routeengine.Candidate{})

	assertMissingBodyStreamCapture(t, "proxyStreamAs", capture, started, err)
}

func TestCodexProxyStreamForImagesRejectsMissingBody(t *testing.T) {
	t.Parallel()

	capture, started, err := (codexProtocolAdapter{downstreamImages: true}).ProxyStream(context.Background(), httptest.NewRecorder(), gatewayStreamTestResponseWithoutBody(), time.Now(), routeengine.Candidate{})

	assertMissingBodyStreamCapture(t, "Codex image ProxyStream", capture, started, err)
}

func TestProxyCodexResponsesImageStreamGuardAndErrorBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		wantStarted bool
		wantReason  string
		wantErr     bool
		wantBody    string
	}{
		{
			name:       "invalid event json",
			body:       "data: {not-json}\n\n",
			wantReason: "upstream_stream_parse_failed",
			wantErr:    true,
		},
		{
			name:        "upstream error event writes downstream error",
			body:        `data: {"error":{"type":"invalid_request_error","code":"bad_image","message":"nope"}}` + "\n\n",
			wantStarted: true,
			wantReason:  "upstream_stream_error",
			wantBody:    "event: error",
		},
		{
			name:       "empty eof",
			body:       "",
			wantReason: "upstream_stream_empty",
		},
		{
			name:       "done marker completes without headers",
			body:       "data: [DONE]\n\n",
			wantReason: "done",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			capture, started, err := proxyCodexResponsesImageStream(context.Background(), rec, gatewayStreamTestResponse(tt.body), time.Now(), "image_generation")

			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if started != tt.wantStarted {
				t.Fatalf("started = %v, want %v", started, tt.wantStarted)
			}
			if capture.endReason != tt.wantReason {
				t.Fatalf("endReason = %q, want %q", capture.endReason, tt.wantReason)
			}
			if tt.wantReason == "done" && (!capture.sawDone || !capture.streamCompleted) {
				t.Fatalf("done flags = sawDone:%v streamCompleted:%v, want both true", capture.sawDone, capture.streamCompleted)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestProxyCodexResponsesImageStreamHonorsCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	capture, started, err := proxyCodexResponsesImageStream(ctx, rec, gatewayStreamTestResponse(`data: {"type":"response.completed"}`+"\n\n"), time.Now(), "image_generation")

	assertCancelledStreamCapture(t, "proxyCodexResponsesImageStream", rec, capture, started, err)
}

func TestBillingMetadataGuardsAndCandidateFallback(t *testing.T) {
	t.Parallel()

	empty := billingAdjustmentFromPayload(nil, routeengine.Candidate{})
	if empty.ServiceTier != "" || empty.Multiplier != 1 || empty.Mode != "" || empty.Reason != "" {
		t.Fatalf("nil payload adjustment = %#v, want neutral adjustment", empty)
	}

	result := applyUpstreamBillingMetadata(gatewayAttemptResult{}, map[string]any{
		"service_tier": " priority ",
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.5"},
	})
	if result.serviceTier != "priority" || result.billingMode != "fast" || result.costMultiplier != 2.5 || result.multiplierReason != "codex_fast_mode" {
		t.Fatalf("candidate fallback billing metadata = %#v", result)
	}

	noFast := applyUpstreamBillingMetadata(gatewayAttemptResult{costMultiplier: 9}, map[string]any{
		"service_tier": "default",
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.5"}})
	if noFast.serviceTier != "default" || noFast.billingMode != "" || noFast.costMultiplier != 1 || noFast.multiplierReason != "" {
		t.Fatalf("non-fast billing metadata = %#v, want neutral multiplier", noFast)
	}
}

func TestServeEndpointReturnsGatewayUnavailableWhenDependenciesMissing(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, gatewayEndpointResponses, strings.NewReader(`{"model":"gpt-test","input":"hi"}`))

	Handler{}.serveEndpoint(rec, req, responsesEndpointAdapter{}, openAIProtocolResolver{})

	assertGatewayErrorEnvelope(t, rec, http.StatusServiceUnavailable, "gateway_unavailable", "")
}
