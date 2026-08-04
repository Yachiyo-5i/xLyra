package site

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
	"xlyra/server/internal/store"
)

func TestDevPostgresSiteReadOnlySmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresSiteSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactSiteDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db, "postgres-smoke-master-key")
	missingID := uuid.New()

	sites, err := service.List(ctx)
	if err != nil {
		t.Fatalf("list sites: %v", err)
	}
	assertSmokeSitesStable(t, sites)

	modelPrices, err := service.ListModelPrices(ctx, ModelPriceFilters{})
	if err != nil {
		t.Fatalf("list model prices: %v", err)
	}
	assertSmokeModelPricesStable(t, modelPrices)

	missingModelPrices, err := service.ListModelPrices(ctx, ModelPriceFilters{SiteID: missingID})
	if err != nil {
		t.Fatalf("list missing site model prices: %v", err)
	}
	if missingModelPrices.Count != 0 || len(missingModelPrices.Items) != 0 {
		t.Fatalf("missing site model price list = count %d items %d, want empty", missingModelPrices.Count, len(missingModelPrices.Items))
	}

	missingModels, err := service.ListModels(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site models: %v", err)
	}
	if len(missingModels) != 0 {
		t.Fatalf("missing site models length = %d, want 0", len(missingModels))
	}

	states, err := service.SiteHealthStates(ctx)
	if err != nil {
		t.Fatalf("list site health states: %v", err)
	}
	assertSmokeSiteHealthStatesStable(t, states)

	if _, err := service.SiteHealthHistory(ctx, missingID, 3, "scheduler"); !errorsIsRecordNotFound(err) {
		t.Fatalf("missing site health history error = %v, want record not found", err)
	}
	if _, err := service.SiteHealthHourly(ctx, missingID, 3); !errorsIsRecordNotFound(err) {
		t.Fatalf("missing site health hourly error = %v, want record not found", err)
	}

	for _, item := range sites {
		history, err := service.SiteHealthHistory(ctx, item.ID, 3, "")
		if err != nil {
			t.Fatalf("site health history for %s: %v", item.ID, err)
		}
		assertSmokeHealthSnapshotsNewestFirst(t, history, 3)

		hourly, err := service.SiteHealthHourly(ctx, item.ID, 3)
		if err != nil {
			t.Fatalf("site health hourly for %s: %v", item.ID, err)
		}
		assertSmokeHealthHourlyBucketsStable(t, hourly, 3)

		models, err := service.ListModels(ctx, item.ID)
		if err != nil {
			t.Fatalf("list site models for %s: %v", item.ID, err)
		}
		assertSmokeSiteModelsVisibleAndSorted(t, models)
		return
	}
}

func devPostgresSiteSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           siteSmokeGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           siteSmokeGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           siteSmokeGetenvDefault("DB_USER", "postgres"),
		DBPassword:       siteSmokeGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        siteSmokeGetenvDefault("DB_SSLMODE", "disable"),
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

func siteSmokeGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactSiteDatabaseOpenError(err error, cfg config.Config) string {
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

func errorsIsRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func assertSmokeSitesStable(t *testing.T, sites []store.Site) {
	t.Helper()
	for index, item := range sites {
		if store.SiteDeleted(item) {
			t.Fatalf("active site list included deleted site %s", item.ID)
		}
		if index > 0 && item.CreatedAt.After(sites[index-1].CreatedAt) {
			t.Fatalf("sites are not newest first at index %d", index)
		}
	}
}

func assertSmokeModelPricesStable(t *testing.T, list ModelPriceList) {
	t.Helper()
	if list.Count != len(list.Items) {
		t.Fatalf("model price count = %d, items = %d", list.Count, len(list.Items))
	}
	if list.PricedCount < 0 || list.MissingCount < 0 || list.ManualCount < 0 {
		t.Fatalf("model price counts should be non-negative: %#v", list)
	}
	if list.ManualCount > list.PricedCount || list.PricedCount+list.MissingCount != len(list.Items) {
		t.Fatalf("model price counts are inconsistent: %#v", list)
	}
	for index, item := range list.Items {
		if item.Site.ID == uuid.Nil || item.Model.ID == uuid.Nil {
			t.Fatalf("model price item %d has nil identifiers: %#v", index, item)
		}
		if item.Model.Status == "unavailable" {
			t.Fatalf("model price item %d exposed unavailable model: %#v", index, item.Model)
		}
		if index == 0 {
			continue
		}
		previous := list.Items[index-1]
		if previous.Site.Name > item.Site.Name ||
			(previous.Site.Name == item.Site.Name && modelPriceDisplayKey(previous) > modelPriceDisplayKey(item)) ||
			(previous.Site.Name == item.Site.Name && modelPriceDisplayKey(previous) == modelPriceDisplayKey(item) && pricingGroupName(previous.Pricing) > pricingGroupName(item.Pricing)) {
			t.Fatalf("model price items are not sorted at index %d", index)
		}
	}
}

func assertSmokeSiteHealthStatesStable(t *testing.T, states map[uuid.UUID]store.SiteHealthState) {
	t.Helper()
	for siteID, state := range states {
		if siteID == uuid.Nil || state.SiteID == uuid.Nil || siteID != state.SiteID {
			t.Fatalf("site health state key mismatch: key=%s state=%#v", siteID, state)
		}
		if state.ConsecutiveFailures < 0 {
			t.Fatalf("site health consecutive failures should be non-negative: %#v", state)
		}
	}
}

func assertSmokeHealthSnapshotsNewestFirst(t *testing.T, snapshots []store.HealthSnapshot, limit int) {
	t.Helper()
	if len(snapshots) > limit {
		t.Fatalf("health snapshots length = %d, want at most %d", len(snapshots), limit)
	}
	for index, snapshot := range snapshots {
		if snapshot.SiteID == uuid.Nil || snapshot.Scope != "site" {
			t.Fatalf("health snapshot %d is not site-scoped: %#v", index, snapshot)
		}
		if index > 0 && snapshot.CheckedAt.After(snapshots[index-1].CheckedAt) {
			t.Fatalf("health snapshots are not newest first at index %d", index)
		}
	}
}

func assertSmokeHealthHourlyBucketsStable(t *testing.T, buckets []HealthHourlyBucket, hours int) {
	t.Helper()
	if len(buckets) != hours {
		t.Fatalf("health hourly buckets length = %d, want %d", len(buckets), hours)
	}
	for index, bucket := range buckets {
		if bucket.SuccessCount < 0 || bucket.FailureCount < 0 || bucket.TotalCount < 0 {
			t.Fatalf("health hourly counts should be non-negative at index %d: %#v", index, bucket)
		}
		if bucket.SuccessCount+bucket.FailureCount != bucket.TotalCount {
			t.Fatalf("health hourly counts are inconsistent at index %d: %#v", index, bucket)
		}
		if index > 0 && bucket.BucketStart.Before(buckets[index-1].BucketStart) {
			t.Fatalf("health hourly buckets are not ascending at index %d", index)
		}
	}
}

func assertSmokeSiteModelsVisibleAndSorted(t *testing.T, models []store.SiteModel) {
	t.Helper()
	for index, model := range models {
		if model.ID == uuid.Nil || model.SiteID == uuid.Nil {
			t.Fatalf("site model %d has nil identifiers: %#v", index, model)
		}
		if model.Status == "unavailable" {
			t.Fatalf("visible site models included unavailable model: %#v", model)
		}
		if index > 0 && model.UpstreamName < models[index-1].UpstreamName {
			t.Fatalf("site models are not sorted by upstream name at index %d", index)
		}
	}
}
