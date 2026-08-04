package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestNewServiceInitializesDependenciesAndAccessors(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	if service == nil {
		t.Fatal("expected service")
	}
	if service.DB() != nil {
		t.Fatalf("DB() = %#v, want nil", service.DB())
	}
	if service.MasterKey() != "master-key" {
		t.Fatalf("MasterKey() = %q, want master-key", service.MasterKey())
	}
	if service.httpClient == nil || service.httpClients == nil {
		t.Fatalf("expected HTTP clients to be initialized: %#v", service)
	}
}

func TestStartOAuthFlowsRejectMissingStoreBeforeExternalWork(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	if _, err := service.StartCodexFlow(context.Background(), StartCodexFlowParams{
		PublicBaseURL: "https://xlyra.example.com",
	}); err == nil || !strings.Contains(err.Error(), "oauth store is not available") {
		t.Fatalf("StartCodexFlow missing store error = %v", err)
	}
	if _, err := service.StartAntigravityFlow(context.Background(), StartAntigravityFlowParams{
		PublicBaseURL: "https://xlyra.example.com",
	}); err == nil || !strings.Contains(err.Error(), "oauth store is not available") {
		t.Fatalf("StartAntigravityFlow missing store error = %v", err)
	}
}

func TestStartOAuthFlowsRejectInvalidPublicBaseURLBeforeRelay(t *testing.T) {
	t.Parallel()

	service := NewService(&store.Store{}, "master-key")
	for _, tc := range []struct {
		name string
		run  func(context.Context, StartCodexFlowParams) error
	}{
		{
			name: "codex",
			run: func(ctx context.Context, params StartCodexFlowParams) error {
				_, err := service.StartCodexFlow(ctx, params)
				return err
			},
		},
		{
			name: "antigravity",
			run: func(ctx context.Context, params StartCodexFlowParams) error {
				_, err := service.StartAntigravityFlow(ctx, StartAntigravityFlowParams(params))
				return err
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.run(context.Background(), StartCodexFlowParams{PublicBaseURL: "localhost:5173"})
			if err == nil || !strings.Contains(err.Error(), "public_base_url must be a valid backend origin") {
				t.Fatalf("Start%sFlow invalid base URL error = %v", tc.name, err)
			}
		})
	}
}

func TestNormalizeCallbackBaseURLTrimsTrailingSlashAndRejectsInvalidOrigins(t *testing.T) {
	t.Parallel()

	if got := normalizeCallbackBaseURL(" https://xlyra.example.com/api/ "); got != "https://xlyra.example.com/api" {
		t.Fatalf("normalizeCallbackBaseURL = %q, want https://xlyra.example.com/api", got)
	}

	for _, value := range []string{"", "localhost:3000", "/relative", "://bad"} {
		if got := normalizeCallbackBaseURL(value); got != "" {
			t.Fatalf("normalizeCallbackBaseURL(%q) = %q, want empty", value, got)
		}
	}
}

func TestDefaultTokenType(t *testing.T) {
	t.Parallel()

	if got := defaultTokenType(""); got != "Bearer" {
		t.Fatalf("defaultTokenType empty = %q, want Bearer", got)
	}
	if got := defaultTokenType("   "); got != "Bearer" {
		t.Fatalf("defaultTokenType blank = %q, want Bearer", got)
	}
	if got := defaultTokenType(" bearer "); got != "bearer" {
		t.Fatalf("defaultTokenType custom = %q, want bearer", got)
	}
	if got := defaultTokenType("  MAC "); got != "MAC" {
		t.Fatalf("defaultTokenType trimmed = %q, want MAC", got)
	}
}

func TestOAuthTokenExpiryHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	if got := codexTokenExpiry(now, 3600); !got.Equal(now.Add(time.Hour)) {
		t.Fatalf("codexTokenExpiry = %s, want %s", got, now.Add(time.Hour))
	}
	if got := antigravityTokenExpiry(now, 1800); !got.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("antigravityTokenExpiry = %s, want %s", got, now.Add(30*time.Minute))
	}
	if got := codexTokenExpiry(now, 0); !got.IsZero() {
		t.Fatalf("codexTokenExpiry zero = %s, want zero", got)
	}
	if got := antigravityTokenExpiry(now, -1); !got.IsZero() {
		t.Fatalf("antigravityTokenExpiry negative = %s, want zero", got)
	}
}

func TestCodexAuthorizeLinkIncludesPKCEChallengeAndState(t *testing.T) {
	t.Parallel()

	rawURL := codexAuthorizeLink("state-123", "verifier-123", "http://127.0.0.1:1455/auth/callback")
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse codex authorize link: %v", err)
	}
	query := parsed.Query()
	if query.Get("response_type") != "code" || query.Get("client_id") == "" {
		t.Fatalf("unexpected authorize query: %s", rawURL)
	}
	if query.Get("state") != "state-123" {
		t.Fatalf("state = %q, want state-123", query.Get("state"))
	}
	if query.Get("redirect_uri") != "http://127.0.0.1:1455/auth/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("code_challenge") != codexChallenge("verifier-123") || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected PKCE query: %s", rawURL)
	}
	if query.Get("prompt") != "login" || query.Get("codex_cli_simplified_flow") != "true" {
		t.Fatalf("expected Codex-specific authorize flags, got %s", rawURL)
	}
}

func TestAntigravityAuthorizeLinkIncludesOfflineConsent(t *testing.T) {
	t.Parallel()

	rawURL := antigravityAuthorizeLink("state-abc", "http://localhost/callback", antigravityOAuthClient{
		ClientID: "client-id",
	})
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse antigravity authorize link: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "client-id" || query.Get("state") != "state-abc" {
		t.Fatalf("unexpected antigravity authorize query: %s", rawURL)
	}
	if query.Get("access_type") != "offline" || query.Get("prompt") != "consent" || query.Get("include_granted_scopes") != "true" {
		t.Fatalf("expected offline consent query, got %s", rawURL)
	}
	if query.Get("scope") == "" {
		t.Fatalf("expected scopes in authorize query, got %s", rawURL)
	}
}

func TestExchangeCodexCodeEncodesFormAndDecodesToken(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		assertOAuthFormRequest(t, req, codexTokenURL, "codex exchange", map[string]string{
			"grant_type":    "authorization_code",
			"code":          "auth-code",
			"redirect_uri":  "http://localhost/callback",
			"code_verifier": "verifier",
			"client_id":     codexClientID,
		})
		if req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.Header.Get("Accept") != "application/json" {
			t.Fatalf("unexpected codex exchange headers: %#v", req.Header)
		}
		return oauthHTTPResponse(http.StatusOK, `{"access_token":"access","refresh_token":"refresh","id_token":"id","token_type":"Bearer","scope":"openid","expires_in":3600}`), nil
	})}

	token, err := service.exchangeCodexCode(context.Background(), "auth-code", "http://localhost/callback", "verifier")

	if err != nil {
		t.Fatalf("exchangeCodexCode: %v", err)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" || token.IDToken != "id" || token.ExpiresIn != 3600 {
		t.Fatalf("unexpected codex token response: %#v", token)
	}
}

func TestRefreshCodexTokensSurfacesNon2xxBody(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	client := &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		assertOAuthFormRequest(t, req, codexTokenURL, "codex refresh", map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": "refresh-token",
			"client_id":     codexClientID,
		})
		return oauthHTTPResponse(http.StatusUnauthorized, ` invalid_grant `), nil
	})}

	_, err := service.refreshCodexTokens(context.Background(), "refresh-token", client)

	if err == nil || !strings.Contains(err.Error(), "codex token refresh returned 401: invalid_grant") {
		t.Fatalf("refreshCodexTokens error = %v", err)
	}
}

