package analytics

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func testTimeZone() config.TimeZone {
	return config.LoadTimeZone("UTC")
}

func testDay(timeZone config.TimeZone, year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, timeZone.Location)
}

func TestUsageRejectsNilStore(t *testing.T) {
	t.Parallel()

	var service *Service
	if _, err := service.Usage(context.Background(), UsageParams{}); err == nil || !strings.Contains(err.Error(), "analytics store is not initialized") {
		t.Fatalf("expected nil store error, got %v", err)
	}
}

func TestResolveRangeDefaultsToLastThirtyDays(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	todayStart := testDay(timeZone, 2026, 8, 13)
	from, to, days := resolveRange(timeZone, todayStart, time.Time{}, time.Time{})

	if got := from.Format("2006-01-02"); got != "2026-07-15" {
		t.Fatalf("unexpected default from: %s", got)
	}
	if got := to.Format("2006-01-02"); got != "2026-08-13" {
		t.Fatalf("unexpected default to: %s", got)
	}
	if days != 30 {
		t.Fatalf("unexpected days: %d", days)
	}
}

func TestResolveRangeClampsToMaxDays(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	todayStart := testDay(timeZone, 2026, 8, 13)
	from, to, days := resolveRange(timeZone, todayStart, testDay(timeZone, 2000, 1, 1), todayStart)

	if days != maxRangeDays {
		t.Fatalf("expected clamp to %d days, got %d", maxRangeDays, days)
	}
	expectedFrom := todayStart.AddDate(0, 0, -(maxRangeDays - 1))
	if !from.Equal(expectedFrom) {
		t.Fatalf("unexpected clamped from: %s, want %s", from, expectedFrom)
	}
	if !to.Equal(todayStart) {
		t.Fatalf("unexpected to: %s", to)
	}
}

func TestResolveRangeClampsFutureToAndFrom(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	todayStart := testDay(timeZone, 2026, 8, 13)
	from, to, days := resolveRange(timeZone, todayStart, testDay(timeZone, 2026, 9, 1), testDay(timeZone, 2026, 9, 2))

	if !to.Equal(todayStart) {
		t.Fatalf("expected future to clamped to today, got %s", to)
	}
	if !from.Equal(todayStart) {
		t.Fatalf("expected from clamped to to, got %s", from)
	}
	if days != 1 {
		t.Fatalf("unexpected days: %d", days)
	}
}

func TestCurrencyOverviewAndPickDisplayCurrency(t *testing.T) {
	t.Parallel()

	rows := []store.RequestUsageDailySummary{
		{Currency: "USD", EstimatedCost: 2},
		{Currency: "CNY", EstimatedCost: 5},
		{Currency: "USD", EstimatedCost: 1},
		{Currency: "", EstimatedCost: 4},
	}
	available, costByCurrency := currencyOverview(rows)

	if len(available) != 2 || available[0] != "CNY" || available[1] != "USD" {
		t.Fatalf("unexpected available currencies: %v", available)
	}
	if costByCurrency["USD"] != 7 || costByCurrency["CNY"] != 5 {
		t.Fatalf("unexpected cost by currency: %v", costByCurrency)
	}
	if got := pickDisplayCurrency("", available, costByCurrency); got != "USD" {
		t.Fatalf("unexpected display currency: %s", got)
	}
	if got := pickDisplayCurrency("cny", available, costByCurrency); got != "CNY" {
		t.Fatalf("unexpected requested display currency: %s", got)
	}
	if got := pickDisplayCurrency("", nil, map[string]float64{}); got != defaultCurrency {
		t.Fatalf("unexpected fallback display currency: %s", got)
	}
}

func TestPickDisplayCurrencyTieBreaksAlphabetically(t *testing.T) {
	t.Parallel()

	available, costByCurrency := currencyOverview([]store.RequestUsageDailySummary{
		{Currency: "USD", EstimatedCost: 3},
		{Currency: "CNY", EstimatedCost: 3},
	})
	if got := pickDisplayCurrency("", available, costByCurrency); got != "CNY" {
		t.Fatalf("unexpected tie-break currency: %s", got)
	}
}

