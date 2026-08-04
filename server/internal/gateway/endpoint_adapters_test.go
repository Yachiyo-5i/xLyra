package gateway

import "testing"

func TestEndpointAdapters_RouteEndpointType(t *testing.T) {
	tests := []struct {
		name     string
		adapter  gatewayEndpointAdapter
		wantType string
		wantPath string
	}{
		{
			name:     "ChatCompletions",
			adapter:  chatCompletionsEndpointAdapter{},
			wantType: upstreamEndpointTypeOpenAI,
			wantPath: "/v1/chat/completions",
		},
		{
			name:     "Responses",
			adapter:  responsesEndpointAdapter{},
			wantType: upstreamEndpointTypeOpenAIResponse,
			wantPath: "/v1/responses",
		},
		{
			name:     "AnthropicMessages",
			adapter:  anthropicMessagesEndpointAdapter{},
			wantType: upstreamEndpointTypeAnthropicMessages,
			wantPath: "/v1/messages",
		},
		{
			name:     "ImagesGenerations",
			adapter:  imagesGenerationsEndpointAdapter{},
			wantType: upstreamEndpointTypeOpenAIImage,
			wantPath: "/v1/images/generations",
		},
		{
			name:     "Embeddings",
			adapter:  embeddingsEndpointAdapter{},
			wantType: upstreamEndpointTypeOpenAIEmbedding,
			wantPath: "/v1/embeddings",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_RouteEndpointType", func(t *testing.T) {
			got := tt.adapter.RouteEndpointType()
			if got != tt.wantType {
				t.Errorf("RouteEndpointType() = %q, want %q", got, tt.wantType)
			}
		})
		t.Run(tt.name+"_DownstreamPath", func(t *testing.T) {
			got := tt.adapter.DownstreamPath()
			if got != tt.wantPath {
				t.Errorf("DownstreamPath() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestAnthropicMessagesEndpointAdapter_RouteEndpointTypeNotEmpty(t *testing.T) {
	adapter := anthropicMessagesEndpointAdapter{}
	endpointType := adapter.RouteEndpointType()
	if endpointType == "" {
		t.Fatal("anthropicMessagesEndpointAdapter.RouteEndpointType() must not return empty string")
	}
	if endpointType != "anthropic-messages" {
		t.Errorf("RouteEndpointType() = %q, want %q", endpointType, "anthropic-messages")
	}
}

func TestSiteTypeHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fn       func(string) bool
		input    string
		expected bool
	}{
		{"codex lowercase", isCodexSite, "codex", true},
		{"codex uppercase", isCodexSite, "Codex", true},
		{"codex whitespace", isCodexSite, " codex ", true},
		{"codex wrong", isCodexSite, "anthropic", false},

		{"antigravity lowercase", isAntigravitySite, "antigravity", true},
		{"antigravity uppercase", isAntigravitySite, "Antigravity", true},
		{"antigravity wrong", isAntigravitySite, "codex", false},

		{"google lowercase", isGoogleSite, "google_gemini", true},
		{"google uppercase", isGoogleSite, "Google_Gemini", true},
		{"google wrong", isGoogleSite, "openai", false},

		{"anthropic lowercase", isAnthropicSite, "anthropic", true},
		{"anthropic uppercase", isAnthropicSite, "Anthropic", true},
		{"anthropic whitespace", isAnthropicSite, " anthropic ", true},
		{"anthropic wrong", isAnthropicSite, "openai", false},
		{"anthropic partial", isAnthropicSite, "anthropic_messages", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.fn(tt.input)
			if got != tt.expected {
				t.Errorf("%s(%q) = %v, want %v", tt.name, tt.input, got, tt.expected)
			}
		})
	}
}
