package gateway

import "testing"

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
