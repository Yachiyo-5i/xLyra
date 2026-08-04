package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"

	"xlyra/server/internal/httpx"
)

func TestResponsesEndpointAdapterDecodesRequest(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, responsesEndpointAdapter{}, `{"model":"gpt-5.4","input":"hi","stream":true}`, gatewayEndpointResponses, "gpt-5.4")
	if !request.Stream {
		t.Fatal("expected stream=true")
	}
}

func TestResponsesEndpointAdapterRejectsInvalidJSONAndMissingModel(t *testing.T) {
	t.Parallel()

	adapter := responsesEndpointAdapter{}
	assertEndpointDecodeFailure(t, "invalid JSON", adapter, `{`, "invalid_json", "decode")
	assertEndpointDecodeFailure(t, "missing model", adapter, `{"input":"hi"}`, "invalid_model", "validate")
}

func TestResponsesEndpointAdapterDecodesZstdRequest(t *testing.T) {
	t.Parallel()

	fixture := []byte(`{"model":"gpt-5.4","input":[{"role":"user","content":"hello"}]}`)
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("create zstd encoder: %v", err)
	}
	compressed := encoder.EncodeAll(fixture, nil)
	encoder.Close()

	var decoded gatewayRequest
	var failure *chatFailure
	handler := httpx.DecompressRequestBody(1024, nil)(httpx.LimitRequestBody(1024)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		decoded, failure = responsesEndpointAdapter{}.DecodeRequest(r)
	})))
	req := httptest.NewRequest(http.MethodPost, gatewayEndpointResponses, bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %+v", failure)
	}
	if decoded.RequestedModel != "gpt-5.4" {
		t.Fatalf("RequestedModel = %q, want gpt-5.4", decoded.RequestedModel)
	}
	input, ok := decoded.Payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want one item", decoded.Payload["input"])
	}
}

func TestResponsesEndpointAdapterRejectsInvalidZstdRequest(t *testing.T) {
	t.Parallel()

	var failure *chatFailure
	handler := httpx.DecompressRequestBody(1024, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, failure = responsesEndpointAdapter{}.DecodeRequest(r)
	}))
	req := httptest.NewRequest(http.MethodPost, gatewayEndpointResponses, bytes.NewReader([]byte(`{"model":"gpt-5.4"}`)))
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if failure == nil {
		t.Fatal("expected DecodeRequest failure")
	}
	if failure.status != http.StatusBadRequest || failure.code != "invalid_content_encoding" || failure.stage != "decode" {
		t.Fatalf("failure = %+v", failure)
	}
}
