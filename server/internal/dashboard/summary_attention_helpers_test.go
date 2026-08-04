package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestCostAndRequestKPIsUseDefaultsAndTodayAttempts(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(3, time.Date(2026, 6, 22, 15, 0, 0, 0, time.UTC), timeZone)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: window.TodayStart, Success: true, RequestCount: 2, SuccessCount: 2, TotalTokens: 200, EstimatedCost: 1.5},
		{BucketStart: window.TodayStart, Success: false, RequestCount: 3, FailureCount: 3, TotalTokens: 90, EstimatedCost: 0.75, Currency: "EUR"},
		{BucketStart: window.TodayStart.AddDate(0, 0, -1), Success: true, RequestCount: 4, SuccessCount: 4, TotalTokens: 400, EstimatedCost: 2.25},
	}

	cost := costKPIFromSummaries(window, rows)
	if cost.Today != 2.25 || cost.Yesterday != 2.25 || cost.Total != 4.5 {
		t.Fatalf("unexpected cost KPI totals: %#v", cost)
	}
	if cost.Currency != "EUR" {
		t.Fatalf("expected last non-empty currency to win, got %q", cost.Currency)
	}

	requests := requestsKPIFromSummaries(window, rows)
	if requests.Today != 2 || requests.TodayTokens != 200 {
		t.Fatalf("unexpected successful today request totals: %#v", requests)
	}
	if requests.Yesterday != 4 || requests.YesterdayTokens != 400 || requests.Total != 6 || requests.TotalTokens != 600 {
		t.Fatalf("unexpected successful historical request totals: %#v", requests)
	}
	if requests.SuccessRate == nil || *requests.SuccessRate != 0.4 {
		t.Fatalf("expected today success rate from all attempts, got %#v", requests.SuccessRate)
	}

	empty := requestsKPIFromSummaries(window, nil)
	if empty.SuccessRate != nil {
		t.Fatalf("expected nil success rate without today attempts, got %#v", empty.SuccessRate)
	}
}

func TestEpaperModelTop3SortsTiesAndUsesUnknownFallback(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(1, time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC), timeZone)
	items := epaperModelTop3TodayFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.TodayStart.Add(-time.Nanosecond), CanonicalModelKey: "old", EstimatedCost: 100},
		{BucketStart: window.TodayStart, CanonicalModelKey: "beta", EstimatedCost: 5},
		{BucketStart: window.TodayStart, CanonicalModelKey: "alpha", EstimatedCost: 5},
		{BucketStart: window.TodayStart, CanonicalModelKey: summaryNoneKey, UpstreamModelName: summaryNoneKey, EstimatedCost: 4},
		{BucketStart: window.TodayStart.AddDate(0, 0, 1), CanonicalModelKey: "future", EstimatedCost: 99},
		{BucketStart: window.TodayStart, CanonicalModelKey: "zero", EstimatedCost: 0},
	})

	expected := []EpaperModelCostItem{
		{ModelKey: "alpha", Cost: 5},
		{ModelKey: "beta", Cost: 5},
		{ModelKey: "unknown", Cost: 4},
	}
	if len(items) != len(expected) {
		t.Fatalf("expected %d epaper items, got %#v", len(expected), items)
	}
	for index, item := range items {
		if item != expected[index] {
			t.Fatalf("item %d = %#v, want %#v", index, item, expected[index])
		}
	}
}

