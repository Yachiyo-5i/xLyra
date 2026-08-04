package site

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

func TestMatchGrokCredentialIdentityUsesPrincipalAndTeam(t *testing.T) {
	service := siteServiceWithoutStore()
	matching := store.SiteCredential{
		ID:             uuid.New(),
		CredentialType: grokCredentialPrefix + uuid.NewString(),
		Meta:           store.JSON(`{"principal_id":"principal-1","team_id":"team-1"}`),
	}
	otherTeam := store.SiteCredential{
		ID:             uuid.New(),
		CredentialType: grokCredentialPrefix + uuid.NewString(),
		Meta:           store.JSON(`{"principal_id":"principal-1","team_id":"team-2"}`),
	}
	got, ok := service.matchGrokCredentialIdentity([]store.SiteCredential{otherTeam, matching}, oauthsvc.GrokTokens{PrincipalID: "principal-1", TeamID: "team-1"})
	if !ok || got.ID != matching.ID {
		t.Fatalf("match = %#v, %v", got, ok)
	}
	if _, ok := service.matchGrokCredentialIdentity([]store.SiteCredential{matching}, oauthsvc.GrokTokens{PrincipalID: "principal-1", TeamID: "team-2"}); ok {
		t.Fatal("the same principal in a different team must remain a separate account")
	}
}

func TestResolveGrokCredentialForLoginUsesExplicitTarget(t *testing.T) {
	service := siteServiceWithoutStore()
	siteID := uuid.New()
	targetID := uuid.New()
	target := store.SiteCredential{
		ID:             targetID,
		SiteID:         siteID,
		CredentialType: grokCredentialPrefix + uuid.NewString(),
	}
	got, found, err := service.resolveGrokCredentialForLogin([]store.SiteCredential{target}, oauthsvc.GrokTokens{}, siteID, &targetID)
	if err != nil || !found || got.ID != targetID {
		t.Fatalf("explicit target = %#v, found=%v, err=%v", got, found, err)
	}
	otherSiteID := uuid.New()
	if _, _, err := service.resolveGrokCredentialForLogin([]store.SiteCredential{target}, oauthsvc.GrokTokens{}, otherSiteID, &targetID); err == nil {
		t.Fatal("cross-site Grok target must be rejected")
	}
}

