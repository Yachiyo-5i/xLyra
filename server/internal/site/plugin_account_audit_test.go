package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestPluginAccountRefreshFailureKeepsUsage(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	spec := pluginAccountTestSpec()
	spec.Selection = "quota_weighted"
	runtime := &modeRuntime{}
	svc := newAuditPluginService(t, st, spec, runtime)
	site := createTypedSite(t, st, pluginAccountTestType)
	live := insertPluginAccount(t, svc, site.ID, "live-token", "live")
	other := insertPluginAccount(t, svc, site.ID, "other-token", "other")

	if _, err := svc.RefreshState(ctx, site.ID); err != nil {
		t.Fatal(err)
	}
	if got := usageRemaining(t, accountCredentialState(ctx, st.DB(), live.ID).Usage); got != 12 {
		t.Fatalf("live remaining after refresh = %v", got)
	}
	if got := siteQuotaRemaining(t, st, site.ID); got != 24 {
		t.Fatalf("quota summary after refresh = %v", got)
	}

	runtime.setFailure("live", false)
	if _, err := svc.RefreshState(ctx, site.ID); err != nil {
		t.Fatal(err)
	}
	liveState := accountCredentialState(ctx, st.DB(), live.ID)
	if !liveState.Enabled || liveState.SyncStatus != "stale" || usageRemaining(t, liveState.Usage) != 12 {
		t.Fatalf("summarize failure kept enabled=%v status=%s usage=%s", liveState.Enabled, liveState.SyncStatus, liveState.Usage)
	}
	if usageRemaining(t, accountCredentialState(ctx, st.DB(), other.ID).Usage) != 12 {
		t.Fatal("other account lost its usage")
	}
	if got := siteQuotaRemaining(t, st, site.ID); got != 24 {
		t.Fatalf("quota summary dropped the failed account: %v", got)
	}

	runtime.setFailure("live", true)
	if _, err := svc.RefreshState(ctx, site.ID); err != nil {
		t.Fatal(err)
	}
	liveState = accountCredentialState(ctx, st.DB(), live.ID)
	if liveState.Enabled || liveState.SyncStatus != "reauth_required" || usageRemaining(t, liveState.Usage) != 12 {
		t.Fatalf("reauth failure enabled=%v status=%s usage=%s", liveState.Enabled, liveState.SyncStatus, liveState.Usage)
	}
	if got := siteQuotaRemaining(t, st, site.ID); got != 12 {
		t.Fatalf("quota summary after reauth = %v, want the other account only", got)
	}
}

func TestPluginAccountFailureMessagesCountAccounts(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	runtime := &modeRuntime{}
	svc := newAuditPluginService(t, st, pluginAccountTestSpec(), runtime)
	site := createTypedSite(t, st, pluginAccountTestType)
	cred := insertPluginAccount(t, svc, site.ID, "live-token", "live")
	if _, err := svc.RefreshState(ctx, site.ID); err != nil {
		t.Fatal(err)
	}
	runtime.setFailure("live", false)
	_, err := svc.RefreshState(ctx, site.ID)
	if err == nil || !strings.Contains(err.Error(), "enabled accounts failed") || strings.Contains(err.Error(), "api keys") {
		t.Fatalf("refresh error = %v", err)
	}
	if usageRemaining(t, accountCredentialState(ctx, st.DB(), cred.ID).Usage) != 12 {
		t.Fatal("all-account failure cleared usage")
	}

	spec := pluginAccountTestSpec()
	spec.ModelsPerAccount = true
	listRuntime := &modeRuntime{failListAfter: 1}
	svc = newAuditPluginService(t, st, spec, listRuntime)
	listed := createTypedSite(t, st, pluginAccountTestType)
	listedCred := insertPluginAccount(t, svc, listed.ID, "list-token", "listed")
	refreshed, err := svc.RefreshState(ctx, listed.ID)
	if err != nil {
		t.Fatal(err)
	}
	message := refreshed.State.SyncMessage.String
	if !strings.Contains(message, "enabled accounts failed") || strings.Contains(message, "api keys") {
		t.Fatalf("model sync message = %q", message)
	}
	if usageRemaining(t, accountCredentialState(ctx, st.DB(), listedCred.ID).Usage) != 12 {
		t.Fatal("listModels failure cleared usage")
	}

	partialRuntime := &modeRuntime{}
	partialSpec := pluginAccountTestSpec()
	partialSpec.ModelsPerAccount = true
	svc = newAuditPluginService(t, st, partialSpec, partialRuntime)
	partial := createTypedSite(t, st, pluginAccountTestType)
	insertPluginAccount(t, svc, partial.ID, "ok-token", "ok")
	insertPluginAccount(t, svc, partial.ID, "bad-token", "bad")
	partialRuntime.setFailure("bad", false)
	refreshed, err = svc.RefreshState(ctx, partial.ID)
	if err != nil {
		t.Fatal(err)
	}
	message = refreshed.State.SyncMessage.String
	if refreshed.State.SyncStatus != "partial" || !strings.Contains(message, "1 of 2 accounts failed") || strings.Contains(message, "api keys") {
		t.Fatalf("partial refresh message = %q status=%s", message, refreshed.State.SyncStatus)
	}
}

