package usage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
)

func TestRequestSummaryUnsupportedFiltersTrimInputs(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	for _, query := range []RequestQuery{
		{Search: "  claude  "},
		{RequestID: " req_123 "},
	} {
		result, err := service.RequestSummary(context.Background(), query, now)
		if err != nil {
			t.Fatalf("RequestSummary unsupported filter: %v", err)
		}
		if result.Supported || result.UnsupportedReason != "search_filter" || result.Currency != "USD" {
			t.Fatalf("unexpected unsupported summary result: %#v", result)
		}
	}
}

func TestChannelSplitNilServiceRejectsBeforeQueryValidation(t *testing.T) {
	t.Parallel()

	var service *Service
	_, err := service.ChannelSplit(context.Background(), ChannelSplitQuery{}, time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "usage store is not initialized") {
		t.Fatalf("ChannelSplit nil service error = %v, want store guard", err)
	}
}

func TestChannelSplitStoreGuardRunsBeforeInvalidRangeValidation(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	_, err := service.ChannelSplit(context.Background(), ChannelSplitQuery{Range: "not-all"}, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "usage store is not initialized") {
		t.Fatalf("ChannelSplit error = %v, want store guard before range validation", err)
	}
	if errors.Is(err, ErrInvalidChannelSplitQuery) {
		t.Fatalf("ChannelSplit error = %v, should not validate range before store guard", err)
	}
}

func TestChannelSplitDateRangeAllIgnoresDateFilters(t *testing.T) {
	t.Parallel()

	dateRange, err := channelSplitDateRange(ChannelSplitQuery{
		Range:    " all ",
		DateFrom: "bad-date",
		DateTo:   "2026-06-01",
	}, time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC), config.LoadTimeZone("UTC"))
	if err != nil {
		t.Fatalf("channelSplitDateRange all: %v", err)
	}
	if !dateRange.All || dateRange.DateFrom != nil || dateRange.DateTo != nil || dateRange.RangeStart != nil || dateRange.RangeEndExclusive != nil {
		t.Fatalf("expected all range to ignore date filters, got %#v", dateRange)
	}
}

func TestChannelSplitDateRangeInvalidAllRangeReturnsTypedError(t *testing.T) {
	t.Parallel()

	_, err := channelSplitDateRange(ChannelSplitQuery{Range: " all-time "}, time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC), config.LoadTimeZone("UTC"))
	if !errors.Is(err, ErrInvalidChannelSplitQuery) {
		t.Fatalf("channelSplitDateRange invalid range error = %v, want ErrInvalidChannelSplitQuery", err)
	}
}

func TestChannelSplitSummaryHandlesEmptyTarget(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	summary := channelSplitSummaryFromItems([]ChannelSplitItem{
		{APIKeyID: uuid.New().String(), RequestCount: 1, TotalTokens: 10},
	}, channelSplitTarget{}, channelSplitWindow{All: true}, time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC), timeZone)

	if summary.SiteID != "" || summary.SiteName != "" || summary.TargetLabel != "" {
		t.Fatalf("expected empty target metadata, got %#v", summary)
	}
	if summary.APIKeyCount != 1 || summary.RequestCount != 1 || summary.TotalTokens != 10 {
		t.Fatalf("expected item totals to still aggregate, got %#v", summary)
	}
}
