package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

func TestShouldMarkGrokCredentialSyncFailed(t *testing.T) {
	if !shouldMarkGrokCredentialSyncFailed(gatewayAttemptResult{
		statusCode:         http.StatusUnauthorized,
		upstreamStatusCode: http.StatusUnauthorized,
		errorType:          "upstream_http_error",
	}) {
		t.Fatal("upstream 401 should mark the Grok credential sync state failed")
	}
	for _, result := range []gatewayAttemptResult{
		{statusCode: http.StatusOK, upstreamStatusCode: http.StatusOK, errorType: "upstream_http_error"},
		{statusCode: http.StatusTooManyRequests, upstreamStatusCode: http.StatusTooManyRequests, errorType: "upstream_http_error"},
		{statusCode: http.StatusForbidden, upstreamStatusCode: http.StatusForbidden, errorType: "upstream_http_error"},
		{statusCode: http.StatusForbidden, upstreamStatusCode: http.StatusForbidden, errorType: "grok_cloudflare_challenge"},
		{statusCode: http.StatusForbidden, upstreamStatusCode: http.StatusForbidden, errorType: "flaresolverr_failed"},
		{statusCode: http.StatusUnauthorized, upstreamStatusCode: http.StatusUnauthorized, errorType: "upstream_response_transform_failed"},
		{statusCode: http.StatusUnauthorized, errorType: "upstream_http_error"},
	} {
		if shouldMarkGrokCredentialSyncFailed(result) {
			t.Fatalf("result %#v should not mark sync state failed", result)
		}
	}
}

func TestMarkGrokCredentialSyncFailedDisablesCredentialAndStateAtomically(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			siteID := uuid.New()
			credentialID := uuid.New()
			credential := store.SiteCredential{
				ID:             credentialID,
				SiteID:         siteID,
				CredentialType: "grok_sso:" + uuid.NewString(),
				Meta:           store.JSON(`{"enabled":true,"tier":"premium"}`),
			}
			state := store.SiteAPIKeyState{
				SiteCredentialID: credentialID,
				SiteID:           siteID,
				Enabled:          true,
				SyncStatus:       "synced",
			}
			db := grokCredentialTransactionGorm(t)
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				switch destination := tx.Statement.Dest.(type) {
				case *store.SiteCredential:
					*destination = credential
				case *store.SiteAPIKeyState:
					*destination = state
				default:
					tx.AddError(gorm.ErrInvalidData)
					return
				}
				tx.Statement.RowsAffected = 1
			}); err != nil {
				t.Fatalf("replace query callback: %v", err)
			}
			updatesInTransaction := 0
			if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
				if _, ok := tx.Statement.ConnPool.(*sql.Tx); !ok {
					tx.AddError(errors.New("grok credential update was not in a transaction"))
					return
				}
				updatesInTransaction++
				switch item := tx.Statement.Dest.(type) {
				case *store.SiteCredential:
					credential = *item
				case *store.SiteAPIKeyState:
					state = *item
				default:
					tx.AddError(gorm.ErrInvalidData)
					return
				}
				tx.Statement.RowsAffected = 1
			}); err != nil {
				t.Fatalf("replace update callback: %v", err)
			}

			handler := Handler{db: gatewayStoreWithGorm(t, db)}
			handler.markGrokCredentialSyncFailed(context.Background(), credential, gatewayAttemptResult{
				statusCode:         status,
				upstreamStatusCode: status,
				errorType:          "upstream_http_error",
			})

			var meta map[string]any
			if err := json.Unmarshal(credential.Meta, &meta); err != nil {
				t.Fatalf("decode credential meta: %v", err)
			}
			if enabled, _ := meta["enabled"].(bool); enabled || meta["tier"] != "premium" {
				t.Fatalf("credential meta = %#v, want disabled with tier preserved", meta)
			}
			wantMessage := fmt.Sprintf("grok upstream returned status %d", status)
			if state.Enabled || state.SyncStatus != "reauth_required" || !state.SyncMessage.Valid || state.SyncMessage.String != wantMessage {
				t.Fatalf("state = %#v, want disabled reauth state with message %q", state, wantMessage)
			}
			if updatesInTransaction != 3 {
				t.Fatalf("transactional updates = %d, want 3", updatesInTransaction)
			}
			if candidates := grokCredentialCandidates([]store.GatewayCredential{{Credential: credential}}, []store.SiteAPIKeyState{state}); len(candidates) != 0 {
				t.Fatalf("candidates = %#v, want disabled credential excluded", candidates)
			}
		})
	}
}

func TestGrokCredentialCandidatesExcludeUnusableAccounts(t *testing.T) {
	validID := uuid.New()
	failedID := uuid.New()
	credentials := []store.GatewayCredential{
		{Credential: store.SiteCredential{ID: validID, CredentialType: "grok_sso:valid"}},
		{Credential: store.SiteCredential{ID: failedID, CredentialType: "grok_sso:failed"}},
		{Credential: store.SiteCredential{ID: uuid.New(), CredentialType: "api_key"}},
	}
	states := []store.SiteAPIKeyState{
		{SiteCredentialID: validID, Enabled: true, SyncStatus: "synced", RemainQuota: sql.NullInt64{Int64: 42, Valid: true}},
		{SiteCredentialID: failedID, Enabled: true, SyncStatus: "failed"},
	}

	got := grokCredentialCandidates(credentials, states)
	if len(got) != 1 || got[0].Credential.Credential.ID != validID || got[0].RemainingQuota != 42 {
		t.Fatalf("candidates = %#v, want only valid account with quota", got)
	}
}

