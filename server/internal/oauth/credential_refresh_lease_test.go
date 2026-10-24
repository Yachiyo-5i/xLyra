package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

type credentialLeaseHold struct {
	leaseID string
	until   time.Time
}

type credentialLeaseHarness struct {
	mu            sync.Mutex
	credential    store.SiteCredential
	leases        map[uuid.UUID]credentialLeaseHold
	onAcquireMiss func()
}

func (h *credentialLeaseHarness) query(tx *gorm.DB) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch dest := tx.Statement.Dest.(type) {
	case *store.SiteCredential:
		*dest = h.credential
		tx.RowsAffected = 1
	case *store.Site:
		*dest = store.Site{ID: h.credential.SiteID, SiteType: "grok", Enabled: true, Meta: store.JSON(`{}`)}
		tx.RowsAffected = 1
	default:
		tx.AddError(fmt.Errorf("unexpected query dest %T", tx.Statement.Dest))
	}
}

func (h *credentialLeaseHarness) update(tx *gorm.DB) {
	values, ok := tx.Statement.Dest.(map[string]any)
	if !ok || len(values) != 1 {
		tx.AddError(fmt.Errorf("unexpected credential update %#v", tx.Statement.Dest))
		return
	}
	secret, ok := values["encrypted_secret"].(string)
	if !ok {
		tx.AddError(fmt.Errorf("unexpected credential update %#v", values))
		return
	}
	h.mu.Lock()
	h.credential.EncryptedSecret = secret
	h.mu.Unlock()
	tx.RowsAffected = 1
}

func (h *credentialLeaseHarness) raw(tx *gorm.DB) {
	if err := tx.Statement.Context.Err(); err != nil {
		tx.AddError(err)
		return
	}
	sqlText := tx.Statement.SQL.String()
	lower := strings.ToLower(sqlText)
	if strings.Contains(lower, "now()") {
		tx.AddError(errors.New("lease sql calls now()"))
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.leases == nil {
		h.leases = map[uuid.UUID]credentialLeaseHold{}
	}
	switch {
	case strings.Contains(lower, "insert into site_credential_refresh_leases"):
		if !strings.Contains(lower, "on conflict (site_credential_id)") || !strings.Contains(lower, "lease_until <") || len(tx.Statement.Vars) != 4 {
			tx.AddError(fmt.Errorf("unexpected acquire sql %s", sqlText))
			return
		}
		id, idOK := tx.Statement.Vars[0].(uuid.UUID)
		leaseID, leaseOK := tx.Statement.Vars[1].(string)
		until, untilOK := tx.Statement.Vars[2].(time.Time)
		now, nowOK := tx.Statement.Vars[3].(time.Time)
		if !idOK || !leaseOK || !untilOK || !nowOK {
			tx.AddError(errors.New("acquire sql bound unexpected value types"))
			return
		}
		current, held := h.leases[id]
		if !held || current.until.Before(now) {
			h.leases[id] = credentialLeaseHold{leaseID: leaseID, until: until}
			tx.RowsAffected = 1
			return
		}
		tx.RowsAffected = 0
		miss := h.onAcquireMiss
		h.onAcquireMiss = nil
		if miss != nil {
			h.mu.Unlock()
			miss()
			h.mu.Lock()
		}
	case strings.Contains(lower, "delete from site_credential_refresh_leases"):
		if len(tx.Statement.Vars) != 2 {
			tx.AddError(fmt.Errorf("unexpected release sql %s", sqlText))
			return
		}
		id, idOK := tx.Statement.Vars[0].(uuid.UUID)
		leaseID, leaseOK := tx.Statement.Vars[1].(string)
		if !idOK || !leaseOK {
			tx.AddError(errors.New("release sql bound unexpected value types"))
			return
		}
		current, held := h.leases[id]
		if held && current.leaseID == leaseID {
			delete(h.leases, id)
			tx.RowsAffected = 1
		}
	default:
		tx.AddError(fmt.Errorf("unexpected lease sql %s", sqlText))
	}
}

func (h *credentialLeaseHarness) hold(credentialID uuid.UUID, leaseID string, until time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.leases == nil {
		h.leases = map[uuid.UUID]credentialLeaseHold{}
	}
	h.leases[credentialID] = credentialLeaseHold{leaseID: leaseID, until: until}
}

func (h *credentialLeaseHarness) leaseCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.leases)
}

