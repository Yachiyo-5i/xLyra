package site

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestRefreshStateSnapshotUsesFallbackOnLookupError(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(errors.New("state lookup stopped"))
	})

	got := service.refreshStateSnapshot(t.Context(), siteID, "skipped")
	if got.SiteID != siteID || got.SyncStatus != "skipped" {
		t.Fatalf("refreshStateSnapshot() = %#v, want fallback site/status", got)
	}
}

func TestAutoDisableAndRestoreReturnInputForNilOrManualDisabledSite(t *testing.T) {
	t.Parallel()

	service := &Service{}
	manualDisabled := store.Site{ID: uuid.New(), Enabled: false}
	autoDisabled := store.Site{
		ID:      uuid.New(),
		Enabled: false,
		Meta:    siteJSONMeta(t, map[string]any{siteMetaAutoDisabledByRefresh: true}),
	}

	if got := service.autoDisableSiteAfterRefreshFailure(t.Context(), store.Site{}); got.ID != uuid.Nil {
		t.Fatalf("autoDisableSiteAfterRefreshFailure(nil) = %#v, want input", got)
	}
	if got := service.autoDisableSiteAfterRefreshFailure(t.Context(), manualDisabled); got.ID != manualDisabled.ID || got.Enabled {
		t.Fatalf("autoDisableSiteAfterRefreshFailure(manual disabled) = %#v, want unchanged disabled site", got)
	}
	if got := service.restoreAutoDisabledSiteAfterRefreshSuccess(t.Context(), store.Site{}); got.ID != uuid.Nil {
		t.Fatalf("restoreAutoDisabledSiteAfterRefreshSuccess(nil) = %#v, want input", got)
	}
	if got := service.restoreAutoDisabledSiteAfterRefreshSuccess(t.Context(), manualDisabled); got.ID != manualDisabled.ID || got.Enabled {
		t.Fatalf("restoreAutoDisabledSiteAfterRefreshSuccess(manual disabled) = %#v, want unchanged disabled site", got)
	}
	if !siteAutoDisabledByRefresh(autoDisabled) {
		t.Fatal("fixture should be marked auto-disabled")
	}
}

func TestRestoreAPIKeyStateAfterAuthRecoveryEnablesExistingState(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	var saved store.SiteAPIKeyState
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			state, ok := tx.Statement.Dest.(*store.SiteAPIKeyState)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			*state = store.SiteAPIKeyState{SiteCredentialID: credentialID, Enabled: false, SyncStatus: "synced"}
			tx.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			state, ok := tx.Statement.Dest.(*store.SiteAPIKeyState)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			saved = *state
			tx.RowsAffected = 1
		},
	})

	state, err := restoreAPIKeyStateAfterAuthRecovery(t.Context(), store.NewSiteAPIKeyStateRepository(db), credentialID)
	if err != nil {
		t.Fatalf("restoreAPIKeyStateAfterAuthRecovery() error = %v", err)
	}
	if !state.Enabled || !saved.Enabled {
		t.Fatalf("restored state = item:%#v saved:%#v, want enabled", state, saved)
	}
}

func TestMarkRefreshFailedKeepsRecoverableCredentialFailuresEnabled(t *testing.T) {
	t.Parallel()

	for _, message := range []string{
		`all 1 enabled api keys failed: upstream returned 401: {"error":{"code":"api_key_daily_quota_exhausted","message":"API key daily quota has been exhausted."}}`,
		`all 1 enabled api keys failed: upstream returned 429: {"code":"USAGE_LIMIT_EXCEEDED","message":"daily limit exceeded"}`,
		`all 1 enabled api keys failed: upstream returned 403: {"code":"INSUFFICIENT_BALANCE","message":"Insufficient account balance"}`,
		"all 1 enabled api keys failed: upstream returned 401: authentication failed",
		"all 1 enabled api keys failed: provider rejected the model request",
	} {
		message := message
		t.Run(message, func(t *testing.T) {
			t.Parallel()

			siteID := uuid.New()
			item := store.Site{ID: siteID, Enabled: true}
			siteWrite := false
			var savedState store.SiteState
			service := siteServiceWithCallbacks(t, siteGormCallbacks{
				query: func(tx *gorm.DB) {
					switch tx.Statement.Dest.(type) {
					case *store.SiteState:
						tx.AddError(gorm.ErrRecordNotFound)
					case *store.Site:
						siteWrite = true
						tx.AddError(gorm.ErrInvalidData)
					default:
						tx.AddError(gorm.ErrInvalidData)
					}
				},
				create: siteCaptureCreate[store.SiteState](t, "site state", func(state store.SiteState) {
					savedState = state
				}),
				update: func(tx *gorm.DB) {
					siteWrite = true
					tx.AddError(gorm.ErrInvalidData)
				},
			})

			result, err := service.markRefreshFailed(t.Context(), item, message)
			if err == nil {
				t.Fatal("markRefreshFailed() error = nil, want refresh error")
			}
			if siteWrite || !result.Site.Enabled {
				t.Fatalf("recoverable failure changed site availability: write=%v site=%#v", siteWrite, result.Site)
			}
			if savedState.SyncStatus != "partial" || !savedState.ValidationOK.Valid || !savedState.ValidationOK.Bool || savedState.ValidationMessage.Valid {
				t.Fatalf("saved recoverable state = %#v, want partial and validation ok", savedState)
			}
		})
	}
}

