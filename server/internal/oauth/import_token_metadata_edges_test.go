package oauth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestImportExpiresAtParsesNumericStringMillisecondsAndRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"null", `""`} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			var value importExpiresAt
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatalf("json.Unmarshal(%s): %v", raw, err)
			}
			if !value.Time.IsZero() {
				t.Fatalf("expires_at = %s, want zero", value.Time)
			}
		})
	}

	var parsed importExpiresAt
	if err := json.Unmarshal([]byte(`"1770000000123"`), &parsed); err != nil {
		t.Fatalf("unmarshal numeric string expires_at: %v", err)
	}
	want := time.UnixMilli(1770000000123)
	if !parsed.Time.Equal(want) {
		t.Fatalf("numeric string expires_at = %s, want %s", parsed.Time, want)
	}

	for _, raw := range []string{`"not-a-time"`, `{}`} {
		var invalid importExpiresAt
		if err := json.Unmarshal([]byte(raw), &invalid); err == nil || !strings.Contains(err.Error(), "parse expires_at:") {
			t.Fatalf("unmarshal expires_at %s error = %v, want parse error", raw, err)
		}
	}

	var badNumber importExpiresAt
	if err := json.Unmarshal([]byte(`not-a-number`), &badNumber); err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("bad number error = %v, want JSON numeric parse error", err)
	}
}

func TestDetectAndImportChatGPTTokenRejectsMissingFallbackMetadataBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	result := NewImportService(nil, "master-key", nil).DetectAndImport(context.Background(), []byte(`{
		"auth_mode":"chatgpt",
		"tokens":{
			"access_token":"access-token",
			"account_id":"acct_123"
		}
	}`), false)

	assertSingleFailedImport(t, result, "id_token is missing and required metadata is incomplete: missing email, plan_type, user_id, expires_at")
}

func TestApplyImportTokenMetadataUpdatesRefreshabilityBranches(t *testing.T) {
	t.Parallel()

	refreshableMeta := map[string]any{
		"refresh_warning": "stale warning",
	}
	applyImportTokenMetadata(refreshableMeta, " refresh-token ")
	if refreshableMeta["refreshable"] != true || refreshableMeta["token_mode"] != "oauth_refresh" {
		t.Fatalf("refreshable metadata = %#v, want oauth_refresh mode", refreshableMeta)
	}
	if _, ok := refreshableMeta["refresh_warning"]; ok {
		t.Fatalf("refreshable metadata kept stale warning: %#v", refreshableMeta)
	}

	accessOnlyMeta := map[string]any{}
	applyImportTokenMetadata(accessOnlyMeta, "   ")
	if accessOnlyMeta["refreshable"] != false || accessOnlyMeta["token_mode"] != "access_token_only" {
		t.Fatalf("access-token-only metadata = %#v, want access_token_only mode", accessOnlyMeta)
	}
	if accessOnlyMeta["refresh_warning"] != importAccessTokenOnlyWarning {
		t.Fatalf("refresh warning = %#v, want access-token-only warning", accessOnlyMeta["refresh_warning"])
	}

	applyImportTokenMetadata(nil, "refresh-token")
}
