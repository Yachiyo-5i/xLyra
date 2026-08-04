package usage

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestRequestSummaryDefaultDayWindowUsesCurrentDetails(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 6, 12, 30, 0, 0, time.UTC)
	from := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	to := now.Add(time.Hour)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if includeSummary {
		t.Fatal("expected default day window to skip current summary rows")
	}
	if summaryFrom == nil || !summaryFrom.Equal(from) {
		t.Fatalf("expected summary from %s, got %#v", from, summaryFrom)
	}
	expectedTo := from
	if summaryTo == nil || !summaryTo.Equal(expectedTo) {
		t.Fatalf("expected summary to %s, got %#v", expectedTo, summaryTo)
	}
	windows := service.requestSummaryDetailWindows(&from, &to, summaryFrom, summaryTo, now)
	if len(windows) != 1 || !windows[0].From.Equal(from) || !windows[0].To.Equal(now) {
		t.Fatalf("unexpected current detail windows: %#v", windows)
	}
}

func TestRequestSummaryOpenEndedWindows(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(nil, nil, now)
	expectedSummaryTo := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	if !includeSummary || summaryFrom != nil || summaryTo == nil || !summaryTo.Equal(expectedSummaryTo) {
		t.Fatalf("expected unbounded summary range, got from=%#v to=%#v include=%v", summaryFrom, summaryTo, includeSummary)
	}
	windows := service.requestSummaryDetailWindows(nil, nil, summaryFrom, summaryTo, now)
	if len(windows) != 1 || !windows[0].From.Equal(expectedSummaryTo) || !windows[0].To.Equal(now) {
		t.Fatalf("unexpected current detail windows for unbounded range: %#v", windows)
	}

	to := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	summaryFrom, summaryTo, includeSummary = service.requestSummaryBucketRange(nil, &to, now)
	if !includeSummary || summaryFrom != nil {
		t.Fatalf("expected open-start summary range, got from=%#v include=%v", summaryFrom, includeSummary)
	}
	expectedSummaryTo = time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if summaryTo == nil || !summaryTo.Equal(expectedSummaryTo) {
		t.Fatalf("expected summary to %s, got %#v", expectedSummaryTo, summaryTo)
	}
	windows = service.requestSummaryDetailWindows(nil, &to, summaryFrom, summaryTo, now)
	if len(windows) != 1 || !windows[0].From.Equal(expectedSummaryTo) || !windows[0].To.Equal(to) {
		t.Fatalf("unexpected open-start detail windows: %#v", windows)
	}
}

func TestRequestSummaryPartialDaysUseBoundedDetailWindows(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	from := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if !includeSummary {
		t.Fatal("expected middle full day to use summary rows")
	}
	expectedSummaryFrom := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	expectedSummaryTo := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if summaryFrom == nil || !summaryFrom.Equal(expectedSummaryFrom) {
		t.Fatalf("expected summary from %s, got %#v", expectedSummaryFrom, summaryFrom)
	}
	if summaryTo == nil || !summaryTo.Equal(expectedSummaryTo) {
		t.Fatalf("expected summary to %s, got %#v", expectedSummaryTo, summaryTo)
	}

	windows := service.requestSummaryDetailWindows(&from, &to, summaryFrom, summaryTo, now)
	if len(windows) != 2 {
		t.Fatalf("expected two detail windows, got %#v", windows)
	}
	firstEnd := expectedSummaryFrom.Add(-time.Nanosecond)
	if !windows[0].From.Equal(from) || !windows[0].To.Equal(firstEnd) {
		t.Fatalf("unexpected first detail window: %#v", windows[0])
	}
	if !windows[1].From.Equal(expectedSummaryTo) || !windows[1].To.Equal(to) {
		t.Fatalf("unexpected second detail window: %#v", windows[1])
	}
}

