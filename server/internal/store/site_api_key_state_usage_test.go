package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSiteAPIKeyStateRepositoryUpdateUsageUsesBoundedORMUpdate(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	credentialID := uuid.New()
	updateCalls := 0
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateCalls++
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			t.Fatalf("update destination = %T, want map", tx.Statement.Dest)
		}
		usage, ok := updates["usage"].(JSON)
		if !ok || string(usage) != `{"source":"opencode_go"}` {
			t.Fatalf("usage update = %#v", updates["usage"])
		}
		tx.Statement.RowsAffected = 1
	})

	err := NewSiteAPIKeyStateRepository(db).UpdateUsage(
		context.Background(),
		credentialID,
		JSON(`{"source":"opencode_go"}`),
	)
	if err != nil {
		t.Fatalf("UpdateUsage returned error: %v", err)
	}
	if updateCalls != 1 {
		t.Fatalf("update calls = %d, want 1", updateCalls)
	}
}
