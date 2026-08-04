package dashboard

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestDevPostgresDashboardSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, err := devPostgresDashboardSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactDashboardDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db, config.LoadTimeZone(config.DefaultTimeZone))
	now := time.Now()

	usage, err := service.Usage(ctx, now)
	if err != nil {
		t.Fatalf("dashboard usage: %v", err)
	}
	assertDashboardUsageStable(t, usage)

	cooldowns, err := service.Cooldowns(ctx, now)
	if err != nil {
		t.Fatalf("dashboard cooldowns: %v", err)
	}
	assertDashboardCooldownsStable(t, cooldowns)

	health, err := service.Health(ctx, now)
	if err != nil {
		t.Fatalf("dashboard health: %v", err)
	}
	assertDashboardHealthStable(t, health)

	insights, err := service.Insights(ctx, now)
	if err != nil {
		t.Fatalf("dashboard insights: %v", err)
	}
	assertDashboardInsightsStable(t, insights)

	epaper, err := service.EpaperSummary(ctx, now)
	if err != nil {
		t.Fatalf("dashboard epaper summary: %v", err)
	}
	assertDashboardEpaperSummaryStable(t, epaper)
}

func devPostgresDashboardSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           dashboardGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           dashboardGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           dashboardGetenvDefault("DB_USER", "postgres"),
		DBPassword:       dashboardGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        dashboardGetenvDefault("DB_SSLMODE", "disable"),
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

func dashboardGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactDashboardDatabaseOpenError(err error, cfg config.Config) string {
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

func assertDashboardUsageStable(t *testing.T, usage UsageOverview) {
	t.Helper()
	if usage.Meta.Days != overviewDays || usage.Meta.Timezone == "" || usage.Meta.GeneratedAt == "" || usage.Meta.TodayStart == "" || usage.Meta.RangeStart == "" || usage.Meta.RangeEnd == "" {
		t.Fatalf("usage meta is incomplete: %#v", usage.Meta)
	}
	if len(usage.Meta.AvailableDays) != len(overviewWindowDays) {
		t.Fatalf("available days = %#v, want %d entries", usage.Meta.AvailableDays, len(overviewWindowDays))
	}
	if usage.KPIs.Cost.Today < 0 || usage.KPIs.Cost.Yesterday < 0 || usage.KPIs.Cost.Total < 0 {
		t.Fatalf("cost KPIs should be non-negative: %#v", usage.KPIs.Cost)
	}
	if usage.KPIs.Requests.Today < 0 || usage.KPIs.Requests.Yesterday < 0 || usage.KPIs.Requests.Total < 0 || usage.KPIs.Requests.TotalTokens < 0 {
		t.Fatalf("request KPIs should be non-negative: %#v", usage.KPIs.Requests)
	}
	assertDashboardSiteCostSummaryStable(t, usage.Charts.SiteCostSummary)
	for key, window := range usage.Windows {
		if window.Days <= 0 || window.RangeStart == "" || window.RangeEnd == "" {
			t.Fatalf("overview window %q is incomplete: %#v", key, window)
		}
		assertDashboardSiteCostSummaryStable(t, window.SiteCostSummary)
		assertDashboardFailureReasonsStable(t, window.FailureReasons)
		assertDashboardHighLatencyStable(t, window.HighLatency)
	}
}

func assertDashboardCooldownsStable(t *testing.T, cooldowns OverviewCooldowns) {
	t.Helper()
	for _, item := range cooldowns.Items {
		if item.ID == "" || item.SiteID == "" || item.ActiveUntil == "" {
			t.Fatalf("cooldown item is incomplete: %#v", item)
		}
		if item.RemainingSeconds < 0 {
			t.Fatalf("cooldown remaining seconds = %d, want non-negative", item.RemainingSeconds)
		}
	}
}

func assertDashboardHealthStable(t *testing.T, health OverviewHealth) {
	t.Helper()
	for _, row := range health.UptimeRows {
		if row.SiteID == "" || row.SiteName == "" {
			t.Fatalf("uptime row is incomplete: %#v", row)
		}
		if len(row.Buckets) != uptimeBucketCount {
			t.Fatalf("uptime bucket count = %d, want %d", len(row.Buckets), uptimeBucketCount)
		}
		for _, bucket := range row.Buckets {
			if bucket.Hour == "" {
				t.Fatalf("uptime bucket hour is empty: %#v", bucket)
			}
			switch bucket.Status {
			case "idle", "healthy", "unhealthy", "degraded":
			default:
				t.Fatalf("unexpected uptime bucket status %q", bucket.Status)
			}
			if bucket.SuccessCount < 0 || bucket.FailureCount < 0 || bucket.TotalCount < 0 {
				t.Fatalf("uptime bucket counts should be non-negative: %#v", bucket)
			}
		}
	}
}

func assertDashboardInsightsStable(t *testing.T, insights InsightsOverview) {
	t.Helper()
	assertDashboardFailureReasonsStable(t, insights.Insights.FailureReasons)
	assertDashboardHighLatencyStable(t, insights.Insights.HighLatency)
	for _, item := range insights.Insights.InsufficientCandidates {
		if item.CanonicalModelID == "" || item.ModelKey == "" {
			t.Fatalf("insufficient candidate item is incomplete: %#v", item)
		}
		if item.SiteModelCount < 0 || item.SiteCount < 0 || item.EligibleCount < 0 || item.CooldownCount < 0 || item.RequestCount24h < 0 {
			t.Fatalf("insufficient candidate counts should be non-negative: %#v", item)
		}
	}
	for _, item := range insights.Attention.Items {
		if item.ID == "" || item.Type == "" || item.Severity == "" || item.UpdatedAt == "" {
			t.Fatalf("attention item is incomplete: %#v", item)
		}
	}
}

func assertDashboardEpaperSummaryStable(t *testing.T, summary EpaperSummary) {
	t.Helper()
	if summary.Date == "" || summary.RefreshAt <= 0 {
		t.Fatalf("epaper summary is incomplete: %#v", summary)
	}
	if summary.KPIs.TodayCost < 0 || summary.KPIs.TotalCost < 0 || summary.KPIs.TodayRequests < 0 || summary.KPIs.TotalRequests < 0 || summary.KPIs.TodayTokens < 0 || summary.KPIs.TotalTokens < 0 {
		t.Fatalf("epaper KPIs should be non-negative: %#v", summary.KPIs)
	}
	for _, item := range summary.ModelTop3Today {
		if item.ModelKey == "" || item.Cost < 0 {
			t.Fatalf("epaper model cost item is invalid: %#v", item)
		}
	}
	if summary.CodexQuota.AccountCount < 0 {
		t.Fatalf("codex quota account count = %d, want non-negative", summary.CodexQuota.AccountCount)
	}
}

func assertDashboardSiteCostSummaryStable(t *testing.T, items []SiteCostSummaryItem) {
	t.Helper()
	for _, item := range items {
		if item.SiteID == "" || item.SiteName == "" {
			t.Fatalf("site cost summary item is incomplete: %#v", item)
		}
		if item.RequestCount < 0 || item.SuccessCount < 0 || item.TotalTokens < 0 || item.Cost < 0 {
			t.Fatalf("site cost summary counts should be non-negative: %#v", item)
		}
	}
}

func assertDashboardFailureReasonsStable(t *testing.T, items []FailureReasonItem) {
	t.Helper()
	for _, item := range items {
		if item.Reason == "" || item.RequestCount < 0 {
			t.Fatalf("failure reason item is invalid: %#v", item)
		}
	}
}

func assertDashboardHighLatencyStable(t *testing.T, items []HighLatencyItem) {
	t.Helper()
	for _, item := range items {
		if item.ModelKey == "" || item.RequestCount < 0 || item.AvgLatencyMS < 0 || item.P95LatencyMS < 0 {
			t.Fatalf("high latency item is invalid: %#v", item)
		}
	}
}
