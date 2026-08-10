package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xlyra/server/internal/site"
)

func TestChatCompletionsEndpointAdapterAllowsStreamRequests(t *testing.T) {
	t.Parallel()

	decoded, failure := decodeEndpointRequest(t, chatCompletionsEndpointAdapter{}, `{"model":"gpt-4o-mini","stream":true}`)
	if failure != nil {
		t.Fatalf("expected request to decode, got failure: %+v", *failure)
	}
	if !decoded.Stream {
		t.Fatal("expected stream request to be preserved")
	}
	if decoded.RequestedModel != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", decoded.RequestedModel)
	}
}

func TestChatCompletionsEndpointAdapterRejectsInvalidJSONAndMissingModel(t *testing.T) {
	t.Parallel()

	adapter := chatCompletionsEndpointAdapter{}
	assertEndpointDecodeFailure(t, "invalid JSON", adapter, `{`, "invalid_json", "decode")
	assertEndpointDecodeFailure(t, "missing model", adapter, `{"messages":[]}`, "invalid_model", "validate")
}

func TestUpstreamClientConfigForRequestDisablesTotalTimeoutForStreaming(t *testing.T) {
	t.Parallel()

	streamingConfig := upstreamClientProfileForRequest(nil, upstreamClientProfileRequest{Stream: true}, "")
	if !streamingConfig.NoRequestTimeout {
		t.Fatalf("expected streaming request timeout to be disabled, got %s", streamingConfig.RequestTimeout)
	}
	if streamingConfig.ResponseHeaderTimeout != defaultStreamingResponseHeaderTimeout {
		t.Fatalf("streaming response header timeout = %s, want %s", streamingConfig.ResponseHeaderTimeout, defaultStreamingResponseHeaderTimeout)
	}

	nonStreamingConfig := upstreamClientProfileForRequest(nil, upstreamClientProfileRequest{}, "")
	if nonStreamingConfig.NoRequestTimeout || nonStreamingConfig.RequestTimeout == 0 {
		t.Fatal("expected non-streaming request timeout to remain configured")
	}
}

func TestUpstreamClientConfigForRequestHonorsStreamingResponseHeaderOverride(t *testing.T) {
	t.Parallel()

	timeoutMS := 90000
	streamingConfig := upstreamClientProfileForRequest(&site.GatewayConfig{ResponseHeaderTimeoutMS: &timeoutMS}, upstreamClientProfileRequest{Stream: true}, "")
	if streamingConfig.ResponseHeaderTimeout != 90*time.Second {
		t.Fatalf("streaming response header timeout = %s, want 90s", streamingConfig.ResponseHeaderTimeout)
	}
}

func TestUpstreamClientConfigForRequestExtendsImageGenerationHeaderTimeout(t *testing.T) {
	t.Parallel()

	streamingConfig := upstreamClientProfileForRequest(nil, upstreamClientProfileRequest{Stream: true, ImageGeneration: true}, "")
	if streamingConfig.ResponseHeaderTimeout != defaultImageGenerationResponseHeaderTimeout {
		t.Fatalf("image generation response header timeout = %s, want %s", streamingConfig.ResponseHeaderTimeout, defaultImageGenerationResponseHeaderTimeout)
	}
}

func TestUpstreamClientConfigForRequestHonorsImageGenerationHeaderOverride(t *testing.T) {
	t.Parallel()

	timeoutMS := 120000
	streamingConfig := upstreamClientProfileForRequest(&site.GatewayConfig{ResponseHeaderTimeoutMS: &timeoutMS}, upstreamClientProfileRequest{Stream: true, ImageGeneration: true}, "")
	if streamingConfig.ResponseHeaderTimeout != 120*time.Second {
		t.Fatalf("image generation response header timeout = %s, want 120s", streamingConfig.ResponseHeaderTimeout)
	}
}

func TestProxyUpstreamStreamWritesEventsAndCapturesUsage(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"id\":\"chunk-1\"}\n\n" +
				"data: {\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}}\n\n" +
				"data: [DONE]\n\n",
		)),
	}
	rec := httptest.NewRecorder()

	capture, responseStarted, err := proxyUpstreamStream(context.Background(), rec, resp, time.Now())
	if err != nil {
		t.Fatalf("proxyUpstreamStream returned error: %v", err)
	}
	if !responseStarted {
		t.Fatal("expected downstream response to start")
	}
	if !capture.streamCompleted {
		t.Fatalf("expected stream to complete, capture=%+v", capture)
	}
	if capture.usage.PromptTokens != 11 || capture.usage.CompletionTokens != 7 {
		t.Fatalf("expected usage to be captured, got %+v", capture.usage)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), "data: [DONE]")
}

func TestProxyUpstreamStreamTreatsLateReadErrorAfterDoneAsComplete(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &lateReadErrorBody{data: []byte("data: [DONE]\n\n")},
	}
	rec := httptest.NewRecorder()
	capture, started, err := proxyUpstreamStream(context.Background(), rec, resp, time.Now())
	if err != nil || !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("late read error result: capture=%+v started=%v err=%v", capture, started, err)
	}
}

type lateReadErrorBody struct {
	data []byte
	done bool
}

func (b *lateReadErrorBody) Read(p []byte) (int, error) {
	if !b.done {
		b.done = true
		return copy(p, b.data), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (*lateReadErrorBody) Close() error { return nil }

func TestProxyUpstreamStreamDoesNotStartResponseOnEmptyBody(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader("")),
	}
	rec := httptest.NewRecorder()

	capture, responseStarted, err := proxyUpstreamStream(context.Background(), rec, resp, time.Now())
	if err != nil {
		t.Fatalf("proxyUpstreamStream returned error: %v", err)
	}
	if responseStarted {
		t.Fatal("expected empty stream not to start downstream response")
	}
	if capture.endReason != "upstream_stream_empty" {
		t.Fatalf("expected upstream_stream_empty, got %q", capture.endReason)
	}
}

func TestProxyResponsesStreamPassthroughPreservesEndReasonOnReadError(t *testing.T) {
	t.Parallel()

	data := "data: " + `{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"limit hit"}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &lateReadErrorBody{data: []byte(data)},
	}
	rec := httptest.NewRecorder()
	capture, _, _ := proxyResponsesStreamPassthrough(context.Background(), rec, resp, time.Now())
	if capture.endReason != "upstream_stream_error" {
		t.Fatalf("endReason = %q, want upstream_stream_error; semantic error reason must survive a subsequent read error", capture.endReason)
	}
}

func TestProxyUpstreamStreamPreservesEndReasonOnReadError(t *testing.T) {
	t.Parallel()

	data := "data: " + `{"type":"error","error":{"message":"upstream returned an error"}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       &lateReadErrorBody{data: []byte(data)},
	}
	rec := httptest.NewRecorder()
	capture, _, _ := proxyUpstreamStreamWithInspector(context.Background(), rec, resp, time.Now(), inspectAnthropicMessagesStreamLine)
	if capture.endReason != "upstream_stream_error" {
		t.Fatalf("endReason = %q, want upstream_stream_error; Anthropic error event endReason must survive a subsequent read error", capture.endReason)
	}
}
