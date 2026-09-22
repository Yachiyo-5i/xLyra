package gateway

import (
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestProviderResponsesEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		siteType string
		model    string
		baseURL  string
		wantURL  string
	}{
		{"zhipu", "zhipu", "glm-5.3", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/v1/responses"},
		{"deepseek", "deepseek", "deepseek-flash", "https://api.deepseek.com", "https://api.deepseek.com/responses"},
		{"zhipu empty base", "zhipu", "glm-5.3", "", "https://open.bigmodel.cn/api/v1/responses"},
		{"deepseek empty base", "deepseek", "deepseek-flash", "", "https://api.deepseek.com/responses"},
		{"zhipu relay", "newapi", "glm-5.3", "https://relay.example.test/root/", "https://relay.example.test/root/v1/responses"},
		{"deepseek relay", "newapi", "deepseek-v4-flash", "https://relay.example.test/root/", "https://relay.example.test/root/v1/responses"},
		{"openai", "openai", "glm-5.3", "https://relay.example.test", "https://relay.example.test/v1/responses"},
		{"glm coding", "glm_code", "glm-5.3", "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/api/v1/responses"},
		{"opencode spec base", "opencode_go", "gpt-5.6-luna", "", "https://opencode.ai/zen/go/v1/responses"},
		{"mimo default", "xiaomi_mimo", "mimo-v2.6-pro", "", "https://token-plan-cn.xiaomimimo.com/v1/responses"},
		{"mimo custom region", "xiaomi_mimo", "mimo-v2.6-pro", "https://token-plan-sgp.xiaomimimo.com", "https://token-plan-sgp.xiaomimimo.com/v1/responses"},
		{"mimo custom base", "xiaomi_mimo", "mimo-v2.6-pro", "https://mimo.example.test/custom", "https://mimo.example.test/custom/v1/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: tt.siteType, BaseURL: tt.baseURL},
				Model: routeengine.CandidateModel{
					UpstreamName:           tt.model,
					SupportedEndpointTypes: []string{"openai", "openai-response", "anthropic-messages"},
				},
			}
			request := gatewayRequest{DownstreamPath: gatewayEndpointResponses}
			for _, mode := range []string{"gateway", siteModelTestProtocolAuto, siteModelTestProtocolResponses} {
				t.Run(mode, func(t *testing.T) {
					var protocol gatewayProtocolAdapter
					var err error
					if mode == "gateway" {
						protocol, err = (openAIProtocolResolver{}).Resolve(t.Context(), request, candidate)
					} else {
						protocol, err = (Handler{}).siteModelTestProtocolAdapter(t.Context(), request, candidate, mode)
					}
					if err != nil {
						t.Fatal(err)
					}
					if got := protocol.ProtocolName(); got != "openai_responses" {
						t.Fatalf("ProtocolName = %q, want openai_responses", got)
					}
					if got := protocol.UpstreamPath(candidate.Site.BaseURL); got != tt.wantURL {
						t.Fatalf("UpstreamPath = %q, want %q", got, tt.wantURL)
					}
				})
			}
		})
	}
}

func TestProviderResponsesEndpointPreservesProtocolSelection(t *testing.T) {
	t.Parallel()

	providers := []struct {
		siteType     string
		model        string
		baseURL      string
		responsesURL string
		chatURL      string
		messagesURL  string
	}{
		{"zhipu", "glm-5.3", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/v1/responses", "https://open.bigmodel.cn/api/paas/v4/chat/completions", "https://open.bigmodel.cn/api/anthropic/v1/messages"},
		{"deepseek", "deepseek-flash", "https://api.deepseek.com", "https://api.deepseek.com/responses", "https://api.deepseek.com/v1/chat/completions", "https://api.deepseek.com/anthropic/v1/messages"},
	}
	for _, provider := range providers {
		t.Run(provider.siteType, func(t *testing.T) {
			t.Parallel()
			tests := []struct {
				name      string
				path      string
				endpoints []string
				wantName  string
				wantURL   string
			}{
				{"chat unchanged", gatewayEndpointChatCompletions, []string{"openai", "openai-response"}, "openai_chat_completions", provider.chatURL},
				{"messages unchanged", gatewayEndpointMessages, []string{"openai", "openai-response", "anthropic-messages"}, provider.siteType + "_anthropic_messages", provider.messagesURL},
				{"chat fallback", gatewayEndpointResponses, []string{"openai"}, "openai_chat_completions_to_responses", provider.chatURL},
				{"messages fallback", gatewayEndpointResponses, []string{"anthropic-messages"}, provider.siteType + "_anthropic_messages_to_responses", provider.messagesURL},
				{"responses only conversion", gatewayEndpointChatCompletions, []string{"openai-response"}, "openai_responses", provider.responsesURL},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					candidate := routeengine.Candidate{
						Site:  routeengine.CandidateSite{SiteType: provider.siteType, BaseURL: provider.baseURL},
						Model: routeengine.CandidateModel{UpstreamName: provider.model, SupportedEndpointTypes: tt.endpoints},
					}
					protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{DownstreamPath: tt.path}, candidate)
					if err != nil {
						t.Fatal(err)
					}
					if got := protocol.ProtocolName(); got != tt.wantName {
						t.Fatalf("ProtocolName = %q, want %q", got, tt.wantName)
					}
					if got := protocol.UpstreamPath(candidate.Site.BaseURL); got != tt.wantURL {
						t.Fatalf("UpstreamPath = %q, want %q", got, tt.wantURL)
					}
				})
			}
		})
	}
}
