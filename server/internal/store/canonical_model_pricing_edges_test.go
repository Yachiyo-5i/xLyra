package store

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestCanonicalModelSyncUpsertPreservesManualPricing(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	modelID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*dest = []CanonicalModel{{
			ID:            modelID,
			ModelKey:      "gpt-5.6-sol",
			DisplayName:   "GPT-5.6-Sol",
			PricingSource: CanonicalPricingSourceManual,
			InputPrice:    sql.NullFloat64{Float64: 5, Valid: true},
			OutputPrice:   sql.NullFloat64{Float64: 30, Valid: true},
			CacheReadRatio: sql.NullFloat64{
				Float64: 0.1,
				Valid:   true,
			},
		}}
		tx.Statement.RowsAffected = 1
	})
	var saved *CanonicalModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*CanonicalModel); ok {
			saved = item
		}
		tx.Statement.RowsAffected = 1
	})

	updated, err := NewCanonicalModelRepository(db).SyncUpsert(context.Background(), UpsertCanonicalModelParams{
		ModelKey:      "gpt-5.6-sol",
		DisplayName:   "GPT-5.6 Sol (models.dev)",
		Provider:      "openai",
		Category:      "chat",
		Status:        "active",
		PricingSource: "models_dev",
		InputPrice:    sql.NullFloat64{Float64: 99, Valid: true},
		OutputPrice:   sql.NullFloat64{Float64: 999, Valid: true},
	})
	if err != nil {
		t.Fatalf("SyncUpsert returned error: %v", err)
	}
	if saved == nil {
		t.Fatal("SyncUpsert did not save the row")
	}
	if updated.PricingSource != CanonicalPricingSourceManual {
		t.Fatalf("pricing source = %q, want manual preserved", updated.PricingSource)
	}
	if !updated.InputPrice.Valid || updated.InputPrice.Float64 != 5 || updated.OutputPrice.Float64 != 30 {
		t.Fatalf("manual prices were clobbered by sync: %#v", updated)
	}
	if !updated.CacheReadRatio.Valid || updated.CacheReadRatio.Float64 != 0.1 {
		t.Fatalf("manual cache ratio was clobbered by sync: %#v", updated)
	}
	if updated.DisplayName != "GPT-5.6 Sol (models.dev)" {
		t.Fatalf("non-pricing fields should still sync: %#v", updated)
	}
}

func TestCanonicalModelSyncUpsertStillSyncsAutomaticPricing(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	modelID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*dest = []CanonicalModel{{
			ID:            modelID,
			ModelKey:      "gpt-5.6-terra",
			PricingSource: "models_dev",
			InputPrice:    sql.NullFloat64{Float64: 1, Valid: true},
		}}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	})

	updated, err := NewCanonicalModelRepository(db).SyncUpsert(context.Background(), UpsertCanonicalModelParams{
		ModelKey:      "gpt-5.6-terra",
		DisplayName:   "GPT-5.6-Terra",
		PricingSource: "models_dev",
		InputPrice:    sql.NullFloat64{Float64: 2.5, Valid: true},
		OutputPrice:   sql.NullFloat64{Float64: 15, Valid: true},
	})
	if err != nil {
		t.Fatalf("SyncUpsert returned error: %v", err)
	}
	if !updated.InputPrice.Valid || updated.InputPrice.Float64 != 2.5 || updated.OutputPrice.Float64 != 15 {
		t.Fatalf("automatic pricing should keep syncing: %#v", updated)
	}
}

func TestCanonicalModelSyncPricingUpsertPreservesModelMetadata(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	modelID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*dest = []CanonicalModel{{
			ID:                     modelID,
			ModelKey:               "gpt-5.6-terra",
			DisplayName:            "GPT-5.6 Terra",
			Provider:               "openai",
			Category:               "chat",
			Capabilities:           JSON(`{"source":"models_dev"}`),
			Status:                 "active",
			SupportedEndpointTypes: JSON(`["openai","openai-response"]`),
			Modalities:             JSON(`{"input":["text","image"],"output":["text"]}`),
			ContextWindow:          sql.NullInt32{Int32: 1050000, Valid: true},
			MaxOutputTokens:        sql.NullInt32{Int32: 128000, Valid: true},
			PricingSource:          "models_dev",
		}}
		tx.Statement.RowsAffected = 1
	})
	var saved CanonicalModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	updated, err := NewCanonicalModelRepository(db).SyncPricingUpsert(context.Background(), SyncCanonicalModelPricingParams{
		ModelKey:      "gpt-5.6-terra",
		Provider:      "openai",
		Category:      "chat",
		Status:        "active",
		InputPrice:    sql.NullFloat64{Float64: 2.5, Valid: true},
		OutputPrice:   sql.NullFloat64{Float64: 15, Valid: true},
		PricingSource: "litellm_repo",
	})
	if err != nil {
		t.Fatalf("SyncPricingUpsert returned error: %v", err)
	}
	if saved.ID != modelID || updated.InputPrice.Float64 != 2.5 || updated.OutputPrice.Float64 != 15 {
		t.Fatalf("updated=%#v saved=%#v, want pricing update", updated, saved)
	}
	if saved.ContextWindow.Int32 != 1050000 || saved.MaxOutputTokens.Int32 != 128000 ||
		string(saved.SupportedEndpointTypes) != `["openai","openai-response"]` ||
		string(saved.Modalities) != `{"input":["text","image"],"output":["text"]}` {
		t.Fatalf("pricing sync changed model metadata: %#v", saved)
	}
}

