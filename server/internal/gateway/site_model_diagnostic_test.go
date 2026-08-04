package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSiteModelTestDownstreamPathPrefersAnthropicMessages(t *testing.T) {
	t.Parallel()

	path, err := siteModelTestDownstreamPath([]string{upstreamEndpointTypeOpenAIResponse, upstreamEndpointTypeAnthropicMessages})
	if err != nil {
		t.Fatalf("siteModelTestDownstreamPath returned error: %v", err)
	}
	if path != gatewayEndpointMessages {
		t.Fatalf("path = %q, want %q", path, gatewayEndpointMessages)
	}
}

func TestSiteModelTestDownstreamPathUsesResponses(t *testing.T) {
	t.Parallel()

	path, err := siteModelTestDownstreamPath([]string{upstreamEndpointTypeOpenAIResponse})
	if err != nil {
		t.Fatalf("siteModelTestDownstreamPath returned error: %v", err)
	}
	if path != gatewayEndpointResponses {
		t.Fatalf("path = %q, want %q", path, gatewayEndpointResponses)
	}
}

func TestSiteModelTestDownstreamPathRejectsImageOnlyModel(t *testing.T) {
	t.Parallel()

	_, err := siteModelTestDownstreamPath([]string{upstreamEndpointTypeOpenAIImage})
	if err == nil {
		t.Fatal("expected image-only model to be rejected")
	}
	testErr, ok := err.(*SiteModelTestError)
	if !ok {
		t.Fatalf("error type = %T, want *SiteModelTestError", err)
	}
	if testErr.Code != "image_model_not_supported" {
		t.Fatalf("code = %q, want image_model_not_supported", testErr.Code)
	}
}

func TestSiteModelTestGatewayRequestBuildsDiagnosticRequest(t *testing.T) {
	t.Parallel()

	request, err := siteModelTestGatewayRequest(gatewayEndpointChatCompletions, "gpt-5.4", "Reply with only: ok", true)
	if err != nil {
		t.Fatalf("siteModelTestGatewayRequest returned error: %v", err)
	}
	if !request.Diagnostic {
		t.Fatal("expected diagnostic request")
	}
	if !request.Stream {
		t.Fatal("expected stream request")
	}
	if request.Canonical == nil {
		t.Fatal("expected canonical request")
	}
	if got := request.Payload["stream"]; got != true {
		t.Fatalf("stream = %#v, want true", got)
	}
	if got := request.Payload["max_tokens"]; got != 16 {
		t.Fatalf("max_tokens = %#v, want 16", got)
	}
}

func TestSiteModelTestGatewayRequestBuildsNonStreamResponsesRequest(t *testing.T) {
	t.Parallel()

	request, err := siteModelTestGatewayRequest(gatewayEndpointResponses, "gpt-5.4", "Reply with only: ok", false)
	if err != nil {
		t.Fatalf("siteModelTestGatewayRequest returned error: %v", err)
	}
	if request.Stream {
		t.Fatal("expected non-stream request")
	}
	if request.DownstreamPath != gatewayEndpointResponses {
		t.Fatalf("DownstreamPath = %q, want %q", request.DownstreamPath, gatewayEndpointResponses)
	}
	if got := request.Payload["stream"]; got != false {
		t.Fatalf("stream = %#v, want false", got)
	}
	if got := request.Payload["max_output_tokens"]; got != 16 {
		t.Fatalf("max_output_tokens = %#v, want 16", got)
	}
}

func TestSiteModelTestDownstreamPathForProtocolOverridesAuto(t *testing.T) {
	t.Parallel()

	path, err := siteModelTestDownstreamPathForProtocol([]string{upstreamEndpointTypeAnthropicMessages}, siteModelTestProtocolResponses)
	if err != nil {
		t.Fatalf("siteModelTestDownstreamPathForProtocol returned error: %v", err)
	}
	if path != gatewayEndpointResponses {
		t.Fatalf("path = %q, want %q", path, gatewayEndpointResponses)
	}
}

func TestSiteModelTestProtocolAdapterHonorsManualProtocol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		protocol     string
		wantProtocol string
	}{
		{
			name:         "chat completions",
			path:         gatewayEndpointChatCompletions,
			protocol:     siteModelTestProtocolChatCompletions,
			wantProtocol: "openai_chat_completions",
		},
		{
			name:         "responses",
			path:         gatewayEndpointResponses,
			protocol:     siteModelTestProtocolResponses,
			wantProtocol: "openai_responses",
		},
		{
			name:         "messages",
			path:         gatewayEndpointMessages,
			protocol:     siteModelTestProtocolMessages,
			wantProtocol: "anthropic_messages",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request, err := siteModelTestGatewayRequest(tt.path, "gpt-5.4", "Reply with only: ok", true)
			if err != nil {
				t.Fatalf("siteModelTestGatewayRequest returned error: %v", err)
			}
			adapter, err := (Handler{}).siteModelTestProtocolAdapter(context.Background(), request, siteModelTestRouteCandidate(), tt.protocol)
			if err != nil {
				t.Fatalf("siteModelTestProtocolAdapter returned error: %v", err)
			}
			if got := adapter.ProtocolName(); got != tt.wantProtocol {
				t.Fatalf("ProtocolName = %q, want %q", got, tt.wantProtocol)
			}
		})
	}
}

