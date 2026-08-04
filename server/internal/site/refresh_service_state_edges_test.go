package site

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

type refreshStateAdapterModule struct {
	siteTypes        []string
	defaultBaseURL   string
	userSummary      adapter.UserSummary
	userSummaryErr   error
	checkin          adapter.CheckinResult
	checkinErr       error
	models           []adapter.Model
	listModelsErr    error
	detect           adapter.DetectResult
	detectErr        error
	pricing          adapter.PricingSnapshot
	pricingErr       error
	apiKeys          []adapter.APIKey
	listAPIKeysErr   error
	apiKeySummary    adapter.APIKeySummary
	apiKeySummaryErr error
}

func (m refreshStateAdapterModule) SiteTypes() []string {
	return m.siteTypes
}

func (m refreshStateAdapterModule) DefaultBaseURL() string {
	return m.defaultBaseURL
}

func (m refreshStateAdapterModule) FetchUserSummary(context.Context, adapter.SiteConfig, adapter.SystemAuth) (adapter.UserSummary, error) {
	return m.userSummary, m.userSummaryErr
}

func (m refreshStateAdapterModule) ExecuteCheckin(context.Context, adapter.SiteConfig, adapter.SystemAuth) (adapter.CheckinResult, error) {
	return m.checkin, m.checkinErr
}

func (m refreshStateAdapterModule) ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	return m.models, m.listModelsErr
}

func (m refreshStateAdapterModule) Detect(context.Context, string) (adapter.DetectResult, error) {
	return m.detect, m.detectErr
}

func (m refreshStateAdapterModule) FetchPricing(context.Context, adapter.SiteConfig, adapter.SystemAuth) (adapter.PricingSnapshot, error) {
	return m.pricing, m.pricingErr
}

func (m refreshStateAdapterModule) ListAPIKeys(context.Context, adapter.SiteConfig, adapter.SystemAuth) ([]adapter.APIKey, error) {
	return m.apiKeys, m.listAPIKeysErr
}

func (m refreshStateAdapterModule) SummarizeAPIKey(context.Context, adapter.SiteConfig, string) (adapter.APIKeySummary, error) {
	return m.apiKeySummary, m.apiKeySummaryErr
}

func TestRunNewAPICheckinsSummaryAndExecuteBranches(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	systemCredentialID := uuid.New()
	tests := []struct {
		name        string
		module      refreshStateAdapterModule
		wantStatus  string
		wantMessage string
	}{
		{
			name: "summary says not ready",
			module: refreshStateAdapterModule{
				siteTypes:   []string{"newapi"},
				userSummary: adapter.UserSummary{CheckinReady: false, Checkin: map[string]any{"message": "come back later"}},
			},
			wantStatus:  "skipped",
			wantMessage: "come back later",
		},
		{
			name: "checkin raw reports skipped",
			module: refreshStateAdapterModule{
				siteTypes:   []string{"newapi"},
				userSummary: adapter.UserSummary{CheckinReady: true},
				checkin:     adapter.CheckinResult{Raw: map[string]any{"success": false, "error": "already checked"}},
			},
			wantStatus:  "skipped",
			wantMessage: "already checked",
		},
		{
			name: "checkin execution error fails item",
			module: refreshStateAdapterModule{
				siteTypes:   []string{"newapi"},
				userSummary: adapter.UserSummary{CheckinReady: true},
				checkinErr:  errors.New("refresh state checkin failed"),
			},
			wantStatus:  "failed",
			wantMessage: "refresh state checkin failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := siteServiceWithCallbacks(t, siteGormCallbacks{
				query: func(tx *gorm.DB) {
					switch dest := tx.Statement.Dest.(type) {
					case *[]store.Site:
						*dest = []store.Site{{ID: siteID, Name: "refresh-state", SiteType: "newapi", BaseURL: "https://newapi.test", Enabled: true}}
						tx.RowsAffected = 1
					case *store.Site:
						*dest = store.Site{ID: siteID, Name: "refresh-state", SiteType: "newapi", BaseURL: "https://newapi.test", Enabled: true}
						tx.RowsAffected = 1
					case *store.SiteCredential:
						*dest = siteEncryptedCredential(t, systemCredentialID, siteID, newAPIAccessTokenCredential, "system-token", siteJSONMeta(t, map[string]any{"user_id": 7}))
						tx.RowsAffected = 1
					default:
						tx.AddError(gorm.ErrInvalidData)
					}
				},
			})
			registry := adapter.NewRegistry()
			registry.Register(tt.module)
			service.adapters = registry

			items, err := service.RunNewAPICheckins(t.Context())
			if err != nil {
				t.Fatalf("RunNewAPICheckins() error = %v", err)
			}
			if len(items) != 1 || items[0].Status != tt.wantStatus || !strings.Contains(items[0].Message, tt.wantMessage) {
				t.Fatalf("RunNewAPICheckins() = %#v, want %s containing %q", items, tt.wantStatus, tt.wantMessage)
			}
		})
	}
}

