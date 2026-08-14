package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
)

func TestRouteCooldownListActiveFiltersExpiredClearedAndSortsOffline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]RouteCooldown)
		if !ok {
			tx.AddError(errors.New("unexpected route cooldown list destination"))
			return
		}
		*items = []RouteCooldown{
			{
				ID:          uuid.New(),
				Scope:       "site",
				ActiveUntil: now.Add(30 * time.Minute),
				CreatedAt:   now.Add(-2 * time.Minute),
			},
			{
				ID:          uuid.New(),
				Scope:       "expired",
				ActiveUntil: now.Add(-time.Minute),
			},
			{
				ID:          uuid.New(),
				Scope:       "cleared",
				ActiveUntil: now.Add(time.Hour),
				ClearedAt:   sql.NullTime{Time: now.Add(-time.Minute), Valid: true},
			},
			{
				ID:          uuid.New(),
				Scope:       "credential",
				ActiveUntil: now.Add(2 * time.Hour),
				CreatedAt:   now.Add(-3 * time.Minute),
			},
			{
				ID:          uuid.New(),
				Scope:       "newer-tie",
				ActiveUntil: now.Add(30 * time.Minute),
				CreatedAt:   now.Add(-time.Minute),
			},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})

	items, err := NewRouteCooldownRepository(db).ListActive(context.Background(), now)
	if err != nil {
		t.Fatalf("ListActive returned error: %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("active cooldowns = %#v, want 3 filtered items", items)
	}
	if items[0].Scope != "credential" || items[1].Scope != "newer-tie" || items[2].Scope != "site" {
		t.Fatalf("active cooldown order = %#v", items)
	}
}

func TestAPIKeyAccessEnabledSiteIDsForEmptyGroupsSkipsDBOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(errors.New("query callback should not run for empty group ids"))
	})

	ids, err := NewAPIKeyAccessRepository(db).EnabledSiteIDsForGroups(context.Background(), nil)
	if err != nil {
		t.Fatalf("EnabledSiteIDsForGroups returned error: %v", err)
	}
	if ids != nil {
		t.Fatalf("empty group ids result = %#v, want nil", ids)
	}
}

func TestAPIKeyAccessEnabledSiteIDsForGroupsFiltersDisabledAndDedupesOffline(t *testing.T) {
	t.Parallel()

	enabledGroupID := uuid.New()
	disabledGroupID := uuid.New()
	siteA := uuid.New()
	siteB := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch dest := tx.Statement.Dest.(type) {
		case *[]SiteGroupSite:
			*dest = []SiteGroupSite{
				{GroupID: enabledGroupID, SiteID: siteA},
				{GroupID: enabledGroupID, SiteID: siteA},
				{GroupID: disabledGroupID, SiteID: siteB},
				{GroupID: enabledGroupID, SiteID: siteB},
			}
		case *[]SiteGroup:
			*dest = []SiteGroup{
				{ID: enabledGroupID, Enabled: true},
				{ID: disabledGroupID, Enabled: false},
			}
		default:
			tx.AddError(errors.New("unexpected api key access query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	ids, err := NewAPIKeyAccessRepository(db).EnabledSiteIDsForGroups(context.Background(), []uuid.UUID{enabledGroupID, disabledGroupID})
	if err != nil {
		t.Fatalf("EnabledSiteIDsForGroups returned error: %v", err)
	}

	if queryCalls != 2 {
		t.Fatalf("query calls = %d, want 2", queryCalls)
	}
	if len(ids) != 2 || ids[0] != siteA || ids[1] != siteB {
		t.Fatalf("enabled site ids = %#v, want deduped enabled group sites", ids)
	}
}

func TestGatewayRateLimitApplyWindowUpdateClampsNegativeCountersOffline(t *testing.T) {
	t.Parallel()

	windowStart := time.Date(2026, 6, 23, 11, 15, 0, 0, time.FixedZone("CST", 8*60*60))
	db := storeRepositoryOfflineGorm(t)
	createCalls := 0
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		createCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*GatewayRateLimitWindow)
		if !ok {
			tx.AddError(errors.New("unexpected rate limit window query destination"))
			return
		}
		*item = GatewayRateLimitWindow{
			ID:          uuid.New(),
			ScopeKey:    "api-key:clamped-window",
			WindowStart: windowStart.UTC(),
			RPMUsed:     2,
			TPMReserved: 3,
			TPMActual:   4,
		}
		tx.Statement.RowsAffected = 1
	})
	var saved GatewayRateLimitWindow
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*GatewayRateLimitWindow)
		if !ok {
			tx.AddError(errors.New("unexpected rate limit window update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	item, err := NewGatewayRateLimitRepository(db).ApplyWindowUpdate(context.Background(), RateLimitWindowUpdate{
		ScopeKey:        "api-key:clamped-window",
		WindowStart:     windowStart,
		RPMDelta:        -10,
		TPMReserveDelta: -10,
		TPMActualDelta:  -10,
	})
	if err != nil {
		t.Fatalf("ApplyWindowUpdate returned error: %v", err)
	}

	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1 upsert attempt", createCalls)
	}
	if item.RPMUsed != 0 || item.TPMReserved != 0 || item.TPMActual != 0 ||
		saved.RPMUsed != 0 || saved.TPMReserved != 0 || saved.TPMActual != 0 {
		t.Fatalf("window counters were not clamped: item=%#v saved=%#v", item, saved)
	}
	if !item.WindowStart.Equal(windowStart.UTC()) {
		t.Fatalf("window start = %s, want UTC %s", item.WindowStart, windowStart.UTC())
	}
}