func TestPluginAccountCancelDoesNotWritePartialState(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()

	t.Run("health", func(t *testing.T) {
		runtime := &gatedRuntime{}
		svc := newAuditPluginService(t, st, pluginAccountTestSpec(), runtime)
		site := createTypedSite(t, st, pluginAccountTestType)
		ids := seedAuditAccounts(t, svc, site.ID, 5)
		if _, err := svc.RefreshState(ctx, site.ID); err != nil {
			t.Fatal(err)
		}
		before := snapshotAuditAccounts(t, st, ids)
		runtime.arm()
		checkCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() {
			_, err := svc.CheckSiteHealth(checkCtx, site.ID, "manual")
			errCh <- err
		}()
		waitAudit(t, runtime.started(), "health probes to block")
		cancel()
		if err := waitAuditErr(t, errCh, "health check"); err != nil {
			t.Fatal(err)
		}
		assertAuditUnchanged(t, st, before)
	})

	t.Run("refresh", func(t *testing.T) {
		runtime := &gatedRuntime{}
		svc := newAuditPluginService(t, st, pluginAccountTestSpec(), runtime)
		site := createTypedSite(t, st, pluginAccountTestType)
		ids := seedAuditAccounts(t, svc, site.ID, 5)
		if _, err := svc.RefreshState(ctx, site.ID); err != nil {
			t.Fatal(err)
		}
		before := snapshotAuditAccounts(t, st, ids)
		runtime.arm()
		checkCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() {
			_, err := svc.RefreshState(checkCtx, site.ID)
			errCh <- err
		}()
		waitAudit(t, runtime.started(), "account refresh to block")
		cancel()
		err := waitAuditErr(t, errCh, "site refresh")
		if err == nil || !strings.Contains(err.Error(), "plugin account refresh timed out") {
			t.Fatalf("refresh error = %v", err)
		}
		assertAuditUnchanged(t, st, before)
	})
}

func newAuditPluginService(t *testing.T, st *store.Store, spec *jsplugin.AccountPoolSpec, runtime PluginAccountRuntime) *Service {
	t.Helper()
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: pluginAccountTestType, DisplayName: "P38"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.cn", "api.example.com"},
		Pool:       spec,
	}}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{pluginAccountTestType: runtime}); err != nil {
		t.Fatal(err)
	}
	return svc
}

type modeRuntime struct {
	mu            sync.Mutex
	failSubject   string
	reauth        bool
	failListAfter int
	listCalls     int
}

func (r *modeRuntime) setFailure(subject string, reauth bool) {
	r.mu.Lock()
	r.failSubject = subject
	r.reauth = reauth
	r.mu.Unlock()
}

func (r *modeRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	return PluginLoginStart{}, errors.New("login is unused")
}

func (r *modeRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	return PluginLoginPoll{}, errors.New("login is unused")
}

func (r *modeRuntime) Refresh(_ context.Context, fields map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range fields {
		out[key] = value
	}
	return out, nil
}

func (r *modeRuntime) Summarize(_ context.Context, _ string, fields map[string]string) (PluginAccountSummary, error) {
	r.mu.Lock()
	subject := r.failSubject
	reauth := r.reauth
	r.mu.Unlock()
	if fields["subject"] == subject && subject != "" {
		if reauth {
			return PluginAccountSummary{}, accountauth.ErrReauthorizationRequired
		}
		return PluginAccountSummary{}, errors.New("summarize failed")
	}
	remaining := 12.0
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok",
		Entries: []QuotaProbeEntry{{
			Label: "credits", Remaining: &remaining, Limit: &limit, Unit: "credits",
		}},
	}, nil
}

func (r *modeRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	r.mu.Lock()
	r.listCalls++
	fail := r.failListAfter > 0 && r.listCalls > r.failListAfter
	r.mu.Unlock()
	if fail {
		return nil, errors.New("list models failed")
	}
	return []adapter.Model{{UpstreamName: "plugin-model", DisplayName: "Plugin"}}, nil
}

