package site

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/store"
)

func TestCanceledRefreshStillRecordsUpstreamReauth(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/cancel_reauth", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fx.runtime.holdNext()
	fx.runtime.mu.Lock()
	fx.runtime.summarizeErr = accountauth.ErrReauthorizationRequired
	fx.runtime.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		_, err := fx.provider.Refresh(ctx, fx.siteID, fx.account.CredentialID)
		errCh <- err
	}()
	fx.runtime.waitEntered(t)
	cancel()
	fx.runtime.releaseHold()

	err := waitErr(t, errCh, "canceled refresh with upstream reauth")
	if !errors.Is(err, accountauth.ErrReauthorizationRequired) {
		t.Fatalf("refresh error = %v, want reauthorization required", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored == "" || stored == before {
		t.Fatalf("stored access token = %q, want the rotated token", stored)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] != true || meta["enabled"] != false {
		t.Fatalf("meta = %#v, want reauth and disabled", meta)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != "reauth_required" {
		t.Fatalf("sync status = %q, want reauth_required", got)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestRefreshGenericFailureKeepsPriorReauth(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/keep_reauth", nil)
	markPluginAccountReauth(t, fx)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	fx.runtime.mu.Lock()
	fx.runtime.summarizeErr = errors.New("summarize failed")
	fx.runtime.mu.Unlock()

	_, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
	if err == nil || !strings.Contains(err.Error(), "summarize failed") {
		t.Fatalf("refresh error = %v", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored == "" || stored == before {
		t.Fatalf("stored access token = %q, want the rotated token", stored)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] != true || meta["enabled"] != false {
		t.Fatalf("meta = %#v, want reauth to stay disabled", meta)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got == "synced" {
		t.Fatalf("sync status = %q, want the account to stay unroutable", got)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestRefreshSaveFailureKeepsUpstreamReauth(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/save_reauth", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	installCredentialMetaFailOnceTrigger(t, fx)
	fx.runtime.mu.Lock()
	fx.runtime.summarizeErr = accountauth.ErrReauthorizationRequired
	fx.runtime.refreshPublic = map[string]string{"realm": "rotated"}
	fx.runtime.mu.Unlock()

	_, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
	if err == nil || !errors.Is(err, accountauth.ErrReauthorizationRequired) || !strings.Contains(err.Error(), "injected credential meta failure") {
		t.Fatalf("refresh error = %v, want the save failure and reauthorization", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored != before {
		t.Fatalf("stored access token = %q, want %q after the rolled-back save", stored, before)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] != true || meta["enabled"] != false {
		t.Fatalf("meta = %#v, want reauth recorded after the save rolled back", meta)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != "reauth_required" {
		t.Fatalf("sync status = %q, want reauth_required", got)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestRefreshSuccessClearsReauth(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/clear_reauth", nil)
	markPluginAccountReauth(t, fx)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]

	account, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if account == nil {
		t.Fatal("missing account")
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored == "" || stored == before {
		t.Fatalf("stored access token = %q, want the rotated token", stored)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] == true || meta["enabled"] == false {
		t.Fatalf("meta = %#v, want reauth cleared and the account enabled", meta)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != "synced" {
		t.Fatalf("sync status = %q, want synced", got)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginCredentialWriteTimeoutReleasesGatewaySiteLock(t *testing.T) {
	pluginAccountPersistTimeoutForTest = 150 * time.Millisecond
	t.Cleanup(func() {
		pluginAccountPersistTimeoutForTest = 0
		pluginCredentialWriteHook = nil
		pluginGatewayHoldsSiteLock = nil
	})
	fx := newPluginRefreshFixture(t, "acme.example/write_timeout", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	giveUp := make(chan struct{})
	t.Cleanup(func() { close(giveUp) })
	pluginCredentialWriteHook = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-giveUp:
			return errors.New("persist write was still blocked when the test gave up")
		}
	}
	held := make(chan struct{})
	pluginGatewayHoldsSiteLock = func(siteID uuid.UUID) {
		if siteID == fx.siteID {
			close(held)
		}
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := fx.svc.RefreshPluginAccountForGateway(context.Background(), fx.siteID, fx.account.CredentialID, fx.siteType, nil)
		errCh <- err
	}()
	select {
	case <-held:
	case <-time.After(15 * time.Second):
		t.Fatal("gateway refresh did not take the site lock")
	}
	lockCh := make(chan struct{})
	go func() {
		unlock := fx.svc.siteLocks.lock(fx.siteID)
		unlock()
		close(lockCh)
	}()
	select {
	case <-lockCh:
	case <-time.After(2 * time.Second):
		t.Fatal("credential write did not return after the persist timeout; the site lock is still held")
	}
	err := waitErr(t, errCh, "timed out gateway write")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write error = %v, want deadline exceeded", err)
	}
	if got := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]; got != before {
		t.Fatalf("stored access token = %q, want %q after the timed-out write", got, before)
	}
}

func TestPluginCredentialWriteAbandonsStolenLease(t *testing.T) {
	t.Cleanup(func() { pluginCredentialWriteHook = nil })
	fx := newPluginRefreshFixture(t, "acme.example/lease_stolen", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	pluginCredentialWriteHook = func(context.Context) error {
		return fx.st.DB().Exec(`UPDATE site_credential_refresh_leases SET lease_id = ?`, "stolen-lease").Error
	}

	_, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "lease") {
		t.Fatalf("refresh error = %v, want the lost lease", err)
	}
	if got := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]; got != before {
		t.Fatalf("stored access token = %q, want %q after abandoning the stolen lease", got, before)
	}
}

func TestStartLoginFullPendingDoesNotCallRuntime(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/login_full", nil)
	fx.svc.pluginLogins.clear()
	for i := 0; i < pluginPendingLoginMax; i++ {
		if err := fx.svc.pluginLogins.rememberRegion("pending-"+uuid.NewString(), "cn"); err != nil {
			t.Fatal(err)
		}
	}
	fx.runtime.mu.Lock()
	before := fx.runtime.startCalls
	fx.runtime.mu.Unlock()

	_, err := fx.provider.StartLogin(context.Background(), fx.siteID, AccountLoginParams{Region: "intl"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "pending") {
		t.Fatalf("start login error = %v", err)
	}
	fx.runtime.mu.Lock()
	after := fx.runtime.startCalls
	fx.runtime.mu.Unlock()
	if after != before {
		t.Fatalf("runtime StartLogin calls = %d, want %d", after, before)
	}
	if len(fx.svc.pluginLogins.pending) != pluginPendingLoginMax {
		t.Fatalf("pending = %d, want %d", len(fx.svc.pluginLogins.pending), pluginPendingLoginMax)
	}
}

func TestProviderUnavailableHealthIsSkippedWithoutSnapshotID(t *testing.T) {
	st := openPluginAccountStore(t)
	svc := NewService(st, siteTestMasterKey)
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	item := createTypedSite(t, st, "acme.example/pool")

	health, err := svc.CheckSiteHealth(context.Background(), item.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Skipped {
		t.Fatal("provider unavailable health was not marked skipped")
	}
	if health.Snapshot.ID != uuid.Nil {
		t.Fatalf("snapshot id = %s, want nil", health.Snapshot.ID)
	}
	if health.SkipReason == "" || !strings.Contains(health.SkipReason, "unavailable") {
		t.Fatalf("skip reason = %q", health.SkipReason)
	}
	if health.Snapshot.Success {
		t.Fatal("skipped snapshot was marked successful")
	}
}

func markPluginAccountReauth(t *testing.T, fx *pluginRefreshFixture) {
	t.Helper()
	ctx := context.Background()
	repo := store.NewSiteCredentialRepository(fx.st.DB())
	row, err := repo.GetByID(ctx, fx.account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	meta := accountMetaMap(row.Meta)
	meta["reauth_required"] = true
	meta["enabled"] = false
	if _, err := updateCredentialMeta(ctx, repo, row.ID, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteAPIKeyStateRepository(fx.st.DB()).UpsertSyncResult(ctx, store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: row.ID,
		SiteID:           row.SiteID,
		Enabled:          false,
		SyncStatus:       "reauth_required",
		SyncMessage:      "reauthorization required",
		LastSyncedAt:     time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
}

func installCredentialMetaFailOnceTrigger(t *testing.T, fx *pluginRefreshFixture) {
	t.Helper()
	if err := fx.st.DB().Exec(`CREATE SEQUENCE meta_fail_once_seq`).Error; err != nil {
		t.Fatal(err)
	}
	if err := fx.st.DB().Exec(`
		CREATE OR REPLACE FUNCTION fail_credential_meta_write_once() RETURNS trigger AS $$
		BEGIN
		  IF NEW.meta IS DISTINCT FROM OLD.meta THEN
		    IF nextval('meta_fail_once_seq') = 1 THEN
		      RAISE EXCEPTION 'injected credential meta failure';
		    END IF;
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatal(err)
	}
	if err := fx.st.DB().Exec(`
		CREATE TRIGGER fail_credential_meta_write_once
		BEFORE UPDATE ON site_credentials
		FOR EACH ROW EXECUTE FUNCTION fail_credential_meta_write_once()`).Error; err != nil {
		t.Fatal(err)
	}
}

func pluginMetaDisablesModel(meta map[string]any, name string) bool {
	disabled, _ := meta["disabled_models"].([]any)
	for _, item := range disabled {
		if item == name {
			return true
		}
	}
	return false
}
