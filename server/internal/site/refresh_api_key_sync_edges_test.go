package site

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestSyncPricingModelsStateIgnoresBlankModelNames(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	models, err := service.syncPricingModelsState(t.Context(), store.Site{ID: uuid.New(), SiteType: "newapi"}, adapter.PricingSnapshot{
		Items: []adapter.ModelPricing{
			{ModelName: " "},
			{ModelName: "\t"},
		},
	}, store.SiteModelRepository{})
	if err != nil {
		t.Fatalf("syncPricingModelsState() error = %v, want nil for empty filtered pricing", err)
	}
	if len(models) != 0 {
		t.Fatalf("syncPricingModelsState() models = %#v, want empty", models)
	}
}

func TestSyncNewAPIKeyWithExistingSiteModelsSkipsUnavailable(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	activeModelID := uuid.New()
	unavailableModelID := uuid.New()

	createdNames := make([]string, 0, 1)
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteModel:
				*dest = []store.SiteModel{
					{ID: activeModelID, SiteID: siteID, UpstreamName: "gpt-active", DisplayName: "Active", Status: "active"},
					{ID: unavailableModelID, SiteID: siteID, UpstreamName: "gpt-stale", DisplayName: "Stale", Status: "unavailable"},
				}
				tx.RowsAffected = 2
			case *store.SiteAPIKeyModel:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: siteCaptureCreate[store.SiteAPIKeyModel](t, "site api key model", func(model store.SiteAPIKeyModel) {
			createdNames = append(createdNames, model.UpstreamModelName)
		}),
	})

	err := syncNewAPIKeyWithExistingSiteModels(t.Context(), db, siteID, store.SiteCredential{ID: credentialID}, time.Now())
	if err != nil {
		t.Fatalf("syncNewAPIKeyWithExistingSiteModels() error = %v, want nil", err)
	}
	if len(createdNames) != 1 || createdNames[0] != "gpt-active" {
		t.Fatalf("created api key models = %#v, want only active model", createdNames)
	}
}

func TestAPIKeysSkipsDefaultWhenScopedKeysExist(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	defaultCredential := siteEncryptedCredential(t, uuid.New(), siteID, defaultCredentialType, "default-secret", siteJSONMeta(t, map[string]any{"name": "default"}))
	scopedCredential := siteEncryptedCredential(t, uuid.New(), siteID, scopedAPIKeyCredentialType(), "scoped-secret", siteJSONMeta(t, map[string]any{"name": "scoped"}))
	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		credentials, ok := tx.Statement.Dest.(*[]store.SiteCredential)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*credentials = []store.SiteCredential{defaultCredential, scopedCredential}
		tx.RowsAffected = 2
	})

	apiKeys, err := service.APIKeys(t.Context(), siteID)
	if err != nil {
		t.Fatalf("APIKeys() error = %v, want nil", err)
	}
	if len(apiKeys) != 1 || apiKeys[0].Credential.ID != scopedCredential.ID || apiKeys[0].Secret != "scoped-secret" {
		t.Fatalf("APIKeys() = %#v, want only scoped credential", apiKeys)
	}
}

func TestSyncModelsRequiresAPIKeyWhenListIsEmpty(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	service := siteServiceWithSiteAndCredentials(t, store.Site{ID: siteID, Name: "model-only", SiteType: "model-only-refresh", BaseURL: "https://model-only.test", Enabled: true}, nil)

	registry := adapter.NewRegistry()
	registry.Register(panicOnListModelsModule{siteTypes: []string{"model-only-refresh"}})
	service.adapters = registry

	if result, err := service.SyncModels(t.Context(), siteID); err == nil || result.SiteID != uuid.Nil || !strings.Contains(err.Error(), "site api key is required") {
		t.Fatalf("SyncModels() = %#v, %v; want api key required guard", result, err)
	}
}

func TestModelSyncCredentialsAcceptsGrokSSOAccounts(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credential := siteEncryptedCredential(t, uuid.New(), siteID, grokCredentialPrefix+uuid.NewString(), "grok-token", siteJSONMeta(t, map[string]any{"enabled": false}))
	service := siteServiceWithSiteAndCredentials(t, store.Site{}, []store.SiteCredential{credential})

	result, err := service.modelSyncCredentials(t.Context(), store.Site{ID: siteID, SiteType: "grok"})
	if err != nil {
		t.Fatalf("modelSyncCredentials() error = %v", err)
	}
	if len(result) != 1 || result[0].Credential.ID != credential.ID || result[0].Secret != "grok-token" || result[0].Enabled {
		t.Fatalf("modelSyncCredentials() = %#v, want disabled grok SSO account", result)
	}
}

func TestRefreshModelOnlyStateMarksSyncModelsFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	syncErr := errors.New("api keys missing")
	var savedState store.SiteState
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = store.Site{ID: siteID, Name: "model-refresh", SiteType: "model-refresh", BaseURL: "https://model-refresh.test", Enabled: true}
				tx.RowsAffected = 1
			case *[]store.SiteCredential:
				tx.AddError(syncErr)
			case *store.SiteState:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: siteCaptureCreate[store.SiteState](t, "site state", func(state store.SiteState) {
			savedState = state
		}),
	})

	registry := adapter.NewRegistry()
	registry.Register(panicOnListModelsModule{siteTypes: []string{"model-refresh"}})
	service.adapters = registry
	result, err := service.refreshModelOnlyState(t.Context(), store.Site{ID: siteID, Name: "model-refresh", SiteType: "model-refresh"})
	if err == nil || !strings.Contains(err.Error(), syncErr.Error()) {
		t.Fatalf("refreshModelOnlyState() error = %v, want sync failure", err)
	}
	if result.State.SyncStatus != "failed" || !strings.Contains(result.State.SyncMessage.String, syncErr.Error()) {
		t.Fatalf("refreshModelOnlyState() = %#v, want failed result with sync error", result)
	}
	if savedState.SiteID != siteID || savedState.SyncStatus != "failed" || !savedState.SyncMessage.Valid {
		t.Fatalf("saved state = %#v, want failed sync state", savedState)
	}
}

type panicOnListModelsModule struct {
	siteTypes []string
}

func (m panicOnListModelsModule) SiteTypes() []string {
	return m.siteTypes
}

func (m panicOnListModelsModule) ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	panic("ListModels should not be called when every API key is disabled")
}
