package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/upstream"
)

func TestXLyraAdminAccessTokenListsAPIKeys(t *testing.T) {
	var gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Access-Token")
		switch r.URL.Path {
		case "/api/v1/profile":
			_ = json.NewEncoder(w).Encode(map[string]any{"admin": map[string]any{"username": "admin"}})
		case "/api/v1/api-keys":
			if r.URL.Query().Get("view") != "sync" {
				t.Fatalf("expected sync view query, got %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":              "b5d53b21-74ac-45c6-8ec4-6fa20a08a7ed",
						"name":            "default",
						"key":             "sk-upstream",
						"masked_key":      "sk-***",
						"status":          "active",
						"quota_available": float64(1500),
						"quota_used":      float64(500),
						"quota_unlimited": false,
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	xlyra := NewXLyra()
	auth := SystemAuth{AccessToken: "xlyra-admin-test"}
	if err := xlyra.ValidateSystemCredentials(context.Background(), SiteConfig{BaseURL: server.URL}, auth); err != nil {
		t.Fatalf("ValidateSystemCredentials returned error: %v", err)
	}
	keys, err := xlyra.ListAPIKeys(context.Background(), SiteConfig{BaseURL: server.URL}, auth)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if gotToken != "xlyra-admin-test" {
		t.Fatalf("admin token header = %q, want xlyra-admin-test", gotToken)
	}
	if len(keys) != 1 || keys[0].Key != "sk-upstream" || keys[0].MaskedKey != "sk-***" {
		t.Fatalf("unexpected keys: %#v", keys)
	}
	if keys[0].ExternalID != "b5d53b21-74ac-45c6-8ec4-6fa20a08a7ed" {
		t.Fatalf("unexpected external id: %#v", keys[0])
	}
	if keys[0].Raw["remain_quota"] != float64(1500) || keys[0].Raw["used_quota"] != float64(500) || keys[0].Raw["unlimited_quota"] != false {
		t.Fatalf("unexpected normalized quota: %#v", keys[0].Raw)
	}
}

func TestXLyraValidateCredentialsClassifiesQuotaHTTPError(t *testing.T) {
	t.Parallel()

	err := NewXLyra().ValidateCredentials(context.Background(), SiteConfig{
		BaseURL: "https://xlyra.example.test",
		Client: &http.Client{Transport: adapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return adapterTestResponse(http.StatusUnauthorized, `{"error":{"code":"api_key_weekly_quota_exhausted"}}`), nil
		})},
	}, "sk-limited")
	if err == nil || !upstream.ClassifyError(err).Limited() {
		t.Fatalf("ValidateCredentials error = %v, want classified quota failure", err)
	}
}

func TestXLyraAPIKeyModeListsModels(t *testing.T) {
	var gotAuth string
	var gotSync string
	var gotIfNoneMatch string
	calls := 0
	etag := `"models-sha256-test"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		calls++
		gotAuth = r.Header.Get("Authorization")
		gotSync = r.Header.Get("X-XLyra-Sync")
		gotIfNoneMatch = r.Header.Get("If-None-Match")
		if calls == 2 {
			if gotIfNoneMatch != etag {
				t.Fatalf("If-None-Match = %q, want %q", gotIfNoneMatch, etag)
			}
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-test", "object": "model", "owned_by": "xlyra"},
			},
		})
	}))
	defer server.Close()

	models, err := NewXLyra().ListModels(context.Background(), SiteConfig{BaseURL: server.URL, SiteType: "xlyra"}, "sk-downstream")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if gotAuth != "Bearer sk-downstream" {
		t.Fatalf("authorization header = %q, want bearer downstream key", gotAuth)
	}
	if gotSync != "models" {
		t.Fatalf("xlyra sync header = %q, want models", gotSync)
	}
	if len(models) != 1 || models[0].UpstreamName != "gpt-test" {
		t.Fatalf("unexpected models: %#v", models)
	}

	models, err = NewXLyra().ListModels(context.Background(), SiteConfig{BaseURL: server.URL, SiteType: "xlyra"}, "sk-downstream")
	if err != nil {
		t.Fatalf("second ListModels returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(models) != 1 || models[0].UpstreamName != "gpt-test" {
		t.Fatalf("unexpected cached models: %#v", models)
	}
}
