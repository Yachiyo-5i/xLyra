package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"xlyra/server/internal/credential"

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

func TestSiteModelTestDownstreamPathAllowsProtocolConversion(t *testing.T) {
	t.Parallel()
	path, err := siteModelTestDownstreamPathForProtocol([]string{upstreamEndpointTypeAnthropicMessages}, siteModelTestProtocolResponses)
	if err != nil || path != gatewayEndpointResponses {
		t.Fatalf("responses protocol path = %q err=%v", path, err)
	}
	for _, endpoints := range [][]string{nil, {}} {
		_, err := siteModelTestDownstreamPathForProtocol(endpoints, siteModelTestProtocolAuto)
		var testErr *SiteModelTestError
		if !errors.As(err, &testErr) || testErr.Code != "model_test_protocol_unavailable" || testErr.StatusCode != http.StatusBadRequest {
			t.Fatalf("error = %v, want 400 model_test_protocol_unavailable", err)
		}
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
			UpstreamName:           "gpt-5.4-codex",
			SupportedEndpointTypes: []string{upstreamEndpointTypeOpenAIResponse},
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
		{name: "codex", model: "gpt-5.4", wantUA: codexGatewayUserAgent()},
		{name: "claude_code", model: "claude-sonnet-4-5", wantUA: claudeCodeGatewayUserAgent(), wantSID: true},
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
			UpstreamName:           "gpt-5.4",
			SupportedEndpointTypes: []string{upstreamEndpointTypeOpenAI, upstreamEndpointTypeOpenAIResponse, upstreamEndpointTypeAnthropicMessages},
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

func TestSiteModelTestSelectsKeyBeforeProtocol(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name         string
		siteType     string
		protocol     string
		keyPolicy    string
		sitePolicy   string
		selected     string
		unavailable  bool
		wantCode     string
		wantProtocol string
		wantPath     string
		wantKey      string
	}{
		{name: "auto uses key chat allowlist", keyPolicy: `{"endpoint_override":{"mode":"allowlist","endpoint_types":["openai"]}}`, wantProtocol: "openai_chat_completions", wantPath: "/v1/chat/completions", wantKey: "first"},
		{name: "auto uses key responses declaration", keyPolicy: `{"supported_endpoint_types":["openai-response"]}`, wantProtocol: "openai_responses", wantPath: "/v1/responses", wantKey: "first"},
		{name: "auto uses key messages allowlist", keyPolicy: `{"endpoint_override":{"mode":"allowlist","endpoint_types":["anthropic-messages"]}}`, wantProtocol: "anthropic_messages", wantPath: "/v1/messages", wantKey: "first"},
		{name: "explicit key uses its own protocols", selected: "second", keyPolicy: `{"supported_endpoint_types":["openai"]}`, wantProtocol: "openai_responses", wantPath: "/v1/responses", wantKey: "second"},
		{name: "manual supported protocol", protocol: "responses", keyPolicy: `{"supported_endpoint_types":["openai-response"]}`, wantProtocol: "openai_responses", wantPath: "/v1/responses", wantKey: "first"},
		{name: "manual protocol converts through another key", protocol: "responses", keyPolicy: `{"supported_endpoint_types":["openai"]}`, wantProtocol: "openai_chat_completions_to_responses", wantPath: "/v1/chat/completions", wantKey: "first"},
		{name: "explicit key converts unsupported protocol", selected: "first", protocol: "messages", keyPolicy: `{"supported_endpoint_types":["openai"]}`, wantProtocol: "openai_chat_completions_to_messages", wantPath: "/v1/chat/completions", wantKey: "first"},
		{name: "disabled key model is skipped", keyPolicy: `{"endpoint_override":{"mode":"disabled"}}`, wantProtocol: "openai_responses", wantPath: "/v1/responses", wantKey: "second"},
		{name: "unavailable key model is skipped", unavailable: true, wantProtocol: "openai_responses", wantPath: "/v1/responses", wantKey: "second"},
		{name: "explicit disabled key model rejected", selected: "first", keyPolicy: `{"endpoint_override":{"mode":"disabled"}}`, wantCode: "model_test_credential_unavailable"},
		{name: "empty key allowlist rejected", selected: "first", keyPolicy: `{"endpoint_override":{"mode":"allowlist","endpoint_types":[]}}`, wantCode: "model_test_credential_unavailable"},
		{name: "unknown key rejected", selected: "unknown", wantCode: "model_test_credential_unavailable"},
		{name: "site override restricts key", sitePolicy: "allowlist", keyPolicy: `{"supported_endpoint_types":["openai","openai-response"]}`, wantProtocol: "openai_chat_completions", wantPath: "/v1/chat/completions", wantKey: "first"},
		{name: "disabled site model rejected", sitePolicy: "disabled", wantCode: "model_test_protocol_unavailable"},
		{name: "xlyra disabled keys cannot fall back", siteType: "xlyra", sitePolicy: "allowlist", keyPolicy: `{"endpoint_override":{"mode":"disabled"}}`, wantCode: "model_test_credential_unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			siteID, modelID, canonicalID := uuid.New(), uuid.New(), uuid.New()
			firstID, secondID := uuid.New(), uuid.New()
			siteType := tt.siteType
			if siteType == "" {
				siteType = "openai"
			}
			secrets := credential.NewService("model-test-master-key")
			credentials := make([]store.SiteCredential, 0, 2)
			for i, name := range []string{"first", "second"} {
				encrypted, masked, err := secrets.Encrypt(name)
				if err != nil {
					t.Fatal(err)
				}
				credentials = append(credentials, store.SiteCredential{ID: []uuid.UUID{firstID, secondID}[i], SiteID: siteID, CredentialType: "api_key", EncryptedSecret: encrypted, MaskedSecret: masked, DisplayName: name, RoutingPriority: float64(2 - i)})
			}
			bindings := []store.SiteAPIKeyModel{
				{SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, SiteCredentialID: firstID, Available: !tt.unavailable, Enabled: true, Raw: store.JSON(tt.keyPolicy)},
				{SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, SiteCredentialID: secondID, Available: true, Enabled: true, Raw: store.JSON(`{"supported_endpoint_types":["openai-response"]}`)},
			}
			credentialReads := 0
			db := gatewayStoreWithQueryCallback(t, func(tx *gorm.DB) {
				tx.Statement.RowsAffected = 1
				switch dest := tx.Statement.Dest.(type) {
				case *store.Site:
					*dest = store.Site{ID: siteID, SiteType: siteType, BaseURL: "https://model-test.invalid"}
				case *store.SiteModel:
					*dest = store.SiteModel{ID: modelID, SiteID: siteID, CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}, UpstreamName: "test-model", Capabilities: store.JSON(`{"supported_endpoint_types":["openai","openai-response","anthropic-messages"]}`)}
				case *store.CanonicalModel:
					*dest = store.CanonicalModel{ID: canonicalID, ModelKey: "test-model", SupportedEndpointTypes: store.JSON(`["openai","openai-response","anthropic-messages"]`)}
				case *store.SiteModelEndpointOverride:
					if tt.sitePolicy == "" {
						tx.AddError(gorm.ErrRecordNotFound)
						return
					}
					*dest = store.SiteModelEndpointOverride{SiteModelID: modelID, Mode: tt.sitePolicy, EndpointTypes: store.JSON(`["openai"]`)}
				case *[]store.SiteAPIKeyModel:
					*dest = bindings
				case *[]store.SiteCredential:
					credentialReads++
					*dest = credentials
				case *[]store.SiteAPIKeyState, *[]store.RouteCooldown, *[]store.SiteModelPricing:
					tx.Statement.RowsAffected = 0
				default:
					t.Fatalf("unexpected query: %T", dest)
				}
			})
			sent := 0
			handler := Handler{db: db, credentials: secrets, logger: gatewayDiscardLogger(), httpClient: &http.Client{Transport: siteModelTestRoundTripper(func(req *http.Request) (*http.Response, error) {
				sent++
				if tt.wantProtocol == "anthropic_messages" {
					if got := req.Header.Get("X-Api-Key"); got != tt.wantKey {
						t.Errorf("X-Api-Key = %q", got)
					}
				} else if got := req.Header.Get("Authorization"); got != "Bearer "+tt.wantKey {
					t.Errorf("Authorization = %q", got)
				}
				if req.URL.Path != tt.wantPath {
					t.Errorf("path = %q, want %q", req.URL.Path, tt.wantPath)
				}
				return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"test upstream response"}}`)), Request: req}, nil
			})}}
			selectedID := map[string]uuid.UUID{"first": firstID, "second": secondID, "unknown": uuid.New()}[tt.selected]
			stream := false
			result, err := handler.TestSiteModel(ctx, SiteModelTestInput{SiteID: siteID, SiteModelID: modelID, SiteCredentialID: selectedID, Protocol: tt.protocol, Stream: &stream})
			if tt.wantCode != "" {
				var testErr *SiteModelTestError
				if !errors.As(err, &testErr) || testErr.Code != tt.wantCode || testErr.StatusCode != http.StatusBadRequest {
					t.Fatalf("error = %v, want 400 %s", err, tt.wantCode)
				}
				if sent != 0 {
					t.Fatalf("sent %d requests for rejected test", sent)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if sent != 1 || credentialReads != 1 {
				t.Fatalf("requests = %d, credential reads = %d, want 1 each", sent, credentialReads)
			}
			if result.Request.UpstreamProtocol != tt.wantProtocol {
				t.Errorf("protocol = %q, want %q", result.Request.UpstreamProtocol, tt.wantProtocol)
			}
			if result.Request.SiteCredentialID != map[string]uuid.UUID{"first": firstID, "second": secondID}[tt.wantKey] {
				t.Errorf("unexpected credential ID: %s", result.Request.SiteCredentialID)
			}
		})
	}
}

type siteModelTestRoundTripper func(*http.Request) (*http.Response, error)

func (f siteModelTestRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSiteModelTestNativeProtocolResolution(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		siteType     string
		endpoints    []string
		wantProtocol string
		wantCode     string
	}{
		{siteType: "google", endpoints: []string{"google-gemini"}, wantProtocol: "google_generate_content"},
		{siteType: "codex", endpoints: []string{"openai", "openai-response"}, wantProtocol: "codex_responses"},
		{siteType: "antigravity", endpoints: []string{"openai", "openai-response", "google-gemini"}, wantProtocol: "antigravity_generate_content"},
		{siteType: "anthropic", endpoints: []string{"openai"}, wantProtocol: "anthropic_messages_to_chat_completions"},
	} {
		t.Run(tt.siteType, func(t *testing.T) {
			candidate := siteModelTestRouteCandidate()
			candidate.Site.SiteType = tt.siteType
			candidate.Model.SupportedEndpointTypes = tt.endpoints
			path, err := siteModelTestDownstreamPath(tt.endpoints)
			if err != nil {
				t.Fatal(err)
			}
			request, err := siteModelTestGatewayRequest(path, "test-model", "ok", false)
			if err != nil {
				t.Fatal(err)
			}
			adapter, err := (Handler{}).siteModelTestProtocolAdapter(context.Background(), request, candidate, siteModelTestProtocolAuto)
			if tt.wantCode != "" {
				var testErr *SiteModelTestError
				if !errors.As(err, &testErr) || testErr.Code != tt.wantCode {
					t.Fatalf("error = %v, want %s", err, tt.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if adapter.ProtocolName() != tt.wantProtocol {
				t.Fatalf("protocol = %q, want %q", adapter.ProtocolName(), tt.wantProtocol)
			}
		})
	}
}
