package adapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xlyra/server/internal/codexversion"
)

func TestNormalizeCodexUsageWHAMWindows(t *testing.T) {
	payload := map[string]any{
		"plan_type": "plus",
		"rate_limit": map[string]any{
			"allowed":       true,
			"limit_reached": false,
			"primary_window": map[string]any{
				"used_percent":         float64(12),
				"limit_window_seconds": float64(18000),
				"reset_at":             "2026-04-23T10:00:00Z",
			},
			"secondary_window": map[string]any{
				"used_percent":         float64(31),
				"limit_window_seconds": float64(604800),
				"reset_at":             "2026-04-30T10:00:00Z",
			},
		},
	}

	usage := normalizeCodexUsage(payload)
	if usage["plan_type"] != "plus" {
		t.Fatalf("plan_type = %v, want plus", usage["plan_type"])
	}
	if usage["available"] != true {
		t.Fatalf("available = %v, want true", usage["available"])
	}
	fiveHour, ok := usage["five_hour"].(map[string]any)
	if !ok {
		t.Fatalf("five_hour missing: %#v", usage)
	}
	if fiveHour["window"] != "five_hour" || fiveHour["remaining_percent"] != 88 {
		t.Fatalf("five_hour = %#v, want remaining 88", fiveHour)
	}
	fiveHourReset, _ := time.Parse(time.RFC3339, "2026-04-23T10:00:00Z")
	if fiveHour["reset_at"] != fiveHourReset.Unix() {
		t.Fatalf("five_hour reset_at = %#v, want %d", fiveHour["reset_at"], fiveHourReset.Unix())
	}
	weekly, ok := usage["weekly"].(map[string]any)
	if !ok {
		t.Fatalf("weekly missing: %#v", usage)
	}
	if weekly["window"] != "weekly" || weekly["remaining_percent"] != 69 {
		t.Fatalf("weekly = %#v, want remaining 69", weekly)
	}
	weeklyReset, _ := time.Parse(time.RFC3339, "2026-04-30T10:00:00Z")
	if weekly["reset_at"] != weeklyReset.Unix() {
		t.Fatalf("weekly reset_at = %#v, want %d", weekly["reset_at"], weeklyReset.Unix())
	}
}

func TestCodexUsageWindowPayloadPreservesGenuineZeroRemaining(t *testing.T) {
	window := codexUsageWindowPayload("primary_window", map[string]any{
		"remaining_percent":    float64(0),
		"limit_window_seconds": float64(18000),
	})
	if window["remaining_percent"] != 0 {
		t.Fatalf("remaining_percent = %#v, want 0 (genuine exhaustion must not be flipped to 100)", window["remaining_percent"])
	}
	if codexUsageAvailable(map[string]any{"five_hour": window}) {
		t.Fatal("codexUsageAvailable should be false when a window is genuinely exhausted")
	}
}

func TestNormalizeCodexUsageMarksExhaustedWindowUnavailable(t *testing.T) {
	payload := map[string]any{
		"rate_limit": map[string]any{
			"allowed":       true,
			"limit_reached": false,
			"primary_window": map[string]any{
				"remaining_percent":    float64(0),
				"limit_window_seconds": float64(18000),
			},
		},
	}

	usage := normalizeCodexUsage(payload)
	fiveHour, ok := usage["five_hour"].(map[string]any)
	if !ok {
		t.Fatalf("five_hour missing: %#v", usage)
	}
	if fiveHour["remaining_percent"] != 0 {
		t.Fatalf("five_hour remaining_percent = %#v, want 0", fiveHour["remaining_percent"])
	}
	if usage["available"] != false {
		t.Fatalf("available = %v, want false for an exhausted window", usage["available"])
	}
}

