package gateway

import "testing"

func TestEmbeddingsEndpointAdapterDecodesRequest(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, embeddingsEndpointAdapter{}, `{"model":" text-embedding-3-large ","input":"hello"}`, gatewayEndpointEmbeddings, "text-embedding-3-large")
	if request.Stream {
		t.Fatal("expected stream=false")
	}
	if request.Payload["input"] != "hello" {
		t.Fatalf("payload input = %#v, want hello", request.Payload["input"])
	}
}

func TestEmbeddingsEndpointAdapterRejectsInvalidJSONAndMissingModel(t *testing.T) {
	t.Parallel()

	adapter := embeddingsEndpointAdapter{}
	assertEndpointDecodeFailure(t, "invalid JSON", adapter, `{`, "invalid_json", "decode")
	assertEndpointDecodeFailure(t, "missing model", adapter, `{"input":"hello"}`, "invalid_model", "validate")
}
