package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
)

func TestSummaryFromRequestLogUsesUsageRecordAndContextDimensions(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	canonicalID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	siteModelID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	apiKeyID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	createdAt := time.Date(2026, 6, 21, 18, 30, 0, 0, time.UTC)
	timeZone := config.LoadTimeZone("Asia/Shanghai")

	log := RequestLog{
		ID:               uuid.New(),
		RequestID:        "req-1",
		APIKeyID:         uuid.NullUUID{UUID: apiKeyID, Valid: true},
		SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
		CanonicalModelID: uuid.NullUUID{UUID: canonicalID, Valid: true},
		SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
		Endpoint:         "/v1/chat/completions",
		StatusCode:       200,
		Success:          true,
		LatencyMS:        sql.NullInt64{Int64: 1200, Valid: true},
		UpstreamLatencyMS: sql.NullInt64{
			Int64: 900,
			Valid: true,
		},
		RequestTokens:  sql.NullInt64{Int64: 999, Valid: true},
		ResponseTokens: sql.NullInt64{Int64: 999, Valid: true},
		CreatedAt:      createdAt,
	}
	usage := &UsageRecord{
		PromptTokens:               10,
		CompletionTokens:           20,
		TotalTokens:                30,
		CachedTokens:               sql.NullInt64{Int64: 4, Valid: true},
		CacheWriteTokens:           sql.NullInt64{Int64: 5, Valid: true},
		CacheCreationInputTokens:   sql.NullInt64{Int64: 6, Valid: true},
		CacheCreation5mInputTokens: sql.NullInt64{Int64: 4, Valid: true},
		CacheCreation1hInputTokens: sql.NullInt64{Int64: 2, Valid: true},
		CacheWriteCost:             sql.NullFloat64{Float64: 0.075, Valid: true},
		EstimatedCost:              sql.NullFloat64{Float64: 0.125, Valid: true},
		Currency:                   "EUR",
	}

	row := summaryFromRequestLog(log, usage, timeZone, requestUsageSummaryContext{
		Sites: map[uuid.UUID]Site{
			siteID: {ID: siteID, Name: "Primary Codex", Slug: "codex-primary", SiteType: "codex"},
		},
		CanonicalModels: map[uuid.UUID]CanonicalModel{
			canonicalID: {ID: canonicalID, ModelKey: "gpt-5.1"},
		},
		SiteModels: map[uuid.UUID]SiteModel{
			siteModelID: {ID: siteModelID, SiteID: siteID, UpstreamName: "gpt-5.1-chat"},
		},
		APIKeys: map[uuid.UUID]APIKey{
			apiKeyID: {ID: apiKeyID, Name: "prod-key"},
		},
	})

	wantBucketStart := timeZone.StartOfDay(createdAt)
	if !row.BucketStart.Equal(wantBucketStart) || row.TimeZone != "Asia/Shanghai" {
		t.Fatalf("unexpected bucket: start=%s timezone=%q", row.BucketStart, row.TimeZone)
	}
	if row.SiteID.UUID != siteID || row.SiteKey != siteID.String() || row.SiteName != "Primary Codex" || row.SiteSlug != "codex-primary" || row.SiteType != "codex" {
		t.Fatalf("unexpected site dimensions: %#v", row)
	}
	if row.CanonicalModelID.UUID != canonicalID || row.CanonicalModelKey != "gpt-5.1" {
		t.Fatalf("unexpected canonical dimensions: %#v", row)
	}
	if row.SiteModelID.UUID != siteModelID || row.UpstreamModelName != "gpt-5.1-chat" {
		t.Fatalf("unexpected site model dimensions: %#v", row)
	}
	if row.APIKeyID.UUID != apiKeyID || row.APIKeyName != "prod-key" {
		t.Fatalf("unexpected api key dimensions: %#v", row)
	}
	if row.PromptTokens != 10 || row.CompletionTokens != 20 || row.TotalTokens != 30 || row.CachedTokens != 4 {
		t.Fatalf("expected usage record tokens to win over log snapshot, got %#v", row)
	}
	if row.CacheWriteTokens != 5 || row.CacheCreationInputTokens != 6 || row.CacheCreation5mInputTokens != 4 || row.CacheCreation1hInputTokens != 2 {
		t.Fatalf("expected cache write tokens to be included in summary, got %#v", row)
	}
	if row.CacheWriteCost != 0.075 {
		t.Fatalf("expected cache write cost to be included in summary, got %#v", row)
	}
	if row.EstimatedCost != 0.125 || row.Currency != "EUR" {
		t.Fatalf("unexpected cost: %#v", row)
	}
	if row.RequestCount != 1 || row.SuccessCount != 1 || row.FailureCount != 0 {
		t.Fatalf("unexpected counts: %#v", row)
	}
	if row.LatencyCount != 1 || row.LatencyMinMS.Int64 != 1200 || row.UpstreamLatencyMinMS.Int64 != 900 {
		t.Fatalf("unexpected latency values: %#v", row)
	}
	if row.SummaryKey == "" {
		t.Fatal("expected deterministic summary key")
	}
}

