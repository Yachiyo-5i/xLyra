package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestServiceValidationRejectsInvalidModelKeyBeforeRepository(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	ctx := context.Background()

	_, err := service.Create(ctx, UpsertCanonicalModelInput{ModelKey: " !!! "})
	assertCatalogErrorContains(t, "Create", err, "model_key is required")

	_, err = service.Update(ctx, uuid.New(), UpsertCanonicalModelInput{ModelKey: " \t "})
	assertCatalogErrorContains(t, "Update", err, "model_key is required")
}

func TestArchiveRejectsAutoCreatedCanonicalBeforeCountingLinks(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		if dest, ok := tx.Statement.Dest.(*store.CanonicalModel); ok {
			*dest = store.CanonicalModel{
				ID:           canonicalID,
				ModelKey:     "gpt-4o",
				DisplayName:  "GPT-4o",
				Provider:     "openai",
				Category:     "chat",
				Capabilities: store.JSON(`{"auto_created":true,"source":"catalog_match"}`),
				Status:       "active",
			}
			return
		}
		tx.AddError(errors.New("unexpected repository query after archive guard"))
	})

	_, err := (&Service{db: catalogStoreWithGorm(t, db)}).Archive(context.Background(), canonicalID)
	assertCatalogErrorContains(t, "Archive", err, "only manually created canonical models can be archived manually")
}

func TestBindSiteModelPropagatesUnbindUpdateError(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New()
	updateErr := errors.New("site model unbind update stopped")
	db := catalogTransactionPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		if dest, ok := tx.Statement.Dest.(*store.SiteModel); ok {
			*dest = store.SiteModel{
				ID:           siteModelID,
				SiteID:       uuid.New(),
				UpstreamName: "openai/gpt-4o",
				Status:       "available",
			}
			return
		}
		tx.AddError(errors.New("unexpected repository query during unbind"))
	})
	replaceCatalogUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(updateErr)
	})

	_, err := (&Service{db: catalogStoreWithGorm(t, db)}).BindSiteModel(context.Background(), siteModelID, nil)
	assertCatalogErrorIs(t, "BindSiteModel", err, updateErr)
}