func TestSiteHealthSuccessAndUnknownState(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	snapshotID := uuid.New()
	var savedHealthState store.SiteHealthState
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = store.Site{ID: siteID, Name: "refresh-state", SiteType: "refresh-state-health", BaseURL: "https://health.test", Enabled: true}
				tx.RowsAffected = 1
			case *store.SiteHealthState:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.HealthSnapshot:
				*dest = []store.HealthSnapshot{{
					ID:        snapshotID,
					SiteID:    siteID,
					Scope:     "site",
					Source:    "manual",
					Success:   true,
					LatencyMS: sql.NullInt64{Int64: 12, Valid: true},
					CheckedAt: time.Now(),
				}}
				tx.RowsAffected = 1
			case *[]store.RouteCooldown:
				*dest = nil
				tx.RowsAffected = 0
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.HealthSnapshot:
				dest.ID = snapshotID
				tx.RowsAffected = 1
			case *store.SiteHealthState:
				dest.SiteID = siteID
				savedHealthState = *dest
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteHealthState:
				savedHealthState = *dest
				tx.RowsAffected = 1
			case map[string]any:
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
	registry := adapter.NewRegistry()
	registry.Register(refreshStateAdapterModule{
		siteTypes: []string{"refresh-state-health"},
		detect:    adapter.DetectResult{SiteType: "refresh-state-health", Matched: true, Confidence: 1},
	})
	service.adapters = registry

	unknown, err := service.SiteHealth(t.Context(), siteID)
	if err != nil {
		t.Fatalf("SiteHealth() error = %v", err)
	}
	if unknown.State.Status != "unknown" || len(unknown.Recent) != 1 {
		t.Fatalf("SiteHealth() = %#v, want unknown state with recent snapshot", unknown)
	}

	checked, err := service.CheckSiteHealth(t.Context(), siteID, " ")
	if err != nil {
		t.Fatalf("CheckSiteHealth() error = %v", err)
	}
	if !checked.Snapshot.Success || checked.State.SiteID != siteID || savedHealthState.Status != "healthy" {
		t.Fatalf("CheckSiteHealth() = %#v saved=%#v, want healthy snapshot/state", checked, savedHealthState)
	}
}

func TestCleanupExpiredHealthDataNilStoreGuard(t *testing.T) {
	t.Parallel()

	err := siteServiceWithoutStore().CleanupExpiredHealthData(t.Context(), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("CleanupExpiredHealthData(nil store) error = %v, want store guard", err)
	}
}

func TestRefreshAllStatesOAuthSkipsAndUnsupportedFailure(t *testing.T) {
	t.Parallel()

	oauthID := uuid.New()
	unsupportedID := uuid.New()
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.Site:
				*dest = []store.Site{
					{ID: oauthID, Name: "codex", SiteType: "codex", BaseURL: "https://codex.test", Enabled: true, CreatedAt: time.Now()},
					{ID: unsupportedID, Name: "unsupported", SiteType: "refresh-state-unsupported", BaseURL: "https://unsupported.test", Enabled: true, CreatedAt: time.Now().Add(-time.Minute)},
				}
				tx.RowsAffected = 2
			case *store.OAuthConnection:
				*dest = store.OAuthConnection{ID: uuid.New(), SiteID: &oauthID, Provider: "codex", Status: "reconnect_required"}
				tx.RowsAffected = 1
			case *[]store.OAuthConnection:
				*dest = []store.OAuthConnection{{ID: uuid.New(), SiteID: &oauthID, Provider: "codex", Status: "reconnect_required"}}
				tx.RowsAffected = 1
			case *store.Site:
				*dest = store.Site{ID: unsupportedID, Name: "unsupported", SiteType: "refresh-state-unsupported", BaseURL: "https://unsupported.test", Enabled: true}
				tx.RowsAffected = 1
			case *store.SiteState:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
	})
	service.adapters = adapter.Registry{}

	results, err := service.RefreshAllStates(t.Context())
	if err != nil {
		t.Fatalf("RefreshAllStates() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("RefreshAllStates() len = %d, want two results: %#v", len(results), results)
	}
	var skipped *RefreshBatchItem
	var failed *RefreshBatchItem
	for index := range results {
		if results[index].Skipped {
			skipped = &results[index]
		}
		if results[index].Err != nil {
			failed = &results[index]
		}
	}
	if skipped == nil || !strings.Contains(skipped.SkipReason, "manual reconnect") {
		t.Fatalf("RefreshAllStates() = %#v, want oauth manual reconnect skip", results)
	}
	if failed == nil || failed.Result.State.SyncStatus != "failed" {
		t.Fatalf("RefreshAllStates() = %#v, want unsupported refresh failure marked", results)
	}
}