func (h *credentialLeaseHarness) secret() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.credential.EncryptedSecret
}

func (h *credentialLeaseHarness) storePlaintext(t *testing.T, service *Service, plaintext string) {
	t.Helper()
	encrypted, _, err := service.credentials.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	h.mu.Lock()
	h.credential.EncryptedSecret = encrypted
	h.mu.Unlock()
}

func newCredentialLeaseService(t *testing.T, harness *credentialLeaseHarness, transport http.RoundTripper) *Service {
	t.Helper()
	db := oauthGormWithCallbacks(t, oauthGormCallbacks{query: harness.query, update: harness.update})
	if err := db.Callback().Raw().Replace("gorm:raw", harness.raw); err != nil {
		t.Fatalf("replace raw callback: %v", err)
	}
	service := NewService(oauthStoreWithGorm(t, db), "master-key")
	service.httpClient = &http.Client{Transport: transport}
	return service
}

func TestEnsureAccountRefreshCollapsesConcurrentCalls(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"grok"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			credentialID := uuid.New()
			harness := &credentialLeaseHarness{credential: store.SiteCredential{
				ID:             credentialID,
				SiteID:         uuid.New(),
				CredentialType: provider + ":" + uuid.NewString(),
			}}
			var calls atomic.Int32
			service := newCredentialLeaseService(t, harness, oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				time.Sleep(80 * time.Millisecond)
				return accountRefreshHTTPResponse(provider, "new-access", "new-refresh"), nil
			}))
			harness.storePlaintext(t, service, expiredAccountBundle(provider, "old-access", "old-refresh"))

			const workers = 2
			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make([]error, workers)
			tokens := make([]string, workers)
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					<-start
					token, err := ensureAccountAccess(service, context.Background(), provider, credentialID)
					tokens[index] = token
					errs[index] = err
				}(i)
			}
			close(start)
			wg.Wait()

			if calls.Load() != 1 {
				t.Fatalf("upstream refreshes = %d, want 1", calls.Load())
			}
			for index := range errs {
				if errs[index] != nil || tokens[index] != "new-access" {
					t.Fatalf("worker %d = %q, %v", index, tokens[index], errs[index])
				}
			}
			if harness.leaseCount() != 0 {
				t.Fatalf("leases left = %d, want the holder to release", harness.leaseCount())
			}
			again, err := ensureAccountAccess(service, context.Background(), provider, credentialID)
			if err != nil || again != "new-access" || calls.Load() != 1 {
				t.Fatalf("follow-up Ensure = %q, %v, upstream %d, want the written token without another refresh", again, err, calls.Load())
			}
		})
	}
}

func TestEnsureAccountRefreshUsesCredentialRefreshedByAnotherInstance(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"grok"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			credentialID := uuid.New()
			harness := &credentialLeaseHarness{credential: store.SiteCredential{
				ID:             credentialID,
				SiteID:         uuid.New(),
				CredentialType: provider + ":" + uuid.NewString(),
			}}
			var calls atomic.Int32
			service := newCredentialLeaseService(t, harness, oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return accountRefreshHTTPResponse(provider, "should-not-refresh", "should-not-refresh"), nil
			}))
			harness.storePlaintext(t, service, expiredAccountBundle(provider, "old-access", "old-refresh"))
			harness.hold(credentialID, "other-instance", time.Now().Add(time.Hour))
			harness.onAcquireMiss = func() {
				harness.storePlaintext(t, service, freshAccountBundle(provider, "fresh-access", "fresh-refresh"))
			}

			token, err := ensureAccountAccess(service, context.Background(), provider, credentialID)
			if err != nil || token != "fresh-access" {
				t.Fatalf("Ensure = %q, %v, want the credential refreshed by the other instance", token, err)
			}
			if calls.Load() != 0 {
				t.Fatalf("upstream refreshes = %d, want 0 while another lease holds the credential", calls.Load())
			}
			if harness.leaseCount() != 1 {
				t.Fatalf("leases = %d, want the other instance to keep its lease", harness.leaseCount())
			}
		})
	}
}

