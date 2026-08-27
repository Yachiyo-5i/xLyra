package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestAPIKeyRepositoryResetsTotalOnceForOAuthWeeklyReset(t *testing.T) {
	apiKeyID := uuid.New()
	oauthConnectionID := uuid.New()
	upstreamResetAt := time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC)
	now := upstreamResetAt.Add(time.Minute)
	state := APIKey{
		ID:                         apiKeyID,
		AutoResetOAuthConnectionID: &oauthConnectionID,
		QuotaLimit:                 sql.NullFloat64{Float64: 100, Valid: true},
		QuotaTotalUsed:             42,
		QuotaDailyUsed:             7,
		QuotaWeeklyUsed:            21,
	}
	db := storeTransactionGorm(t, "oauth auto reset")
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch destination := tx.Statement.Dest.(type) {
		case *[]APIKey:
			*destination = []APIKey{state}
			tx.Statement.RowsAffected = 1
		case *APIKey:
			if locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking); !ok || locking.Strength != clause.LockingStrengthUpdate {
				tx.AddError(errors.New("oauth auto reset must lock api key row"))
				return
			}
			*destination = state
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected oauth auto reset query destination"))
		}
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			tx.AddError(errors.New("unexpected oauth auto reset update destination"))
			return
		}
		if updates["quota_total_used"] != 0 {
			tx.AddError(errors.New("oauth auto reset must clear total usage"))
			return
		}
		if updates["quota_daily_used"] != nil || updates["quota_weekly_used"] != nil {
			tx.AddError(errors.New("oauth auto reset must preserve periodic usage"))
			return
		}
		state.QuotaTotalUsed = 0
		resetAt, ok := updates["quota_total_reset_at"].(*time.Time)
		if !ok || resetAt == nil || !resetAt.Equal(now) {
			tx.AddError(errors.New("oauth auto reset must record reset time"))
			return
		}
		lastResetAt, ok := updates["auto_reset_last_reset_at"].(time.Time)
		if !ok || !lastResetAt.Equal(upstreamResetAt) {
			tx.AddError(errors.New("oauth auto reset must record upstream reset time"))
			return
		}
		state.AutoResetLastResetAt = &lastResetAt
		tx.Statement.RowsAffected = 1
	})

	repo := NewAPIKeyRepository(db)
	resetCount, err := repo.ResetTotalForOAuthWeeklyReset(context.Background(), oauthConnectionID, upstreamResetAt, now)
	if err != nil {
		t.Fatalf("first reset returned error: %v", err)
	}
	if resetCount != 1 || state.QuotaTotalUsed != 0 || state.QuotaDailyUsed != 7 || state.QuotaWeeklyUsed != 21 {
		t.Fatalf("first reset count/state = %d/%#v", resetCount, state)
	}

	resetCount, err = repo.ResetTotalForOAuthWeeklyReset(context.Background(), oauthConnectionID, upstreamResetAt, now)
	if err != nil {
		t.Fatalf("duplicate reset returned error: %v", err)
	}
	if resetCount != 0 {
		t.Fatalf("duplicate reset count = %d, want 0", resetCount)
	}
}
