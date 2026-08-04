package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

const dashboardAttentionGeneratedAt = "2026-06-23T13:00:00Z"

func newDashboardServiceWithQueryCallback(t *testing.T, query func(*gorm.DB)) *Service {
	t.Helper()

	return NewService(dashboardStoreWithQueryCallback(t, query), config.LoadTimeZone("UTC"))
}

func TestDashboardServiceMethodsReturnNilStoreError(t *testing.T) {
	t.Parallel()

	service := NewService(nil, config.LoadTimeZone("UTC"))
	ctx := context.Background()
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	publicMethods := []struct {
		name string
		call func() error
	}{
		{name: "usage", call: func() error { _, err := service.Usage(ctx, now); return err }},
		{name: "cooldowns", call: func() error { _, err := service.Cooldowns(ctx, now); return err }},
		{name: "health", call: func() error { _, err := service.Health(ctx, now); return err }},
		{name: "insights", call: func() error { _, err := service.Insights(ctx, now); return err }},
		{name: "epaper", call: func() error { _, err := service.EpaperSummary(ctx, now); return err }},
	}

	for _, method := range publicMethods {
		method := method
		t.Run(method.name, func(t *testing.T) {
			t.Parallel()

			err := method.call()
			if err == nil || !strings.Contains(err.Error(), "dashboard store is not initialized") {
				t.Fatalf("expected nil store error, got %v", err)
			}
		})
	}
}

func TestServiceTimeZoneUsesDefaultForEmptyConfig(t *testing.T) {
	t.Parallel()

	timeZone := serviceTimeZone(config.TimeZone{})
	if timeZone.Location == nil {
		t.Fatalf("expected fallback timezone location, got %#v", timeZone)
	}
	if timeZone.Name == "" {
		t.Fatalf("expected fallback timezone name, got %#v", timeZone)
	}
}

func TestSummarySiteSnapshotUsesLegacyNameAndTypeFallbacks(t *testing.T) {
	t.Parallel()

	nameOnly := resolveSummarySiteSnapshot(store.RequestUsageDailySummary{
		SiteName: "Legacy Name",
		SiteType: "codex",
	}, nil)
	if nameOnly.aggregateKey != "snapshot-name:Legacy Name" || nameOnly.siteKey != "Legacy Name" {
		t.Fatalf("expected name-only snapshot key, got %#v", nameOnly)
	}

	typeOnly := resolveSummarySiteSnapshot(store.RequestUsageDailySummary{SiteType: "openai"}, nil)
	if typeOnly.aggregateKey != "snapshot-type:openai" || typeOnly.siteKey != "unknown" {
		t.Fatalf("expected type-only snapshot key with unknown display key, got %#v", typeOnly)
	}
}

func TestDashboardUptimeSiteFilterCompactsActiveSites(t *testing.T) {
	t.Parallel()

	activeID := uuid.New()
	disabledID := uuid.New()
	deletedID := uuid.New()
	sites := []store.Site{
		{ID: disabledID, Enabled: false, Status: "active"},
		{ID: activeID, Enabled: true, Status: "active"},
		{ID: deletedID, Enabled: true, Status: store.SiteStatusDeleted},
	}

	filtered := filterDashboardUptimeSites(sites)
	if len(filtered) != 1 || filtered[0].ID != activeID {
		t.Fatalf("expected only active site, got %#v", filtered)
	}
	if len(sites) == 0 || sites[0].ID != activeID {
		t.Fatalf("expected active site to be compacted into input slice prefix, got %#v", sites)
	}
}

