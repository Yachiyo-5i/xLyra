package dashboard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

var serviceQueryEdgesNow = time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)

func TestUsageFromSummariesWrapsRateLimitQueryError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("rate limit query offline")
	service, window := serviceQueryEdgesServiceWithError(t, queryErr)

	_, err := service.usageFromSummaries(context.Background(), window, nil)
	if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "dashboard rate limit kpi") {
		t.Fatalf("usageFromSummaries error = %v, want wrapped rate limit query error", err)
	}
}

func TestUsageFromSummariesWrapsAPIKeyNameQueryError(t *testing.T) {
	t.Parallel()

	apiKeyErr := errors.New("api key lookup offline")
	apiKeyID := uuid.New()
	service, window := serviceQueryEdgesService(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.GatewayRateLimit:
			*dest = nil
		case *[]store.RequestLog:
			*dest = nil
		case *[]store.Site:
			*dest = nil
		case *[]store.APIKey:
			tx.AddError(apiKeyErr)
		default:
			tx.AddError(errors.New("unexpected dashboard query destination"))
		}
	})

	_, err := service.usageFromSummaries(context.Background(), window, []store.RequestUsageDailySummary{
		{BucketStart: window.TodayStart, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, APIKeyName: "Old Name", TotalTokens: 1},
	})
	if !errors.Is(err, apiKeyErr) || !strings.Contains(err.Error(), "dashboard api key names") {
		t.Fatalf("usageFromSummaries error = %v, want wrapped api key name query error", err)
	}
}

func TestHighLatencyWrapsRequestLogQueryError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("request log query offline")
	service, window := serviceQueryEdgesServiceWithError(t, queryErr)

	_, err := service.highLatency(context.Background(), window)
	if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "dashboard high latency") {
		t.Fatalf("highLatency error = %v, want wrapped request log query error", err)
	}
}

func TestCooldownsWrapsSiteLookupErrorAfterLoadingActiveRows(t *testing.T) {
	t.Parallel()

	siteErr := errors.New("cooldown site lookup offline")
	siteID := uuid.New()
	service, window := serviceQueryEdgesService(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.RouteCooldown:
			*dest = []store.RouteCooldown{{
				ID:          uuid.New(),
				SiteID:      siteID,
				Scope:       "site",
				Source:      "test",
				Reason:      "active cooldown",
				ActiveUntil: time.Date(2026, 6, 23, 13, 0, 0, 0, time.UTC),
			}}
			tx.RowsAffected = 1
		case *[]store.Site:
			tx.AddError(siteErr)
		default:
			tx.AddError(errors.New("unexpected cooldown dashboard query destination"))
		}
	})

	_, err := service.cooldowns(context.Background(), window)
	if !errors.Is(err, siteErr) || !strings.Contains(err.Error(), "dashboard cooldowns") {
		t.Fatalf("cooldowns error = %v, want wrapped site lookup error", err)
	}
}

func serviceQueryEdgesServiceWithError(t *testing.T, queryErr error) (*Service, timeWindow) {
	t.Helper()

	return serviceQueryEdgesService(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})
}

func serviceQueryEdgesService(t *testing.T, query func(*gorm.DB)) (*Service, timeWindow) {
	t.Helper()

	service := NewService(dashboardStoreWithQueryCallback(t, query), config.LoadTimeZone("UTC"))
	return service, service.newTimeWindow(1, serviceQueryEdgesNow)
}
