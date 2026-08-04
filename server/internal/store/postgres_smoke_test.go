package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"
	"xlyra/server/internal/config"
)

func TestDevPostgresStoreSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		t.Fatalf("ping store: %v", err)
	}

	gormDB := db.DB()
	sites, err := NewSiteRepository(gormDB).List(ctx)
	if err != nil {
		t.Fatalf("list sites: %v", err)
	}
	assertSitesAreActiveAndNewestFirst(t, sites)

	allSites, err := NewSiteRepository(gormDB).ListIncludingDeleted(ctx)
	if err != nil {
		t.Fatalf("list sites including deleted: %v", err)
	}
	if len(allSites) < len(sites) {
		t.Fatalf("including deleted sites length = %d, active length = %d", len(allSites), len(sites))
	}
	assertSitesNewestFirst(t, allSites)

	canonicalModels, err := NewCanonicalModelRepository(gormDB).List(ctx)
	if err != nil {
		t.Fatalf("list canonical models: %v", err)
	}
	assertCanonicalModelsSorted(t, canonicalModels)

	apiKeys, err := NewAPIKeyRepository(gormDB).List(ctx)
	if err != nil {
		t.Fatalf("list api keys: %v", err)
	}
	assertAPIKeysNewestFirst(t, apiKeys)

	apiKeyIDs := apiKeyIDsForSmoke(apiKeys, 3)
	apiKeysByID, err := NewAPIKeyRepository(gormDB).ListByIDs(ctx, apiKeyIDs)
	if err != nil {
		t.Fatalf("list api keys by ids: %v", err)
	}
	if len(apiKeysByID) > len(apiKeyIDs) {
		t.Fatalf("api keys by id length = %d, requested %d", len(apiKeysByID), len(apiKeyIDs))
	}

	requestLogs := NewRequestLogRepository(gormDB)
	logPage, err := requestLogs.ListDetailedPage(ctx, ListRequestLogsParams{Page: 1, PageSize: 5})
	if err != nil {
		t.Fatalf("list detailed request log page: %v", err)
	}
	if logPage.Total < 0 {
		t.Fatalf("request log total = %d, want non-negative", logPage.Total)
	}
	if logPage.Page != 1 || logPage.PageSize != 5 {
		t.Fatalf("request log pagination = page %d size %d, want page 1 size 5", logPage.Page, logPage.PageSize)
	}
	if len(logPage.Items) > logPage.PageSize {
		t.Fatalf("request log page returned %d items, page size %d", len(logPage.Items), logPage.PageSize)
	}
	assertRequestLogDetailsNewestFirst(t, logPage.Items)

	limitedLogs, err := requestLogs.ListDetailed(ctx, ListRequestLogsParams{Limit: 3})
	if err != nil {
		t.Fatalf("list detailed request logs: %v", err)
	}
	if len(limitedLogs) > 3 {
		t.Fatalf("limited request logs length = %d, want at most 3", len(limitedLogs))
	}
	assertRequestLogDetailsNewestFirst(t, limitedLogs)

	if len(logPage.Items) > 0 {
		detail, err := requestLogs.GetDetailed(ctx, logPage.Items[0].ID)
		if err != nil {
			t.Fatalf("get detailed request log: %v", err)
		}
		if detail.ID != logPage.Items[0].ID {
			t.Fatalf("detailed request log id = %s, want %s", detail.ID, logPage.Items[0].ID)
		}
	}

	recentUsage, err := requestLogs.RecentRateUsage(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("recent request rate usage: %v", err)
	}
	if recentUsage.RPM < 0 || recentUsage.TPM < 0 {
		t.Fatalf("recent usage should be non-negative, got %#v", recentUsage)
	}

	summaryStart, summaryEnd := requestUsageSmokeWindow(logPage.Items)
	summaries := NewRequestUsageSummaryRepository(gormDB)
	summaryRows, err := summaries.List(ctx, RequestUsageSummaryQuery{
		TimeZone: config.DefaultTimeZone,
		From:     &summaryStart,
		To:       &summaryEnd,
	})
	if err != nil {
		t.Fatalf("list request usage summaries: %v", err)
	}
	assertRequestUsageSummariesStable(t, summaryRows)

	detailRows, err := summaries.ListFromDetails(ctx, summaryStart, summaryEnd, config.LoadTimeZone(config.DefaultTimeZone))
	if err != nil {
		t.Fatalf("list request usage summaries from details: %v", err)
	}
	assertRequestUsageSummariesStable(t, detailRows)
	assertRequestUsageSummariesByKey(t, detailRows)
}

