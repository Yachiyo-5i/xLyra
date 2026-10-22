package site

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestPluginAccountVisibleLabel(t *testing.T) {
	spec := pluginAccountTestSpec()
	spec.Credential.Display = jsplugin.Sources("summary.label", "field.email")
	spec.Credential.Fields = append(spec.Credential.Fields, jsplugin.SiteProviderField{Name: "email"})
	api := pluginAccountAPI{pool: spec}
	token := "ACCESS-" + pluginAccountTokenMark + "-extra"
	fields := map[string]string{
		"access_token": token,
		"subject":      "user-1",
		"email":        "ada@example.com",
	}
	if got := api.visibleLabel(fields, "Ada"); got != "Ada" {
		t.Fatalf("display label = %q", got)
	}
	if got := api.visibleLabel(fields, ""); got != "ada@example.com" {
		t.Fatalf("display field = %q", got)
	}

	identityOnly := pluginAccountTestSpec()
	identityOnly.Credential.Fields = append(identityOnly.Credential.Fields, jsplugin.SiteProviderField{Name: "email"})
	api.pool = identityOnly
	if got := api.visibleLabel(fields, "Ada"); got != "user-1" {
		t.Fatalf("identity label = %q", got)
	}

	alias := pluginAccountTestSpec()
	alias.Credential.Identity = jsplugin.CredentialSources{}
	alias.Credential.Fields = append(alias.Credential.Fields, jsplugin.SiteProviderField{Name: "phone"})
	api.pool = alias
	phoneFields := map[string]string{"access_token": token, "phone": "13800138000"}
	if got := api.visibleLabel(phoneFields, ""); got != "13800138000" {
		t.Fatalf("alias label = %q", got)
	}

	masked := pluginAccountTestSpec()
	masked.Credential.Identity = jsplugin.CredentialSources{}
	api.pool = masked
	got := api.visibleLabel(map[string]string{"access_token": token}, "")
	if got == token || got == uuid.NewString() || !strings.Contains(got, "…") || strings.Contains(got, pluginAccountTokenMark) {
		t.Fatalf("truncated mask = %q", got)
	}
	if _, err := uuid.Parse(got); err == nil {
		t.Fatalf("truncated mask is a uuid: %q", got)
	}
}

func TestPluginAccountCredentialDisplayName(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	spec := pluginAccountTestSpec()
	spec.Credential.Display = jsplugin.Sources("summary.label")
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
	runtime := &fakePluginAccountRuntime{db: st.DB(), label: "13800138000"}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{pluginAccountTestType: runtime}); err != nil {
		t.Fatal(err)
	}
	provider, ok := svc.AccountProviderFor(pluginAccountTestType)
	if !ok {
		t.Fatal("account provider was not installed")
	}
	site := createTypedSite(t, st, pluginAccountTestType)
	started, err := provider.StartLogin(ctx, site.ID, AccountLoginParams{Region: "intl"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.PollLogin(ctx, site.ID, started.Session, nil); err != nil {
		t.Fatal(err)
	}
	completed, err := provider.PollLogin(ctx, site.ID, started.Session, nil)
	if err != nil || completed.Status != "complete" || completed.Account == nil {
		t.Fatalf("complete = %#v %v", completed, err)
	}
	account := pluginAccountFromAny(t, completed.Account)
	credential := mustCredential(t, st, account.CredentialID)
	state := accountCredentialState(ctx, st.DB(), credential.ID)
	assertAccountLabel(t, "login", credential, state, "13800138000")
	if strings.Contains(mustJSON(t, completed), pluginAccountTokenMark) {
		t.Fatal("login response contains a token")
	}

	idText := credential.ID.String()
	if _, err := store.NewSiteCredentialRepository(st.DB()).UpdateAccountLabel(ctx, credential.ID, idText, idText); err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteAPIKeyStateRepository(st.DB()).Upsert(ctx, store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: credential.ID,
		SiteID:           site.ID,
		Name:             idText,
		Enabled:          true,
		SyncStatus:       "synced",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Refresh(ctx, site.ID, credential.ID); err != nil {
		t.Fatal(err)
	}
	credential = mustCredential(t, st, credential.ID)
	state = accountCredentialState(ctx, st.DB(), credential.ID)
	assertAccountLabel(t, "backfill", credential, state, "13800138000")

	custom := "办公室"
	if _, err := store.NewSiteCredentialRepository(st.DB()).UpdateAccountLabel(ctx, credential.ID, custom, "13800138000"); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Refresh(ctx, site.ID, credential.ID); err != nil {
		t.Fatal(err)
	}
	credential = mustCredential(t, st, credential.ID)
	state = accountCredentialState(ctx, st.DB(), credential.ID)
	if credential.DisplayName != custom {
		t.Fatalf("custom display name = %q", credential.DisplayName)
	}
	if store.SiteCredentialDisplayName(credential, state) != custom {
		t.Fatalf("custom log name = %q", store.SiteCredentialDisplayName(credential, state))
	}
	if strings.Contains(credential.DisplayName, pluginAccountTokenMark) || strings.Contains(credential.MaskedSecret, pluginAccountTokenMark) || strings.Contains(state.Name, pluginAccountTokenMark) {
		t.Fatal("stored name contains a token")
	}
}

func assertAccountLabel(t *testing.T, step string, credential store.SiteCredential, state store.SiteAPIKeyState, want string) {
	t.Helper()
	if credential.DisplayName != want || credential.MaskedSecret != want || state.Name != want {
		t.Fatalf("%s display=%q mask=%q state=%q", step, credential.DisplayName, credential.MaskedSecret, state.Name)
	}
	if got := store.SiteCredentialDisplayName(credential, state); got != want {
		t.Fatalf("%s log name = %q", step, got)
	}
	if _, err := uuid.Parse(credential.DisplayName); err == nil {
		t.Fatalf("%s display name is a uuid", step)
	}
	if strings.Contains(credential.DisplayName, pluginAccountTokenMark) || strings.Contains(credential.MaskedSecret, pluginAccountTokenMark) || strings.Contains(state.Name, pluginAccountTokenMark) {
		t.Fatalf("%s stored name contains a token", step)
	}
}
