package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func TestRequestLogFiltersParseValidQuery(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()
	from := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	values := url.Values{}
	values.Set("success", "true")
	values.Set("hide_without_site", "true")
	values.Set("site_id", siteID.String())
	values.Set("api_key_id", apiKeyID.String())
	values.Set("search", " claude ")
	values.Set("model_key", " gpt-5 ")
	values.Set("error_type", " upstream ")
	values.Set("endpoint", " /v1/responses ")
	values.Set("request_id", " req_123 ")
	values.Set("created_from", from.Format(time.RFC3339Nano))
	values.Set("created_to", to.Format(time.RFC3339Nano))
	req := adminTestRequest(http.MethodGet, "/api/v1/requests?"+values.Encode(), "")
	rec := adminRecorder()

	query, ok := (Handler{}).requestLogFilters(rec, req)
	adminRequireParserOK(t, rec, ok, "request log filters")
	if query.Success == nil || *query.Success != true {
		t.Fatalf("success filter = %#v, want true", query.Success)
	}
	if query.SiteID == nil || *query.SiteID != siteID || query.APIKeyID == nil || *query.APIKeyID != apiKeyID {
		t.Fatalf("unexpected UUID filters: site=%v api_key=%v", query.SiteID, query.APIKeyID)
	}
	if query.Search != "claude" || query.ModelKey != "gpt-5" || query.ErrorType != "upstream" || query.Endpoint != "/v1/responses" || query.RequestID != "req_123" {
		t.Fatalf("unexpected text filters: %#v", query)
	}
	if !query.HideWithoutSite {
		t.Fatal("expected hide_without_site to be true")
	}
	if query.CreatedFrom == nil || !query.CreatedFrom.Equal(from) || query.CreatedTo == nil || !query.CreatedTo.Equal(to) {
		t.Fatalf("unexpected time filters: from=%v to=%v", query.CreatedFrom, query.CreatedTo)
	}
}

func TestRequestLogFiltersRejectInvalidBooleanFilters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		target string
		code   string
	}{
		{name: "success", target: "/api/v1/requests?success=maybe", code: "invalid_success"},
		{name: "hide_without_site", target: "/api/v1/requests?hide_without_site=maybe", code: "invalid_hide_without_site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodGet, tc.target, "")
			rec := adminRecorder()
			_, ok := (Handler{}).requestLogFilters(rec, req)
			adminAssertParserError(t, rec, ok, tc.code)
		})
	}
}

func TestRequestLogSummaryReturnsUnsupportedPayloadForSearchFilter(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, nil, usage.NewService(nil), nil, nil, nil, nil, nil)
	rec := adminPerform(handler.RequestLogSummary, adminTestRequest(http.MethodGet, "/api/v1/requests/summary?search=claude", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Summary struct {
			TotalCost         any    `json:"total_cost"`
			PromptTokens      any    `json:"prompt_tokens"`
			CompletionTokens  any    `json:"completion_tokens"`
			TotalTokens       any    `json:"total_tokens"`
			CachedTokens      any    `json:"cached_tokens"`
			Currency          string `json:"currency"`
			Supported         bool   `json:"supported"`
			UnsupportedReason string `json:"unsupported_reason"`
		} `json:"summary"`
	}](t, rec)
	if body.Summary.Supported {
		t.Fatalf("expected unsupported summary, got %#v", body.Summary)
	}
	if body.Summary.UnsupportedReason != "search_filter" || body.Summary.Currency != "USD" {
		t.Fatalf("unexpected unsupported summary metadata: %#v", body.Summary)
	}
	if body.Summary.TotalCost != nil || body.Summary.PromptTokens != nil || body.Summary.CompletionTokens != nil || body.Summary.TotalTokens != nil || body.Summary.CachedTokens != nil {
		t.Fatalf("unsupported summary should null totals, got %#v", body.Summary)
	}
}

func TestRequestLogPayloadFallsBackToMetadataSnapshot(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"site_id":                 "site-deleted-id",
		"site_name":               "Codex_xcha_0419",
		"site_slug":               "codex-n45pjx",
		"site_type":               "codex",
		"site_model_id":           "site-model-deleted-id",
		"upstream_model":          "gpt-5.4",
		"site_model_display_name": "GPT 5.4",
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:        uuid.New(),
			RequestID: "req_123",
			Endpoint:  "/v1/responses",
			Success:   true,
			Metadata:  metadata,
		},
	}, false)

	site, _ := payload["site"].(map[string]any)
	model, _ := payload["model"].(map[string]any)
	if site["name"] != "Codex_xcha_0419" || site["slug"] != "codex-n45pjx" || site["site_type"] != "codex" {
		t.Fatalf("unexpected site snapshot fallback: %#v", site)
	}
	if model["site_model_id"] != "site-model-deleted-id" || model["upstream_model"] != "gpt-5.4" || model["display_name"] != "GPT 5.4" {
		t.Fatalf("unexpected model snapshot fallback: %#v", model)
	}
}

func TestRequestLogPayloadIncludesFastBillingCostCalculation(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"billing_mode": "fast",
		"cost_calculation": map[string]any{
			"service_tier":        "fast",
			"billing_mode":        "fast",
			"base_estimated_cost": 0.5,
			"cost_multiplier":     2.5,
			"estimated_cost":      1.25,
			"currency":            "USD",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:        uuid.New(),
			RequestID: "req_fast",
			Endpoint:  "/v1/responses",
			Success:   true,
			Metadata:  metadata,
		},
		EstimatedCost: sql.NullFloat64{Float64: 1.25, Valid: true},
		UsageCurrency: sql.NullString{String: "USD", Valid: true},
	}, false)

	calculation, ok := payload["cost_calculation"].(map[string]any)
	if !ok {
		t.Fatalf("expected cost_calculation payload, got %T", payload["cost_calculation"])
	}
	if calculation["billing_mode"] != "fast" || calculation["service_tier"] != "fast" {
		t.Fatalf("unexpected fast metadata: %#v", calculation)
	}
	if calculation["cost_multiplier"] != 2.5 || calculation["estimated_cost"] != 1.25 {
		t.Fatalf("unexpected cost calculation: %#v", calculation)
	}
}