func TestRequestSummaryWindowsUseServiceTimeZone(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("Asia/Shanghai")
	service := NewService(nil, timeZone)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, timeZone.Location)
	from := time.Date(2026, 6, 1, 8, 0, 0, 0, timeZone.Location)
	to := time.Date(2026, 6, 3, 23, 59, 59, 999999999, timeZone.Location)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if !includeSummary {
		t.Fatal("expected full local days to use summary rows")
	}
	if got := timeZone.Format(*summaryFrom, time.RFC3339); got != "2026-06-02T00:00:00+08:00" {
		t.Fatalf("summary from = %s, want 2026-06-02T00:00:00+08:00", got)
	}
	if got := timeZone.Format(*summaryTo, time.RFC3339); got != "2026-06-04T00:00:00+08:00" {
		t.Fatalf("summary to = %s, want 2026-06-04T00:00:00+08:00", got)
	}

	windows := service.requestSummaryDetailWindows(&from, &to, summaryFrom, summaryTo, now)
	if len(windows) != 1 {
		t.Fatalf("expected one partial local-day window, got %#v", windows)
	}
	if got := timeZone.Format(windows[0].From, time.RFC3339); got != "2026-06-01T08:00:00+08:00" {
		t.Fatalf("detail window from = %s", got)
	}
	if got := timeZone.Format(windows[0].To, time.RFC3339Nano); got != "2026-06-01T23:59:59.999999999+08:00" {
		t.Fatalf("detail window to = %s", got)
	}
}

func TestRequestSummaryBucketRangeExcludesSummaryWhenNoFullDay(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	from := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if includeSummary {
		t.Fatalf("expected no complete summary day, got from=%#v to=%#v", summaryFrom, summaryTo)
	}
	expectedSummaryFrom := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	expectedSummaryTo := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if summaryFrom == nil || !summaryFrom.Equal(expectedSummaryFrom) {
		t.Fatalf("expected summary from %s, got %#v", expectedSummaryFrom, summaryFrom)
	}
	if summaryTo == nil || !summaryTo.Equal(expectedSummaryTo) {
		t.Fatalf("expected summary to %s, got %#v", expectedSummaryTo, summaryTo)
	}
}

func TestRequestSummaryDetailWindowsDeduplicatesSinglePartialDay(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	from := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if includeSummary {
		t.Fatalf("expected single partial day to skip summary rows, got from=%#v to=%#v", summaryFrom, summaryTo)
	}

	windows := service.requestSummaryDetailWindows(&from, &to, summaryFrom, summaryTo, now)
	if len(windows) != 1 {
		t.Fatalf("expected one deduplicated detail window, got %#v", windows)
	}
	if !windows[0].From.Equal(from) || !windows[0].To.Equal(to) {
		t.Fatalf("unexpected detail window: %#v", windows[0])
	}
}

func TestRequestSummaryCurrentPartialDayUsesDetails(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	from := time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if includeSummary {
		t.Fatalf("expected partial current day to skip summary rows, got from=%#v to=%#v", summaryFrom, summaryTo)
	}
	windows := service.requestSummaryDetailWindows(&from, &to, summaryFrom, summaryTo, now)
	if len(windows) != 1 || !windows[0].From.Equal(from) || !windows[0].To.Equal(to) {
		t.Fatalf("unexpected partial current-day detail windows: %#v", windows)
	}
}

func TestRequestSummaryCurrentFullDayUsesDetails(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	from := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	to := now

	summaryFrom, summaryTo, includeSummary := service.requestSummaryBucketRange(&from, &to, now)
	if includeSummary {
		t.Fatalf("expected current day through now to skip summary rows")
	}
	expectedTo := from
	if summaryFrom == nil || !summaryFrom.Equal(from) || summaryTo == nil || !summaryTo.Equal(expectedTo) {
		t.Fatalf("unexpected current-day summary bounds: from=%#v to=%#v", summaryFrom, summaryTo)
	}
	windows := service.requestSummaryDetailWindows(&from, &to, summaryFrom, summaryTo, now)
	if len(windows) != 1 || !windows[0].From.Equal(from) || !windows[0].To.Equal(now) {
		t.Fatalf("unexpected current-day detail windows: %#v", windows)
	}
}

func TestChannelSplitDateRangeUsesNaturalDays(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 6, 30, 15, 30, 0, 0, timeZone.Location)
	dateRange, err := channelSplitDateRange(ChannelSplitQuery{
		DateFrom: "2026-06-01",
		DateTo:   "2026-06-30",
	}, now, timeZone)
	if err != nil {
		t.Fatalf("channel split date range: %v", err)
	}

	if dateRange.RangeStart == nil {
		t.Fatal("expected bounded range start")
	}
	if got := dateRange.RangeStart.Format(time.RFC3339); got != "2026-06-01T00:00:00+08:00" {
		t.Fatalf("unexpected range start: %s", got)
	}
	if dateRange.RangeEndExclusive == nil {
		t.Fatal("expected bounded range end")
	}
	if got := dateRange.RangeEndExclusive.Format(time.RFC3339); got != "2026-07-01T00:00:00+08:00" {
		t.Fatalf("unexpected exclusive range end: %s", got)
	}
}