func TestNormalizeCodexUsageSupportsResetAliases(t *testing.T) {
	payload := map[string]any{
		"rate_limit": map[string]any{
			"allowed": true,
			"primary_window": map[string]any{
				"used_percent":         float64(20),
				"limit_window_seconds": float64(18000),
				"resets_at":            float64(1772000000),
			},
			"secondary_window": map[string]any{
				"used_percent":         float64(40),
				"limit_window_seconds": float64(604800),
				"resetAt":              "2026-04-26T10:00:00Z",
			},
		},
	}

	usage := normalizeCodexUsage(payload)
	fiveHour, _ := usage["five_hour"].(map[string]any)
	weekly, _ := usage["weekly"].(map[string]any)
	if fiveHour["reset_at"] != int64(1772000000) {
		t.Fatalf("five_hour reset_at alias not normalized: %#v", fiveHour["reset_at"])
	}
	weeklyReset, _ := time.Parse(time.RFC3339, "2026-04-26T10:00:00Z")
	if weekly["reset_at"] != weeklyReset.Unix() {
		t.Fatalf("weekly reset_at alias not normalized: %#v", weekly["reset_at"])
	}
}

func TestCodexOAuthAuthErrorIgnoresTransientHTML403(t *testing.T) {
	err := codexUpstreamHTTPError{
		statusCode:  403,
		contentType: "text/html; charset=utf-8",
		body:        `<html><head><meta http-equiv="refresh" content="360"></head><body></body></html>`,
	}
	if codexOAuthAuthError(err) {
		t.Fatal("transient codex html refresh challenge should not be treated as oauth auth failure")
	}
	if !codexOAuthAuthError(codexUpstreamHTTPError{
		statusCode:  403,
		contentType: "application/json",
		body:        `{"error":"invalid_token"}`,
	}) {
		t.Fatal("json 403 auth failure should still be treated as oauth auth failure")
	}
}

func TestCodexPricingSnapshotHasOfficialTokenPrices(t *testing.T) {
	snapshot := codexPricingSnapshot("plus")
	if len(snapshot.Items) == 0 {
		t.Fatal("expected pricing items")
	}

	gpt54 := codexPricingByModel(snapshot.Items, "gpt-5.4")
	if gpt54 == nil {
		t.Fatal("missing gpt-5.4 pricing")
	}
	if !gpt54.HasInputValue || gpt54.InputValue != 2.50 {
		t.Fatalf("unexpected gpt-5.4 input pricing: %#v", gpt54)
	}
	if !gpt54.HasOutputValue || gpt54.OutputValue != 15.00 {
		t.Fatalf("unexpected gpt-5.4 output pricing: %#v", gpt54)
	}
	if !gpt54.HasCacheRatio || gpt54.CacheRatio != 0.1 {
		t.Fatalf("unexpected gpt-5.4 cache ratio: %#v", gpt54)
	}

	gpt55 := codexPricingByModel(snapshot.Items, "gpt-5.5")
	if gpt55 == nil {
		t.Fatal("missing gpt-5.5 pricing")
	}
	if !gpt55.HasInputValue || gpt55.InputValue != 5.00 {
		t.Fatalf("unexpected gpt-5.5 input pricing: %#v", gpt55)
	}
	if !gpt55.HasOutputValue || gpt55.OutputValue != 30.00 {
		t.Fatalf("unexpected gpt-5.5 output pricing: %#v", gpt55)
	}
	if !gpt55.HasCacheRatio || gpt55.CacheRatio != 0.1 {
		t.Fatalf("unexpected gpt-5.5 cache ratio: %#v", gpt55)
	}
	freeSnapshot := codexPricingSnapshot("free")
	if codexPricingByModel(freeSnapshot.Items, "gpt-5.5") == nil {
		t.Fatalf("pricing is plan-agnostic and should include gpt-5.5 for free plans too: %#v", freeSnapshot.Items)
	}

	sol := codexPricingByModel(snapshot.Items, "gpt-5.6-sol")
	if sol == nil {
		t.Fatal("missing gpt-5.6-sol pricing")
	}
	if !sol.HasInputValue || sol.InputValue != 5.00 || !sol.HasOutputValue || sol.OutputValue != 30.00 {
		t.Fatalf("unexpected gpt-5.6-sol pricing: %#v", sol)
	}
	if !sol.HasCacheRatio || sol.CacheRatio != 0.1 || !sol.HasCreateCacheRatio || sol.CreateCacheRatio != 1.25 {
		t.Fatalf("unexpected gpt-5.6-sol cache ratios: %#v", sol)
	}

	terra := codexPricingByModel(snapshot.Items, "gpt-5.6-terra")
	if terra == nil || !terra.HasInputValue || terra.InputValue != 2.50 || terra.OutputValue != 15.00 {
		t.Fatalf("unexpected gpt-5.6-terra pricing: %#v", terra)
	}

	luna := codexPricingByModel(snapshot.Items, "gpt-5.6-luna")
	if luna == nil || !luna.HasInputValue || luna.InputValue != 1.00 || luna.OutputValue != 6.00 {
		t.Fatalf("unexpected gpt-5.6-luna pricing: %#v", luna)
	}

	image := codexPricingByModel(snapshot.Items, "gpt-image-2")
	if image == nil {
		t.Fatal("missing gpt-image-2 pricing")
	}
	if image.DisplayName != "GPT Image 2" {
		t.Fatalf("unexpected gpt-image-2 display name: %#v", image.DisplayName)
	}
	if !image.HasInputValue || image.InputValue != 5.00 {
		t.Fatalf("unexpected gpt-image-2 text input pricing: %#v", image)
	}
	if !image.HasOutputValue || image.OutputValue != 30.00 {
		t.Fatalf("unexpected gpt-image-2 image output pricing: %#v", image)
	}
	if !image.HasCacheRatio || image.CacheRatio != 0.25 {
		t.Fatalf("unexpected gpt-image-2 cache ratio: %#v", image)
	}
	if !image.HasImageRatio || image.ImageRatio != 1.6 {
		t.Fatalf("unexpected gpt-image-2 image ratio: %#v", image)
	}
}

