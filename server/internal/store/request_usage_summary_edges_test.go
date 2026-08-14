package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
)

func TestRequestUsageSummaryIncrementBranchesOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db := storeTransactionGorm(t, "request_usage_summary")
	queryCalls := 0
	var created []RequestUsageDailySummary
	var saved []RequestUsageDailySummary
	existingStart := time.Date(2026, 6, 20, 8, 0, 0, 0, time.UTC)
	existingEnd := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*RequestUsageDailySummary)
		if !ok {
			tx.AddError(errors.New("unexpected request usage summary query destination"))
			return
		}
		queryCalls++
		if queryCalls == 1 {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*item = RequestUsageDailySummary{
			SummaryKey:       "existing-summary",
			RequestCount:     2,
			SuccessCount:     1,
			FailureCount:     1,
			PromptTokens:     3,
			CompletionTokens: 4,
			TotalTokens:      7,
			EstimatedCost:    0.5,
			FirstRequestAt:   sql.NullTime{Time: existingStart, Valid: true},
			LastRequestAt:    sql.NullTime{Time: existingEnd, Valid: true},
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*RequestUsageDailySummary)
		if !ok {
			tx.AddError(errors.New("unexpected request usage summary create destination"))
			return
		}
		created = append(created, *item)
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*RequestUsageDailySummary)
		if !ok {
			tx.AddError(errors.New("unexpected request usage summary update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	requestLogID := uuid.New()
	requestAt := time.Date(2026, 6, 20, 10, 30, 0, 0, time.UTC)
	repo := NewRequestUsageSummaryRepository(db)
	if err := repo.Increment(ctx, RequestUsageSummaryIncrement{
		RequestLog: RequestLog{
			ID:         requestLogID,
			RequestID:  "request-increment",
			Endpoint:   "/v1/chat/completions",
			StatusCode: 200,
			Success:    true,
			LatencyMS:  sql.NullInt64{Int64: 120, Valid: true},
			CreatedAt:  requestAt,
		},
		UsageRecord: &UsageRecord{
			ID:               uuid.New(),
			RequestLogID:     requestLogID,
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    sql.NullFloat64{Float64: 0.25, Valid: true},
			Currency:         "EUR",
		},
		TimeZone: config.LoadTimeZone("UTC"),
	}); err != nil {
		t.Fatalf("Increment returned error: %v", err)
	}

	deltaStart := existingStart.Add(-time.Hour)
	deltaEnd := existingEnd.Add(time.Hour)
	if err := repo.incrementSummary(ctx, RequestUsageDailySummary{
		SummaryKey:       "existing-summary",
		RequestCount:     1,
		SuccessCount:     1,
		PromptTokens:     6,
		CompletionTokens: 7,
		TotalTokens:      13,
		EstimatedCost:    0.75,
		FirstRequestAt:   sql.NullTime{Time: deltaStart, Valid: true},
		LastRequestAt:    sql.NullTime{Time: deltaEnd, Valid: true},
	}); err != nil {
		t.Fatalf("incrementSummary returned error: %v", err)
	}

	if len(created) != 1 || created[0].RequestCount != 1 || created[0].SuccessCount != 1 ||
		created[0].TotalTokens != 15 || created[0].Currency != "EUR" ||
		!created[0].FirstRequestAt.Time.Equal(requestAt) {
		t.Fatalf("created summary = %#v, want usage-backed increment row", created)
	}
	if len(saved) != 1 || saved[0].RequestCount != 3 || saved[0].SuccessCount != 2 ||
		saved[0].FailureCount != 1 || saved[0].TotalTokens != 20 ||
		saved[0].EstimatedCost != 1.25 || !saved[0].FirstRequestAt.Time.Equal(deltaStart) ||
		!saved[0].LastRequestAt.Time.Equal(deltaEnd) {
		t.Fatalf("saved summary = %#v, want aggregated existing row", saved)
	}
}

func TestRequestUsageSummaryRebuildAndCleanupOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	requestLogID := uuid.New()
	db := storeTransactionGorm(t, "request_usage_summary")
	requestLogQueries := 0
	usageQueries := 0
	dayQueries := 0
	deleteSummaryCalls := 0
	var rebuiltRows []RequestUsageDailySummary
	var completedDay RequestUsageSummaryDay
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]RequestLog:
			requestLogQueries++
			if requestLogQueries == 1 {
				*dest = []RequestLog{{ID: uuid.New(), CreatedAt: base}}
			} else {
				*dest = []RequestLog{{
					ID:             requestLogID,
					RequestID:      "request-rebuild",
					Endpoint:       "/v1/messages",
					StatusCode:     503,
					Success:        false,
					ErrorType:      sql.NullString{String: "upstream", Valid: true},
					RequestTokens:  sql.NullInt64{Int64: 2, Valid: true},
					ResponseTokens: sql.NullInt64{Int64: 3, Valid: true},
					CreatedAt:      base.Add(2 * time.Hour),
				}}
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *[]UsageRecord:
			usageQueries++
			*dest = []UsageRecord{{
				ID:               uuid.New(),
				RequestLogID:     requestLogID,
				PromptTokens:     7,
				CompletionTokens: 8,
				TotalTokens:      15,
				EstimatedCost:    sql.NullFloat64{Float64: 0.33, Valid: true},
				Currency:         "JPY",
			}}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *RequestUsageSummaryDay:
			dayQueries++
			tx.AddError(gorm.ErrRecordNotFound)
		default:
			tx.AddError(errors.New("unexpected request usage rebuild query destination"))
		}
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*RequestUsageDailySummary); !ok {
			tx.AddError(errors.New("unexpected request usage summary delete destination"))
			return
		}
		deleteSummaryCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]RequestUsageDailySummary:
			rebuiltRows = append(rebuiltRows, (*dest)...)
		case *RequestUsageSummaryDay:
			completedDay = *dest
		default:
			tx.AddError(errors.New("unexpected request usage rebuild create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	rebuilt, err := NewRequestUsageSummaryRepository(db).EnsureSummariesThrough(
		ctx,
		base.AddDate(0, 0, 1).Add(12*time.Hour),
		config.LoadTimeZone("UTC"),
		"request_usage_summary",
	)
	if err != nil {
		t.Fatalf("EnsureSummariesThrough returned error: %v", err)
	}

	if rebuilt != 1 || requestLogQueries != 2 || usageQueries != 1 || dayQueries != 2 ||
		deleteSummaryCalls != 1 || len(rebuiltRows) != 1 {
		t.Fatalf("rebuilt=%d requestLogQueries=%d usageQueries=%d dayQueries=%d deleteSummaryCalls=%d rows=%#v",
			rebuilt, requestLogQueries, usageQueries, dayQueries, deleteSummaryCalls, rebuiltRows)
	}
	if rebuiltRows[0].FailureCount != 1 || rebuiltRows[0].TotalTokens != 15 ||
		rebuiltRows[0].EstimatedCost != 0.33 || rebuiltRows[0].Currency != "JPY" {
		t.Fatalf("rebuilt row = %#v, want usage-backed failure summary", rebuiltRows[0])
	}
	if completedDay.Status != RequestUsageSummaryDayStatusComplete || completedDay.Source != "request_usage_summary" ||
		completedDay.RequestCount != 1 || !completedDay.LastSummarizedAt.Valid {
		t.Fatalf("completed day = %#v, want complete day marker", completedDay)
	}

	cleanupDB := storeRepositoryOfflineGorm(t)
	cleanupQueries := 0
	cleanupDeletes := 0
	storeReplaceQueryCallback(t, cleanupDB, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]RequestLog)
		if !ok {
			tx.AddError(errors.New("unexpected request detail cleanup query destination"))
			return
		}
		cleanupQueries++
		if cleanupQueries <= 2 {
			*items = []RequestLog{{ID: uuid.New(), CreatedAt: base.Add(-time.Duration(cleanupQueries) * time.Hour)}}
		} else {
			*items = nil
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	storeReplaceDeleteCallback(t, cleanupDB, func(tx *gorm.DB) {
		cleanupDeletes++
		tx.Statement.RowsAffected = 1
	})

	deleted, err := NewRequestUsageSummaryRepository(cleanupDB).DeleteDetailsBefore(ctx, base)
	if err != nil {
		t.Fatalf("DeleteDetailsBefore returned error: %v", err)
	}
	if deleted != 2 || cleanupQueries != 3 || cleanupDeletes != 2 {
		t.Fatalf("deleted=%d cleanupQueries=%d cleanupDeletes=%d, want two cleanup batches then empty", deleted, cleanupQueries, cleanupDeletes)
	}
}

