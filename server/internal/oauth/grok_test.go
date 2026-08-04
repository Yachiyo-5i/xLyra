package oauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func grokAccessTokenWithExpiry(expiresAt int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, expiresAt)))
	return header + "." + payload + ".sig"
}

func TestGrokBundleFreshRejectsUnknownExpiry(t *testing.T) {
	if grokBundleFresh(GrokCredentialBundle{AccessToken: "opaque-token"}) {
		t.Fatal("an access token with unknown expiry must not be considered fresh")
	}
}

func TestGrokBundleFreshUsesJWTExpiryForLegacyBundle(t *testing.T) {
	future := grokAccessTokenWithExpiry(time.Now().Add(10 * time.Minute).Unix())
	if !grokBundleFresh(GrokCredentialBundle{AccessToken: future}) {
		t.Fatal("a legacy bundle with a fresh JWT expiry should be considered fresh")
	}
	past := grokAccessTokenWithExpiry(time.Now().Add(-time.Minute).Unix())
	if grokBundleFresh(GrokCredentialBundle{AccessToken: past}) {
		t.Fatal("an expired JWT must not be considered fresh")
	}
}

func TestGrokScopeMatchesCLIRequiredPermissions(t *testing.T) {
	want := "openid profile email offline_access grok-cli:access api:access"
	if grokScope != want {
		t.Fatalf("grokScope = %q, want %q", grokScope, want)
	}
}

func TestGrokTokenIdentityFallsBackToSubject(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"principal-1","team_id":"team-1"}`))
	principalID, teamID := GrokTokenIdentity(header + "." + payload + ".sig")
	if principalID != "principal-1" || teamID != "team-1" {
		t.Fatalf("identity = %q/%q", principalID, teamID)
	}
}

func TestGrokRefreshInvalidGrantRequiresReauthorization(t *testing.T) {
	client := &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid_grant","error_description":"refresh token revoked"}`)),
			Request:    req,
		}, nil
	})}
	form := make(url.Values)
	form.Set("grant_type", "refresh_token")
	_, err := (&Service{}).grokTokenRequest(context.Background(), client, form)
	if !errors.Is(err, ErrGrokReauthorizationRequired) {
		t.Fatalf("grokTokenRequest() error = %v, want reauthorization required", err)
	}
}