func TestDevPostgresAdminAuthRepositoriesSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	gormDB := db.DB()
	admins := NewAdminRepository(gormDB)
	adminCount, err := admins.Count(ctx)
	if err != nil {
		t.Fatalf("count admins: %v", err)
	}
	if adminCount < 0 {
		t.Fatalf("admin count = %d, want non-negative", adminCount)
	}
	_, err = admins.GetByUsername(ctx, "xlyra-smoke-missing-"+uuid.NewString())
	assertSmokeRecordNotFound(t, err)
	_, err = admins.GetByID(ctx, uuid.New())
	assertSmokeRecordNotFound(t, err)

	sessions := NewAdminSessionRepository(gormDB)
	_, err = sessions.GetActiveByTokenHash(ctx, "xlyra-smoke-missing-"+uuid.NewString(), time.Now())
	assertSmokeRecordNotFound(t, err)
	sessionRows, err := sessions.ListByAdmin(ctx, uuid.New(), time.Now())
	if err != nil {
		t.Fatalf("list admin sessions by missing admin: %v", err)
	}
	if len(sessionRows) != 0 {
		t.Fatalf("missing admin sessions length = %d, want 0", len(sessionRows))
	}

	tokens := NewAdminAccessTokenRepository(gormDB)
	if token, err := tokens.Get(ctx); err != nil {
		assertSmokeRecordNotFound(t, err)
	} else if token.ID == uuid.Nil {
		t.Fatalf("admin access token id is nil: %#v", token)
	}
	_, err = tokens.GetActiveByTokenHash(ctx, "xlyra-smoke-missing-"+uuid.NewString())
	assertSmokeRecordNotFound(t, err)

	oauthConnections := NewOAuthConnectionRepository(gormDB)
	connections, err := oauthConnections.List(ctx)
	if err != nil {
		t.Fatalf("list oauth connections: %v", err)
	}
	assertOAuthConnectionsAscending(t, connections)
	_, err = oauthConnections.GetByID(ctx, uuid.New())
	assertSmokeRecordNotFound(t, err)
	_, err = oauthConnections.GetBySiteID(ctx, uuid.New())
	assertSmokeRecordNotFound(t, err)

	oauthSessions := NewOAuthSessionRepository(gormDB)
	_, err = oauthSessions.GetByState(ctx, "xlyra-smoke-missing-"+uuid.NewString())
	assertSmokeRecordNotFound(t, err)
	_, err = oauthSessions.GetByID(ctx, uuid.New())
	assertSmokeRecordNotFound(t, err)

	rateLimits := NewGatewayRateLimitRepository(gormDB)
	if item, err := rateLimits.GetGlobal(ctx); err != nil {
		assertSmokeRecordNotFound(t, err)
	} else if item.Scope != RateLimitScopeGlobal {
		t.Fatalf("global rate limit scope = %q, want %q", item.Scope, RateLimitScopeGlobal)
	}
	_, err = rateLimits.GetByAPIKey(ctx, uuid.New())
	assertSmokeRecordNotFound(t, err)
}

func devPostgresSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           getenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           getenvDefault("DB_NAME", "xlyra"),
		DBUser:           getenvDefault("DB_USER", "postgres"),
		DBPassword:       getenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        getenvDefault("DB_SSLMODE", "disable"),
	}
	if value := strings.TrimSpace(os.Getenv("DB_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return config.Config{}, fmt.Errorf("invalid DB_PORT %q", value)
		}
		cfg.DBPort = port
	}
	return cfg, nil
}

func getenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactDatabaseOpenError(err error, cfg config.Config) string {
	msg := err.Error()
	secrets := []string{cfg.DBPassword}
	if parsed, parseErr := url.Parse(strings.TrimSpace(cfg.PostgresDSN)); parseErr == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			secrets = append(secrets, password)
		}
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
		msg = strings.ReplaceAll(msg, url.QueryEscape(secret), "[redacted]")
		msg = strings.ReplaceAll(msg, url.PathEscape(secret), "[redacted]")
	}
	return msg
}

