package site

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestSyncAPIKeyModelsForAddedGroupSitesSkipsEmptySiteIDsBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	if err := syncAPIKeyModelsForAddedGroupSites(t.Context(), nil, uuid.New(), nil); err != nil {
		t.Fatalf("syncAPIKeyModelsForAddedGroupSites empty site IDs error = %v, want nil", err)
	}
}

func TestCleanupDeletedSiteRelationsPropagatesModelListError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site models unavailable")
	db := siteGormWithQueryError(t, queryErr)

	err := cleanupDeletedSiteRelations(context.Background(), db, uuid.New())
	assertSiteQueryError(t, "cleanupDeletedSiteRelations", err, queryErr)
}

func TestSyncSiteHealthCooldownPropagatesUnhealthyRouteCooldownClearError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("cooldown lookup unavailable")
	service := siteServiceFailingRouteCooldownQueries(t, queryErr)

	err := service.syncSiteHealthCooldown(context.Background(), uuid.New(), store.SiteHealthState{
		Status: "unhealthy",
	}, time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC), "scheduler")
	assertSiteWrappedQueryError(t, "syncSiteHealthCooldown unhealthy", err, queryErr, "clear existing route cooldowns")
}

func TestSyncSiteHealthCooldownPropagatesHealthyRouteCooldownClearError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("active cooldowns unavailable")
	service := siteServiceFailingRouteCooldownQueries(t, queryErr)

	err := service.syncSiteHealthCooldown(context.Background(), uuid.New(), store.SiteHealthState{
		Status: "healthy",
	}, time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC), "manual")
	assertSiteWrappedQueryError(t, "syncSiteHealthCooldown healthy", err, queryErr, "clear route cooldowns")
}

func siteServiceFailingRouteCooldownQueries(t *testing.T, queryErr error) *Service {
	t.Helper()

	db := siteTransactionPostgresGorm(t)
	siteReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})
	return NewService(siteStoreWithGorm(t, db), siteTestMasterKey)
}