func TestBuildTotalsMultiCurrencyAndRates(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	rows := []store.RequestUsageDailySummary{
		{Currency: "USD", RequestCount: 8, SuccessCount: 6, FailureCount: 2, PromptTokens: 100, CompletionTokens: 50, CachedTokens: 25, TotalTokens: 150, EstimatedCost: 2, LatencyCount: 2, LatencyTotalMS: 300, LatencyMaxMS: sql.NullInt64{Int64: 200, Valid: true}, UpstreamLatencyCount: 2, UpstreamLatencyTotalMS: 240},
		{Currency: "USD", RequestCount: 2, SuccessCount: 2, PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150, EstimatedCost: 1, LatencyCount: 1, LatencyTotalMS: 900, LatencyMaxMS: sql.NullInt64{Int64: 900, Valid: true}},
		{Currency: "CNY", RequestCount: 5, SuccessCount: 5, PromptTokens: 500, CompletionTokens: 500, CachedTokens: 100, TotalTokens: 1000, EstimatedCost: 9},
	}
	_, costByCurrency := currencyOverview(rows)
	filtered := filterRowsByCurrency(rows, "USD")

	previousRows := []store.RequestUsageDailySummary{
		{Currency: "USD", RequestCount: 4, SuccessCount: 3, FailureCount: 1, TotalTokens: 40, EstimatedCost: 0.5, LatencyCount: 2, LatencyTotalMS: 100},
	}
	prevFrom := testDay(timeZone, 2026, 7, 1)
	prevTo := testDay(timeZone, 2026, 7, 2)
	totals := buildTotals(filtered, previousRows, costByCurrency, timeZone, prevFrom, prevTo)

	if totals.Requests != 10 || totals.SuccessCount != 8 || totals.FailureCount != 2 {
		t.Fatalf("unexpected request counts: %#v", totals)
	}
	if totals.SuccessRate == nil || *totals.SuccessRate != 0.8 {
		t.Fatalf("unexpected success rate: %v", totals.SuccessRate)
	}
	if totals.TotalTokens != 300 || totals.PromptTokens != 200 || totals.CompletionTokens != 100 {
		t.Fatalf("unexpected token totals: %#v", totals)
	}
	if totals.CacheHitRate == nil || *totals.CacheHitRate != 0.125 {
		t.Fatalf("unexpected cache hit rate: %v", totals.CacheHitRate)
	}
	if totals.Cost != 3 {
		t.Fatalf("unexpected cost: %v", totals.Cost)
	}
	if totals.CostByCurrency["CNY"] != 9 || totals.CostByCurrency["USD"] != 3 {
		t.Fatalf("unexpected cost by currency: %v", totals.CostByCurrency)
	}
	if totals.AvgLatencyMS != 400 {
		t.Fatalf("unexpected avg latency: %v", totals.AvgLatencyMS)
	}
	if totals.MaxLatencyMS != 900 {
		t.Fatalf("unexpected max latency: %v", totals.MaxLatencyMS)
	}
	if totals.AvgUpstreamLatencyMS != 120 {
		t.Fatalf("unexpected avg upstream latency: %v", totals.AvgUpstreamLatencyMS)
	}
	previous := totals.PreviousPeriod
	if previous.From != "2026-07-01" || previous.To != "2026-07-02" {
		t.Fatalf("unexpected previous range: %#v", previous)
	}
	if previous.Requests != 4 || previous.TotalTokens != 40 || previous.Cost != 0.5 || previous.AvgLatencyMS != 50 {
		t.Fatalf("unexpected previous totals: %#v", previous)
	}
	if previous.SuccessRate == nil || *previous.SuccessRate != 0.75 {
		t.Fatalf("unexpected previous success rate: %v", previous.SuccessRate)
	}
}

func TestBuildTotalsNullRatesWhenEmpty(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	totals := buildTotals(nil, nil, map[string]float64{}, timeZone, testDay(timeZone, 2026, 7, 1), testDay(timeZone, 2026, 7, 2))

	if totals.SuccessRate != nil {
		t.Fatalf("expected nil success rate, got %v", *totals.SuccessRate)
	}
	if totals.CacheHitRate != nil {
		t.Fatalf("expected nil cache hit rate, got %v", *totals.CacheHitRate)
	}
	if totals.PreviousPeriod.SuccessRate != nil {
		t.Fatalf("expected nil previous success rate, got %v", *totals.PreviousPeriod.SuccessRate)
	}
}