func TestDashboardServiceQueryErrorsPropagateFromSiteAndQuotaReaders(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("offline query failed")
	service := newDashboardServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(storeErr)
	})
	ctx := context.Background()
	window := service.newTimeWindow(1, time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC))

	if _, err := service.activeSiteList(ctx); !errors.Is(err, storeErr) {
		t.Fatalf("activeSiteList error = %v, want store query error", err)
	}
	if _, err := service.activeSitesByID(ctx); !errors.Is(err, storeErr) {
		t.Fatalf("activeSitesByID error = %v, want store query error", err)
	}
	if _, err := service.rateLimitKPI(ctx, window); !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "dashboard rate limit kpi") {
		t.Fatalf("rateLimitKPI error = %v, want wrapped store query error", err)
	}
	if _, err := service.epaperCodexQuota(ctx); !errors.Is(err, storeErr) || !strings.Contains(err.Error(), "dashboard epaper codex quota sites") {
		t.Fatalf("epaperCodexQuota active site error = %v, want wrapped store query error", err)
	}
}

func TestEpaperCodexQuotaReturnsWrappedStateQueryError(t *testing.T) {
	t.Parallel()

	stateErr := errors.New("offline state query failed")
	codexSiteID := uuid.New()
	service := newDashboardServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Site:
			*dest = []store.Site{{ID: codexSiteID, SiteType: "codex", Status: "active", Enabled: true}}
			tx.RowsAffected = 1
		case *[]store.SiteState:
			tx.AddError(stateErr)
		default:
			tx.AddError(fmt.Errorf("unexpected dashboard query destination %T", tx.Statement.Dest))
		}
	})

	_, err := service.epaperCodexQuota(context.Background())
	if !errors.Is(err, stateErr) || !strings.Contains(err.Error(), "dashboard epaper codex quota states") {
		t.Fatalf("epaperCodexQuota state error = %v, want wrapped state query error", err)
	}
}

func TestUnhealthySiteAttentionReportsEnabledSitesByFailureCount(t *testing.T) {
	t.Parallel()

	newer := time.Date(2026, 6, 23, 13, 0, 0, 0, time.UTC)
	older := newer.Add(-time.Hour)
	firstSiteID := uuid.New()
	secondSiteID := uuid.New()
	disabledSiteID := uuid.New()
	service := newDashboardServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteHealthState:
			*dest = []store.SiteHealthState{
				{SiteID: disabledSiteID, Status: "unhealthy", ConsecutiveFailures: 99, CheckedAt: sql.NullTime{Time: newer, Valid: true}},
				{SiteID: secondSiteID, Status: "unhealthy", ConsecutiveFailures: 3, CheckedAt: sql.NullTime{Time: newer, Valid: true}},
				{SiteID: firstSiteID, Status: "unhealthy", ConsecutiveFailures: 5, CheckedAt: sql.NullTime{Time: older, Valid: true}, Message: sql.NullString{String: "timeout", Valid: true}},
				{SiteID: firstSiteID, Status: "healthy", ConsecutiveFailures: 100, CheckedAt: sql.NullTime{Time: newer, Valid: true}},
			}
			tx.RowsAffected = int64(len(*dest))
		case *[]store.Site:
			*dest = []store.Site{
				{ID: firstSiteID, Name: "First", Slug: "first", Status: "active", Enabled: true},
				{ID: secondSiteID, Name: "Second", Slug: "second", Status: "active", Enabled: true},
				{ID: disabledSiteID, Name: "Disabled", Slug: "disabled", Status: "active", Enabled: false},
			}
			tx.RowsAffected = int64(len(*dest))
		default:
			tx.AddError(fmt.Errorf("unexpected dashboard query destination %T", tx.Statement.Dest))
		}
	})

	items, err := service.unhealthySiteAttention(context.Background(), dashboardAttentionGeneratedAt)
	if err != nil {
		t.Fatalf("unhealthySiteAttention: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected two enabled unhealthy site items, got %#v", items)
	}
	if items[0].ID != "site:"+firstSiteID.String()+":unhealthy" || items[1].ID != "site:"+secondSiteID.String()+":unhealthy" {
		t.Fatalf("expected items sorted by consecutive failures, got %#v", items)
	}
	if items[0].Metrics["message"] == nil || items[0].Action.Params["site_id"] != firstSiteID.String() {
		t.Fatalf("expected first unhealthy item to include message and site action, got %#v", items[0])
	}
}