func TestChannelSplitDateRangeDefaultsToThirtyNaturalDays(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	now := time.Date(2026, 6, 30, 15, 30, 0, 0, time.UTC)
	dateRange, err := channelSplitDateRange(ChannelSplitQuery{}, now, timeZone)
	if err != nil {
		t.Fatalf("channel split date range: %v", err)
	}

	if dateRange.DateFrom == nil {
		t.Fatal("expected default date_from")
	}
	if got := dateRange.DateFrom.Format("2006-01-02"); got != "2026-06-01" {
		t.Fatalf("expected default date_from 2026-06-01, got %s", got)
	}
	if dateRange.DateTo == nil {
		t.Fatal("expected default date_to")
	}
	if got := dateRange.DateTo.Format("2006-01-02"); got != "2026-06-30" {
		t.Fatalf("expected default date_to 2026-06-30, got %s", got)
	}
}

func TestChannelSplitDateRangeAllowsLongRange(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	dateRange, err := channelSplitDateRange(ChannelSplitQuery{
		DateFrom: "2026-06-01",
		DateTo:   "2026-07-02",
	}, time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC), timeZone)
	if err != nil {
		t.Fatalf("channel split date range: %v", err)
	}
	if dateRange.DateTo == nil || dateRange.DateTo.Format("2006-01-02") != "2026-07-02" {
		t.Fatalf("expected long range to be accepted, got %#v", dateRange.DateTo)
	}
}

func TestChannelSplitDateRangeTrimsInputs(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	dateRange, err := channelSplitDateRange(ChannelSplitQuery{
		DateFrom: " 2026-06-01 ",
		DateTo:   " 2026-06-02 ",
	}, now, timeZone)
	if err != nil {
		t.Fatalf("channel split trimmed dates: %v", err)
	}
	if dateRange.DateFrom == nil || dateRange.DateFrom.Format("2006-01-02") != "2026-06-01" {
		t.Fatalf("unexpected trimmed date_from: %#v", dateRange.DateFrom)
	}
	if dateRange.DateTo == nil || dateRange.DateTo.Format("2006-01-02") != "2026-06-02" {
		t.Fatalf("unexpected trimmed date_to: %#v", dateRange.DateTo)
	}
}

func TestChannelSplitDateRangeRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		query ChannelSplitQuery
	}{
		{name: "unknown range", query: ChannelSplitQuery{Range: "week"}},
		{name: "invalid from", query: ChannelSplitQuery{DateFrom: "2026/06/01"}},
		{name: "invalid to", query: ChannelSplitQuery{DateTo: "tomorrow"}},
		{name: "from after to", query: ChannelSplitQuery{DateFrom: "2026-07-02", DateTo: "2026-07-01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := channelSplitDateRange(tc.query, now, timeZone)
			if !errors.Is(err, ErrInvalidChannelSplitQuery) {
				t.Fatalf("channelSplitDateRange error = %v, want ErrInvalidChannelSplitQuery", err)
			}
		})
	}
}

func TestParseChannelSplitDateFallsBackWhenLocationMissing(t *testing.T) {
	t.Parallel()

	got, err := parseChannelSplitDate(" 2026-06-22 ", nil)
	if err != nil {
		t.Fatalf("parseChannelSplitDate: %v", err)
	}
	if got.Format("2006-01-02") != "2026-06-22" {
		t.Fatalf("parsed date = %s, want 2026-06-22", got)
	}
}