func TestBuildBreakdownsSortsAndLimits(t *testing.T) {
	t.Parallel()

	rows := make([]store.RequestUsageDailySummary, 0, 25)
	for index := 0; index < 25; index++ {
		siteID := uuid.New()
		rows = append(rows, store.RequestUsageDailySummary{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			SiteName:      "site-" + string(rune('A'+index)),
			RequestCount:  int64(index) + 1,
			SuccessCount:  int64(index) + 1,
			EstimatedCost: float64(100 - index),
		})
	}
	breakdowns := buildBreakdowns(rows, map[uuid.UUID]store.APIKey{})

	if len(breakdowns.Site) != breakdownLimit {
		t.Fatalf("expected %d site items, got %d", breakdownLimit, len(breakdowns.Site))
	}
	if breakdowns.Site[0].Label != "site-A" || breakdowns.Site[0].Cost != 100 {
		t.Fatalf("unexpected first site item: %#v", breakdowns.Site[0])
	}
	if breakdowns.Site[0].ID == nil || *breakdowns.Site[0].ID == "" {
		t.Fatalf("expected site id, got %#v", breakdowns.Site[0].ID)
	}
	if breakdowns.Site[0].SuccessRate == nil || *breakdowns.Site[0].SuccessRate != 1 {
		t.Fatalf("unexpected site success rate: %v", breakdowns.Site[0].SuccessRate)
	}
}

func TestBuildBreakdownsFallsBackToUnknownAndLiveAPIKeyName(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	liveName := "live-name"
	rows := []store.RequestUsageDailySummary{
		{RequestCount: 1, SuccessCount: 1},
		{APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "snapshot-name", RequestCount: 2, SuccessCount: 1, FailureCount: 1, EstimatedCost: 1},
	}
	breakdowns := buildBreakdowns(rows, map[uuid.UUID]store.APIKey{
		apiKeyID: {ID: apiKeyID, Name: liveName},
	})

	if len(breakdowns.Site) != 1 || breakdowns.Site[0].Key != unknownLabel || breakdowns.Site[0].Label != unknownLabel {
		t.Fatalf("unexpected site fallback: %#v", breakdowns.Site)
	}
	if len(breakdowns.Model) != 1 || breakdowns.Model[0].Key != unknownLabel {
		t.Fatalf("unexpected model fallback: %#v", breakdowns.Model)
	}
	if len(breakdowns.APIKey) != 2 {
		t.Fatalf("unexpected api key items: %#v", breakdowns.APIKey)
	}
	var named *BreakdownItem
	for index := range breakdowns.APIKey {
		if breakdowns.APIKey[index].ID != nil {
			named = &breakdowns.APIKey[index]
		}
	}
	if named == nil || named.Label != liveName {
		t.Fatalf("expected live api key name, got %#v", breakdowns.APIKey)
	}
}

func TestBuildMatrixKeepsTopSitesAndModels(t *testing.T) {
	t.Parallel()

	rows := make([]store.RequestUsageDailySummary, 0)
	for siteIndex := 0; siteIndex < 12; siteIndex++ {
		for modelIndex := 0; modelIndex < 12; modelIndex++ {
			siteID := uuid.New()
			rows = append(rows, store.RequestUsageDailySummary{
				SiteID:            uuid.NullUUID{UUID: siteID, Valid: true},
				SiteName:          "site",
				CanonicalModelKey: "model-" + string(rune('a'+modelIndex)),
				RequestCount:      1,
				TotalTokens:       10,
				EstimatedCost:     float64(siteIndex * modelIndex),
			})
		}
	}
	breakdowns := buildBreakdowns(rows, map[uuid.UUID]store.APIKey{})

	seenSite := map[string]bool{}
	seenModel := map[string]bool{}
	for _, item := range breakdowns.Matrix {
		seenSite[item.Site.Key] = true
		seenModel[item.Model.Key] = true
	}
	if len(seenSite) > matrixTopN || len(seenModel) > matrixTopN {
		t.Fatalf("matrix exceeds top limits: sites=%d models=%d", len(seenSite), len(seenModel))
	}
	if len(breakdowns.Matrix) == 0 {
		t.Fatal("expected matrix items")
	}
	for index := 1; index < len(breakdowns.Matrix); index++ {
		if breakdowns.Matrix[index-1].Cost < breakdowns.Matrix[index].Cost {
			t.Fatalf("matrix not sorted by cost desc at %d", index)
		}
	}
}

