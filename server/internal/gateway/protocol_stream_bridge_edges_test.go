package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestProxyChatCompletionsStreamAsResponsesRejectsMissingBody(t *testing.T) {
	t.Parallel()

	capture, started, err := proxyChatCompletionsStreamAsResponses(context.Background(), httptest.NewRecorder(), gatewayStreamTestResponseWithoutBody(), time.Now())

	assertMissingBodyStreamCapture(t, "proxyChatCompletionsStreamAsResponses", capture, started, err)
}

func TestProxyChatCompletionsStreamAsResponsesHonorsCancelledContextBeforeRead(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	capture, started, err := proxyChatCompletionsStreamAsResponses(ctx, rec, gatewayStreamTestResponse(`data: {"id":"chatcmpl_1"}`+"\n\n"), time.Now())

	assertCancelledStreamCapture(t, "proxyChatCompletionsStreamAsResponses", rec, capture, started, err)
}

func TestProviderAnthropicMessagesStreamRejectsMissingBody(t *testing.T) {
	t.Parallel()

	capture, started, err := proxyProviderAnthropicMessagesStream(context.Background(), httptest.NewRecorder(), gatewayStreamTestResponseWithoutBody(), time.Now())

	assertMissingBodyStreamCapture(t, "proxyProviderAnthropicMessagesStream", capture, started, err)
}

func TestProviderAnthropicMessagesStreamHonorsCancelledContextBeforeRead(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	capture, started, err := proxyProviderAnthropicMessagesStream(ctx, rec, gatewayStreamTestResponse(`data: {"type":"message_start"}`+"\n\n"), time.Now())

	assertCancelledStreamCapture(t, "proxyProviderAnthropicMessagesStream", rec, capture, started, err)
}

func TestProviderAnthropicMessagesStreamFlushesThinkingCacheOnEOF(t *testing.T) {
	t.Parallel()

	toolUseID := "tool_use_eof"
	body := strings.Join([]string{
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"thinking","thinking":"first "}}`,
		"",
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"second"}}`,
		"",
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"sig-test"}}`,
		"",
		`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"` + toolUseID + `","name":"lookup","input":{}}}`,
		"",
	}, "\n")

	rec := httptest.NewRecorder()
	capture, started, err := proxyProviderAnthropicMessagesStream(context.Background(), rec, gatewayStreamTestResponse(body), time.Now())

	if err != nil {
		t.Fatalf("proxyProviderAnthropicMessagesStream returned error: %v", err)
	}
	if !started {
		t.Fatal("stream with events should start the downstream response")
	}
	if capture.streamCompleted {
		t.Fatal("EOF without message_stop should not mark stream completed")
	}
	if capture.endReason != "upstream_stream_eof" {
		t.Fatalf("endReason = %q, want upstream_stream_eof", capture.endReason)
	}

	blocks, ok := providerThinkingCache.lookup([]string{toolUseID})
	if !ok || len(blocks) != 1 {
		t.Fatalf("cached thinking blocks = %#v, want one block", blocks)
	}
	if blocks[0]["thinking"] != "first second" || blocks[0]["signature"] != "sig-test" {
		t.Fatalf("cached thinking block = %#v", blocks[0])
	}
}

func TestSiteModelTestProtocolAdapterRejectsInvalidManualProtocol(t *testing.T) {
	t.Parallel()

	request, err := siteModelTestGatewayRequest(gatewayEndpointResponses, "gpt-5.4", "Reply with only: ok", false)
	if err != nil {
		t.Fatalf("siteModelTestGatewayRequest returned error: %v", err)
	}
	_, err = (Handler{}).siteModelTestProtocolAdapter(context.Background(), request, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai", BaseURL: "https://api.example.test"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"},
	}, "unsupported_protocol")
	if err == nil {
		t.Fatal("expected invalid manual protocol to be rejected")
	}
	var testErr *SiteModelTestError
	if !errors.As(err, &testErr) || testErr.Code != "invalid_protocol" || testErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("error = %v, want invalid_protocol bad request", err)
	}
}
