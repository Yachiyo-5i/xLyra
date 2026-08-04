package oauth

import (
	"net/url"
	"strings"
	"testing"
)

func TestAntigravityAuthorizeLinkUsesGoogleOAuthEndpointAndRequiredParams(t *testing.T) {
	t.Parallel()

	rawURL := antigravityAuthorizeLink(" state-123 ", "http://localhost:1456/oauth/antigravity/callback", antigravityOAuthClient{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
	})

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse antigravity authorize link: %v", err)
	}
	if gotEndpoint := parsed.Scheme + "://" + parsed.Host + parsed.Path; gotEndpoint != antigravityAuthorizeURL {
		t.Fatalf("authorize endpoint = %q, want %q", gotEndpoint, antigravityAuthorizeURL)
	}

	query := parsed.Query()
	want := map[string]string{
		"response_type":          "code",
		"client_id":              "client-id",
		"redirect_uri":           "http://localhost:1456/oauth/antigravity/callback",
		"scope":                  strings.Join(antigravityScopes, " "),
		"access_type":            "offline",
		"prompt":                 "consent",
		"include_granted_scopes": "true",
		"state":                  " state-123 ",
	}
	for key, wantValue := range want {
		if got := query.Get(key); got != wantValue {
			t.Fatalf("%s = %q, want %q in %s", key, got, wantValue, rawURL)
		}
	}
	if got := query.Get("client_secret"); got != "" {
		t.Fatalf("authorize link leaked client_secret = %q", got)
	}
}