func TestRequestUsageCachedTokensBackfillDoesNotMarkIncompleteCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	day := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	requestLogID := uuid.New()
	db := storeTransactionGorm(t, "request_usage_cached_tokens_backfill")
	markerQueries := 0
	markerCreates := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *schemaUpgradeMarker:
			markerQueries++
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]RequestLog:
			*dest = []RequestLog{{ID: requestLogID, CreatedAt: day.Add(time.Hour)}}
			tx.Statement.RowsAffected = 1
		case *[]UsageRecord:
			*dest = []UsageRecord{{
				ID:           uuid.New(),
				RequestLogID: requestLogID,
				CachedTokens: sql.NullInt64{Int64: 3, Valid: true},
			}}
			tx.Statement.RowsAffected = 1
		case *RequestUsageSummaryDay:
			*dest = RequestUsageSummaryDay{
				BucketStart:  day,
				TimeZone:     "UTC",
				Status:       RequestUsageSummaryDayStatusComplete,
				RequestCount: 2,
			}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected cached tokens backfill query destination"))
		}
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*schemaUpgradeMarker); ok {
			markerCreates++
		}
		tx.Statement.RowsAffected = 1
	})
	repo := NewRequestUsageSummaryRepository(db)
	for attempt := 0; attempt < 2; attempt++ {
		_, err := repo.BackfillCachedTokens(ctx, day.AddDate(0, 0, 1).Add(time.Hour), config.LoadTimeZone("UTC"))
		if err == nil || !strings.Contains(err.Error(), "summary coverage incomplete") {
			t.Fatalf("BackfillCachedTokens attempt %d error = %v, want incomplete coverage", attempt+1, err)
		}
	}
	if markerQueries != 2 || markerCreates != 0 {
		t.Fatalf("marker queries=%d creates=%d, want retry without completion marker", markerQueries, markerCreates)
	}
}

