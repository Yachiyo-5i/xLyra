package oauth

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
	"xlyra/server/migrations"
)

func TestRecordCodexQuotaSnapshotEstimatesAndResets(t *testing.T) {
	dbStore, cleanup := openQuotaEstimateTestStore(t)
	defer cleanup()
	ctx := context.Background()
	service := NewService(dbStore, "master-key")
	db := dbStore.DB()

	site := createOAuthQuotaTestSite(t, ctx, db)
	connection := createOAuthQuotaTestConnection(t, ctx, db, site.ID)

	t0 := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Hour)
	t2 := t0.Add(3 * time.Hour)

	baselineQuota := map[string]any{
		"available": true,
		"five_hour": map[string]any{
			"used_percent":      10.0,
			"remaining_percent": 90.0,
			"reset_at":          t0.Add(5 * time.Hour).Unix(),
		},
		"weekly": map[string]any{
			"used_percent":      20.0,
			"remaining_percent": 80.0,
			"reset_at":          t0.Add(7 * 24 * time.Hour).Unix(),
		},
	}
	view, err := service.RecordCodexQuotaSnapshot(ctx, connection.ID, site.ID, baselineQuota, t0)
	if err != nil {
		t.Fatalf("baseline snapshot: %v", err)
	}
	if view.FiveHour == nil || view.FiveHour.Confidence != store.OAuthQuotaConfidenceBaseline {
		t.Fatalf("five_hour baseline view = %#v", view.FiveHour)
	}
	if view.Weekly == nil || view.Weekly.Confidence != store.OAuthQuotaConfidenceBaseline {
		t.Fatalf("weekly baseline view = %#v", view.Weekly)
	}

	// Three priced requests in (t0, t1) so confidence can reach ready once percent delta is enough.
	for i := 0; i < 3; i++ {
		createOAuthQuotaPricedRequest(t, ctx, db, site.ID, t0.Add(time.Duration(i+1)*10*time.Minute), 2.0)
	}

	secondQuota := map[string]any{
		"available": true,
		"five_hour": map[string]any{
			"used_percent":      20.0,
			"remaining_percent": 80.0,
			"reset_at":          t0.Add(5 * time.Hour).Unix(),
		},
		"weekly": map[string]any{
			"used_percent":      25.0,
			"remaining_percent": 75.0,
			"reset_at":          t0.Add(7 * 24 * time.Hour).Unix(),
		},
	}
	view, err = service.RecordCodexQuotaSnapshot(ctx, connection.ID, site.ID, secondQuota, t1)
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if view.FiveHour == nil || view.FiveHour.EstimatedTotal == nil {
		t.Fatalf("expected five_hour estimate after spend, got %#v", view.FiveHour)
	}
	// cost 6 / used 10% => total 60
	if math.Abs(*view.FiveHour.EstimatedTotal-60) > 1e-6 {
		t.Fatalf("five_hour estimated_total = %v, want 60", *view.FiveHour.EstimatedTotal)
	}
	if view.FiveHour.Confidence != store.OAuthQuotaConfidenceReady {
		t.Fatalf("five_hour confidence = %q, want ready", view.FiveHour.Confidence)
	}
	if view.FiveHour.ObservedTo == nil || !view.FiveHour.ObservedTo.Equal(t1) {
		t.Fatalf("five_hour observed_to = %#v, want %v", view.FiveHour.ObservedTo, t1)
	}
	if view.Weekly == nil || view.Weekly.EstimatedTotal == nil {
		t.Fatalf("expected weekly estimate, got %#v", view.Weekly)
	}
	// weekly used delta 5% with same $6 => total 120
	if math.Abs(*view.Weekly.EstimatedTotal-120) > 1e-6 {
		t.Fatalf("weekly estimated_total = %v, want 120", *view.Weekly.EstimatedTotal)
	}

	resetQuota := map[string]any{
		"available": true,
		"five_hour": map[string]any{
			"used_percent":      0.0,
			"remaining_percent": 100.0,
			"reset_at":          t2.Add(5 * time.Hour).Unix(),
		},
		"weekly": map[string]any{
			"used_percent":      25.0,
			"remaining_percent": 75.0,
			"reset_at":          t0.Add(7 * 24 * time.Hour).Unix(),
		},
	}
	view, err = service.RecordCodexQuotaSnapshot(ctx, connection.ID, site.ID, resetQuota, t2)
	if err != nil {
		t.Fatalf("reset snapshot: %v", err)
	}
	if view.FiveHour == nil || view.FiveHour.Confidence != store.OAuthQuotaConfidenceBaseline {
		t.Fatalf("five_hour after reset = %#v", view.FiveHour)
	}
	if view.FiveHour.PreviousTotal == nil || math.Abs(*view.FiveHour.PreviousTotal-60) > 1e-6 {
		t.Fatalf("five_hour previous_estimated_total = %#v, want 60", view.FiveHour.PreviousTotal)
	}
	if view.FiveHour.EstimatedTotal != nil {
		t.Fatalf("new five_hour baseline should not have estimated_total yet, got %#v", view.FiveHour.EstimatedTotal)
	}
	// weekly did not reset
	if view.Weekly == nil || view.Weekly.EstimatedTotal == nil || math.Abs(*view.Weekly.EstimatedTotal-120) > 1e-6 {
		t.Fatalf("weekly should keep estimate across five_hour reset: %#v", view.Weekly)
	}
}

