package site

import (
	"testing"

	"xlyra/server/internal/store"
)

func TestOAuthRefreshMessageHelpersRecognizeHTTP4xxAndTransientChallenges(t *testing.T) {
	t.Parallel()

	if messageContainsAuthFailureCode("upstream returned status=429 after quota check") {
		t.Fatal("expected 429 (rate limit) not to count as an auth failure")
	}
	if !messageContainsAuthFailureCode("upstream returned 401: unauthorized") {
		t.Fatal("expected 401 to be recognized as an auth failure")
	}
	if !messageContainsAuthFailureCode("upstream returned 403: forbidden") {
		t.Fatal("expected 403 to be recognized as an auth failure")
	}
	// F17: non-auth 4xx must not auto-disable a usable credential.
	if messageContainsAuthFailureCode("upstream returned 404: not found") {
		t.Fatal("expected 404 not to count as an auth failure")
	}
	if messageContainsAuthFailureCode("upstream returned 408: request timeout") {
		t.Fatal("expected 408 not to count as an auth failure")
	}
	if messageContainsAuthFailureCode("upstream returned 400: bad request") {
		t.Fatal("expected bare 400 not to count as an auth failure")
	}
	if messageContainsAuthFailureCode("model context window is 400000 tokens") {
		t.Fatal("expected long non-status numbers to be ignored")
	}
	if messageContainsAuthFailureCode("status was 39 then 500") {
		t.Fatal("expected non-4xx values to be ignored")
	}
	if messageContainsAuthFailureCode("dial tcp 10.0.0.1:443: i/o timeout") {
		t.Fatal("expected port 443 in network error not to be detected as an auth failure")
	}
	if messageContainsAuthFailureCode("connect to 127.0.0.1:4000 failed") {
		t.Fatal("expected port 4000 not to be detected as an auth failure")
	}
	if !oauthTransientHTML403RefreshMessage(`codex upstream returned 403: <html><head><meta http-equiv='refresh'></head></html>`) {
		t.Fatal("expected single-quoted meta refresh challenge to be transient")
	}
	if !oauthTransientHTML403RefreshMessage(`codex upstream returned 403: <html><head><meta content="360"></head></html>`) {
		t.Fatal("expected html meta refresh interval challenge to be transient")
	}
	if oauthTransientHTML403RefreshMessage(`codex upstream returned 403: {"error":"invalid_grant"}`) {
		t.Fatal("expected non-html 403 auth error not to be transient")
	}
}

func TestRefreshMetadataHelpersTrimStringsAndReadAutoDisableMarker(t *testing.T) {
	t.Parallel()

	if got := firstNonEmptyString(" \t ", " first ", "second"); got != "first" {
		t.Fatalf("first non-empty string = %q, want first", got)
	}
	if got := firstNonEmptyString(" ", "\n"); got != "" {
		t.Fatalf("all blank strings = %q, want empty", got)
	}

	invalidMetaSite := store.Site{Meta: store.JSON(`{`)}
	if siteAutoDisabledByRefresh(invalidMetaSite) {
		t.Fatal("invalid site meta should not be treated as auto-disabled")
	}

	autoDisabledSite := store.Site{
		Meta: siteJSONMeta(t, map[string]any{siteMetaAutoDisabledByRefresh: true, "keep": "value"}),
	}
	if !siteAutoDisabledByRefresh(autoDisabledSite) {
		t.Fatal("expected auto-disabled marker to be detected")
	}
	meta := siteMetaMap(autoDisabledSite)
	if meta["keep"] != "value" {
		t.Fatalf("site meta map = %#v, want keep=value", meta)
	}
}