func TestCodexPricingSnapshotExcludesUnusableBaselineModels(t *testing.T) {
	snapshot := codexPricingSnapshot("plus")
	for _, unusable := range []string{
		"gpt-5.2",
		"gpt-5.2-codex",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
	} {
		if codexPricingByModel(snapshot.Items, unusable) != nil {
			t.Fatalf("pricing should not surface unusable model %s: %#v", unusable, snapshot.Items)
		}
	}
}

func TestCodexListModelsUsesRemotePayloadAndHeaders(t *testing.T) {
	t.Parallel()

	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		if got := r.Header.Get("Origin"); got != codexOriginURL {
			t.Fatalf("Origin = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                       "gpt-5.4",
					"display_name":             "GPT 5.4",
					"available":                false,
					"supported_endpoint_types": []string{"openai-response", " openai-response ", "openai"},
				},
				{
					"id":   "   ",
					"name": "ignored",
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewCodex().ListModels(context.Background(), SiteConfig{
		BaseURL: server.URL + "/codex",
		Meta: map[string]any{
			"oauth_account_id": "acct-123",
		},
	}, " codex-token ")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(requestedPaths) != 1 || requestedPaths[0] != codexModelsPrimaryPath() {
		t.Fatalf("requested paths = %#v, want primary path only", requestedPaths)
	}
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1", len(models))
	}
	model := models[0]
	if model.UpstreamName != "gpt-5.4" || model.DisplayName != "GPT 5.4" {
		t.Fatalf("unexpected model: %#v", model)
	}
	if model.Capabilities["available"] != false {
		t.Fatalf("available capability = %#v, want false", model.Capabilities["available"])
	}
	endpoints, _ := model.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 3 || endpoints[0] != "openai-response" || endpoints[1] != "openai-response" || endpoints[2] != "openai" {
		t.Fatalf("supported_endpoint_types = %#v, want remote endpoint order preserved", endpoints)
	}
	if raw, _ := model.Capabilities["raw"].(map[string]any); raw["id"] != "gpt-5.4" {
		t.Fatalf("raw model missing: %#v", model.Capabilities["raw"])
	}
}

func TestCodexListModelsParsesOfficialSlugResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("client_version"); got != codexversion.Version() {
			t.Fatalf("client_version = %q, want %q", got, codexversion.Version())
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"slug":         "codex-auto-review",
					"display_name": "Codex Auto Review",
					"visibility":   "hide",
					"priority":     43,
				},
				{
					"slug":         "gpt-5.6-terra",
					"display_name": "GPT-5.6-Terra",
					"visibility":   "list",
					"priority":     2,
				},
				{
					"slug":         "gpt-5.6-sol",
					"display_name": "GPT-5.6-Sol",
					"visibility":   "list",
					"priority":     1,
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewCodex().ListModels(context.Background(), SiteConfig{
		BaseURL: server.URL,
		Meta: map[string]any{
			"oauth_account_id": "acct-123",
		},
	}, "codex-token")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models length = %d, want 2 (hidden model filtered): %#v", len(models), models)
	}
	if models[0].UpstreamName != "gpt-5.6-sol" || models[1].UpstreamName != "gpt-5.6-terra" {
		t.Fatalf("models not sorted by priority: %#v", models)
	}
	endpoints, _ := models[0].Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "openai-response" {
		t.Fatalf("official response without endpoint types should default to dual protocol, got %#v", endpoints)
	}
}

