package site

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestPersistGrokRefreshMapsTierQuotaAndModels(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	credential := siteEncryptedCredential(t, credentialID, siteID, "grok_sso:"+uuid.NewString(), `{"access_token":"token","refresh_token":"r","expires_at":9999999999}`, siteJSONMeta(t, map[string]any{
		"enabled": true,
	}))
	var savedCredential store.SiteCredential
	var createdState store.SiteAPIKeyState
	var createdModels []store.SiteAPIKeyModel
	db, _ := grokTransactionalGorm(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteCredential:
				*dest = credential
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *store.SiteAPIKeyState, *store.SiteAPIKeyModel:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.SiteAPIKeyModel:
				*dest = nil
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteAPIKeyState:
				createdState = *dest
			case *store.SiteAPIKeyModel:
				createdModels = append(createdModels, *dest)
			default:
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
		update: siteCaptureUpdate[store.SiteCredential](t, "grok credential", func(item store.SiteCredential) {
			savedCredential = item
		}),
	})
	summary := adapter.APIKeySummary{
		Usage: map[string]any{"tier": "premium_plus", "limit": int64(100), "remaining": int64(42), "used": int64(58)},
		Models: []adapter.Model{{
			UpstreamName: "grok-4-heavy",
			DisplayName:  "Grok 4 Heavy",
			Capabilities: map[string]any{"raw": map[string]any{"mode": "heavy"}},
		}},
		Raw: map[string]any{"subscription": map[string]any{"tier": "premium_plus"}},
	}

	state, models, err := persistGrokRefresh(context.Background(), siteStoreWithGorm(t, db), credential, summary, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatalf("persistGrokRefresh() error = %v", err)
	}
	if state.SiteCredentialID != credentialID || createdState.GroupName.String != "premium_plus" || createdState.RemainQuota.Int64 != 42 || createdState.UsedQuota.Int64 != 58 || createdState.SyncStatus != "synced" {
		t.Fatalf("state=%#v created=%#v", state, createdState)
	}
	if len(models) != 1 || len(createdModels) != 1 || createdModels[0].UpstreamModelName != "grok-4-heavy" {
		t.Fatalf("models=%#v created=%#v", models, createdModels)
	}
	meta := map[string]any{}
	if err := json.Unmarshal(savedCredential.Meta, &meta); err != nil {
		t.Fatalf("decode saved credential meta: %v", err)
	}
	if meta["tier"] != "premium_plus" {
		t.Fatalf("saved credential meta = %#v", meta)
	}
}

func TestPersistGrokRefreshUsesTransactionAndRollsBackWriteFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	credential := siteEncryptedCredential(t, credentialID, siteID, "grok_sso:"+uuid.NewString(), `{"access_token":"token","refresh_token":"r","expires_at":9999999999}`, siteJSONMeta(t, map[string]any{"enabled": true}))
	writeErr := errors.New("save grok model failed")
	transactionalWrites := 0
	db, tracker := grokTransactionalGorm(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteCredential:
				*dest = credential
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *store.SiteAPIKeyState, *store.SiteAPIKeyModel:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); ok {
				transactionalWrites++
			}
			switch tx.Statement.Dest.(type) {
			case *store.SiteAPIKeyState:
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *store.SiteAPIKeyModel:
				tx.AddError(writeErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); ok {
				transactionalWrites++
			}
			if _, ok := tx.Statement.Dest.(*store.SiteCredential); !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})

	_, _, err := persistGrokRefresh(context.Background(), siteStoreWithGorm(t, db), credential, adapter.APIKeySummary{
		Usage:  map[string]any{"tier": "premium"},
		Models: []adapter.Model{{UpstreamName: "grok-4"}},
	}, time.Unix(1_800_000_000, 0))
	if !errors.Is(err, writeErr) {
		t.Fatalf("persistGrokRefresh() error = %v, want %v", err, writeErr)
	}
	if transactionalWrites != 3 {
		t.Fatalf("transactional writes = %d, want credential, state, and model writes in one transaction", transactionalWrites)
	}
	if tracker.rollbacks.Load() != 1 || tracker.commits.Load() != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want rollback only", tracker.commits.Load(), tracker.rollbacks.Load())
	}
}

