package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCodexValidateCredentialsFetchesUsageAndModelsWithSiteAccount(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		assertCodexHTTPHeaders(t, r, "codex-token", "acct-site")

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plan_type": "plus",
				"rate_limit": map[string]any{
					"allowed":       true,
					"limit_reached": false,
				},
			})
		case "/backend-api/codex/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id":           "gpt-validate",
					"display_name": "GPT Validate",
				}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	err := NewCodex().ValidateCredentials(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
		Meta: map[string]any{
			"oauth_account_id": "acct-site",
		},
	}, " codex-token ")
	if err != nil {
		t.Fatalf("ValidateCredentials returned error: %v", err)
	}

	wantPaths := []string{"/backend-api/wham/usage", "/backend-api/codex/models"}
	if !sameStringSlice(requestedPaths, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requestedPaths, wantPaths)
	}
}

func TestCodexValidateSystemCredentialsStopsBeforeModelsOnUsageAuthError(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		assertCodexHTTPHeaders(t, r, "system-token", "acct-system")
		if strings.Contains(r.URL.Path, "models") {
			t.Fatalf("models endpoint should not be requested after usage auth failure: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
	}))
	defer server.Close()

	err := NewCodex().ValidateSystemCredentials(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, SystemAuth{
		AccessToken: " system-token ",
		AccountID:   " acct-system ",
	})
	if err == nil {
		t.Fatal("ValidateSystemCredentials returned nil error, want auth failure")
	}
	if !strings.Contains(err.Error(), "codex upstream returned 401") {
		t.Fatalf("ValidateSystemCredentials error = %v, want upstream 401", err)
	}
	if len(requestedPaths) != 4 {
		t.Fatalf("requested paths = %#v, want all usage fallbacks only", requestedPaths)
	}
}

func TestCodexListModelsWithAuthFallsBackToStaticModels(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		assertCodexHTTPHeaders(t, r, "auth-token", "acct-auth")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []map[string]any{}})
	}))
	defer server.Close()

	models, err := NewCodex().ListModelsWithAuth(context.Background(), SiteConfig{
		BaseURL: server.URL + "/codex",
	}, SystemAuth{
		AccessToken: " auth-token ",
		AccountID:   "acct-auth",
		Metadata: map[string]any{
			"plan_type": "plus",
		},
	})
	if err != nil {
		t.Fatalf("ListModelsWithAuth returned error: %v", err)
	}
	if len(requestedPaths) != 4 {
		t.Fatalf("requested paths = %#v, want all distinct model endpoints", requestedPaths)
	}
	if codexModelByID(models, "gpt-5.6-sol") == nil {
		t.Fatalf("fallback models missing gpt-5.6-sol: %#v", models)
	}
	if codexModelByID(models, "gpt-5.5") == nil {
		t.Fatalf("fallback models missing gpt-5.5: %#v", models)
	}
	if codexModelByID(models, "gpt-5.3-codex-spark") != nil {
		t.Fatalf("fallback catalog should not include plan-gated spark: %#v", models)
	}
}

func TestCodexListAPIKeysBuildsSyntheticOAuthKey(t *testing.T) {
	keys, err := NewCodex().ListAPIKeys(context.Background(), SiteConfig{}, SystemAuth{
		AccessToken: "oauth-token",
		AccountID:   "acct-keys",
		Email:       " user@example.com ",
		Metadata: map[string]any{
			"plan_type": "team",
		},
	})
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys length = %d, want 1", len(keys))
	}
	key := keys[0]
	if key.ID != 1 || key.Name != "user@example.com" || key.Status != "connected" || key.Key != "oauth-token" {
		t.Fatalf("unexpected key fields: %#v", key)
	}
	if key.Raw["provider"] != "codex" || key.Raw["account_id"] != "acct-keys" ||
		key.Raw["email"] != " user@example.com " || key.Raw["plan_type"] != "team" {
		t.Fatalf("unexpected raw key payload: %#v", key.Raw)
	}
}

