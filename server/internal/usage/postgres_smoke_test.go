package usage

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

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"

	"gorm.io/gorm"
)

func TestDevPostgresUsageSummarySmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresUsageSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactUsageDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db, config.LoadTimeZone(config.DefaultTimeZone))
	rows, err := service.UsageSummaryBySite(ctx, nil)
	if err != nil {
		t.Fatalf("usage summary by site: %v", err)
	}
	assertUsageSummaryRowsStable(t, rows)

	after := time.Now().AddDate(0, 0, -30)
	recentRows, err := service.UsageSummaryBySite(ctx, &after)
	if err != nil {
		t.Fatalf("usage summary by site after cutoff: %v", err)
	}
	assertUsageSummaryRowsStable(t, recentRows)

	missing, found, err := service.UsageSummaryForSite(ctx, uuid.New(), nil)
	if err != nil {
		t.Fatalf("usage summary for missing site: %v", err)
	}
	if found || missing.SiteID != uuid.Nil {
		t.Fatalf("missing site summary = %#v found=%v, want zero false", missing, found)
	}

	if len(rows) == 0 {
		return
	}

	row, found, err := service.UsageSummaryForSite(ctx, rows[0].SiteID, nil)
	if err != nil {
		t.Fatalf("usage summary for known site: %v", err)
	}
	if !found {
		t.Fatalf("usage summary for known site %s was not found", rows[0].SiteID)
	}
	if row.SiteID != rows[0].SiteID {
		t.Fatalf("usage summary site id = %s, want %s", row.SiteID, rows[0].SiteID)
	}
	assertUsageSummaryRowsStable(t, []store.SiteUsageSummaryRow{row})
}

func TestDevPostgresUsageServiceEntriesSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresUsageSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactUsageDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db, config.LoadTimeZone(config.DefaultTimeZone))
	page, err := service.ListRequestsPage(ctx, RequestQuery{Page: -1, PageSize: 3})
	if err != nil {
		t.Fatalf("list requests page: %v", err)
	}
	if page.Page != 1 || page.PageSize != 3 {
		t.Fatalf("request page pagination = page %d size %d, want page 1 size 3", page.Page, page.PageSize)
	}
	if page.Total < 0 || len(page.Items) > page.PageSize {
		t.Fatalf("request page has unstable counts: total=%d len=%d size=%d", page.Total, len(page.Items), page.PageSize)
	}
	assertUsageRequestDetailsStable(t, page.Items)

	limited, err := service.ListRequests(ctx, RequestQuery{Limit: 2})
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(limited) > 2 {
		t.Fatalf("limited requests length = %d, want at most 2", len(limited))
	}
	assertUsageRequestDetailsStable(t, limited)

	missing, err := service.GetRequest(ctx, uuid.New())
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get missing request = %#v err=%v, want record-not-found error", missing, err)
	}

	rateUsage, err := service.RecentRateUsage(ctx, time.Now())
	if err != nil {
		t.Fatalf("recent rate usage: %v", err)
	}
	if rateUsage.RPM < 0 || rateUsage.TPM < 0 {
		t.Fatalf("recent rate usage should be non-negative, got %#v", rateUsage)
	}

	_, err = service.ChannelSplit(ctx, ChannelSplitQuery{SiteID: uuidPtr(uuid.New())}, time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC))
	if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("channel split missing site error = %v, want record-not-found", err)
	}
}

func devPostgresUsageSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           usageGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           usageGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           usageGetenvDefault("DB_USER", "postgres"),
		DBPassword:       usageGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        usageGetenvDefault("DB_SSLMODE", "disable"),
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

func usageGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactUsageDatabaseOpenError(err error, cfg config.Config) string {
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

func assertUsageSummaryRowsStable(t *testing.T, rows []store.SiteUsageSummaryRow) {
	t.Helper()
	for index, row := range rows {
		if row.SiteID == uuid.Nil {
			t.Fatalf("usage summary row %d has nil site id: %#v", index, row)
		}
		if row.RequestCount < 0 || row.SuccessCount < 0 || row.FailedCount < 0 {
			t.Fatalf("usage summary row %d has negative counts: %#v", index, row)
		}
		if row.PromptTokens < 0 || row.CompletionTokens < 0 || row.TotalTokens < 0 {
			t.Fatalf("usage summary row %d has negative tokens: %#v", index, row)
		}
		if row.EstimatedCost < 0 {
			t.Fatalf("usage summary row %d has negative cost: %#v", index, row)
		}
		if index > 0 && row.EstimatedCost > rows[index-1].EstimatedCost {
			t.Fatalf("usage summary rows are not sorted by estimated cost at index %d", index)
		}
	}
}

func assertUsageRequestDetailsStable(t *testing.T, rows []store.RequestLogDetail) {
	t.Helper()
	for index, row := range rows {
		if row.ID == uuid.Nil {
			t.Fatalf("request detail row %d has nil id: %#v", index, row)
		}
		if row.RequestTokens.Valid && row.RequestTokens.Int64 < 0 {
			t.Fatalf("request detail row %d has negative request tokens: %#v", index, row)
		}
		if row.ResponseTokens.Valid && row.ResponseTokens.Int64 < 0 {
			t.Fatalf("request detail row %d has negative response tokens: %#v", index, row)
		}
		if row.UsageTotalTokens.Valid && row.UsageTotalTokens.Int64 < 0 {
			t.Fatalf("request detail row %d has negative usage tokens: %#v", index, row)
		}
		if row.EstimatedCost.Valid && row.EstimatedCost.Float64 < 0 {
			t.Fatalf("request detail row %d has negative estimated cost: %#v", index, row)
		}
		if index > 0 && row.CreatedAt.After(rows[index-1].CreatedAt) {
			t.Fatalf("request detail rows are not newest first at index %d", index)
		}
	}
}

func uuidPtr(value uuid.UUID) *uuid.UUID {
	return &value
}
