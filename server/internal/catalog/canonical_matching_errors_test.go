package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestMatchOrCreateCanonicalRejectsUnusableNamesBeforeRepositoryLookup(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	model := store.SiteModel{
		ID:           uuid.New(),
		UpstreamName: " !!! ",
		DisplayName:  " ... ",
	}

	_, _, err := service.matchOrCreateCanonical(context.Background(), store.NewCanonicalModelRepository(nil), model)
	assertCatalogErrorContains(t, "matchOrCreateCanonical", err, "has no usable model name")
}

func TestMatchOrCreateCanonicalReturnsKeyLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("canonical key lookup stopped")
	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	_, _, err := (&Service{}).matchOrCreateCanonical(context.Background(), store.NewCanonicalModelRepository(db), store.SiteModel{
		ID:           uuid.New(),
		UpstreamName: "openai/gpt-4o",
	})
	assertCatalogErrorIs(t, "matchOrCreateCanonical", err, queryErr)
}

func TestMatchOrCreateCanonicalReturnsArchivedRestoreError(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	updateErr := errors.New("canonical restore stopped")
	archivedCanonical := canonicalModelForRestore(canonicalID)
	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{archivedCanonical}
		case *store.CanonicalModel:
			*dest = archivedCanonical
		default:
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
	replaceCatalogUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(updateErr)
	})

	_, _, err := (&Service{}).matchOrCreateCanonical(context.Background(), store.NewCanonicalModelRepository(db), store.SiteModel{
		ID:           uuid.New(),
		UpstreamName: "openai/gpt-4o",
	})
	assertCatalogErrorIs(t, "matchOrCreateCanonical", err, updateErr)
}

func canonicalModelForRestore(id uuid.UUID) store.CanonicalModel {
	return store.CanonicalModel{
		ID:          id,
		ModelKey:    "gpt-4o",
		DisplayName: "gpt-4o",
		Provider:    "openai",
		Category:    "chat",
		Status:      "archived",
	}
}
