package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestNewServiceUsesConfiguredTimeZoneForWindowFormat(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	service := NewService(nil, timeZone)
	window := service.newTimeWindow(1, time.Date(2026, 5, 10, 3, 4, 5, 0, time.FixedZone("CST", 8*60*60)))

	if window.TimeZoneName != "UTC" {
		t.Fatalf("unexpected service timezone: %s", window.TimeZoneName)
	}
	if got := window.format(window.TodayStart, time.RFC3339); got != "2026-05-09T00:00:00Z" {
		t.Fatalf("unexpected formatted start: %s", got)
	}
}

func TestDashboardPublicMethodsRejectNilStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	var service *Service
	checks := []struct {
		name string
		call func() error
	}{
		{name: "usage", call: func() error { _, err := service.Usage(ctx, now); return err }},
		{name: "cooldowns", call: func() error { _, err := service.Cooldowns(ctx, now); return err }},
		{name: "health", call: func() error { _, err := service.Health(ctx, now); return err }},
		{name: "insights", call: func() error { _, err := service.Insights(ctx, now); return err }},
		{name: "epaper", call: func() error { _, err := service.EpaperSummary(ctx, now); return err }},
	}

	for _, check := range checks {
		check := check
		t.Run(check.name, func(t *testing.T) {
			t.Parallel()

			err := check.call()
			if err == nil || !strings.Contains(err.Error(), "dashboard store is not initialized") {
				t.Fatalf("expected nil store error, got %v", err)
			}
		})
	}
}

func TestNewTimeWindowUsesShanghaiDayBoundary(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	location := timeZone.Location
	now := time.Date(2026, 5, 10, 3, 4, 5, 0, location)
	window := newTimeWindow(7, now, timeZone)

	if got := window.TodayStart.Format(time.RFC3339); got != "2026-05-10T00:00:00+08:00" {
		t.Fatalf("unexpected today start: %s", got)
	}
	if got := window.RangeStart.Format(time.RFC3339); got != "2026-05-04T00:00:00+08:00" {
		t.Fatalf("unexpected range start: %s", got)
	}
}

func TestNewTimeWindowUsesConfiguredTimeZone(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(7, time.Date(2026, 5, 10, 3, 4, 5, 0, time.FixedZone("CST", 8*60*60)), timeZone)

	if window.TimeZoneName != "UTC" {
		t.Fatalf("unexpected timezone name: %s", window.TimeZoneName)
	}
	if got := window.TodayStart.Format(time.RFC3339); got != "2026-05-09T00:00:00Z" {
		t.Fatalf("unexpected UTC today start: %s", got)
	}
}

func TestCostKPIFromSummariesTracksTodayYesterdayAndTotal(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, timeZone.Location), timeZone)
	kpi := costKPIFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.TodayStart, EstimatedCost: 1.25, Currency: "USD"},
		{BucketStart: window.TodayStart.AddDate(0, 0, -1), EstimatedCost: 2.5, Currency: "USD"},
		{BucketStart: window.TodayStart.AddDate(0, 0, -2), EstimatedCost: 3.75, Currency: "USD"},
	})

	if kpi.Today != 1.25 || kpi.Yesterday != 2.5 || kpi.Total != 7.5 {
		t.Fatalf("unexpected cost KPI: %#v", kpi)
	}
	if kpi.Currency != "USD" {
		t.Fatalf("unexpected currency: %s", kpi.Currency)
	}
}

func TestRequestsKPIFromSummariesCountsOnlySuccessfulRequestsAndTokens(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, timeZone.Location), timeZone)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: window.TodayStart, Success: true, RequestCount: 1, SuccessCount: 1, PromptTokens: 70, CompletionTokens: 30, TotalTokens: 100, CachedTokens: 20},
		{BucketStart: window.TodayStart, Success: false, RequestCount: 1, FailureCount: 1, PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30, CachedTokens: 10},
		{BucketStart: window.TodayStart.AddDate(0, 0, -1), Success: true, RequestCount: 1, SuccessCount: 1, PromptTokens: 60, CompletionTokens: 20, TotalTokens: 80, CachedTokens: 15},
		{BucketStart: window.TodayStart.AddDate(0, 0, -2), Success: true, RequestCount: 1, SuccessCount: 1, PromptTokens: 40, CompletionTokens: 10, TotalTokens: 50},
	}

	kpi := requestsKPIFromSummaries(window, rows)
	if kpi.Today != 1 || kpi.TodayTokens != 100 {
		t.Fatalf("expected today request/tokens 1/100, got %d/%d", kpi.Today, kpi.TodayTokens)
	}
	if kpi.Yesterday != 1 || kpi.YesterdayTokens != 80 {
		t.Fatalf("expected yesterday request/tokens 1/80, got %d/%d", kpi.Yesterday, kpi.YesterdayTokens)
	}
	if kpi.SuccessRate == nil || *kpi.SuccessRate != 0.5 {
		t.Fatalf("expected today's success rate 0.5, got %#v", kpi.SuccessRate)
	}
	if kpi.Total != 3 || kpi.TotalTokens != 230 {
		t.Fatalf("expected total request/tokens 3/230, got %d/%d", kpi.Total, kpi.TotalTokens)
	}
	if kpi.TodayPromptTokens != 70 || kpi.TodayCompletionTokens != 30 || kpi.TodayCachedTokens != 20 {
		t.Fatalf("unexpected today token breakdown: %#v", kpi)
	}
	if kpi.TotalPromptTokens != 170 || kpi.TotalCompletionTokens != 60 || kpi.TotalCachedTokens != 35 {
		t.Fatalf("unexpected total token breakdown: %#v", kpi)
	}
}

