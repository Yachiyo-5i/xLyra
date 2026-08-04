package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestServiceRepositoryQueryErrorsPropagate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queryErr := errors.New("catalog query stopped")
	service := catalogServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	_, err := service.List(ctx)
	assertCatalogErrorIs(t, "List", err, queryErr)
	_, err = service.Matrix(ctx, uuid.New())
	assertCatalogErrorIs(t, "Matrix", err, queryErr)
	_, err = service.MatchSiteModels(ctx, uuid.New())
	assertCatalogErrorIs(t, "MatchSiteModels", err, queryErr)
	_, err = service.Archive(ctx, uuid.New())
	assertCatalogErrorIs(t, "Archive", err, queryErr)
}

func TestBindSiteModelPropagatesRepositoryErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	restoreErr := errors.New("catalog restore stopped")
	db := catalogTransactionPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(restoreErr)
	})
	restoreService := &Service{db: catalogStoreWithGorm(t, db)}
	canonicalID := uuid.New()
	_, err := restoreService.BindSiteModel(ctx, uuid.New(), &canonicalID)
	assertCatalogErrorIs(t, "BindSiteModel restore", err, restoreErr)
}

func TestServiceNilStoreGuardsPanicBeforeRepositoryUse(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	tests := []struct {
		name string
		run  func()
	}{
		{name: "list", run: func() { _, _ = service.List(context.Background()) }},
		{name: "matrix", run: func() { _, _ = service.Matrix(context.Background(), uuid.New()) }},
		{name: "match site models", run: func() { _, _ = service.MatchSiteModels(context.Background(), uuid.New()) }},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("expected nil store panic")
				}
				if !strings.Contains(strings.ToLower(strings.TrimSpace(recovered.(error).Error())), "nil pointer") {
					t.Fatalf("panic = %v, want nil pointer dereference", recovered)
				}
			}()

			tt.run()
		})
	}
	if _, err := service.BindSiteModel(context.Background(), uuid.New(), nil); err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("BindSiteModel error = %v, want store initialization error", err)
	}
}