func TestRequestUsageCacheWriteTokensBackfillDoesNotMarkIncompleteCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	day := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	requestLogID := uuid.New()
	db := storeTransactionGorm(t, "request_usage_cache_write_tokens_backfill")
	markerQueries := 0
	markerCreates := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *schemaUpgradeMarker:
			markerQueries++
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]RequestLog:
			*dest = []RequestLog{{
				ID:        requestLogID,
				CreatedAt: day.Add(time.Hour),
				Metadata:  JSON(`{"cost_calculation":{"cache_write_tokens":3}}`),
			}}
			tx.Statement.RowsAffected = 1
		case *[]UsageRecord:
			*dest = []UsageRecord{{ID: uuid.New(), RequestLogID: requestLogID}}
			tx.Statement.RowsAffected = 1
		case *RequestUsageSummaryDay:
			*dest = RequestUsageSummaryDay{
				BucketStart:  day,
				TimeZone:     "UTC",
				Status:       RequestUsageSummaryDayStatusComplete,
				RequestCount: 2,
			}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected cache write tokens backfill query destination"))
		}
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*schemaUpgradeMarker); ok {
			markerCreates++
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*UsageRecord)
		if !ok || !item.CacheWriteTokens.Valid || item.CacheWriteTokens.Int64 != 3 {
			tx.AddError(errors.New("unexpected cache write tokens backfill update"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	repo := NewRequestUsageSummaryRepository(db)
	for attempt := 0; attempt < 2; attempt++ {
		_, err := repo.BackfillCacheWriteTokens(ctx, day.AddDate(0, 0, 1).Add(time.Hour), config.LoadTimeZone("UTC"))
		if err == nil || !strings.Contains(err.Error(), "summary coverage incomplete") {
			t.Fatalf("BackfillCacheWriteTokens attempt %d error = %v, want incomplete coverage", attempt+1, err)
		}
	}
	if markerQueries != 2 || markerCreates != 0 {
		t.Fatalf("marker queries=%d creates=%d, want retry without completion marker", markerQueries, markerCreates)
	}
}

func TestSiteModelRepositoryBranchesOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	siteID := uuid.New()
	updateB := uuid.New()
	updateA := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	sliceQueries := 0
	singleQueries := 0
	var saved []SiteModel
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]SiteModel:
			sliceQueries++
			*dest = []SiteModel{
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "stale", Status: "available"},
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "manual", Status: "available", Capabilities: JSON(`{"manual":true}`)},
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "seen", Status: "available"},
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "disabled", Status: "disabled"},
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *SiteModel:
			singleQueries++
			if singleQueries == 1 {
				*dest = SiteModel{ID: updateB, SiteID: siteID, UpstreamName: "b-model", Status: "available"}
			} else {
				*dest = SiteModel{ID: updateA, SiteID: siteID, UpstreamName: "a-model", Status: "available"}
			}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected site model query destination"))
		}
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteModel)
		if !ok {
			tx.AddError(errors.New("unexpected site model update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteModelRepository(db)
	listed, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if err := repo.MarkUnavailableExcept(ctx, siteID, []string{"seen"}); err != nil {
		t.Fatalf("MarkUnavailableExcept returned error: %v", err)
	}
	updated, err := repo.UpdateStatuses(ctx, siteID, []uuid.UUID{updateB, updateA}, "disabled")
	if err != nil {
		t.Fatalf("UpdateStatuses returned error: %v", err)
	}

	if len(listed) != 4 || listed[0].UpstreamName != "disabled" || listed[3].UpstreamName != "stale" {
		t.Fatalf("listed site models = %#v, want upstream-name sort", listed)
	}
	if sliceQueries != 2 || singleQueries != 2 || len(saved) != 3 {
		t.Fatalf("sliceQueries=%d singleQueries=%d saved=%#v", sliceQueries, singleQueries, saved)
	}
	if saved[0].UpstreamName != "stale" || saved[0].Status != "unavailable" {
		t.Fatalf("first saved model = %#v, want only stale model marked unavailable", saved[0])
	}
	if len(updated) != 2 || updated[0].UpstreamName != "a-model" || updated[1].UpstreamName != "b-model" ||
		saved[1].Status != "disabled" || saved[2].Status != "disabled" {
		t.Fatalf("updated=%#v saved=%#v, want sorted disabled updates", updated, saved)
	}
}

func TestSiteAPIKeyModelAndStateBranchesOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	siteA := uuid.MustParse("00000000-0000-0000-0000-000000000021")
	siteB := uuid.MustParse("00000000-0000-0000-0000-000000000022")
	credentialA := uuid.MustParse("00000000-0000-0000-0000-000000000031")
	credentialB := uuid.MustParse("00000000-0000-0000-0000-000000000032")
	db := storeRepositoryOfflineGorm(t)
	stateQueries := 0
	var deletedSchemas []string
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]SiteAPIKeyModel:
			*dest = []SiteAPIKeyModel{
				{SiteID: siteB, SiteCredentialID: credentialA, UpstreamModelName: "b-model"},
				{SiteID: siteA, SiteCredentialID: credentialB, UpstreamModelName: "z-model"},
				{SiteID: siteA, SiteCredentialID: credentialA, UpstreamModelName: "b-model"},
				{SiteID: siteA, SiteCredentialID: credentialA, UpstreamModelName: "a-model"},
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *[]SiteAPIKeyState:
			stateQueries++
			if stateQueries == 1 {
				*dest = []SiteAPIKeyState{
					{SiteID: siteA, SiteCredentialID: uuid.New(), Name: "zeta"},
					{SiteID: siteA, SiteCredentialID: uuid.New(), Name: "beta", UpstreamID: sql.NullInt64{Int64: 20, Valid: true}},
					{SiteID: siteA, SiteCredentialID: uuid.New(), Name: "alpha", UpstreamID: sql.NullInt64{Int64: 10, Valid: true}},
				}
			} else {
				*dest = []SiteAPIKeyState{
					{SiteID: siteB, SiteCredentialID: uuid.New(), Name: "b-site", UpstreamID: sql.NullInt64{Int64: 1, Valid: true}},
					{SiteID: siteA, SiteCredentialID: uuid.New(), Name: "nil-upstream"},
					{SiteID: siteA, SiteCredentialID: uuid.New(), Name: "a-site", UpstreamID: sql.NullInt64{Int64: 2, Valid: true}},
				}
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		default:
			tx.AddError(errors.New("unexpected site api key query destination"))
		}
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deletedSchemas = append(deletedSchemas, tx.Statement.Schema.Name)
		tx.Statement.RowsAffected = 1
	})

	modelRepo := NewSiteAPIKeyModelRepository(db)
	models, err := modelRepo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll site api key models returned error: %v", err)
	}
	if err := modelRepo.DeleteByCredential(ctx, credentialA); err != nil {
		t.Fatalf("DeleteByCredential site api key models returned error: %v", err)
	}

	stateRepo := NewSiteAPIKeyStateRepository(db)
	statesBySite, err := stateRepo.ListBySite(ctx, siteA)
	if err != nil {
		t.Fatalf("ListBySite site api key states returned error: %v", err)
	}
	statesAll, err := stateRepo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll site api key states returned error: %v", err)
	}
	if err := stateRepo.DeleteByCredential(ctx, credentialA); err != nil {
		t.Fatalf("DeleteByCredential site api key state returned error: %v", err)
	}

	if len(models) != 4 || models[0].SiteID != siteA || models[0].SiteCredentialID != credentialA ||
		models[0].UpstreamModelName != "a-model" || models[3].SiteID != siteB {
		t.Fatalf("models=%#v, want site, credential, upstream sort", models)
	}
	if len(statesBySite) != 3 || statesBySite[0].Name != "alpha" ||
		statesBySite[1].Name != "beta" || statesBySite[2].Name != "zeta" {
		t.Fatalf("statesBySite=%#v, want upstream id then name sort", statesBySite)
	}
	if len(statesAll) != 3 || statesAll[0].SiteID != siteA || statesAll[0].Name != "a-site" ||
		statesAll[1].Name != "nil-upstream" || statesAll[2].SiteID != siteB {
		t.Fatalf("statesAll=%#v, want site then upstream sort", statesAll)
	}
	if len(deletedSchemas) != 2 || deletedSchemas[0] != "SiteAPIKeyModel" || deletedSchemas[1] != "SiteAPIKeyState" {
		t.Fatalf("deletedSchemas=%#v, want model then state deletes", deletedSchemas)
	}
}