func TestRequestUsageListFromDetailsEmptyRangeSkipsDBOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(errors.New("query callback should not run for empty detail range"))
	})

	start := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	rows, err := NewRequestUsageSummaryRepository(db).ListFromDetails(context.Background(), start, start, config.LoadTimeZone("UTC"))
	if err != nil {
		t.Fatalf("ListFromDetails returned error: %v", err)
	}
	if rows != nil {
		t.Fatalf("empty detail range rows = %#v, want nil", rows)
	}
}

func TestRequestUsageCostSummaryFiltersModelAndKeepsCurrencyOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		rows, ok := tx.Statement.Dest.(*[]RequestUsageDailySummary)
		if !ok {
			tx.AddError(errors.New("unexpected request usage cost summary destination"))
			return
		}
		*rows = []RequestUsageDailySummary{
			{
				CanonicalModelKey:        "gpt-4.1",
				EstimatedCost:            0.25,
				TotalTokens:              10,
				CacheWriteTokens:         2,
				CacheCreationInputTokens: 4,
				CacheWriteCost:           0.05,
				Currency:                 "",
			},
			{
				UpstreamModelName:          "vendor-gpt-4.1-mini",
				EstimatedCost:              0.75,
				TotalTokens:                20,
				CacheWriteTokens:           3,
				CacheCreationInputTokens:   6,
				CacheCreation5mInputTokens: 2,
				CacheCreation1hInputTokens: 4,
				CacheWriteCost:             0.2,
				Currency:                   "EUR",
			},
			{
				CanonicalModelKey: "claude",
				EstimatedCost:     100,
				TotalTokens:       1000,
				Currency:          "JPY",
			},
		}
		tx.Statement.RowsAffected = int64(len(*rows))
	})

	summary, err := NewRequestUsageSummaryRepository(db).CostSummary(context.Background(), RequestUsageCostSummaryQuery{
		ModelKey: "GPT-4.1",
	})
	if err != nil {
		t.Fatalf("CostSummary returned error: %v", err)
	}

	// F22: TotalCost reports the summary currency (EUR) only, not a USD+EUR mix;
	// the USD row is tracked separately in CostByCurrency.
	if summary.TotalCost != 0.75 || summary.TotalTokens != 30 || summary.Currency != "EUR" {
		t.Fatalf("cost summary = %#v, want EUR-consistent total", summary)
	}
	if summary.CostByCurrency["USD"] != 0.25 || summary.CostByCurrency["EUR"] != 0.75 {
		t.Fatalf("per-currency breakdown = %#v", summary.CostByCurrency)
	}
	if summary.CacheWriteTokens != 5 || summary.CacheCreationInputTokens != 10 || summary.CacheCreation5mInputTokens != 2 || summary.CacheCreation1hInputTokens != 4 || summary.CacheWriteTotalTokens != 15 || summary.CacheWriteCost != 0.2 {
		t.Fatalf("cache write summary = %#v, want deduplicated EUR-consistent values", summary)
	}
}

func TestRequestUsageMarkDetailsCleanedSavesCompleteDaysOffline(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		days, ok := tx.Statement.Dest.(*[]RequestUsageSummaryDay)
		if !ok {
			tx.AddError(errors.New("unexpected request usage summary days destination"))
			return
		}
		*days = []RequestUsageSummaryDay{
			{BucketStart: cutoff.AddDate(0, 0, -2), TimeZone: "UTC", Status: RequestUsageSummaryDayStatusComplete},
			{BucketStart: cutoff.AddDate(0, 0, -1), TimeZone: "UTC", Status: RequestUsageSummaryDayStatusComplete},
		}
		tx.Statement.RowsAffected = int64(len(*days))
	})
	var saved []RequestUsageSummaryDay
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		day, ok := tx.Statement.Dest.(*RequestUsageSummaryDay)
		if !ok {
			tx.AddError(errors.New("unexpected request usage summary day update destination"))
			return
		}
		saved = append(saved, *day)
		tx.Statement.RowsAffected = 1
	})

	if err := NewRequestUsageSummaryRepository(db).MarkDetailsCleaned(context.Background(), cutoff, config.LoadTimeZone("UTC")); err != nil {
		t.Fatalf("MarkDetailsCleaned returned error: %v", err)
	}

	if len(saved) != 2 {
		t.Fatalf("saved days = %#v, want 2", saved)
	}
	for _, day := range saved {
		if !day.LastCleanedAt.Valid {
			t.Fatalf("saved day missing cleaned timestamp: %#v", day)
		}
	}
}
