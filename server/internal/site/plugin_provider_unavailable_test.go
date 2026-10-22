package site

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

const (
	lifecyclePoolType = "acme.example/pool"
	lifecycleKeyType  = "acme.sample/chat"
)

func TestPluginProviderUnavailableSkipsRefreshAndHealth(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	svc := NewService(st, siteTestMasterKey)
	t.Cleanup(func() {
		ClearPluginSiteTypes()
		_ = svc.SetPluginAdapterModules(nil)
		_ = svc.SetPluginAccountRuntimes(nil)
	})
	installLifecyclePlugins(t, svc)

	poolSite := createTypedSite(t, st, lifecyclePoolType)
	keySite := createTypedSite(t, st, lifecycleKeyType)
	insertLifecycleAPIKey(t, svc, keySite.ID)

	if !SiteTypeAvailable(lifecyclePoolType) || !SiteTypeAvailable(lifecycleKeyType) {
		t.Fatal("plugin site types in the snapshot should stay routable")
	}
	assertLifecycleRefreshHealthy(t, svc, poolSite.ID)
	assertLifecycleRefreshHealthy(t, svc, keySite.ID)
	assertBulkRefreshRuns(t, svc, poolSite.ID, keySite.ID)

	hideLifecyclePlugins(t, svc)
	if SiteTypeAvailable(lifecyclePoolType) || SiteTypeAvailable(lifecycleKeyType) || !SiteTypeAvailable("grok") {
		t.Fatal("gateway availability should drop only the missing plugin types")
	}

	for _, id := range []uuid.UUID{poolSite.ID, keySite.ID} {
		assertProviderUnavailableRefresh(t, svc, st, id)
		assertProviderUnavailableHealth(t, svc, st, id)
	}
	assertBulkRefreshSkipsUnavailable(t, svc, poolSite.ID, keySite.ID)

	worker := NewSyncWorker(svc, nil, nil)
	if err := worker.runJob(ctx, store.SiteSyncJob{Kind: store.SiteSyncJobKindSiteRefresh, SiteID: poolSite.ID}); err != nil {
		t.Fatalf("sync worker refresh: %v", err)
	}
	assertSiteNotAutoDisabled(t, st, poolSite.ID)

	_, err := svc.RefreshSingleAPIKey(ctx, keySite.ID, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "unsupported site_type") {
		t.Fatalf("single api key refresh = %v, want unsupported site_type without disabling the site", err)
	}
	assertSiteNotAutoDisabled(t, st, keySite.ID)

	installLifecyclePlugins(t, svc)
	assertLifecycleRefreshHealthy(t, svc, poolSite.ID)
	assertLifecycleRefreshHealthy(t, svc, keySite.ID)

	for _, id := range []uuid.UUID{poolSite.ID, keySite.ID} {
		if _, err := store.NewSiteRepository(st.DB()).SetEnabledAndMeta(ctx, id, false, store.JSON(`{"auto_disabled_by_refresh":true}`)); err != nil {
			t.Fatal(err)
		}
		refreshed, err := svc.RefreshState(ctx, id)
		if err != nil {
			t.Fatalf("refresh after provider returns: %v", err)
		}
		if !refreshed.Site.Enabled || siteAutoDisabledByRefresh(refreshed.Site) {
			t.Fatalf("auto-disabled site was not restored: enabled=%v meta=%s", refreshed.Site.Enabled, refreshed.Site.Meta)
		}
		health, err := svc.CheckSiteHealth(ctx, id, "manual")
		if err != nil {
			t.Fatal(err)
		}
		if !health.Snapshot.Success || health.State.Status != "healthy" {
			t.Fatalf("health after provider returns success=%v status=%s type=%s message=%s", health.Snapshot.Success, health.State.Status, health.Snapshot.ErrorType.String, health.Snapshot.ErrorMessage.String)
		}
	}
}