func TestCodexListModelsErrorsWhenRemoteEmpty(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()

	models, err := NewCodex().ListModels(context.Background(), SiteConfig{
		BaseURL: server.URL,
		Meta: map[string]any{
			"oauth_plan_type": "free",
		},
	}, "codex-token")
	if err == nil {
		t.Fatalf("ListModels should return an error when upstream returns no models, got %#v", models)
	}
	if requests != 4 {
		t.Fatalf("requests = %d, want all distinct model endpoints tried", requests)
	}
}

func TestCodexFetchUsageFallbacksAndNormalizesNestedQuotaWindows(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := r.Header.Get("Origin"); got != codexOriginURL {
			t.Fatalf("Origin = %q", got)
		}

		switch r.URL.Path {
		case "/backend-api/wham/usage":
			http.Error(w, "quota service warming up", http.StatusBadGateway)
		case "/backend-api/api/codex/usage":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plan_type":          " plus ",
				"email":              "user@example.com",
				"chatgpt_account_id": "acct-123",
				"quota": map[string]any{
					"windows": []map[string]any{
						{
							"name":               "primary",
							"windowDurationMins": float64(300),
							"limit":              float64(100),
							"used":               float64(25),
							"resetsAt":           "2026-04-23T10:00:00Z",
						},
						{
							"name":                    "secondary",
							"window_duration_minutes": float64(10080),
							"limit":                   float64(100),
							"remaining":               float64(40),
							"nextResetAt":             float64(1772000000),
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path after successful fallback: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	usage, err := NewCodex().fetchUsage(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, " codex-token ", " acct-123 ")
	if err != nil {
		t.Fatalf("fetchUsage returned error: %v", err)
	}
	if len(requestedPaths) != 2 ||
		requestedPaths[0] != "/backend-api/wham/usage" ||
		requestedPaths[1] != "/backend-api/api/codex/usage" {
		t.Fatalf("requested paths = %#v, want primary usage then fallback usage", requestedPaths)
	}
	if usage["plan_type"] != "plus" || usage["email"] != "user@example.com" || usage["account_id"] != "acct-123" {
		t.Fatalf("usage identity fields not normalized: %#v", usage)
	}
	if usage["available"] != true {
		t.Fatalf("available = %#v, want true", usage["available"])
	}
	fiveHour, ok := usage["five_hour"].(map[string]any)
	if !ok {
		t.Fatalf("five_hour missing: %#v", usage)
	}
	fiveHourReset, _ := time.Parse(time.RFC3339, "2026-04-23T10:00:00Z")
	if fiveHour["window"] != "five_hour" ||
		fiveHour["limit_window_seconds"] != 18000 ||
		fiveHour["used"] != 25 ||
		fiveHour["remaining"] != 75 ||
		fiveHour["remaining_percent"] != 75 ||
		fiveHour["reset_at"] != fiveHourReset.Unix() {
		t.Fatalf("five_hour window not normalized: %#v", fiveHour)
	}
	weekly, ok := usage["weekly"].(map[string]any)
	if !ok {
		t.Fatalf("weekly missing: %#v", usage)
	}
	if weekly["window"] != "weekly" ||
		weekly["limit_window_seconds"] != 604800 ||
		weekly["used"] != 60 ||
		weekly["remaining"] != 40 ||
		weekly["remaining_percent"] != 40 ||
		weekly["reset_at"] != int64(1772000000) {
		t.Fatalf("weekly window not normalized: %#v", weekly)
	}
}

func TestCodexFetchUserSummaryIncludesResetCreditsFromUsage(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plan_type": "plus",
				"rate_limit": map[string]any{
					"allowed": true,
					"primary_window": map[string]any{
						"used_percent":         float64(25),
						"limit_window_seconds": float64(18000),
					},
				},
				"rate_limit_reset_credits": map[string]any{
					"available_count": float64(2),
				},
			})
		case "/backend-api/codex/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id":           "gpt-summary",
					"display_name": "GPT Summary",
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
		AccessToken: "codex-token",
		AccountID:   "acct-123",
	})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}
	wantPaths := []string{"/backend-api/wham/usage", "/backend-api/codex/models"}
	if !sameStringSlice(requestedPaths, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requestedPaths, wantPaths)
	}
	user, _ := summary.User.(map[string]any)
	quota, _ := user["quota"].(map[string]any)
	resetCredits, ok := quota["reset_credits"].(map[string]any)
	if !ok || resetCredits["available_count"] != 2 {
		t.Fatalf("reset_credits = %#v, want available_count 2", quota["reset_credits"])
	}
}

