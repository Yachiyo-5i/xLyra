package oauth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestResolveImportedOAuthSiteReturnsBoundSiteOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	service := importedSiteResolutionService(t, func(tx *gorm.DB) {
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected bound site query destination"))
			return
		}
		*site = store.Site{ID: siteID, Name: "Bound Codex"}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("bound site resolution should not update"))
	})

	gotID, gotName, err := resolveImportedOAuthSiteForTest(t, service, store.OAuthConnection{SiteID: &siteID}, codexProvider, "user@example.com", true)
	if err != nil {
		t.Fatalf("resolveImportedOAuthSite returned error: %v", err)
	}
	if gotID != siteID || gotName != "Bound Codex" {
		t.Fatalf("resolveImportedOAuthSite = %s/%q, want bound site", gotID, gotName)
	}
}

func TestResolveImportedOAuthSiteWrapsBoundSiteLookupErrorOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	lookupErr := errors.New("bound site lookup stopped")
	service := importedSiteResolutionService(t, func(tx *gorm.DB) {
		tx.AddError(lookupErr)
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("bound site lookup failure should not update"))
	})

	gotID, gotName, err := resolveImportedOAuthSiteForTest(t, service, store.OAuthConnection{SiteID: &siteID}, codexProvider, "user@example.com", true)
	if gotID != uuid.Nil || gotName != "" {
		t.Fatalf("resolveImportedOAuthSite = %s/%q, want zero values", gotID, gotName)
	}
	if err == nil || !strings.Contains(err.Error(), "get bound site") || !strings.Contains(err.Error(), lookupErr.Error()) {
		t.Fatalf("resolveImportedOAuthSite error = %v, want wrapped bound lookup error", err)
	}
}

func TestResolveImportedOAuthSiteReturnsExistingOAuthSiteOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	service := importedSiteResolutionService(t, func(tx *gorm.DB) {
		sites, ok := tx.Statement.Dest.(*[]store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected oauth site query destination"))
			return
		}
		*sites = []store.Site{{
			ID:       siteID,
			Name:     "Existing OAuth",
			SiteType: codexProvider,
			Status:   "active",
			Meta:     store.JSON(`{"oauth_email":"user@example.com"}`),
		}}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("existing oauth site should not update"))
	})

	gotID, gotName, err := resolveImportedOAuthSiteForTest(t, service, store.OAuthConnection{AccountID: " acct-123 "}, codexProvider, " user@example.com ", false)
	if err != nil {
		t.Fatalf("resolveImportedOAuthSite returned error: %v", err)
	}
	if gotID != siteID || gotName != "Existing OAuth" {
		t.Fatalf("resolveImportedOAuthSite = %s/%q, want existing oauth site", gotID, gotName)
	}
}

func TestResolveImportedOAuthSiteStopsAfterSlugCollisionsOffline(t *testing.T) {
	t.Parallel()

	queryCount := 0
	service := importedSiteResolutionService(t, func(tx *gorm.DB) {
		queryCount++
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Site:
			*dest = nil
			tx.AddError(gorm.ErrRecordNotFound)
		case *store.Site:
			*dest = store.Site{ID: uuid.New(), Name: "Slug Collision"}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected collision query destination"))
		}
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("slug collision exhaustion should not update"))
	})

	gotID, gotName, err := resolveImportedOAuthSiteForTest(t, service, store.OAuthConnection{AccountID: "acct-123"}, codexProvider, "user@example.com", true)
	if gotID != uuid.Nil || gotName != "" {
		t.Fatalf("resolveImportedOAuthSite = %s/%q, want zero values", gotID, gotName)
	}
	if err == nil || !strings.Contains(err.Error(), "too many collisions") {
		t.Fatalf("resolveImportedOAuthSite error = %v, want collision exhaustion", err)
	}
	if queryCount != 6 {
		t.Fatalf("query count = %d, want oauth lookup plus five slug checks", queryCount)
	}
}

func importedSiteResolutionService(t *testing.T, query func(*gorm.DB), update func(*gorm.DB)) *ImportService {
	t.Helper()

	return NewImportService(oauthStoreWithGorm(t, oauthGormWithQueryUpdate(t, query, update)), "master-key", nil)
}

func resolveImportedOAuthSiteForTest(t *testing.T, service *ImportService, connection store.OAuthConnection, provider string, email string, enabled bool) (uuid.UUID, string, error) {
	t.Helper()

	return service.resolveImportedOAuthSite(context.Background(), store.NewSiteRepository(service.db.DB()), connection, provider, email, enabled)
}
