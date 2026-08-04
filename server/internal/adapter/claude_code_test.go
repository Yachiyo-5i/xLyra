package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClaudeCodePlanTypeDistinguishesMaxTiers(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		organizationType string
		rateLimitTier    string
		want             string
	}{
		"max 20x": {organizationType: "claude_max", rateLimitTier: "default_claude_max_20x", want: "max20x"},
		"max 5x":  {organizationType: "claude_max", rateLimitTier: "default_claude_max_5x", want: "max5x"},
		"max":     {organizationType: "claude_max", want: "max"},
		"pro":     {organizationType: "claude_pro", want: "pro"},
	}
	for name, tt := range cases {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := ClaudeCodePlanType(tt.organizationType, tt.rateLimitTier); got != tt.want {
				t.Fatalf("ClaudeCodePlanType = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaudeCodeQuotaSummaryAdaptsUsageForQuotaPanel(t *testing.T) {
	t.Parallel()

	got := ClaudeCodeQuotaSummary(map[string]any{
		"five_hour": map[string]any{
			"utilization": 37.5,
			"resets_at":   "2026-07-26T12:00:00Z",
		},
		"limits": []any{
			map[string]any{
				"kind":      "weekly_scoped",
				"group":     "weekly",
				"percent":   42.0,
				"is_active": true,
				"scope": map[string]any{
					"model": map[string]any{"display_name": "Sonnet"},
				},
			},
		},
	})

	if got["type"] != "claude_code" || got["raw"] == nil {
		t.Fatalf("quota summary lost type/raw: %#v", got)
	}
	fiveHour := got["five_hour"].(map[string]any)
	if fiveHour["used_percent"] != 37.5 || fiveHour["remaining_percent"] != 62.5 || fiveHour["reset_at"] != "2026-07-26T12:00:00Z" {
		t.Fatalf("five hour quota = %#v", fiveHour)
	}
	weekly := got["weekly"].(map[string]any)
	if weekly["display_name"] != "Sonnet" || weekly["used_percent"] != 42.0 || weekly["remaining_percent"] != 58.0 {
		t.Fatalf("weekly quota = %#v", weekly)
	}
	models := got["models"].([]map[string]any)
	if len(models) != 1 || models[0]["name"] != "weekly" {
		t.Fatalf("model quotas = %#v", models)
	}
}

func TestClaudeCodeQuotaSummaryExposesScopedSevenDayWindows(t *testing.T) {
	t.Parallel()

	got := ClaudeCodeQuotaSummary(map[string]any{
		"five_hour": map[string]any{
			"utilization": 12.0,
			"resets_at":   "2026-07-26T12:00:00Z",
		},
		"seven_day": map[string]any{
			"utilization": 30.0,
			"resets_at":   "2026-07-30T00:00:00Z",
		},
		"seven_day_fable": map[string]any{
			"utilization": 55.0,
			"resets_at":   "2026-07-30T00:00:00Z",
		},
	})

	weekly := got["weekly"].(map[string]any)
	if weekly["used_percent"] != 30.0 || weekly["display_name"] != nil {
		t.Fatalf("weekly quota should stay the account-wide window: %#v", weekly)
	}
	models := got["models"].([]map[string]any)
	if len(models) != 1 {
		t.Fatalf("model quotas = %#v", models)
	}
	fable := models[0]
	if fable["name"] != "weekly" || fable["display_name"] != "Fable" || fable["used_percent"] != 55.0 || fable["remaining_percent"] != 45.0 {
		t.Fatalf("fable quota = %#v", fable)
	}
}

func TestClaudeCodeFetchUserSummaryUsesOAuthHeadersAndAdaptedQuota(t *testing.T) {
	t.Parallel()

	var usageAuth string
	var usageBeta string
	var modelsApp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/oauth/usage":
			usageAuth = r.Header.Get("Authorization")
			usageBeta = r.Header.Get("anthropic-beta")
			_, _ = w.Write([]byte(`{"five_hour":{"utilization":10},"limits":[{"kind":"session","group":"session","percent":10}]}`))
		case "/v1/models":
			modelsApp = r.Header.Get("X-App")
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-5","display_name":"Claude Sonnet 5","type":"model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	summary, err := NewClaudeCode().FetchUserSummary(context.Background(), SiteConfig{BaseURL: server.URL, Client: server.Client()}, SystemAuth{
		AccessToken: " token-main ",
		Email:       "user@example.com",
		AccountID:   "acct",
		Metadata: map[string]any{
			"organization_type": "claude_max",
			"rate_limit_tier":   "default_claude_max_20x",
		},
	})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}
	if usageAuth != "Bearer token-main" || usageBeta != ClaudeCodeOAuthBeta || modelsApp != "cli" {
		t.Fatalf("unexpected headers: usageAuth=%q usageBeta=%q modelsApp=%q", usageAuth, usageBeta, modelsApp)
	}
	user := summary.User.(map[string]any)
	if user["plan_type"] != "max20x" {
		t.Fatalf("plan_type = %#v, want max20x", user["plan_type"])
	}
	quota := user["quota"].(map[string]any)
	fiveHour := quota["five_hour"].(map[string]any)
	if fiveHour["remaining_percent"] != 90.0 {
		t.Fatalf("quota = %#v", quota)
	}
}
