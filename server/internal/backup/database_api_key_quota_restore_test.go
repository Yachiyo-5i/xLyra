package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"xlyra/server/internal/store"
)

type apiKeyQuotaFlagUpdate struct {
	Column string
	IDs    []uuid.UUID
}

func TestApplyRowToModelRestoresAPIKeyPeriodicQuotaFields(t *testing.T) {
	t.Parallel()

	parsed, err := schema.Parse(&store.APIKey{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse api key schema: %v", err)
	}
	dailyStart := "2026-08-05T00:00:00Z"
	weeklyStart := "2026-08-03T00:00:00Z"
	var apiKey store.APIKey
	err = applyRowToModel(context.Background(), parsed, reflect.ValueOf(&apiKey).Elem(), map[string]any{
		"id":                        "00000000-0000-0000-0000-000000000200",
		"quota_daily_limit":         json.Number("12.5"),
		"quota_daily_used":          json.Number("3.25"),
		"quota_daily_unlimited":     false,
		"quota_daily_window_start":  dailyStart,
		"quota_weekly_limit":        json.Number("75"),
		"quota_weekly_used":         json.Number("21.5"),
		"quota_weekly_unlimited":    false,
		"quota_weekly_window_start": weeklyStart,
	})
	if err != nil {
		t.Fatalf("restore api key quota fields: %v", err)
	}
	if !apiKey.QuotaDailyLimit.Valid || apiKey.QuotaDailyLimit.Float64 != 12.5 || apiKey.QuotaDailyUsed != 3.25 || apiKey.QuotaDailyUnlimited {
		t.Fatalf("daily quota fields = %#v", apiKey)
	}
	if apiKey.QuotaDailyWindowStart == nil || apiKey.QuotaDailyWindowStart.Format(time.RFC3339) != dailyStart {
		t.Fatalf("daily quota window = %v, want %s", apiKey.QuotaDailyWindowStart, dailyStart)
	}
	if !apiKey.QuotaWeeklyLimit.Valid || apiKey.QuotaWeeklyLimit.Float64 != 75 || apiKey.QuotaWeeklyUsed != 21.5 || apiKey.QuotaWeeklyUnlimited {
		t.Fatalf("weekly quota fields = %#v", apiKey)
	}
	if apiKey.QuotaWeeklyWindowStart == nil || apiKey.QuotaWeeklyWindowStart.Format(time.RFC3339) != weeklyStart {
		t.Fatalf("weekly quota window = %v, want %s", apiKey.QuotaWeeklyWindowStart, weeklyStart)
	}
}

func TestRestoreAPIKeyPeriodicQuotaFlagsReappliesExplicitFalseValues(t *testing.T) {
	t.Parallel()

	dailyID := uuid.MustParse("00000000-0000-0000-0000-000000000201")
	weeklyID := uuid.MustParse("00000000-0000-0000-0000-000000000202")
	bothID := uuid.MustParse("00000000-0000-0000-0000-000000000203")
	unlimitedID := uuid.MustParse("00000000-0000-0000-0000-000000000204")
	rows := []map[string]any{
		{"id": dailyID.String(), "quota_daily_unlimited": false, "quota_weekly_unlimited": true},
		{"id": weeklyID.String(), "quota_daily_unlimited": true, "quota_weekly_unlimited": false},
		{"id": bothID.String(), "quota_daily_unlimited": false, "quota_weekly_unlimited": false},
		{"id": unlimitedID.String(), "quota_daily_unlimited": true, "quota_weekly_unlimited": true},
		{"id": uuid.NewString()},
	}

	db := backupOfflineGorm(t)
	updates := make([]apiKeyQuotaFlagUpdate, 0, 2)
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		update, err := capturedAPIKeyQuotaFlagUpdate(tx)
		if err != nil {
			tx.AddError(err)
			return
		}
		updates = append(updates, update)
		tx.RowsAffected = int64(len(update.IDs))
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}

	if err := restoreAPIKeyPeriodicQuotaFlags(context.Background(), db, rows); err != nil {
		t.Fatalf("restore periodic quota flags: %v", err)
	}
	want := []apiKeyQuotaFlagUpdate{
		{Column: "quota_daily_unlimited", IDs: []uuid.UUID{dailyID, bothID}},
		{Column: "quota_weekly_unlimited", IDs: []uuid.UUID{weeklyID, bothID}},
	}
	if !reflect.DeepEqual(updates, want) {
		t.Fatalf("quota flag updates = %#v, want %#v", updates, want)
	}
}

func TestRestoreAPIKeyPeriodicQuotaFlagsRejectsIncompleteUpdate(t *testing.T) {
	t.Parallel()

	db := backupOfflineGorm(t)
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}

	err := restoreAPIKeyPeriodicQuotaFlags(context.Background(), db, []map[string]any{{
		"id":                    uuid.NewString(),
		"quota_daily_unlimited": false,
	}})
	assertBackupErrorContains(t, "restore incomplete quota flag update", err, "updated 0 rows, want 1")
}

func TestAPIKeyIDsWithExplicitFalseRejectsInvalidID(t *testing.T) {
	t.Parallel()

	_, err := apiKeyIDsWithExplicitFalse([]map[string]any{{
		"id":                    "not-a-uuid",
		"quota_daily_unlimited": false,
	}}, "quota_daily_unlimited")
	assertBackupErrorContains(t, "restore invalid quota flag id", err, "invalid api key id")
}

func capturedAPIKeyQuotaFlagUpdate(tx *gorm.DB) (apiKeyQuotaFlagUpdate, error) {
	values, ok := tx.Statement.Dest.(map[string]any)
	if !ok || len(values) != 1 {
		return apiKeyQuotaFlagUpdate{}, fmt.Errorf("unexpected quota flag update values %#v", tx.Statement.Dest)
	}
	var column string
	for name, value := range values {
		flag, ok := value.(bool)
		if !ok || flag {
			return apiKeyQuotaFlagUpdate{}, fmt.Errorf("unexpected quota flag update %s=%#v", name, value)
		}
		column = name
	}
	whereClause, ok := tx.Statement.Clauses["WHERE"]
	if !ok {
		return apiKeyQuotaFlagUpdate{}, fmt.Errorf("quota flag update is missing WHERE clause")
	}
	where, ok := whereClause.Expression.(clause.Where)
	if !ok || len(where.Exprs) != 1 {
		return apiKeyQuotaFlagUpdate{}, fmt.Errorf("unexpected quota flag WHERE clause %#v", whereClause.Expression)
	}
	in, ok := where.Exprs[0].(clause.IN)
	if !ok {
		return apiKeyQuotaFlagUpdate{}, fmt.Errorf("unexpected quota flag predicate %#v", where.Exprs[0])
	}
	ids := make([]uuid.UUID, len(in.Values))
	for i, value := range in.Values {
		id, ok := value.(uuid.UUID)
		if !ok {
			return apiKeyQuotaFlagUpdate{}, fmt.Errorf("unexpected quota flag id %#v", value)
		}
		ids[i] = id
	}
	return apiKeyQuotaFlagUpdate{Column: column, IDs: ids}, nil
}
