package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestPluginRefreshRepairCancelKeepsToken(t *testing.T) {
	t.Run("summarize observes cancel", func(t *testing.T) {
		assertCanceledRefreshKeepsToken(t, true)
	})
	t.Run("summarize ignores cancel", func(t *testing.T) {
		assertCanceledRefreshKeepsToken(t, false)
	})
}

func assertCanceledRefreshKeepsToken(t *testing.T, summarizeHonorsCancel bool) {
	t.Helper()
	fx := newPluginRefreshFixture(t, "acme.example/refresh_cancel", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	statusBefore := pluginSyncStatus(t, fx.st, fx.account.CredentialID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fx.runtime.holdNext()
	fx.runtime.mu.Lock()
	fx.runtime.summarizeHonorsCancel = summarizeHonorsCancel
	fx.runtime.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		_, err := fx.provider.Refresh(ctx, fx.siteID, fx.account.CredentialID)
		errCh <- err
	}()
	fx.runtime.waitEntered(t)
	cancel()
	fx.runtime.releaseHold()

	err := waitErr(t, errCh, "canceled refresh")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("refresh error = %v, want context canceled", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored == "" || stored == before {
		t.Fatalf("stored access token = %q, want the token issued after cancel", stored)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != statusBefore {
		t.Fatalf("sync status = %q, want %q after cancel", got, statusBefore)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] == true {
		t.Fatalf("cancel recorded reauth: %#v", meta)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairSummarizeFailureKeepsToken(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/refresh_sum", nil)
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
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != "stale" {
		t.Fatalf("sync status = %q, want stale", got)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] == true {
		t.Fatalf("summarize failure required reauth: %#v", meta)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairListModelsFailureKeepsToken(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/refresh_models", func(spec *jsplugin.AccountPoolSpec) {
		spec.ModelsPerAccount = true
	})
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	fx.runtime.mu.Lock()
	fx.runtime.listErr = errors.New("list models failed")
	fx.runtime.mu.Unlock()

	_, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
	if err == nil || !strings.Contains(err.Error(), "list models failed") {
		t.Fatalf("refresh error = %v", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored == "" || stored == before {
		t.Fatalf("stored access token = %q, want the rotated token", stored)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != "stale" {
		t.Fatalf("sync status = %q, want stale", got)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairProbeCancelKeepsToken(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/probe_cancel", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	statusBefore := pluginSyncStatus(t, fx.st, fx.account.CredentialID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fx.runtime.holdNext()

	type probeResult struct {
		status  string
		message string
		err     error
	}
	ch := make(chan probeResult, 1)
	go func() {
		row, err := store.NewSiteCredentialRepository(fx.st.DB()).GetByID(context.Background(), fx.account.CredentialID)
		if err != nil {
			ch <- probeResult{err: err}
			return
		}
		status, message, probeErr := fx.api.probeAccountHealth(ctx, row)
		ch <- probeResult{status: status, message: message, err: probeErr}
	}()
	fx.runtime.waitEntered(t)
	cancel()
	fx.runtime.releaseHold()

	select {
	case result := <-ch:
		if !errors.Is(result.err, context.Canceled) {
			t.Fatalf("probe = %q %q %v, want context canceled", result.status, result.message, result.err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("probe did not return after cancel")
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored == "" || stored == before {
		t.Fatalf("stored access token = %q, want the token issued before cancel", stored)
	}
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != statusBefore {
		t.Fatalf("sync status = %q, want %q after probe cancel", got, statusBefore)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairDoesNotOverwriteProbeToken(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/refresh_race", nil)
	release := fx.svc.siteLocks.lock(fx.siteID)
	var releaseOnce sync.Once
	unlock := func() { releaseOnce.Do(release) }
	defer unlock()

	leased := make(chan struct{})
	probeDone := make(chan struct{})
	var probeOnce sync.Once
	finishProbe := func() { probeOnce.Do(func() { close(probeDone) }) }
	defer finishProbe()
	pluginRefreshLeaseReleased = func(siteID uuid.UUID) {
		if siteID != fx.siteID {
			return
		}
		close(leased)
		<-probeDone
	}
	t.Cleanup(func() { pluginRefreshLeaseReleased = nil })

	errCh := make(chan error, 1)
	go func() {
		_, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
		errCh <- err
	}()
	select {
	case <-leased:
	case <-time.After(15 * time.Second):
		t.Fatal("refreshOne did not leave the lease while the site lock was held")
	}

	row, err := store.NewSiteCredentialRepository(fx.st.DB()).GetByID(context.Background(), fx.account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, probeErr := fx.api.probeAccountHealth(context.Background(), row); probeErr != nil {
		t.Fatalf("probe: %v", probeErr)
	}
	finishProbe()
	unlock()

	if err := waitErr(t, errCh, "refreshOne after probe"); err != nil {
		t.Fatal(err)
	}
	fx.runtime.mu.Lock()
	issued := append([]string(nil), fx.runtime.issued...)
	fx.runtime.mu.Unlock()
	if len(issued) < 3 {
		t.Fatalf("issued tokens = %d, want login, refreshOne, and probe", len(issued))
	}
	probeToken := issued[len(issued)-1]
	refreshToken := issued[len(issued)-2]
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored != probeToken || stored == refreshToken {
		t.Fatalf("stored %q, want probe token %q rather than refreshOne token %q", stored, probeToken, refreshToken)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairCredentialWriteIsAtomic(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/refresh_atomic", nil)
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
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	installCredentialMetaFailTrigger(t, fx)

	_, err = fx.provider.Refresh(ctx, fx.siteID, fx.account.CredentialID)
	if err == nil || !strings.Contains(err.Error(), "injected credential meta failure") {
		t.Fatalf("refresh error = %v, want injected meta failure", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored != before {
		t.Fatalf("stored access token = %q, want %q after meta write failure", stored, before)
	}
	got := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if got["reauth_required"] != true || got["enabled"] != false {
		t.Fatalf("meta = %#v, want the pre-refresh reauth flags", got)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairRejectsCredentialTypeChange(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/refresh_type", nil)
	before := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	fx.runtime.holdNext()
	errCh := make(chan error, 1)
	go func() {
		_, err := fx.provider.Refresh(context.Background(), fx.siteID, fx.account.CredentialID)
		errCh <- err
	}()
	fx.runtime.waitEntered(t)
	if _, err := store.NewSiteCredentialRepository(fx.st.DB()).UpdateCredentialType(context.Background(), fx.account.CredentialID, store.CredentialTypeAPIKey); err != nil {
		t.Fatal(err)
	}
	fx.runtime.releaseHold()

	err := waitErr(t, errCh, "refresh after type change")
	if !errors.Is(err, store.ErrCredentialTypeMismatch) || !strings.Contains(err.Error(), "credential type") {
		t.Fatalf("refresh error = %v, want a credential type mismatch", err)
	}
	stored := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
	if stored != before {
		t.Fatalf("stored access token = %q, want %q after the type change", stored, before)
	}
	row, err := store.NewSiteCredentialRepository(fx.st.DB()).GetByID(context.Background(), fx.account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if row.CredentialType != store.CredentialTypeAPIKey {
		t.Fatalf("credential type = %s, want api_key", row.CredentialType)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairModelMissingStillUpdatesMeta(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/model_missing", nil)
	account, err := fx.provider.SetModelEnabled(context.Background(), fx.siteID, fx.account.CredentialID, "acme-chat", false)
	if err != nil {
		t.Fatalf("missing model row: %v", err)
	}
	if account == nil {
		t.Fatal("missing account")
	}
	if !pluginMetaDisablesModel(pluginCredentialMeta(t, fx.st, fx.account.CredentialID), "acme-chat") {
		t.Fatal("disabled_models was not updated")
	}
}

func TestPluginRefreshRepairModelSyncFailureRollsBack(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/model_sync", nil)
	ctx := context.Background()
	siteModel := insertPluginSiteModel(t, fx, "acme-chat", "active")
	insertPluginAccountModel(t, fx, siteModel.ID, "acme-chat", true)
	installSiteModelStatusFailTrigger(t, fx)
	before, err := store.NewSiteCredentialRepository(fx.st.DB()).GetByID(ctx, fx.account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fx.provider.SetModelEnabled(ctx, fx.siteID, fx.account.CredentialID, "acme-chat", false)
	if err == nil || !strings.Contains(err.Error(), "injected site model status failure") {
		t.Fatalf("SetModelEnabled error = %v", err)
	}
	after, err := store.NewSiteCredentialRepository(fx.st.DB()).GetByID(ctx, fx.account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if string(after.Meta) != string(before.Meta) {
		t.Fatalf("meta changed from %s to %s", before.Meta, after.Meta)
	}
	if got := pluginAccountModelEnabled(t, fx, "acme-chat"); !got {
		t.Fatal("model row was disabled despite the failed status sync")
	}
	if got := pluginSiteModelStatus(t, fx, siteModel.ID); got != "active" {
		t.Fatalf("site model status = %q, want active", got)
	}
}

func TestPluginRefreshRepairModelEnablePersists(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/model_ok", nil)
	ctx := context.Background()
	siteModel := insertPluginSiteModel(t, fx, "acme-chat", "active")
	insertPluginAccountModel(t, fx, siteModel.ID, "acme-chat", true)

	account, err := fx.provider.SetModelEnabled(ctx, fx.siteID, fx.account.CredentialID, "acme-chat", false)
	if err != nil {
		t.Fatal(err)
	}
	if account == nil {
		t.Fatal("missing account")
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	disabled, _ := meta["disabled_models"].([]any)
	found := false
	for _, item := range disabled {
		if item == "acme-chat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("disabled_models = %#v", meta["disabled_models"])
	}
	if pluginAccountModelEnabled(t, fx, "acme-chat") {
		t.Fatal("model row stayed enabled")
	}
	if got := pluginSiteModelStatus(t, fx, siteModel.ID); got != "disabled" {
		t.Fatalf("site model status = %q, want disabled", got)
	}
}

func TestPluginRefreshRepairPendingRegionCap(t *testing.T) {
	results := newPluginLoginResults()
	now := time.Now()
	for i := 0; i < 1000; i++ {
		results.pending[fmt.Sprintf("expired-%d", i)] = pluginPendingLogin{region: "cn", expiresAt: now.Add(-time.Second)}
	}
	if err := results.rememberRegion("fresh", "intl"); err != nil {
		t.Fatal(err)
	}
	if len(results.pending) != 1 || results.region("fresh") != "intl" {
		t.Fatalf("pending after sweep = %d region %q", len(results.pending), results.region("fresh"))
	}

	results = newPluginLoginResults()
	for i := 0; i < pluginPendingLoginMax; i++ {
		if err := results.rememberRegion(fmt.Sprintf("session-%d", i), "cn"); err != nil {
			t.Fatalf("remember %d: %v", i, err)
		}
	}
	if err := results.rememberRegion("overflow", "intl"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "pending") {
		t.Fatalf("overflow error = %v", err)
	}
	if len(results.pending) != pluginPendingLoginMax {
		t.Fatalf("pending = %d, want %d", len(results.pending), pluginPendingLoginMax)
	}
	if results.region("overflow") != "" {
		t.Fatal("rejected login was stored")
	}
	for id, item := range results.pending {
		item.expiresAt = time.Now().Add(-time.Second)
		results.pending[id] = item
		break
	}
	if err := results.rememberRegion("after-sweep", "intl"); err != nil {
		t.Fatal(err)
	}
	if results.region("after-sweep") != "intl" || len(results.pending) != pluginPendingLoginMax {
		t.Fatalf("pending after one expiry = %d", len(results.pending))
	}
	results.clear()
	if len(results.pending) != 0 || len(results.items) != 0 {
		t.Fatalf("clear pending %d items %d", len(results.pending), len(results.items))
	}
}

func TestPluginRefreshRepairStartLoginRejectsFullPending(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/login_cap", nil)
	fx.svc.pluginLogins.clear()
	for i := 0; i < pluginPendingLoginMax; i++ {
		if err := fx.svc.pluginLogins.rememberRegion(fmt.Sprintf("session-%d", i), "cn"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := fx.provider.StartLogin(context.Background(), fx.siteID, AccountLoginParams{Region: "intl"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "pending") {
		t.Fatalf("start login error = %v", err)
	}
	if len(fx.svc.pluginLogins.pending) != pluginPendingLoginMax {
		t.Fatalf("pending = %d, want %d", len(fx.svc.pluginLogins.pending), pluginPendingLoginMax)
	}
}

func TestPluginRefreshRepairPathsDoNotDeadlock(t *testing.T) {
	fx := newPluginRefreshFixture(t, "acme.example/refresh_lock", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gatewayHeld := make(chan struct{})
	allowGateway := make(chan struct{})
	var gatewayOnce sync.Once
	leaseReleased := make(chan struct{})
	var leaseOnce sync.Once
	pluginGatewayHoldsSiteLock = func(siteID uuid.UUID) {
		if siteID != fx.siteID {
			return
		}
		gatewayOnce.Do(func() { close(gatewayHeld) })
		select {
		case <-allowGateway:
		case <-ctx.Done():
		}
	}
	pluginRefreshLeaseReleased = func(siteID uuid.UUID) {
		if siteID != fx.siteID {
			return
		}
		leaseOnce.Do(func() { close(leaseReleased) })
	}
	t.Cleanup(func() {
		pluginGatewayHoldsSiteLock = nil
		pluginRefreshLeaseReleased = nil
	})

	errCh := make(chan error, 3)
	go func() {
		_, err := fx.svc.RefreshPluginAccountForGateway(ctx, fx.siteID, fx.account.CredentialID, fx.siteType, nil)
		errCh <- err
	}()
	select {
	case <-gatewayHeld:
	case <-ctx.Done():
		t.Fatal("gateway refresh did not take the site lock")
	}
	go func() {
		_, err := fx.provider.RefreshAll(ctx, fx.siteID)
		errCh <- err
	}()
	select {
	case <-leaseReleased:
	case <-ctx.Done():
		t.Fatal("RefreshAll did not leave the credential lease")
	}
	go func() {
		row, err := store.NewSiteCredentialRepository(fx.st.DB()).GetByID(context.Background(), fx.account.CredentialID)
		if err != nil {
			errCh <- err
			return
		}
		_, _, probeErr := fx.api.probeAccountHealth(ctx, row)
		errCh <- probeErr
	}()
	close(allowGateway)
	for i := 0; i < 3; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("refresh path: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("refresh paths deadlocked")
		}
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

func TestPluginRefreshRepairGatewayCancelSkipsFailure(t *testing.T) {
	t.Run("canceled error on a live context", func(t *testing.T) {
		fx := newPluginRefreshFixture(t, "acme.example/gw_cancel", nil)
		statusBefore := pluginSyncStatus(t, fx.st, fx.account.CredentialID)
		tokenBefore := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
		fx.runtime.mu.Lock()
		fx.runtime.returnCancel = true
		fx.runtime.mu.Unlock()

		_, err := fx.svc.RefreshPluginAccountForGateway(context.Background(), fx.siteID, fx.account.CredentialID, fx.siteType, nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("gateway refresh error = %v", err)
		}
		assertGatewayCancelKeptAccount(t, fx, statusBefore, tokenBefore)
	})
	t.Run("parent context canceled during refresh", func(t *testing.T) {
		fx := newPluginRefreshFixture(t, "acme.example/gw_parent", nil)
		statusBefore := pluginSyncStatus(t, fx.st, fx.account.CredentialID)
		tokenBefore := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fx.runtime.holdNext()
		fx.runtime.mu.Lock()
		fx.runtime.honorCancel = true
		fx.runtime.mu.Unlock()

		errCh := make(chan error, 1)
		go func() {
			_, err := fx.svc.RefreshPluginAccountForGateway(ctx, fx.siteID, fx.account.CredentialID, fx.siteType, nil)
			errCh <- err
		}()
		fx.runtime.waitEntered(t)
		cancel()
		fx.runtime.releaseHold()
		err := waitErr(t, errCh, "canceled gateway refresh")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("gateway refresh error = %v", err)
		}
		assertGatewayCancelKeptAccount(t, fx, statusBefore, tokenBefore)
	})
}

func assertGatewayCancelKeptAccount(t *testing.T, fx *pluginRefreshFixture, statusBefore, tokenBefore string) {
	t.Helper()
	if got := pluginSyncStatus(t, fx.st, fx.account.CredentialID); got != statusBefore {
		t.Fatalf("sync status = %q, want %q", got, statusBefore)
	}
	meta := pluginCredentialMeta(t, fx.st, fx.account.CredentialID)
	if meta["reauth_required"] == true || meta["enabled"] == false {
		t.Fatalf("cancel changed account meta: %#v", meta)
	}
	if got := decryptPluginCredential(t, fx.svc, fx.account.CredentialID)["access_token"]; got != tokenBefore {
		t.Fatalf("stored access token = %q, want %q", got, tokenBefore)
	}
	assertPluginLeaseCount(t, fx.st, 0)
}

type pluginRefreshFixture struct {
	st       *store.Store
	svc      *Service
	provider AccountProvider
	api      pluginAccountAPI
	siteID   uuid.UUID
	siteType string
	account  PluginAccount
	runtime  *refreshRepairRuntime
}

func newPluginRefreshFixture(t *testing.T, siteType string, mutate func(*jsplugin.AccountPoolSpec)) *pluginRefreshFixture {
	t.Helper()
	pluginRefreshLeaseReleased = nil
	pluginGatewayHoldsSiteLock = nil
	st := openPluginAccountStore(t)
	ctx := context.Background()
	spec := pluginAccountTestSpec()
	spec.Credential.Fields = append(spec.Credential.Fields, jsplugin.SiteProviderField{Name: "expires_at", Type: "unix"})
	spec.Credential.ExpiresAtField = "expires_at"
	lead := 300
	spec.Refresh.LeadSeconds = &lead
	if mutate != nil {
		mutate(spec)
	}
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	t.Cleanup(func() {
		pluginRefreshLeaseReleased = nil
		pluginGatewayHoldsSiteLock = nil
	})
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: siteType, DisplayName: "Acme", CredentialKey: siteType, ShowInCreateDialog: true},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.cn", "api.example.com"},
		Pool:       spec,
	}}); err != nil {
		t.Fatal(err)
	}
	runtime := &refreshRepairRuntime{}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{siteType: runtime}); err != nil {
		t.Fatal(err)
	}
	provider, ok := svc.AccountProviderFor(siteType)
	if !ok {
		t.Fatal("account provider was not installed")
	}
	api, ok := provider.(pluginAccountAPI)
	if !ok {
		t.Fatal("account provider type")
	}
	created, err := store.NewSiteRepository(st.DB()).Create(ctx, store.CreateSiteParams{
		Name: "Acme", Slug: "acme-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType: siteType, BaseURL: "https://api.example.com", Status: "active", Enabled: true, RoutingPriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := provider.StartLogin(ctx, created.ID, AccountLoginParams{Region: "intl"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.PollLogin(ctx, created.ID, started.Session, nil); err != nil {
		t.Fatal(err)
	}
	completed, err := provider.PollLogin(ctx, created.ID, started.Session, nil)
	if err != nil {
		t.Fatal(err)
	}
	account := pluginAccountFromAny(t, completed.Account)
	agePluginCredential(t, svc, account.CredentialID, time.Now().Add(10*time.Second))
	return &pluginRefreshFixture{
		st: st, svc: svc, provider: provider, api: api,
		siteID: created.ID, siteType: siteType, account: account, runtime: runtime,
	}
}

func installCredentialMetaFailTrigger(t *testing.T, fx *pluginRefreshFixture) {
	t.Helper()
	if err := fx.st.DB().Exec(`
		CREATE OR REPLACE FUNCTION fail_credential_meta_write() RETURNS trigger AS $$
		BEGIN
		  IF NEW.meta IS DISTINCT FROM OLD.meta THEN
		    RAISE EXCEPTION 'injected credential meta failure';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatal(err)
	}
	if err := fx.st.DB().Exec(`
		CREATE TRIGGER fail_credential_meta_write
		BEFORE UPDATE ON site_credentials
		FOR EACH ROW EXECUTE FUNCTION fail_credential_meta_write()`).Error; err != nil {
		t.Fatal(err)
	}
}

func installSiteModelStatusFailTrigger(t *testing.T, fx *pluginRefreshFixture) {
	t.Helper()
	if err := fx.st.DB().Exec(`
		CREATE OR REPLACE FUNCTION fail_site_model_status() RETURNS trigger AS $$
		BEGIN
		  IF NEW.status IS DISTINCT FROM OLD.status THEN
		    RAISE EXCEPTION 'injected site model status failure';
		  END IF;
		  RETURN NEW;
		END;
		$$ LANGUAGE plpgsql`).Error; err != nil {
		t.Fatal(err)
	}
	if err := fx.st.DB().Exec(`
		CREATE TRIGGER fail_site_model_status
		BEFORE UPDATE ON site_models
		FOR EACH ROW EXECUTE FUNCTION fail_site_model_status()`).Error; err != nil {
		t.Fatal(err)
	}
}

func insertPluginSiteModel(t *testing.T, fx *pluginRefreshFixture, name, status string) store.SiteModel {
	t.Helper()
	model, err := store.NewSiteModelRepository(fx.st.DB()).Upsert(context.Background(), store.UpsertSiteModelParams{
		SiteID:       fx.siteID,
		UpstreamName: name,
		DisplayName:  name,
		Capabilities: store.JSON(`{}`),
		Status:       status,
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func insertPluginAccountModel(t *testing.T, fx *pluginRefreshFixture, siteModelID uuid.UUID, name string, enabled bool) {
	t.Helper()
	if _, err := store.NewSiteAPIKeyModelRepository(fx.st.DB()).Upsert(context.Background(), store.UpsertSiteAPIKeyModelParams{
		SiteID:            fx.siteID,
		SiteCredentialID:  fx.account.CredentialID,
		SiteModelID:       siteModelID,
		UpstreamModelName: name,
		DisplayName:       name,
		Available:         true,
		Enabled:           enabled,
		Raw:               store.JSON(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
}

func pluginAccountModelEnabled(t *testing.T, fx *pluginRefreshFixture, name string) bool {
	t.Helper()
	models, err := store.NewSiteAPIKeyModelRepository(fx.st.DB()).ListBySite(context.Background(), fx.siteID)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		if model.UpstreamModelName == name && model.SiteCredentialID == fx.account.CredentialID {
			return model.Enabled
		}
	}
	t.Fatalf("model %s was not stored", name)
	return false
}

func pluginSiteModelStatus(t *testing.T, fx *pluginRefreshFixture, id uuid.UUID) string {
	t.Helper()
	model, err := store.NewSiteModelRepository(fx.st.DB()).GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return model.Status
}

func pluginSyncStatus(t *testing.T, st *store.Store, credentialID uuid.UUID) string {
	t.Helper()
	return accountCredentialState(context.Background(), st.DB(), credentialID).SyncStatus
}

func pluginCredentialMeta(t *testing.T, st *store.Store, credentialID uuid.UUID) map[string]any {
	t.Helper()
	row, err := store.NewSiteCredentialRepository(st.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(row.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	return meta
}

func waitErr(t *testing.T, errCh <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(15 * time.Second):
		t.Fatalf("%s did not finish", what)
		return nil
	}
}

type refreshRepairRuntime struct {
	mu                    sync.Mutex
	calls                 int
	polls                 int
	issued                []string
	hold                  bool
	entered               chan struct{}
	release               chan struct{}
	returnCancel          bool
	honorCancel           bool
	summarizeErr          error
	summarizeHonorsCancel bool
	listErr               error
	startCalls            int
	refreshPublic         map[string]string
}

func (r *refreshRepairRuntime) holdNext() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hold = true
	r.entered = make(chan struct{})
	r.release = make(chan struct{})
}

func (r *refreshRepairRuntime) waitEntered(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	entered := r.entered
	r.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("refresh did not reach the upstream call")
	}
}

func (r *refreshRepairRuntime) releaseHold() {
	r.mu.Lock()
	release := r.release
	r.mu.Unlock()
	close(release)
}

func (r *refreshRepairRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	r.mu.Lock()
	r.startCalls++
	r.mu.Unlock()
	return PluginLoginStart{
		ID: "sess-repair", Kind: accountLoginCustomPoll, Status: "pending",
		UserCode: "CODE-1", VerificationURL: "https://api.example.com/activate", Interval: 2,
	}, nil
}

func (r *refreshRepairRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.polls++
	if r.polls == 1 {
		return PluginLoginPoll{Status: "pending", Interval: 2}, nil
	}
	return PluginLoginPoll{
		Status: "complete", Identity: "user-1",
		Credential: map[string]string{
			"access_token": pluginAccountAccessSeed, "refresh_token": pluginAccountRefreshTok,
			"subject": "user-1", "realm": "intl",
			"expires_at": strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10),
		},
	}, nil
}

func (r *refreshRepairRuntime) Refresh(ctx context.Context, fields map[string]string) (map[string]string, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	hold := r.hold
	entered := r.entered
	release := r.release
	returnCancel := r.returnCancel
	honorCancel := r.honorCancel
	if hold {
		r.hold = false
	}
	r.mu.Unlock()
	if hold {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	}
	if returnCancel {
		return nil, context.Canceled
	}
	if honorCancel && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	token := fmt.Sprintf("ACCESS-%s-r%d", pluginAccountTokenMark, call)
	r.mu.Lock()
	r.issued = append(r.issued, token)
	extra := cloneStringMap(r.refreshPublic)
	r.mu.Unlock()
	out := cloneStringMap(fields)
	out["access_token"] = token
	for key, value := range extra {
		out[key] = value
	}
	return out, nil
}

func (r *refreshRepairRuntime) Summarize(ctx context.Context, _ string, _ map[string]string) (PluginAccountSummary, error) {
	r.mu.Lock()
	err := r.summarizeErr
	honor := r.summarizeHonorsCancel
	r.mu.Unlock()
	if honor && ctx.Err() != nil {
		return PluginAccountSummary{}, ctx.Err()
	}
	if err != nil {
		return PluginAccountSummary{}, err
	}
	remaining := 12.0
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok", Remark: "pro",
		Entries: []QuotaProbeEntry{{Label: "credits", Remaining: &remaining, Limit: &limit, Unit: "credits"}},
	}, nil
}

func (r *refreshRepairRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	r.mu.Lock()
	err := r.listErr
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (r *refreshRepairRuntime) ParseImport([]byte) ([]PluginImportedAccount, error) {
	return nil, nil
}
