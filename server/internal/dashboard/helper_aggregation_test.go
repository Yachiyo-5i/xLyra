package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestLatencyAttentionLimitsToFirstThreeActionableRows(t *testing.T) {
	t.Parallel()

	siteID := uuid.New().String()
	modelID := uuid.New().String()
	items := latencyAttention([]HighLatencyItem{
		{SiteName: "Unknown", ModelKey: "unknown", RequestCount: 10, AvgLatencyMS: 60000, P95LatencyMS: 60000},
		{SiteName: "Too Few", ModelKey: "few", RequestCount: 2, AvgLatencyMS: 60000, P95LatencyMS: 60000},
		{SiteName: "Below", ModelKey: "below", RequestCount: 3, AvgLatencyMS: 10000, P95LatencyMS: highLatencyP95MS - 1},
		{SiteID: &siteID, SiteName: "Site A", ModelID: &modelID, ModelKey: "slow-a", RequestCount: 3, AvgLatencyMS: 45000, P95LatencyMS: highLatencyP95MS},
		{SiteName: "Site B", ModelKey: "slow-b", RequestCount: 4, AvgLatencyMS: 55000, P95LatencyMS: highLatencyCriticalP95MS},
		{SiteName: "Site C", ModelKey: "slow-c", RequestCount: 5, AvgLatencyMS: 65000, P95LatencyMS: highLatencyP95MS + 1},
		{SiteName: "Site D", ModelKey: "slow-d", RequestCount: 6, AvgLatencyMS: 75000, P95LatencyMS: highLatencyP95MS + 2},
	}, "2026-06-22T12:00:00Z")

	if len(items) != 3 {
		t.Fatalf("expected first three actionable latency items, got %#v", items)
	}
	if items[0].ID != "latency:slow-a:"+siteID || items[0].Severity != "warning" {
		t.Fatalf("unexpected first latency item: %#v", items[0])
	}
	if items[0].Subject["site_id"] != siteID || items[0].Action.Params["site_id"] != siteID {
		t.Fatalf("expected site-scoped latency action, got subject=%#v action=%#v", items[0].Subject, items[0].Action)
	}
	if items[1].ID != "latency:slow-b:unknown" || items[1].Severity != "critical" {
		t.Fatalf("expected critical latency item without site id fallback, got %#v", items[1])
	}
	if items[2].Subject["model_key"] != "slow-c" {
		t.Fatalf("expected third actionable latency model, got %#v", items[2].Subject)
	}
}

func TestAttentionTypeRankOrdersAllKnownDashboardTypes(t *testing.T) {
	t.Parallel()

	orderedTypes := []string{
		"no_route_candidates",
		"active_cooldown",
		"site_unhealthy",
		"high_failure_rate",
		"rate_limit_exceeded",
		"low_route_candidates",
		"missing_model_pricing",
		"high_latency",
	}
	for index, itemType := range orderedTypes {
		if got := attentionTypeRank(itemType); got != index {
			t.Fatalf("expected %s to rank %d, got %d", itemType, index, got)
		}
	}
	if got := attentionTypeRank("custom"); got <= len(orderedTypes) {
		t.Fatalf("expected custom attention type to sort after known types, got rank %d", got)
	}
}

func TestSelectAttentionItemsReturnsInputWhenWithinLimit(t *testing.T) {
	t.Parallel()

	items := []AttentionItem{
		{ID: "b", Severity: "warning", Type: "high_latency"},
		{ID: "a", Severity: "critical", Type: "no_route_candidates"},
	}
	selected := selectAttentionItems(items)

	if len(selected) != len(items) {
		t.Fatalf("expected items within limit to be returned unchanged, got %#v", selected)
	}
	if &selected[0] != &items[0] {
		t.Fatal("expected selectAttentionItems to return the original slice when no limiting is needed")
	}
}

func TestFailureRateAttentionWarnsForModerateFailureRate(t *testing.T) {
	t.Parallel()

	successRate := 0.6
	siteID := uuid.New()
	items := failureRateAttention(OverviewWindow{
		SiteCostSummary: []SiteCostSummaryItem{
			{SiteID: siteID.String(), SiteName: "Primary", SiteType: "openai", RequestCount: 5, SuccessCount: 3, SuccessRate: &successRate},
		},
	}, "2026-06-22T12:00:00Z", storeSiteForAttention(siteID))

	if len(items) != 1 {
		t.Fatalf("expected one warning failure-rate attention item, got %#v", items)
	}
	if items[0].Severity != "warning" || items[0].Action.Params["success"] != "false" {
		t.Fatalf("unexpected failure-rate attention item: %#v", items[0])
	}
}

func TestPercentileContHandlesEmptySingletonAndMaxPercentile(t *testing.T) {
	t.Parallel()

	if got := percentileCont(nil, 0.95); got != 0 {
		t.Fatalf("expected empty percentile to be zero, got %v", got)
	}
	if got := percentileCont([]float64{12.5}, 0.95); got != 12.5 {
		t.Fatalf("expected singleton percentile to return only value, got %v", got)
	}
	if got := percentileCont([]float64{3, 1, 2}, 1); got != 3 {
		t.Fatalf("expected max percentile to return largest value, got %v", got)
	}
}

func TestQuotaScalarHelpersHandleNativeIntsNilAndRFC3339Reset(t *testing.T) {
	t.Parallel()

	if got := intPtrFromAny(nil); got != nil {
		t.Fatalf("expected nil int input to stay nil, got %#v", got)
	}
	if got := intPtrFromAny(17); got == nil || *got != 17 {
		t.Fatalf("expected native int conversion, got %#v", got)
	}
	if got := intPtrFromAny(json.Number("bad")); got != nil {
		t.Fatalf("expected invalid json number to be nil, got %#v", got)
	}
	if got := unixSecondsPtrFromAny(nil); got != nil {
		t.Fatalf("expected nil unix input to stay nil, got %#v", got)
	}
	if got := unixSecondsPtrFromAny(123); got == nil || *got != 123 {
		t.Fatalf("expected native int unix conversion, got %#v", got)
	}
	resetAt := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	if got := unixSecondsPtrFromAny(resetAt.Format(time.RFC3339Nano)); got == nil || *got != resetAt.Unix() {
		t.Fatalf("expected RFC3339Nano reset conversion, got %#v", got)
	}
}

func storeSiteForAttention(siteID uuid.UUID) map[uuid.UUID]store.Site {
	return map[uuid.UUID]store.Site{
		siteID: {ID: siteID, Enabled: true, Status: "active"},
	}
}
