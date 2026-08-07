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

func TestAPIKeyQuotaExceededAtUsesCurrentDailyAndWeeklyWindows(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	dailyStart := timeZone.StartOfDay(now)
	weeklyStart := timeZone.StartOfWeek(now)
	key := APIKey{
		QuotaUnlimited:         true,
		QuotaDailyLimit:        sql.NullFloat64{Float64: 10, Valid: true},
		QuotaDailyUsed:         10,
		QuotaDailyUnlimited:    false,
		QuotaDailyWindowStart:  &dailyStart,
		QuotaWeeklyLimit:       sql.NullFloat64{Float64: 50, Valid: true},
		QuotaWeeklyUsed:        49,
		QuotaWeeklyUnlimited:   false,
		QuotaWeeklyWindowStart: &weeklyStart,
	}
	if !key.QuotaExceededAt(now, timeZone) {
		t.Fatal("current daily quota at limit should be exceeded")
	}
	dailyErr := key.QuotaExceededErrorAt(now, timeZone)
	if dailyErr == nil || dailyErr.Scope != "daily" || dailyErr.ResetAt == nil || !dailyErr.ResetAt.Equal(timeZone.StartOfDay(now).AddDate(0, 0, 1)) {
		t.Fatalf("daily quota error = %#v", dailyErr)
	}
	previousDay := dailyStart.AddDate(0, 0, -1)
	key.QuotaDailyWindowStart = &previousDay
	if key.QuotaExceededAt(now, timeZone) {
		t.Fatal("stale daily usage below the current weekly limit should not be exceeded")
	}
	key.QuotaWeeklyUsed = 50
	if !key.QuotaExceededAt(now, timeZone) {
		t.Fatal("current weekly quota at limit should be exceeded")
	}
	weeklyErr := key.QuotaExceededErrorAt(now, timeZone)
	if weeklyErr == nil || weeklyErr.Scope != "weekly" || weeklyErr.ResetAt == nil || !weeklyErr.ResetAt.Equal(timeZone.StartOfWeek(now).AddDate(0, 0, 7)) {
		t.Fatalf("weekly quota error = %#v", weeklyErr)
	}
	key.QuotaUnlimited = false
	key.QuotaLimit = sql.NullFloat64{Float64: 100, Valid: true}
	key.QuotaUsed = 500
	key.QuotaTotalUsed = 100
	totalErr := key.QuotaExceededErrorAt(now, timeZone)
	if totalErr == nil || totalErr.Scope != "total" || totalErr.ResetAt != nil {
		t.Fatalf("total quota error = %#v", totalErr)
	}
}

func TestAPIKeyRepositoryAddUsageAtResetsExpiredWindows(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	previousDay := timeZone.StartOfDay(now).AddDate(0, 0, -1)
	weeklyStart := timeZone.StartOfWeek(now)
	apiKeyID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected periodic quota query destination"))
			return
		}
		*item = APIKey{
			ID: apiKeyID, QuotaUsed: 20, QuotaTotalUsed: 10,
			QuotaDailyUsed: 8, QuotaDailyWindowStart: &previousDay,
			QuotaWeeklyUsed: 12, QuotaWeeklyWindowStart: &weeklyStart,
		}
		tx.Statement.RowsAffected = 1
	})
	var saved map[string]any
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			tx.AddError(errors.New("unexpected periodic quota update destination"))
			return
		}
		saved = updates
		tx.Statement.RowsAffected = 1
	})

	updated, err := NewAPIKeyRepository(db).AddUsageAt(context.Background(), apiKeyID, 2.5, now, timeZone)
	if err != nil {
		t.Fatalf("AddUsageAt returned error: %v", err)
	}
	if updated.QuotaUsed != 22.5 || updated.QuotaTotalUsed != 12.5 || updated.QuotaDailyUsed != 2.5 || updated.QuotaWeeklyUsed != 14.5 {
		t.Fatalf("unexpected updated quota counters: %#v", updated)
	}
	dailyWindowStart, ok := saved["quota_daily_window_start"].(*time.Time)
	if !ok || dailyWindowStart == nil || !dailyWindowStart.Equal(timeZone.StartOfDay(now)) {
		t.Fatalf("daily window start = %v", saved["quota_daily_window_start"])
	}
	if len(saved) != 6 || saved["quota_used"] != 22.5 || saved["quota_total_used"] != 12.5 || saved["quota_daily_used"] != 2.5 || saved["quota_weekly_used"] != 14.5 {
		t.Fatalf("unexpected periodic quota update fields: %#v", saved)
	}
}

