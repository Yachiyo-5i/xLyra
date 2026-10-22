package site

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestPluginAccountRefreshHealthAndDelete(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: pluginAccountTestType, DisplayName: "P38"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.cn", "api.example.com"},
		Pool:       pluginAccountTestSpec(),
	}}); err != nil {
		t.Fatal(err)
	}
	runtime := &scriptedPluginRuntime{}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{pluginAccountTestType: runtime}); err != nil {
		t.Fatal(err)
	}
	site := createTypedSite(t, st, pluginAccountTestType)
	live := insertPluginAccount(t, svc, site.ID, "live-token", "live")
	dead := insertPluginAccount(t, svc, site.ID, "dead-token", "dead")
	if _, err := store.NewSiteModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteModelParams{
		SiteID: site.ID, UpstreamName: "plugin-model", DisplayName: "Plugin", Status: "available",
	}); err != nil {
		t.Fatal(err)
	}

	refreshed, err := svc.RefreshState(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.State.APIKeyCount != 2 || refreshed.State.ModelCount != 1 || refreshed.State.SyncStatus != "partial" || !strings.Contains(refreshed.State.SyncMessage.String, "models.perAccount is not enabled") {
		t.Fatalf("refresh state count=%d models=%d status=%s message=%v", refreshed.State.APIKeyCount, refreshed.State.ModelCount, refreshed.State.SyncStatus, refreshed.State.SyncMessage)
	}
	if runtime.refreshCalls < 2 || runtime.summarizeCalls < 2 {
		t.Fatalf("refresh calls=%d summarize=%d", runtime.refreshCalls, runtime.summarizeCalls)
	}
	liveState := accountCredentialState(ctx, st.DB(), live.ID)
	if liveState.SyncStatus != "synced" || !liveState.RemainQuota.Valid || liveState.RemainQuota.Int64 != 12 {
		t.Fatalf("live summary = %#v", liveState)
	}

	runtime.setFailDead(true)
	health, err := svc.CheckSiteHealth(ctx, site.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Snapshot.Success || health.Snapshot.Endpoint != "summarizeAccount" {
		t.Fatalf("health success=%v endpoint=%s message=%v", health.Snapshot.Success, health.Snapshot.Endpoint, health.Snapshot.ErrorMessage)
	}
	if health.Snapshot.ErrorMessage.Valid && health.Snapshot.ErrorMessage.String != "ok (1/2 accounts healthy)" {
		t.Fatalf("health message = %v", health.Snapshot.ErrorMessage)
	}
	deadState := accountCredentialState(ctx, st.DB(), dead.ID)
	deadCred := mustCredential(t, st, dead.ID)
	var deadMeta map[string]any
	if err := json.Unmarshal(deadCred.Meta, &deadMeta); err != nil {
		t.Fatal(err)
	}
	if deadState.Enabled || deadState.SyncStatus != "reauth_required" || deadMeta["reauth_required"] != true || deadMeta["enabled"] != false {
		t.Fatalf("dead enabled=%v status=%s meta=%s", deadState.Enabled, deadState.SyncStatus, deadCred.Meta)
	}
	if strings.Contains(deadState.SyncMessage.String, "dead-token") || strings.Contains(string(health.Snapshot.Metadata), "dead-token") {
		t.Fatal("health recorded a token")
	}

	provider, ok := svc.AccountProviderFor(pluginAccountTestType)
	if !ok {
		t.Fatal("missing provider")
	}
	enabled := false
	if _, err := provider.Update(ctx, site.ID, live.ID, &enabled); err != nil {
		t.Fatal(err)
	}
	afterToggle, err := store.NewSiteStateRepository(st.DB()).GetBySite(ctx, site.ID)
	if err != nil || afterToggle.APIKeyCount != 2 || afterToggle.ModelCount != 1 {
		t.Fatalf("toggle counts = %d %d %v", afterToggle.APIKeyCount, afterToggle.ModelCount, err)
	}

	model, err := store.NewSiteModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteModelParams{
		SiteID: site.ID, UpstreamName: "bound-model", DisplayName: "Bound", Status: "available",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteAPIKeyModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
		SiteID: site.ID, SiteCredentialID: live.ID, SiteModelID: model.ID,
		UpstreamModelName: "bound-model", Available: true, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := provider.Delete(ctx, site.ID, live.ID); err != nil {
		t.Fatal(err)
	}
	bindings, err := store.NewSiteAPIKeyModelRepository(st.DB()).ListByCredential(ctx, live.ID)
	if err != nil || len(bindings) != 0 {
		t.Fatalf("bindings after delete = %#v %v", bindings, err)
	}
	if _, err := store.NewSiteCredentialRepository(st.DB()).GetByID(ctx, live.ID); err == nil {
		t.Fatal("deleted credential still exists")
	}
	afterDelete, err := store.NewSiteStateRepository(st.DB()).GetBySite(ctx, site.ID)
	if err != nil || afterDelete.APIKeyCount != 1 {
		t.Fatalf("count after delete = %d %v", afterDelete.APIKeyCount, err)
	}

	other := createTypedSite(t, st, pluginAccountTestType)
	otherCred := insertPluginAccount(t, svc, other.ID, "site-token", "site-user")
	otherModel, err := store.NewSiteModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteModelParams{
		SiteID: other.ID, UpstreamName: "gone", DisplayName: "Gone", Status: "available",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteAPIKeyModelRepository(st.DB()).Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
		SiteID: other.ID, SiteCredentialID: otherCred.ID, SiteModelID: otherModel.ID,
		UpstreamModelName: "gone", Available: true, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	left, err := store.NewSiteAPIKeyModelRepository(st.DB()).ListByCredential(ctx, otherCred.ID)
	if err != nil || len(left) != 0 {
		t.Fatalf("site delete left bindings %#v %v", left, err)
	}
	if _, err := store.NewSiteCredentialRepository(st.DB()).GetByID(ctx, otherCred.ID); err == nil {
		t.Fatal("site delete left the credential")
	}
}

type scriptedPluginRuntime struct {
	mu             sync.Mutex
	refreshCalls   int
	summarizeCalls int
	failDead       bool
	failAll        bool
	label          string
}

func (s *scriptedPluginRuntime) setFailDead(fail bool) {
	s.mu.Lock()
	s.failDead = fail
	s.mu.Unlock()
}

func (s *scriptedPluginRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	return PluginLoginStart{}, errors.New("login is unused")
}

func (s *scriptedPluginRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	return PluginLoginPoll{}, errors.New("login is unused")
}

func (s *scriptedPluginRuntime) Refresh(_ context.Context, fields map[string]string) (map[string]string, error) {
	s.mu.Lock()
	s.refreshCalls++
	s.mu.Unlock()
	out := map[string]string{}
	for key, value := range fields {
		out[key] = value
	}
	return out, nil
}

func (s *scriptedPluginRuntime) Summarize(_ context.Context, _ string, fields map[string]string) (PluginAccountSummary, error) {
	s.mu.Lock()
	s.summarizeCalls++
	failDead := s.failDead
	failAll := s.failAll
	label := s.label
	s.mu.Unlock()
	if failAll {
		return PluginAccountSummary{}, errors.New("invalid api key")
	}
	if failDead && fields["subject"] == "dead" {
		return PluginAccountSummary{}, accountauth.ErrReauthorizationRequired
	}
	remaining := 12.4
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok",
		Label:  label,
		Entries: []QuotaProbeEntry{{
			Label: "credits", Remaining: &remaining, Limit: &limit, Unit: "credits",
		}},
	}, nil
}

func (s *scriptedPluginRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	return nil, nil
}

func (s *scriptedPluginRuntime) ParseImport([]byte) ([]PluginImportedAccount, error) {
	return nil, errors.New("import is unused")
}

func createTypedSite(t *testing.T, st *store.Store, siteType string) store.Site {
	t.Helper()
	created, err := store.NewSiteRepository(st.DB()).Create(context.Background(), store.CreateSiteParams{
		Name:            "Pool",
		Slug:            "cv-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType:        siteType,
		BaseURL:         "https://api.example.com",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func insertPluginAccount(t *testing.T, svc *Service, siteID uuid.UUID, access, subject string) store.SiteCredential {
	t.Helper()
	encoded, err := encodePluginCredential(map[string]string{
		"access_token": access, "refresh_token": "refresh-" + subject, "subject": subject, "realm": "intl",
	})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, masked, err := svc.credentials.Encrypt(encoded)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{"enabled": true, "subject": subject, "realm": "intl"})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := store.NewSiteCredentialRepository(svc.db.DB()).Upsert(context.Background(), store.UpsertSiteCredentialParams{
		SiteID:          siteID,
		CredentialType:  store.NewAccountCredentialType(pluginAccountTestType, uuid.New()),
		EncryptedSecret: encrypted,
		MaskedSecret:    masked,
		Meta:            meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteAPIKeyStateRepository(svc.db.DB()).Upsert(context.Background(), store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: cred.ID,
		SiteID:           siteID,
		Name:             subject,
		Enabled:          true,
		SyncStatus:       "synced",
	}); err != nil {
		t.Fatal(err)
	}
	return cred
}

func mustSite(t *testing.T, st *store.Store, id uuid.UUID) store.Site {
	t.Helper()
	site, err := store.NewSiteRepository(st.DB()).GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return site
}

func mustCredential(t *testing.T, st *store.Store, id uuid.UUID) store.SiteCredential {
	t.Helper()
	cred, err := store.NewSiteCredentialRepository(st.DB()).GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return cred
}

func TestPluginAccountEmptyPoolStaysNormal(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: pluginAccountTestType, DisplayName: "P38"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.com"},
		Pool:       pluginAccountTestSpec(),
	}}); err != nil {
		t.Fatal(err)
	}
	runtime := &scriptedPluginRuntime{}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{pluginAccountTestType: runtime}); err != nil {
		t.Fatal(err)
	}
	site := createTypedSite(t, st, pluginAccountTestType)

	refreshed, err := svc.RefreshState(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed.State.ValidationOK.Valid || !refreshed.State.ValidationOK.Bool || refreshed.State.SyncStatus != "synced" || refreshed.State.SyncMessage.Valid {
		t.Fatalf("empty refresh status=%s validation=%v message=%v", refreshed.State.SyncStatus, refreshed.State.ValidationOK, refreshed.State.SyncMessage)
	}
	if !refreshed.Site.Enabled || refreshed.Site.Status != "active" {
		t.Fatalf("empty site enabled=%v status=%s", refreshed.Site.Enabled, refreshed.Site.Status)
	}
	health, err := svc.CheckSiteHealth(ctx, site.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Snapshot.Success || (health.Snapshot.ErrorType.Valid && health.Snapshot.ErrorType.String != "") {
		t.Fatalf("empty health success=%v type=%v message=%v", health.Snapshot.Success, health.Snapshot.ErrorType, health.Snapshot.ErrorMessage)
	}
	snapshots, err := store.NewHealthRepository(st.DB()).ListSiteSnapshots(ctx, site.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) == 0 {
		t.Fatal("empty pool wrote no health snapshot")
	}
	for _, snapshot := range snapshots {
		if !snapshot.Success {
			t.Fatalf("empty pool failure snapshot: %#v", snapshot)
		}
	}

	live := insertPluginAccount(t, svc, site.ID, "live-token", "live")
	refreshed, err = svc.RefreshState(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed.State.ValidationOK.Bool || refreshed.State.SyncStatus != "partial" || !strings.Contains(refreshed.State.SyncMessage.String, "models.perAccount is not enabled") {
		t.Fatalf("account refresh status=%s validation=%v message=%v", refreshed.State.SyncStatus, refreshed.State.ValidationOK, refreshed.State.SyncMessage)
	}
	health, err = svc.CheckSiteHealth(ctx, site.ID, "manual")
	if err != nil || !health.Snapshot.Success || !strings.Contains(health.Snapshot.ErrorMessage.String, "ok (1/1 accounts healthy)") {
		t.Fatalf("account health success=%v message=%v err=%v", health.Snapshot.Success, health.Snapshot.ErrorMessage, err)
	}
	if accountCredentialState(ctx, st.DB(), live.ID).SyncStatus != "synced" {
		t.Fatal("added account did not sync")
	}

	failedRuntime := &scriptedPluginRuntime{failAll: true}
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{pluginAccountTestType: failedRuntime}); err != nil {
		t.Fatal(err)
	}
	failedSite := createTypedSite(t, st, pluginAccountTestType)
	insertPluginAccount(t, svc, failedSite.ID, "bad-token", "bad")
	_, err = svc.RefreshState(ctx, failedSite.ID)
	if err == nil || !strings.Contains(err.Error(), "enabled accounts failed") {
		t.Fatalf("all-failed refresh = %v", err)
	}
	failedState, err := store.NewSiteStateRepository(st.DB()).GetBySite(ctx, failedSite.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failedState.ValidationOK.Bool || failedState.SyncStatus != "failed" {
		t.Fatalf("all-failed state status=%s validation=%v", failedState.SyncStatus, failedState.ValidationOK)
	}
	failedHealth, err := svc.CheckSiteHealth(ctx, failedSite.ID, "manual")
	if err != nil || failedHealth.Snapshot.Success {
		t.Fatalf("all-failed health success=%v err=%v", failedHealth.Snapshot.Success, err)
	}
}