func TestChannelSplitItemsFromRowsAggregatesByAPIKey(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	otherSiteID := uuid.New()
	apiKeyID := uuid.New()
	otherAPIKeyID := uuid.New()

	items := channelSplitItemsFromRows([]store.RequestUsageDailySummary{
		{
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:         uuid.NullUUID{UUID: apiKeyID, Valid: true},
			APIKeyName:       "old name",
			RequestCount:     2,
			SuccessCount:     2,
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			EstimatedCost:    3,
			Currency:         "USD",
		},
		{
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:         uuid.NullUUID{UUID: apiKeyID, Valid: true},
			RequestCount:     1,
			FailureCount:     1,
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    1,
			Currency:         "USD",
		},
		{
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:         uuid.NullUUID{UUID: otherAPIKeyID, Valid: true},
			APIKeyName:       "Team B",
			RequestCount:     1,
			SuccessCount:     1,
			PromptTokens:     20,
			CompletionTokens: 15,
			TotalTokens:      35,
			EstimatedCost:    2,
			Currency:         "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: otherSiteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: apiKeyID, Valid: true},
			RequestCount:  99,
			EstimatedCost: 99,
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			RequestCount:  99,
			EstimatedCost: 99,
		},
	}, []uuid.UUID{siteID}, map[uuid.UUID]store.APIKey{
		apiKeyID: {ID: apiKeyID, Name: "Team A", MaskedKey: "sk-***aaaa"},
	})

	if len(items) != 2 {
		t.Fatalf("expected two api key split items, got %#v", items)
	}
	if items[0].APIKeyID != apiKeyID.String() {
		t.Fatalf("expected highest cost item first, got %#v", items[0])
	}
	if items[0].APIKeyName != "Team A" || items[0].MaskedKey != "sk-***aaaa" {
		t.Fatalf("expected current api key snapshot, got %#v", items[0])
	}
	if items[0].RequestCount != 3 || items[0].SuccessCount != 2 || items[0].FailureCount != 1 {
		t.Fatalf("unexpected request counts: %#v", items[0])
	}
	if items[0].PromptTokens != 110 || items[0].CompletionTokens != 55 || items[0].TotalTokens != 165 {
		t.Fatalf("unexpected token totals: %#v", items[0])
	}
	if !almostEqual(items[0].EstimatedCost, 4) || !almostEqual(items[0].CostShare, 4.0/6.0) || !almostEqual(items[0].TokenShare, 165.0/200.0) {
		t.Fatalf("unexpected cost/share: %#v", items[0])
	}
}

func TestChannelSplitItemsKeepRowNameWhenAPIKeySnapshotBlank(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()
	items := channelSplitItemsFromRows([]store.RequestUsageDailySummary{
		{
			SiteID:       uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:     uuid.NullUUID{UUID: apiKeyID, Valid: true},
			APIKeyName:   " Row Name ",
			RequestCount: 1,
			TotalTokens:  10,
			Currency:     "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: apiKeyID, Valid: true},
			RequestCount:  2,
			TotalTokens:   30,
			EstimatedCost: 0,
			Currency:      "EUR",
		},
	}, []uuid.UUID{siteID}, map[uuid.UUID]store.APIKey{
		apiKeyID: {ID: apiKeyID, Name: " \t ", MaskedKey: "sk-***"},
	})

	if len(items) != 1 {
		t.Fatalf("expected one channel split item, got %#v", items)
	}
	if items[0].APIKeyName != "Row Name" || items[0].MaskedKey != "sk-***" {
		t.Fatalf("expected row name with masked key snapshot, got %#v", items[0])
	}
	if items[0].RequestCount != 3 || items[0].TotalTokens != 40 {
		t.Fatalf("unexpected aggregate totals: %#v", items[0])
	}
	if items[0].Currency != "EUR" {
		t.Fatalf("currency = %q, want latest non-empty row currency EUR", items[0].Currency)
	}
	if items[0].CostShare != 0 || items[0].TokenShare != 1 {
		t.Fatalf("unexpected shares for zero-cost token aggregate: %#v", items[0])
	}
}

func TestChannelSplitItemsFromRowsAggregatesMultipleSitesByAPIKey(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	oldSiteID := uuid.New()
	apiKeyID := uuid.New()

	items := channelSplitItemsFromRows([]store.RequestUsageDailySummary{
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: apiKeyID, Valid: true},
			RequestCount:  1,
			TotalTokens:   100,
			EstimatedCost: 2,
			Currency:      "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: oldSiteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: apiKeyID, Valid: true},
			RequestCount:  2,
			TotalTokens:   300,
			EstimatedCost: 6,
			Currency:      "USD",
		},
	}, []uuid.UUID{siteID, oldSiteID}, map[uuid.UUID]store.APIKey{})

	if len(items) != 1 {
		t.Fatalf("expected one api key split item, got %#v", items)
	}
	if items[0].RequestCount != 3 || items[0].TotalTokens != 400 || !almostEqual(items[0].EstimatedCost, 8) {
		t.Fatalf("unexpected multi-site aggregate: %#v", items[0])
	}
}

