package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestOpenAIImagesProtocolBuildsPayloadFromRawAndCanonicalRequests(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	}
	protocol := newOpenAIImagesProtocolAdapter(gatewayRequest{}, candidate)

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		Payload: map[string]any{
			"model":   "alias-image",
			"prompt":  "draw a cat",
			"quality": "high",
		},
	}, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-image-2" || payload["prompt"] != "draw a cat" || payload["quality"] != "high" {
		t.Fatalf("unexpected raw image payload: %#v", payload)
	}

	canonicalPayload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		Canonical: &canonicalRequest{
			Image: &canonicalImageRequest{Prompt: "draw a skyline"},
			Raw: map[string]any{
				"model": "alias-image",
				"n":     float64(2),
			},
		},
	}, candidate)
	if err != nil {
		t.Fatalf("canonical BuildUpstreamPayload returned error: %v", err)
	}
	if canonicalPayload["model"] != "gpt-image-2" || canonicalPayload["prompt"] != "draw a skyline" || canonicalPayload["n"] != float64(2) {
		t.Fatalf("unexpected canonical image payload: %#v", canonicalPayload)
	}

	body, contentType, err := protocol.BuildUpstreamBody(gatewayRequest{}, payload)
	if err != nil {
		t.Fatalf("BuildUpstreamBody returned error: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q, want application/json", contentType)
	}
	var encoded map[string]any
	if err := json.Unmarshal(body, &encoded); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	if encoded["model"] != "gpt-image-2" {
		t.Fatalf("encoded model = %#v, want upstream model", encoded["model"])
	}
	if got := protocol.UpstreamPath("https://api.example.test"); got != "https://api.example.test/v1/images/generations" {
		t.Fatalf("UpstreamPath = %q, want image generations path", got)
	}
	if got := protocol.ProtocolName(); got != "openai_images_generations" {
		t.Fatalf("ProtocolName = %q, want openai_images_generations", got)
	}
}

func TestOpenAIImagesProtocolTransformsBufferedResponseUsage(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"created": 1,
		"data": [{"b64_json":"aW1hZ2Ux"},{"b64_json":"aW1hZ2Uy"}],
		"usage": {
			"input_tokens": 12,
			"output_tokens": 20,
			"total_tokens": 32,
			"input_tokens_details": {"text_tokens": 7, "image_tokens": 5},
			"output_tokens_details": {"image_tokens": 20}
		}
	}`)
	transformed, err := (openAIImagesProtocolAdapter{}).TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusOK || transformed.ContentType != "application/json" || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected transformed response: %#v", transformed)
	}
	usage := transformed.Usage
	if usage.PromptTokens != 12 || usage.CompletionTokens != 20 || usage.TotalTokens != 32 || usage.ImageCount != 2 {
		t.Fatalf("usage tokens/image count = %#v", usage)
	}
	if usage.InputTextTokens != 7 || usage.InputImageTokens != 5 || usage.OutputImageTokens != 20 {
		t.Fatalf("usage image token details = %#v", usage)
	}
}

func TestOpenAIImagesProtocolPassesThroughErrorsAndDerivesUsageFallbacks(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"message":"policy violation"}}`)
	transformed, err := (openAIImagesProtocolAdapter{}).TransformBufferedResponse(http.StatusBadRequest, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusBadRequest || transformed.ContentType != "application/json" || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected error passthrough: %#v", transformed)
	}
	if transformed.Usage != (gatewayUsage{}) {
		t.Fatalf("error response should not parse usage: %#v", transformed.Usage)
	}

	usage := usageFromOpenAIImagesUsage(&openAIImagesUsage{InputTokens: 3, OutputTokens: 4}, 1)
	if usage.TotalTokens != 7 || usage.ImageCount != 1 {
		t.Fatalf("usage fallback = %#v, want total prompt+completion and image count", usage)
	}

	converted := openAIImagesUsageFromGatewayUsage(gatewayUsage{
		PromptTokens:      9,
		CompletionTokens:  11,
		TotalTokens:       20,
		InputImageTokens:  4,
		OutputImageTokens: 13,
	})
	if converted.InputTokens != 9 || converted.OutputTokens != 11 || converted.TotalTokens != 20 {
		t.Fatalf("converted usage tokens = %#v", converted)
	}
	if converted.InputTokensDetails.TextTokens != 5 || converted.InputTokensDetails.ImageTokens != 4 || converted.OutputTokensDetails.ImageTokens != 13 {
		t.Fatalf("converted usage details = %#v", converted)
	}
}

func TestOpenAIImagesStreamInspectionCapturesPartialCompletedAndErrorEvents(t *testing.T) {
	t.Parallel()

	var capture streamCaptureState
	inspectOpenAIImagesStreamLine([]byte("data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cGFydA==\"}\n\n"), &capture)
	if capture.usage.ImageCount != 1 {
		t.Fatalf("partial image usage = %#v, want image count 1", capture.usage)
	}

	inspectOpenAIImagesStreamLine([]byte("data: {\"type\":\"image_generation.completed\",\"response\":{\"data\":[{\"b64_json\":\"MQ==\"},{\"b64_json\":\"Mg==\"}],\"usage\":{\"input_tokens\":5,\"output_tokens\":8,\"input_tokens_details\":{\"text_tokens\":3,\"image_tokens\":2}}}}\n\n"), &capture)
	if !capture.streamCompleted || !capture.sawDone {
		t.Fatalf("expected completed stream flags, capture=%+v", capture)
	}
	if capture.usage.TotalTokens != 13 || capture.usage.ImageCount != 2 || capture.usage.InputTextTokens != 3 {
		t.Fatalf("completed usage = %#v", capture.usage)
	}

	inspectOpenAIImagesStreamLine([]byte("data: {\"type\":\"error\",\"error\":{\"message\":\"boom\"}}\n\n"), &capture)
	if capture.endReason != "upstream_stream_error" {
		t.Fatalf("endReason = %q, want upstream_stream_error", capture.endReason)
	}
}

func TestOpenAIImagesProtocolProxyStreamReportsMissingBody(t *testing.T) {
	t.Parallel()

	capture, started, err := (openAIImagesProtocolAdapter{}).ProxyStream(t.Context(), httptest.NewRecorder(), &http.Response{StatusCode: http.StatusOK}, time.Now(), routeengine.Candidate{})
	assertMissingBodyStreamCapture(t, "OpenAI images ProxyStream", capture, started, err)

	resp := gatewayStreamTestResponse(
		"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cGFydA==\"}\n\n" +
			"data: [DONE]\n\n",
	)
	rec := httptest.NewRecorder()
	capture, started, err = (openAIImagesProtocolAdapter{}).ProxyStream(t.Context(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), "image_generation.partial_image")
}