func TestDailyModelSummariesAggregateCostAndSuccessfulRequests(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), timeZone)
	modelID := uuid.New()
	service := NewService(nil, timeZone)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.AddDate(0, 0, -1), CanonicalModelKey: "old", EstimatedCost: 99, Success: true, SuccessCount: 99},
		{BucketStart: window.RangeStart, CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CanonicalModelKey: "gpt-5", EstimatedCost: 1.25, Currency: "USD", Success: true, SuccessCount: 1},
		{BucketStart: window.RangeStart, CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CanonicalModelKey: "gpt-5", EstimatedCost: 0.75, Currency: "USD", Success: true, SuccessCount: 2},
		{BucketStart: window.RangeStart, CanonicalModelKey: summaryNoneKey, EstimatedCost: 3, Success: false, SuccessCount: 9},
	}

	costs, err := service.dailyModelCostFromSummaries(window, rows)
	if err != nil {
		t.Fatalf("daily model cost: %v", err)
	}
	if len(costs) != 2 {
		t.Fatalf("expected two model cost points, got %#v", costs)
	}
	if costs[0].ModelKey != "unknown" || costs[0].ModelID != nil || costs[0].Cost != 3 || costs[0].Currency != "USD" {
		t.Fatalf("expected unknown model cost first, got %#v", costs[0])
	}
	if costs[1].ModelKey != "gpt-5" || costs[1].ModelID == nil || *costs[1].ModelID != modelID.String() || costs[1].Cost != 2 {
		t.Fatalf("expected gpt-5 aggregate, got %#v", costs[1])
	}

	requests, err := service.dailyModelRequestsFromSummaries(window, rows)
	if err != nil {
		t.Fatalf("daily model requests: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected one successful request aggregate, got %#v", requests)
	}
	if requests[0].ModelKey != "gpt-5" || requests[0].RequestCount != 3 {
		t.Fatalf("expected successful gpt-5 request count, got %#v", requests[0])
	}
}

func TestDailyAPIKeyUsageFromSummariesAggregatesByDayAndKey(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, timeZone.Location), timeZone)
	apiKeyA := uuid.New()
	apiKeyB := uuid.New()
	items := dailyAPIKeyUsageFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.AddDate(0, 0, -1), APIKeyID: uuid.NullUUID{UUID: apiKeyA, Valid: true}, APIKeyName: "Old", TotalTokens: 900, EstimatedCost: 9, Currency: "USD"},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: apiKeyA, Valid: true}, APIKeyName: "Build Bot", TotalTokens: 100, EstimatedCost: 1, Currency: "USD"},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: apiKeyA, Valid: true}, APIKeyName: "Build Bot", TotalTokens: 40, EstimatedCost: 0.4, Currency: "USD"},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: apiKeyB, Valid: true}, APIKeyName: "QA", TotalTokens: 200, EstimatedCost: 2, Currency: "USD"},
		{BucketStart: window.RangeStart, APIKeyName: "Missing Key", TotalTokens: 500, EstimatedCost: 5, Currency: "USD"},
	}, nil)

	if len(items) != 2 {
		t.Fatalf("expected two api key usage points, got %#v", items)
	}
	if items[0].APIKeyID != apiKeyB.String() || items[0].TotalTokens != 200 || items[0].Cost != 2 {
		t.Fatalf("expected QA key to be first by tokens, got %#v", items[0])
	}
	if items[1].APIKeyID != apiKeyA.String() || items[1].APIKeyName != "Build Bot" || items[1].TotalTokens != 140 || items[1].Cost != 1.4 {
		t.Fatalf("expected Build Bot aggregate, got %#v", items[1])
	}
}

