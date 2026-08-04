package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXLyraValidateSystemCredentialsSuccessAndEmptyToken(t *testing.T) {
	t.Run("success calls profile with access token", func(t *testing.T) {
		var gotPath string
		var gotToken string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotToken = r.Header.Get("X-Access-Token")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "admin-1"})
		}))
		defer server.Close()

		err := NewXLyra().ValidateSystemCredentials(context.Background(), SiteConfig{BaseURL: server.URL}, SystemAuth{AccessToken: " admin-token "})
		if err != nil {
			t.Fatalf("ValidateSystemCredentials returned error: %v", err)
		}
		if gotPath != "/api/v1/profile" {
			t.Fatalf("profile path = %q, want /api/v1/profile", gotPath)
		}
		if gotToken != "admin-token" {
			t.Fatalf("access token header = %q, want trimmed token", gotToken)
		}
	})

	t.Run("empty token is rejected before request", func(t *testing.T) {
		called := false
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		}))
		defer server.Close()

		err := NewXLyra().ValidateSystemCredentials(context.Background(), SiteConfig{BaseURL: server.URL}, SystemAuth{AccessToken: " \t\n "})
		if err == nil || !strings.Contains(err.Error(), "access token is required") {
			t.Fatalf("ValidateSystemCredentials error = %v, want access token required", err)
		}
		if called {
			t.Fatalf("server was called for empty access token")
		}
	})
}

func TestXLyraListAPIKeysParsesKeysAndNormalizesRaw(t *testing.T) {
	var gotQuery string
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/api-keys" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.RawQuery
		gotToken = r.Header.Get("X-Access-Token")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{
				map[string]any{
					"id":              "real-key-id",
					"name":            "real key",
					"key":             " sk-real ",
					"masked_key":      " sk-real-*** ",
					"status":          "active",
					"quota_available": float64(42),
					"quota_remaining": float64(99),
					"quota_used":      float64(7),
					"quota_unlimited": true,
					"expires_at":      "2030-01-02T03:04:05Z",
				},
				map[string]any{
					"id":         "masked-key-id",
					"name":       "masked key",
					"masked_key": " sk-masked-*** ",
					"status":     "disabled",
				},
				map[string]any{
					"id":         "prefix-key-id",
					"name":       "prefix key",
					"key_prefix": " sk-prefix ",
				},
				map[string]any{
					"id":   "skip-no-key-material",
					"name": "skip",
				},
				"skip-non-object",
			},
		})
	}))
	defer server.Close()

	keys, err := NewXLyra().ListAPIKeys(context.Background(), SiteConfig{BaseURL: server.URL}, SystemAuth{AccessToken: " admin-token "})
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if gotQuery != "view=sync" {
		t.Fatalf("query = %q, want view=sync", gotQuery)
	}
	if gotToken != "admin-token" {
		t.Fatalf("access token header = %q, want trimmed token", gotToken)
	}
	if len(keys) != 3 {
		t.Fatalf("keys len = %d, want 3: %#v", len(keys), keys)
	}

	first := keys[0]
	if first.ExternalID != "real-key-id" || first.Name != "real key" || first.Key != "sk-real" || first.MaskedKey != "sk-real-***" || first.Status != "active" {
		t.Fatalf("unexpected first key: %#v", first)
	}
	if first.Raw["remain_quota"] != float64(42) {
		t.Fatalf("remain_quota = %#v, want quota_available", first.Raw["remain_quota"])
	}
	if first.Raw["used_quota"] != float64(7) || first.Raw["unlimited_quota"] != true || first.Raw["expired_time"] != "2030-01-02T03:04:05Z" {
		t.Fatalf("unexpected normalized raw: %#v", first.Raw)
	}

	if keys[1].ExternalID != "masked-key-id" || keys[1].Key != "" || keys[1].MaskedKey != "sk-masked-***" {
		t.Fatalf("unexpected masked-only key: %#v", keys[1])
	}
	if keys[2].ExternalID != "prefix-key-id" || keys[2].Key != "" || keys[2].MaskedKey != "sk-prefix" {
		t.Fatalf("unexpected prefix-only key: %#v", keys[2])
	}
}

func TestXLyraSummarizeAPIKeyAndFetchUserSummarySuccess(t *testing.T) {
	var gotModelAuth string
	var gotProfileToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			gotModelAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-xlyra-a", "object": "model", "owned_by": "xlyra"},
					{"id": "gpt-xlyra-b", "object": "model", "metadata": map[string]any{"tier": "fast"}},
					{"id": "", "object": "model"},
				},
			})
		case "/api/v1/profile":
			gotProfileToken = r.Header.Get("X-Access-Token")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "user-1",
				"email": "admin@example.test",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	xlyra := NewXLyra()
	summary, err := xlyra.SummarizeAPIKey(context.Background(), SiteConfig{BaseURL: server.URL, SiteType: "xlyra"}, "sk-summary")
	if err != nil {
		t.Fatalf("SummarizeAPIKey returned error: %v", err)
	}
	if gotModelAuth != "Bearer sk-summary" {
		t.Fatalf("model authorization header = %q, want bearer api key", gotModelAuth)
	}
	if len(summary.Models) != 2 {
		t.Fatalf("summary model len = %d, want 2: %#v", len(summary.Models), summary.Models)
	}
	raw, ok := summary.Raw.(map[string]any)
	if !ok {
		t.Fatalf("summary raw = %#v, want map", summary.Raw)
	}
	if raw["model_count"] != 2 {
		t.Fatalf("model_count = %#v, want 2", raw["model_count"])
	}

	userSummary, err := xlyra.FetchUserSummary(context.Background(), SiteConfig{BaseURL: server.URL}, SystemAuth{AccessToken: "profile-token"})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}
	if gotProfileToken != "profile-token" {
		t.Fatalf("profile token header = %q, want profile-token", gotProfileToken)
	}
	user, ok := userSummary.User.(map[string]any)
	if !ok {
		t.Fatalf("user summary = %#v, want map user", userSummary.User)
	}
	if user["email"] != "admin@example.test" {
		t.Fatalf("user email = %#v, want admin@example.test", user["email"])
	}
}