func TestBuildSeriesGroupsByModelWithTopNineAndOther(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	rows := make([]store.RequestUsageDailySummary, 0)
	for index := 0; index < 11; index++ {
		rows = append(rows, store.RequestUsageDailySummary{
			BucketStart:       testDay(timeZone, 2026, 8, 1),
			CanonicalModelKey: "model-" + string(rune('a'+index)),
			RequestCount:      1,
			SuccessCount:      1,
			TotalTokens:       10,
			EstimatedCost:     float64(11 - index),
		})
		rows = append(rows, store.RequestUsageDailySummary{
			BucketStart:       testDay(timeZone, 2026, 8, 2),
			CanonicalModelKey: "model-" + string(rune('a'+index)),
			RequestCount:      2,
			SuccessCount:      2,
			TotalTokens:       20,
			EstimatedCost:     1,
		})
	}
	series := buildSeries(rows, GroupByModel, map[uuid.UUID]store.APIKey{}, timeZone, "day")

	if len(series) != seriesTopN+1 {
		t.Fatalf("expected %d series, got %d", seriesTopN+1, len(series))
	}
	if series[0].Key != "model-a" || series[0].Label != "model-a" {
		t.Fatalf("unexpected top series: %#v", series[0])
	}
	other := series[len(series)-1]
	if other.Key != otherSeriesKey || other.Label != otherSeriesLabel {
		t.Fatalf("unexpected other series: %#v", other)
	}
	if len(other.Points) != 2 {
		t.Fatalf("unexpected other points: %#v", other.Points)
	}
	if other.Points[0].Date != "2026-08-01" || other.Points[1].Date != "2026-08-02" {
		t.Fatalf("other points not sorted by date: %#v", other.Points)
	}
	if other.Points[0].Requests != 2 || other.Points[1].Requests != 4 {
		t.Fatalf("unexpected other requests: %#v", other.Points)
	}
	if len(series[0].Points) != 2 || series[0].Points[0].Date != "2026-08-01" {
		t.Fatalf("unexpected series points: %#v", series[0].Points)
	}
}

func TestBuildSeriesNoneReturnsSingleTotal(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	rows := []store.RequestUsageDailySummary{
		{BucketStart: testDay(timeZone, 2026, 8, 2), CanonicalModelKey: "b", RequestCount: 1, SuccessCount: 1, LatencyCount: 1, LatencyTotalMS: 100, LatencyMaxMS: sql.NullInt64{Int64: 100, Valid: true}},
		{BucketStart: testDay(timeZone, 2026, 8, 1), CanonicalModelKey: "a", RequestCount: 3, SuccessCount: 2, FailureCount: 1},
	}
	series := buildSeries(rows, GroupByNone, map[uuid.UUID]store.APIKey{}, timeZone, "day")

	if len(series) != 1 {
		t.Fatalf("expected single series, got %d", len(series))
	}
	if series[0].Key != totalSeriesKey || series[0].Label != totalSeriesLabel || series[0].ID != nil {
		t.Fatalf("unexpected total series: %#v", series[0])
	}
	if len(series[0].Points) != 2 || series[0].Points[0].Date != "2026-08-01" || series[0].Points[1].Date != "2026-08-02" {
		t.Fatalf("unexpected total points: %#v", series[0].Points)
	}
	if series[0].Points[1].AvgLatencyMS != 100 || series[0].Points[1].MaxLatencyMS != 100 {
		t.Fatalf("unexpected latency in point: %#v", series[0].Points[1])
	}
}

