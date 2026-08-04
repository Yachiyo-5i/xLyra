package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCopyUpstreamResponseWritesBufferedResult(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	copyUpstreamResponse(recorder, gatewayAttemptResult{
		statusCode:   http.StatusTeapot,
		contentType:  "application/problem+json",
		body:         []byte(`{"error":"short and stout"}`),
		promptTokens: 99,
	}, "tokenfree", true)

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTeapot)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	if got := recorder.Header().Get(RouteSiteHeader); got != "tokenfree" {
		t.Fatalf("%s = %q, want tokenfree", RouteSiteHeader, got)
	}
	if got := recorder.Body.String(); got != `{"error":"short and stout"}` {
		t.Fatalf("body = %q, want upstream body", got)
	}
}

func TestCopyUpstreamResponseOmitsRouteSiteByDefault(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	copyUpstreamResponse(recorder, gatewayAttemptResult{
		statusCode:  http.StatusOK,
		contentType: "application/json",
		body:        []byte(`{"ok":true}`),
	}, "tokenfree", false)

	if got := recorder.Header().Get(RouteSiteHeader); got != "" {
		t.Fatalf("%s = %q, want empty", RouteSiteHeader, got)
	}
}

func TestSetRouteSiteHeaderEncodesUnicodeSiteName(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	setRouteSiteHeader(headers, "图片站点")

	if got := headers.Get(RouteSiteHeader); got != "%E5%9B%BE%E7%89%87%E7%AB%99%E7%82%B9" {
		t.Fatalf("%s = %q", RouteSiteHeader, got)
	}
}

func TestModelsETagMatchesWeakCommaSeparatedCandidates(t *testing.T) {
	t.Parallel()

	etag := `"models-sha256-test"`
	if !modelsETagMatches([]string{` "stale" , W/` + etag + ` `}, etag) {
		t.Fatal("expected weak comma-separated If-None-Match candidate to match")
	}
	if modelsETagMatches([]string{`W/"other", "stale"`}, etag) {
		t.Fatal("unexpected ETag match for different weak and strong candidates")
	}
}

func TestBufferedResponsesStreamReturnsAPIErrorEvent(t *testing.T) {
	t.Parallel()

	_, err := readBufferedResponsesStreamBody(strings.NewReader(`data: {"type":"error","error":{"type":"invalid_request_error","code":"bad_model","message":"model is unavailable"}}` + "\n\n"))
	if err == nil {
		t.Fatal("expected response stream error event to return an error")
	}
	if got := err.Error(); !strings.Contains(got, "bad_model") || !strings.Contains(got, "model is unavailable") {
		t.Fatalf("error = %q, want code and message", got)
	}
}

func TestBufferedResponsesStreamBuildsPartialImageOutputWithoutCompletedPayload(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`data: {"type":"response.image_generation_call.partial_image","partial_image_b64":"image-b64","revised_prompt":"a bright moon","background":"transparent","output_format":"png","quality":"high","size":"1024x1024"}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_partial"}}`,
		"",
	}, "\n")

	body, err := readBufferedResponsesStreamBody(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}
	if !bytes.Contains(body, []byte(`"id":"partial-image-1"`)) || !bytes.Contains(body, []byte(`"result":"image-b64"`)) {
		t.Fatalf("buffered body missing synthesized partial image output: %s", body)
	}
	if !bytes.Contains(body, []byte(`"revised_prompt":"a bright moon"`)) || !bytes.Contains(body, []byte(`"output_format":"png"`)) {
		t.Fatalf("buffered body missing partial image metadata: %s", body)
	}
}

func TestResponsesUsageMetadataFallsBackToOutputTokensAndDetails(t *testing.T) {
	t.Parallel()

	raw := upstreamResponseForMetadata(true, []byte(`{
		"usage": {
			"input_tokens": 12,
			"output_tokens": 5,
			"input_tokens_details": {"cached_tokens": 4, "image_tokens": 3},
			"completion_tokens_details": {"reasoning_tokens": 2}
		}
	}`))
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
	if usage.PromptTokens != 12 || usage.CompletionTokens != 5 || usage.TotalTokens != 17 {
		t.Fatalf("usage tokens = %+v, want prompt=12 completion=5 total=17", usage)
	}
	if usage.CachedPromptTokens != 4 || usage.InputImageTokens != 3 || usage.ReasoningTokens != 2 {
		t.Fatalf("usage details = %+v, want cached=4 image=3 reasoning=2", usage)
	}
}
