package site

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestMigrateDefaultAPIKeyCredentialSkipsAlreadyScopedCredentials(t *testing.T) {
	t.Parallel()

	err := migrateDefaultAPIKeyCredential(t.Context(), store.SiteCredentialRepository{}, []store.SiteCredential{
		{ID: uuid.New(), CredentialType: scopedAPIKeyCredentialType()},
		{ID: uuid.New(), CredentialType: "oauth"},
	})
	if err != nil {
		t.Fatalf("migrateDefaultAPIKeyCredential() error = %v, want nil for already scoped credentials", err)
	}
}

func TestMigrateDefaultAPIKeyCredentialScopesFirstDefaultCredential(t *testing.T) {
	t.Parallel()

	defaultID := uuid.New()
	laterID := uuid.New()

	var saved store.SiteCredential
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			credential, ok := tx.Statement.Dest.(*store.SiteCredential)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			*credential = store.SiteCredential{ID: defaultID, CredentialType: defaultCredentialType}
			tx.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			credential, ok := tx.Statement.Dest.(*store.SiteCredential)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			saved = *credential
			tx.RowsAffected = 1
		},
	})

	err := migrateDefaultAPIKeyCredential(t.Context(), store.NewSiteCredentialRepository(db), []store.SiteCredential{
		{ID: defaultID, CredentialType: defaultCredentialType},
		{ID: laterID, CredentialType: defaultCredentialType},
	})
	if err != nil {
		t.Fatalf("migrateDefaultAPIKeyCredential() error = %v, want nil", err)
	}
	if saved.ID != defaultID {
		t.Fatalf("updated credential id = %s, want first default credential %s", saved.ID, defaultID)
	}
	if saved.CredentialType == defaultCredentialType || !strings.HasPrefix(saved.CredentialType, defaultCredentialType+":") {
		t.Fatalf("updated credential type = %q, want scoped api key type", saved.CredentialType)
	}
}

func TestMigrateDefaultAPIKeyCredentialWrapsRepositoryLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("default credential lookup stopped")
	db := siteGormWithQueryError(t, queryErr)

	err := migrateDefaultAPIKeyCredential(t.Context(), store.NewSiteCredentialRepository(db), []store.SiteCredential{
		{ID: uuid.New(), CredentialType: defaultCredentialType},
	})
	assertSiteWrappedQueryError(t, "migrateDefaultAPIKeyCredential", err, queryErr, "get site credential by id")
}

func TestSyncSiteModelStatusFromAPIKeyModelsDisablesModelWhenAnyLinkedAPIKeyModelIsDisabled(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()

	var savedStatus string
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteAPIKeyModel:
				*dest = []store.SiteAPIKeyModel{
					{
						SiteID:            siteID,
						SiteModelID:       uuid.NullUUID{UUID: modelID, Valid: true},
						UpstreamModelName: "gpt-disabled-sync",
						Available:         false,
						Enabled:           true,
					},
					{
						SiteID:            siteID,
						SiteModelID:       uuid.NullUUID{UUID: modelID, Valid: true},
						UpstreamModelName: "gpt-disabled-sync",
						Available:         true,
						Enabled:           false,
					},
				}
				tx.RowsAffected = 2
			case *[]store.SiteModel:
				*dest = []store.SiteModel{
					{ID: uuid.New(), SiteID: siteID, UpstreamName: "other-disabled-sync", Status: "active"},
					{ID: modelID, SiteID: siteID, UpstreamName: "renamed-disabled-sync", Status: "active"},
				}
				tx.RowsAffected = 2
			case *store.SiteModel:
				*dest = store.SiteModel{ID: modelID, SiteID: siteID, UpstreamName: "renamed-disabled-sync", Status: "active"}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			model, ok := tx.Statement.Dest.(*store.SiteModel)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			savedStatus = model.Status
			tx.RowsAffected = 1
		},
	})
	err := service.syncSiteModelStatusFromAPIKeyModels(t.Context(), siteID, "gpt-disabled-sync", store.SiteAPIKeyModel{
		SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
	})
	if err != nil {
		t.Fatalf("syncSiteModelStatusFromAPIKeyModels() error = %v, want nil", err)
	}
	if savedStatus != "disabled" {
		t.Fatalf("saved site model status = %q, want disabled", savedStatus)
	}
}

func TestSyncSiteModelStatusFromAPIKeyModelsReturnsEarlyWithoutAvailableMatch(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelsQueried := false
	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteAPIKeyModel:
			*dest = []store.SiteAPIKeyModel{
				{SiteID: siteID, UpstreamModelName: "gpt-no-available-match", Available: false, Enabled: true},
				{SiteID: siteID, UpstreamModelName: "other-no-available-match", Available: true, Enabled: true},
			}
			tx.RowsAffected = 2
		case *[]store.SiteModel:
			siteModelsQueried = true
			tx.AddError(errors.New("site model list should not be reached"))
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	})

	err := service.syncSiteModelStatusFromAPIKeyModels(t.Context(), siteID, "gpt-no-available-match", store.SiteAPIKeyModel{})
	if err != nil {
		t.Fatalf("syncSiteModelStatusFromAPIKeyModels() error = %v, want nil for no available match", err)
	}
	if siteModelsQueried {
		t.Fatalf("syncSiteModelStatusFromAPIKeyModels() queried site models, want early return")
	}
}

func TestSetAPIKeyModelEnabledPersistsCredentialMetaWhenAPIKeyModelSyncFails(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	credential := siteEncryptedCredential(t, credentialID, siteID, defaultCredentialType, "model-meta-fallback-secret", siteJSONMeta(t, map[string]any{
		"disabled_models": []string{"old-model-meta-fallback"},
		"enabled":         true,
	}))

	var savedMeta store.JSON
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteCredential:
				*dest = credential
				tx.RowsAffected = 1
			case *store.SiteAPIKeyModel:
				tx.AddError(errors.New("api key model update stopped during meta fallback"))
			case *[]store.SiteAPIKeyModel:
				tx.AddError(errors.New("api key model list stopped during meta fallback"))
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: siteCaptureUpdate[store.SiteCredential](t, "site credential", func(credential store.SiteCredential) {
			savedMeta = append(store.JSON(nil), credential.Meta...)
		}),
	})
	got, err := service.SetAPIKeyModelEnabled(t.Context(), siteID, credentialID, " gpt-model-meta-fallback ", false)
	if err != nil {
		t.Fatalf("SetAPIKeyModelEnabled() error = %v, want nil", err)
	}
	if got.Secret != "model-meta-fallback-secret" || !got.Enabled {
		t.Fatalf("SetAPIKeyModelEnabled() = %#v, want decrypted enabled credential", got)
	}

	meta := siteMustJSONMap(t, savedMeta)
	disabled := stringSetFromAny(meta["disabled_models"])
	if _, ok := disabled["old-model-meta-fallback"]; !ok {
		t.Fatalf("saved credential meta = %s, want existing disabled model preserved", string(savedMeta))
	}
	if _, ok := disabled["gpt-model-meta-fallback"]; !ok {
		t.Fatalf("saved credential meta = %s, want requested disabled model recorded", string(savedMeta))
	}
}