func TestBuildSeriesByErrorTypeNormalizesNone(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	rows := []store.RequestUsageDailySummary{
		{BucketStart: testDay(timeZone, 2026, 8, 1), ErrorType: summaryNoneKey, Success: true, RequestCount: 1, SuccessCount: 1, EstimatedCost: 1},
		{BucketStart: testDay(timeZone, 2026, 8, 1), ErrorType: "rate_limit_exceeded", RequestCount: 2, FailureCount: 2, EstimatedCost: 2},
	}
	series := buildSeries(rows, GroupByErrorType, map[uuid.UUID]store.APIKey{}, timeZone, "day")

	if len(series) != 2 {
		t.Fatalf("expected 2 series, got %#v", series)
	}
	if series[0].Key != "rate_limit_exceeded" {
		t.Fatalf("unexpected first series: %#v", series[0])
	}
	if series[1].Key != unknownLabel || series[1].Label != unknownLabel {
		t.Fatalf("unexpected normalized series: %#v", series[1])
	}
}

func TestMatchesUsageFilters(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()
	success := true
	params := UsageParams{
		SiteIDs:   []uuid.UUID{siteID},
		APIKeyIDs: []uuid.UUID{apiKeyID},
		ModelKeys: []string{"gpt-5"},
		Success:   &success,
	}

	matching := store.RequestUsageDailySummary{
		SiteID:            uuid.NullUUID{UUID: siteID, Valid: true},
		APIKeyID:          uuid.NullUUID{UUID: apiKeyID, Valid: true},
		CanonicalModelKey: "gpt-5",
		Success:           true,
	}
	if !matchesUsageFilters(matching, params) {
		t.Fatal("expected row to match filters")
	}
	cases := []store.RequestUsageDailySummary{
		{APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, CanonicalModelKey: "gpt-5", Success: true},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, CanonicalModelKey: "gpt-5", Success: true},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, CanonicalModelKey: "gpt-4", Success: true},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, CanonicalModelKey: "gpt-5", Success: false},
	}
	for index, row := range cases {
		if matchesUsageFilters(row, params) {
			t.Fatalf("expected case %d to be filtered out", index)
		}
	}
	if !matchesUsageFilters(store.RequestUsageDailySummary{}, UsageParams{}) {
		t.Fatal("expected empty filters to match any row")
	}
}

func TestPreviousRange(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	from, to := previousRange(testDay(timeZone, 2026, 7, 15), 30)
	if got := from.Format("2006-01-02"); got != "2026-06-15" {
		t.Fatalf("unexpected previous from: %s", got)
	}
	if got := to.Format("2006-01-02"); got != "2026-07-14" {
		t.Fatalf("unexpected previous to: %s", got)
	}
}

func TestUsageGranularityDay(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	rows := []store.RequestUsageDailySummary{
		{BucketStart: testDay(timeZone, 2026, 8, 1), RequestCount: 1},
		{BucketStart: testDay(timeZone, 2026, 8, 2), RequestCount: 2},
	}
	series := buildSeries(rows, GroupByNone, map[uuid.UUID]store.APIKey{}, timeZone, "day")

	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(series[0].Points))
	}
	for _, pt := range series[0].Points {
		if _, err := time.Parse("2006-01-02", pt.Date); err != nil {
			t.Fatalf("expected day-format date, got %q", pt.Date)
		}
	}
}

func TestUsageGranularityHour(t *testing.T) {
	t.Parallel()

	timeZone := testTimeZone()
	hour9 := time.Date(2026, 8, 1, 9, 0, 0, 0, timeZone.Location)
	hour14 := time.Date(2026, 8, 1, 14, 0, 0, 0, timeZone.Location)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: hour9, RequestCount: 3},
		{BucketStart: hour14, RequestCount: 5},
	}
	series := buildSeries(rows, GroupByNone, map[uuid.UUID]store.APIKey{}, timeZone, "hour")

	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(series[0].Points))
	}
	if series[0].Points[0].Date != "2026-08-01 09:00" {
		t.Fatalf("unexpected first point date: %q", series[0].Points[0].Date)
	}
	if series[0].Points[1].Date != "2026-08-01 14:00" {
		t.Fatalf("unexpected second point date: %q", series[0].Points[1].Date)
	}
}