func openQuotaEstimateTestStore(t *testing.T) (*store.Store, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)

	baseCfg, err := devPostgresOAuthSmokeConfig()
	if err != nil {
		cancel()
		t.Skipf("dev PostgreSQL disabled: %v", err)
	}

	schemaName := "xlyra_quota_est_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	bootstrap, err := store.Open(ctx, baseCfg)
	if err != nil {
		cancel()
		t.Skipf("dev PostgreSQL unavailable: %s", redactOAuthDatabaseOpenError(err, baseCfg))
	}
	if err := bootstrap.DB().WithContext(ctx).Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		bootstrap.Close()
		cancel()
		t.Fatalf("create test schema: %v", err)
	}
	bootstrap.Close()

	cfg := baseCfg
	parsed, err := url.Parse(baseCfg.DatabaseDSN())
	if err != nil {
		dropQuotaEstimateSchema(baseCfg, schemaName)
		cancel()
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	cfg.PostgresDSN = parsed.String()
	cfg.DBMinConns = 0
	cfg.DBMaxConns = 2

	dbStore, err := store.Open(ctx, cfg)
	if err != nil {
		dropQuotaEstimateSchema(baseCfg, schemaName)
		cancel()
		t.Fatalf("open schema store: %v", err)
	}
	sqlDB, err := dbStore.DB().DB()
	if err != nil {
		dbStore.Close()
		dropQuotaEstimateSchema(baseCfg, schemaName)
		cancel()
		t.Fatalf("sql db: %v", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		dbStore.Close()
		dropQuotaEstimateSchema(baseCfg, schemaName)
		cancel()
		t.Fatalf("goose dialect: %v", err)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		dbStore.Close()
		dropQuotaEstimateSchema(baseCfg, schemaName)
		cancel()
		t.Fatalf("migrate schema: %v", err)
	}

	return dbStore, func() {
		dbStore.Close()
		dropQuotaEstimateSchema(baseCfg, schemaName)
		cancel()
	}
}

func dropQuotaEstimateSchema(cfg config.Config, schemaName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg)
	if err != nil {
		return
	}
	defer db.Close()
	_ = db.DB().WithContext(ctx).Exec("DROP SCHEMA IF EXISTS " + schemaName + " CASCADE").Error
}

func createOAuthQuotaTestSite(t *testing.T, ctx context.Context, db *gorm.DB) store.Site {
	t.Helper()
	site, err := store.NewSiteRepository(db).Create(ctx, store.CreateSiteParams{
		Name:            "OAuth Quota Estimate",
		Slug:            "oauth-quota-" + uuid.NewString(),
		SiteType:        "codex",
		BaseURL:         "https://example.invalid",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	return site
}

func createOAuthQuotaTestConnection(t *testing.T, ctx context.Context, db *gorm.DB, siteID uuid.UUID) store.OAuthConnection {
	t.Helper()
	siteIDCopy := siteID
	item := store.OAuthConnection{
		Provider:          "codex",
		SiteID:            &siteIDCopy,
		Status:            "active",
		AccountID:         "acct-" + uuid.NewString(),
		Email:             "oauth-quota-" + uuid.NewString() + "@example.com",
		MaskedAccessToken: "sk-...test",
		RawProfile:        store.JSON(`{}`),
		Metadata:          store.JSON(`{}`),
	}
	if err := db.WithContext(ctx).Create(&item).Error; err != nil {
		t.Fatalf("create oauth connection: %v", err)
	}
	return item
}

func createOAuthQuotaPricedRequest(t *testing.T, ctx context.Context, db *gorm.DB, siteID uuid.UUID, createdAt time.Time, cost float64) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"cost_calculation": map[string]any{
			"base_estimated_cost": cost,
		},
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	requestLogID := uuid.New()
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO request_logs (
			id, request_id, site_id, endpoint, status_code, success, metadata, created_at
		) VALUES (?, ?, ?, ?, 200, TRUE, ?::jsonb, ?)
	`, requestLogID, "oauth-quota-"+uuid.NewString(), siteID, "/v1/responses", string(raw), createdAt).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO usage_records (
			id, request_log_id, site_id, prompt_tokens, completion_tokens, total_tokens, estimated_cost, currency, created_at
		) VALUES (?, ?, ?, 1, 0, 1, ?, 'USD', ?)
	`, uuid.New(), requestLogID, siteID, cost, createdAt).Error; err != nil {
		t.Fatalf("insert usage record: %v", err)
	}
}