func TestOAuthHTTPHelpersSurfaceDecodeAndTransportErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		transport oauthRoundTripFunc
		wantParts []string
	}{
		{
			name: "codex_refresh_bad_json",
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
					t.Fatalf("unexpected codex refresh request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusOK, `{not-json`), nil
			},
			wantParts: []string{"decode codex token refresh"},
		},
		{
			name: "codex_refresh_transport_error",
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
					t.Fatalf("unexpected codex refresh request: %s %s", req.Method, req.URL.String())
				}
				return nil, errors.New("codex refresh transport failed")
			},
			wantParts: []string{"refresh codex token", "codex refresh transport failed"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(nil, "master-key")
			client := &http.Client{Transport: tc.transport}

			_, err := service.refreshCodexTokens(context.Background(), "refresh-token", client)

			assertOAuthHelperErrorContains(t, "refreshCodexTokens", err, tc.wantParts...)
		})
	}
}

func TestExchangeAntigravityCodeEncodesFormAndRequiresRefreshToken(t *testing.T) {
	t.Parallel()

	clientConfig := antigravityOAuthClient{ClientID: "client-id", ClientSecret: "client-secret"}
	for _, tc := range []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "success", body: `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","scope":"scope","expires_in":1800}`},
		{name: "missing_refresh", body: `{"access_token":"access","token_type":"Bearer"}`, wantErr: "google did not return refresh_token"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(nil, "master-key")
			service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("User-Agent") != antigravityUserAgent || req.Header.Get("Accept") != "application/json" {
					t.Fatalf("unexpected antigravity exchange headers: %#v", req.Header)
				}
				assertOAuthFormRequest(t, req, antigravityTokenURL, "antigravity exchange", map[string]string{
					"grant_type":    "authorization_code",
					"code":          "auth-code",
					"redirect_uri":  "http://localhost/callback",
					"client_id":     "client-id",
					"client_secret": "client-secret",
				})
				return oauthHTTPResponse(http.StatusOK, tc.body), nil
			})}

			token, err := service.exchangeAntigravityCode(context.Background(), "auth-code", "http://localhost/callback", clientConfig)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("exchangeAntigravityCode error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("exchangeAntigravityCode: %v", err)
			}
			if token.AccessToken != "access" || token.RefreshToken != "refresh" || token.ExpiresIn != 1800 {
				t.Fatalf("unexpected antigravity token response: %#v", token)
			}
		})
	}
}

func TestRefreshAntigravityTokensAndFetchUserInfo(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	clientConfig := antigravityOAuthClient{ClientID: "client-id", ClientSecret: "client-secret"}
	client := &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case antigravityTokenURL:
			assertOAuthFormRequest(t, req, antigravityTokenURL, "antigravity refresh", map[string]string{
				"grant_type":    "refresh_token",
				"refresh_token": "refresh-token",
				"client_id":     "client-id",
				"client_secret": "client-secret",
			})
			if req.Header.Get("User-Agent") != antigravityUserAgent {
				t.Fatalf("unexpected antigravity refresh user agent: %q", req.Header.Get("User-Agent"))
			}
			return oauthHTTPResponse(http.StatusOK, `{"access_token":"access-2","token_type":"Bearer","scope":"scope","expires_in":900}`), nil
		case antigravityUserInfoURL:
			if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer access-2" || req.Header.Get("User-Agent") != antigravityUserAgent {
				t.Fatalf("unexpected antigravity userinfo request: %s %#v", req.Method, req.Header)
			}
			return oauthHTTPResponse(http.StatusOK, `{"id":"user-123","email":"user@example.com","verified_email":true,"name":"User Example"}`), nil
		default:
			t.Fatalf("unexpected OAuth URL: %s", req.URL.String())
			return nil, nil
		}
	})}

	token, err := service.refreshAntigravityTokens(context.Background(), "refresh-token", clientConfig, client)
	if err != nil {
		t.Fatalf("refreshAntigravityTokens: %v", err)
	}
	if token.AccessToken != "access-2" || token.ExpiresIn != 900 {
		t.Fatalf("unexpected refreshed antigravity token: %#v", token)
	}

	info, raw, err := service.fetchAntigravityUserInfo(context.Background(), token.AccessToken, client)
	if err != nil {
		t.Fatalf("fetchAntigravityUserInfo: %v", err)
	}
	if info.ID != "user-123" || info.Email != "user@example.com" || !info.VerifiedEmail || raw["email"] != "user@example.com" {
		t.Fatalf("unexpected antigravity user info: info=%#v raw=%#v", info, raw)
	}
}

