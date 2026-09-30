package catalog

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestSyncCatalogModelDoesNotDeriveCacheRatiosFromCost(t *testing.T) {
	t.Parallel()

	db := catalogPostgresGorm(t)
	var saved store.CanonicalModel
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.CanonicalModel:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected catalog query destination"))
		}
	})
	replaceCatalogCreateCallback(t, db, func(tx *gorm.DB) {
		model, ok := tx.Statement.Dest.(*store.CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected catalog create destination"))
			return
		}
		saved = *model
		tx.Statement.RowsAffected = 1
	})

	inputPrice := 3.0
	err := (&SyncService{}).syncCatalogModel(context.Background(), store.NewCanonicalModelRepository(db), "anthropic", "claude-test", catalogModel{
		ModelKey:   "claude-test",
		InputPrice: &inputPrice,
		Cost: map[string]any{
			"cache_read":     0.3,
			"cache_write":    3.75,
			"cache_write_1h": 6.0,
		},
	})
	if err != nil {
		t.Fatalf("syncCatalogModel returned error: %v", err)
	}
	if saved.CacheReadRatio.Valid || saved.CacheWriteRatio.Valid || saved.CacheWrite1hRatio.Valid {
		t.Fatalf("cache ratios = %#v, want all absent when upstream ratios are absent", saved)
	}
}

func TestSyncCatalogModelPersistsAllUpstreamPricingFields(t *testing.T) {
	t.Parallel()

	db := catalogPostgresGorm(t)
	var saved store.CanonicalModel
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]store.CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected catalog query destination"))
			return
		}
		*dest = nil
		tx.Statement.RowsAffected = 0
	})
	replaceCatalogCreateCallback(t, db, func(tx *gorm.DB) {
		model, ok := tx.Statement.Dest.(*store.CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected catalog create destination"))
			return
		}
		saved = *model
		tx.Statement.RowsAffected = 1
	})

	inputPrice := 3.25
	outputPrice := 16.5
	cacheReadRatio := 0.125
	cacheWriteRatio := 1.375
	cacheWrite1hRatio := 2.25
	err := (&SyncService{}).syncCatalogModel(context.Background(), store.NewCanonicalModelRepository(db), "anthropic", "claude-test", catalogModel{
		ModelKey:          "claude-test",
		InputPrice:        &inputPrice,
		OutputPrice:       &outputPrice,
		CacheReadRatio:    &cacheReadRatio,
		CacheWriteRatio:   &cacheWriteRatio,
		CacheWrite1hRatio: &cacheWrite1hRatio,
		Cost: map[string]any{
			"input":          99.0,
			"output":         100.0,
			"cache_read":     98.0,
			"cache_write":    97.0,
			"cache_write_1h": 96.0,
		},
	})
	if err != nil {
		t.Fatalf("syncCatalogModel returned error: %v", err)
	}
	if saved.InputPrice.Float64 != inputPrice || !saved.InputPrice.Valid || saved.OutputPrice.Float64 != outputPrice || !saved.OutputPrice.Valid ||
		saved.CacheReadRatio.Float64 != cacheReadRatio || !saved.CacheReadRatio.Valid ||
		saved.CacheWriteRatio.Float64 != cacheWriteRatio || !saved.CacheWriteRatio.Valid ||
		saved.CacheWrite1hRatio.Float64 != cacheWrite1hRatio || !saved.CacheWrite1hRatio.Valid {
		t.Fatalf("saved pricing = %#v, want exact upstream fields", saved)
	}
}