func TestRequestUsageSummaryRepositoryHelpers(t *testing.T) {
	t.Parallel()

	repo := NewRequestUsageSummaryRepository(nil)
	if repo.db != nil {
		t.Fatalf("request usage summary repository db = %#v, want nil", repo.db)
	}

	first := uuid.New()
	second := uuid.New()
	ids := requestLogIDs([]RequestLog{{ID: first}, {ID: second}})
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("requestLogIDs = %#v, want [%s %s]", ids, first, second)
	}
	if got := requestLogIDs(nil); len(got) != 0 {
		t.Fatalf("requestLogIDs(nil) = %#v, want empty", got)
	}
}

func TestRequestUsageSummaryRepositoryNilStoreGuards(t *testing.T) {
	t.Parallel()

	repo := NewRequestUsageSummaryRepository(nil)
	day := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	timeZone := config.LoadTimeZone("UTC")
	log := RequestLog{ID: uuid.New(), RequestID: "req-guard", CreatedAt: day, Success: true}

	assertRequestUsageSummaryStoreNotInitialized(t, "Increment", repo.Increment(t.Context(), RequestUsageSummaryIncrement{
		RequestLog: log,
		TimeZone:   timeZone,
	}))

	_, err := repo.RebuildDay(t.Context(), day, timeZone, "test")
	assertRequestUsageSummaryStoreNotInitialized(t, "RebuildDay", err)

	_, err = repo.ListFromDetails(t.Context(), day, day.Add(time.Hour), timeZone)
	assertRequestUsageSummaryStoreNotInitialized(t, "ListFromDetails", err)

	_, err = repo.EnsureSummariesThrough(t.Context(), day, timeZone, "test")
	assertRequestUsageSummaryStoreNotInitialized(t, "EnsureSummariesThrough", err)

	_, err = repo.BackfillCachedTokens(t.Context(), day, timeZone)
	assertRequestUsageSummaryStoreNotInitialized(t, "BackfillCachedTokens", err)

	_, err = repo.BackfillCacheWriteTokens(t.Context(), day, timeZone)
	assertRequestUsageSummaryStoreNotInitialized(t, "BackfillCacheWriteTokens", err)

	_, err = repo.List(t.Context(), RequestUsageSummaryQuery{TimeZone: "UTC"})
	assertRequestUsageSummaryStoreNotInitialized(t, "List", err)

	_, err = repo.SummarizeBySite(t.Context(), RequestUsageSummaryQuery{TimeZone: "UTC"})
	assertRequestUsageSummaryStoreNotInitialized(t, "SummarizeBySite", err)

	_, err = repo.CostSummary(t.Context(), RequestUsageCostSummaryQuery{TimeZone: "UTC"})
	assertRequestUsageSummaryStoreNotInitialized(t, "CostSummary", err)

	_, err = repo.DeleteDetailsBefore(t.Context(), day)
	assertRequestUsageSummaryStoreNotInitialized(t, "DeleteDetailsBefore", err)
}

func TestRequestUsageSummaryCacheWriteTokensMetadata(t *testing.T) {
	t.Parallel()

	values, known := requestUsageSummaryCacheWriteTokens(JSON(`{
		"cost_calculation": {
			"cache_write_tokens": 3,
			"cache_creation_tokens": 5,
			"cache_creation_5m_tokens": 2,
			"cache_creation_1h_tokens": 3
		}
	}`))
	if !known || !values.CacheWriteTokens.Valid || values.CacheWriteTokens.Int64 != 3 ||
		!values.CacheCreationInputTokens.Valid || values.CacheCreationInputTokens.Int64 != 5 ||
		!values.CacheCreation5mInputTokens.Valid || values.CacheCreation5mInputTokens.Int64 != 2 ||
		!values.CacheCreation1hInputTokens.Valid || values.CacheCreation1hInputTokens.Int64 != 3 {
		t.Fatalf("cache write tokens = %#v known=%t, want structured metadata values", values, known)
	}

	values, known = requestUsageSummaryCacheWriteTokens(JSON(`{"cost_calculation":{"cache_write_tokens":-1}}`))
	if known || values != (requestUsageCacheWriteTokens{}) {
		t.Fatalf("negative cache write tokens = %#v known=%t, want unknown", values, known)
	}
}