func TestGrokRefreshQuotaValuesAcceptBoundedNumericTypes(t *testing.T) {
	t.Parallel()

	tier, limit, remaining, used, unlimited := grokRefreshQuotaValues(map[string]any{
		"tier":      "premium",
		"limit":     float64(100),
		"remaining": json.Number("75"),
		"used":      int(25),
	})
	if tier != "premium" || limit != int64(100) || remaining != int64(75) || used != int64(25) || unlimited {
		t.Fatalf("quota = %q %v %v %v unlimited=%v", tier, limit, remaining, used, unlimited)
	}
}

func TestGrokRefreshQuotaValuesKeepsMissingQuotaUnknown(t *testing.T) {
	tier, limit, remaining, used, unlimited := grokRefreshQuotaValues(map[string]any{"tier": "premium"})
	if tier != "premium" || limit != nil || remaining != nil || used != nil || unlimited {
		t.Fatalf("quota = %q %v %v %v unlimited=%v", tier, limit, remaining, used, unlimited)
	}
}

func TestShouldReauthorizeGrokCredentialOnlyOnAuthFailure(t *testing.T) {
	t.Parallel()

	if shouldReauthorizeGrokCredential(fmt.Errorf("grok upstream returned status 500")) {
		t.Fatal("a transient upstream error must not disable the Grok credential")
	}
	if !shouldReauthorizeGrokCredential(fmt.Errorf("%w: status 401", adapter.ErrGrokUnauthorized)) {
		t.Fatal("an unauthorized upstream error should disable the Grok credential")
	}
}

func TestGrokRefreshFailureStatusPreservesLastGoodModels(t *testing.T) {
	if got := grokRefreshFailureStatus(fmt.Errorf("grok upstream returned status 500")); got != "stale" {
		t.Fatalf("transient refresh status = %q, want stale", got)
	}
	if got := grokRefreshFailureStatus(fmt.Errorf("%w: status 401", adapter.ErrGrokUnauthorized)); got != "reauth_required" {
		t.Fatalf("auth refresh status = %q, want reauth_required", got)
	}
}

func TestRefreshGrokAccountReturnsSiteModelSyncFailure(t *testing.T) {
	t.Parallel()

	service, siteID, credentialID, _ := grokRefreshService(t, 1, errors.New("list grok api key models failed"))
	_, err := service.RefreshGrokAccount(context.Background(), siteID, credentialID)
	if err == nil || !strings.Contains(err.Error(), "list grok api key models failed") {
		t.Fatalf("RefreshGrokAccount() error = %v, want site model sync failure", err)
	}
}

func TestRefreshGrokAccountUpdatesSiteStateCounts(t *testing.T) {
	t.Parallel()

	service, siteID, credentialID, savedState := grokRefreshService(t, 2, nil)
	account, err := service.RefreshGrokAccount(context.Background(), siteID, credentialID)
	if err != nil {
		t.Fatalf("RefreshGrokAccount() error = %v", err)
	}
	if account.ModelCount != 2 {
		t.Fatalf("account model count = %d, want 2", account.ModelCount)
	}
	if savedState.APIKeyCount != 1 || savedState.ModelCount != 2 || savedState.SyncStatus != "synced" {
		t.Fatalf("saved site state = %#v, want api_key_count=1 model_count=2 synced", *savedState)
	}
}

func TestRefreshStateGrokUpdatesSiteStateAPIKeyCount(t *testing.T) {
	t.Parallel()

	service, siteID, _, savedState := grokRefreshService(t, 2, nil)
	result, err := service.RefreshState(context.Background(), siteID)
	if err != nil {
		t.Fatalf("RefreshState() error = %v", err)
	}
	if result.State.APIKeyCount != 1 || savedState.APIKeyCount != 1 || result.State.ModelCount != 2 || savedState.ModelCount != 2 {
		t.Fatalf("site state counts = result:%d/%d saved:%d/%d, want 1/2", result.State.APIKeyCount, result.State.ModelCount, savedState.APIKeyCount, savedState.ModelCount)
	}
}