func TestDailyAPIKeyUsageFromSummariesSupportsActivityYearWindow(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(apiKeyActivityDays, time.Date(2026, 5, 10, 12, 0, 0, 0, timeZone.Location), timeZone)
	apiKeyID := uuid.New()
	items := dailyAPIKeyUsageFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.AddDate(0, 0, -1), APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "Too Old", TotalTokens: 900, EstimatedCost: 9, Currency: "USD"},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "Year Key", TotalTokens: 100, EstimatedCost: 1, Currency: "USD"},
		{BucketStart: window.TodayStart, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "Year Key", TotalTokens: 200, EstimatedCost: 2, Currency: "USD"},
	}, nil)

	if len(items) != 2 {
		t.Fatalf("expected two activity year points, got %#v", items)
	}
	if items[0].Date != summaryDate(window.RangeStart, window) || items[0].TotalTokens != 100 {
		t.Fatalf("expected first activity day at range start, got %#v", items[0])
	}
	if items[1].Date != summaryDate(window.TodayStart, window) || items[1].TotalTokens != 200 {
		t.Fatalf("expected second activity day today, got %#v", items[1])
	}
}

func TestDailyAPIKeyUsageFromSummariesUsesCurrentAPIKeyName(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, timeZone.Location), timeZone)
	apiKeyID := uuid.New()
	items := dailyAPIKeyUsageFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "Old Name", TotalTokens: 100, EstimatedCost: 1, Currency: "USD"},
	}, map[uuid.UUID]store.APIKey{
		apiKeyID: {ID: apiKeyID, Name: "New Name"},
	})

	if len(items) != 1 {
		t.Fatalf("expected one API key usage point, got %#v", items)
	}
	if items[0].APIKeyName != "New Name" {
		t.Fatalf("expected current API key name, got %#v", items[0])
	}
}

func TestDailyAPIKeyUsageFromSummariesFallsBackToSnapshotName(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, timeZone.Location), timeZone)
	apiKeyID := uuid.New()
	items := dailyAPIKeyUsageFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "Snapshot Name", TotalTokens: 100, EstimatedCost: 1, Currency: "USD"},
	}, map[uuid.UUID]store.APIKey{
		apiKeyID: {ID: apiKeyID, Name: " "},
	})

	if len(items) != 1 {
		t.Fatalf("expected one API key usage point, got %#v", items)
	}
	if items[0].APIKeyName != "Snapshot Name" {
		t.Fatalf("expected snapshot API key name fallback, got %#v", items[0])
	}
}

func TestFailureReasonsFromSummariesNormalizesSortsAndLimits(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(7, time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), timeZone)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.AddDate(0, 0, -1), Success: false, ErrorType: "old", FailureCount: 99},
		{BucketStart: window.RangeStart, Success: true, ErrorType: "ignored", FailureCount: 99},
		{BucketStart: window.RangeStart, Success: false, ErrorType: "", FailureCount: 7},
		{BucketStart: window.RangeStart, Success: false, ErrorType: summaryNoneKey, FailureCount: 5},
	}
	for index := 0; index < 11; index++ {
		rows = append(rows, store.RequestUsageDailySummary{
			BucketStart:   window.RangeStart,
			Success:       false,
			ErrorType:     "reason-" + string(rune('a'+index)),
			FailureCount:  int64(index + 1),
			RequestCount:  int64(index + 1),
			EstimatedCost: float64(index),
		})
	}

	items, err := NewService(nil, timeZone).failureReasonsFromSummaries(window, rows)
	if err != nil {
		t.Fatalf("failure reasons: %v", err)
	}
	if len(items) != failureReasonTopN {
		t.Fatalf("expected top %d failure reasons, got %#v", failureReasonTopN, items)
	}
	if items[0].Reason != "unknown" || items[0].RequestCount != 12 {
		t.Fatalf("expected normalized unknown reason first, got %#v", items[0])
	}
	if items[1].Reason != "reason-k" || items[1].RequestCount != 11 {
		t.Fatalf("expected reason-k second, got %#v", items[1])
	}
	if items[len(items)-1].Reason != "reason-c" {
		t.Fatalf("expected lower counts to be trimmed, got %#v", items)
	}
}

