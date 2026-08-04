package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestDetectAndImportRejectsMismatchedTopLevelImportShapesBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil)
	for _, tc := range []struct {
		name      string
		payload   string
		wantError string
	}{
		{
			name:      "accounts_field_is_not_sub2api_array",
			payload:   `{"accounts":"not-an-array"}`,
			wantError: "parse Sub2API format:",
		},
		{
			name:      "tokens_field_is_not_chatgpt_token_object",
			payload:   `{"tokens":"not-an-object"}`,
			wantError: "parse ChatGPT token format:",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := service.DetectAndImport(context.Background(), []byte(tc.payload), false)

			assertSingleFailedImport(t, result, tc.wantError)
		})
	}
}

func TestDecodeIDTokenExtractsChatGPTAuthMetadataAndRejectsInvalidTokens(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"", "one.two", "one.not-base64.three", "one." + base64.RawURLEncoding.EncodeToString([]byte("not-json")) + ".three"} {
		raw, meta := decodeIDToken(token)
		if raw != nil || meta != nil {
			t.Fatalf("decodeIDToken(%q) = raw:%#v meta:%#v, want nil maps", token, raw, meta)
		}
	}

	payload := map[string]any{
		"email": "agent@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id":                 "acct_123",
			"chatgpt_plan_type":                  "plus",
			"chatgpt_subscription_active_start":  float64(1770000000),
			"chatgpt_subscription_active_until":  float64(1770600000),
			"chatgpt_user_id":                    "user_123",
			"ignored_non_import_metadata_member": "ignored",
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	raw, meta := decodeIDToken("header." + base64.RawURLEncoding.EncodeToString(encoded) + ".signature")
	if raw["email"] != "agent@example.com" {
		t.Fatalf("raw email = %#v, want agent@example.com", raw["email"])
	}
	if meta["chatgpt_account_id"] != "acct_123" || meta["plan_type"] != "plus" || meta["chatgpt_user_id"] != "user_123" {
		t.Fatalf("auth metadata = %#v, want account, plan, and user metadata", meta)
	}
	if meta["chatgpt_subscription_active_start"] != float64(1770000000) || meta["chatgpt_subscription_active_until"] != float64(1770600000) {
		t.Fatalf("subscription metadata = %#v, want imported subscription window", meta)
	}
	if _, ok := meta["ignored_non_import_metadata_member"]; ok {
		t.Fatalf("unexpected ignored metadata copied: %#v", meta)
	}
}

func TestMergeIDTokenMetadataPreservesExistingValuesWhenClaimsAreUnusable(t *testing.T) {
	t.Parallel()

	meta := map[string]any{
		"plan_type":       "team",
		"chatgpt_user_id": "user_existing",
	}
	mergeIDTokenMetadata(meta, map[string]any{
		"plan_type":                         "  ",
		"chatgpt_user_id":                   42,
		"chatgpt_subscription_active_start": "2026-06-01T00:00:00Z",
		"chatgpt_subscription_active_until": "2026-07-01T00:00:00Z",
	})

	if meta["plan_type"] != "team" || meta["chatgpt_user_id"] != "user_existing" {
		t.Fatalf("blank/non-string claims overwrote stable metadata: %#v", meta)
	}
	if meta["chatgpt_subscription_active_start"] != "2026-06-01T00:00:00Z" || meta["chatgpt_subscription_active_until"] != "2026-07-01T00:00:00Z" {
		t.Fatalf("subscription metadata was not copied: %#v", meta)
	}
}

func TestImportTokenHelpersTrimCPAFieldsAndResolveProviderDefaults(t *testing.T) {
	t.Parallel()

	antigravity := CPAExport{
		Type:      "google",
		Email:     " agent@example.com ",
		ProjectID: " project-123 ",
	}
	if got := cpaAccountID(antigravityProvider, antigravity); got != "agent@example.com" {
		t.Fatalf("antigravity fallback account id = %q, want trimmed email", got)
	}

	profile := cpaRawProfile(antigravity)
	if profile["email"] != "agent@example.com" || profile["project_id"] != "project-123" {
		t.Fatalf("CPA raw profile = %#v, want trimmed email and project_id", profile)
	}
	if got := cpaRawProfile(CPAExport{}); got != nil {
		t.Fatalf("empty CPA raw profile = %#v, want nil", got)
	}

	if got := oauthImportedDefaultBaseURL(" ANTIGRAVITY "); got != "https://daily-cloudcode-pa.sandbox.googleapis.com" {
		t.Fatalf("antigravity default base URL = %q", got)
	}
	if got := oauthImportedDefaultBaseURL("openai"); got != codexDefaultBackendBaseURL {
		t.Fatalf("codex default base URL = %q, want %q", got, codexDefaultBackendBaseURL)
	}

	if got := safeAccountIDPrefix(" 1234567890 "); got != "12345678" {
		t.Fatalf("safeAccountIDPrefix long id = %q, want first eight trimmed chars", got)
	}
	if got := safeAccountIDPrefix(" short "); got != "short" {
		t.Fatalf("safeAccountIDPrefix short id = %q, want trimmed id", got)
	}
}

func TestImportResultAndUnixTimeHelpersHandleBoundaryValues(t *testing.T) {
	t.Parallel()

	if got := importResultStatus(" connected "); got != "queued" {
		t.Fatalf("connected import result status = %q, want queued", got)
	}
	if got := importResultStatus("unknown"); got != "failed" {
		t.Fatalf("unknown import result status = %q, want failed", got)
	}

	if got := importUnixTime(-1); !got.IsZero() {
		t.Fatalf("negative import unix time = %s, want zero", got)
	}
	seconds := importUnixTime(1770000000)
	if !seconds.Equal(time.Unix(1770000000, 0)) {
		t.Fatalf("seconds import time = %s, want %s", seconds, time.Unix(1770000000, 0))
	}
}
