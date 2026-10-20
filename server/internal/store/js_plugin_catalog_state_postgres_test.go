package store

import (
	"context"
	"testing"
)

func TestPostgresJSPluginCatalogGeneration(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}

	repo := NewJSPluginRepository(db)
	got, err := repo.CatalogGeneration(ctx)
	if err != nil || got != 0 {
		t.Fatalf("CatalogGeneration() = %d, %v, want 0", got, err)
	}
	first, err := repo.BumpCatalogGeneration(ctx)
	if err != nil || first != 1 {
		t.Fatalf("BumpCatalogGeneration() = %d, %v, want 1", first, err)
	}
	second, err := repo.BumpCatalogGeneration(ctx)
	if err != nil || second != 2 {
		t.Fatalf("BumpCatalogGeneration() = %d, %v, want 2", second, err)
	}
	got, err = repo.CatalogGeneration(ctx)
	if err != nil || got != 2 {
		t.Fatalf("CatalogGeneration() = %d, %v, want 2", got, err)
	}

	if err := db.Exec(`DELETE FROM js_plugin_catalog_state`).Error; err != nil {
		t.Fatalf("delete catalog row: %v", err)
	}
	got, err = repo.CatalogGeneration(ctx)
	if err != nil || got != 0 {
		t.Fatalf("CatalogGeneration(missing) = %d, %v, want 0", got, err)
	}
	restored, err := repo.BumpCatalogGeneration(ctx)
	if err != nil || restored != 1 {
		t.Fatalf("BumpCatalogGeneration(missing) = %d, %v, want 1", restored, err)
	}
	got, err = repo.CatalogGeneration(ctx)
	if err != nil || got != 1 {
		t.Fatalf("CatalogGeneration() = %d, %v, want 1", got, err)
	}
	next, err := repo.BumpCatalogGeneration(ctx)
	if err != nil || next != 2 {
		t.Fatalf("BumpCatalogGeneration() after restore = %d, %v, want 2", next, err)
	}
}