func TestDashboardUsageSummaryRowsUseHistorySummaryAndTodayDetails(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(90, time.Date(2026, 6, 6, 12, 30, 0, 0, timeZone.Location), timeZone)
	query := dashboardHistoricalSummaryQuery(window)
	if query.TimeZone != config.DefaultTimeZone {
		t.Fatalf("unexpected summary timezone: %s", query.TimeZone)
	}
	if query.From != nil {
		t.Fatalf("expected history summary query to have no lower bound, got %s", query.From)
	}
	if query.To == nil || !query.To.Equal(window.TodayStart) {
		t.Fatalf("expected history summary to stop at today start %s, got %#v", window.TodayStart, query.To)
	}

	start, end, detailTimeZone := dashboardTodayDetailRange(window)
	if !start.Equal(window.TodayStart) || !end.Equal(window.Now) {
		t.Fatalf("unexpected today detail range: %s - %s", start, end)
	}
	if detailTimeZone.Name != config.DefaultTimeZone || detailTimeZone.Location != timeZone.Location {
		t.Fatalf("unexpected detail timezone: %#v", detailTimeZone)
	}
}

func TestDashboardSiteActiveRequiresEnabledExistingSite(t *testing.T) {
	t.Parallel()

	activeID := uuid.New()
	disabledID := uuid.New()
	deletedID := uuid.New()
	sites := map[uuid.UUID]store.Site{
		activeID:   {ID: activeID, Status: "active", Enabled: true},
		disabledID: {ID: disabledID, Status: "active", Enabled: false},
		deletedID:  {ID: deletedID, Status: store.SiteStatusDeleted, Enabled: true},
	}

	if !dashboardSiteActive(uuid.NullUUID{UUID: activeID, Valid: true}, sites) {
		t.Fatal("expected active enabled site to be included")
	}
	if dashboardSiteActive(uuid.NullUUID{UUID: disabledID, Valid: true}, sites) {
		t.Fatal("expected disabled site to be excluded")
	}
	if dashboardSiteActive(uuid.NullUUID{UUID: deletedID, Valid: true}, sites) {
		t.Fatal("expected deleted site to be excluded")
	}
	if dashboardSiteActive(uuid.NullUUID{}, sites) {
		t.Fatal("expected unknown site to be excluded")
	}
}