func TestUpsertModelPriceTxCreatesManualPricing(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	canonicalID := uuid.New()
	input := 1.25
	cache := 0.25
	output := 2.5
	var savedPricing store.SiteModelPricing
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteModel:
				*dest = store.SiteModel{
					ID:           modelID,
					SiteID:       siteID,
					UpstreamName: "gpt-refresh-state",
					DisplayName:  "GPT Refresh State",
					CanonicalID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
					Status:       "active",
				}
				tx.RowsAffected = 1
			case *store.Site:
				*dest = store.Site{ID: siteID, Name: "openai", SiteType: "openai", Enabled: true}
				tx.RowsAffected = 1
			case *[]store.SiteModelPricing:
				*dest = nil
				tx.RowsAffected = 0
			case *store.SiteModelPricing:
				tx.AddError(gorm.ErrRecordNotFound)
			case *store.CanonicalModel:
				*dest = store.CanonicalModel{ID: canonicalID, Provider: "openai", ModelKey: "gpt-refresh-state", DisplayName: "GPT Refresh State", Status: "active"}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			if pricing, ok := tx.Statement.Dest.(*store.SiteModelPricing); ok {
				pricing.ID = uuid.New()
				savedPricing = *pricing
				tx.RowsAffected = 1
				return
			}
			tx.AddError(gorm.ErrInvalidData)
		},
	})

	item, created, err := service.upsertModelPriceTx(t.Context(), service.db.DB(), modelID, ModelPriceInput{
		GroupName:       " premium ",
		BillingType:     "tokens",
		Currency:        "usd",
		InputValue:      &input,
		OutputValue:     &output,
		CacheInputValue: &cache,
		ManualNote:      "refresh-state",
	}, uuid.Nil)
	if err != nil {
		t.Fatalf("upsertModelPriceTx() error = %v", err)
	}
	if !created || item.Pricing == nil || item.PricingStatus != "manual" {
		t.Fatalf("upsertModelPriceTx() = %#v created=%v, want created manual pricing", item, created)
	}
	if savedPricing.GroupName != "default" || savedPricing.GroupRatio != 1 || !savedPricing.CacheRatio.Valid || savedPricing.CacheRatio.Float64 != cache/input {
		t.Fatalf("saved pricing = %#v, want api key base pricing and cache ratio", savedPricing)
	}
}

func TestModelPriceTransactionEntryGuards(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	canonicalID := uuid.New()
	value := 0.1
	service := siteServiceWithoutStore()

	if _, _, err := service.UpsertModelPrice(t.Context(), modelID, ModelPriceInput{InputValue: &value}); err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("UpsertModelPrice(nil store) error = %v, want store guard", err)
	}
	result, err := service.BulkUpsertModelPrices(t.Context(), BulkModelPriceInput{
		CanonicalModelID: canonicalID,
		SiteModelIDs:     []uuid.UUID{modelID},
		ModelPriceInput:  ModelPriceInput{InputValue: &value},
	})
	if result.Count != 0 || err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("BulkUpsertModelPrices(nil store) = %#v, %v, want store guard", result, err)
	}
}