func TestRequestUsageSummaryRepositorySiteIDsWithUsageEmptyInputDoesNotNeedDB(t *testing.T) {
	t.Parallel()

	got, err := NewRequestUsageSummaryRepository(nil).SiteIDsWithUsage(t.Context(), nil)
	if err != nil {
		t.Fatalf("SiteIDsWithUsage empty input should not require a store: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("SiteIDsWithUsage empty input = %#v, want empty map", got)
	}
}

func TestRequestUsageSummaryRepositorySummaryFromRequestLogNoDimensionsDoesNotNeedDB(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 6, 23, 1, 30, 0, 0, time.UTC)
	row, err := NewRequestUsageSummaryRepository(nil).summaryFromRequestLog(t.Context(), RequestLog{
		ID:             uuid.New(),
		RequestID:      "req-no-dimensions",
		Endpoint:       "/v1/chat/completions",
		StatusCode:     500,
		Success:        false,
		ErrorType:      sql.NullString{String: "upstream_error", Valid: true},
		RequestTokens:  sql.NullInt64{Int64: 4, Valid: true},
		ResponseTokens: sql.NullInt64{Int64: 6, Valid: true},
		CreatedAt:      createdAt,
	}, nil, config.LoadTimeZone("UTC"))
	if err != nil {
		t.Fatalf("summaryFromRequestLog without dimensions should not require DB: %v", err)
	}
	if row.RequestCount != 1 || row.SuccessCount != 0 || row.FailureCount != 1 {
		t.Fatalf("unexpected request summary counts: %#v", row)
	}
	if row.PromptTokens != 4 || row.CompletionTokens != 6 || row.TotalTokens != 10 {
		t.Fatalf("unexpected token fallback summary: %#v", row)
	}
	if row.SiteID.Valid || row.APIKeyID.Valid || row.CanonicalModelID.Valid || row.SiteModelID.Valid {
		t.Fatalf("dimension ids should remain invalid without log/context ids: %#v", row)
	}
	if row.SiteKey != requestUsageSummaryNoneKey || row.CanonicalModelKey != requestUsageSummaryNoneKey || row.SiteModelKey != requestUsageSummaryNoneKey || row.APIKeyKey != requestUsageSummaryNoneKey {
		t.Fatalf("missing dimensions should normalize to none keys: %#v", row)
	}
	if !row.BucketStart.Equal(config.LoadTimeZone("UTC").StartOfDay(createdAt)) || row.TimeZone != "UTC" {
		t.Fatalf("unexpected bucket fields: %#v", row)
	}
}

func TestRequestUsageSummaryRepositorySummaryContextWithoutIDsDoesNotNeedDB(t *testing.T) {
	t.Parallel()

	context, err := NewRequestUsageSummaryRepository(nil).summaryContext(t.Context(), []RequestLog{
		{
			ID:       uuid.New(),
			Metadata: JSON(`{"site_id":"not-a-uuid"}`),
		},
		{
			ID:       uuid.New(),
			Metadata: JSON(`{"site_name":"metadata-only"}`),
		},
	})
	if err != nil {
		t.Fatalf("summaryContext without usable ids should not require DB: %v", err)
	}
	if len(context.Sites) != 0 || len(context.CanonicalModels) != 0 || len(context.SiteModels) != 0 || len(context.APIKeys) != 0 {
		t.Fatalf("expected empty summary context maps, got %#v", context)
	}
}

func TestSummarizeRequestUsageBySiteRowsKeepsFirstNonDefaultCurrency(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	rows := summarizeRequestUsageBySiteRows([]RequestUsageDailySummary{
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			EstimatedCost: 0.25,
			Currency:      "",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			EstimatedCost: 0.50,
			Currency:      "EUR",
		},
		{
			SiteID:        uuid.NullUUID{UUID: siteID, Valid: true},
			EstimatedCost: 0.75,
			Currency:      "JPY",
		},
	})

	if len(rows) != 1 {
		t.Fatalf("expected one summarized site row, got %#v", rows)
	}
	if rows[0].Currency != "EUR" {
		t.Fatalf("expected first non-default currency to be kept, got %#v", rows[0])
	}
	if rows[0].EstimatedCost != 1.5 {
		t.Fatalf("expected aggregated cost 1.5, got %#v", rows[0])
	}
}