func TestAttentionSortingRanksKnownTypesAndLimitsByType(t *testing.T) {
	t.Parallel()

	items := []AttentionItem{
		{ID: "z-custom", Severity: "debug", Type: "custom"},
		{ID: "latency:a", Severity: "warning", Type: "high_latency"},
		{ID: "pricing:a", Severity: "info", Type: "missing_model_pricing"},
		{ID: "route:no", Severity: "critical", Type: "no_route_candidates"},
		{ID: "cooldown:a", Severity: "warning", Type: "active_cooldown"},
	}
	sortAttentionItems(items)
	for index, expectedID := range []string{"route:no", "cooldown:a", "latency:a", "pricing:a", "z-custom"} {
		if items[index].ID != expectedID {
			t.Fatalf("sorted item %d = %s, want %s in %#v", index, items[index].ID, expectedID, items)
		}
	}

	many := []AttentionItem{
		{ID: "route:critical", Severity: "critical", Type: "no_route_candidates"},
		{ID: "cooldown:a", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:b", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:c", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:d", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:e", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:f", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:g", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:h", Severity: "warning", Type: "active_cooldown"},
		{ID: "cooldown:i", Severity: "warning", Type: "active_cooldown"},
		{ID: "pricing:info", Severity: "info", Type: "missing_model_pricing"},
		{ID: "latency:late", Severity: "warning", Type: "high_latency"},
	}
	sortAttentionItems(many)
	selected := selectAttentionItems(many)
	if len(selected) != attentionLimit {
		t.Fatalf("expected selected attention limit %d, got %d", attentionLimit, len(selected))
	}
	byType := map[string]int{}
	byID := map[string]bool{}
	for _, item := range selected {
		byType[item.Type]++
		byID[item.ID] = true
	}
	if byType["active_cooldown"] <= attentionInitialTypeLimit {
		t.Fatalf("expected deferred cooldown items to refill remaining slots, got counts %#v", byType)
	}
	if !byID["route:critical"] || !byID["latency:late"] || !byID["pricing:info"] {
		t.Fatalf("expected limited selection to preserve distinct item types, got %#v", selected)
	}
}

func TestRouteCandidateAttentionLowCandidatePayload(t *testing.T) {
	t.Parallel()

	items := routeCandidateAttentionItems([]InsufficientCandidateItem{
		{
			CanonicalModelID: "model-123",
			ModelKey:         "gpt-low",
			SiteModelCount:   3,
			SiteCount:        2,
			EligibleCount:    1,
			CooldownCount:    1,
			RequestCount24h:  8,
		},
	}, "2026-06-22T12:00:00Z")

	if len(items) != 1 {
		t.Fatalf("expected one low-candidate attention item, got %#v", items)
	}
	item := items[0]
	if item.ID != "route:gpt-low:low_candidates" || item.Type != "low_route_candidates" || item.Severity != "warning" {
		t.Fatalf("unexpected low-candidate identity: %#v", item)
	}
	if item.Subject["canonical_model_id"] != "model-123" || item.Subject["model_key"] != "gpt-low" {
		t.Fatalf("unexpected low-candidate subject: %#v", item.Subject)
	}
	if item.Metrics["eligible_count"] != 1 || item.Metrics["request_count_24h"] != 8 {
		t.Fatalf("unexpected low-candidate metrics: %#v", item.Metrics)
	}
	if item.Action.Type != "open_routes" || item.Action.Params["model_key"] != "gpt-low" {
		t.Fatalf("unexpected low-candidate action: %#v", item.Action)
	}
}

func TestQuotaAccumulatorsCountOnlyRemainingPercent(t *testing.T) {
	t.Parallel()

	quota := summarizeEpaperCodexQuota([]map[string]any{
		{"five_hour": map[string]any{"reset_at": "2026-06-22T12:00:00Z"}},
		{"weekly": map[string]any{"remaining_percent": json.Number("33.6"), "reset_at": "12345.6"}},
	})

	if quota.AccountCount != 1 {
		t.Fatalf("expected only item with remaining percent to count, got %#v", quota)
	}
	if quota.FiveHour.RemainingPercent != nil {
		t.Fatalf("expected reset-only five-hour window to omit remaining average, got %#v", quota.FiveHour)
	}
	if quota.FiveHour.ResetAt == nil || *quota.FiveHour.ResetAt != time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC).Unix() {
		t.Fatalf("expected reset-only five-hour reset to be retained, got %#v", quota.FiveHour.ResetAt)
	}
	if quota.Weekly.RemainingPercent == nil || *quota.Weekly.RemainingPercent != 34 {
		t.Fatalf("expected rounded weekly remaining percent, got %#v", quota.Weekly.RemainingPercent)
	}
	if quota.Weekly.ResetAt == nil || *quota.Weekly.ResetAt != 12346 {
		t.Fatalf("expected rounded numeric reset, got %#v", quota.Weekly.ResetAt)
	}
}

func TestParseActiveDashboardSiteIDRequiresActiveMapEntry(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	active := map[uuid.UUID]store.Site{
		siteID: {ID: siteID, Enabled: true, Status: "active"},
	}

	if parsed, ok := parseActiveDashboardSiteID(" "+siteID.String()+" ", active); !ok || parsed != siteID {
		t.Fatalf("expected trimmed active site id to parse, got %s ok=%v", parsed, ok)
	}
	if _, ok := parseActiveDashboardSiteID(uuid.New().String(), active); ok {
		t.Fatal("expected missing active site id to be rejected")
	}
	if _, ok := parseActiveDashboardSiteID(uuid.Nil.String(), active); ok {
		t.Fatal("expected nil site id to be rejected")
	}
	if _, ok := parseActiveDashboardSiteID("not-a-uuid", active); ok {
		t.Fatal("expected invalid site id to be rejected")
	}
}