func TestEpaperSummarySerializesTotalRequestAndTokenKPIs(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(EpaperSummary{
		KPIs: EpaperKPIs{
			TodayRequests: 3,
			TotalRequests: 10,
			TodayTokens:   300,
			TotalTokens:   1000,
		},
	})
	if err != nil {
		t.Fatalf("marshal epaper summary: %v", err)
	}
	var decoded struct {
		KPIs struct {
			TodayRequests int64 `json:"today_requests"`
			TotalRequests int64 `json:"total_requests"`
			TodayTokens   int64 `json:"today_tokens"`
			TotalTokens   int64 `json:"total_tokens"`
		} `json:"kpis"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal epaper summary: %v", err)
	}
	if decoded.KPIs.TodayRequests != 3 || decoded.KPIs.TotalRequests != 10 {
		t.Fatalf("expected request KPIs 3/10, got %d/%d", decoded.KPIs.TodayRequests, decoded.KPIs.TotalRequests)
	}
	if decoded.KPIs.TodayTokens != 300 || decoded.KPIs.TotalTokens != 1000 {
		t.Fatalf("expected token KPIs 300/1000, got %d/%d", decoded.KPIs.TodayTokens, decoded.KPIs.TotalTokens)
	}
}

func TestEpaperModelTop3TodayFromSummariesUsesTodayRows(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(1, time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC), timeZone)
	items := epaperModelTop3TodayFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.TodayStart, CanonicalModelKey: "gpt-5", EstimatedCost: 3},
		{BucketStart: window.TodayStart, CanonicalModelKey: "gpt-5", EstimatedCost: 2},
		{BucketStart: window.TodayStart, CanonicalModelKey: "claude", EstimatedCost: 6},
		{BucketStart: window.TodayStart, CanonicalModelKey: summaryNoneKey, UpstreamModelName: "upstream", EstimatedCost: 4},
		{BucketStart: window.TodayStart, CanonicalModelKey: "gemini", EstimatedCost: 1},
		{BucketStart: window.TodayStart, CanonicalModelKey: "zero", EstimatedCost: 0},
		{BucketStart: window.TodayStart.AddDate(0, 0, -1), CanonicalModelKey: "old", EstimatedCost: 100},
	})

	if len(items) != 3 {
		t.Fatalf("expected top 3 items, got %#v", items)
	}
	expected := []EpaperModelCostItem{
		{ModelKey: "claude", Cost: 6},
		{ModelKey: "gpt-5", Cost: 5},
		{ModelKey: "upstream", Cost: 4},
	}
	for index, item := range items {
		if item != expected[index] {
			t.Fatalf("expected item %d to be %#v, got %#v", index, expected[index], item)
		}
	}
}

func TestSummarizeEpaperCodexQuotaAveragesRemainingAndEarliestReset(t *testing.T) {
	t.Parallel()

	summary := summarizeEpaperCodexQuota([]map[string]any{
		{
			"five_hour": map[string]any{"remaining_percent": float64(60), "reset_at": float64(1780260121)},
			"weekly":    map[string]any{"remaining_percent": float64(94), "reset_at": float64(1780846921)},
		},
		{
			"five_hour": map[string]any{"remaining_percent": float64(80), "reset_at": float64(1780260999)},
			"weekly":    map[string]any{"remaining_percent": float64(90), "reset_at": float64(1780840000)},
		},
	})

	if summary.AccountCount != 2 {
		t.Fatalf("expected two counted accounts, got %d", summary.AccountCount)
	}
	if summary.FiveHour.RemainingPercent == nil || *summary.FiveHour.RemainingPercent != 70 {
		t.Fatalf("expected five-hour remaining average 70, got %#v", summary.FiveHour.RemainingPercent)
	}
	if summary.FiveHour.ResetAt == nil || *summary.FiveHour.ResetAt != 1780260121 {
		t.Fatalf("expected earliest five-hour reset, got %#v", summary.FiveHour.ResetAt)
	}
	if summary.Weekly.RemainingPercent == nil || *summary.Weekly.RemainingPercent != 92 {
		t.Fatalf("expected weekly remaining average 92, got %#v", summary.Weekly.RemainingPercent)
	}
	if summary.Weekly.ResetAt == nil || *summary.Weekly.ResetAt != 1780840000 {
		t.Fatalf("expected earliest weekly reset, got %#v", summary.Weekly.ResetAt)
	}
}

func TestSummarizeEpaperCodexQuotaSkipsAccountsWithoutWindowData(t *testing.T) {
	t.Parallel()

	summary := summarizeEpaperCodexQuota([]map[string]any{
		{"available": false, "message": "quota unavailable"},
		{"five_hour": map[string]any{"reset_at": float64(1780260121)}},
	})

	if summary.AccountCount != 0 {
		t.Fatalf("expected no counted accounts, got %d", summary.AccountCount)
	}
	if summary.FiveHour.RemainingPercent != nil {
		t.Fatalf("expected missing five-hour remaining to stay null, got %#v", summary.FiveHour.RemainingPercent)
	}
	if summary.Weekly.ResetAt != nil {
		t.Fatalf("expected missing weekly reset to stay null, got %#v", summary.Weekly.ResetAt)
	}
}

func TestEpaperCodexQuotaPayloadReadsUserQuota(t *testing.T) {
	t.Parallel()

	payload := epaperCodexQuotaPayload(store.JSON(`{"user":{"quota":{"five_hour":{"remaining_percent":42}}}}`))
	window, _ := payload["five_hour"].(map[string]any)
	if window["remaining_percent"] != float64(42) {
		t.Fatalf("expected nested user quota payload, got %#v", payload)
	}
}

func TestUptimeBucketPayloadFormatsStatusAndSuccessRate(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(1, time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), timeZone)
	payload := uptimeBucket{
		hour:         window.TodayStart.Add(10 * time.Hour),
		successCount: 3,
		failureCount: 1,
		totalCount:   4,
	}.payload(window)

	if payload.Hour != "2026-05-10T10:00:00Z" || payload.Status != "degraded" {
		t.Fatalf("unexpected uptime payload: %#v", payload)
	}
	if payload.SuccessRate == nil || *payload.SuccessRate != 0.75 {
		t.Fatalf("expected success rate 0.75, got %#v", payload.SuccessRate)
	}
}

func TestBucketStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		success  int64
		failure  int64
		expected string
	}{
		{name: "idle", expected: "idle"},
		{name: "healthy", success: 2, expected: "healthy"},
		{name: "unhealthy", failure: 2, expected: "unhealthy"},
		{name: "degraded", success: 1, failure: 1, expected: "degraded"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := BucketStatus(tc.success, tc.failure); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestResolveSummarySiteSnapshotPrefersLiveSiteAndFallsBackToSnapshots(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	live := store.Site{ID: siteID, Name: "Live Site", Slug: "live", SiteType: "openai"}
	snapshot := resolveSummarySiteSnapshot(store.RequestUsageDailySummary{
		SiteID:   uuid.NullUUID{UUID: siteID, Valid: true},
		SiteName: "Stale Name",
		SiteSlug: "stale",
		SiteType: "codex",
	}, map[uuid.UUID]store.Site{siteID: live})
	if snapshot.siteName != "Live Site" || snapshot.siteSlug != "live" || snapshot.aggregateKey != "site:"+siteID.String() {
		t.Fatalf("expected live site snapshot, got %#v", snapshot)
	}

	missingID := uuid.New()
	snapshot = resolveSummarySiteSnapshot(store.RequestUsageDailySummary{
		SiteID:   uuid.NullUUID{UUID: missingID, Valid: true},
		SiteName: "Historical Site",
		SiteSlug: "historical",
		SiteType: "anthropic",
	}, map[uuid.UUID]store.Site{})
	if snapshot.siteName != "Historical Site" || snapshot.aggregateKey != "site:"+missingID.String() {
		t.Fatalf("expected historical site id snapshot, got %#v", snapshot)
	}

	snapshot = resolveSummarySiteSnapshot(store.RequestUsageDailySummary{SiteSlug: "slug-only", SiteType: "google"}, nil)
	if snapshot.siteKey != "slug-only" || snapshot.aggregateKey != "snapshot-slug:slug-only" {
		t.Fatalf("expected slug snapshot, got %#v", snapshot)
	}

	snapshot = resolveSummarySiteSnapshot(store.RequestUsageDailySummary{SiteKey: "legacy-key"}, nil)
	if snapshot.siteKey != "legacy-key" || snapshot.aggregateKey != "legacy-key" {
		t.Fatalf("expected legacy site key snapshot, got %#v", snapshot)
	}

	snapshot = resolveSummarySiteSnapshot(store.RequestUsageDailySummary{SiteKey: summaryNoneKey}, nil)
	if snapshot.siteKey != "unknown" || snapshot.aggregateKey != "unknown" {
		t.Fatalf("expected unknown snapshot, got %#v", snapshot)
	}
}

func TestFilterDashboardUptimeSitesExcludesDisabledAndDeleted(t *testing.T) {
	t.Parallel()

	enabledID := uuid.New()
	disabledID := uuid.New()
	deletedID := uuid.New()
	sites := []store.Site{
		{ID: enabledID, Status: "active", Enabled: true},
		{ID: disabledID, Status: "active", Enabled: false},
		{ID: deletedID, Status: store.SiteStatusDeleted, Enabled: true},
	}

	filtered := filterDashboardUptimeSites(sites)
	if len(filtered) != 1 || filtered[0].ID != enabledID {
		t.Fatalf("expected only enabled active site, got %#v", filtered)
	}
}

func TestRateLimitAttentionReportsRateLimitFailures(t *testing.T) {
	t.Parallel()

	if item := rateLimitAttention(OverviewWindow{}, "2026-05-10T00:00:00Z"); item != nil {
		t.Fatalf("expected no rate limit attention, got %#v", item)
	}

	item := rateLimitAttention(OverviewWindow{
		FailureReasons: []FailureReasonItem{
			{Reason: "timeout", RequestCount: 3},
			{Reason: "rate_limit_exceeded", RequestCount: 5},
		},
	}, "2026-05-10T00:00:00Z")
	if item == nil {
		t.Fatal("expected rate limit attention item")
	}
	if item.ID != "gateway:rate_limit_exceeded" || item.Action.Params["error_type"] != "rate_limit_exceeded" {
		t.Fatalf("unexpected rate limit attention: %#v", item)
	}
	if item.Metrics["request_count"] != int64(5) {
		t.Fatalf("unexpected rate limit count: %#v", item.Metrics)
	}
}

func TestLatencyAttentionUsesThirtySecondP95Threshold(t *testing.T) {
	t.Parallel()

	items := latencyAttention([]HighLatencyItem{
		{SiteName: "Fast", ModelKey: "gpt-5.4", RequestCount: 3, P95LatencyMS: highLatencyP95MS - 1},
		{SiteName: "Slow", ModelKey: "gpt-5.5", RequestCount: 3, P95LatencyMS: highLatencyP95MS},
	}, "2026-05-10T00:00:00+08:00")

	if len(items) != 1 {
		t.Fatalf("expected one latency attention item, got %d", len(items))
	}
	if items[0].Subject["model_key"] != "gpt-5.5" {
		t.Fatalf("expected gpt-5.5 attention item, got %#v", items[0].Subject)
	}
	if items[0].Action.Type != "open_requests" {
		t.Fatalf("expected open_requests action, got %#v", items[0].Action)
	}
}

func TestLatencyAttentionEscalatesVeryHighP95(t *testing.T) {
	t.Parallel()

	items := latencyAttention([]HighLatencyItem{
		{SiteName: "Very Slow", ModelKey: "gpt-5.6", RequestCount: 3, P95LatencyMS: highLatencyCriticalP95MS},
	}, "2026-05-10T00:00:00+08:00")

	if len(items) != 1 {
		t.Fatalf("expected one latency attention item, got %d", len(items))
	}
	if items[0].Severity != "critical" {
		t.Fatalf("expected critical latency attention, got %q", items[0].Severity)
	}
}

func TestFailureRateAttentionEscalatesVeryLowSuccessRate(t *testing.T) {
	t.Parallel()

	successRate := 0.4
	siteID := uuid.New()
	items := failureRateAttention(OverviewWindow{
		SiteCostSummary: []SiteCostSummaryItem{
			{SiteID: siteID.String(), SiteName: "Site", RequestCount: failureRateCriticalRequestN, SuccessCount: 4, SuccessRate: &successRate},
		},
	}, "2026-05-10T00:00:00+08:00", map[uuid.UUID]store.Site{
		siteID: {ID: siteID, Status: "active", Enabled: true},
	})

	if len(items) != 1 {
		t.Fatalf("expected one failure rate attention item, got %d", len(items))
	}
	if items[0].Severity != "critical" {
		t.Fatalf("expected critical failure rate attention, got %q", items[0].Severity)
	}
}

func TestFailureRateAttentionSkipsUnknownAndDeletedSites(t *testing.T) {
	t.Parallel()

	successRate := 0.4
	deletedID := uuid.New()
	items := failureRateAttention(OverviewWindow{
		SiteCostSummary: []SiteCostSummaryItem{
			{SiteID: "unknown", SiteName: "", RequestCount: failureRateCriticalRequestN, SuccessCount: 0, SuccessRate: &successRate},
			{SiteID: deletedID.String(), SiteName: "Deleted", RequestCount: failureRateCriticalRequestN, SuccessCount: 0, SuccessRate: &successRate},
		},
	}, "2026-05-10T00:00:00+08:00", map[uuid.UUID]store.Site{})

	if len(items) != 0 {
		t.Fatalf("expected unknown and deleted sites to be skipped, got %#v", items)
	}
}

func TestRouteCandidateAttentionSkipsNoRouteWithoutRecentRequests(t *testing.T) {
	t.Parallel()

	items := routeCandidateAttentionItems([]InsufficientCandidateItem{
		{ModelKey: "gpt-idle", SiteModelCount: 1, EligibleCount: 0},
	}, "2026-05-10T00:00:00+08:00")
	if len(items) != 0 {
		t.Fatalf("expected no idle route attention items, got %#v", items)
	}
}

func TestRouteCandidateAttentionReportsNoRouteWithRecentRequests(t *testing.T) {
	t.Parallel()

	items := routeCandidateAttentionItems([]InsufficientCandidateItem{
		{CanonicalModelID: "model-1", ModelKey: "gpt-active", SiteModelCount: 1, EligibleCount: 0, RequestCount24h: 3},
	}, "2026-05-10T00:00:00+08:00")
	if len(items) != 1 {
		t.Fatalf("expected one route attention item, got %d", len(items))
	}
	if items[0].Type != "no_route_candidates" || items[0].Severity != "critical" {
		t.Fatalf("expected critical no_route_candidates, got type=%q severity=%q", items[0].Type, items[0].Severity)
	}
}

func TestDashboardScalarHelpersConvertNullableValues(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	checkedAt := sql.NullTime{Time: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC), Valid: true}
	if got := nullTimeString(checkedAt, timeZone); got == nil || *got != "2026-05-10T12:00:00Z" {
		t.Fatalf("unexpected null time string: %#v", got)
	}
	if got := nullTimeString(sql.NullTime{}, timeZone); got != nil {
		t.Fatalf("expected invalid time to be nil, got %#v", got)
	}

	if nullFloat(sql.NullFloat64{Float64: 1.5, Valid: true}) != 1.5 || nullFloat(sql.NullFloat64{}) != 0 {
		t.Fatal("unexpected nullable float conversion")
	}
	if nullInt(sql.NullInt64{Int64: 42, Valid: true}) != 42 || nullInt(sql.NullInt64{}) != 0 {
		t.Fatal("unexpected nullable int conversion")
	}
	if got := nullIntPtr(sql.NullInt64{Int64: 7, Valid: true}); got == nil || *got != 7 {
		t.Fatalf("unexpected nullable int pointer: %#v", got)
	}
	if got := nullIntPtr(sql.NullInt64{}); got != nil {
		t.Fatalf("expected invalid int pointer to be nil, got %#v", got)
	}

	id := uuid.New()
	if got := nullableUUIDString(uuid.NullUUID{UUID: id, Valid: true}); got == nil || *got != id.String() {
		t.Fatalf("unexpected nullable uuid string: %#v", got)
	}
	if got := nullableUUIDString(uuid.NullUUID{UUID: uuid.Nil, Valid: true}); got != nil {
		t.Fatalf("expected nil uuid string, got %#v", got)
	}
	if got := nullableUUIDKey(uuid.NullUUID{UUID: id, Valid: true}); got != id.String() {
		t.Fatalf("unexpected nullable uuid key: %s", got)
	}
	if got := nullableUUIDKey(uuid.NullUUID{}); got != "" {
		t.Fatalf("expected empty nullable uuid key, got %q", got)
	}
	if got := nullableStringPtr(sql.NullString{String: "value", Valid: true}); got == nil || *got != "value" {
		t.Fatalf("unexpected nullable string pointer: %#v", got)
	}
	if got := nullableStringPtr(sql.NullString{String: "", Valid: true}); got != nil {
		t.Fatalf("expected empty nullable string pointer to be nil, got %#v", got)
	}
	if got := dashboardSiteIDString(uuid.NullUUID{UUID: id, Valid: true}, "fallback"); got != id.String() {
		t.Fatalf("unexpected dashboard site id: %s", got)
	}
	if got := dashboardSiteIDString(uuid.NullUUID{}, "fallback"); got != "fallback" {
		t.Fatalf("expected fallback site id, got %s", got)
	}
}

func TestPricingHelpersRequireAvailableValues(t *testing.T) {
	t.Parallel()

	if hasAvailablePricing([]store.SiteModelPricing{{Available: false, BillingType: "tokens", InputValue: sql.NullFloat64{Float64: 1, Valid: true}}}) {
		t.Fatal("expected unavailable pricing to be ignored")
	}
	if hasAvailablePricing([]store.SiteModelPricing{{Available: true, BillingType: "tokens"}}) {
		t.Fatal("expected token pricing without values to be missing")
	}
	if !hasAvailablePricing([]store.SiteModelPricing{{Available: true, BillingType: "tokens", InputValue: sql.NullFloat64{Float64: 1, Valid: true}}}) {
		t.Fatal("expected token input pricing to be available")
	}
	if !hasAvailablePricing([]store.SiteModelPricing{{Available: true, BillingType: "per_request", PerRequestValue: sql.NullFloat64{Float64: 0.01, Valid: true}}}) {
		t.Fatal("expected per-request pricing to be available")
	}

	if !isNewAPISite("newapi") || !isNewAPISite("new_api") || isNewAPISite("openai") {
		t.Fatal("unexpected newapi site type classification")
	}
}

func TestJSONAndQuotaScalarHelpersParseDashboardPayloads(t *testing.T) {
	t.Parallel()

	if got := jsonMap(store.JSON(`{`)); len(got) != 0 {
		t.Fatalf("expected invalid JSON to return empty map, got %#v", got)
	}
	payload := epaperCodexQuotaPayload(store.JSON(`{"quota":{"weekly":{"remaining_percent":"91"}}}`))
	weekly, _ := payload["weekly"].(map[string]any)
	if weekly["remaining_percent"] != "91" {
		t.Fatalf("expected top-level quota payload, got %#v", payload)
	}

	if got := intPtrFromAny(int64(41)); got == nil || *got != 41 {
		t.Fatalf("unexpected int64 conversion: %#v", got)
	}
	if got := intPtrFromAny(float64(41.6)); got == nil || *got != 42 {
		t.Fatalf("unexpected rounded float conversion: %#v", got)
	}
	if got := intPtrFromAny(json.Number("43")); got == nil || *got != 43 {
		t.Fatalf("unexpected json number int conversion: %#v", got)
	}
	if got := intPtrFromAny("44.4"); got == nil || *got != 44 {
		t.Fatalf("unexpected string number conversion: %#v", got)
	}
	if got := intPtrFromAny(" "); got != nil {
		t.Fatalf("expected blank int to be nil, got %#v", got)
	}

	resetAt := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	if got := unixSecondsPtrFromAny(resetAt.Format(time.RFC3339)); got == nil || *got != resetAt.Unix() {
		t.Fatalf("unexpected RFC3339 unix conversion: %#v", got)
	}
	if got := unixSecondsPtrFromAny(json.Number("123.4")); got == nil || *got != 123 {
		t.Fatalf("unexpected json number unix conversion: %#v", got)
	}
	if got := unixSecondsPtrFromAny("bad"); got != nil {
		t.Fatalf("expected bad unix value to be nil, got %#v", got)
	}
}