func assertRequestUsageSummaryStoreNotInitialized(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s error = nil, want request usage summary store initialization error", operation)
	}
	if !strings.Contains(err.Error(), "request usage summary store is not initialized") {
		t.Fatalf("%s error = %v, want request usage summary store initialization error", operation, err)
	}
}

func TestSummaryFromRequestLogFallsBackToMetadataForPreRouteFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	createdAt := time.Date(2026, 6, 22, 3, 15, 0, 0, time.UTC)

	row := summaryFromRequestLog(RequestLog{
		ID:             uuid.New(),
		RequestID:      "req-no-route",
		Endpoint:       "",
		StatusCode:     429,
		Success:        false,
		RequestTokens:  sql.NullInt64{Int64: 3, Valid: true},
		ResponseTokens: sql.NullInt64{Int64: 4, Valid: true},
		Metadata: JSON(`{
			"site_id":"55555555-5555-5555-5555-555555555555",
			"site_name":"metadata-site",
			"site_slug":"metadata-slug",
			"site_type":"openai",
			"error_code":"rate_limited"
		}`),
		CreatedAt: createdAt,
	}, nil, config.LoadTimeZone("UTC"), requestUsageSummaryContext{})

	if row.SiteID.UUID != siteID || row.SiteKey != siteID.String() {
		t.Fatalf("expected metadata site id to become summary dimension, got %#v", row)
	}
	if row.SiteName != "metadata-site" || row.SiteSlug != "metadata-slug" || row.SiteType != "openai" {
		t.Fatalf("expected metadata site labels, got %#v", row)
	}
	if row.Endpoint != requestUsageSummaryNoneKey {
		t.Fatalf("empty endpoint should normalize to none, got %q", row.Endpoint)
	}
	if row.ErrorType != "rate_limited" {
		t.Fatalf("expected metadata error_code, got %q", row.ErrorType)
	}
	if row.PromptTokens != 3 || row.CompletionTokens != 4 || row.TotalTokens != 7 {
		t.Fatalf("expected log token snapshot fallback, got %#v", row)
	}
	if row.RequestCount != 1 || row.SuccessCount != 0 || row.FailureCount != 1 {
		t.Fatalf("unexpected failure counts: %#v", row)
	}
}

func TestSummaryFromRequestLogUsesMetadataSiteNameAsKeyWithoutSiteID(t *testing.T) {
	t.Parallel()

	row := summaryFromRequestLog(RequestLog{
		ID:         uuid.New(),
		RequestID:  "req-metadata-name",
		Endpoint:   "/v1/responses",
		StatusCode: 200,
		Success:    true,
		Metadata: JSON(`{
			"site_name":" metadata site ",
			"site_slug":" metadata-slug ",
			"site_type":" codex "
		}`),
		CreatedAt: time.Date(2026, 6, 22, 3, 15, 0, 0, time.UTC),
	}, nil, config.LoadTimeZone("UTC"), requestUsageSummaryContext{})

	if row.SiteID.Valid {
		t.Fatalf("expected missing metadata site_id to keep SiteID invalid, got %#v", row.SiteID)
	}
	if row.SiteName != "metadata site" || row.SiteSlug != "metadata-slug" || row.SiteType != "codex" {
		t.Fatalf("expected trimmed metadata site labels, got %#v", row)
	}
	if row.SiteKey != "metadata site" {
		t.Fatalf("expected metadata site name to become site key, got %q", row.SiteKey)
	}
}