func TestBuiltinUnknownSiteTypeStillAutoDisablesOnRefresh(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	svc := NewService(st, siteTestMasterKey)
	item := createTypedSite(t, st, "legacyvendor")

	_, err := svc.RefreshState(ctx, item.ID)
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type "legacyvendor"`) {
		t.Fatalf("refresh = %v, want unsupported site_type", err)
	}
	saved := mustSite(t, st, item.ID)
	if saved.Enabled || !siteAutoDisabledByRefresh(saved) {
		t.Fatalf("unknown built-in type enabled=%v meta=%s, want auto-disabled", saved.Enabled, saved.Meta)
	}
	health, err := svc.CheckSiteHealth(ctx, item.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if health.Snapshot.Success || health.Snapshot.ErrorType.String != "unsupported_site_type" {
		t.Fatalf("health success=%v type=%s message=%s", health.Snapshot.Success, health.Snapshot.ErrorType.String, health.Snapshot.ErrorMessage.String)
	}
}

func installLifecyclePlugins(t *testing.T, svc *Service) {
	t.Helper()
	if err := SetPluginSiteTypes([]PluginSiteType{
		{
			Descriptor: SiteTypeDescriptor{SiteType: lifecyclePoolType, DisplayName: "Acme Pool"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"api.example.com"},
			Pool:       pluginAccountTestSpec(),
		},
		{
			Descriptor: SiteTypeDescriptor{SiteType: lifecycleKeyType, DisplayName: "Acme Chat"},
			PluginID:   "acme.sample",
			Enabled:    true,
			Hosts:      []string{"api.example.com"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPluginAdapterModules([]adapter.Module{lifecycleAPIKeyModule{siteType: lifecycleKeyType}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{lifecyclePoolType: &scriptedPluginRuntime{}}); err != nil {
		t.Fatal(err)
	}
}

func hideLifecyclePlugins(t *testing.T, svc *Service) {
	t.Helper()
	ClearPluginSiteTypes()
	if err := svc.SetPluginAdapterModules(nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPluginAccountRuntimes(nil); err != nil {
		t.Fatal(err)
	}
}

func insertLifecycleAPIKey(t *testing.T, svc *Service, siteID uuid.UUID) {
	t.Helper()
	prepared := siteEncryptedCredential(t, uuid.Nil, siteID, "api_key", "sk-acme", store.JSON(`{"enabled":true,"name":"primary"}`))
	if _, err := store.NewSiteCredentialRepository(svc.db.DB()).Upsert(context.Background(), store.UpsertSiteCredentialParams{
		SiteID:          siteID,
		CredentialType:  prepared.CredentialType,
		EncryptedSecret: prepared.EncryptedSecret,
		MaskedSecret:    prepared.MaskedSecret,
		Meta:            prepared.Meta,
	}); err != nil {
		t.Fatal(err)
	}
}

func assertLifecycleRefreshHealthy(t *testing.T, svc *Service, siteID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	refreshed, err := svc.RefreshState(ctx, siteID)
	if err != nil {
		t.Fatalf("refresh %s: %v", siteID, err)
	}
	status := refreshed.State.SyncStatus
	message := refreshed.State.SyncMessage.String
	if !refreshed.Site.Enabled || siteAutoDisabledByRefresh(refreshed.Site) || (status != "synced" && status != "partial") || strings.Contains(message, "unsupported site_type") || strings.Contains(message, "provider plugin") {
		t.Fatalf("refresh enabled=%v status=%s message=%v meta=%s", refreshed.Site.Enabled, status, refreshed.State.SyncMessage, refreshed.Site.Meta)
	}
	health, err := svc.CheckSiteHealth(ctx, siteID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Snapshot.Success || health.State.Status != "healthy" || health.State.ConsecutiveFailures != 0 {
		t.Fatalf("health success=%v status=%s failures=%d type=%s message=%s", health.Snapshot.Success, health.State.Status, health.State.ConsecutiveFailures, health.Snapshot.ErrorType.String, health.Snapshot.ErrorMessage.String)
	}
}

func assertProviderUnavailableRefresh(t *testing.T, svc *Service, st *store.Store, siteID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	before := mustSite(t, st, siteID)
	refreshed, err := svc.RefreshState(ctx, siteID)
	if err != nil {
		t.Fatalf("refresh while provider is missing: %v", err)
	}
	if !refreshed.Site.Enabled || siteAutoDisabledByRefresh(refreshed.Site) || refreshed.Site.Enabled != before.Enabled {
		t.Fatalf("refresh changed site enabled=%v meta=%s", refreshed.Site.Enabled, refreshed.Site.Meta)
	}
	if refreshed.State.SyncStatus != "partial" || !refreshed.State.ValidationOK.Valid || !refreshed.State.ValidationOK.Bool {
		t.Fatalf("sync status=%s validation=%v message=%v", refreshed.State.SyncStatus, refreshed.State.ValidationOK, refreshed.State.SyncMessage)
	}
	message := refreshed.State.SyncMessage.String
	if !refreshed.State.SyncMessage.Valid || !strings.Contains(message, "unavailable") || !strings.Contains(message, "skipped") || strings.Contains(message, "unsupported site_type") {
		t.Fatalf("sync message = %q", message)
	}
	saved := mustSite(t, st, siteID)
	if !saved.Enabled || siteAutoDisabledByRefresh(saved) || string(saved.Meta) != string(before.Meta) {
		t.Fatalf("stored site enabled=%v meta=%s, before meta=%s", saved.Enabled, saved.Meta, before.Meta)
	}
	again, err := svc.RefreshState(ctx, siteID)
	if err != nil || !again.Site.Enabled || siteAutoDisabledByRefresh(again.Site) {
		t.Fatalf("second refresh err=%v enabled=%v meta=%s", err, again.Site.Enabled, again.Site.Meta)
	}
}

func assertProviderUnavailableHealth(t *testing.T, svc *Service, st *store.Store, siteID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	repo := store.NewHealthRepository(st.DB())
	beforeState, err := repo.GetSiteState(ctx, siteID)
	if err != nil {
		t.Fatal(err)
	}
	beforeSnaps, err := repo.ListSiteSnapshots(ctx, siteID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		health, err := svc.CheckSiteHealth(ctx, siteID, "manual")
		if err != nil {
			t.Fatalf("health %d: %v", i, err)
		}
		if health.Snapshot.Success || health.Snapshot.ErrorType.String != "provider_unavailable" || !strings.Contains(health.Snapshot.ErrorMessage.String, "unavailable") {
			t.Fatalf("health %d success=%v type=%s message=%s", i, health.Snapshot.Success, health.Snapshot.ErrorType.String, health.Snapshot.ErrorMessage.String)
		}
		if health.State.Status != "healthy" || health.State.ConsecutiveFailures != beforeState.ConsecutiveFailures {
			t.Fatalf("health %d state status=%s failures=%d, want healthy/%d", i, health.State.Status, health.State.ConsecutiveFailures, beforeState.ConsecutiveFailures)
		}
	}
	afterState, err := repo.GetSiteState(ctx, siteID)
	if err != nil {
		t.Fatal(err)
	}
	if afterState.Status != "healthy" || afterState.ConsecutiveFailures != beforeState.ConsecutiveFailures || afterState.UpdatedAt != beforeState.UpdatedAt {
		t.Fatalf("stored health changed status=%s failures=%d updated=%s, before status=%s failures=%d updated=%s", afterState.Status, afterState.ConsecutiveFailures, afterState.UpdatedAt, beforeState.Status, beforeState.ConsecutiveFailures, beforeState.UpdatedAt)
	}
	afterSnaps, err := repo.ListSiteSnapshots(ctx, siteID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterSnaps) != len(beforeSnaps) {
		t.Fatalf("snapshot count %d, want %d", len(afterSnaps), len(beforeSnaps))
	}
	for _, snap := range afterSnaps {
		if snap.ErrorType.String == "provider_unavailable" || snap.ErrorType.String == "unsupported_site_type" {
			t.Fatalf("stored failure snapshot type=%s message=%s", snap.ErrorType.String, snap.ErrorMessage.String)
		}
	}
	if n := activeSiteHealthCooldowns(t, st, siteID); n != 0 {
		t.Fatalf("site health cooldowns = %d, want 0", n)
	}
}

func assertBulkRefreshRuns(t *testing.T, svc *Service, ids ...uuid.UUID) {
	t.Helper()
	results, err := svc.RefreshAllStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := bulkResultsBySite(t, results)
	for _, id := range ids {
		item, ok := byID[id]
		if !ok || item.Skipped || item.Err != nil {
			t.Fatalf("bulk refresh %s skipped=%v err=%v", id, item.Skipped, item.Err)
		}
	}
}

func assertBulkRefreshSkipsUnavailable(t *testing.T, svc *Service, ids ...uuid.UUID) {
	t.Helper()
	results, err := svc.RefreshAllStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byID := bulkResultsBySite(t, results)
	for _, id := range ids {
		item, ok := byID[id]
		if !ok || !item.Skipped || item.Err != nil || !strings.Contains(item.SkipReason, "unavailable") || !strings.Contains(item.SkipReason, "skipped") {
			t.Fatalf("bulk refresh %s skipped=%v err=%v reason=%q", id, item.Skipped, item.Err, item.SkipReason)
		}
		if !item.Result.Site.Enabled || siteAutoDisabledByRefresh(item.Result.Site) {
			t.Fatalf("bulk refresh changed site enabled=%v meta=%s", item.Result.Site.Enabled, item.Result.Site.Meta)
		}
	}
}

func bulkResultsBySite(t *testing.T, results []RefreshBatchItem) map[uuid.UUID]RefreshBatchItem {
	t.Helper()
	byID := make(map[uuid.UUID]RefreshBatchItem, len(results))
	for _, item := range results {
		byID[item.Result.Site.ID] = item
	}
	return byID
}

func assertSiteNotAutoDisabled(t *testing.T, st *store.Store, siteID uuid.UUID) {
	t.Helper()
	item := mustSite(t, st, siteID)
	if !item.Enabled || siteAutoDisabledByRefresh(item) {
		t.Fatalf("site enabled=%v meta=%s", item.Enabled, item.Meta)
	}
}

func activeSiteHealthCooldowns(t *testing.T, st *store.Store, siteID uuid.UUID) int {
	t.Helper()
	items, err := store.NewRouteCooldownRepository(st.DB()).ListActive(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range items {
		if item.SiteID == siteID && item.Source == "site_health" && item.Scope == "site" {
			count++
		}
	}
	return count
}

type lifecycleAPIKeyModule struct {
	siteType string
}

func (m lifecycleAPIKeyModule) SiteTypes() []string { return []string{m.siteType} }

func (m lifecycleAPIKeyModule) ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	return []adapter.Model{{UpstreamName: "grok-2", DisplayName: "Grok 2"}}, nil
}

func (m lifecycleAPIKeyModule) Scope() adapter.HealthProbeScope { return adapter.HealthProbeSiteScope }

func (m lifecycleAPIKeyModule) ProbeHealth(ctx context.Context, site adapter.SiteConfig, credential string) ([]adapter.Model, error) {
	return m.ListModels(ctx, site, credential)
}

func TestPluginProviderUnavailableOnlyForMissingSlashTypes(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)

	if pluginProviderUnavailable("openai") || pluginProviderUnavailable("grok") || pluginProviderUnavailable("legacyvendor") || pluginProviderUnavailable("  ") {
		t.Fatal("slash-free types are not a missing plugin provider")
	}
	if !pluginProviderUnavailable("acme.example/pool") || !pluginProviderUnavailable(" ACME.SAMPLE/Chat ") {
		t.Fatal("slash types missing from the snapshot should be unavailable")
	}
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: lifecycleKeyType, DisplayName: "Acme Chat"},
		PluginID:   "acme.sample",
		Enabled:    false,
		Hosts:      []string{"api.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}
	if pluginProviderUnavailable(lifecycleKeyType) {
		t.Fatal("a type still present in the snapshot is not the missing-provider case")
	}
}