func TestGrokSiteAPIKeyCountIncludesDisabledAndFailedAccounts(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	firstID := uuid.New()
	secondID := uuid.New()
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{
					{ID: firstID, SiteID: siteID, CredentialType: "grok_sso:" + uuid.NewString(), Meta: store.JSON(`{"enabled":false}`)},
					{ID: secondID, SiteID: siteID, CredentialType: "grok_sso:" + uuid.NewString(), Meta: store.JSON(`{"enabled":true}`)},
					{ID: uuid.New(), SiteID: siteID, CredentialType: "api_key"},
				}
			case *[]store.SiteAPIKeyState:
				*dest = []store.SiteAPIKeyState{{SiteCredentialID: secondID, SiteID: siteID, Enabled: true, SyncStatus: "failed"}}
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})

	count, err := service.grokSiteAPIKeyCount(context.Background(), siteID)
	if err != nil {
		t.Fatalf("grokSiteAPIKeyCount() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("grok account count = %d, want total credential count 2", count)
	}
}

func TestSyncModelsLockedCloudflareChallengeKeepsGrokStateEligible(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	site := store.Site{ID: siteID, SiteType: "grok", BaseURL: "https://grok.com", Enabled: true}
	credential := siteEncryptedCredential(t, credentialID, siteID, "grok_sso:"+uuid.NewString(), `{"access_token":"token","refresh_token":"r","expires_at":9999999999}`, siteJSONMeta(t, map[string]any{"enabled": true}))
	savedState := store.SiteAPIKeyState{}
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = site
				tx.RowsAffected = 1
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{credential}
				tx.RowsAffected = 1
			case *store.SiteAPIKeyState:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
			tx.Statement.RowsAffected = tx.RowsAffected
		},
		create: siteCaptureCreate[store.SiteAPIKeyState](t, "grok api key state", func(state store.SiteAPIKeyState) {
			savedState = state
		}),
	})
	registry := adapter.NewRegistry()
	registry.Register(grokRefreshFailureModule{})
	service.adapters = registry

	_, err := service.syncModelsLocked(context.Background(), siteID)
	if err == nil || !strings.Contains(err.Error(), "all 1 enabled api keys failed") {
		t.Fatalf("syncModelsLocked() error = %v, want all keys failed", err)
	}
	if !savedState.Enabled || savedState.SyncStatus == "failed" {
		t.Fatalf("saved Grok state = %#v, want enabled selector-eligible state", savedState)
	}
}

type grokRefreshSummaryModule struct {
	models []adapter.Model
}

func (m grokRefreshSummaryModule) SiteTypes() []string {
	return []string{"grok"}
}

func (m grokRefreshSummaryModule) SummarizeAPIKey(context.Context, adapter.SiteConfig, string) (adapter.APIKeySummary, error) {
	return adapter.APIKeySummary{
		Usage:  map[string]any{"tier": "premium", "remaining": int64(140)},
		Models: m.models,
	}, nil
}

func (m grokRefreshSummaryModule) ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	return append([]adapter.Model(nil), m.models...), nil
}

type grokRefreshFailureModule struct{}

func (grokRefreshFailureModule) SiteTypes() []string {
	return []string{"grok"}
}

func (grokRefreshFailureModule) ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	return nil, fmt.Errorf("grok upstream returned status 500")
}

func (grokRefreshFailureModule) SummarizeAPIKey(context.Context, adapter.SiteConfig, string) (adapter.APIKeySummary, error) {
	return adapter.APIKeySummary{}, fmt.Errorf("grok upstream returned status 500")
}