func TestAddSummaryValuesAggregatesLatencyAndRequestWindow(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	second := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	existing := RequestUsageDailySummary{
		RequestCount:           1,
		SuccessCount:           1,
		PromptTokens:           10,
		CompletionTokens:       5,
		TotalTokens:            15,
		CachedTokens:           3,
		EstimatedCost:          0.1,
		LatencyCount:           1,
		LatencyTotalMS:         300,
		LatencyMinMS:           sql.NullInt64{Int64: 300, Valid: true},
		LatencyMaxMS:           sql.NullInt64{Int64: 300, Valid: true},
		UpstreamLatencyCount:   1,
		UpstreamLatencyTotalMS: 200,
		UpstreamLatencyMinMS:   sql.NullInt64{Int64: 200, Valid: true},
		UpstreamLatencyMaxMS:   sql.NullInt64{Int64: 200, Valid: true},
		FirstRequestAt:         sql.NullTime{Time: second, Valid: true},
		LastRequestAt:          sql.NullTime{Time: second, Valid: true},
	}

	addSummaryValues(&existing, RequestUsageDailySummary{
		RequestCount:           1,
		FailureCount:           1,
		PromptTokens:           2,
		CompletionTokens:       3,
		TotalTokens:            5,
		CachedTokens:           1,
		EstimatedCost:          0.05,
		LatencyCount:           1,
		LatencyTotalMS:         100,
		LatencyMinMS:           sql.NullInt64{Int64: 100, Valid: true},
		LatencyMaxMS:           sql.NullInt64{Int64: 100, Valid: true},
		UpstreamLatencyCount:   1,
		UpstreamLatencyTotalMS: 250,
		UpstreamLatencyMinMS:   sql.NullInt64{Int64: 250, Valid: true},
		UpstreamLatencyMaxMS:   sql.NullInt64{Int64: 250, Valid: true},
		FirstRequestAt:         sql.NullTime{Time: first, Valid: true},
		LastRequestAt:          sql.NullTime{Time: first, Valid: true},
		SiteName:               "filled-later",
		UpstreamModelName:      "model-a",
		APIKeyName:             "key-a",
	})

	if existing.RequestCount != 2 || existing.SuccessCount != 1 || existing.FailureCount != 1 {
		t.Fatalf("unexpected counts: %#v", existing)
	}
	if existing.PromptTokens != 12 || existing.CompletionTokens != 8 || existing.TotalTokens != 20 || existing.CachedTokens != 4 {
		t.Fatalf("unexpected tokens: %#v", existing)
	}
	if existing.EstimatedCost != 0.15000000000000002 {
		t.Fatalf("unexpected cost: %#v", existing)
	}
	if existing.LatencyCount != 2 || existing.LatencyTotalMS != 400 || existing.LatencyMinMS.Int64 != 100 || existing.LatencyMaxMS.Int64 != 300 {
		t.Fatalf("unexpected latency aggregation: %#v", existing)
	}
	if existing.UpstreamLatencyCount != 2 || existing.UpstreamLatencyTotalMS != 450 || existing.UpstreamLatencyMinMS.Int64 != 200 || existing.UpstreamLatencyMaxMS.Int64 != 250 {
		t.Fatalf("unexpected upstream latency aggregation: %#v", existing)
	}
	if !existing.FirstRequestAt.Time.Equal(first) || !existing.LastRequestAt.Time.Equal(second) {
		t.Fatalf("unexpected request window: %#v", existing)
	}
	if existing.SiteName != "filled-later" || existing.UpstreamModelName != "model-a" || existing.APIKeyName != "key-a" {
		t.Fatalf("expected empty labels to be filled from delta, got %#v", existing)
	}
}

func TestRequestUsageSummaryKeyIsStableAndDimensionSensitive(t *testing.T) {
	t.Parallel()

	row := RequestUsageDailySummary{
		BucketStart:       time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
		TimeZone:          "UTC",
		SiteKey:           "site-a",
		CanonicalModelKey: "gpt-5",
		SiteModelKey:      "site-model-a",
		APIKeyKey:         "api-key-a",
		Endpoint:          "/v1/responses",
		StatusCode:        200,
		Success:           true,
		ErrorType:         requestUsageSummaryNoneKey,
		Currency:          "USD",
	}

	first := requestUsageSummaryKey(row)
	second := requestUsageSummaryKey(row)
	if first == "" || first != second {
		t.Fatalf("summary key should be stable, got first=%q second=%q", first, second)
	}

	row.StatusCode = 500
	if got := requestUsageSummaryKey(row); got == first {
		t.Fatalf("summary key should change when dimensions change")
	}
}

