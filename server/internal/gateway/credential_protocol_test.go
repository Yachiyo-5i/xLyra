package gateway

import (
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestCodexImageCredentialSupportsResolvedAdapter(t *testing.T) {
	request := typedGatewayRequest(gatewayEndpointImagesGenerations, map[string]any{"model": "gpt-image-2", "prompt": "draw a cat"})
	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), request, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2", SupportedEndpointTypes: []string{"openai-image"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !credentialSupportsAdapter([]string{"openai-image"}, protocol) {
		t.Fatal("image credential must support the resolved Codex image adapter")
	}
	if credentialSupportsAdapter([]string{"openai-response"}, protocol) {
		t.Fatal("responses-only credential must not grant image access")
	}
	textProtocol := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses})
	if credentialSupportsAdapter([]string{"openai-image"}, textProtocol) {
		t.Fatal("image-only credential must not grant text access")
	}
	if !credentialSupportsAdapter([]string{"openai-response"}, textProtocol) {
		t.Fatal("responses credential must retain text access")
	}
}

func TestCredentialSupportsProtocolMapsAdaptersToEndpointTypes(t *testing.T) {
	tests := []struct {
		protocol string
		endpoint string
	}{
		{protocol: "openai_chat_completions", endpoint: "openai"},
		{protocol: "openai_responses_to_messages", endpoint: "openai-response"},
		{protocol: "anthropic_messages_to_responses", endpoint: "anthropic-messages"},
		{protocol: "google_generate_content", endpoint: "google-gemini"},
		{protocol: "openai_images_generations", endpoint: "openai-image"},
		{protocol: "openai_embeddings", endpoint: "openai-embedding"},
		{protocol: "openai_audio_speech", endpoint: "openai-audio-speech"},
		{protocol: "codex_responses", endpoint: "openai-response"},
	}
	for _, test := range tests {
		if !credentialSupportsProtocol([]string{test.endpoint}, test.protocol) {
			t.Fatalf("protocol %q should match endpoint %q", test.protocol, test.endpoint)
		}
	}
}

func TestCredentialSupportsProtocolRejectsUnconfiguredEndpoint(t *testing.T) {
	if credentialSupportsProtocol([]string{"openai-response"}, "openai_chat_completions") {
		t.Fatal("chat protocol should not match responses endpoint")
	}
}