func TestSiteModelTestProtocolAdapterKeepsCodexAdapterForManualResponses(t *testing.T) {
	t.Parallel()

	request, err := siteModelTestGatewayRequest(gatewayEndpointResponses, "gpt-5.4", "Reply with only: ok", true)
	if err != nil {
		t.Fatalf("siteModelTestGatewayRequest returned error: %v", err)
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "codex",
			BaseURL:  "https://chatgpt.com/backend-api",
		},
		Model: routeengine.CandidateModel{
			UpstreamName: "gpt-5.4-codex",
		},
	}

	adapter, err := (Handler{}).siteModelTestProtocolAdapter(context.Background(), request, candidate, siteModelTestProtocolResponses)
	if err != nil {
		t.Fatalf("siteModelTestProtocolAdapter returned error: %v", err)
	}
	if got := adapter.ProtocolName(); got != "codex_responses" {
		t.Fatalf("ProtocolName = %q, want codex_responses", got)
	}
	if got := adapter.UpstreamPath(candidate.Site.BaseURL); got != "https://chatgpt.com/backend-api/codex/responses" {
		t.Fatalf("UpstreamPath = %q, want https://chatgpt.com/backend-api/codex/responses", got)
	}
}

func TestFirstSiteModelTestGatewayCredentialUsesGatewayOrderedCandidate(t *testing.T) {
	t.Parallel()

	gptOnlyCredentialID := uuid.New()
	claudeCredentialID := uuid.New()
	selected, err := firstSiteModelTestGatewayCredential([]store.GatewayCredential{
		{Credential: store.SiteCredential{ID: claudeCredentialID, MaskedSecret: "sk-claude"}},
		{Credential: store.SiteCredential{ID: gptOnlyCredentialID, MaskedSecret: "sk-gpt"}},
	})
	if err != nil {
		t.Fatalf("firstSiteModelTestGatewayCredential returned error: %v", err)
	}
	if selected.Credential.ID != claudeCredentialID {
		t.Fatalf("credential ID = %s, want %s", selected.Credential.ID, claudeCredentialID)
	}
}

func TestFirstSiteModelTestGatewayCredentialRejectsEmptyCandidates(t *testing.T) {
	t.Parallel()

	_, err := firstSiteModelTestGatewayCredential(nil)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestApplyOpenAISiteModelTestUserAgentDefaultsOpenAIAdapters(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://api.example.test/v1/responses", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	applyOpenAISiteModelTestUserAgent(req, openAIResponsesProtocolAdapter{})
	if got := req.Header.Get("User-Agent"); got != openAISiteModelTestUA {
		t.Fatalf("User-Agent = %q, want %q", got, openAISiteModelTestUA)
	}
}

func TestApplyOpenAISiteModelTestUserAgentPreservesExistingValue(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://api.example.test/v1/responses", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("User-Agent", "custom-agent")
	applyOpenAISiteModelTestUserAgent(req, openAIResponsesProtocolAdapter{})
	if got := req.Header.Get("User-Agent"); got != "custom-agent" {
		t.Fatalf("User-Agent = %q, want custom-agent", got)
	}
}

func TestSiteModelTestClientImpersonationOverridesDiagnosticUserAgent(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		model   string
		wantUA  string
		wantSID bool
	}{
		{name: "codex", model: "gpt-5.4", wantUA: codexGatewayUserAgent},
		{name: "claude_code", model: "claude-sonnet-4-5", wantUA: claudeCodeGatewayUserAgent, wantSID: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://api.example.test/v1/responses", nil)
			if err != nil {
				t.Fatalf("NewRequest returned error: %v", err)
			}

			protocol := openAIResponsesProtocolAdapter{}
			applyOpenAISiteModelTestUserAgent(req, protocol)
			applyGatewayClientImpersonationHeaders(req, &sitepkg.GatewayConfig{
				ImpersonateCodexClient:      testBool(true),
				ImpersonateClaudeCodeClient: testBool(true),
			}, protocol, testGatewayRequest(tt.model), testRouteCandidate(tt.model), "")

			if got := req.Header.Get("User-Agent"); got != tt.wantUA {
				t.Fatalf("User-Agent = %q, want %q", got, tt.wantUA)
			}
			if got := req.Header.Get("X-Claude-Code-Session-Id"); (got != "") != tt.wantSID {
				t.Fatalf("X-Claude-Code-Session-Id presence = %t, want %t", got != "", tt.wantSID)
			}
		})
	}
}

func siteModelTestRouteCandidate() routeengine.Candidate {
	return routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "openai",
			BaseURL:  "https://api.example.test",
		},
		Model: routeengine.CandidateModel{
			UpstreamName: "gpt-5.4",
		},
	}
}

func TestNormalizeSiteModelTestProtocolRejectsInvalidProtocol(t *testing.T) {
	t.Parallel()

	_, err := normalizeSiteModelTestProtocol("invalid")
	if err == nil {
		t.Fatal("expected invalid protocol to be rejected")
	}
	testErr, ok := err.(*SiteModelTestError)
	if !ok {
		t.Fatalf("error type = %T, want *SiteModelTestError", err)
	}
	if testErr.Code != "invalid_protocol" {
		t.Fatalf("code = %q, want invalid_protocol", testErr.Code)
	}
}