func TestSummariesFromRequestLogsGroupsSameDimensionsAndSplitsStatus(t *testing.T) {
	t.Parallel()

	firstLogID := uuid.New()
	secondLogID := uuid.New()
	failedLogID := uuid.New()
	createdAt := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := RequestUsageSummaryRepository{}

	rows, err := repo.summariesFromRequestLogs(t.Context(), []RequestLog{
		{
			ID:         firstLogID,
			RequestID:  "req-1",
			Endpoint:   "/v1/responses",
			StatusCode: 200,
			Success:    true,
			LatencyMS:  sql.NullInt64{Int64: 100, Valid: true},
			CreatedAt:  createdAt,
		},
		{
			ID:         secondLogID,
			RequestID:  "req-2",
			Endpoint:   "/v1/responses",
			StatusCode: 200,
			Success:    true,
			LatencyMS:  sql.NullInt64{Int64: 300, Valid: true},
			CreatedAt:  createdAt.Add(time.Hour),
		},
		{
			ID:         failedLogID,
			RequestID:  "req-3",
			Endpoint:   "/v1/responses",
			StatusCode: 500,
			Success:    false,
			CreatedAt:  createdAt.Add(2 * time.Hour),
		},
	}, map[uuid.UUID]UsageRecord{
		firstLogID: {
			ID:               uuid.New(),
			RequestLogID:     firstLogID,
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    sql.NullFloat64{Float64: 0.1, Valid: true},
			Currency:         "EUR",
		},
		secondLogID: {
			ID:               uuid.New(),
			RequestLogID:     secondLogID,
			PromptTokens:     4,
			CompletionTokens: 6,
			TotalTokens:      10,
			EstimatedCost:    sql.NullFloat64{Float64: 0.2, Valid: true},
			Currency:         "EUR",
		},
		failedLogID: {
			ID:           uuid.New(),
			RequestLogID: failedLogID,
			Currency:     "EUR",
		},
	}, config.LoadTimeZone("UTC"))
	if err != nil {
		t.Fatalf("summaries from request logs: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected success and failure summaries, got %#v", rows)
	}

	var successRow RequestUsageDailySummary
	var failureRow RequestUsageDailySummary
	for _, row := range rows {
		if row.Success {
			successRow = row
		} else {
			failureRow = row
		}
	}
	if successRow.RequestCount != 2 || successRow.SuccessCount != 2 || successRow.FailureCount != 0 {
		t.Fatalf("unexpected success summary counts: %#v", successRow)
	}
	if successRow.PromptTokens != 14 || successRow.CompletionTokens != 11 || successRow.TotalTokens != 25 {
		t.Fatalf("unexpected success summary tokens: %#v", successRow)
	}
	if successRow.EstimatedCost != 0.30000000000000004 || successRow.Currency != "EUR" {
		t.Fatalf("unexpected success summary cost: %#v", successRow)
	}
	if successRow.LatencyCount != 2 || successRow.LatencyMinMS.Int64 != 100 || successRow.LatencyMaxMS.Int64 != 300 {
		t.Fatalf("unexpected success summary latency: %#v", successRow)
	}
	if !successRow.FirstRequestAt.Time.Equal(createdAt) || !successRow.LastRequestAt.Time.Equal(createdAt.Add(time.Hour)) {
		t.Fatalf("unexpected success request window: %#v", successRow)
	}
	if failureRow.RequestCount != 1 || failureRow.FailureCount != 1 || failureRow.ErrorType != "unknown" {
		t.Fatalf("unexpected failure summary: %#v", failureRow)
	}
}

func TestRequestUsageSummaryRowMatchesCostFilterByCanonicalOrUpstreamModel(t *testing.T) {
	t.Parallel()

	row := RequestUsageDailySummary{
		CanonicalModelKey: "gpt-5.1",
		UpstreamModelName: "provider-chat-large",
	}
	if !requestUsageSummaryRowMatchesCostFilter(row, RequestUsageCostSummaryQuery{ModelKey: "GPT-5"}) {
		t.Fatal("expected canonical model key to match case-insensitively")
	}
	if !requestUsageSummaryRowMatchesCostFilter(row, RequestUsageCostSummaryQuery{ModelKey: "chat-large"}) {
		t.Fatal("expected upstream model name to match")
	}
	if !requestUsageSummaryRowMatchesCostFilter(row, RequestUsageCostSummaryQuery{ModelKey: " CHAT-LARGE "}) {
		t.Fatal("expected model filter to trim surrounding whitespace")
	}
	if requestUsageSummaryRowMatchesCostFilter(row, RequestUsageCostSummaryQuery{ModelKey: "embedding"}) {
		t.Fatal("expected unrelated model key to be filtered out")
	}
	if !requestUsageSummaryRowMatchesCostFilter(row, RequestUsageCostSummaryQuery{}) {
		t.Fatal("empty model filter should include row")
	}
}

