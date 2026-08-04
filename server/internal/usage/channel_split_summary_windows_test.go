package usage

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestChannelSplitItemsPreferCostThenTokensAndDefaultName(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	highCostID := uuid.New()
	highTokensID := uuid.New()
	blankNameID := uuid.New()
	items := channelSplitItemsFromRows([]store.RequestUsageDailySummary{
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: highTokensID, Valid: true},
			APIKeyName:    "Token Heavy",
			RequestCount:  1,
			TotalTokens:   300,
			EstimatedCost: 5,
			Currency:      "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: highCostID, Valid: true},
			APIKeyName:    "Cost Heavy",
			RequestCount:  1,
			TotalTokens:   100,
			EstimatedCost: 8,
			Currency:      "USD",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			APIKeyID:      uuid.NullUUID{UUID: blankNameID, Valid: true},
			APIKeyName:    " ",
			RequestCount:  1,
			TotalTokens:   50,
			EstimatedCost: 1,
		},
	}, []uuid.UUID{siteID}, map[uuid.UUID]store.APIKey{})

	if len(items) != 3 {
		t.Fatalf("expected three channel split items, got %#v", items)
	}
	if items[0].APIKeyID != highCostID.String() || items[1].APIKeyID != highTokensID.String() {
		t.Fatalf("expected cost to sort before token totals, got %#v", items)
	}
	if items[2].APIKeyName != "" || items[2].Currency != "USD" {
		t.Fatalf("expected blank row name and default currency to be preserved, got %#v", items[2])
	}
	if !almostEqual(items[0].CostShare, 8.0/14.0) || !almostEqual(items[1].TokenShare, 300.0/450.0) {
		t.Fatalf("unexpected channel split shares: %#v", items)
	}
}

func TestChannelSplitSummaryUsesSingleSiteLabelAndCurrencyDefault(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	timeZone := config.LoadTimeZone("UTC")
	now := time.Date(2026, 6, 22, 8, 30, 0, 0, time.UTC)
	dateFrom := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	dateTo := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	rangeEnd := dateTo.AddDate(0, 0, 1)

	summary := channelSplitSummaryFromItems([]ChannelSplitItem{
		{RequestCount: 2, SuccessCount: 1, FailureCount: 1, PromptTokens: 20, CompletionTokens: 30, TotalTokens: 50, EstimatedCost: 0.5},
	}, channelSplitTarget{
		Sites: []channelSplitSite{{ID: siteID, Name: "Primary", Slug: "primary", SiteType: "openai"}},
	}, channelSplitWindow{
		DateFrom:          &dateFrom,
		DateTo:            &dateTo,
		RangeStart:        &dateFrom,
		RangeEndExclusive: &rangeEnd,
	}, now, timeZone)

	if summary.TargetLabel != "Primary" || summary.SiteID != siteID.String() || len(summary.SiteIDs) != 1 || summary.SiteNames[0] != "Primary" {
		t.Fatalf("unexpected single-site summary target metadata: %#v", summary)
	}
	if summary.Currency != "USD" || summary.APIKeyCount != 1 {
		t.Fatalf("expected default currency and one API key count, got %#v", summary)
	}
	if summary.DateFrom != "2026-06-20" || summary.DateTo != "2026-06-22" || summary.RangeEnd != "2026-06-23T00:00:00Z" {
		t.Fatalf("unexpected bounded range metadata: %#v", summary)
	}
	if summary.RequestCount != 2 || summary.SuccessCount != 1 || summary.FailureCount != 1 || summary.TotalTokens != 50 {
		t.Fatalf("unexpected aggregate totals: %#v", summary)
	}
}

func TestChannelSplitDateRangeUsesFallbackResolvedTimeZone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	dateRange, err := channelSplitDateRange(ChannelSplitQuery{DateFrom: "2026-06-21", DateTo: "2026-06-22"}, now, config.TimeZone{Name: "UTC"})
	if err != nil {
		t.Fatalf("channelSplitDateRange with unresolved timezone: %v", err)
	}
	if dateRange.RangeStart == nil || dateRange.RangeEndExclusive == nil {
		t.Fatalf("expected bounded date range, got %#v", dateRange)
	}
	if got := dateRange.DateFrom.Format("2006-01-02"); got != "2026-06-21" {
		t.Fatalf("date_from = %s, want 2026-06-21", got)
	}
}

func TestChannelSplitHelpersPreserveNilAndDuplicateUUIDBehavior(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	values := appendChannelSplitUUIDOnce(nil, first)
	values = appendChannelSplitUUIDOnce(values, first)
	values = appendChannelSplitUUIDOnce(values, uuid.Nil)
	values = appendChannelSplitUUIDOnce(values, second)

	if len(values) != 3 || values[0] != first || values[1] != uuid.Nil || values[2] != second {
		t.Fatalf("appendChannelSplitUUIDOnce = %#v, want first, nil, second", values)
	}
	if ids := channelSplitTargetSiteIDs(channelSplitTarget{}); len(ids) != 0 {
		t.Fatalf("empty target site IDs = %#v, want empty", ids)
	}
	if names := channelSplitTargetSiteNames(channelSplitTarget{}); len(names) != 0 {
		t.Fatalf("empty target site names = %#v, want empty", names)
	}
}

func TestInvalidChannelSplitDateErrorsStayTyped(t *testing.T) {
	t.Parallel()

	_, err := parseChannelSplitDate("2026-02-30", time.UTC)
	if err == nil {
		t.Fatal("expected invalid calendar date to fail")
	}
	_, err = channelSplitDateRange(ChannelSplitQuery{DateFrom: "2026-02-30"}, time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC), config.LoadTimeZone("UTC"))
	if !errors.Is(err, ErrInvalidChannelSplitQuery) {
		t.Fatalf("channelSplitDateRange invalid calendar error = %v, want ErrInvalidChannelSplitQuery", err)
	}
}
