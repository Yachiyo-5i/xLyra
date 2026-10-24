package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

const (
	pluginAccountTestType   = "acme.example/p38pool"
	pluginAccountTokenMark  = "TOKEN-plugin-9f3c"
	pluginAccountAccessSeed = "ACCESS-TOKEN-plugin-9f3c"
	pluginAccountRefreshTok = "REFRESH-TOKEN-plugin-9f3c"
)

func TestPluginAccountLoginRefreshImportDelete(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	spec := pluginAccountTestSpec()
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{
			SiteType:           pluginAccountTestType,
			DisplayName:        "P38",
			CredentialKey:      pluginAccountTestType,
			ShowInCreateDialog: true,
		},
		PluginID: "acme.example",
		Enabled:  true,
		Hosts:    []string{"api.example.cn", "api.example.com"},
		Pool:     spec,
	}}); err != nil {
		t.Fatal(err)
	}

	runtime := &fakePluginAccountRuntime{db: st.DB()}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{
		pluginAccountTestType: runtime,
	}); err != nil {
		t.Fatal(err)
	}
	provider, ok := svc.AccountProviderFor(pluginAccountTestType)
	if !ok || provider == nil {
		t.Fatal("account provider was not installed")
	}

	created, err := store.NewSiteRepository(st.DB()).Create(ctx, store.CreateSiteParams{
		Name:            "P38",
		Slug:            "p38-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType:        pluginAccountTestType,
		BaseURL:         "https://api.example.cn",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	siteID := created.ID

	regions, defaultRegion, err := provider.Regions(ctx, siteID)
	if err != nil || defaultRegion != "cn" || sliceLen(regions) != 2 {
		t.Fatalf("regions = %#v %q %v", regions, defaultRegion, err)
	}

	started, err := provider.StartLogin(ctx, siteID, AccountLoginParams{Region: "intl"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Kind != accountLoginCustomPoll || started.Session == "" || started.AuthURL != "https://api.example.com/activate" || started.Region != "intl" || started.UserCode != "CODE-1" || started.Interval != 2 {
		t.Fatalf("start = %#v", started)
	}
	assertNoPluginSecret(t, "login start", mustJSON(t, started))

	pending, err := provider.PollLogin(ctx, siteID, started.Session, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "pending" || pending.Account != nil {
		t.Fatalf("pending poll = %#v", pending)
	}
	assertNoPluginSecret(t, "pending poll", mustJSON(t, pending))

	completed, err := provider.PollLogin(ctx, siteID, started.Session, nil)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "complete" {
		t.Fatalf("complete poll = %#v", completed)
	}
	account := pluginAccountFromAny(t, completed.Account)
	if account.Identity != "user-1" || account.Region != "intl" || account.MaskedToken != "user-1" || account.Remark != "pro" || account.Status != "ok" || account.SyncStatus != "synced" || !account.Enabled {
		t.Fatalf("logged-in account = %#v", account)
	}
	if account.Fields["subject"] != "user-1" || account.Fields["realm"] != "intl" {
		t.Fatalf("public fields = %#v", account.Fields)
	}
	if _, secret := account.Fields["access_token"]; secret {
		t.Fatal("account fields include access_token")
	}
	if account.RemainingQuota == nil || *account.RemainingQuota != 12 || account.QuotaLimit == nil || *account.QuotaLimit != 20 || account.QuotaUnit != "credits" || len(account.Entries) != 1 {
		t.Fatalf("summary = remark %q status %q remain %v limit %v unit %q entries %#v", account.Remark, account.Status, account.RemainingQuota, account.QuotaLimit, account.QuotaUnit, account.Entries)
	}
	assertNoPluginSecret(t, "complete poll", mustJSON(t, completed))
	if !pluginAccountContains(runtime.bases, "https://api.example.com") {
		t.Fatalf("summarize bases = %#v", runtime.bases)
	}

	listed := listPluginAccounts(t, provider, siteID)
	if len(listed) != 1 || listed[0].CredentialID != account.CredentialID {
		t.Fatalf("list after login = %#v", listed)
	}
	assertNoPluginSecret(t, "list after login", mustJSON(t, listed))
	assertPluginCredentialStored(t, svc, siteID, account.CredentialID, "user-1", "intl")

	refreshedAny, err := provider.Refresh(ctx, siteID, account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := pluginAccountFromAny(t, refreshedAny)
	if refreshed.CredentialID != account.CredentialID || refreshed.Remark != "pro" || refreshed.Identity != "user-1" {
		t.Fatalf("refresh = %#v", refreshed)
	}
	assertNoPluginSecret(t, "refresh", mustJSON(t, refreshed))
	assertPluginCredentialStored(t, svc, siteID, account.CredentialID, "user-1", "intl")
	assertPluginLeaseCount(t, st, 0)
	if leases := runtime.leaseSamples; len(leases) == 0 || !allEqual(leases, 1) {
		t.Fatalf("lease held during refresh = %#v, want 1 for every call", leases)
	}

	importedAny, err := provider.Import(ctx, siteID, []AccountImportFile{{
		Name: "accounts.json",
		Content: `[
			{"subject":"user-1","realm":"intl","access_token":"ACCESS-` + pluginAccountTokenMark + `-imp","refresh_token":"REFRESH-` + pluginAccountTokenMark + `-imp"},
			{"subject":"user-2","realm":"cn","access_token":"ACCESS-` + pluginAccountTokenMark + `-u2","refresh_token":"REFRESH-` + pluginAccountTokenMark + `-u2"}
		]`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	imported, ok := importedAny.([]PluginAccountImportItem)
	if !ok || len(imported) != 2 {
		t.Fatalf("import = %#v", importedAny)
	}
	importedCount := 0
	var user2ID uuid.UUID
	for _, item := range imported {
		if item.Status != "imported" || item.Error != "" || item.Warning != "" || item.CredentialID == "" {
			t.Fatalf("import item = %#v", item)
		}
		importedCount++
		if item.Label == "user-2" {
			user2ID = uuid.MustParse(item.CredentialID)
		}
	}
	if importedCount != 2 || user2ID == uuid.Nil {
		t.Fatalf("import items = %#v", imported)
	}
	assertNoPluginSecret(t, "import", mustJSON(t, imported))
	listed = listPluginAccounts(t, provider, siteID)
	if len(listed) != 2 {
		t.Fatalf("list after import = %d %#v", len(listed), listed)
	}
	assertNoPluginSecret(t, "list after import", mustJSON(t, listed))
	sameIdentity := 0
	for _, item := range listed {
		if item.Identity == "user-1" {
			sameIdentity++
			if item.CredentialID != account.CredentialID {
				t.Fatalf("dedup created %s, want %s", item.CredentialID, account.CredentialID)
			}
		}
	}
	if sameIdentity != 1 {
		t.Fatalf("user-1 rows = %d", sameIdentity)
	}

	runtime.mu.Lock()
	runtime.failNext = true
	runtime.mu.Unlock()
	_, refreshErr := provider.Refresh(ctx, siteID, user2ID)
	if refreshErr == nil || strings.Contains(refreshErr.Error(), pluginAccountTokenMark) || !strings.Contains(refreshErr.Error(), "[redacted]") {
		t.Fatalf("scrubbed refresh error = %v", refreshErr)
	}
	if errors.Is(refreshErr, accountauth.ErrReauthorizationRequired) {
		t.Fatal("scrubbed error still wraps the reauth sentinel")
	}
	listed = listPluginAccounts(t, provider, siteID)
	assertNoPluginSecret(t, "list after reauth", mustJSON(t, listed))
	var reauth PluginAccount
	for _, item := range listed {
		if item.CredentialID == user2ID {
			reauth = item
		}
	}
	if !reauth.ReauthRequired || reauth.Enabled || reauth.SyncStatus != "reauth_required" || !strings.Contains(reauth.SyncMessage, "[redacted]") {
		t.Fatalf("reauth account = %#v", reauth)
	}

	for _, id := range []uuid.UUID{account.CredentialID, user2ID} {
		if err := provider.Delete(ctx, siteID, id); err != nil {
			t.Fatal(err)
		}
	}
	listed = listPluginAccounts(t, provider, siteID)
	if len(listed) != 0 {
		t.Fatalf("list after delete = %#v", listed)
	}
	assertPluginLeaseCount(t, st, 0)
}

type fakePluginAccountRuntime struct {
	mu           sync.Mutex
	db           *gorm.DB
	polls        int
	rotations    int
	failNext     bool
	label        string
	bases        []string
	leaseSamples []int64
}

func (f *fakePluginAccountRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	return PluginLoginStart{
		ID:              "sess-p38",
		Kind:            accountLoginCustomPoll,
		Status:          "pending",
		UserCode:        "CODE-1",
		VerificationURL: "https://api.example.com/activate",
		Interval:        2,
	}, nil
}

func (f *fakePluginAccountRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	if f.polls == 1 {
		return PluginLoginPoll{Status: "pending", Interval: 2}, nil
	}
	return PluginLoginPoll{
		Status:   "complete",
		Identity: "user-1",
		Credential: map[string]string{
			"access_token":  pluginAccountAccessSeed,
			"refresh_token": pluginAccountRefreshTok,
			"subject":       "user-1",
			"realm":         "intl",
		},
	}, nil
}

func (f *fakePluginAccountRuntime) Refresh(ctx context.Context, fields map[string]string) (map[string]string, error) {
	var held int64
	err := f.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM site_credential_refresh_leases`).Scan(&held).Error
	f.mu.Lock()
	defer f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	f.leaseSamples = append(f.leaseSamples, held)
	if f.failNext {
		f.failNext = false
		return nil, fmt.Errorf("upstream rejected %s: %w", fields["access_token"], accountauth.ErrReauthorizationRequired)
	}
	f.rotations++
	out := map[string]string{}
	for key, value := range fields {
		out[key] = value
	}
	out["access_token"] = fmt.Sprintf("ACCESS-%s-%d", pluginAccountTokenMark, f.rotations)
	return out, nil
}

func (f *fakePluginAccountRuntime) Summarize(_ context.Context, baseURL string, _ map[string]string) (PluginAccountSummary, error) {
	f.mu.Lock()
	f.bases = append(f.bases, baseURL)
	label := f.label
	f.mu.Unlock()
	remaining := 12.4
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok",
		Remark: "pro",
		Label:  label,
		Entries: []QuotaProbeEntry{{
			Label:     "credits",
			Remaining: &remaining,
			Limit:     &limit,
			Unit:      "credits",
		}},
	}, nil
}

func (f *fakePluginAccountRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	return nil, nil
}

func (f *fakePluginAccountRuntime) ParseImport(raw []byte) ([]PluginImportedAccount, error) {
	var rows []map[string]string
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, errors.New("account import file is not valid JSON")
	}
	out := make([]PluginImportedAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, PluginImportedAccount{Fields: row})
	}
	return out, nil
}

func pluginAccountTestSpec() *jsplugin.AccountPoolSpec {
	return &jsplugin.AccountPoolSpec{
		Credential: jsplugin.SiteProviderCredential{
			Model: "account_pool",
			Fields: []jsplugin.SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "refresh_token", Secret: true},
				{Name: "subject"},
				{Name: "realm"},
			},
			AccessTokenField:  "access_token",
			RefreshTokenField: "refresh_token",
			Identity:          jsplugin.Sources("field.subject"),
		},
		Regions: []jsplugin.SiteProviderRegion{
			{ID: "cn", Label: jsplugin.LocalizedText{"zh": "中国", "en": "China"}, BaseURL: "https://api.example.cn"},
			{ID: "intl", Label: jsplugin.LocalizedText{"zh": "国际", "en": "International"}, BaseURL: "https://api.example.com", Realms: []string{"intl"}},
		},
		DefaultRegion:  "cn",
		Login:          jsplugin.SiteProviderLogin{Kind: "custom_poll"},
		Refresh:        jsplugin.SiteProviderRefresh{Kind: "custom_steps"},
		Import:         jsplugin.SiteProviderImport{Enabled: true},
		QuotaAggregate: "sum",
		UI: jsplugin.SiteProviderUI{
			RowPills:   []jsplugin.SiteProviderRowPill{{Field: "realm"}},
			Operations: []string{"add", "refresh", "delete", "import"},
		},
	}
}

func listPluginAccounts(t *testing.T, provider AccountProvider, siteID uuid.UUID) []PluginAccount {
	t.Helper()
	items, count, err := provider.List(context.Background(), siteID)
	if err != nil {
		t.Fatal(err)
	}
	accounts, ok := items.([]PluginAccount)
	if !ok || len(accounts) != count {
		t.Fatalf("list = %#v count %d", items, count)
	}
	return accounts
}

func pluginAccountFromAny(t *testing.T, value any) PluginAccount {
	t.Helper()
	account, ok := value.(PluginAccount)
	if !ok {
		t.Fatalf("account type = %T", value)
	}
	return account
}

func assertPluginCredentialStored(t *testing.T, svc *Service, siteID, credentialID uuid.UUID, identity, region string) {
	t.Helper()
	credential, err := store.NewSiteCredentialRepository(svc.db.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if credential.SiteID != siteID || !strings.HasPrefix(credential.CredentialType, store.CredentialPrefixAccount+pluginAccountTestType+":") {
		t.Fatalf("credential type = %s", credential.CredentialType)
	}
	if _, err := uuid.Parse(strings.TrimPrefix(credential.CredentialType, store.CredentialPrefixAccount+pluginAccountTestType+":")); err != nil {
		t.Fatalf("credential id suffix: %v (%s)", err, credential.CredentialType)
	}
	if strings.Contains(credential.EncryptedSecret, pluginAccountTokenMark) || strings.Contains(credential.MaskedSecret, pluginAccountTokenMark) || strings.Contains(string(credential.Meta), pluginAccountTokenMark) {
		t.Fatal("credential row contains a token")
	}
	var meta map[string]any
	if err := json.Unmarshal(credential.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if _, ok := meta["access_token"]; ok {
		t.Fatal("meta stores access_token")
	}
	if _, ok := meta["refresh_token"]; ok {
		t.Fatal("meta stores refresh_token")
	}
	if meta["identity"] != identity || meta["region"] != region || meta["subject"] != identity {
		t.Fatalf("meta = %#v", meta)
	}
	plain, err := svc.credentials.Decrypt(credential.EncryptedSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain, pluginAccountTokenMark) || !strings.Contains(plain, "access_token") {
		t.Fatal("decrypted package is missing the access token")
	}
}

func assertPluginLeaseCount(t *testing.T, st *store.Store, want int64) {
	t.Helper()
	var got int64
	if err := st.DB().Raw(`SELECT COUNT(*) FROM site_credential_refresh_leases`).Scan(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("refresh leases = %d, want %d", got, want)
	}
}

func assertNoPluginSecret(t *testing.T, label, body string) {
	t.Helper()
	if strings.Contains(body, pluginAccountTokenMark) || strings.Contains(body, "access_token") || strings.Contains(body, "refresh_token") || strings.Contains(body, "encrypted_secret") {
		t.Fatalf("%s leaked a secret: %s", label, body)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func pluginAccountContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func allEqual(items []int64, want int64) bool {
	for _, item := range items {
		if item != want {
			return false
		}
	}
	return true
}

func openPluginAccountStore(t *testing.T) *store.Store {
	t.Helper()
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMaxConns:       4,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           pluginAccountEnv("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           pluginAccountEnv("DB_NAME", "xlyra"),
		DBUser:           pluginAccountEnv("DB_USER", "postgres"),
		DBPassword:       pluginAccountEnv("DB_PASSWORD", "postgres"),
		DBSSLMode:        pluginAccountEnv("DB_SSLMODE", "disable"),
	}
	if value := strings.TrimSpace(os.Getenv("DB_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			t.Skipf("invalid DB_PORT %q", value)
		}
		cfg.DBPort = port
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %v", err)
	}
	schema := "xlyra_site_p38_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := base.DB().Exec(fmt.Sprintf("CREATE SCHEMA %q", schema)).Error; err != nil {
		base.Close()
		t.Skipf("cannot create a test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = base.DB().Exec(fmt.Sprintf("DROP SCHEMA %q CASCADE", schema)).Error
		base.Close()
	})
	parsed, err := url.Parse(cfg.DatabaseDSN())
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	cfg.PostgresDSN = parsed.String()
	var migrateErr error
	for attempt := 0; attempt < 5; attempt++ {
		if migrateErr = store.EnsureDatabaseInitialized(ctx, cfg); migrateErr == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if migrateErr != nil {
		t.Fatalf("migrate the test schema: %v", migrateErr)
	}
	st, err := store.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open the test schema: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func pluginAccountEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