func TestMarkRefreshFailedDisablesConfirmedInvalidCredential(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	item := store.Site{ID: siteID, Enabled: true, Meta: store.JSON(`{}`)}
	var savedSite store.Site
	var savedState store.SiteState
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = item
				tx.RowsAffected = 1
			case *store.SiteState:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: siteCaptureCreate[store.SiteState](t, "site state", func(state store.SiteState) {
			savedState = state
		}),
		update: siteCaptureUpdate[store.Site](t, "site", func(site store.Site) {
			savedSite = site
		}),
	})

	message := `all 1 enabled api keys failed: upstream returned 401: {"error":{"code":"invalid_api_key","message":"Invalid API key"}}`
	result, err := service.markRefreshFailed(t.Context(), item, message)
	if err == nil {
		t.Fatal("markRefreshFailed() error = nil, want refresh error")
	}
	if result.Site.Enabled || savedSite.Enabled || !siteAutoDisabledByRefresh(savedSite) {
		t.Fatalf("invalid credential site = result:%#v saved:%#v, want auto-disabled", result.Site, savedSite)
	}
	if savedState.SyncStatus != "failed" || !savedState.ValidationOK.Valid || savedState.ValidationOK.Bool || !savedState.ValidationMessage.Valid {
		t.Fatalf("saved invalid credential state = %#v, want failed validation", savedState)
	}
}

func TestMarkRefreshFailedDisablesUnknownHTTPFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	item := store.Site{ID: siteID, Enabled: true, Meta: store.JSON(`{}`)}
	var savedSite store.Site
	var savedState store.SiteState
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = item
				tx.RowsAffected = 1
			case *store.SiteState:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: siteCaptureCreate[store.SiteState](t, "site state", func(state store.SiteState) {
			savedState = state
		}),
		update: siteCaptureUpdate[store.Site](t, "site", func(site store.Site) {
			savedSite = site
		}),
	})

	result, err := service.markRefreshFailed(t.Context(), item, "upstream returned 404: endpoint not found")
	if err == nil {
		t.Fatal("markRefreshFailed() error = nil, want refresh error")
	}
	if result.Site.Enabled || savedSite.Enabled || !siteAutoDisabledByRefresh(savedSite) {
		t.Fatalf("unknown HTTP failure site = result:%#v saved:%#v, want auto-disabled", result.Site, savedSite)
	}
	if savedState.SyncStatus != "failed" || !savedState.ValidationOK.Valid || savedState.ValidationOK.Bool || !savedState.ValidationMessage.Valid {
		t.Fatalf("saved unknown HTTP failure state = %#v, want failed validation", savedState)
	}
}

func TestRefreshKeySummaryValidatedRequiresUpstreamSummary(t *testing.T) {
	t.Parallel()

	if refreshKeySummaryValidated(preparedRefreshKey{hasRawKey: false}, refreshKeySummary{modelsSynced: true}) {
		t.Fatal("missing raw key should not be considered validated")
	}
	if refreshKeySummaryValidated(preparedRefreshKey{hasRawKey: true}, refreshKeySummary{modelsSynced: false}) {
		t.Fatal("missing upstream summary should not be considered validated")
	}
	if !refreshKeySummaryValidated(preparedRefreshKey{hasRawKey: true}, refreshKeySummary{modelsSynced: true}) {
		t.Fatal("successful upstream summary should be considered validated")
	}
}

func TestAPIKeyCredentialInputForRefreshPreservesCompatibleExistingSecret(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	siteID := uuid.New()
	credentialType := defaultCredentialType + ":upstream-1"
	encryptedSecret, maskedSecret, err := service.credentials.Encrypt("sk-live-existing")
	if err != nil {
		t.Fatalf("encrypt fixture secret: %v", err)
	}

	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteCredential:
				*dest = store.SiteCredential{
					ID:              uuid.New(),
					SiteID:          siteID,
					CredentialType:  credentialType,
					EncryptedSecret: encryptedSecret,
					MaskedSecret:    maskedSecret,
					Meta:            siteJSONMeta(t, map[string]any{"raw_key_missing": false, "manually_completed": true}),
				}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})

	input, hasRawKey, err := service.apiKeyCredentialInputForRefresh(t.Context(), store.NewSiteCredentialRepository(db), siteID, credentialType, adapter.APIKey{
		ID:        7,
		Name:      "Imported",
		MaskedKey: "...ting",
	})
	if err != nil {
		t.Fatalf("apiKeyCredentialInputForRefresh() error = %v", err)
	}
	if !hasRawKey || input.Secret != "sk-live-existing" {
		t.Fatalf("credential input secret = %q hasRaw=%v, want preserved existing raw key", input.Secret, hasRawKey)
	}
	if input.Meta["raw_key_missing"] != false || input.Meta["manually_completed"] != true {
		t.Fatalf("credential input meta = %#v, want completed raw key preserved", input.Meta)
	}
}