func TestCodexFetchUserSummaryIncludesQuotaModelsAndPricingMetadata(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		assertCodexHTTPHeaders(t, r, "summary-token", "acct-summary")

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plan_type": " plus ",
				"rate_limit": map[string]any{
					"allowed": true,
					"primary_window": map[string]any{
						"used_percent":         float64(10),
						"limit_window_seconds": float64(18000),
					},
				},
				"rate_limit_reset_credits": map[string]any{
					"available_count": float64(3),
				},
			})
		case "/backend-api/codex/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id":                       "gpt-summary",
					"display_name":             "GPT Summary",
					"supported_endpoint_types": []string{"openai-response"},
				}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	summary, err := NewCodex().FetchUserSummary(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, SystemAuth{
		AccessToken: " summary-token ",
		AccountID:   "acct-summary",
		Email:       "summary@example.com",
		Metadata: map[string]any{
			"plan_type":                               "free",
			"chatgpt_subscription_active_start":       "2026-06-01T00:00:00Z",
			"chatgpt_subscription_active_until":       "2026-07-01T00:00:00Z",
			"ignored_non_subscription_metadata_field": "ignored",
		},
	})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}
	wantPaths := []string{"/backend-api/wham/usage", "/backend-api/codex/models"}
	if !sameStringSlice(requestedPaths, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requestedPaths, wantPaths)
	}

	user, ok := summary.User.(map[string]any)
	if !ok {
		t.Fatalf("summary user = %#v, want map", summary.User)
	}
	if user["provider"] != "codex" || user["email"] != "summary@example.com" ||
		user["account_id"] != "acct-summary" || user["plan_type"] != "plus" {
		t.Fatalf("unexpected summary user: %#v", user)
	}
	if user["chatgpt_subscription_active_start"] != "2026-06-01T00:00:00Z" ||
		user["chatgpt_subscription_active_until"] != "2026-07-01T00:00:00Z" {
		t.Fatalf("subscription metadata missing from user: %#v", user)
	}
	quota, _ := user["quota"].(map[string]any)
	resetCredits, _ := quota["reset_credits"].(map[string]any)
	if resetCredits["available_count"] != 3 {
		t.Fatalf("summary reset credits = %#v, want available_count 3", quota["reset_credits"])
	}

	apiKeys, ok := summary.APIKeys.(map[string]any)
	if !ok || apiKeys["count"] != 1 || apiKeys["mode"] != "oauth_bearer" {
		t.Fatalf("summary api keys = %#v, want oauth bearer count", summary.APIKeys)
	}
	userModels, ok := summary.UserModels.(map[string]any)
	if !ok {
		t.Fatalf("summary models = %#v, want map", summary.UserModels)
	}
	modelItems, ok := userModels["data"].([]map[string]any)
	if !ok || len(modelItems) != 1 || modelItems[0]["id"] != "gpt-summary" {
		t.Fatalf("summary model data = %#v, want raw remote model", userModels["data"])
	}
	pricing, ok := summary.Pricing.(map[string]any)
	if !ok || pricing["plan_type"] != "plus" {
		t.Fatalf("summary pricing = %#v, want plus pricing payload", summary.Pricing)
	}
}

func TestCodexFetchUserSummaryReturnsAuthErrorWithoutModelFallback(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		assertCodexHTTPHeaders(t, r, "expired-token", "acct-expired")
		if strings.Contains(r.URL.Path, "models") {
			t.Fatalf("models endpoint should not be requested after oauth auth failure: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"token_invalidated"}`, http.StatusForbidden)
	}))
	defer server.Close()

	_, err := NewCodex().FetchUserSummary(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, SystemAuth{
		AccessToken: "expired-token",
		AccountID:   "acct-expired",
		Email:       "expired@example.com",
	})
	if err == nil {
		t.Fatal("FetchUserSummary returned nil error, want oauth auth failure")
	}
	if !strings.Contains(err.Error(), "codex upstream returned 403") {
		t.Fatalf("FetchUserSummary error = %v, want upstream 403", err)
	}
	if len(requestedPaths) != 4 {
		t.Fatalf("requested paths = %#v, want all usage fallbacks only", requestedPaths)
	}
}