func TestRequestUsageSummaryErrorTypeFallbacks(t *testing.T) {
	t.Parallel()

	if got := requestUsageSummaryErrorType(RequestLog{
		ErrorType: sql.NullString{String: " upstream_timeout ", Valid: true},
		Success:   false,
	}); got != "upstream_timeout" {
		t.Fatalf("explicit error type = %q, want upstream_timeout", got)
	}

	if got := requestUsageSummaryErrorType(RequestLog{
		Metadata: JSON(`{"error_code":"rate_limited"}`),
		Success:  false,
	}); got != "rate_limited" {
		t.Fatalf("metadata error type = %q, want rate_limited", got)
	}

	if got := requestUsageSummaryErrorType(RequestLog{Success: false}); got != "unknown" {
		t.Fatalf("failed request fallback = %q, want unknown", got)
	}

	if got := requestUsageSummaryErrorType(RequestLog{Success: true}); got != requestUsageSummaryNoneKey {
		t.Fatalf("successful request fallback = %q, want none", got)
	}
}

func TestRequestUsageSummaryMetadataUUIDRejectsBlankInvalidAndNil(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	valid := requestUsageSummaryMetadataUUID(JSON(`{"site_id":"`+id.String()+`"}`), "site_id")
	if !valid.Valid || valid.UUID != id {
		t.Fatalf("valid metadata UUID = %#v, want %s", valid, id)
	}

	for _, raw := range []JSON{
		JSON(`{"site_id":""}`),
		JSON(`{"site_id":"not-a-uuid"}`),
		JSON(`{"site_id":"00000000-0000-0000-0000-000000000000"}`),
		JSON(`{invalid-json`),
		nil,
	} {
		if got := requestUsageSummaryMetadataUUID(raw, "site_id"); got.Valid {
			t.Fatalf("metadata UUID %#v should be invalid, got %#v", raw, got)
		}
	}
}

func TestRequestUsageSummaryMetadataHelpersHandleNullAndMapValues(t *testing.T) {
	t.Parallel()

	if values := requestUsageSummaryMetadata(JSON(`null`)); len(values) != 0 {
		t.Fatalf("null metadata should behave like empty map, got %#v", values)
	}
	if values := requestUsageSummaryMetadata(JSON(`["not-object"]`)); len(values) != 0 {
		t.Fatalf("non-object metadata should behave like empty map, got %#v", values)
	}

	id := uuid.New()
	values := map[string]any{
		"site_id": " " + id.String() + " ",
		"nil_id":  nil,
	}
	if got := requestUsageSummaryMetadataUUIDFromMap(values, "site_id"); !got.Valid || got.UUID != id {
		t.Fatalf("trimmed metadata UUID from map = %#v, want %s", got, id)
	}
	if got := requestUsageSummaryMetadataUUIDFromMap(values, "nil_id"); got.Valid {
		t.Fatalf("nil metadata UUID from map should be invalid, got %#v", got)
	}
}

