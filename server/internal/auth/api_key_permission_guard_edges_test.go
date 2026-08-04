package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

const apiKeyPermissionGuardMasterKey = "api-key-permission-guard-master-key"

func TestReplaceAdminAccessTokenWithMissingStoreStopsAtRepositoryBoundaryOffline(t *testing.T) {
	t.Parallel()

	service := NewService(nil, apiKeyPermissionGuardMasterKey)
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("ReplaceAdminAccessToken with nil store did not stop at repository boundary")
		}
	}()

	_, _ = service.ReplaceAdminAccessToken(context.Background(), uuid.New())
}

func TestDeleteAPIKeyReturnsNotFoundWhenDeleteAffectsNoRowsOffline(t *testing.T) {
	t.Parallel()

	service := newAPIKeyPermissionGuardService(t)
	if err := service.db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 0
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}

	err := service.DeleteAPIKey(context.Background(), uuid.New())
	if err == nil || err.Error() != "delete api key: not found" {
		t.Fatalf("DeleteAPIKey error = %v, want not found", err)
	}
}

func TestSetAPIKeySiteModelsRejectsUnknownModelBeforeWritesOffline(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New()
	service := newAPIKeyPermissionGuardService(t)
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.AddError(errors.New("missing site model should stop before transaction writes"))
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}

	apiKey, models, err := service.SetAPIKeySiteModels(context.Background(), uuid.New(), []uuid.UUID{uuid.Nil, siteModelID, siteModelID}, "allow_list")
	if apiKey.ID != uuid.Nil || models != nil {
		t.Fatalf("SetAPIKeySiteModels result = %#v/%#v, want zero values", apiKey, models)
	}
	assertAuthErrorContains(t, "SetAPIKeySiteModels", err, "site_model_id "+siteModelID.String()+" was not found")
}

func TestResolveAPIKeyAccessSetsWithAllowAllSkipsPermissionLookupsOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	service := newAPIKeyPermissionGuardService(t)
	queryCount := 0
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		queryCount++
		apiKey, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(errors.New("allow_all should only query api key"))
			return
		}
		*apiKey = store.APIKey{ID: apiKeyID, ModelPolicy: "allow_all", SitePolicy: "allow_all"}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}

	result, err := service.ResolveAPIKeyAccessSets(context.Background(), apiKeyID)
	if err != nil {
		t.Fatalf("ResolveAPIKeyAccessSets returned error: %v", err)
	}
	if result.APIKey.ID != apiKeyID || result.AllowedSiteIDs != nil || result.AllowedSiteModelIDs != nil {
		t.Fatalf("ResolveAPIKeyAccessSets result = %#v, want allow_all with nil access sets", result)
	}
	if queryCount != 1 {
		t.Fatalf("query count = %d, want only api key lookup", queryCount)
	}
}

func newAPIKeyPermissionGuardService(t *testing.T) *Service {
	t.Helper()

	return NewService(authPostgresGorm(t), apiKeyPermissionGuardMasterKey)
}