func TestServiceCreateAttachSystemAuthAndAPIKeyPaths(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = store.Site{ID: siteID, Name: "refresh-state", Slug: "refresh-state", SiteType: "newapi", BaseURL: "https://old.test", Status: "active", Enabled: true}
				tx.RowsAffected = 1
			case *store.SiteCredential:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.SiteCredential:
				*dest = nil
				tx.RowsAffected = 0
			case *[]store.SiteModel:
				*dest = []store.SiteModel{{ID: uuid.New(), SiteID: siteID, UpstreamName: "gpt-refresh-state", DisplayName: "GPT Refresh State", Status: "active"}}
				tx.RowsAffected = 1
			case *store.SiteAPIKeyModel:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				if dest.ID == uuid.Nil {
					dest.ID = siteID
				}
				tx.RowsAffected = 1
			case *store.SiteAPIKeyModel:
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			switch tx.Statement.Dest.(type) {
			case *store.Site:
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
	registry := adapter.NewRegistry()
	registry.Register(refreshStateAdapterModule{siteTypes: []string{"refresh-state-default"}, defaultBaseURL: "https://default.test"})
	service.adapters = registry

	createGuardService := siteServiceWithoutStore()
	createGuardService.adapters = registry
	created, _, err := createGuardService.Create(t.Context(), CreateSiteParams{Name: " Refresh State ", Slug: " refresh-state ", SiteType: "refresh-state-default", Enabled: true})
	if created.ID != uuid.Nil || err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("Create(nil store) = %#v, %v, want default-base-url path to reach store guard", created, err)
	}
	if _, err := service.CreateAPIKey(t.Context(), siteID, CreateAPIKeyInput{APIKey: "sk-refresh-state"}); err == nil || !strings.Contains(err.Error(), "does not support multiple api keys") {
		t.Fatalf("CreateAPIKey(non api-key site) error = %v, want site type guard", err)
	}

	attached, err := service.AttachOAuthConnection(t.Context(), siteID, " codex ", uuid.MustParse("11111111-1111-1111-1111-111111111111"), map[string]any{"email": "refresh-state@example.test"})
	if err != nil {
		t.Fatalf("AttachOAuthConnection() error = %v", err)
	}
	if !strings.Contains(string(attached.Meta), "refresh-state@example.test") {
		t.Fatalf("AttachOAuthConnection() meta = %s, want merged attributes", attached.Meta)
	}
	if strings.Contains(string(attached.Meta), "old-connection") || strings.Contains(string(attached.Meta), "old-provider") {
		t.Fatalf("AttachOAuthConnection() meta retained stale oauth fields: %s", attached.Meta)
	}
	if attached.Name != "refresh-state" {
		t.Fatalf("AttachOAuthConnection() changed site name to %q, want original name", attached.Name)
	}

	systemService := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = store.Site{ID: siteID, SiteType: "xlyra", BaseURL: "https://xlyra.test", Enabled: true}
				tx.RowsAffected = 1
			case *store.SiteCredential:
				*dest = siteEncryptedCredential(t, credentialID, siteID, xlyraAccessTokenCredential, "xlyra-token", nil)
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
	auth, err := systemService.SystemAuth(t.Context(), siteID)
	if err != nil {
		t.Fatalf("SystemAuth() error = %v", err)
	}
	if auth.AccessToken != "xlyra-token" {
		t.Fatalf("SystemAuth() = %#v, want decrypted xlyra token", auth)
	}
	displayName := "unsupported"
	if _, err := systemService.UpdateAPIKey(t.Context(), siteID, credentialID, UpdateAPIKeyInput{DisplayName: &displayName}); err == nil || !strings.Contains(err.Error(), "does not support api key routing configuration") {
		t.Fatalf("UpdateAPIKey(non api-key site routing config) error = %v, want site type guard", err)
	}
}

func TestManualNewAPICredentialValidationAfterStateLookup(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	wrongSiteCredentialID := uuid.New()
	disabledCredentialID := uuid.New()
	nonAPIKeyCredentialID := uuid.New()
	now := time.Now()

	tests := []struct {
		name       string
		credential store.SiteCredential
		states     []store.SiteAPIKeyState
		want       string
	}{
		{
			name:       "credential belongs to another site",
			credential: store.SiteCredential{ID: wrongSiteCredentialID, SiteID: uuid.New(), CredentialType: defaultCredentialType},
			want:       "does not belong to site",
		},
		{
			name:       "credential is not api key type",
			credential: store.SiteCredential{ID: nonAPIKeyCredentialID, SiteID: siteID, CredentialType: newAPIAccessTokenCredential},
			want:       "is not an api key",
		},
		{
			name:       "state disables credential",
			credential: store.SiteCredential{ID: disabledCredentialID, SiteID: siteID, CredentialType: defaultCredentialType, Meta: siteJSONMeta(t, map[string]any{"enabled": true})},
			states:     []store.SiteAPIKeyState{{SiteID: siteID, SiteCredentialID: disabledCredentialID, Enabled: false}},
			want:       "is disabled",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
				switch dest := tx.Statement.Dest.(type) {
				case *[]store.SiteAPIKeyState:
					*dest = tt.states
					tx.RowsAffected = int64(len(tt.states))
				case *store.SiteCredential:
					*dest = tt.credential
					tx.RowsAffected = 1
				default:
					tx.AddError(gorm.ErrInvalidData)
				}
			}})

			err := syncManualNewAPISiteAPIKeyModels(t.Context(), db, siteID, store.SiteModel{ID: modelID, SiteID: siteID, UpstreamName: "gpt-refresh-state"}, []uuid.UUID{tt.credential.ID}, now)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("syncManualNewAPISiteAPIKeyModels() error = %v, want %q", err, tt.want)
			}
		})
	}
}