func TestEnsureAccountRefreshReturnsCanceledWhileWaitingForLease(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"grok"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			credentialID := uuid.New()
			harness := &credentialLeaseHarness{credential: store.SiteCredential{
				ID:             credentialID,
				SiteID:         uuid.New(),
				CredentialType: provider + ":" + uuid.NewString(),
			}}
			var calls atomic.Int32
			service := newCredentialLeaseService(t, harness, oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return accountRefreshHTTPResponse(provider, "should-not-refresh", "should-not-refresh"), nil
			}))
			harness.storePlaintext(t, service, expiredAccountBundle(provider, "old-access", "old-refresh"))
			harness.hold(credentialID, "other-instance", time.Now().Add(time.Hour))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			harness.onAcquireMiss = cancel

			_, err := ensureAccountAccess(service, ctx, provider, credentialID)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Ensure error = %v, want context.Canceled", err)
			}
			if calls.Load() != 0 {
				t.Fatalf("upstream refreshes = %d, want 0 after cancel", calls.Load())
			}
		})
	}
}

func TestEnsureAccountRefreshKeepsRefreshTokenWhenUpstreamOmitsIt(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"grok"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			credentialID := uuid.New()
			harness := &credentialLeaseHarness{credential: store.SiteCredential{
				ID:             credentialID,
				SiteID:         uuid.New(),
				CredentialType: provider + ":" + uuid.NewString(),
			}}
			service := newCredentialLeaseService(t, harness, oauthRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return accountRefreshHTTPResponse(provider, "new-access", ""), nil
			}))
			harness.storePlaintext(t, service, expiredAccountBundle(provider, "old-access", "old-refresh"))

			token, err := ensureAccountAccess(service, context.Background(), provider, credentialID)
			if err != nil || token != "new-access" {
				t.Fatalf("Ensure = %q, %v", token, err)
			}
			plaintext, err := service.credentials.Decrypt(harness.secret())
			if err != nil {
				t.Fatalf("decrypt stored credential: %v", err)
			}
			access, refresh := decodedAccountTokens(t, provider, plaintext)
			if access != "new-access" || refresh != "old-refresh" {
				t.Fatalf("stored tokens = access %q refresh %q, want new access and the previous refresh token", access, refresh)
			}
			if harness.leaseCount() != 0 {
				t.Fatalf("leases left = %d, want release after refresh", harness.leaseCount())
			}
		})
	}
}

func ensureAccountAccess(service *Service, ctx context.Context, provider string, credentialID uuid.UUID) (string, error) {
	switch provider {
	case "grok":
		return service.EnsureGrokAccessToken(ctx, credentialID)
	default:
		return "", fmt.Errorf("unknown provider %s", provider)
	}
}

func expiredAccountBundle(provider string, accessToken string, refreshToken string) string {
	switch provider {
	case "grok":
		raw, err := EncodeGrokBundle(GrokCredentialBundle{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: 1})
		if err != nil {
			panic(err)
		}
		return raw
	default:
		panic(provider)
	}
}

func freshAccountBundle(provider string, accessToken string, refreshToken string) string {
	expiresAt := time.Now().Add(2 * time.Hour).Unix()
	switch provider {
	case "grok":
		raw, err := EncodeGrokBundle(GrokCredentialBundle{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt})
		if err != nil {
			panic(err)
		}
		return raw
	default:
		panic(provider)
	}
}

func accountRefreshHTTPResponse(provider string, accessToken string, refreshToken string) *http.Response {
	switch provider {
	case "grok":
		body := `{"access_token":"` + accessToken + `","expires_in":3600,"token_type":"Bearer"}`
		if refreshToken != "" {
			body = `{"access_token":"` + accessToken + `","refresh_token":"` + refreshToken + `","expires_in":3600,"token_type":"Bearer"}`
		}
		return oauthHTTPResponse(http.StatusOK, body)
	default:
		panic(provider)
	}
}

func decodedAccountTokens(t *testing.T, provider string, plaintext string) (string, string) {
	t.Helper()
	switch provider {
	case "grok":
		bundle, err := DecodeGrokBundle(plaintext)
		if err != nil {
			t.Fatalf("decode grok bundle: %v", err)
		}
		return bundle.AccessToken, bundle.RefreshToken
	default:
		t.Fatalf("unknown provider %s", provider)
		return "", ""
	}
}