func TestChannelSplitItemsFromRowsUsesRowNameAndSortsTiesByName(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	alphaAPIKeyID := uuid.New()
	betaAPIKeyID := uuid.New()
	gammaAPIKeyID := uuid.New()

	items := channelSplitItemsFromRows([]store.RequestUsageDailySummary{
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: betaAPIKeyID, Valid: true},
			APIKeyName:    " Beta ",
			RequestCount:  1,
			TotalTokens:   100,
			EstimatedCost: 1,
			Currency:      "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: alphaAPIKeyID, Valid: true},
			APIKeyName:    "Alpha",
			RequestCount:  1,
			TotalTokens:   100,
			EstimatedCost: 1,
			Currency:      "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: gammaAPIKeyID, Valid: true},
			RequestCount:  1,
			TotalTokens:   100,
			EstimatedCost: 1,
			Currency:      "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: gammaAPIKeyID, Valid: true},
			APIKeyName:    "Gamma",
			TotalTokens:   0,
			EstimatedCost: 0,
			Currency:      "USD",
		},
	}, []uuid.UUID{siteID}, map[uuid.UUID]store.APIKey{})

	if len(items) != 3 {
		t.Fatalf("expected three api key split items, got %#v", items)
	}
	if items[0].APIKeyName != "Alpha" || items[1].APIKeyName != "Beta" || items[2].APIKeyName != "Gamma" {
		t.Fatalf("expected row names to sort equal cost/token rows, got %#v", items)
	}
}

func TestChannelSplitItemsFromRowsDefaultsCurrencyAndLeavesZeroShares(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()

	items := channelSplitItemsFromRows([]store.RequestUsageDailySummary{
		{
			SiteID:       uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:     uuid.NullUUID{UUID: apiKeyID, Valid: true},
			APIKeyName:   " Team A ",
			RequestCount: 1,
		},
	}, []uuid.UUID{siteID}, map[uuid.UUID]store.APIKey{
		apiKeyID: {ID: apiKeyID, MaskedKey: "sk-***aaaa"},
	})

	if len(items) != 1 {
		t.Fatalf("expected one api key split item, got %#v", items)
	}
	if items[0].APIKeyName != "Team A" || items[0].MaskedKey != "sk-***aaaa" {
		t.Fatalf("unexpected api key display values: %#v", items[0])
	}
	if items[0].Currency != "USD" {
		t.Fatalf("currency = %q, want USD", items[0].Currency)
	}
	if items[0].CostShare != 0 || items[0].TokenShare != 0 {
		t.Fatalf("expected zero shares when totals are zero, got %#v", items[0])
	}
}

func TestChannelSplitSummaryFromItemsAggregatesTargetAndDateMetadata(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	otherSiteID := uuid.New()
	connectionID := uuid.New()
	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, timeZone.Location)
	dateFrom := time.Date(2026, 6, 1, 0, 0, 0, 0, timeZone.Location)
	dateTo := time.Date(2026, 6, 22, 0, 0, 0, 0, timeZone.Location)
	rangeEnd := dateTo.AddDate(0, 0, 1)

	summary := channelSplitSummaryFromItems([]ChannelSplitItem{
		{
			RequestCount:     2,
			SuccessCount:     2,
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
			EstimatedCost:    1.25,
			Currency:         "USD",
		},
		{
			RequestCount:     1,
			FailureCount:     1,
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    0.75,
			Currency:         "EUR",
		},
	}, channelSplitTarget{
		Sites: []channelSplitSite{
			{ID: siteID, Name: "Primary", Slug: "primary", SiteType: "codex"},
			{ID: otherSiteID, Name: "Fallback", Slug: "fallback", SiteType: "openai"},
		},
		OAuthConnectionID: &connectionID,
		OAuthProvider:     "codex",
		OAuthAccount:      "user@example.com",
	}, channelSplitWindow{
		DateFrom:          &dateFrom,
		DateTo:            &dateTo,
		RangeStart:        &dateFrom,
		RangeEndExclusive: &rangeEnd,
	}, now, timeZone)

	if summary.SiteID != siteID.String() || summary.SiteName != "Primary" || summary.SiteSlug != "primary" || summary.SiteType != "codex" {
		t.Fatalf("unexpected primary site summary: %#v", summary)
	}
	if len(summary.SiteIDs) != 2 || summary.SiteIDs[0] != siteID.String() || summary.SiteIDs[1] != otherSiteID.String() {
		t.Fatalf("unexpected site ids: %#v", summary.SiteIDs)
	}
	if summary.TargetLabel != "Primary +1" {
		t.Fatalf("target label = %q, want Primary +1", summary.TargetLabel)
	}
	if summary.OAuthConnectionID == nil || *summary.OAuthConnectionID != connectionID.String() {
		t.Fatalf("unexpected oauth connection id: %#v", summary.OAuthConnectionID)
	}
	if summary.OAuthProvider == nil || *summary.OAuthProvider != "codex" || summary.OAuthAccount == nil || *summary.OAuthAccount != "user@example.com" {
		t.Fatalf("unexpected oauth metadata: %#v", summary)
	}
	if summary.DateFrom != "2026-06-01" || summary.DateTo != "2026-06-22" {
		t.Fatalf("unexpected dates: %#v", summary)
	}
	if summary.RangeStart != "2026-06-01T00:00:00+08:00" || summary.RangeEnd != "2026-06-23T00:00:00+08:00" {
		t.Fatalf("unexpected range bounds: %#v", summary)
	}
	if summary.RequestCount != 3 || summary.SuccessCount != 2 || summary.FailureCount != 1 {
		t.Fatalf("unexpected request counts: %#v", summary)
	}
	if summary.PromptTokens != 110 || summary.CompletionTokens != 55 || summary.TotalTokens != 165 {
		t.Fatalf("unexpected token totals: %#v", summary)
	}
	if !almostEqual(summary.EstimatedCost, 2) || summary.Currency != "EUR" || summary.APIKeyCount != 2 {
		t.Fatalf("unexpected cost/currency/api key count: %#v", summary)
	}
}