func TestCodexFetchUserSummaryOmitsResetCreditsWhenAbsent(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"plan_type": "plus",
				"rate_limit": map[string]any{
					"allowed": true,
					"primary_window": map[string]any{
						"used_percent":         float64(25),
						"limit_window_seconds": float64(18000),
					},
				},
			})
		case "/backend-api/codex/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{{
					"id":           "gpt-summary",
					"display_name": "GPT Summary",
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
		AccessToken: "codex-token",
		AccountID:   "acct-123",
	})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}
	wantPaths := []string{"/backend-api/wham/usage", "/backend-api/codex/models"}
	if !sameStringSlice(requestedPaths, wantPaths) {
		t.Fatalf("requested paths = %#v, want %#v", requestedPaths, wantPaths)
	}
	user, _ := summary.User.(map[string]any)
	quota, _ := user["quota"].(map[string]any)
	if _, ok := quota["reset_credits"]; ok {
		t.Fatalf("reset_credits should be omitted when usage omits it: %#v", quota)
	}
	fiveHour, ok := quota["five_hour"].(map[string]any)
	if !ok || fiveHour["remaining_percent"] != 75 {
		t.Fatalf("five_hour = %#v, want usage quota", quota["five_hour"])
	}
}

func TestCodexListRateLimitResetCreditsParsesCredits(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"available_count":    float64(2),
			"total_earned_count": float64(5),
			"credits": []map[string]any{
				{
					"id":          "RateLimitResetCredit_aaa",
					"status":      "available",
					"reset_type":  "codex_rate_limits",
					"granted_at":  "2026-06-12T03:50:00Z",
					"expires_at":  "2026-07-12T03:50:00Z",
					"title":       "Full reset (Weekly + 5 hr)",
					"description": "Thanks for using Codex!",
				},
				{
					"id":          "RateLimitResetCredit_bbb",
					"status":      "redeemed",
					"granted_at":  "2026-06-01T00:00:00Z",
					"redeemed_at": "2026-06-05T00:00:00Z",
				},
				{
					"status": "no-id-should-be-skipped",
				},
			},
		})
	}))
	defer server.Close()

	got, err := NewCodex().ListRateLimitResetCredits(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, SystemAuth{
		AccessToken: "codex-token",
		AccountID:   "acct-123",
	})
	if err != nil {
		t.Fatalf("ListRateLimitResetCredits returned error: %v", err)
	}
	if requestedPath != "/backend-api/wham/rate-limit-reset-credits" {
		t.Fatalf("requested path = %q", requestedPath)
	}
	if got.AvailableCount != 2 || got.TotalEarnedCount != 5 {
		t.Fatalf("counts = %#v", got)
	}
	if len(got.Credits) != 2 {
		t.Fatalf("credits length = %d, want 2 (third entry should be skipped): %#v", len(got.Credits), got.Credits)
	}
	first := got.Credits[0]
	if first.ID != "RateLimitResetCredit_aaa" || first.Status != "available" || first.GrantedAt != "2026-06-12T03:50:00Z" || first.ExpiresAt != "2026-07-12T03:50:00Z" || first.Title != "Full reset (Weekly + 5 hr)" {
		t.Fatalf("first credit = %#v", first)
	}
	second := got.Credits[1]
	if second.ID != "RateLimitResetCredit_bbb" || second.Status != "redeemed" || second.RedeemedAt != "2026-06-05T00:00:00Z" {
		t.Fatalf("second credit = %#v", second)
	}
}