func TestGrokAccountFromStoreReturnsMaskedDataOnly(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	account, ok := grokAccountFromStore(store.SiteCredential{
		ID:             credentialID,
		CredentialType: "grok_sso:" + uuid.NewString(),
		MaskedSecret:   "sens...oken",
		Meta:           store.JSON(`{"enabled":true,"tier":"premium"}`),
	}, store.SiteAPIKeyState{SiteCredentialID: credentialID, SyncStatus: "synced", RemainQuota: sql.NullInt64{Int64: 25, Valid: true}}, []store.SiteAPIKeyModel{
		{UpstreamModelName: "grok-4.5", Available: true, Enabled: true},
		{UpstreamModelName: "grok-composer-2.5-fast", Available: true, Enabled: true},
		{UpstreamModelName: "grok-legacy", Available: true, Enabled: false},
	})
	if !ok {
		t.Fatal("grokAccountFromStore() ok = false")
	}
	if account.CredentialID != credentialID || account.MaskedToken != "sens...oken" || account.RemainingQuota != 25 || account.ModelCount != 3 {
		t.Fatalf("account = %#v", account)
	}
	encoded, err := json.Marshal(account)
	if err != nil {
		t.Fatalf("marshal account: %v", err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "encrypted_secret") || strings.Contains(string(encoded), `"token"`) {
		t.Fatalf("encoded account contains sensitive field: %s", encoded)
	}
}

func TestGrokAccountFromStoreImageModelShowsUpstreamName(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	account, ok := grokAccountFromStore(store.SiteCredential{
		ID:             credentialID,
		CredentialType: "grok_sso:" + uuid.NewString(),
		Meta:           store.JSON(`{"enabled":true}`),
	}, store.SiteAPIKeyState{SiteCredentialID: credentialID, SyncStatus: "synced"}, []store.SiteAPIKeyModel{
		{UpstreamModelName: "grok-4.5", DisplayName: "Grok 4.5", Available: true, Enabled: true},
		{UpstreamModelName: "grok-imagine-image-quality", DisplayName: "Grok Imagine Image", Available: true, Enabled: true},
	})
	if !ok {
		t.Fatal("grokAccountFromStore() ok = false")
	}
	byName := map[string]string{}
	for _, model := range account.Models {
		byName[model.UpstreamName] = model.DisplayName
	}
	if byName["grok-4.5"] != "Grok 4.5" {
		t.Fatalf("chat model display = %q, want kept display name", byName["grok-4.5"])
	}
	if byName["grok-imagine-image-quality"] != "grok-imagine-image-quality" {
		t.Fatalf("image model display = %q, want raw upstream name", byName["grok-imagine-image-quality"])
	}
}

func TestDeleteGrokAccountRelationsCleansDynamicState(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	deleted := map[string]int{}
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		deleteCallback: func(tx *gorm.DB) {
			switch tx.Statement.Model.(type) {
			case *store.RouteCooldown:
				deleted["cooldown"]++
			case *store.SiteAPIKeyModel:
				deleted["model"]++
			case *store.SiteAPIKeyState:
				deleted["state"]++
			case *store.SiteCredential:
				deleted["credential"]++
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	if err := deleteGrokAccountRelations(context.Background(), db, siteID, credentialID); err != nil {
		t.Fatalf("deleteGrokAccountRelations() error = %v", err)
	}
	for _, key := range []string{"cooldown", "model", "state", "credential"} {
		if deleted[key] != 1 {
			t.Fatalf("deleted[%q] = %d, want 1; all=%#v", key, deleted[key], deleted)
		}
	}
}

func TestUpdateGrokAccountRollsBackWhenCooldownCleanupFails(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	service := siteServiceWithoutStore()
	credential := siteEncryptedCredential(t, credentialID, siteID, grokCredentialPrefix+uuid.NewString(), "old-token", store.JSON(`{"enabled":false}`))
	state := store.SiteAPIKeyState{
		SiteCredentialID: credentialID,
		SiteID:           siteID,
		Enabled:          false,
		SyncStatus:       "failed",
	}
	tracker := &grokAccountTransactionTracker{}
	db := grokAccountTrackedTransactionGorm(t, tracker)
	siteReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{ID: siteID, SiteType: "grok"}
		case *store.SiteCredential:
			*dest = credential
		case *[]store.SiteAPIKeyState:
			*dest = []store.SiteAPIKeyState{state}
		case *store.SiteAPIKeyState:
			*dest = state
		case *[]store.SiteAPIKeyModel:
			*dest = nil
		default:
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	})
	siteReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	})
	cooldownErr := errors.New("cooldown cleanup unavailable")
	siteReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*store.RouteCooldown); !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		tx.AddError(cooldownErr)
	})
	service.db = siteStoreWithGorm(t, db)

	_, err := service.UpdateGrokAccount(context.Background(), siteID, credentialID, GrokAccountUpdate{Enabled: boolPointer(true)})
	if !errors.Is(err, cooldownErr) {
		t.Fatalf("UpdateGrokAccount() error = %v, want cooldown cleanup error", err)
	}
	if tracker.rollbacks != 1 || tracker.commits != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want commits=0 rollbacks=1", tracker.commits, tracker.rollbacks)
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func grokAccountTransactionGorm(t *testing.T) *gorm.DB {
	t.Helper()

	return grokAccountTrackedTransactionGorm(t, nil)
}

func grokAccountTrackedTransactionGorm(t *testing.T, tracker *grokAccountTransactionTracker) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(grokAccountTransactionConnector{tracker: tracker})
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
		t.Fatalf("open grok account transaction gorm db: %v", err)
	}
	return db
}

type grokAccountTransactionTracker struct {
	commits   int
	rollbacks int
}

type grokAccountTransactionConnector struct {
	tracker *grokAccountTransactionTracker
}

func (c grokAccountTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return grokAccountTransactionConn{tracker: c.tracker}, nil
}

func (grokAccountTransactionConnector) Driver() driver.Driver {
	return grokAccountTransactionDriver{}
}

type grokAccountTransactionDriver struct{}

func (grokAccountTransactionDriver) Open(string) (driver.Conn, error) {
	return grokAccountTransactionConn{}, nil
}

type grokAccountTransactionConn struct {
	tracker *grokAccountTransactionTracker
}

func (grokAccountTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("grok account fake driver only supports transactions")
}

func (grokAccountTransactionConn) Close() error {
	return nil
}

func (c grokAccountTransactionConn) Begin() (driver.Tx, error) {
	return grokAccountTransactionTx{tracker: c.tracker}, nil
}

func (c grokAccountTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return grokAccountTransactionTx{tracker: c.tracker}, nil
}

type grokAccountTransactionTx struct {
	tracker *grokAccountTransactionTracker
}

func (tx grokAccountTransactionTx) Commit() error {
	if tx.tracker != nil {
		tx.tracker.commits++
	}
	return nil
}

func (tx grokAccountTransactionTx) Rollback() error {
	if tx.tracker != nil {
		tx.tracker.rollbacks++
	}
	return nil
}