func TestChannelSplitSummaryFromItemsHandlesAllRange(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	timeZone := config.LoadTimeZone("UTC")
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)

	summary := channelSplitSummaryFromItems(nil, channelSplitTarget{
		Sites: []channelSplitSite{{ID: siteID, Name: "Primary", Slug: "primary", SiteType: "openai"}},
	}, channelSplitWindow{All: true}, now, timeZone)

	if !summary.RangeAll {
		t.Fatalf("expected all range summary, got %#v", summary)
	}
	if summary.DateFrom != "" || summary.DateTo != "" || summary.RangeStart != "" || summary.RangeEnd != "" {
		t.Fatalf("expected all range to omit bounded dates, got %#v", summary)
	}
	if summary.SiteID != siteID.String() || summary.TargetLabel != "Primary" || len(summary.SiteIDs) != 1 || summary.SiteNames[0] != "Primary" {
		t.Fatalf("unexpected all range target metadata: %#v", summary)
	}
	if summary.GeneratedAt != "2026-06-22T12:00:00Z" || summary.Currency != "USD" || summary.APIKeyCount != 0 {
		t.Fatalf("unexpected all range defaults: %#v", summary)
	}
}

func TestChannelSplitTargetHelpers(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	target := channelSplitTarget{Sites: []channelSplitSite{
		{ID: first, Name: "Primary"},
		{ID: first, Name: "Duplicate"},
		{ID: second, Name: "Fallback"},
	}}

	ids := target.siteIDs()
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("target.siteIDs = %#v, want first+second unique ids", ids)
	}
	if got := channelSplitTargetLabel(channelSplitTarget{}); got != "" {
		t.Fatalf("empty target label = %q, want empty", got)
	}
	if got := channelSplitUUIDString(uuid.Nil); got != "" {
		t.Fatalf("nil uuid string = %q, want empty", got)
	}
	if got := channelSplitStringDefault(" ", "fallback"); got != "fallback" {
		t.Fatalf("blank string default = %q, want fallback", got)
	}
}

