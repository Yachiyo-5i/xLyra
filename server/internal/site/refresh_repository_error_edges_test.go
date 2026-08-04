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

type apiKeyInventoryOnlyModule struct{}

func (apiKeyInventoryOnlyModule) SiteTypes() []string {
	return []string{"newapi"}
}

func (apiKeyInventoryOnlyModule) ListAPIKeys(context.Context, adapter.SiteConfig, adapter.SystemAuth) ([]adapter.APIKey, error) {
	return nil, nil
}

func TestRefreshCapabilityStateRequiresSummaryFetcher(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	site := store.Site{ID: siteID, Name: "refresh-error", SiteType: "newapi", BaseURL: "https://refresh-error.test", Enabled: true}
	credential := siteEncryptedCredential(t, credentialID, siteID, newAPIAccessTokenCredential, "system-token", nil)
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = site
				tx.RowsAffected = 1
			case *store.SiteCredential:
				*dest = credential
				tx.RowsAffected = 1
			case *store.SiteState:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*store.Site); !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
		},
		create: func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*store.SiteState); !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
		},
	})
	result, err := service.refreshCapabilityState(t.Context(), site, apiKeyInventoryOnlyModule{})
	if err == nil || !strings.Contains(err.Error(), "does not support api key summary sync") {
		t.Fatalf("refreshCapabilityState() error = %v, want missing summary fetcher failure", err)
	}
	if result.Site.Enabled || result.State.SyncStatus != "failed" || result.State.ValidationOK.Bool {
		t.Fatalf("refreshCapabilityState() = %#v, want auto-disabled failed state", result)
	}
}

func TestSyncUserModelsStateRejectsUnmarshalableRawPayload(t *testing.T) {
	t.Parallel()

	service := &Service{}
	models, err := service.syncUserModelsState(t.Context(), store.Site{}, map[string]any{
		"models": []any{
			map[string]any{
				"id":  "gpt-refresh-error",
				"raw": func() {},
			},
		},
	}, store.SiteModelRepository{})
	if models != nil || err == nil || !strings.Contains(err.Error(), "marshal user model capabilities") {
		t.Fatalf("syncUserModelsState() = %#v, %v, want capabilities marshal error", models, err)
	}
}

func TestSyncSiteModelsFromAdapterModelsRepositoryErrors(t *testing.T) {
	t.Parallel()

	t.Run("upsert query error", func(t *testing.T) {
		t.Parallel()

		queryErr := errors.New("refresh error model lookup stopped")
		db := siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
			switch tx.Statement.Dest.(type) {
			case *store.SiteModel:
				tx.AddError(queryErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		}})

		models, err := (&Service{}).syncSiteModelsFromAdapterModels(
			t.Context(),
			store.Site{ID: uuid.New()},
			[]adapter.Model{{UpstreamName: "gpt-refresh-error", DisplayName: "GPT Refresh Error"}},
			store.NewSiteModelRepository(db),
			true,
		)
		if models != nil {
			t.Fatalf("syncSiteModelsFromAdapterModels() models = %#v, want nil", models)
		}
		assertSiteWrappedQueryError(t, "syncSiteModelsFromAdapterModels", err, queryErr, "upsert site model")
	})

	t.Run("mark unavailable list error", func(t *testing.T) {
		t.Parallel()

		listErr := errors.New("refresh error stale model list stopped")
		db := siteGormWithCallbacks(t, siteGormCallbacks{
			query: func(tx *gorm.DB) {
				switch tx.Statement.Dest.(type) {
				case *store.SiteModel:
					tx.AddError(gorm.ErrRecordNotFound)
				case *[]store.SiteModel:
					tx.AddError(listErr)
				default:
					tx.AddError(gorm.ErrInvalidData)
				}
			},
			create: func(tx *gorm.DB) {
				if model, ok := tx.Statement.Dest.(*store.SiteModel); ok {
					model.ID = uuid.New()
					tx.RowsAffected = 1
					return
				}
				tx.AddError(gorm.ErrInvalidData)
			},
		})

		models, err := (&Service{}).syncSiteModelsFromAdapterModels(
			t.Context(),
			store.Site{ID: uuid.New()},
			[]adapter.Model{{UpstreamName: "gpt-refresh-error", DisplayName: "GPT Refresh Error"}},
			store.NewSiteModelRepository(db),
			true,
		)
		if models != nil || !errors.Is(err, listErr) || !strings.Contains(err.Error(), "list site models") {
			t.Fatalf("syncSiteModelsFromAdapterModels() = %#v, %v, want stale list error", models, err)
		}
	})
}

