package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestDailyMaintenanceRebuildsAndSkipsCleanupWhenDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	confFile := usageGeneralConfigFile(t, false, 30)
	var logQueries int
	var deletedSummaryDays int
	var completedDays []store.RequestUsageSummaryDay

	db := usageTransactionGormWithCallbacks(t, usageGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.RequestLog:
				logQueries++
				*dest = nil
				tx.Statement.RowsAffected = 0
			case *store.RequestUsageSummaryDay:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(errors.New("unexpected daily maintenance query destination"))
			}
		},
		deleteCallback: func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "RequestUsageDailySummary" {
				deletedSummaryDays++
			}
			tx.Statement.RowsAffected = 0
		},
		create: func(tx *gorm.DB) {
			day, ok := tx.Statement.Dest.(*store.RequestUsageSummaryDay)
			if !ok {
				tx.AddError(errors.New("unexpected daily maintenance create destination"))
				return
			}
			completedDays = append(completedDays, *day)
			tx.Statement.RowsAffected = 1
		},
	})

	service := NewSummaryService(usageStoreWithGorm(t, db), confFile, config.LoadTimeZone("UTC"))
	result, err := service.DailyMaintenance(ctx, now)
	if err != nil {
		t.Fatalf("DailyMaintenance returned error: %v", err)
	}

	yesterday := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	if result.SummarizedDays != 1 || result.DeletedDetailRows != 0 {
		t.Fatalf("DailyMaintenance result = %#v, want one rebuilt day and no cleanup", result)
	}
	if logQueries != 2 || deletedSummaryDays != 1 || len(completedDays) != 1 {
		t.Fatalf("logQueries=%d deletedSummaryDays=%d completedDays=%#v", logQueries, deletedSummaryDays, completedDays)
	}
	if !completedDays[0].BucketStart.Equal(yesterday) ||
		completedDays[0].TimeZone != "UTC" ||
		completedDays[0].Source != "daily" ||
		completedDays[0].Status != store.RequestUsageSummaryDayStatusComplete {
		t.Fatalf("completed day = %#v, want yesterday marked complete from daily maintenance", completedDays[0])
	}
}

func TestDailyMaintenanceCleanupDeletesDetailRowsAndMarksDaysCleaned(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	confFile := usageGeneralConfigFile(t, true, 7)
	oldRequestLogID := uuid.New()
	logQueries := 0
	var deletedSchemas []string
	var completedDays []store.RequestUsageSummaryDay
	var cleanedDays []store.RequestUsageSummaryDay

	db := usageTransactionGormWithCallbacks(t, usageGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.RequestLog:
				logQueries++
				switch logQueries {
				case 1, 2, 3:
					*dest = nil
				case 4:
					*dest = []store.RequestLog{{ID: oldRequestLogID, CreatedAt: now.AddDate(0, 0, -10)}}
				default:
					*dest = nil
				}
				tx.Statement.RowsAffected = int64(len(*dest))
			case *store.RequestUsageSummaryDay:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.RequestUsageSummaryDay:
				*dest = []store.RequestUsageSummaryDay{
					{BucketStart: now.AddDate(0, 0, -9), TimeZone: "UTC", Status: store.RequestUsageSummaryDayStatusComplete},
					{BucketStart: now.AddDate(0, 0, -8), TimeZone: "UTC", Status: store.RequestUsageSummaryDayStatusComplete},
				}
				tx.Statement.RowsAffected = int64(len(*dest))
			default:
				tx.AddError(errors.New("unexpected cleanup query destination"))
			}
		},
		deleteCallback: func(tx *gorm.DB) {
			if tx.Statement.Schema != nil {
				deletedSchemas = append(deletedSchemas, tx.Statement.Schema.Name)
				if tx.Statement.Schema.Name == "RequestLog" {
					tx.Statement.RowsAffected = 1
					return
				}
			}
			tx.Statement.RowsAffected = 0
		},
		create: func(tx *gorm.DB) {
			day, ok := tx.Statement.Dest.(*store.RequestUsageSummaryDay)
			if !ok {
				tx.AddError(errors.New("unexpected cleanup create destination"))
				return
			}
			completedDays = append(completedDays, *day)
			tx.Statement.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			day, ok := tx.Statement.Dest.(*store.RequestUsageSummaryDay)
			if !ok {
				tx.AddError(errors.New("unexpected cleanup update destination"))
				return
			}
			cleanedDays = append(cleanedDays, *day)
			tx.Statement.RowsAffected = 1
		},
	})

	service := NewSummaryService(usageStoreWithGorm(t, db), confFile, config.LoadTimeZone("UTC"))
	result, err := service.DailyMaintenance(ctx, now)
	if err != nil {
		t.Fatalf("DailyMaintenance returned error: %v", err)
	}

	if result.SummarizedDays != 1 || result.DeletedDetailRows != 1 {
		t.Fatalf("DailyMaintenance result = %#v, want one rebuilt day and one deleted detail", result)
	}
	if logQueries != 5 {
		t.Fatalf("logQueries = %d, want rebuild, two summary checks, and two cleanup batches", logQueries)
	}
	if len(completedDays) != 1 || completedDays[0].Source != "daily" {
		t.Fatalf("completedDays = %#v, want daily rebuild marker", completedDays)
	}
	if len(deletedSchemas) != 2 || deletedSchemas[0] != "RequestUsageDailySummary" || deletedSchemas[1] != "RequestLog" {
		t.Fatalf("deletedSchemas = %#v, want summary delete then request log delete", deletedSchemas)
	}
	if len(cleanedDays) != 2 || !cleanedDays[0].LastCleanedAt.Valid || !cleanedDays[1].LastCleanedAt.Valid {
		t.Fatalf("cleanedDays = %#v, want completed days marked as cleaned", cleanedDays)
	}
}