func TestChannelSplitQueryHelpers(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	ids := channelSplitRequestedSiteIDs(ChannelSplitQuery{
		SiteID:  &first,
		SiteIDs: []uuid.UUID{first, uuid.Nil, second, second},
	})
	if len(ids) != 3 || ids[0] != first || ids[1] != uuid.Nil || ids[2] != second {
		t.Fatalf("channelSplitRequestedSiteIDs = %#v", ids)
	}

	siteSet := channelSplitSiteSet(ids)
	if _, ok := siteSet[first]; !ok {
		t.Fatalf("site set missing first id: %#v", siteSet)
	}
	if _, ok := siteSet[uuid.Nil]; ok {
		t.Fatalf("site set should ignore nil uuid: %#v", siteSet)
	}
	if !channelSplitRowMatchesSites(store.RequestUsageDailySummary{SiteID: uuid.NullUUID{UUID: second, Valid: true}}, siteSet) {
		t.Fatal("expected row with target site to match")
	}
	if channelSplitRowMatchesSites(store.RequestUsageDailySummary{}, siteSet) {
		t.Fatal("expected row without site id not to match")
	}

	err := invalidChannelSplitQuery("site %s is invalid", "alpha")
	if !errors.Is(err, ErrInvalidChannelSplitQuery) || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("invalidChannelSplitQuery = %v", err)
	}

	left := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	right := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if got := minTime(left, right); !got.Equal(left) {
		t.Fatalf("minTime = %s, want %s", got, left)
	}
	if got := minTime(right, left); !got.Equal(left) {
		t.Fatalf("minTime reversed = %s, want %s", got, left)
	}
	if got := maxTime(left, right); !got.Equal(right) {
		t.Fatalf("maxTime = %s, want %s", got, right)
	}
	if got := maxTime(right, left); !got.Equal(right) {
		t.Fatalf("maxTime reversed = %s, want %s", got, right)
	}
}

func TestChannelSplitAPIKeyIDsDeduplicatesMatchingRows(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	otherSiteID := uuid.New()
	firstAPIKeyID := uuid.New()
	secondAPIKeyID := uuid.New()

	ids := channelSplitAPIKeyIDs([]store.RequestUsageDailySummary{
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, APIKeyID: uuid.NullUUID{UUID: firstAPIKeyID, Valid: true}},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, APIKeyID: uuid.NullUUID{UUID: firstAPIKeyID, Valid: true}},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, APIKeyID: uuid.NullUUID{UUID: secondAPIKeyID, Valid: true}},
		{SiteID: uuid.NullUUID{UUID: otherSiteID, Valid: true}, APIKeyID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}},
	}, []uuid.UUID{siteID})
	if len(ids) != 2 || ids[0] != firstAPIKeyID || ids[1] != secondAPIKeyID {
		t.Fatalf("channelSplitAPIKeyIDs = %#v, want first+second unique IDs", ids)
	}
}

func TestChannelSplitAPIKeyIDsReturnsEmptyForNoMatches(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	ids := channelSplitAPIKeyIDs([]store.RequestUsageDailySummary{
		{SiteID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, APIKeyID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
		{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}},
		{APIKeyID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
	}, []uuid.UUID{siteID})
	if len(ids) != 0 {
		t.Fatalf("channelSplitAPIKeyIDs = %#v, want empty", ids)
	}

	if ids = channelSplitAPIKeyIDs(nil, []uuid.UUID{siteID}); len(ids) != 0 {
		t.Fatalf("channelSplitAPIKeyIDs with nil rows = %#v, want empty", ids)
	}
}

func TestAppendRequestTimeRangeSkipsInvalidAndDuplicateRanges(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	windows := appendRequestTimeRange(nil, from, to)
	windows = appendRequestTimeRange(windows, from, to)
	windows = appendRequestTimeRange(windows, to, from)
	if len(windows) != 1 || !windows[0].From.Equal(from) || !windows[0].To.Equal(to) {
		t.Fatalf("appendRequestTimeRange = %#v, want one original range", windows)
	}
}

func TestUsageServicesResolveTimeZones(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("Asia/Shanghai")
	service := NewSummaryService(nil, nil, timeZone)
	if service.db != nil || service.confFile != nil || service.timeZone.Name != "Asia/Shanghai" {
		t.Fatalf("NewSummaryService = %#v", service)
	}
	if got := summaryServiceTimeZone(config.TimeZone{Name: "UTC"}); got.Location == nil {
		t.Fatalf("summaryServiceTimeZone should fall back to a resolved timezone, got %#v", got)
	}

	usageService := usageUTCService()
	if usageService.timeZone.Location == nil || usageService.timeZone.Name != "UTC" {
		t.Fatalf("usage service timezone = %#v", usageService.timeZone)
	}
	if got := usageServiceTimeZone(config.TimeZone{Name: "UTC"}); got.Location == nil {
		t.Fatalf("usageServiceTimeZone should fall back to a resolved timezone, got %#v", got)
	}
}

func almostEqual(left float64, right float64) bool {
	return math.Abs(left-right) < 0.0000001
}