func TestSyncPricingStateRepositoryErrors(t *testing.T) {
	t.Parallel()

	t.Run("group mark unavailable error", func(t *testing.T) {
		t.Parallel()

		groupErr := errors.New("refresh error pricing group list stopped")
		db := siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
			switch tx.Statement.Dest.(type) {
			case *[]store.SitePricingGroup:
				tx.AddError(groupErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		}})

		groups, pricings, err := syncPricingState(
			t.Context(),
			uuid.New(),
			"openai",
			adapter.PricingSnapshot{},
			nil,
			store.NewSitePricingGroupRepository(db),
			store.NewSiteModelPricingRepository(db),
			time.Now(),
		)
		if groups != nil || pricings != nil || !errors.Is(err, groupErr) || !strings.Contains(err.Error(), "list site pricing groups") {
			t.Fatalf("syncPricingState() = %#v %#v %v, want group stale list error", groups, pricings, err)
		}
	})

	t.Run("model pricing upsert error", func(t *testing.T) {
		t.Parallel()

		pricingErr := errors.New("refresh error pricing lookup stopped")
		siteID := uuid.New()
		modelID := uuid.New()
		db := siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SitePricingGroup:
				*dest = nil
				tx.RowsAffected = 0
			case *store.SiteModelPricing:
				tx.AddError(pricingErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		}})

		groups, pricings, err := syncPricingState(
			t.Context(),
			siteID,
			"openai",
			adapter.PricingSnapshot{
				Items: []adapter.ModelPricing{{
					ModelName:   "gpt-refresh-error",
					GroupName:   "default",
					BillingType: "tokens",
					GroupRatio:  1,
				}},
			},
			map[string]store.SiteModel{
				"gpt-refresh-error": {ID: modelID, SiteID: siteID, UpstreamName: "gpt-refresh-error"},
			},
			store.NewSitePricingGroupRepository(db),
			store.NewSiteModelPricingRepository(db),
			time.Now(),
		)
		if groups != nil || pricings != nil || !errors.Is(err, pricingErr) || !strings.Contains(err.Error(), "upsert site model pricing") {
			t.Fatalf("syncPricingState() = %#v %#v %v, want model pricing upsert error", groups, pricings, err)
		}
	})
}

func TestSyncSiteModelAPIKeySwitchesPropagatesAPIKeyModelListError(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	apiKeyModelErr := errors.New("refresh error api key model list stopped")
	var savedCredential store.SiteCredential
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{
					{ID: credentialID, SiteID: siteID, CredentialType: defaultCredentialType, Meta: siteJSONMeta(t, map[string]any{"disabled_models": []string{"old-model"}})},
					{ID: uuid.New(), SiteID: siteID, CredentialType: newAPIAccessTokenCredential, Meta: siteJSONMeta(t, map[string]any{})},
				}
				tx.RowsAffected = 2
			case *store.SiteCredential:
				*dest = store.SiteCredential{ID: credentialID, SiteID: siteID, CredentialType: defaultCredentialType, Meta: siteJSONMeta(t, map[string]any{"disabled_models": []string{"old-model"}})}
				tx.RowsAffected = 1
			case *[]store.SiteAPIKeyModel:
				tx.AddError(apiKeyModelErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			if credential, ok := tx.Statement.Dest.(*store.SiteCredential); ok {
				savedCredential = *credential
				tx.RowsAffected = 1
				return
			}
			tx.AddError(gorm.ErrInvalidData)
		},
	})

	err := syncSiteModelAPIKeySwitches(t.Context(), db, siteID, store.SiteModel{
		ID:           uuid.New(),
		SiteID:       siteID,
		UpstreamName: "gpt-refresh-error",
	}, false)
	if !errors.Is(err, apiKeyModelErr) || !strings.Contains(err.Error(), "list site api key models") {
		t.Fatalf("syncSiteModelAPIKeySwitches() error = %v, want api key model list error", err)
	}
	meta := siteMustJSONMap(t, savedCredential.Meta)
	disabled := stringSetFromAny(meta["disabled_models"])
	if _, ok := disabled["gpt-refresh-error"]; !ok {
		t.Fatalf("saved credential meta = %s, want disabled model recorded before list error", string(savedCredential.Meta))
	}
}
