package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
)

func TestUsageRecordRepositoryConstructorKeepsDB(t *testing.T) {
	t.Parallel()

	nilRepo := NewUsageRecordRepository(nil)
	if nilRepo.db != nil {
		t.Fatalf("nil usage record repository db = %#v, want nil", nilRepo.db)
	}

	db := storeRepositoryOfflineGorm(t)
	repo := NewUsageRecordRepository(db)
	if repo.db != db {
		t.Fatalf("usage record repository db = %#v, want %#v", repo.db, db)
	}
}

func TestUsageRecordRepositoryCreateBuildsRecordWithoutRealWrite(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	captured := storeCaptureCreate[UsageRecord](t, db, "usage record", func(item *UsageRecord) {
		item.ID = uuid.MustParse("00000000-0000-0000-0000-000000000999")
	})

	requestID := uuid.New()
	apiKeyID := uuid.New()
	siteID := uuid.New()
	canonicalID := uuid.New()
	item, err := NewUsageRecordRepository(db).Create(context.Background(), CreateUsageRecordParams{
		RequestLogID:     requestID,
		APIKeyID:         &apiKeyID,
		SiteID:           siteID,
		CanonicalModelID: uuid.NullUUID{UUID: canonicalID, Valid: true},
		PromptTokens:     11,
		CompletionTokens: 22,
		TotalTokens:      33,
		CachedTokens:     7,
		EstimatedCost:    float32(0.125),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if item.ID == uuid.Nil || item.RequestLogID != requestID || item.Currency != "USD" {
		t.Fatalf("created usage record defaults/identity = %#v", item)
	}
	if captured.APIKeyID.UUID != apiKeyID || !captured.APIKeyID.Valid ||
		captured.SiteID.UUID != siteID || !captured.SiteID.Valid ||
		captured.CanonicalModelID.UUID != canonicalID || !captured.CanonicalModelID.Valid {
		t.Fatalf("captured nullable IDs = %#v", captured)
	}
	if captured.PromptTokens != 11 || captured.CompletionTokens != 22 || captured.TotalTokens != 33 || !captured.CachedTokens.Valid || captured.CachedTokens.Int64 != 7 {
		t.Fatalf("captured tokens = %#v", captured)
	}
	if !captured.EstimatedCost.Valid || captured.EstimatedCost.Float64 != float64(float32(0.125)) {
		t.Fatalf("captured cost = %#v", captured.EstimatedCost)
	}
}

func TestUsageRecordRepositoryCreateWrapsCreateError(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	createErr := errors.New("usage insert stopped")
	storeCreateError(t, db, createErr)

	_, err := NewUsageRecordRepository(db).Create(context.Background(), CreateUsageRecordParams{
		RequestLogID: uuid.New(),
		Currency:     "EUR",
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("Create error = %v, want wrapped create error", err)
	}
}

func TestRequestUsageSummaryFromRequestLogNormalizesDefaultDimensions(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 6, 23, 7, 45, 0, 0, time.UTC)
	row := summaryFromRequestLog(RequestLog{
		ID:                uuid.New(),
		RequestID:         "req-defaults",
		Endpoint:          "  ",
		StatusCode:        503,
		Success:           false,
		ErrorType:         sql.NullString{String: "  ", Valid: true},
		RequestTokens:     sql.NullInt64{Int64: 2, Valid: true},
		ResponseTokens:    sql.NullInt64{Int64: 3, Valid: true},
		LatencyMS:         sql.NullInt64{Int64: 50, Valid: true},
		UpstreamLatencyMS: sql.NullInt64{Int64: 40, Valid: true},
		CreatedAt:         createdAt,
	}, &UsageRecord{
		PromptTokens:     4,
		CompletionTokens: 5,
		TotalTokens:      9,
	}, config.LoadTimeZone("UTC"), requestUsageSummaryContext{})

	if row.Endpoint != requestUsageSummaryNoneKey || row.ErrorType != "unknown" {
		t.Fatalf("blank endpoint/error should normalize to defaults, got endpoint=%q error=%q", row.Endpoint, row.ErrorType)
	}
	if row.Currency != requestUsageSummaryDefaultCurrency {
		t.Fatalf("empty usage currency should default to %q, got %q", requestUsageSummaryDefaultCurrency, row.Currency)
	}
	if row.PromptTokens != 4 || row.CompletionTokens != 5 || row.TotalTokens != 9 {
		t.Fatalf("usage record token values should win over request log snapshot, got %#v", row)
	}
	if row.EstimatedCost != 0 {
		t.Fatalf("missing usage cost should stay zero, got %#v", row)
	}
	if row.LatencyCount != 1 || row.UpstreamLatencyCount != 1 {
		t.Fatalf("valid latency snapshots should be counted, got %#v", row)
	}
}

func TestRequestUsageBySiteRowsFiltersAndSortsByCost(t *testing.T) {
	t.Parallel()

	lowCostSite := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highCostSite := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	rows := summarizeRequestUsageBySiteRows([]RequestUsageDailySummary{
		{
			SiteID:        uuid.NullUUID{UUID: lowCostSite, Valid: true},
			SiteName:      "low",
			RequestCount:  1,
			FailureCount:  1,
			EstimatedCost: 0.25,
			FirstRequestAt: sql.NullTime{
				Time:  time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
				Valid: true,
			},
			LastRequestAt: sql.NullTime{
				Time:  time.Date(2026, 6, 21, 11, 0, 0, 0, time.UTC),
				Valid: true,
			},
		},
		{
			SiteName:      "missing-site",
			RequestCount:  100,
			EstimatedCost: 100,
		},
		{
			SiteID:         uuid.NullUUID{UUID: highCostSite, Valid: true},
			SiteName:       "high",
			RequestCount:   2,
			SuccessCount:   2,
			PromptTokens:   10,
			EstimatedCost:  0.75,
			FirstRequestAt: sql.NullTime{Time: time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC), Valid: true},
			LastRequestAt:  sql.NullTime{Time: time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC), Valid: true},
		},
		{
			SiteID:        uuid.NullUUID{UUID: lowCostSite, Valid: true},
			SiteName:      "low",
			RequestCount:  3,
			SuccessCount:  3,
			EstimatedCost: 0.25,
			FirstRequestAt: sql.NullTime{
				Time:  time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC),
				Valid: true,
			},
			LastRequestAt: sql.NullTime{
				Time:  time.Date(2026, 6, 21, 13, 0, 0, 0, time.UTC),
				Valid: true,
			},
		},
	})

	if len(rows) != 2 {
		t.Fatalf("rows without valid site ids should be ignored, got %#v", rows)
	}
	if rows[0].SiteID != highCostSite || rows[1].SiteID != lowCostSite {
		t.Fatalf("summaries should sort by estimated cost descending, got %#v", rows)
	}
	if rows[1].RequestCount != 4 || rows[1].SuccessCount != 3 || rows[1].FailedCount != 1 {
		t.Fatalf("low cost site counts were not aggregated: %#v", rows[1])
	}
	if !rows[1].FirstRequestAt.Equal(time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)) ||
		!rows[1].LastRequestAt.Equal(time.Date(2026, 6, 21, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("low cost site request window was not aggregated: %#v", rows[1])
	}
}