func grokRefreshService(t *testing.T, modelCount int, syncErr error) (*Service, uuid.UUID, uuid.UUID, *store.SiteState) {
	t.Helper()

	siteID := uuid.New()
	credentialID := uuid.New()
	site := store.Site{ID: siteID, Name: "Grok", SiteType: "grok", BaseURL: "https://grok.com", Enabled: true}
	credential := siteEncryptedCredential(t, credentialID, siteID, "grok_sso:"+uuid.NewString(), `{"access_token":"token","refresh_token":"r","expires_at":9999999999}`, siteJSONMeta(t, map[string]any{"enabled": true}))
	models := make([]adapter.Model, 0, modelCount)
	apiKeyModels := make([]store.SiteAPIKeyModel, 0, modelCount)
	siteModels := make([]store.SiteModel, 0, modelCount)
	state := store.SiteAPIKeyState{}
	savedSiteState := &store.SiteState{}
	siteStateExists := false
	apiKeyModelListCalls := 0
	db, _ := grokTransactionalGorm(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = site
				tx.RowsAffected = 1
			case *store.SiteCredential:
				*dest = credential
				tx.RowsAffected = 1
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{credential}
				tx.RowsAffected = 1
			case *store.SiteAPIKeyState:
				if state.SiteCredentialID == uuid.Nil {
					tx.AddError(gorm.ErrRecordNotFound)
					return
				}
				*dest = state
				tx.RowsAffected = 1
			case *[]store.SiteAPIKeyState:
				*dest = []store.SiteAPIKeyState{state}
				tx.RowsAffected = 1
			case *store.SiteAPIKeyModel, *store.SiteModel:
				tx.AddError(gorm.ErrRecordNotFound)
			case *store.SiteState:
				if !siteStateExists {
					tx.AddError(gorm.ErrRecordNotFound)
					return
				}
				*dest = *savedSiteState
				tx.RowsAffected = 1
			case *[]store.SiteAPIKeyModel:
				apiKeyModelListCalls++
				if syncErr != nil && apiKeyModelListCalls == 2 {
					tx.AddError(syncErr)
					return
				}
				*dest = append([]store.SiteAPIKeyModel(nil), apiKeyModels...)
				tx.RowsAffected = int64(len(apiKeyModels))
			case *[]store.SiteModel:
				*dest = append([]store.SiteModel(nil), siteModels...)
				tx.RowsAffected = int64(len(siteModels))
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
			tx.Statement.RowsAffected = tx.RowsAffected
		},
		create: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteAPIKeyState:
				state = *dest
			case *store.SiteAPIKeyModel:
				dest.ID = uuid.New()
				apiKeyModels = append(apiKeyModels, *dest)
			case *store.SiteModel:
				dest.ID = uuid.New()
				siteModels = append(siteModels, *dest)
			case *store.SiteState:
				*savedSiteState = *dest
				siteStateExists = true
			default:
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteCredential:
				credential = *dest
			case map[string]any:
			case *store.SiteState:
				*savedSiteState = *dest
			default:
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	service := NewService(siteStoreWithGorm(t, db), siteTestMasterKey)
	registry := adapter.NewRegistry()
	for index := 0; index < modelCount; index++ {
		models = append(models, adapter.Model{UpstreamName: fmt.Sprintf("grok-model-%d", index+1)})
	}
	registry.Register(grokRefreshSummaryModule{models: models})
	service.adapters = registry
	return service, siteID, credentialID, savedSiteState
}

type grokTransactionTracker struct {
	commits   atomic.Int64
	rollbacks atomic.Int64
}

type grokTransactionConnector struct {
	tracker *grokTransactionTracker
}

func (c grokTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return grokTransactionConn{tracker: c.tracker}, nil
}

func (grokTransactionConnector) Driver() driver.Driver {
	return grokTransactionDriver{}
}

type grokTransactionDriver struct{}

func (grokTransactionDriver) Open(string) (driver.Conn, error) {
	return grokTransactionConn{tracker: &grokTransactionTracker{}}, nil
}

type grokTransactionConn struct {
	tracker *grokTransactionTracker
}

func (grokTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("grok transaction driver only supports transactions")
}

func (grokTransactionConn) Close() error {
	return nil
}

func (c grokTransactionConn) Begin() (driver.Tx, error) {
	return grokTransactionTx{tracker: c.tracker}, nil
}

func (c grokTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return grokTransactionTx{tracker: c.tracker}, nil
}

type grokTransactionTx struct {
	tracker *grokTransactionTracker
}

func (tx grokTransactionTx) Commit() error {
	tx.tracker.commits.Add(1)
	return nil
}

func (tx grokTransactionTx) Rollback() error {
	tx.tracker.rollbacks.Add(1)
	return nil
}

func grokTransactionalGorm(t *testing.T, callbacks siteGormCallbacks) (*gorm.DB, *grokTransactionTracker) {
	t.Helper()

	db := siteGormWithCallbacks(t, callbacks)
	tracker := &grokTransactionTracker{}
	sqlDB := sql.OpenDB(grokTransactionConnector{tracker: tracker})
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db.ConnPool = sqlDB
	db.Statement.ConnPool = sqlDB
	return db, tracker
}
