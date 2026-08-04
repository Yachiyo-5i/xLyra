package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestOpenAIEmbeddingsProtocolBuildsPayloadAndPath(t *testing.T) {
	t.Parallel()

	protocol := newOpenAIEmbeddingsProtocolAdapter(gatewayRequest{}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "text-embedding-3-large"},
	})

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		Payload: map[string]any{
			"model":           "alias-embedding",
			"input":           []any{"hello", "world"},
			"encoding_format": "float",
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "text-embedding-3-large"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "text-embedding-3-large" {
		t.Fatalf("model = %#v, want upstream model", payload["model"])
	}
	if payload["encoding_format"] != "float" {
		t.Fatalf("encoding_format = %#v, want float", payload["encoding_format"])
	}
	if got := protocol.UpstreamPath("https://api.example.test"); got != "https://api.example.test/v1/embeddings" {
		t.Fatalf("UpstreamPath = %q, want embeddings path", got)
	}
	if got := protocol.ProtocolName(); got != "openai_embeddings" {
		t.Fatalf("ProtocolName = %q, want openai_embeddings", got)
	}
}

func TestOpenAIEmbeddingsProtocolTransformsBufferedResponseUsage(t *testing.T) {
	t.Parallel()

	body := []byte(`{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"text-embedding-3-large","usage":{"prompt_tokens":8,"total_tokens":8}}`)
	transformed, err := (openAIEmbeddingsProtocolAdapter{}).TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusOK || transformed.ContentType != "application/json" || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected transformed response: %#v", transformed)
	}
	if transformed.Usage.PromptTokens != 8 || transformed.Usage.TotalTokens != 8 {
		t.Fatalf("usage = %#v, want prompt/total tokens 8", transformed.Usage)
	}
}

func TestOpenAIEmbeddingsProtocolPassesThroughErrorsAndDoesNotStream(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"message":"invalid input"}}`)
	transformed, err := (openAIEmbeddingsProtocolAdapter{}).TransformBufferedResponse(http.StatusBadRequest, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusBadRequest || transformed.ContentType != "application/json" || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected error passthrough: %#v", transformed)
	}
	if transformed.Usage != (gatewayUsage{}) {
		t.Fatalf("error response should not parse usage: %#v", transformed.Usage)
	}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {}\n\n")),
	}
	capture, started, err := (openAIEmbeddingsProtocolAdapter{}).ProxyStream(t.Context(), httptest.NewRecorder(), resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if started || capture.usage != (gatewayUsage{}) || capture.endReason != "" || capture.streamCompleted || capture.sawDone {
		t.Fatalf("embeddings stream should be unsupported no-op, started=%v capture=%+v", started, capture)
	}
}
