package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestImagesGenerationsEndpointAdapterDecodesRequest(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, imagesGenerationsEndpointAdapter{}, `{"model":"gpt-image-2","prompt":"draw a cat","n":2}`, gatewayEndpointImagesGenerations, "gpt-image-2")
	if request.Stream {
		t.Fatal("expected stream=false")
	}
}

func TestImagesGenerationsEndpointAdapterDecodesStreamRequest(t *testing.T) {
	t.Parallel()

	request, failure := decodeEndpointRequest(t, imagesGenerationsEndpointAdapter{}, `{"model":"gpt-image-2","prompt":"draw a cat","stream":true}`)
	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %+v", failure)
	}
	if !request.Stream {
		t.Fatal("expected stream=true")
	}
}

func TestImagesGenerationsEndpointAdapterRejectsInvalidJSONAndMissingModel(t *testing.T) {
	t.Parallel()

	adapter := imagesGenerationsEndpointAdapter{}
	assertEndpointDecodeFailure(t, "invalid JSON", adapter, `{`, "invalid_json", "decode")
	assertEndpointDecodeFailure(t, "missing model", adapter, `{"prompt":"draw a cat"}`, "invalid_model", "validate")
}

func TestImagesGenerationsEndpointAdapterDecodesMultipart(t *testing.T) {
	t.Parallel()

	body, contentType := buildMultipartImageRequest(t, map[string]string{
		"prompt": "draw a cat",
		"model":  "gpt-image-2",
	}, nil)

	req := httptest.NewRequest(http.MethodPost, gatewayEndpointImagesGenerations, body)
	req.Header.Set("Content-Type", contentType)

	request, failure := imagesGenerationsEndpointAdapter{}.DecodeRequest(req)
	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %+v", failure)
	}
	if request.DownstreamPath != gatewayEndpointImagesGenerations {
		t.Fatalf("DownstreamPath = %q, want %q", request.DownstreamPath, gatewayEndpointImagesGenerations)
	}
	if request.RequestedModel != "gpt-image-2" {
		t.Fatalf("RequestedModel = %q, want gpt-image-2", request.RequestedModel)
	}
	if request.Canonical == nil || request.Canonical.Image == nil {
		t.Fatal("expected canonical image request")
	}
	if request.Canonical.Image.Action != "generate" {
		t.Fatalf("Action = %q, want generate", request.Canonical.Image.Action)
	}
}

func TestOpenAIImagesProtocolAdapterStreamsGenerationEvents(t *testing.T) {
	t.Parallel()

	adapter := newOpenAIImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations})
	resp := gatewayStreamTestResponse(
		"event: image_generation.completed\n" +
			"data: {\"type\":\"image_generation.completed\",\"b64_json\":\"ZmluYWw=\",\"usage\":{\"input_tokens\":5,\"output_tokens\":9,\"total_tokens\":14}}\n\n",
	)
	rec := httptest.NewRecorder()

	capture, started, err := adapter.ProxyStream(t.Context(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("started=%v streamCompleted=%v", started, capture.streamCompleted)
	}
	if capture.usage.TotalTokens != 14 || capture.usage.ImageCount != 1 {
		t.Fatalf("usage = %#v", capture.usage)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), "event: image_generation.completed")
}
