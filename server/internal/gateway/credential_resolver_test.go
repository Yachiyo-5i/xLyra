package gateway

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	oauthsvc "xlyra/server/internal/oauth"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

type stubGatewayOAuth struct {
	codex            oauthsvc.CodexConnection
	codexErr         error
	codexCalls       []uuid.UUID
	antigravity      oauthsvc.CodexConnection
	antigravityErr   error
	antigravityCalls []uuid.UUID
	claude           oauthsvc.CodexConnection
	claudeErr        error
	claudeCalls      []uuid.UUID
	grokToken        string
	grokErr          error
	grokCalls        []uuid.UUID
}

func (s *stubGatewayOAuth) EnsureCodexConnectionFresh(_ context.Context, siteID uuid.UUID) (oauthsvc.CodexConnection, error) {
	s.codexCalls = append(s.codexCalls, siteID)
	return s.codex, s.codexErr
}

func (s *stubGatewayOAuth) EnsureAntigravityConnectionFresh(_ context.Context, siteID uuid.UUID) (oauthsvc.CodexConnection, error) {
	s.antigravityCalls = append(s.antigravityCalls, siteID)
	return s.antigravity, s.antigravityErr
}

func (s *stubGatewayOAuth) EnsureClaudeCodeConnectionFresh(_ context.Context, siteID uuid.UUID) (oauthsvc.CodexConnection, error) {
	s.claudeCalls = append(s.claudeCalls, siteID)
	return s.claude, s.claudeErr
}

func (s *stubGatewayOAuth) EnsureGrokAccessToken(_ context.Context, credentialID uuid.UUID) (string, error) {
	s.grokCalls = append(s.grokCalls, credentialID)
	return s.grokToken, s.grokErr
}

func (s *stubGatewayOAuth) MarkConnectionUnavailableBySiteID(context.Context, uuid.UUID, string) error {
	return nil
}

func (s *stubGatewayOAuth) calls(kind string) []uuid.UUID {
	switch kind {
	case "codex":
		return s.codexCalls
	case "antigravity":
		return s.antigravityCalls
	case "claude_code":
		return s.claudeCalls
	case "grok":
		return s.grokCalls
	default:
		return nil
	}
}

func TestCredentialResolverForSiteReturnsNilOutsideOAuthSites(t *testing.T) {
	t.Parallel()

	for _, siteType := range []string{"", "openai", "anthropic", "google", "gemini", "newapi", "kimi", "glm", "xlyra"} {
		if credentialResolverForSite(siteType) != nil {
			t.Fatalf("credentialResolverForSite(%q) returned a resolver", siteType)
		}
	}
}

func TestCredentialResolverSuccess(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	credentialID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	tests := []struct {
		name       string
		siteType   string
		kind       string
		accountID  string
		stub       stubGatewayOAuth
		wantKey    string
		wantAcct   string
		wantBase   string
		wantCalled uuid.UUID
	}{
		{
			name:      "codex",
			siteType:  " Codex ",
			kind:      "codex",
			accountID: "meta-account",
			stub: stubGatewayOAuth{codex: oauthsvc.CodexConnection{
				AccessToken: "codex-access",
				AccountID:   "codex-acct",
			}},
			wantKey:    "codex-access",
			wantAcct:   "codex-acct",
			wantCalled: siteID,
		},
		{
			name:      "antigravity",
			siteType:  "ANTIGRAVITY",
			kind:      "antigravity",
			accountID: "meta-account",
			stub: stubGatewayOAuth{antigravity: oauthsvc.CodexConnection{
				AccessToken: "antigravity-access",
				AccountID:   "antigravity-acct",
			}},
			wantKey:    "antigravity-access",
			wantAcct:   "antigravity-acct",
			wantCalled: siteID,
		},
		{
			name:      "claude_code",
			siteType:  "claude_code",
			kind:      "claude_code",
			accountID: "meta-account",
			stub: stubGatewayOAuth{claude: oauthsvc.CodexConnection{
				AccessToken: "claude-access",
				AccountID:   "claude-acct",
			}},
			wantKey:    "claude-access",
			wantAcct:   "claude-acct",
			wantCalled: siteID,
		},
		{
			name:       "grok",
			siteType:   "grok",
			kind:       "grok",
			accountID:  "meta-account",
			stub:       stubGatewayOAuth{grokToken: "grok-access"},
			wantKey:    "grok-access",
			wantAcct:   "meta-account",
			wantCalled: credentialID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := tt.stub
			resolved, failure := resolveCredentialForTest(t, tt.siteType, &stub, siteID, credentialID, "decrypted-key", tt.accountID)
			if failure != nil {
				t.Fatalf("failure = %#v, want nil", failure)
			}
			if resolved.Key != tt.wantKey || resolved.AccountID != tt.wantAcct || resolved.BaseURL != tt.wantBase {
				t.Fatalf("resolved = %+v, want key %q account %q base %q", resolved, tt.wantKey, tt.wantAcct, tt.wantBase)
			}
			assertOAuthCalls(t, &stub, tt.kind, tt.wantCalled)
		})
	}
}

func TestCredentialResolverFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	credentialID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	plain := errors.New("refresh unavailable")
	grokReauth := fmt.Errorf("refresh grok token: %w", oauthsvc.ErrGrokReauthorizationRequired)
	otherSentinel := errors.New("other reauthorization sentinel")

	tests := []struct {
		name     string
		siteType string
		kind     string
		stub     stubGatewayOAuth
		err      error
		wantType string
		tryNext  bool
		reauth   bool
		called   uuid.UUID
	}{
		{
			name:     "codex",
			siteType: "codex",
			kind:     "codex",
			err:      plain,
			wantType: "codex_oauth_refresh_failed",
			called:   siteID,
		},
		{
			name:     "antigravity",
			siteType: "antigravity",
			kind:     "antigravity",
			err:      plain,
			wantType: "antigravity_oauth_refresh_failed",
			called:   siteID,
		},
		{
			name:     "claude_code",
			siteType: "claude_code",
			kind:     "claude_code",
			err:      plain,
			wantType: "claude_code_oauth_refresh_failed",
			called:   siteID,
		},
		{
			name:     "grok transient",
			siteType: "grok",
			kind:     "grok",
			err:      plain,
			wantType: "grok_oauth_refresh_failed",
			tryNext:  true,
			called:   credentialID,
		},
		{
			name:     "grok reauth",
			siteType: "grok",
			kind:     "grok",
			err:      oauthsvc.ErrGrokReauthorizationRequired,
			wantType: "grok_oauth_refresh_failed",
			tryNext:  true,
			reauth:   true,
			called:   credentialID,
		},
		{
			name:     "grok wrapped reauth",
			siteType: "grok",
			kind:     "grok",
			err:      grokReauth,
			wantType: "grok_oauth_refresh_failed",
			tryNext:  true,
			reauth:   true,
			called:   credentialID,
		},
		{
			name:     "grok other sentinel",
			siteType: "grok",
			kind:     "grok",
			err:      otherSentinel,
			wantType: "grok_oauth_refresh_failed",
			tryNext:  true,
			called:   credentialID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := tt.stub
			switch tt.kind {
			case "codex":
				stub.codexErr = tt.err
			case "antigravity":
				stub.antigravityErr = tt.err
			case "claude_code":
				stub.claudeErr = tt.err
			case "grok":
				stub.grokErr = tt.err
			}
			resolved, failure := resolveCredentialForTest(t, tt.siteType, &stub, siteID, credentialID, "decrypted-key", "meta-account")
			if failure == nil {
				t.Fatal("expected a resolve failure")
			}
			if resolved.Key != "" || resolved.AccountID != "" || resolved.BaseURL != "" || resolved.HeaderVars != nil {
				t.Fatalf("resolved = %+v, want zero value", resolved)
			}
			if failure.ErrorType != tt.wantType || failure.TryNextCredential != tt.tryNext || failure.MarkReauth != tt.reauth {
				t.Fatalf("failure = %+v, want type %q tryNext %v reauth %v", failure, tt.wantType, tt.tryNext, tt.reauth)
			}
			if failure.Err != tt.err {
				t.Fatalf("failure error = %v, want %v", failure.Err, tt.err)
			}
			assertOAuthCalls(t, &stub, tt.kind, tt.called)
		})
	}
}

func resolveCredentialForTest(t *testing.T, siteType string, stub *stubGatewayOAuth, siteID uuid.UUID, credentialID uuid.UUID, key string, accountID string) (resolvedUpstreamCredential, *credentialResolveFailure) {
	t.Helper()

	resolver := credentialResolverForSite(siteType)
	if resolver == nil {
		t.Fatalf("credentialResolverForSite(%q) = nil", siteType)
	}
	return resolver.Resolve(context.Background(), Handler{oauth: stub}, routeengine.Candidate{
		Site: routeengine.CandidateSite{ID: siteID, SiteType: siteType, BaseURL: "https://site.example"},
	}, store.GatewayCredential{
		Credential: store.SiteCredential{ID: credentialID},
	}, key, accountID)
}

func assertOAuthCalls(t *testing.T, stub *stubGatewayOAuth, kind string, want uuid.UUID) {
	t.Helper()

	for _, name := range []string{"codex", "antigravity", "claude_code", "grok"} {
		calls := stub.calls(name)
		if name != kind {
			if len(calls) != 0 {
				t.Fatalf("%s calls = %v, want none", name, calls)
			}
			continue
		}
		if len(calls) != 1 || calls[0] != want {
			t.Fatalf("%s calls = %v, want [%s]", name, calls, want)
		}
	}
}