func TestMissingPricingAttentionCountsCodexModelsWithoutAvailablePrices(t *testing.T) {
	t.Parallel()

	activeSiteID := uuid.New()
	newAPISiteID := uuid.New()
	disabledSiteID := uuid.New()
	withPricingModelID := uuid.New()
	missingModelID := uuid.New()
	secondMissingModelID := uuid.New()
	canonicalID := uuid.New()
	secondCanonicalID := uuid.New()
	service := newDashboardServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteModel:
			*dest = []store.SiteModel{
				{ID: missingModelID, SiteID: activeSiteID, CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}, Status: "active"},
				{ID: secondMissingModelID, SiteID: activeSiteID, CanonicalID: uuid.NullUUID{UUID: secondCanonicalID, Valid: true}, Status: "active"},
				{ID: withPricingModelID, SiteID: activeSiteID, CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}, Status: "active"},
				{ID: uuid.New(), SiteID: newAPISiteID, Status: "active"},
				{ID: uuid.New(), SiteID: disabledSiteID, Status: "active"},
				{ID: uuid.New(), SiteID: activeSiteID, Status: "disabled"},
			}
			tx.RowsAffected = int64(len(*dest))
		case *[]store.Site:
			*dest = []store.Site{
				{ID: activeSiteID, SiteType: "codex", Status: "active", Enabled: true},
				{ID: newAPISiteID, SiteType: "newapi", Status: "active", Enabled: true},
				{ID: disabledSiteID, SiteType: "codex", Status: "active", Enabled: false},
			}
			tx.RowsAffected = int64(len(*dest))
		case *[]store.SiteModelPricing:
			*dest = []store.SiteModelPricing{
				{SiteModelID: uuid.NullUUID{UUID: withPricingModelID, Valid: true}, BillingType: "tokens", InputValue: sql.NullFloat64{Float64: 0.01, Valid: true}, Available: true},
				{SiteModelID: uuid.NullUUID{UUID: missingModelID, Valid: true}, Available: false},
			}
			tx.RowsAffected = int64(len(*dest))
		default:
			tx.AddError(fmt.Errorf("unexpected dashboard query destination %T", tx.Statement.Dest))
		}
	})

	item, err := service.missingPricingAttention(context.Background(), dashboardAttentionGeneratedAt)
	if err != nil {
		t.Fatalf("missingPricingAttention: %v", err)
	}
	if item == nil {
		t.Fatal("expected missing pricing attention item, got nil")
	}
	if item.Metrics["missing_count"] != int64(2) || item.Metrics["model_count"] != int64(2) || item.Metrics["site_count"] != int64(1) {
		t.Fatalf("unexpected missing pricing metrics: %#v", item.Metrics)
	}
	if item.Action.Type != "open_model_prices" || item.Action.Params["pricing_status"] != "missing" {
		t.Fatalf("unexpected missing pricing action: %#v", item.Action)
	}
}

func TestMissingPricingAttentionSkipsFullyPricedEligibleModels(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	service := newDashboardServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteModel:
			*dest = []store.SiteModel{{ID: modelID, SiteID: siteID, Status: "active"}}
			tx.RowsAffected = 1
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, SiteType: "codex", Status: "active", Enabled: true}}
			tx.RowsAffected = 1
		case *[]store.SiteModelPricing:
			*dest = []store.SiteModelPricing{{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, BillingType: "tokens", InputValue: sql.NullFloat64{Float64: 0.01, Valid: true}, Available: true}}
			tx.RowsAffected = 1
		default:
			tx.AddError(fmt.Errorf("unexpected dashboard query destination %T", tx.Statement.Dest))
		}
	})

	item, err := service.missingPricingAttention(context.Background(), dashboardAttentionGeneratedAt)
	if err != nil {
		t.Fatalf("missingPricingAttention: %v", err)
	}
	if item != nil {
		t.Fatalf("expected no missing pricing item, got %#v", item)
	}
}
