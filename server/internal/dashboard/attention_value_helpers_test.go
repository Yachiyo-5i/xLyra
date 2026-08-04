package dashboard

import (
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestEpaperSummaryModelKeyFallsBackToUpstreamAndUnknown(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		row      store.RequestUsageDailySummary
		expected string
	}{
		{
			name:     "canonical model key wins",
			row:      store.RequestUsageDailySummary{CanonicalModelKey: " gpt-5 "},
			expected: "gpt-5",
		},
		{
			name:     "none canonical falls back to upstream",
			row:      store.RequestUsageDailySummary{CanonicalModelKey: summaryNoneKey, UpstreamModelName: " upstream-model "},
			expected: "upstream-model",
		},
		{
			name:     "blank and none values become unknown",
			row:      store.RequestUsageDailySummary{CanonicalModelKey: " ", UpstreamModelName: summaryNoneKey},
			expected: "unknown",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := epaperSummaryModelKey(tc.row); got != tc.expected {
				t.Fatalf("epaperSummaryModelKey = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestDashboardSiteActiveRejectsMissingAndZeroSiteRows(t *testing.T) {
	t.Parallel()

	missingID := uuid.New()
	zeroRowID := uuid.New()
	sites := map[uuid.UUID]store.Site{
		zeroRowID: {},
	}

	if dashboardSiteActive(uuid.NullUUID{UUID: missingID, Valid: true}, sites) {
		t.Fatal("expected missing site row to be inactive")
	}
	if dashboardSiteActive(uuid.NullUUID{UUID: zeroRowID, Valid: true}, sites) {
		t.Fatal("expected zero-value site row to be inactive")
	}
	if dashboardSiteActive(uuid.NullUUID{UUID: uuid.Nil, Valid: true}, sites) {
		t.Fatal("expected nil uuid site row to be inactive")
	}
}

func TestFailureRateAttentionWarnsAndSkipsBelowThresholds(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	lowSuccess := 0.7
	okSuccess := 0.8
	items := failureRateAttention(OverviewWindow{
		SiteCostSummary: []SiteCostSummaryItem{
			{SiteID: siteID.String(), SiteName: "Low Traffic", RequestCount: 4, SuccessCount: 1, SuccessRate: &lowSuccess},
			{SiteID: siteID.String(), SiteName: "No Rate", RequestCount: 10, SuccessCount: 0},
			{SiteID: siteID.String(), SiteName: "At Threshold", RequestCount: 10, SuccessCount: 8, SuccessRate: &okSuccess},
			{SiteID: siteID.String(), SiteName: "Warning", SiteType: "openai", RequestCount: 5, SuccessCount: 3, SuccessRate: &lowSuccess},
		},
	}, "2026-06-22T10:00:00Z", map[uuid.UUID]store.Site{
		siteID: {ID: siteID, Enabled: true, Status: "active"},
	})

	if len(items) != 1 {
		t.Fatalf("expected one warning failure-rate item, got %#v", items)
	}
	item := items[0]
	if item.Severity != "warning" || item.Type != "high_failure_rate" {
		t.Fatalf("unexpected failure-rate item severity/type: %#v", item)
	}
	if item.Action.Type != "open_requests" || item.Action.Params["site_id"] != siteID.String() || item.Action.Params["success"] != "false" {
		t.Fatalf("unexpected failure-rate action: %#v", item.Action)
	}
	if item.Subject["site_type"] != "openai" || item.Metrics["success_count"] != int64(3) {
		t.Fatalf("unexpected failure-rate payload: subject=%#v metrics=%#v", item.Subject, item.Metrics)
	}
}

func TestLatencyAttentionAddsSiteParamsSkipsInvalidAndLimitsItems(t *testing.T) {
	t.Parallel()

	siteID := uuid.New().String()
	updatedAt := "2026-06-22T10:00:00Z"
	items := latencyAttention([]HighLatencyItem{
		{SiteID: &siteID, SiteName: "Primary", ModelKey: "slow-a", RequestCount: 3, AvgLatencyMS: 40000, P95LatencyMS: highLatencyP95MS},
		{ModelKey: "unknown", RequestCount: 10, AvgLatencyMS: 90000, P95LatencyMS: highLatencyCriticalP95MS},
		{ModelKey: "too-few", RequestCount: 2, AvgLatencyMS: 90000, P95LatencyMS: highLatencyCriticalP95MS},
		{ModelKey: "too-fast", RequestCount: 3, AvgLatencyMS: 1000, P95LatencyMS: highLatencyP95MS - 1},
		{ModelKey: "slow-b", RequestCount: 3, AvgLatencyMS: 50000, P95LatencyMS: highLatencyP95MS},
		{ModelKey: "slow-c", RequestCount: 3, AvgLatencyMS: 60000, P95LatencyMS: highLatencyP95MS},
		{ModelKey: "slow-d", RequestCount: 3, AvgLatencyMS: 70000, P95LatencyMS: highLatencyP95MS},
	}, updatedAt)

	if len(items) != 3 {
		t.Fatalf("expected latency attention to keep first three eligible items, got %#v", items)
	}
	first := items[0]
	if first.ID != "latency:slow-a:"+siteID || first.Subject["site_id"] != siteID || first.Subject["site_name"] != "Primary" {
		t.Fatalf("expected site-specific latency subject, got %#v", first)
	}
	if first.Action.Params["site_id"] != siteID || first.Action.Params["model_key"] != "slow-a" {
		t.Fatalf("expected site-specific latency action params, got %#v", first.Action)
	}
	if first.UpdatedAt != updatedAt || first.Metrics["avg_latency_ms"] != float64(40000) {
		t.Fatalf("unexpected latency metadata: %#v", first)
	}
	for _, item := range items {
		modelKey, _ := item.Subject["model_key"].(string)
		if modelKey == "unknown" || modelKey == "too-few" || modelKey == "too-fast" || modelKey == "slow-d" {
			t.Fatalf("unexpected ineligible latency item selected: %#v", item)
		}
	}
}

func TestDefaultStringPtrUsesNonEmptyPointer(t *testing.T) {
	t.Parallel()

	value := "configured"
	if got := defaultStringPtr(&value, "fallback"); got != "configured" {
		t.Fatalf("defaultStringPtr non-empty = %q, want configured", got)
	}
}

func TestSelectFloat64CanSelectUpperPartition(t *testing.T) {
	t.Parallel()

	values := []float64{4, 1, 5, 2, 3}
	if got := selectFloat64(values, 4); got != 5 {
		t.Fatalf("selectFloat64 upper index = %v, want 5", got)
	}
}

func TestUnixSecondsPointerRejectsBlankString(t *testing.T) {
	t.Parallel()

	if got := unixSecondsPtrFromAny(" \t "); got != nil {
		t.Fatalf("blank unix timestamp = %#v, want nil", got)
	}
}