func TestCanonicalModelSyncPricingUpsertKeepsPricedModelsDevRow(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	modelID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*dest = []CanonicalModel{{
			ID:            modelID,
			ModelKey:      "gpt-5.6-sol",
			Provider:      "openai",
			Category:      "chat",
			Status:        "active",
			PricingSource: "models_dev",
			InputPrice:    sql.NullFloat64{Float64: 4, Valid: true},
			OutputPrice:   sql.NullFloat64{Float64: 20, Valid: true},
		}}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		t.Error("priced models_dev row must not be overwritten by the litellm overlay")
	})

	updated, err := NewCanonicalModelRepository(db).SyncPricingUpsert(context.Background(), SyncCanonicalModelPricingParams{
		ModelKey:      "gpt-5.6-sol",
		Provider:      "openai",
		Category:      "chat",
		Status:        "active",
		InputPrice:    sql.NullFloat64{Float64: 5, Valid: true},
		OutputPrice:   sql.NullFloat64{Float64: 30, Valid: true},
		PricingSource: "litellm_repo",
	})
	if err != nil {
		t.Fatalf("SyncPricingUpsert returned error: %v", err)
	}
	if updated.ID != modelID || updated.InputPrice.Float64 != 4 || updated.OutputPrice.Float64 != 20 {
		t.Fatalf("updated = %#v, want priced models_dev row kept as-is", updated)
	}
	if updated.PricingSource != "models_dev" {
		t.Fatalf("updated pricing source = %q, want models_dev kept", updated.PricingSource)
	}
}

func TestCanonicalModelSyncPricingUpsertRefreshesLitellmOwnedRow(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	modelID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*[]CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*dest = []CanonicalModel{{
			ID:            modelID,
			ModelKey:      "gpt-5.6-cyber",
			Provider:      "openai",
			Category:      "chat",
			Status:        "active",
			PricingSource: "litellm_repo",
			InputPrice:    sql.NullFloat64{Float64: 1, Valid: true},
			OutputPrice:   sql.NullFloat64{Float64: 8, Valid: true},
		}}
		tx.Statement.RowsAffected = 1
	})
	var saved CanonicalModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		saved = *item
		tx.RowsAffected = 1
	})

	updated, err := NewCanonicalModelRepository(db).SyncPricingUpsert(context.Background(), SyncCanonicalModelPricingParams{
		ModelKey:      "gpt-5.6-cyber",
		Provider:      "openai",
		Category:      "chat",
		Status:        "active",
		InputPrice:    sql.NullFloat64{Float64: 1.5, Valid: true},
		OutputPrice:   sql.NullFloat64{Float64: 10, Valid: true},
		PricingSource: "litellm_repo",
	})
	if err != nil {
		t.Fatalf("SyncPricingUpsert returned error: %v", err)
	}
	if saved.ID != modelID || updated.InputPrice.Float64 != 1.5 || updated.OutputPrice.Float64 != 10 {
		t.Fatalf("updated=%#v saved=%#v, want litellm-owned row refreshed", updated, saved)
	}
	if updated.PricingSource != "litellm_repo" {
		t.Fatalf("updated pricing source = %q, want litellm_repo", updated.PricingSource)
	}
}

func TestCanonicalModelUpdatePricingSetsManualAndReset(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	modelID := uuid.New()
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*dest = CanonicalModel{
			ID:            modelID,
			ModelKey:      "gpt-5.6-luna",
			PricingSource: "none",
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	})

	repo := NewCanonicalModelRepository(db)
	updated, err := repo.UpdatePricing(context.Background(), UpdateCanonicalModelPricingParams{
		ID:          modelID,
		Manual:      true,
		InputPrice:  sql.NullFloat64{Float64: 1, Valid: true},
		OutputPrice: sql.NullFloat64{Float64: 6, Valid: true},
	})
	if err != nil {
		t.Fatalf("UpdatePricing returned error: %v", err)
	}
	if updated.PricingSource != CanonicalPricingSourceManual || updated.InputPrice.Float64 != 1 || updated.OutputPrice.Float64 != 6 {
		t.Fatalf("manual pricing not applied: %#v", updated)
	}
	if !updated.LastPricingSyncedAt.Valid {
		t.Fatalf("manual pricing should stamp last synced at: %#v", updated)
	}

	reset, err := repo.UpdatePricing(context.Background(), UpdateCanonicalModelPricingParams{
		ID:     modelID,
		Manual: false,
	})
	if err != nil {
		t.Fatalf("UpdatePricing reset returned error: %v", err)
	}
	if reset.PricingSource != "none" {
		t.Fatalf("reset pricing source = %q, want none", reset.PricingSource)
	}
}