func (r *modeRuntime) ParseImport([]byte) ([]PluginImportedAccount, error) {
	return nil, errors.New("import is unused")
}

type gatedRuntime struct {
	mu      sync.Mutex
	block   bool
	hits    int
	entered chan struct{}
}

func (g *gatedRuntime) arm() {
	g.mu.Lock()
	g.block = true
	g.hits = 0
	g.entered = make(chan struct{})
	g.mu.Unlock()
}

func (g *gatedRuntime) started() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.entered
}

func (g *gatedRuntime) blockOn(ctx context.Context) error {
	g.mu.Lock()
	block := g.block
	if block {
		g.hits++
		if g.hits == pluginAccountWorkers {
			close(g.entered)
		}
	}
	g.mu.Unlock()
	if !block {
		return nil
	}
	<-ctx.Done()
	return errors.New("upstream closed")
}

func (g *gatedRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	return PluginLoginStart{}, errors.New("login is unused")
}

func (g *gatedRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	return PluginLoginPoll{}, errors.New("login is unused")
}

func (g *gatedRuntime) Refresh(ctx context.Context, fields map[string]string) (map[string]string, error) {
	if err := g.blockOn(ctx); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for key, value := range fields {
		out[key] = value
	}
	return out, nil
}

func (g *gatedRuntime) Summarize(ctx context.Context, _ string, _ map[string]string) (PluginAccountSummary, error) {
	if err := g.blockOn(ctx); err != nil {
		return PluginAccountSummary{}, err
	}
	remaining := 12.0
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok",
		Entries: []QuotaProbeEntry{{
			Label: "credits", Remaining: &remaining, Limit: &limit, Unit: "credits",
		}},
	}, nil
}

func (g *gatedRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	return nil, nil
}

func (g *gatedRuntime) ParseImport([]byte) ([]PluginImportedAccount, error) {
	return nil, errors.New("import is unused")
}

type auditAccountRow struct {
	secret  string
	meta    string
	usage   string
	status  string
	enabled bool
}

func seedAuditAccounts(t *testing.T, svc *Service, siteID uuid.UUID, count int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, count)
	for i := 0; i < count; i++ {
		cred := insertPluginAccount(t, svc, siteID, fmt.Sprintf("token-%d", i), fmt.Sprintf("user-%d", i))
		ids = append(ids, cred.ID)
	}
	return ids
}

func snapshotAuditAccounts(t *testing.T, st *store.Store, ids []uuid.UUID) map[uuid.UUID]auditAccountRow {
	t.Helper()
	ctx := context.Background()
	out := make(map[uuid.UUID]auditAccountRow, len(ids))
	for _, id := range ids {
		cred := mustCredential(t, st, id)
		state := accountCredentialState(ctx, st.DB(), id)
		out[id] = auditAccountRow{
			secret:  cred.EncryptedSecret,
			meta:    string(cred.Meta),
			usage:   string(state.Usage),
			status:  state.SyncStatus,
			enabled: state.Enabled,
		}
	}
	return out
}

func assertAuditUnchanged(t *testing.T, st *store.Store, before map[uuid.UUID]auditAccountRow) {
	t.Helper()
	ctx := context.Background()
	for id, want := range before {
		cred := mustCredential(t, st, id)
		state := accountCredentialState(ctx, st.DB(), id)
		if cred.EncryptedSecret != want.secret || string(cred.Meta) != want.meta || string(state.Usage) != want.usage || state.SyncStatus != want.status || state.Enabled != want.enabled {
			t.Fatalf("account %s changed secret=%v meta=%v usage=%q status=%s enabled=%v", id, cred.EncryptedSecret != want.secret, string(cred.Meta) != want.meta, state.Usage, state.SyncStatus, state.Enabled)
		}
	}
}

func usageRemaining(t *testing.T, raw store.JSON) float64 {
	t.Helper()
	view := pluginUsageView(raw)
	if len(view.Entries) != 1 || view.Entries[0].Remaining == nil {
		t.Fatalf("usage = %s", raw)
	}
	return *view.Entries[0].Remaining
}

func siteQuotaRemaining(t *testing.T, st *store.Store, siteID uuid.UUID) float64 {
	t.Helper()
	var meta map[string]any
	if err := json.Unmarshal(mustSite(t, st, siteID).Meta, &meta); err != nil {
		t.Fatal(err)
	}
	summary, _ := meta["quota_probe_summary"].(map[string]any)
	value, ok := summary["remaining_min"].(float64)
	if !ok {
		t.Fatalf("quota summary = %#v", summary)
	}
	return value
}

func waitAudit(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitAuditErr(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}
