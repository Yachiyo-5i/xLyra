package dashboard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestSiteCostSummaryAndOverviewWindowsUseOnlyActiveStoreSites(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	now := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)
	window := newTimeWindow(7, now, timeZone)
	activeSiteID := uuid.New()
	disabledSiteID := uuid.New()
	deletedSiteID := uuid.New()
	svc := NewService(dashboardStoreReturningSites(t, []store.Site{
		{ID: activeSiteID, Name: "Active", Slug: "active", SiteType: "codex", Status: "active", Enabled: true},
		{ID: disabledSiteID, Name: "Disabled", Slug: "disabled", SiteType: "openai", Status: "active", Enabled: false},
		{ID: deletedSiteID, Name: "Deleted", Slug: "deleted", SiteType: "openai", Status: store.SiteStatusDeleted, Enabled: true},
	}), timeZone)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.Add(-time.Nanosecond), SiteID: uuid.NullUUID{UUID: activeSiteID, Valid: true}, RequestCount: 10, SuccessCount: 10, TotalTokens: 1000, EstimatedCost: 99, Success: true},
		{BucketStart: window.RangeStart, SiteID: uuid.NullUUID{UUID: activeSiteID, Valid: true}, RequestCount: 4, SuccessCount: 3, FailureCount: 1, TotalTokens: 400, EstimatedCost: 2, Currency: "USD", Success: true},
		{BucketStart: window.RangeStart.Add(time.Hour), SiteID: uuid.NullUUID{UUID: activeSiteID, Valid: true}, RequestCount: 2, SuccessCount: 1, FailureCount: 1, TotalTokens: 50, EstimatedCost: 1, Currency: "EUR", Success: false, ErrorType: "timeout"},
		{BucketStart: window.RangeStart, SiteID: uuid.NullUUID{UUID: disabledSiteID, Valid: true}, RequestCount: 5, SuccessCount: 5, TotalTokens: 500, EstimatedCost: 20, Success: true},
		{BucketStart: window.RangeStart, SiteID: uuid.NullUUID{UUID: deletedSiteID, Valid: true}, RequestCount: 5, SuccessCount: 5, TotalTokens: 500, EstimatedCost: 30, Success: true},
		{BucketStart: window.RangeStart, SiteName: "Snapshot", SiteSlug: "snapshot", SiteType: "openai", RequestCount: 1, SuccessCount: 1, TotalTokens: 10, EstimatedCost: 10, Success: true},
	}

	items, err := svc.siteCostSummaryFromSummaries(context.Background(), window, rows)
	if err != nil {
		t.Fatalf("site cost summary: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only active store-backed site summary, got %#v", items)
	}
	if items[0].SiteID != activeSiteID.String() || items[0].SiteName != "Active" || items[0].RequestCount != 6 || items[0].SuccessCount != 4 || items[0].TotalTokens != 450 || items[0].Cost != 3 || items[0].Currency != "EUR" {
		t.Fatalf("unexpected active site summary aggregate: %#v", items[0])
	}
	if items[0].SuccessRate == nil || *items[0].SuccessRate != float64(4)/float64(6) {
		t.Fatalf("unexpected active site success rate: %#v", items[0].SuccessRate)
	}

	windows, err := svc.overviewWindowsFromSummaries(context.Background(), now, rows)
	if err != nil {
		t.Fatalf("overview windows: %v", err)
	}
	for _, days := range overviewWindowDays {
		key := fmt.Sprintf("%d", days)
		item, ok := windows[key]
		if !ok {
			t.Fatalf("missing overview window %s in %#v", key, windows)
		}
		if item.Days != days || len(item.HighLatency) != 0 {
			t.Fatalf("unexpected overview window metadata for %s: %#v", key, item)
		}
		if len(item.SiteCostSummary) != 1 || item.SiteCostSummary[0].SiteID != activeSiteID.String() {
			t.Fatalf("overview window %s site summaries = %#v", key, item.SiteCostSummary)
		}
		if days == 7 && (len(item.FailureReasons) != 1 || item.FailureReasons[0].Reason != "timeout" || item.FailureReasons[0].RequestCount != 1) {
			t.Fatalf("overview window %s failure reasons = %#v", key, item.FailureReasons)
		}
	}
}

func dashboardStoreReturningSites(t *testing.T, sites []store.Site) *store.Store {
	t.Helper()

	return dashboardStoreWithQueryCallback(t, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]store.Site)
		if !ok {
			tx.AddError(fmt.Errorf("unexpected dashboard query destination %T", tx.Statement.Dest))
			return
		}
		*dest = append((*dest)[:0], sites...)
		tx.RowsAffected = int64(len(sites))
	})
}