func TestExistingAPIKeyCredentialTreatsNotFoundAsMissing(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			tx.AddError(gorm.ErrRecordNotFound)
		},
	})

	credential, secret, meta, found, err := service.existingAPIKeyCredential(t.Context(), store.NewSiteCredentialRepository(db), uuid.New(), defaultCredentialType)
	if err != nil || found || credential.ID != uuid.Nil || secret != "" || meta != nil {
		t.Fatalf("existingAPIKeyCredential() = %#v %q %#v found=%v err=%v, want missing nil result", credential, secret, meta, found, err)
	}
}

func TestAttachOAuthConnectionPropagatesSiteLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site lookup stopped")
	service := siteServiceWithQueryError(t, queryErr)

	site, err := service.AttachOAuthConnection(t.Context(), uuid.New(), "codex", uuid.New(), map[string]any{"status": "connected"})
	if site.ID != uuid.Nil {
		t.Fatalf("AttachOAuthConnection() site = %#v, want zero site", site)
	}
	assertSiteQueryError(t, "AttachOAuthConnection", err, queryErr)
}

func TestSetAPIKeyEnabledPropagatesCredentialLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("credential lookup stopped")
	service := siteServiceWithQueryError(t, queryErr)

	got, err := service.SetAPIKeyEnabled(t.Context(), uuid.New(), uuid.New(), false)
	if got.Credential.ID != uuid.Nil {
		t.Fatalf("SetAPIKeyEnabled() = %#v, want zero credential", got)
	}
	assertSiteQueryError(t, "SetAPIKeyEnabled", err, queryErr)
}

func TestSyncGatewayAPIKeyModelsSkipsInvalidSnapshotsAndMarksKnownCredentials(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	siteModelID := uuid.New()
	now := time.Now()
	createCount := 0
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteAPIKeyModel:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.SiteAPIKeyModel:
				*dest = nil
				tx.RowsAffected = 0
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			createCount++
			tx.RowsAffected = 1
		},
	})

	items, err := syncGatewayAPIKeyModels(
		t.Context(),
		siteID,
		[]uuid.UUID{credentialID, uuid.Nil},
		[]gatewayAPIKeyModelSnapshot{
			{SiteCredentialID: uuid.Nil, Model: adapter.Model{UpstreamName: "ignored"}},
			{SiteCredentialID: credentialID, Model: adapter.Model{UpstreamName: " \t\n "}},
			{SiteCredentialID: credentialID, Model: adapter.Model{UpstreamName: "gpt-linked-credential", DisplayName: "GPT Linked Credential", Capabilities: map[string]any{"raw": map[string]any{"owned_by": "test"}}}, Enabled: false},
		},
		map[string]store.SiteModel{"gpt-linked-credential": {ID: siteModelID, UpstreamName: "gpt-linked-credential"}},
		store.NewSiteAPIKeyModelRepository(db),
		now,
	)
	if err != nil {
		t.Fatalf("syncGatewayAPIKeyModels() error = %v", err)
	}
	if len(items) != 1 || items[0].SiteID != siteID || items[0].SiteCredentialID != credentialID || items[0].UpstreamModelName != "gpt-linked-credential" {
		t.Fatalf("syncGatewayAPIKeyModels() items = %#v, want one valid snapshot", items)
	}
	if items[0].Enabled || !items[0].SiteModelID.Valid || items[0].SiteModelID.UUID != siteModelID {
		t.Fatalf("synced api key model = %#v, want disabled linked site model", items[0])
	}
	if createCount != 1 {
		t.Fatalf("create count = %d, want only valid snapshot created", createCount)
	}
}

func TestPricingAuthPropagatesAPIKeyLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("api key lookup stopped")
	service := siteServiceWithQueryError(t, queryErr)

	auth, err := service.pricingAuth(t.Context(), store.Site{ID: uuid.New(), SiteType: "openai"})
	if auth.AccessToken != "" {
		t.Fatalf("pricingAuth() = %#v, want zero auth", auth)
	}
	assertSiteQueryError(t, "pricingAuth", err, queryErr)
}