func TestAntigravityHTTPHelpersSurfaceRefreshAndUserInfoErrors(t *testing.T) {
	t.Parallel()

	clientConfig := antigravityOAuthClient{ClientID: "client-id", ClientSecret: "client-secret"}
	for _, tc := range []struct {
		name      string
		call      func(*Service, *http.Client) error
		transport oauthRoundTripFunc
		wantParts []string
	}{
		{
			name: "refresh_bad_json",
			call: func(service *Service, client *http.Client) error {
				_, err := service.refreshAntigravityTokens(context.Background(), "refresh-token", clientConfig, client)
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
					t.Fatalf("unexpected antigravity refresh request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusOK, `{not-json`), nil
			},
			wantParts: []string{"decode antigravity token refresh"},
		},
		{
			name: "refresh_transport_error",
			call: func(service *Service, client *http.Client) error {
				_, err := service.refreshAntigravityTokens(context.Background(), "refresh-token", clientConfig, client)
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
					t.Fatalf("unexpected antigravity refresh request: %s %s", req.Method, req.URL.String())
				}
				return nil, errors.New("antigravity refresh transport failed")
			},
			wantParts: []string{"refresh antigravity token", "antigravity refresh transport failed"},
		},
		{
			name: "refresh_non_2xx",
			call: func(service *Service, client *http.Client) error {
				_, err := service.refreshAntigravityTokens(context.Background(), "refresh-token", clientConfig, client)
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
					t.Fatalf("unexpected antigravity refresh request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusTooManyRequests, ` slow down `), nil
			},
			wantParts: []string{"antigravity token refresh returned 429: slow down"},
		},
		{
			name: "userinfo_bad_json",
			call: func(service *Service, client *http.Client) error {
				_, _, err := service.fetchAntigravityUserInfo(context.Background(), "access-token", client)
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.String() != antigravityUserInfoURL {
					t.Fatalf("unexpected antigravity userinfo request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusOK, `{not-json`), nil
			},
			wantParts: []string{"decode antigravity userinfo"},
		},
		{
			name: "userinfo_transport_error",
			call: func(service *Service, client *http.Client) error {
				_, _, err := service.fetchAntigravityUserInfo(context.Background(), "access-token", client)
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.String() != antigravityUserInfoURL {
					t.Fatalf("unexpected antigravity userinfo request: %s %s", req.Method, req.URL.String())
				}
				return nil, errors.New("antigravity userinfo transport failed")
			},
			wantParts: []string{"fetch antigravity userinfo", "antigravity userinfo transport failed"},
		},
		{
			name: "userinfo_non_2xx",
			call: func(service *Service, client *http.Client) error {
				_, _, err := service.fetchAntigravityUserInfo(context.Background(), "access-token", client)
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.String() != antigravityUserInfoURL {
					t.Fatalf("unexpected antigravity userinfo request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusForbidden, ` forbidden `), nil
			},
			wantParts: []string{"antigravity userinfo returned 403: forbidden"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(nil, "master-key")
			client := &http.Client{Transport: tc.transport}

			err := tc.call(service, client)

			assertOAuthHelperErrorContains(t, "antigravity HTTP helper", err, tc.wantParts...)
		})
	}
}

func TestAntigravityOAuthClientFromEnvUsesExplicitKeyAndEnvSecrets(t *testing.T) {
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_KEY", "env-key")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "env-client")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "env-secret")

	client := antigravityOAuthClientFromEnv(" explicit-key ")
	if client.Key != "explicit-key" || client.ClientID != "env-client" || client.ClientSecret != "env-secret" {
		t.Fatalf("unexpected explicit client: %#v", client)
	}

	client = antigravityOAuthClientFromEnv(" ")
	if client.Key != "env-key" || client.ClientID != "env-client" || client.ClientSecret != "env-secret" {
		t.Fatalf("unexpected env client: %#v", client)
	}
}

func TestAntigravityOAuthClientFromEnvFallsBackToBundledDefaults(t *testing.T) {
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_KEY", "")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_ID", "")
	t.Setenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET", "")

	client := antigravityOAuthClientFromEnv("")
	if client.Key != antigravityDefaultClientKey {
		t.Fatalf("key = %q, want %q", client.Key, antigravityDefaultClientKey)
	}
	if client.ClientID != antigravityDefaultClientID {
		t.Fatalf("client id = %q, want bundled default", client.ClientID)
	}
	if client.ClientSecret != antigravityDefaultClientSecret {
		t.Fatalf("client secret = %q, want bundled default", client.ClientSecret)
	}
}

func TestGeneratedPKCEVerifierAndOAuthStateAreURLSafe(t *testing.T) {
	t.Parallel()

	verifier, err := newPKCEVerifier()
	if err != nil {
		t.Fatalf("newPKCEVerifier: %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(verifier)
	if err != nil {
		t.Fatalf("verifier should be raw URL base64: %v", err)
	}
	if len(decoded) != 32 {
		t.Fatalf("verifier decoded length = %d, want 32", len(decoded))
	}

	state, err := newOAuthState()
	if err != nil {
		t.Fatalf("newOAuthState: %v", err)
	}
	decoded, err = base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		t.Fatalf("state should be raw URL base64: %v", err)
	}
	if len(decoded) != 24 {
		t.Fatalf("state decoded length = %d, want 24", len(decoded))
	}
}

func TestParseCodexIDTokenExtractsClaimsAndRawPayload(t *testing.T) {
	t.Parallel()

	payload := base64.RawURLEncoding.EncodeToString([]byte(`{
		"email":"user@example.com",
		"email_verified":true,
		"exp":1770000000,
		"https://api.openai.com/auth":{
			"chatgpt_account_id":"acct_123",
			"chatgpt_plan_type":"plus",
			"chatgpt_user_id":"user_123"
		}
	}`))
	claims, raw, err := parseCodexIDToken("header." + payload + ".signature")
	if err != nil {
		t.Fatalf("parseCodexIDToken: %v", err)
	}
	if claims.Email != "user@example.com" || !claims.EmailVerified || claims.Exp != 1770000000 {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if claims.AuthInfo.ChatGPTAccountID != "acct_123" || claims.AuthInfo.ChatGPTPlanType != "plus" {
		t.Fatalf("unexpected auth claims: %#v", claims.AuthInfo)
	}
	if raw["email"] != "user@example.com" {
		t.Fatalf("unexpected raw payload: %#v", raw)
	}

	if _, _, err := parseCodexIDToken("not-a-jwt"); err == nil {
		t.Fatal("expected invalid token format to fail")
	}

	if _, _, err := parseCodexIDToken("header.not_base64!.signature"); err == nil || !strings.Contains(err.Error(), "decode id_token payload") {
		t.Fatalf("bad base64 error = %v, want decode payload error", err)
	}

	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	if _, _, err := parseCodexIDToken("header." + notJSON + ".signature"); err == nil || !strings.Contains(err.Error(), "unmarshal id_token payload") {
		t.Fatalf("bad JSON error = %v, want unmarshal payload error", err)
	}
}

func TestUpdateMetadataErrorPreservesExistingMetadata(t *testing.T) {
	t.Parallel()

	updated := updateMetadataError(store.JSON(`{"token_mode":"oauth_refresh"}`), "invalid_grant")
	var values map[string]any
	if err := json.Unmarshal(updated, &values); err != nil {
		t.Fatalf("updated metadata should be JSON: %v", err)
	}
	if values["token_mode"] != "oauth_refresh" {
		t.Fatalf("expected existing metadata to be preserved, got %#v", values)
	}
	if values["last_error"] != "invalid_grant" {
		t.Fatalf("last_error = %#v, want invalid_grant", values["last_error"])
	}
	if _, err := time.Parse(time.RFC3339, values["last_error_at"].(string)); err != nil {
		t.Fatalf("last_error_at should be RFC3339: %v", err)
	}
}

func TestCodexConnectionDetailsDecryptsTokensAndNormalizesMetadata(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	accessToken, _, err := service.credentials.Encrypt("access-token")
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}
	refreshToken, _, err := service.credentials.Encrypt("refresh-token")
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}
	idToken, _, err := service.credentials.Encrypt("id-token")
	if err != nil {
		t.Fatalf("encrypt id token: %v", err)
	}
	siteModelID := uuid.NewString()

	details, err := service.codexConnectionDetails(store.OAuthConnection{
		ID:                    uuid.New(),
		Provider:              "codex",
		AccountID:             "acct_123",
		Email:                 "user@example.com",
		EncryptedAccessToken:  accessToken,
		EncryptedRefreshToken: refreshToken,
		EncryptedIDToken:      idToken,
		RawProfile:            store.JSON(`{"sub":"user_123","email":"user@example.com"}`),
		Metadata: store.JSON(jsonBytes(map[string]any{
			"plan_type": " plus ",
			"quota":     map[string]any{"available": true},
			"models": []any{
				map[string]any{
					"id":                  siteModelID,
					"upstream_model_name": "gpt-5-codex",
					"display":             "GPT-5 Codex",
					"status":              "active",
				},
			},
		})),
	})
	if err != nil {
		t.Fatalf("codexConnectionDetails: %v", err)
	}

	if details.AccessToken != "access-token" || details.RefreshToken != "refresh-token" || details.IDToken != "id-token" {
		t.Fatalf("unexpected decrypted tokens: %#v", details)
	}
	if details.AccountID != "acct_123" || details.Email != "user@example.com" || details.Claims["sub"] != "user_123" {
		t.Fatalf("unexpected identity details: %#v", details)
	}
	if details.PlanType != "plus" || details.Quota["available"] != true {
		t.Fatalf("unexpected plan/quota details: %#v", details)
	}
	if len(details.Models) != 1 {
		t.Fatalf("expected one normalized model, got %#v", details.Models)
	}
	model := details.Models[0]
	if model["site_model_id"] != siteModelID || model["id"] != "gpt-5-codex" || model["display_name"] != "GPT-5 Codex" || model["enabled"] != true {
		t.Fatalf("unexpected normalized model detail: %#v", model)
	}
}

func TestCodexConnectionDetailsReturnsDecryptError(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	_, err := service.codexConnectionDetails(store.OAuthConnection{
		EncryptedAccessToken: "not-encrypted",
	})
	if err == nil {
		t.Fatal("expected invalid encrypted access token to fail")
	}
}

func TestIsPermanentAuthError(t *testing.T) {
	t.Parallel()

	permanent := []string{
		"codex token refresh returned 401: token_invalidated",
		"upstream returned 403 forbidden",
		"refresh_token_reused",
		"invalid_grant",
	}
	for _, value := range permanent {
		if !isPermanentAuthError(value) {
			t.Fatalf("expected permanent auth error for %q", value)
		}
	}

	for _, value := range []string{"context deadline exceeded", "upstream returned 429", "temporary network failure"} {
		if isPermanentAuthError(value) {
			t.Fatalf("expected transient error for %q", value)
		}
	}
}

func TestCallbackRelayEnsureRejectsMissingStoreWithoutStartingListener(t *testing.T) {
	t.Parallel()

	if err := ensureCodexCallbackRelay(nil); err == nil || !strings.Contains(err.Error(), "oauth store is not available") {
		t.Fatalf("ensureCodexCallbackRelay(nil) error = %v", err)
	}
	if err := ensureAntigravityCallbackRelay(nil); err == nil || !strings.Contains(err.Error(), "oauth store is not available") {
		t.Fatalf("ensureAntigravityCallbackRelay(nil) error = %v", err)
	}
}

func TestCallbackRelaySuccessHandlersWriteHTML(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		want    string
	}{
		{name: "codex", handler: (&codexCallbackRelay{}).handleSuccess, want: "Codex login complete"},
		{name: "antigravity", handler: (&antigravityCallbackRelay{}).handleSuccess, want: "Antigravity login complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/success", nil)
			rec := httptest.NewRecorder()
			tc.handler(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if contentType := rec.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
				t.Fatalf("content-type = %q", contentType)
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body %q does not contain %q", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestRelayTargetURLAndWithRelayQuery(t *testing.T) {
	t.Parallel()

	raw := store.JSON(`{"relay_target_url":" https://xlyra.example.com/api/v1/oauth/providers/codex/callback?existing=1 "}`)
	if got := relayTargetURL(raw); got != "https://xlyra.example.com/api/v1/oauth/providers/codex/callback?existing=1" {
		t.Fatalf("relayTargetURL = %q", got)
	}

	values := url.Values{}
	values.Set("code", "auth-code")
	values.Set("state", "oauth-state")
	merged, err := withRelayQuery("https://xlyra.example.com/callback?existing=1", values)
	if err != nil {
		t.Fatalf("withRelayQuery: %v", err)
	}
	parsed, err := url.Parse(merged)
	if err != nil {
		t.Fatalf("parse merged relay URL: %v", err)
	}
	if parsed.Query().Get("existing") != "1" || parsed.Query().Get("code") != "auth-code" || parsed.Query().Get("state") != "oauth-state" {
		t.Fatalf("unexpected merged query: %s", merged)
	}
	if _, err := withRelayQuery("/relative", values); err == nil {
		t.Fatal("expected relative relay target to fail")
	}
}

func TestJSONBytesFallsBackToEmptyObject(t *testing.T) {
	t.Parallel()

	if got := string(jsonBytes(nil)); got != `{}` {
		t.Fatalf("jsonBytes(nil) = %s, want {}", got)
	}
	if got := string(jsonBytes(make(chan int))); got != `{}` {
		t.Fatalf("jsonBytes(unmarshalable) = %s, want {}", got)
	}
}

func TestMapsAndStringHelpers(t *testing.T) {
	t.Parallel()

	items := mapsFromAny([]any{
		map[string]any{"id": "one"},
		"ignored",
		map[string]any{"id": "two"},
	})
	if len(items) != 2 || items[0]["id"] != "one" || items[1]["id"] != "two" {
		t.Fatalf("mapsFromAny = %#v", items)
	}
	if mapsFromAny([]map[string]any{{"id": "typed"}}) != nil {
		t.Fatal("mapsFromAny should only accept []any input")
	}

	if !looksLikeUUID(uuid.NewString()) {
		t.Fatal("expected UUID string to be recognized")
	}
	if looksLikeUUID("not-a-uuid") {
		t.Fatal("expected non-UUID string to be rejected")
	}
	if got := stringFromAny(" value "); got != "value" {
		t.Fatalf("stringFromAny = %q, want value", got)
	}
	if got := stringFromAny(123); got != "" {
		t.Fatalf("non-string stringFromAny = %q, want empty", got)
	}
	if got := firstNonEmptyString(" ", " first ", "second"); got != "first" {
		t.Fatalf("firstNonEmptyString = %q, want first", got)
	}
}

func TestNormalizeCodexModelSnapshotsKeepsStableModelIdentity(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New().String()
	items := normalizeCodexModelSnapshots([]map[string]any{
		{
			"id":                  siteModelID,
			"upstream_model_name": "gpt-5-codex",
			"display":             "GPT-5 Codex",
			"status":              "active",
		},
		{
			"name":   "gpt-5-mini",
			"status": "disabled",
		},
	})

	if len(items) != 2 {
		t.Fatalf("expected two model snapshots, got %#v", items)
	}
	if items[0]["site_model_id"] != siteModelID || items[0]["id"] != "gpt-5-codex" {
		t.Fatalf("expected UUID id to move to site_model_id, got %#v", items[0])
	}
	if items[0]["display_name"] != "GPT-5 Codex" || items[0]["enabled"] != true {
		t.Fatalf("unexpected normalized active model: %#v", items[0])
	}
	if items[1]["id"] != "gpt-5-mini" || items[1]["enabled"] != false {
		t.Fatalf("unexpected normalized disabled model: %#v", items[1])
	}
}

type oauthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f oauthRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func assertOAuthFormRequest(t *testing.T, req *http.Request, wantURL string, label string, wantForm map[string]string) url.Values {
	t.Helper()

	if req.Method != http.MethodPost || req.URL.String() != wantURL {
		t.Fatalf("unexpected %s request: %s %s", label, req.Method, req.URL.String())
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read %s body: %v", label, err)
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse %s body: %v", label, err)
	}
	for key, want := range wantForm {
		if got := form.Get(key); got != want {
			t.Fatalf("%s form %q = %q, want %q in %s", label, key, got, want, form.Encode())
		}
	}
	return form
}

func assertOAuthHelperErrorContains(t *testing.T, label string, err error, wantParts ...string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected %s to fail", label)
	}
	for _, want := range wantParts {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%s error = %v, want to contain %q", label, err, want)
		}
	}
}

func assertOAuthRelayResponse(t *testing.T, handler http.HandlerFunc, target string, status int, wantBody string) {
	t.Helper()

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != status || !strings.Contains(rec.Body.String(), wantBody) {
		t.Fatalf("response = %d %q, want status %d containing %q", rec.Code, rec.Body.String(), status, wantBody)
	}
}

func assertOAuthMissingFields(t *testing.T, missing []string, want ...string) {
	t.Helper()

	if len(missing) != len(want) {
		t.Fatalf("missing fields = %v, want %v", missing, want)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Fatalf("missing[%d] = %q, want %q in %v", i, missing[i], want[i], missing)
		}
	}
}

func oauthHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestClaudeCodeQuotaForDetailsReadaptsStoredSummaryFromRaw(t *testing.T) {
	t.Parallel()

	stored := map[string]any{
		"type":      "claude_code",
		"available": true,
		"five_hour": map[string]any{"name": "5h", "used_percent": 10.0},
		"weekly":    map[string]any{"name": "weekly", "used_percent": 20.0},
		"raw": map[string]any{
			"five_hour":       map[string]any{"utilization": 12.0, "resets_at": "2026-07-26T12:00:00Z"},
			"seven_day":       map[string]any{"utilization": 30.0, "resets_at": "2026-07-30T00:00:00Z"},
			"seven_day_fable": map[string]any{"utilization": 55.0, "resets_at": "2026-07-30T00:00:00Z"},
		},
	}

	got := claudeCodeQuotaForDetails(stored)

	if got["available"] != true {
		t.Fatalf("extra keys should survive re-adaptation: %#v", got)
	}
	weekly, _ := got["weekly"].(map[string]any)
	if weekly == nil || weekly["used_percent"] != 30.0 {
		t.Fatalf("weekly should be rebuilt from raw: %#v", got["weekly"])
	}
	models, _ := got["models"].([]map[string]any)
	if len(models) != 1 || models[0]["display_name"] != "Fable" || models[0]["used_percent"] != 55.0 {
		t.Fatalf("stale summary should gain scoped model windows: %#v", got["models"])
	}
}