func TestXLyraListModelsETagCacheReturnsClonedModelsOnNotModified(t *testing.T) {
	calls := 0
	etag := `"xlyra-models-etag-admin-api"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		calls++
		if calls == 2 {
			if r.Header.Get("If-None-Match") != etag {
				t.Fatalf("If-None-Match = %q, want %q", r.Header.Get("If-None-Match"), etag)
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Fatalf("first If-None-Match = %q, want empty", got)
		}
		w.Header().Set("ETag", etag)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":       "gpt-cache",
					"object":   "model",
					"owned_by": "xlyra",
					"metadata": map[string]any{"family": "cached"},
				},
			},
		})
	}))
	defer server.Close()

	site := SiteConfig{BaseURL: server.URL, SiteType: "xlyra"}
	apiKey := "sk-cache-clone"
	t.Cleanup(func() {
		xlyraModelsCache.Delete(xlyraModelsCacheKey(site, apiKey))
	})

	xlyra := NewXLyra()
	models, err := xlyra.ListModels(context.Background(), site, apiKey)
	if err != nil {
		t.Fatalf("first ListModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("first models len = %d, want 1: %#v", len(models), models)
	}
	models[0].DisplayName = "mutated-display"
	models[0].Capabilities["owned_by"] = "mutated-owner"

	cachedModels, err := xlyra.ListModels(context.Background(), site, apiKey)
	if err != nil {
		t.Fatalf("second ListModels returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("model calls = %d, want 2", calls)
	}
	if len(cachedModels) != 1 || cachedModels[0].DisplayName != "gpt-cache" {
		t.Fatalf("unexpected cached models: %#v", cachedModels)
	}
	if cachedModels[0].Capabilities["owned_by"] != "xlyra" {
		t.Fatalf("cached capabilities were mutated: %#v", cachedModels[0].Capabilities)
	}
}

func TestXLyraAdminGetErrorAndDecodeBranches(t *testing.T) {
	t.Run("missing base url", func(t *testing.T) {
		_, err := NewXLyra().adminGet(context.Background(), SiteConfig{}, "token", "/api/v1/profile")
		if err == nil || !strings.Contains(err.Error(), "base_url is required") {
			t.Fatalf("adminGet error = %v, want base_url required", err)
		}
	})

	t.Run("missing access token", func(t *testing.T) {
		_, err := NewXLyra().adminGet(context.Background(), SiteConfig{BaseURL: "https://example.invalid"}, " \t ", "/api/v1/profile")
		if err == nil || !strings.Contains(err.Error(), "access token is required") {
			t.Fatalf("adminGet error = %v, want access token required", err)
		}
	})

	t.Run("request creation error", func(t *testing.T) {
		_, err := NewXLyra().adminGet(context.Background(), SiteConfig{BaseURL: "http://[::1"}, "token", "/api/v1/profile")
		if err == nil || !strings.Contains(err.Error(), "create xlyra admin request") {
			t.Fatalf("adminGet error = %v, want request creation error", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		wantErr := errors.New("transport stopped")
		site := SiteConfig{
			BaseURL: "https://example.invalid",
			Client: &http.Client{Transport: adapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, wantErr
			})},
		}
		_, err := NewXLyra().adminGet(context.Background(), site, "token", "/api/v1/profile")
		if err == nil || !strings.Contains(err.Error(), "call xlyra admin api") || !errors.Is(err, wantErr) {
			t.Fatalf("adminGet error = %v, want wrapped transport error", err)
		}
	})

	t.Run("non success status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "admin unavailable", http.StatusBadGateway)
		}))
		defer server.Close()

		_, err := NewXLyra().adminGet(context.Background(), SiteConfig{BaseURL: server.URL}, "token", "/api/v1/profile")
		if err == nil || !strings.Contains(err.Error(), "xlyra admin api returned 502: admin unavailable") {
			t.Fatalf("adminGet error = %v, want status error", err)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "{")
		}))
		defer server.Close()

		_, err := NewXLyra().adminGet(context.Background(), SiteConfig{BaseURL: server.URL}, "token", "/api/v1/profile")
		if err == nil || !strings.Contains(err.Error(), "decode xlyra admin api") {
			t.Fatalf("adminGet error = %v, want decode error", err)
		}
	})
}
