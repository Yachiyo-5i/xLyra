package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
