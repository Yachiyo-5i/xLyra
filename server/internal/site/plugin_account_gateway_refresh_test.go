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

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func TestRefreshPluginAccountForGateway(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	const siteType = "acme.example/p38gw"
	lead := 300
	spec := pluginAccountTestSpec()
	spec.Credential.Fields = append(spec.Credential.Fields, jsplugin.SiteProviderField{Name: "expires_at", Type: "unix"})
	spec.Credential.ExpiresAtField = "expires_at"
	spec.Refresh.LeadSeconds = &lead
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: siteType, DisplayName: "P38", CredentialKey: siteType, ShowInCreateDialog: true},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.cn", "api.example.com"},
		Pool:       spec,
	}}); err != nil {
		t.Fatal(err)
	}
	runtime := &gatewayRefreshRuntime{}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{siteType: runtime}); err != nil {
		t.Fatal(err)
	}
	provider, ok := svc.AccountProviderFor(siteType)
	if !ok {
		t.Fatal("account provider was not installed")
	}
	created, err := store.NewSiteRepository(st.DB()).Create(ctx, store.CreateSiteParams{
		Name: "P38-gw", Slug: "p38gw-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		SiteType: siteType, BaseURL: "https://api.example.cn", Status: "active", Enabled: true, RoutingPriority: 1,
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

	runtime.mu.Lock()
	baseline := runtime.calls
	runtime.hold = true
	runtime.entered = make(chan struct{})
	runtime.release = make(chan struct{})
	runtime.mu.Unlock()

	errCh := make(chan error, 2)
	var first, second map[string]string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		fields, refreshErr := svc.RefreshPluginAccountForGateway(ctx, created.ID, account.CredentialID, siteType, nil)
		first = fields
		errCh <- refreshErr
	}()
	select {
	case <-runtime.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("gateway refresh did not take the lease")
	}
	go func() {
		defer wg.Done()
		fields, refreshErr := svc.RefreshPluginAccountForGateway(ctx, created.ID, account.CredentialID, siteType, nil)
		second = fields
		errCh <- refreshErr
	}()
	close(runtime.release)
	wg.Wait()
	close(errCh)
	for refreshErr := range errCh {
		if refreshErr != nil {
			if strings.Contains(refreshErr.Error(), pluginAccountTokenMark) {
				t.Fatal("gateway refresh error contains a token")
			}
			t.Fatal(refreshErr)
		}
	}
	runtime.mu.Lock()
	refreshes := runtime.calls - baseline
	runtime.hold = false
	runtime.mu.Unlock()
	if refreshes != 1 {
		t.Fatalf("runtime refreshes under one lease = %d, want 1", refreshes)
	}
	if first["access_token"] == "" || first["access_token"] != second["access_token"] {
		t.Fatal("lease follower did not reuse the stored refresh")
	}
	stored := decryptPluginCredential(t, svc, account.CredentialID)
	if stored["access_token"] != first["access_token"] {
		t.Fatal("refreshed token was not written back")
	}
	assertPluginCiphertextHides(t, svc, account.CredentialID, stored["access_token"])
	assertPluginLeaseCount(t, st, 0)

	agePluginCredential(t, svc, account.CredentialID, time.Now().Add(10*time.Second))
	runtime.mu.Lock()
	runtime.failNext = true
	runtime.mu.Unlock()
	_, refreshErr := svc.RefreshPluginAccountForGateway(ctx, created.ID, account.CredentialID, siteType, nil)
	if !errors.Is(refreshErr, accountauth.ErrReauthorizationRequired) {
		t.Fatalf("reauth error = %v", refreshErr)
	}
	if refreshErr == nil || strings.Contains(refreshErr.Error(), pluginAccountTokenMark) {
		t.Fatal("reauth error contains a token")
	}
	row, err := store.NewSiteCredentialRepository(st.DB()).GetByID(ctx, account.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(row.Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta["reauth_required"] != true || meta["enabled"] != false {
		t.Fatalf("reauth meta = %#v", meta)
	}
	if strings.Contains(string(row.Meta), pluginAccountTokenMark) || strings.Contains(row.EncryptedSecret, pluginAccountTokenMark) {
		t.Fatal("reauth row contains a token")
	}
}

func agePluginCredential(t *testing.T, svc *Service, credentialID uuid.UUID, expires time.Time) {
	t.Helper()
	fields := decryptPluginCredential(t, svc, credentialID)
	fields["expires_at"] = strconv.FormatInt(expires.Unix(), 10)
	writePluginCredential(t, svc, credentialID, fields)
}

func decryptPluginCredential(t *testing.T, svc *Service, credentialID uuid.UUID) map[string]string {
	t.Helper()
	row, err := store.NewSiteCredentialRepository(svc.db.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := svc.credentials.Decrypt(row.EncryptedSecret)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := decodePluginCredential(plain)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func writePluginCredential(t *testing.T, svc *Service, credentialID uuid.UUID, fields map[string]string) {
	t.Helper()
	encoded, err := encodePluginCredential(fields)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, _, err := svc.credentials.Encrypt(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteCredentialRepository(svc.db.DB()).UpdateSecret(context.Background(), credentialID, encrypted); err != nil {
		t.Fatal(err)
	}
	assertPluginCiphertextHides(t, svc, credentialID, fields["access_token"])
}

func assertPluginCiphertextHides(t *testing.T, svc *Service, credentialID uuid.UUID, secret string) {
	t.Helper()
	row, err := store.NewSiteCredentialRepository(svc.db.DB()).GetByID(context.Background(), credentialID)
	if err != nil {
		t.Fatal(err)
	}
	if secret != "" && (strings.Contains(row.EncryptedSecret, secret) || strings.Contains(row.MaskedSecret, secret) || strings.Contains(string(row.Meta), secret)) {
		t.Fatal("credential row contains a token")
	}
}

type gatewayRefreshRuntime struct {
	mu       sync.Mutex
	calls    int
	failNext bool
	hold     bool
	entered  chan struct{}
	release  chan struct{}
}

func (g *gatewayRefreshRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	return PluginLoginStart{
		ID: "sess-gw", Kind: accountLoginCustomPoll, Status: "pending",
		UserCode: "CODE-1", VerificationURL: "https://api.example.com/activate", Interval: 2,
	}, nil
}

func (g *gatewayRefreshRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.calls == 1 {
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

func (g *gatewayRefreshRuntime) Refresh(_ context.Context, fields map[string]string) (map[string]string, error) {
	g.mu.Lock()
	g.calls++
	fail := g.failNext
	g.failNext = false
	hold := g.hold
	entered := g.entered
	release := g.release
	token := fmt.Sprintf("ACCESS-%s-gw-%d", pluginAccountTokenMark, g.calls)
	g.mu.Unlock()
	if fail {
		return nil, fmt.Errorf("upstream rejected %s: %w", fields["access_token"], accountauth.ErrReauthorizationRequired)
	}
	if hold {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
	}
	out := cloneStringMap(fields)
	out["access_token"] = token
	out["expires_at"] = strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10)
	return out, nil
}

func (g *gatewayRefreshRuntime) Summarize(context.Context, string, map[string]string) (PluginAccountSummary, error) {
	remaining := 12.0
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok", Remark: "pro",
		Entries: []QuotaProbeEntry{{Label: "credits", Remaining: &remaining, Limit: &limit, Unit: "credits"}},
	}, nil
}

func (g *gatewayRefreshRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	return nil, nil
}

func (g *gatewayRefreshRuntime) ParseImport([]byte) ([]PluginImportedAccount, error) {
	return nil, nil
}
