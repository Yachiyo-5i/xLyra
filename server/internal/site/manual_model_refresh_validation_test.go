package site

import (
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestNormalizeManualEndpointTypesSkipsBlanksAndDuplicates(t *testing.T) {
	t.Parallel()

	got, err := normalizeManualEndpointTypes([]string{" ", "OPENAI-RESPONSE", "\t", "openai-response"})
	if err != nil {
		t.Fatalf("normalizeManualEndpointTypes() error = %v", err)
	}
	if len(got) != 1 || got[0] != "openai-response" {
		t.Fatalf("endpoint types = %#v, want [openai-response]", got)
	}

	got, err = normalizeManualEndpointTypes([]string{" openai ", "OPENAI", "openai-response", "OPENAI-AUDIO-SPEECH"})
	if err != nil {
		t.Fatalf("supported endpoint types error = %v", err)
	}
	if len(got) != 3 || got[0] != "openai" || got[1] != "openai-response" || got[2] != "openai-audio-speech" {
		t.Fatalf("supported endpoint types = %#v, want [openai openai-response openai-audio-speech]", got)
	}
	if _, err := normalizeManualEndpointTypes([]string{"unknown"}); err == nil {
		t.Fatal("unsupported endpoint type should fail")
	}
	if _, err := normalizeManualEndpointTypes(nil); err == nil {
		t.Fatal("empty endpoint types should fail")
	}
}

func TestManualSiteModelCapabilitiesIncludeManualSourceAndEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := []string{"openai", "openai-response"}
	capabilities := manualSiteModelCapabilities(endpoints)

	raw, ok := capabilities["raw"].(map[string]any)
	if !ok {
		t.Fatalf("raw capabilities = %#v, want map", capabilities["raw"])
	}
	if capabilities["source"] != "manual" || capabilities["manual"] != true {
		t.Fatalf("top-level manual markers = %#v, want manual source", capabilities)
	}
	if raw["source"] != "manual" {
		t.Fatalf("raw source = %#v, want manual", raw["source"])
	}
	gotEndpoints, ok := capabilities["supported_endpoint_types"].([]string)
	if !ok {
		t.Fatalf("supported endpoint types = %#v, want []string", capabilities["supported_endpoint_types"])
	}
	if len(gotEndpoints) != len(endpoints) || gotEndpoints[0] != endpoints[0] || gotEndpoints[1] != endpoints[1] {
		t.Fatalf("supported endpoint types = %#v, want %#v", gotEndpoints, endpoints)
	}
}

func TestOAuthPermanentAuthRefreshMessageIgnoresBlankAndTransientChallenges(t *testing.T) {
	t.Parallel()

	if oauthPermanentAuthRefreshMessage(" ") {
		t.Fatal("blank auth refresh message should not be permanent")
	}
	if !oauthPermanentAuthRefreshMessage("provider returned INVALID_GRANT for refresh token") {
		t.Fatal("invalid_grant should be treated as permanent auth failure")
	}
	if !oauthPermanentAuthRefreshMessage("upstream returned HTTP 401") {
		t.Fatal("HTTP 401 should be treated as permanent auth failure")
	}
	if oauthPermanentAuthRefreshMessage(`codex upstream returned 403: <html><head><meta http-equiv=refresh></head></html>`) {
		t.Fatal("transient Codex html refresh challenge should not be permanent")
	}
}

func TestRefreshValidationForSyncKeepsNonOAuthHTTP4xxFailuresValid(t *testing.T) {
	t.Parallel()

	status, validationOK, validationMessage, authFailure := refreshValidationForSync("openai", "partial", "HTTP 401")
	if status != "partial" || validationOK != true || validationMessage != nil || authFailure != "" {
		t.Fatalf("non-oauth validation = (%#v, %#v, %#v, %#v), want original status and valid", status, validationOK, validationMessage, authFailure)
	}
}

func TestManualCredentialEnabledAcceptsAnySyncedStateID(t *testing.T) {
	t.Parallel()

	credential := store.SiteCredential{Meta: siteJSONMeta(t, map[string]any{"enabled": true})}
	if !manualCredentialEnabled(credential, store.SiteAPIKeyState{SiteCredentialID: uuid.New(), Enabled: true}) {
		t.Fatal("manualCredentialEnabled should accept any non-nil synced state id when enabled")
	}
}