func TestGrokCredentialCandidatesExcludeCredentialDisabledInMetaWithoutState(t *testing.T) {
	credentialID := uuid.New()
	candidates := grokCredentialCandidates([]store.GatewayCredential{{Credential: store.SiteCredential{
		ID:             credentialID,
		CredentialType: "grok_sso:" + uuid.NewString(),
		Meta:           store.JSON(`{"enabled":false}`),
	}}}, nil)
	if len(candidates) != 0 {
		t.Fatalf("candidates = %#v, want disabled credential excluded", candidates)
	}
}

func TestCredentialWindowSelectorPrefersAttemptsInflightQuotaAndAge(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	selector := newCredentialWindowSelector(time.Minute)
	selector.now = func() time.Time { return now }
	first := grokCredentialCandidate{Credential: store.GatewayCredential{Credential: store.SiteCredential{ID: uuid.New()}}, RemainingQuota: 20}
	second := grokCredentialCandidate{Credential: store.GatewayCredential{Credential: store.SiteCredential{ID: uuid.New()}}, RemainingQuota: 10}

	selected, release, ok := selector.selectCredential([]grokCredentialCandidate{second, first})
	if !ok || selected.Credential.Credential.ID != first.Credential.Credential.ID {
		t.Fatalf("selected credential = %s, want higher quota %s", selected.Credential.Credential.ID, first.Credential.Credential.ID)
	}

	selectedAgain, releaseAgain, ok := selector.selectCredential([]grokCredentialCandidate{first, second})
	if !ok || selectedAgain.Credential.Credential.ID != second.Credential.Credential.ID {
		t.Fatalf("selected credential = %s, want lower-attempt credential %s", selectedAgain.Credential.Credential.ID, second.Credential.Credential.ID)
	}
	releaseAgain()
	release()
	_, releaseExtra, ok := selector.selectCredential([]grokCredentialCandidate{first})
	if !ok {
		t.Fatal("expected an extra selection")
	}
	releaseExtra()

	now = now.Add(time.Minute)
	selectedAfterWindow, releaseAfterWindow, ok := selector.selectCredential([]grokCredentialCandidate{first, second})
	if !ok || selectedAfterWindow.Credential.Credential.ID != first.Credential.Credential.ID {
		t.Fatalf("selected credential = %s, want higher quota after expiry %s", selectedAfterWindow.Credential.Credential.ID, first.Credential.Credential.ID)
	}
	releaseAfterWindow()
}

func TestCredentialWindowSelectorIsConcurrencySafe(t *testing.T) {
	selector := newCredentialWindowSelector(time.Minute)
	candidates := []grokCredentialCandidate{
		{Credential: store.GatewayCredential{Credential: store.SiteCredential{ID: uuid.New()}}},
		{Credential: store.GatewayCredential{Credential: store.SiteCredential{ID: uuid.New()}}},
	}
	counts := map[uuid.UUID]int{}
	var countsMu sync.Mutex
	var wg sync.WaitGroup

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			selected, release, ok := selector.selectCredential(candidates)
			if !ok {
				t.Error("expected a selected credential")
				return
			}
			countsMu.Lock()
			counts[selected.Credential.Credential.ID]++
			countsMu.Unlock()
			release()
		}()
	}
	wg.Wait()

	if counts[candidates[0].Credential.Credential.ID] != 50 || counts[candidates[1].Credential.Credential.ID] != 50 {
		t.Fatalf("selection counts = %#v, want an even split", counts)
	}
}

func grokCredentialTransactionGorm(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(grokCredentialTransactionConnector{})
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open grok credential transaction gorm db: %v", err)
	}
	return db
}

type grokCredentialTransactionConnector struct{}

func (grokCredentialTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return grokCredentialTransactionConn{}, nil
}

func (grokCredentialTransactionConnector) Driver() driver.Driver {
	return grokCredentialTransactionDriver{}
}

type grokCredentialTransactionDriver struct{}

func (grokCredentialTransactionDriver) Open(string) (driver.Conn, error) {
	return grokCredentialTransactionConn{}, nil
}

type grokCredentialTransactionConn struct{}

func (grokCredentialTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("grok credential fake driver only supports transactions")
}

func (grokCredentialTransactionConn) Close() error {
	return nil
}

func (grokCredentialTransactionConn) Begin() (driver.Tx, error) {
	return grokCredentialTransactionTx{}, nil
}

func (grokCredentialTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return grokCredentialTransactionTx{}, nil
}

type grokCredentialTransactionTx struct{}

func (grokCredentialTransactionTx) Commit() error {
	return nil
}

func (grokCredentialTransactionTx) Rollback() error {
	return nil
}