func TestCodexConsumeRateLimitResetCreditPostsRedeemRequest(t *testing.T) {
	var requestPayload map[string]any
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &requestPayload); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"outcome": "reset",
		})
	}))
	defer server.Close()

	result, err := NewCodex().ConsumeRateLimitResetCredit(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, SystemAuth{
		AccessToken: " codex-token ",
		AccountID:   "acct-123",
	}, " reset-key ", "")
	if err != nil {
		t.Fatalf("ConsumeRateLimitResetCredit returned error: %v", err)
	}
	if result["outcome"] != "reset" {
		t.Fatalf("outcome = %#v, want reset", result)
	}
	if requestedPath != "/backend-api/wham/rate-limit-reset-credits/consume" {
		t.Fatalf("requested path = %q, want wham consume endpoint", requestedPath)
	}
	if requestPayload["redeem_request_id"] != "reset-key" {
		t.Fatalf("payload = %#v, want trimmed redeem_request_id", requestPayload)
	}
	if _, ok := requestPayload["credit_id"]; ok {
		t.Fatalf("payload = %#v, credit_id should be omitted when empty", requestPayload)
	}
}

func TestCodexConsumeRateLimitResetCreditSendsCreditID(t *testing.T) {
	var requestPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &requestPayload); err != nil {
			t.Fatalf("request body is not JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":          "reset",
			"windows_reset": 2,
		})
	}))
	defer server.Close()

	result, err := NewCodex().ConsumeRateLimitResetCredit(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
	}, SystemAuth{
		AccessToken: "codex-token",
		AccountID:   "acct-123",
	}, "reset-key", " RateLimitResetCredit_aaa ")
	if err != nil {
		t.Fatalf("ConsumeRateLimitResetCredit returned error: %v", err)
	}
	if requestPayload["credit_id"] != "RateLimitResetCredit_aaa" {
		t.Fatalf("payload = %#v, want trimmed credit_id", requestPayload)
	}
	if result["outcome"] != "reset" {
		t.Fatalf("outcome = %#v, want reset mapped from code", result)
	}
}

func TestCodexResetCreditOutcomeFallsBackToCode(t *testing.T) {
	cases := map[string]string{
		"reset":            "reset",
		"nothing_to_reset": "nothingToReset",
		"no_credit":        "noCredit",
		"already_redeemed": "alreadyRedeemed",
	}
	for code, want := range cases {
		if got := codexResetCreditOutcome(map[string]any{"code": code}); got != want {
			t.Fatalf("codexResetCreditOutcome(code=%q) = %q, want %q", code, got, want)
		}
	}
	if got := codexResetCreditOutcome(map[string]any{"outcome": "reset", "code": "no_credit"}); got != "reset" {
		t.Fatalf("outcome should take precedence over code, got %q", got)
	}
	if got := codexResetCreditOutcome(map[string]any{"code": "unknown"}); got != "" {
		t.Fatalf("unknown code should map to empty outcome, got %q", got)
	}
}

