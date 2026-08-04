package oauth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestOAuthHTTPHelpersCoverExchangeDecodeAndTransportErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		call      func(*Service) error
		transport oauthRoundTripFunc
		wantParts []string
	}{
		{
			name: "codex_exchange_bad_json",
			call: func(service *Service) error {
				_, err := service.exchangeCodexCode(context.Background(), "code", "http://localhost/callback", "verifier")
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
					t.Fatalf("unexpected codex exchange request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusOK, `{not-json`), nil
			},
			wantParts: []string{"decode codex token exchange"},
		},
		{
			name: "codex_exchange_transport_error",
			call: func(service *Service) error {
				_, err := service.exchangeCodexCode(context.Background(), "code", "http://localhost/callback", "verifier")
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
					t.Fatalf("unexpected codex exchange request: %s %s", req.Method, req.URL.String())
				}
				return nil, errors.New("codex exchange transport failed")
			},
			wantParts: []string{"exchange codex code", "codex exchange transport failed"},
		},
		{
			name: "antigravity_exchange_bad_json",
			call: func(service *Service) error {
				_, err := service.exchangeAntigravityCode(context.Background(), "code", "http://localhost/callback", antigravityOAuthClient{
					ClientID:     "client-id",
					ClientSecret: "client-secret",
				})
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
					t.Fatalf("unexpected antigravity exchange request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusOK, `{not-json`), nil
			},
			wantParts: []string{"decode antigravity token exchange"},
		},
		{
			name: "antigravity_exchange_non_2xx",
			call: func(service *Service) error {
				_, err := service.exchangeAntigravityCode(context.Background(), "code", "http://localhost/callback", antigravityOAuthClient{
					ClientID:     "client-id",
					ClientSecret: "client-secret",
				})
				return err
			},
			transport: func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
					t.Fatalf("unexpected antigravity exchange request: %s %s", req.Method, req.URL.String())
				}
				return oauthHTTPResponse(http.StatusBadGateway, ` google down `), nil
			},
			wantParts: []string{"antigravity token exchange returned 502: google down"},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(nil, "master-key")
			service.httpClient = &http.Client{Transport: tc.transport}

			err := tc.call(service)
			assertOAuthHelperErrorContains(t, "OAuth helper", err, tc.wantParts...)
		})
	}
}

func TestImportResultWrappersMarkFailedItemsAndRefreshNilGuard(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil)

	for _, tc := range []struct {
		name string
		run  func() ImportResult
	}{
		{
			name: "cpa_missing_access_token",
			run: func() ImportResult {
				return service.ImportCPAAccounts(context.Background(), CPAExport{
					Type:      "openai",
					AccountID: "acct_123",
					Email:     "agent@example.com",
				}, false)
			},
		},
		{
			name: "chatgpt_token_missing_access_token",
			run: func() ImportResult {
				return service.ImportChatGPTTokenAccounts(context.Background(), ChatGPTTokenExport{
					Tokens: ChatGPTTokenDetails{AccountID: "acct_123"},
				}, false)
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := tc.run()

			assertSingleImportFailure(t, result, codexProvider, "access_token is required")
		})
	}

	if service.refreshImportedConnection(context.Background(), uuid.Nil) {
		t.Fatal("nil connection id should not be refreshed")
	}
}
