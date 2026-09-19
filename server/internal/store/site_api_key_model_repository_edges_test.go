package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSiteAPIKeyModelBackfillPreservesCredentialDeclarations(t *testing.T) {
	db := storeTransactionGorm(t, "endpoint backfill")
	siteID := uuid.New()
	models := []SiteAPIKeyModel{
		{ID: uuid.New(), Raw: JSON(`{"supported_endpoint_types":["openai-response"]}`)},
		{ID: uuid.New(), Raw: JSON(`{"raw":{"supported_endpoint_types":["openai"]}}`)},
		{ID: uuid.New(), Raw: JSON(`{"supported_endpoint_types":[]}`)},
		{ID: uuid.New(), Raw: JSON(`{}`)},
	}
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		*tx.Statement.Dest.(*[]SiteAPIKeyModel) = models
		tx.RowsAffected = int64(len(models))
	})
	updated := map[uuid.UUID]JSON{}
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		condition := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
		id := condition.Exprs[0].(clause.Eq).Value.(uuid.UUID)
		values := tx.Statement.Dest.(map[string]any)
		if len(values) != 1 {
			t.Fatalf("unexpected updates: %v", values)
		}
		updated[id] = values["raw"].(JSON)
		tx.RowsAffected = 1
	})
	err := storeWithGorm(db).WithinTx(t.Context(), func(tx Tx) error {
		return NewSiteAPIKeyModelRepository(tx).BackfillEndpointTypesInTx(t.Context(), siteID, "gpt-5", []string{"openai", "openai-response"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 2 {
		t.Fatalf("updated %d credentials, want only the two missing declarations", len(updated))
	}
	for _, model := range models[:2] {
		if _, ok := updated[model.ID]; ok {
			t.Fatalf("overwrote existing declaration: %s", model.Raw)
		}
	}
	for _, model := range models[2:] {
		got := (SiteAPIKeyModel{Raw: updated[model.ID]}).Capabilities().SupportedEndpointTypes
		if !reflect.DeepEqual(got, []string{"openai", "openai-response"}) {
			t.Fatalf("backfilled protocols = %v", got)
		}
	}
}

// BindSiteModel must update only site_model_id, scoped to the site + upstream
// model name. Credential selection filters api-key models by site_model_id, so
// a single-key refresh that skips this binding leaves the key invisible to
// routing (regression guard for the wrong-credential model-test bug).
func TestSiteAPIKeyModelBindSiteModelUpdatesOnlySiteModelID(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	var updateStatement *gorm.Statement
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateStatement = tx.Statement
		tx.Statement.RowsAffected = 2
	})

	siteID := uuid.New()
	siteModelID := uuid.New()
	repo := NewSiteAPIKeyModelRepository(db)
	if err := repo.BindSiteModel(context.Background(), siteID, "gpt-5", siteModelID); err != nil {
		t.Fatalf("BindSiteModel returned error: %v", err)
	}

	if updateStatement == nil {
		t.Fatal("BindSiteModel did not run an update")
	}
	updates, ok := updateStatement.Dest.(map[string]any)
	if !ok {
		t.Fatalf("update dest = %#v, want map assignment", updateStatement.Dest)
	}
	if len(updates) != 1 || updates["site_model_id"] != siteModelID {
		t.Fatalf("update dest = %#v, want only site_model_id=%s", updates, siteModelID)
	}
	sql := updateStatement.SQL.String()
	if sql == "" {
		// SQL is built during the callback chain; fall back to clause inspection.
		if _, hasWhere := updateStatement.Clauses["WHERE"]; !hasWhere {
			t.Fatal("BindSiteModel update is missing a WHERE clause (would touch all rows)")
		}
	}
}