func TestCodexSummaryKeepsModelsWhenUsageTransientFailure(t *testing.T) {
	var requestedPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer codex-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id = %q", got)
		}
		if got := r.Header.Get("Origin"); got != codexOriginURL {
			t.Fatalf("Origin = %q", got)
		}

		switch r.URL.Path {
		case "/backend-api/wham/usage",
			"/backend-api/api/codex/usage",
			"/api/codex/usage",
			"/backend-api/codex/usage":
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"temporary quota service unavailable"}`, http.StatusBadGateway)
		case "/backend-api/codex/models":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":                       "gpt-summary-test",
						"display_name":             "GPT Summary Test",
						"supported_endpoint_types": []string{"openai-response"},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	summary, err := NewCodex().SummarizeAPIKey(context.Background(), SiteConfig{
		BaseURL: server.URL + "/backend-api",
		Meta: map[string]any{
			"oauth_account_id": "acct-123",
			"oauth_plan_type":  "plus",
		},
	}, " codex-token ")
	if err != nil {
		t.Fatalf("SummarizeAPIKey returned error: %v", err)
	}
	if len(requestedPaths) != 5 || requestedPaths[4] != "/backend-api/codex/models" {
		t.Fatalf("requested paths = %#v, want usage endpoints followed by models", requestedPaths)
	}
	usage, ok := summary.Usage.(map[string]any)
	if !ok {
		t.Fatalf("summary usage = %#v, want map", summary.Usage)
	}
	if usage["available"] != false || usage["success"] != false {
		t.Fatalf("summary usage should record transient failure: %#v", usage)
	}
	if len(summary.Models) != 1 {
		t.Fatalf("summary models length = %d, want 1", len(summary.Models))
	}
	model := summary.Models[0]
	if model.UpstreamName != "gpt-summary-test" || model.DisplayName != "GPT Summary Test" {
		t.Fatalf("unexpected summary model: %#v", model)
	}
}

func TestCodexEndpointAndValueHelpers(t *testing.T) {
	t.Parallel()

	if got := normalizeCodexBaseURL(" https://chatgpt.example/backend-api/codex/ "); got != "https://chatgpt.example/backend-api" {
		t.Fatalf("normalizeCodexBaseURL = %q", got)
	}
	if got := normalizeCodexBaseURL(" "); got != codexBackendDefaultBaseURL {
		t.Fatalf("empty normalizeCodexBaseURL = %q", got)
	}
	if got := codexResetCreditConsumeEndpoints("https://chatgpt.example/backend-api"); len(got) == 0 || got[0] != "https://chatgpt.example/backend-api/wham/rate-limit-reset-credits/consume" {
		t.Fatalf("codexResetCreditConsumeEndpoints = %#v", got)
	}
	if got := codexResetCreditConsumeEndpoints("https://chatgpt.example/backend-api/codex/"); len(got) == 0 || got[0] != "https://chatgpt.example/backend-api/wham/rate-limit-reset-credits/consume" {
		t.Fatalf("codexResetCreditConsumeEndpoints with codex suffix = %#v", got)
	}
	if got := codexRateLimitResetCreditsEndpoints("https://chatgpt.example/backend-api"); len(got) == 0 || got[0] != "https://chatgpt.example/backend-api/wham/rate-limit-reset-credits" {
		t.Fatalf("codexRateLimitResetCreditsEndpoints = %#v", got)
	}
	if got := codexOriginFromBaseURL("not a url"); got != codexOriginURL {
		t.Fatalf("invalid origin = %q", got)
	}
	if got := codexOriginFromBaseURL("https://example.com/backend-api"); got != "https://example.com" {
		t.Fatalf("origin = %q, want https://example.com", got)
	}

	usageEndpoints := codexUsageEndpoints("https://example.com/backend-api")
	if len(usageEndpoints) != 4 || usageEndpoints[0] != "https://example.com/backend-api/wham/usage" {
		t.Fatalf("usage endpoints = %#v", usageEndpoints)
	}
	modelEndpoints := codexModelEndpoints("https://example.com/backend-api")
	if len(modelEndpoints) != 2 || modelEndpoints[0] != "https://example.com/backend-api/codex/models?client_version="+codexversion.Version() {
		t.Fatalf("model endpoints = %#v", modelEndpoints)
	}

	if defaultCodexAPIKeyName(" user@example.com ") != "user@example.com" {
		t.Fatal("defaultCodexAPIKeyName should prefer email")
	}
	if defaultCodexAPIKeyName(" ") != "Codex OAuth" {
		t.Fatal("defaultCodexAPIKeyName should fallback to Codex OAuth")
	}
	if !valueOrDefault(nil, true) || valueOrDefault(false, true) {
		t.Fatal("valueOrDefault did not preserve explicit bool/fallback")
	}
	if got := uniqueStrings(" a ", "a", "", "b"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("uniqueStrings = %#v", got)
	}
}

func codexPricingByModel(items []ModelPricing, modelName string) *ModelPricing {
	for index := range items {
		if items[index].ModelName == modelName {
			return &items[index]
		}
	}
	return nil
}

func codexModelByID(items []Model, id string) *Model {
	for index := range items {
		if items[index].UpstreamName == id {
			return &items[index]
		}
	}
	return nil
}

func codexUsageBucketByLimitID(items []map[string]any, limitID string) map[string]any {
	for _, item := range items {
		if item["limit_id"] == limitID {
			return item
		}
	}
	return nil
}