func TestRequestUsageSummaryCachedTokensReadsKnownAliases(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		raw  JSON
		want int64
		ok   bool
	}{
		{raw: JSON(`{"cost_calculation":{"cached_tokens":42}}`), want: 42, ok: true},
		{raw: JSON(`{"cost_calculation":{"cache_tokens":17}}`), want: 17, ok: true},
		{raw: JSON(`{"cost_calculation":{"cached_tokens":0}}`), want: 0, ok: true},
		{raw: JSON(`{"cost_calculation":{}}`)},
		{raw: JSON(`{"cost_calculation":{"cached_tokens":-1}}`)},
		{raw: JSON(`{"cost_calculation":{"cached_tokens":1.5}}`)},
		{raw: JSON(`{}`)},
	} {
		got, ok := requestUsageSummaryCachedTokens(testCase.raw)
		if got != testCase.want || ok != testCase.ok {
			t.Fatalf("cached tokens for %s = %d, %v, want %d, %v", testCase.raw, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestRequestUsageSummaryMetadataStringTrimsAndRejectsNonStrings(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"name":    " codex ",
		"numeric": 123,
	}
	if got := requestUsageSummaryMetadataString(values, "name"); got != "codex" {
		t.Fatalf("metadata string = %q, want codex", got)
	}
	if got := requestUsageSummaryMetadataString(values, "missing"); got != "" {
		t.Fatalf("missing metadata string = %q, want empty", got)
	}
	if got := requestUsageSummaryMetadataString(values, "numeric"); got != "" {
		t.Fatalf("non-string metadata value = %q, want empty", got)
	}
}

func TestNullableSummaryHelpers(t *testing.T) {
	t.Parallel()

	if got := nullableUUIDSummaryKey(uuid.NullUUID{}); got != requestUsageSummaryNoneKey {
		t.Fatalf("nullableUUIDSummaryKey invalid = %q, want none", got)
	}
	id := uuid.New()
	if got := nullableUUIDSummaryKey(uuid.NullUUID{UUID: id, Valid: true}); got != id.String() {
		t.Fatalf("nullableUUIDSummaryKey valid = %q, want %s", got, id)
	}

	if got := nullableStringSummaryValue(sql.NullString{String: "  ", Valid: true}); got != requestUsageSummaryNoneKey {
		t.Fatalf("nullableStringSummaryValue blank = %q, want none", got)
	}
	if got := nullableStringSummaryValue(sql.NullString{String: " codex ", Valid: true}); got != "codex" {
		t.Fatalf("nullableStringSummaryValue valid = %q, want codex", got)
	}
}

func TestRequestUsageCostSummaryAddCostAndCurrencyFallback(t *testing.T) {
	t.Parallel()

	summary := RequestUsageCostSummary{Currency: requestUsageSummaryDefaultCurrency}
	summary.AddCost(sql.NullFloat64{}, "EUR")
	if summary.TotalCost != 0 || summary.Currency != requestUsageSummaryDefaultCurrency {
		t.Fatalf("invalid AddCost should not change summary, got %#v", summary)
	}

	summary.AddCost(sql.NullFloat64{Float64: 1.25, Valid: true}, "EUR")
	if summary.TotalCost != 1.25 || summary.Currency != "EUR" {
		t.Fatalf("valid AddCost = %#v, want 1.25 EUR", summary)
	}

	// A later default-currency value is tracked separately, not summed into the
	// established EUR total (currency-consistent). F22.
	summary.AddFloat(0.75, "")
	if summary.TotalCost != 1.25 || summary.Currency != "EUR" {
		t.Fatalf("default-currency value must not mix into the EUR total, got %#v", summary)
	}
	if summary.CostByCurrency["USD"] != 0.75 || summary.CostByCurrency["EUR"] != 1.25 {
		t.Fatalf("per-currency breakdown = %#v", summary.CostByCurrency)
	}
}

func TestNullableTimeHelpersPreferValidExtremes(t *testing.T) {
	t.Parallel()

	earlier := sql.NullTime{Time: time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC), Valid: true}
	later := sql.NullTime{Time: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC), Valid: true}
	invalid := sql.NullTime{}

	if got := minNullableTime(invalid, later); !got.Valid || !got.Time.Equal(later.Time) {
		t.Fatalf("min with invalid left = %#v, want later", got)
	}
	if got := minNullableTime(earlier, invalid); !got.Valid || !got.Time.Equal(earlier.Time) {
		t.Fatalf("min with invalid right = %#v, want earlier", got)
	}
	if got := minNullableTime(later, earlier); !got.Valid || !got.Time.Equal(earlier.Time) {
		t.Fatalf("min valid values = %#v, want earlier", got)
	}
	if got := maxNullableTime(invalid, earlier); !got.Valid || !got.Time.Equal(earlier.Time) {
		t.Fatalf("max with invalid left = %#v, want earlier", got)
	}
	if got := maxNullableTime(later, invalid); !got.Valid || !got.Time.Equal(later.Time) {
		t.Fatalf("max with invalid right = %#v, want later", got)
	}
	if got := maxNullableTime(earlier, later); !got.Valid || !got.Time.Equal(later.Time) {
		t.Fatalf("max valid values = %#v, want later", got)
	}
}

func TestNormalizeSummaryTimeZoneLoadsNamedLocation(t *testing.T) {
	t.Parallel()

	timeZone := normalizeSummaryTimeZone(config.TimeZone{Name: "Asia/Shanghai"})
	if timeZone.Location == nil {
		t.Fatal("expected named timezone location to be loaded")
	}
	if got := time.Date(2026, 6, 22, 1, 30, 0, 0, time.UTC).In(timeZone.Location).Format("-07:00"); got != "+08:00" {
		t.Fatalf("Asia/Shanghai offset = %s, want +08:00", got)
	}
}