func TestCodexFetchBalanceAndMetadataSnapshots(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		assertCodexHTTPHeaders(t, r, "snapshot-token", "acct-snapshot")

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plan_type": "team",
				"credits": map[string]any{
					"remaining": float64(42),
				},
				"rate_limits": map[string]any{
					"allowed":       true,
					"limit_reached": false,
				},
			})
		case "/backend-api/codex/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{
					"id":   "gpt-metadata",
					"name": "GPT Metadata",
				}},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	auth := SystemAuth{
		AccessToken: "snapshot-token",
		AccountID:   "acct-snapshot",
		Metadata: map[string]any{
			"plan_type": "plus",
		},
	}
	site := SiteConfig{BaseURL: server.URL + "/backend-api"}

	balance, err := NewCodex().FetchBalance(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchBalance returned error: %v", err)
	}
	balanceRaw, ok := balance.Raw.(map[string]any)
	if !ok || balanceRaw["plan_type"] != "team" || balanceRaw["available"] != true {
		t.Fatalf("balance raw = %#v, want normalized usage", balance.Raw)
	}

	metadata, err := NewCodex().FetchMetadata(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchMetadata returned error: %v", err)
	}
	metadataRaw, ok := metadata.Raw.(map[string]any)
	if !ok {
		t.Fatalf("metadata raw = %#v, want map", metadata.Raw)
	}
	quota, ok := metadataRaw["quota"].(map[string]any)
	if !ok || quota["plan_type"] != "team" {
		t.Fatalf("metadata quota = %#v, want normalized usage", metadataRaw["quota"])
	}
	models, ok := metadataRaw["models"].([]Model)
	if !ok || len(models) != 1 || models[0].UpstreamName != "gpt-metadata" || models[0].DisplayName != "GPT Metadata" {
		t.Fatalf("metadata models = %#v, want remote model", metadataRaw["models"])
	}

	wantPaths := []string{
		"/backend-api/wham/usage",
		"/backend-api/wham/usage",
		"/backend-api/codex/models",
	}
	if !sameStringSlice(requestedPaths, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requestedPaths, wantPaths)
	}
}

func TestCodexFetchPricingAndParsePricingArePlanAgnostic(t *testing.T) {
	pricing, err := NewCodex().FetchPricing(context.Background(), SiteConfig{}, SystemAuth{
		Metadata: map[string]any{
			"plan_type": "free",
		},
	})
	if err != nil {
		t.Fatalf("FetchPricing returned error: %v", err)
	}
	if codexPricingByModel(pricing.Items, "gpt-image-2") == nil {
		t.Fatalf("free pricing missing image model: %#v", pricing.Items)
	}
	if codexPricingByModel(pricing.Items, "gpt-5.5") == nil {
		t.Fatalf("pricing is plan-agnostic and should include gpt-5.5: %#v", pricing.Items)
	}
	if codexPricingByModel(pricing.Items, "gpt-5.6-sol") == nil {
		t.Fatalf("pricing missing gpt-5.6-sol: %#v", pricing.Items)
	}

	parsed := NewCodex().ParsePricing(map[string]any{"plan_type": "team"})
	if codexPricingByModel(parsed.Items, "gpt-5.3-codex-spark") == nil {
		t.Fatalf("parsed pricing missing spark: %#v", parsed.Items)
	}
	if codexPricingByModel(parsed.Items, "gpt-5.5") == nil {
		t.Fatalf("parsed pricing missing gpt-5.5: %#v", parsed.Items)
	}
	raw, ok := parsed.Raw.(map[string]any)
	if !ok || raw["plan_type"] != "team" {
		t.Fatalf("parsed raw = %#v, want team payload", parsed.Raw)
	}

	defaultParsed := NewCodex().ParsePricing("not a map")
	if codexPricingByModel(defaultParsed.Items, "gpt-5.5") == nil {
		t.Fatalf("default parsed pricing missing gpt-5.5: %#v", defaultParsed.Items)
	}
}

func assertCodexHTTPHeaders(t *testing.T, r *http.Request, token string, accountID string) {
	t.Helper()
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want application/json", got)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("Authorization = %q, want Bearer %s", got, token)
	}
	if got := r.Header.Get("Origin"); got != codexOriginURL {
		t.Fatalf("Origin = %q, want %s", got, codexOriginURL)
	}
	if got := r.Header.Get("Referer"); got != codexRefererURL {
		t.Fatalf("Referer = %q, want %s", got, codexRefererURL)
	}
	if got := r.Header.Get("User-Agent"); got != CodexUserAgent {
		t.Fatalf("User-Agent = %q, want %s", got, CodexUserAgent)
	}
	if got := r.Header.Get("ChatGPT-Account-Id"); got != accountID {
		t.Fatalf("ChatGPT-Account-Id = %q, want %s", got, accountID)
	}
}

func sameStringSlice(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