func TestAPIKeyRepositoryIncreaseAndResetQuota(t *testing.T) {
	t.Parallel()

	db := storeTransactionGorm(t, "api key quota mutations")
	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	dailyStart := timeZone.StartOfDay(now)
	weeklyStart := timeZone.StartOfWeek(now)
	apiKeyID := uuid.New()
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected quota mutation query destination"))
			return
		}
		*item = APIKey{
			ID:              apiKeyID,
			QuotaLimit:      sql.NullFloat64{Float64: 100, Valid: true},
			QuotaUsed:       90,
			QuotaTotalUsed:  40,
			QuotaDailyLimit: sql.NullFloat64{Float64: 20, Valid: true},
			QuotaDailyUsed:  8, QuotaDailyWindowStart: &dailyStart,
			QuotaWeeklyUsed: 30, QuotaWeeklyWindowStart: &weeklyStart,
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*APIKey); !ok {
			tx.AddError(errors.New("unexpected quota mutation update destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	repo := NewAPIKeyRepository(db)

	increased, err := repo.IncreaseQuotaLimit(context.Background(), apiKeyID, 25)
	if err != nil || !increased.QuotaLimit.Valid || increased.QuotaLimit.Float64 != 125 {
		t.Fatalf("IncreaseQuotaLimit = %#v, %v", increased, err)
	}
	reset, err := repo.ResetQuota(context.Background(), apiKeyID, true, true, false, now, timeZone)
	if err != nil {
		t.Fatalf("ResetPeriodicQuota returned error: %v", err)
	}
	if reset.TotalUsedBefore != 40 || reset.DailyUsedBefore != 8 || reset.WeeklyUsedBefore != 30 || reset.APIKey.QuotaUsed != 90 || reset.APIKey.QuotaTotalUsed != 0 || reset.APIKey.QuotaTotalResetAt == nil || reset.APIKey.QuotaDailyUsed != 0 || reset.APIKey.QuotaWeeklyUsed != 30 {
		t.Fatalf("unexpected reset result: %#v", reset)
	}
	if queryCalls != 2 {
		t.Fatalf("query calls = %d, want 2", queryCalls)
	}
}

func TestBackfillCurrentAPIKeyPeriodicQuotaUsesDailySummaries(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	dailyStart := timeZone.StartOfDay(now)
	previousDay := dailyStart.AddDate(0, 0, -1)
	apiKeyID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]RequestUsageDailySummary:
			*dest = []RequestUsageDailySummary{
				{APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, BucketStart: previousDay, EstimatedCost: 3},
				{APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, BucketStart: dailyStart, EstimatedCost: 2},
			}
		case *[]APIKey:
			*dest = []APIKey{{ID: apiKeyID, QuotaDailyUnlimited: true, QuotaWeeklyUnlimited: true}}
		default:
			tx.AddError(errors.New("unexpected periodic quota backfill destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	var saved APIKey
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected periodic quota backfill update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	if err := backfillCurrentAPIKeyPeriodicQuota(context.Background(), db, timeZone, now); err != nil {
		t.Fatalf("backfillCurrentAPIKeyPeriodicQuota returned error: %v", err)
	}
	if saved.QuotaDailyUsed != 2 || saved.QuotaWeeklyUsed != 5 {
		t.Fatalf("unexpected backfilled quota: %#v", saved)
	}
	if saved.QuotaDailyWindowStart == nil || !saved.QuotaDailyWindowStart.Equal(dailyStart) {
		t.Fatalf("daily window start = %v", saved.QuotaDailyWindowStart)
	}
}

func TestSchemaUpgradeMarkerTableName(t *testing.T) {
	t.Parallel()

	if table := (schemaUpgradeMarker{}).TableName(); table != "schema_upgrade_markers" {
		t.Fatalf("schema upgrade marker table = %q", table)
	}
}

func TestAPIKeyPeriodicQuotaBackfillRetriesWhenMarkerWriteFails(t *testing.T) {
	t.Parallel()

	db := storeTransactionGorm(t, "periodic quota backfill retry")
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *schemaUpgradeMarker:
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]RequestUsageDailySummary:
			*dest = nil
			tx.Statement.RowsAffected = 0
		case *[]APIKey:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected periodic quota retry query destination"))
		}
	})
	markerWrites := 0
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*schemaUpgradeMarker); !ok {
			tx.AddError(errors.New("unexpected periodic quota retry create destination"))
			return
		}
		markerWrites++
		if markerWrites == 1 {
			tx.AddError(errors.New("marker write failed"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	timeZone := config.LoadTimeZone("Asia/Shanghai")
	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	if err := ensureAPIKeyPeriodicQuotaBackfill(context.Background(), db, timeZone, now); err == nil {
		t.Fatal("first periodic quota backfill should fail when marker write fails")
	}
	if err := ensureAPIKeyPeriodicQuotaBackfill(context.Background(), db, timeZone, now); err != nil {
		t.Fatalf("retry periodic quota backfill: %v", err)
	}
	if markerWrites != 2 {
		t.Fatalf("periodic quota marker writes = %d, want 2", markerWrites)
	}
}

func TestAPIKeyTotalQuotaBackfillPreservesAccumulatedUsage(t *testing.T) {
	t.Parallel()

	db := storeTransactionGorm(t, "total quota backfill")
	apiKeyID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *schemaUpgradeMarker:
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]APIKey:
			*dest = []APIKey{{ID: apiKeyID, QuotaUsed: 42.5}}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected total quota backfill query destination"))
		}
	})
	var saved APIKey
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected total quota backfill update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*schemaUpgradeMarker); !ok {
			tx.AddError(errors.New("unexpected total quota backfill create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	if err := ensureAPIKeyTotalQuotaBackfill(context.Background(), db, now); err != nil {
		t.Fatalf("ensureAPIKeyTotalQuotaBackfill returned error: %v", err)
	}
	if saved.QuotaUsed != 42.5 || saved.QuotaTotalUsed != 42.5 {
		t.Fatalf("backfilled quota = accumulated:%f total:%f, want 42.5 each", saved.QuotaUsed, saved.QuotaTotalUsed)
	}
}
