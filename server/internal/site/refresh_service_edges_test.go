package site

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestCreateManualSiteModelValidationStopsBeforeTransaction(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	siteID := uuid.New()
	canonicalID := uuid.New()

	tests := []struct {
		name   string
		params CreateManualSiteModelParams
		want   string
	}{
		{
			name: "missing site",
			params: CreateManualSiteModelParams{
				UpstreamModelName:      "gpt-manual",
				CanonicalModelID:       canonicalID,
				SupportedEndpointTypes: []string{"openai"},
			},
			want: "site id is required",
		},
		{
			name: "missing upstream",
			params: CreateManualSiteModelParams{
				SiteID:                 siteID,
				CanonicalModelID:       canonicalID,
				SupportedEndpointTypes: []string{"openai"},
			},
			want: "upstream_model_name is required",
		},
		{
			name: "missing canonical",
			params: CreateManualSiteModelParams{
				SiteID:                 siteID,
				UpstreamModelName:      "gpt-manual",
				SupportedEndpointTypes: []string{"openai"},
			},
			want: "canonical_model_id is required",
		},
		{
			name: "unsupported endpoint",
			params: CreateManualSiteModelParams{
				SiteID:                 siteID,
				UpstreamModelName:      "gpt-manual",
				CanonicalModelID:       canonicalID,
				SupportedEndpointTypes: []string{"openai", "manual-site"},
			},
			want: `unsupported endpoint type "manual-site"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, err := service.CreateManualSiteModel(t.Context(), tt.params)
			if model.ID != uuid.Nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CreateManualSiteModel() = %#v, %v, want %q", model, err, tt.want)
			}
		})
	}
}

func TestDeleteManualSiteModelValidationStopsBeforeTransaction(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	if err := service.DeleteManualSiteModel(t.Context(), uuid.Nil, uuid.New()); err == nil || !strings.Contains(err.Error(), "site id is required") {
		t.Fatalf("DeleteManualSiteModel(nil site) error = %v, want site id validation", err)
	}
	if err := service.DeleteManualSiteModel(t.Context(), uuid.New(), uuid.Nil); err == nil || !strings.Contains(err.Error(), "model id is required") {
		t.Fatalf("DeleteManualSiteModel(nil model) error = %v, want model id validation", err)
	}
}

func TestModelPriceValidationStopsBeforeTransaction(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	if result, err := service.BulkUpsertModelPrices(t.Context(), BulkModelPriceInput{}); result.Count != 0 || !errors.Is(err, ErrModelPriceInvalid) || !strings.Contains(err.Error(), "canonical_model_id is required") {
		t.Fatalf("BulkUpsertModelPrices(missing canonical) = %#v, %v, want canonical validation", result, err)
	}
	if result, err := service.BulkUpsertModelPrices(t.Context(), BulkModelPriceInput{CanonicalModelID: uuid.New()}); result.Count != 0 || !errors.Is(err, ErrModelPriceInvalid) || !strings.Contains(err.Error(), "site_model_ids is required") {
		t.Fatalf("BulkUpsertModelPrices(missing ids) = %#v, %v, want ids validation", result, err)
	}

}

func TestUpsertModelPriceTxEarlyReadonlyAndCanonicalMismatch(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	canonicalID := uuid.New()
	service := siteServiceWithoutStore()
	db := siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.SiteModel:
			*dest = store.SiteModel{
				ID:           modelID,
				SiteID:       siteID,
				UpstreamName: "gpt-manual",
				CanonicalID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
			}
			tx.RowsAffected = 1
		case *store.Site:
			*dest = store.Site{ID: siteID, SiteType: "newapi"}
			tx.RowsAffected = 1
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}})

	value := 1.0
	item, created, err := service.upsertModelPriceTx(t.Context(), db, modelID, ModelPriceInput{InputValue: &value}, uuid.Nil)
	if item.Model.ID != uuid.Nil || created || !errors.Is(err, ErrModelPriceReadonly) {
		t.Fatalf("upsertModelPriceTx(newapi) = %#v created=%v err=%v, want readonly", item, created, err)
	}

	siteReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.SiteModel:
			*dest = store.SiteModel{
				ID:           modelID,
				SiteID:       siteID,
				UpstreamName: "gpt-manual",
				CanonicalID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
			}
			tx.RowsAffected = 1
		case *store.Site:
			*dest = store.Site{ID: siteID, SiteType: "openai"}
			tx.RowsAffected = 1
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	})

	_, _, err = service.upsertModelPriceTx(t.Context(), db, modelID, ModelPriceInput{InputValue: &value}, uuid.New())
	if !errors.Is(err, ErrModelPriceCanonicalMismatch) {
		t.Fatalf("upsertModelPriceTx(canonical mismatch) error = %v, want canonical mismatch", err)
	}
}

func TestRunNewAPICheckinsUnsupportedRegistry(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Site:
			*dest = []store.Site{
				{ID: uuid.New(), Name: "disabled", SiteType: "newapi", Enabled: false},
				{ID: uuid.New(), Name: "openai", SiteType: "openai", Enabled: true},
				{ID: siteID, Name: "manual-site", SiteType: "newapi", Enabled: true},
			}
			tx.RowsAffected = 3
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	})
	service.adapters = adapter.Registry{}

	items, err := service.RunNewAPICheckins(t.Context())
	if err != nil {
		t.Fatalf("RunNewAPICheckins() error = %v", err)
	}
	if len(items) != 1 || items[0].SiteID != siteID || items[0].Status != "failed" || items[0].Err == nil {
		t.Fatalf("RunNewAPICheckins() = %#v, want one failed enabled newapi item", items)
	}
}

func TestSetAPIKeySecretValidatesEmptySecretAfterCredentialLookup(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	service := siteServiceWithCredential(t, store.SiteCredential{ID: credentialID, SiteID: siteID, CredentialType: defaultCredentialType})

	if _, err := service.SetAPIKeySecret(t.Context(), siteID, credentialID, " "); err == nil || !strings.Contains(err.Error(), "api key must not be empty") {
		t.Fatalf("SetAPIKeySecret(empty secret) error = %v, want validation", err)
	}
}

func TestSyncModelStateCreatesOnlyEnabledAggregateModels(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	enabledCredentialID := uuid.New()
	disabledCredentialID := uuid.New()
	createCount := 0
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch tx.Statement.Dest.(type) {
			case *store.SiteAPIKeyModel, *store.SiteModel:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.SiteAPIKeyModel, *[]store.SiteModel:
				tx.RowsAffected = 0
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			createCount++
			tx.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
	})

	siteModels, apiKeyModels, err := syncModelState(
		t.Context(),
		siteID,
		[]refreshKey{
			{
				credential:   store.SiteCredential{ID: enabledCredentialID},
				state:        store.SiteAPIKeyState{Enabled: true},
				name:         "enabled-key",
				modelsSynced: true,
				models: []refreshKeyModel{
					{model: adapter.Model{UpstreamName: "gpt-manual", DisplayName: "GPT Manual", Capabilities: map[string]any{"raw": map[string]any{"source": "test"}}}},
					{model: adapter.Model{UpstreamName: " "}},
				},
			},
			{
				credential:   store.SiteCredential{ID: disabledCredentialID},
				state:        store.SiteAPIKeyState{Enabled: false},
				name:         "disabled-key",
				modelsSynced: true,
				models: []refreshKeyModel{
					{model: adapter.Model{UpstreamName: "disabled-only", DisplayName: "Disabled Only"}},
				},
			},
		},
		store.NewSiteModelRepository(db),
		store.NewSiteAPIKeyModelRepository(db),
	)
	if err != nil {
		t.Fatalf("syncModelState() error = %v", err)
	}
	if len(siteModels) != 1 || siteModels[0].UpstreamName != "gpt-manual" {
		t.Fatalf("syncModelState() siteModels = %#v, want only enabled aggregate model", siteModels)
	}
	if len(apiKeyModels) != 3 {
		t.Fatalf("syncModelState() apiKeyModels len = %d, want first-pass plus linked enabled model", len(apiKeyModels))
	}
	if createCount == 0 {
		t.Fatal("syncModelState() did not exercise create callbacks")
	}
}

func TestSyncModelStatePreservesModelsWhenKeySummaryFailed(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	siteModelID := uuid.New()
	existingSiteModel := store.SiteModel{
		ID:           siteModelID,
		SiteID:       siteID,
		UpstreamName: "gpt-existing",
		DisplayName:  "GPT Existing",
		Capabilities: store.JSON(`{"source":"existing"}`),
		Status:       "active",
	}
	existingBinding := store.SiteAPIKeyModel{
		ID:                uuid.New(),
		SiteID:            siteID,
		SiteCredentialID:  credentialID,
		SiteModelID:       uuid.NullUUID{UUID: siteModelID, Valid: true},
		UpstreamModelName: "gpt-existing",
		DisplayName:       "GPT Existing",
		Available:         true,
		Enabled:           true,
	}
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteModel:
				*dest = []store.SiteModel{existingSiteModel}
				tx.RowsAffected = 1
			case *[]store.SiteAPIKeyModel:
				*dest = []store.SiteAPIKeyModel{existingBinding}
				tx.RowsAffected = 1
			case *store.SiteModel:
				*dest = existingSiteModel
				tx.RowsAffected = 1
			case *store.SiteAPIKeyModel:
				*dest = existingBinding
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
	})

	siteModels, apiKeyModels, err := syncModelState(
		t.Context(),
		siteID,
		[]refreshKey{{
			credential: store.SiteCredential{ID: credentialID},
			state:      store.SiteAPIKeyState{SiteCredentialID: credentialID, Enabled: true, SyncStatus: "failed"},
			name:       "existing-key",
		}},
		store.NewSiteModelRepository(db),
		store.NewSiteAPIKeyModelRepository(db),
	)
	if err != nil {
		t.Fatalf("syncModelState() error = %v", err)
	}
	if len(siteModels) != 1 || siteModels[0].UpstreamName != "gpt-existing" {
		t.Fatalf("site models = %#v, want preserved existing model", siteModels)
	}
	if len(apiKeyModels) != 1 || !apiKeyModels[0].Available {
		t.Fatalf("api key models = %#v, want preserved available binding", apiKeyModels)
	}
}

func TestOAuthConnectionEnableBlockedReasonBlocksExpiredStatus(t *testing.T) {
	t.Parallel()

	if got := oauthConnectionEnableBlockedReason(store.OAuthConnection{Status: "expired"}); got != "oauth connection status is expired" {
		t.Fatalf("oauthConnectionEnableBlockedReason() = %q, want expired reason", got)
	}
}
