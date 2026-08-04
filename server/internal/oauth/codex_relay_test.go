package oauth

import (
	"net/url"
	"testing"

	"xlyra/server/internal/store"
)

func TestRelayTargetURLHandlesMalformedOrMissingMetadata(t *testing.T) {
	t.Parallel()

	for _, raw := range []store.JSON{
		nil,
		store.JSON(``),
		store.JSON(`{`),
		store.JSON(`{not-json`),
		store.JSON(`{"relay_target_url":""}`),
		store.JSON(`{"relay_target_url":"   "}`),
		store.JSON(`{"relay_target_url":42}`),
		store.JSON(`{"relay_target_url":false}`),
	} {
		if got := relayTargetURL(raw); got != "" {
			t.Fatalf("relayTargetURL(%q) = %q, want empty", string(raw), got)
		}
	}
}

func TestWithRelayQueryOverwritesCallbackValuesAndPreservesTargetParts(t *testing.T) {
	t.Parallel()

	values := url.Values{}
	values["code"] = []string{"old-callback-code", "new-callback-code"}
	values.Set("state", "oauth state")

	merged, err := withRelayQuery(" https://xlyra.example.com/callback?code=target-code&keep=1#done ", values)
	if err != nil {
		t.Fatalf("withRelayQuery: %v", err)
	}

	parsed, err := url.Parse(merged)
	if err != nil {
		t.Fatalf("parse merged relay URL: %v", err)
	}
	query := parsed.Query()
	if parsed.Scheme != "https" || parsed.Host != "xlyra.example.com" || parsed.Path != "/callback" || parsed.Fragment != "done" {
		t.Fatalf("target URL parts were not preserved: %s", merged)
	}
	if query.Get("code") != "new-callback-code" {
		t.Fatalf("code = %q, want new-callback-code", query.Get("code"))
	}
	if query.Get("state") != "oauth state" {
		t.Fatalf("state = %q, want oauth state", query.Get("state"))
	}
	if query.Get("keep") != "1" {
		t.Fatalf("keep = %q, want 1", query.Get("keep"))
	}
}

func TestWithRelayQueryRejectsTargetsWithoutAbsoluteHTTPOrigin(t *testing.T) {
	t.Parallel()

	values := url.Values{"state": []string{"oauth-state"}}
	for _, target := range []string{
		"",
		"   ",
		"/callback",
		"/relative/callback",
		"//xlyra.example.com/callback",
		"https:///callback",
		"://bad",
		"localhost:3000/callback",
	} {
		if _, err := withRelayQuery(target, values); err == nil {
			t.Fatalf("withRelayQuery(%q) succeeded, want error", target)
		}
	}
}