func TestSiteGroupSetGroupSitesDeduplicatesOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	groupID := uuid.New()
	siteA := uuid.New()
	siteB := uuid.New()
	db := storeTransactionGorm(t, "request_usage_summary")
	deleteCalls := 0
	var created []SiteGroupSite
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*SiteGroupSite); !ok {
			tx.AddError(errors.New("unexpected site group site delete destination"))
			return
		}
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteGroupSite)
		if !ok {
			tx.AddError(errors.New("unexpected site group site create destination"))
			return
		}
		created = append(created, *item)
		tx.Statement.RowsAffected = 1
	})

	if err := NewSiteGroupRepository(db).SetGroupSites(ctx, groupID, []uuid.UUID{siteA, uuid.Nil, siteA, siteB}); err != nil {
		t.Fatalf("SetGroupSites returned error: %v", err)
	}

	if deleteCalls != 1 || len(created) != 2 {
		t.Fatalf("deleteCalls=%d created=%#v, want one clear and two unique links", deleteCalls, created)
	}
	if created[0].GroupID != groupID || created[0].SiteID != siteA ||
		created[1].GroupID != groupID || created[1].SiteID != siteB {
		t.Fatalf("created=%#v, want deduped non-nil group sites in input order", created)
	}
}

func TestPricingAndRouteCandidateBranchesOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	siteID := uuid.New()
	siteModelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	modelPricingQueries := 0
	pricingGroupQueries := 0
	var createdModelPricings []SiteModelPricing
	var savedModelPricings []SiteModelPricing
	var createdPricingGroups []SitePricingGroup
	var savedPricingGroups []SitePricingGroup
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *SiteModelPricing:
			modelPricingQueries++
			switch modelPricingQueries {
			case 1:
				tx.AddError(gorm.ErrRecordNotFound)
			case 2:
				*dest = SiteModelPricing{
					ID:             uuid.New(),
					SiteID:         siteID,
					ModelName:      "manual-model",
					GroupName:      "default",
					ManualOverride: true,
					Available:      true,
					Currency:       "USD",
				}
				tx.Statement.RowsAffected = 1
			default:
				*dest = SiteModelPricing{
					ID:        uuid.New(),
					SiteID:    siteID,
					ModelName: "auto-model",
					GroupName: "default",
					Available: true,
					Currency:  "USD",
				}
				tx.Statement.RowsAffected = 1
			}
		case *SitePricingGroup:
			pricingGroupQueries++
			if pricingGroupQueries == 1 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*dest = SitePricingGroup{ID: uuid.New(), SiteID: siteID, GroupName: "default", Available: true}
			tx.Statement.RowsAffected = 1
		case *[]SitePricingGroup:
			*dest = []SitePricingGroup{
				{ID: uuid.New(), SiteID: siteID, GroupName: "seen", Available: true},
				{ID: uuid.New(), SiteID: siteID, GroupName: "stale", Available: true},
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *CanonicalModel:
			*dest = CanonicalModel{ID: uuid.New(), ModelKey: "archived-canonical", Status: "archived"}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected pricing or route candidate query destination"))
		}
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *SiteModelPricing:
			createdModelPricings = append(createdModelPricings, *dest)
		case *SitePricingGroup:
			createdPricingGroups = append(createdPricingGroups, *dest)
		default:
			tx.AddError(errors.New("unexpected pricing create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *SiteModelPricing:
			savedModelPricings = append(savedModelPricings, *dest)
		case *SitePricingGroup:
			savedPricingGroups = append(savedPricingGroups, *dest)
		default:
			tx.AddError(errors.New("unexpected pricing update destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	modelPricingRepo := NewSiteModelPricingRepository(db)
	createdModelPricing, err := modelPricingRepo.Upsert(ctx, UpsertSiteModelPricingParams{
		SiteID:        siteID,
		SiteModelID:   siteModelID,
		ModelName:     "created-model",
		GroupName:     "default",
		BillingType:   "tokens",
		Currency:      "USD",
		GroupRatio:    1.25,
		ModelRatio:    0.5,
		PricingSource: "",
		Available:     true,
	})
	if err != nil {
		t.Fatalf("create model pricing Upsert returned error: %v", err)
	}
	manualModelPricing, err := modelPricingRepo.Upsert(ctx, UpsertSiteModelPricingParams{
		SiteID:         siteID,
		ModelName:      "manual-model",
		GroupName:      "default",
		Currency:       "CNY",
		PreserveManual: true,
	})
	if err != nil {
		t.Fatalf("preserve manual model pricing Upsert returned error: %v", err)
	}
	updatedModelPricing, err := modelPricingRepo.Upsert(ctx, UpsertSiteModelPricingParams{
		SiteID:        siteID,
		ModelName:     "auto-model",
		GroupName:     "default",
		BillingType:   "tokens",
		Currency:      "EUR",
		GroupRatio:    2,
		ModelPrice:    0.02,
		PricingSource: "sync",
		Available:     false,
		Raw:           JSON(`{"source":"sync"}`),
	})
	if err != nil {
		t.Fatalf("update model pricing Upsert returned error: %v", err)
	}

	pricingGroupRepo := NewSitePricingGroupRepository(db)
	createdGroup, err := pricingGroupRepo.Upsert(ctx, UpsertSitePricingGroupParams{
		SiteID:      siteID,
		GroupName:   "created",
		DisplayName: "Created",
		Ratio:       1.1,
		IsAuto:      true,
		Available:   true,
	})
	if err != nil {
		t.Fatalf("create pricing group Upsert returned error: %v", err)
	}
	updatedGroup, err := pricingGroupRepo.Upsert(ctx, UpsertSitePricingGroupParams{
		SiteID:      siteID,
		GroupName:   "default",
		DisplayName: "Default",
		Ratio:       1.5,
		IsAuto:      false,
		Available:   false,
		Raw:         JSON(`{"group":true}`),
	})
	if err != nil {
		t.Fatalf("update pricing group Upsert returned error: %v", err)
	}
	if err := pricingGroupRepo.MarkUnavailableExcept(ctx, siteID, []string{"seen"}); err != nil {
		t.Fatalf("MarkUnavailableExcept pricing group returned error: %v", err)
	}
	routeRows, err := NewRouteCandidateRepository(db).ListByCanonicalModel(ctx, uuid.New())
	if err != nil {
		t.Fatalf("ListByCanonicalModel returned error: %v", err)
	}

	if len(createdModelPricings) != 1 || !createdModelPricing.SiteModelID.Valid ||
		string(createdModelPricings[0].Raw) != "{}" || createdModelPricings[0].PricingSource != "unknown" {
		t.Fatalf("created model pricing = item:%#v captured:%#v", createdModelPricing, createdModelPricings)
	}
	if !manualModelPricing.ManualOverride || manualModelPricing.Currency != "USD" ||
		len(savedModelPricings) != 1 || updatedModelPricing.Currency != "EUR" ||
		string(savedModelPricings[0].Raw) != `{"source":"sync"}` || !savedModelPricings[0].ModelPrice.Valid {
		t.Fatalf("manual=%#v updated=%#v savedModelPricings=%#v", manualModelPricing, updatedModelPricing, savedModelPricings)
	}
	if len(createdPricingGroups) != 1 || string(createdPricingGroups[0].Raw) != "{}" ||
		createdGroup.Ratio != 1.1 || len(savedPricingGroups) != 2 ||
		updatedGroup.Ratio != 1.5 || savedPricingGroups[1].GroupName != "stale" ||
		savedPricingGroups[1].Available || !savedPricingGroups[1].LastSyncedAt.Valid {
		t.Fatalf("createdGroup=%#v updatedGroup=%#v savedPricingGroups=%#v", createdGroup, updatedGroup, savedPricingGroups)
	}
	if len(routeRows) != 0 {
		t.Fatalf("routeRows=%#v, want inactive canonical model to short-circuit", routeRows)
	}
}