func assertSitesAreActiveAndNewestFirst(t *testing.T, sites []Site) {
	t.Helper()
	for _, site := range sites {
		if SiteDeleted(site) {
			t.Fatalf("active site list included deleted site %s", site.ID)
		}
	}
	assertSitesNewestFirst(t, sites)
}

func assertSitesNewestFirst(t *testing.T, sites []Site) {
	t.Helper()
	for i := 1; i < len(sites); i++ {
		if sites[i].CreatedAt.After(sites[i-1].CreatedAt) {
			t.Fatalf("sites are not newest first at index %d", i)
		}
	}
}

func assertCanonicalModelsSorted(t *testing.T, models []CanonicalModelWithStats) {
	t.Helper()
	for i := 1; i < len(models); i++ {
		if models[i].ModelKey < models[i-1].ModelKey {
			t.Fatalf("canonical models are not sorted by key at index %d", i)
		}
	}
	for _, model := range models {
		if model.SiteCount < 0 || model.SiteModelCount < 0 {
			t.Fatalf("canonical model stats should be non-negative: %#v", model)
		}
	}
}

func assertAPIKeysNewestFirst(t *testing.T, apiKeys []APIKey) {
	t.Helper()
	for i := 1; i < len(apiKeys); i++ {
		if apiKeys[i].CreatedAt.After(apiKeys[i-1].CreatedAt) {
			t.Fatalf("api keys are not newest first at index %d", i)
		}
	}
}

func assertSmokeRecordNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func assertOAuthConnectionsAscending(t *testing.T, connections []OAuthConnection) {
	t.Helper()
	for i := 1; i < len(connections); i++ {
		previous := connections[i-1]
		current := connections[i]
		if previous.SiteID == nil || current.SiteID == nil {
			t.Fatalf("oauth connection list should only include site-bound records: previous=%#v current=%#v", previous.SiteID, current.SiteID)
		}
		if previous.CreatedAt.After(current.CreatedAt) {
			t.Fatalf("oauth connections are not oldest first at index %d", i)
		}
		if previous.CreatedAt.Equal(current.CreatedAt) && previous.ID.String() > current.ID.String() {
			t.Fatalf("oauth connections with equal created_at are not sorted by id at index %d", i)
		}
	}
}

func apiKeyIDsForSmoke(apiKeys []APIKey, limit int) []uuid.UUID {
	if limit > len(apiKeys) {
		limit = len(apiKeys)
	}
	ids := make([]uuid.UUID, 0, limit)
	for _, apiKey := range apiKeys[:limit] {
		ids = append(ids, apiKey.ID)
	}
	if len(ids) == 0 {
		ids = append(ids, uuid.New())
	}
	return ids
}

func assertRequestLogDetailsNewestFirst(t *testing.T, logs []RequestLogDetail) {
	t.Helper()
	for i := 1; i < len(logs); i++ {
		if logs[i].CreatedAt.After(logs[i-1].CreatedAt) {
			t.Fatalf("request log details are not newest first at index %d", i)
		}
	}
}

func requestUsageSmokeWindow(logs []RequestLogDetail) (time.Time, time.Time) {
	if len(logs) == 0 {
		end := time.Now().UTC()
		return end.Add(-24 * time.Hour), end
	}
	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	start := timeZone.StartOfDay(logs[0].CreatedAt)
	return start, start.AddDate(0, 0, 1)
}

func assertRequestUsageSummariesStable(t *testing.T, rows []RequestUsageDailySummary) {
	t.Helper()
	for _, row := range rows {
		if row.RequestCount < 0 || row.SuccessCount < 0 || row.FailureCount < 0 {
			t.Fatalf("summary counts should be non-negative: %#v", row)
		}
		if row.PromptTokens < 0 || row.CompletionTokens < 0 || row.TotalTokens < 0 {
			t.Fatalf("summary tokens should be non-negative: %#v", row)
		}
		if row.LatencyCount < 0 || row.UpstreamLatencyCount < 0 {
			t.Fatalf("summary latency counts should be non-negative: %#v", row)
		}
	}
}

func assertRequestUsageSummariesByKey(t *testing.T, rows []RequestUsageDailySummary) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SummaryKey < rows[i-1].SummaryKey {
			t.Fatalf("summary detail rows are not sorted by key at index %d", i)
		}
	}
}
