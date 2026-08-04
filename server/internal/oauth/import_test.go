package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestNewImportServiceWithProxyAndEmptyImport(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil).WithProxyID(" proxy-main ")
	if service == nil {
		t.Fatal("expected import service")
	}
	if service.db != nil {
		t.Fatalf("db = %#v, want nil", service.db)
	}
	if service.oauthSvc != nil {
		t.Fatalf("oauth service = %#v, want nil", service.oauthSvc)
	}
	if service.proxyID != "proxy-main" {
		t.Fatalf("proxyID = %q, want proxy-main", service.proxyID)
	}

	result := service.ImportAccounts(context.Background(), Sub2APIExport{}, false)
	if result.Meta.Total != 0 || result.Meta.Queued != 0 || result.Meta.Failed != 0 || len(result.Items) != 0 {
		t.Fatalf("empty import result = %#v", result)
	}
}

func TestSub2APIExpiresAtAcceptsRFC3339String(t *testing.T) {
	t.Parallel()

	var export Sub2APIExport
	err := json.Unmarshal([]byte(`{
		"accounts": [{
			"name": "user@example.com",
			"platform": "openai",
			"type": "oauth",
			"credentials": {
				"access_token": "access",
				"refresh_token": "refresh",
				"chatgpt_account_id": "acct",
				"expires_at": "2026-05-30T01:59:28.000Z"
			}
		}]
	}`), &export)
	if err != nil {
		t.Fatalf("expected RFC3339 expires_at to unmarshal: %v", err)
	}
	want := time.Date(2026, 5, 30, 1, 59, 28, 0, time.UTC)
	if !export.Accounts[0].Credentials.ExpiresAt.Time.Equal(want) {
		t.Fatalf("unexpected expires_at: got %s want %s", export.Accounts[0].Credentials.ExpiresAt.Time, want)
	}
}

func TestSub2APIExpiresAtAcceptsUnixSecondsAndMilliseconds(t *testing.T) {
	t.Parallel()

	var seconds importExpiresAt
	if err := json.Unmarshal([]byte(`1770000000`), &seconds); err != nil {
		t.Fatalf("expected unix seconds to unmarshal: %v", err)
	}
	if !seconds.Time.Equal(time.Unix(1770000000, 0)) {
		t.Fatalf("unexpected unix seconds time: %s", seconds.Time)
	}

	var milliseconds importExpiresAt
	if err := json.Unmarshal([]byte(`1770000000000`), &milliseconds); err != nil {
		t.Fatalf("expected unix milliseconds to unmarshal: %v", err)
	}
	if !milliseconds.Time.Equal(time.UnixMilli(1770000000000)) {
		t.Fatalf("unexpected unix milliseconds time: %s", milliseconds.Time)
	}
}

func TestParseImportExpiresAtString(t *testing.T) {
	t.Parallel()

	empty, err := parseImportExpiresAtString(" ")
	if err != nil {
		t.Fatalf("empty expires_at should parse: %v", err)
	}
	if !empty.IsZero() {
		t.Fatalf("empty expires_at = %s, want zero", empty)
	}

	rfc3339, err := parseImportExpiresAtString("2026-05-30T01:59:28.123Z")
	if err != nil {
		t.Fatalf("RFC3339 expires_at should parse: %v", err)
	}
	wantRFC3339 := time.Date(2026, 5, 30, 1, 59, 28, 123_000_000, time.UTC)
	if !rfc3339.Equal(wantRFC3339) {
		t.Fatalf("RFC3339 expires_at = %s, want %s", rfc3339, wantRFC3339)
	}

	seconds, err := parseImportExpiresAtString("1770000000")
	if err != nil {
		t.Fatalf("unix seconds expires_at should parse: %v", err)
	}
	if !seconds.Equal(time.Unix(1770000000, 0)) {
		t.Fatalf("unix seconds expires_at = %s, want %s", seconds, time.Unix(1770000000, 0))
	}

	if _, err := parseImportExpiresAtString("tomorrow"); err == nil {
		t.Fatal("invalid expires_at should fail")
	}
}

func TestImportUnixTimeHandlesZeroSecondsAndMilliseconds(t *testing.T) {
	t.Parallel()

	if got := importUnixTime(0); !got.IsZero() {
		t.Fatalf("importUnixTime(0) = %s, want zero", got)
	}
	if got := importUnixTime(1770000000); !got.Equal(time.Unix(1770000000, 0)) {
		t.Fatalf("importUnixTime seconds = %s, want %s", got, time.Unix(1770000000, 0))
	}
	if got := importUnixTime(1770000000000); !got.Equal(time.UnixMilli(1770000000000)) {
		t.Fatalf("importUnixTime milliseconds = %s, want %s", got, time.UnixMilli(1770000000000))
	}
}

func TestMissingSub2APIFallbackMetadataWhenIDTokenAbsent(t *testing.T) {
	t.Parallel()

	account := Sub2APIAccount{
		Name: "user@example.com",
		Credentials: Sub2APICredentials{
			AccessToken:      "access",
			ChatGPTAccountID: "acct",
		},
	}

	if missing := missingSub2APIFallbackMetadata(account, account.Name, account.Credentials.ChatGPTAccountID); len(missing) != 0 {
		t.Fatalf("expected required fallback metadata to be complete, got %v", missing)
	}
}

func TestMissingSub2APIFallbackMetadataReportsAllMissingFields(t *testing.T) {
	t.Parallel()

	missing := missingSub2APIFallbackMetadata(Sub2APIAccount{}, "", "")
	assertOAuthMissingFields(t, missing, "email", "chatgpt_account_id")
}

func TestImportTokenMetadataMarksAccessTokenOnly(t *testing.T) {
	t.Parallel()

	meta := map[string]any{}
	applyImportTokenMetadata(meta, "")
	if meta["refreshable"] != false {
		t.Fatalf("expected refreshable false, got %#v", meta["refreshable"])
	}
	if meta["token_mode"] != "access_token_only" {
		t.Fatalf("expected access_token_only mode, got %#v", meta["token_mode"])
	}
	if meta["refresh_warning"] == "" {
		t.Fatalf("expected refresh warning")
	}
}

func TestImportTokenMetadataClearsWarningForRefreshableTokens(t *testing.T) {
	t.Parallel()

	meta := map[string]any{"refresh_warning": "old warning"}
	applyImportTokenMetadata(meta, "refresh-token")
	if meta["refreshable"] != true {
		t.Fatalf("expected refreshable true, got %#v", meta["refreshable"])
	}
	if meta["token_mode"] != "oauth_refresh" {
		t.Fatalf("expected oauth_refresh mode, got %#v", meta["token_mode"])
	}
	if _, ok := meta["refresh_warning"]; ok {
		t.Fatalf("refresh warning should be removed for refreshable token: %#v", meta)
	}
}

func TestMissingFlatTokenFallbackMetadataReportsIDTokenDerivedFields(t *testing.T) {
	t.Parallel()

	missing := missingFlatTokenFallbackMetadata("user@example.com", "acct", "", "", time.Unix(1770000000, 0))
	assertOAuthMissingFields(t, missing, "plan_type", "user_id")
}

func TestSmallImportHelpers(t *testing.T) {
	t.Parallel()

	values := map[string]any{"name": " agent "}
	if got := stringFromImportMap(values, "name"); got != "agent" {
		t.Fatalf("stringFromImportMap = %q, want agent", got)
	}
	if got := stringFromImportMap(nil, "name"); got != "" {
		t.Fatalf("nil stringFromImportMap = %q, want empty", got)
	}

	if got := importRefreshWarning("refresh"); got != "" {
		t.Fatalf("refreshable warning = %q, want empty", got)
	}
	if got := importRefreshWarning(" "); got == "" {
		t.Fatal("access-token-only import should expose a refresh warning")
	}
	if got := importResultStatus("connected"); got != "queued" {
		t.Fatalf("connected import status = %q, want queued", got)
	}
	if got := importResultStatus("disabled"); got != "failed" {
		t.Fatalf("disabled import status = %q, want failed", got)
	}
	if got := platformToProvider(" openai "); got != "codex" {
		t.Fatalf("openai platform provider = %q, want codex", got)
	}
	if got := platformToProvider("ANTIGRAVITY"); got != "antigravity" {
		t.Fatalf("antigravity platform provider = %q, want antigravity", got)
	}
	if got := platformToProvider("unknown"); got != "" {
		t.Fatalf("unknown platform provider = %q, want empty", got)
	}
	if ptr := boolPtr(true); ptr == nil || *ptr != true {
		t.Fatalf("boolPtr(true) = %#v", ptr)
	}
	if got := safeAccountIDPrefix(" 1234567890 "); got != "12345678" {
		t.Fatalf("safe account prefix = %q, want first 8 chars", got)
	}
	if got := oauthImportedDefaultBaseURL("antigravity"); got != antigravityDefaultBackendBaseURL {
		t.Fatalf("antigravity default base url = %q", got)
	}
	if got := oauthImportedDefaultBaseURL("codex"); got != codexDefaultBackendBaseURL {
		t.Fatalf("codex default base url = %q", got)
	}
}

func TestMergeAndDecodeIDTokenMetadata(t *testing.T) {
	t.Parallel()

	meta := map[string]any{"existing": true}
	mergeIDTokenMetadata(meta, map[string]any{
		"plan_type":                             " plus ",
		"chatgpt_user_id":                       " user_123 ",
		"chatgpt_subscription_active_start":     float64(1),
		"chatgpt_subscription_active_until":     float64(2),
		"chatgpt_subscription_active_ignored":   "ignored",
		"chatgpt_subscription_active_start_nil": nil,
	})
	if meta["plan_type"] != "plus" || meta["chatgpt_user_id"] != "user_123" {
		t.Fatalf("unexpected merged id token metadata: %#v", meta)
	}
	if meta["chatgpt_subscription_active_start"] != float64(1) || meta["chatgpt_subscription_active_until"] != float64(2) {
		t.Fatalf("expected subscription timestamps to merge: %#v", meta)
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte(`{
		"email":"user@example.com",
		"https://api.openai.com/auth":{
			"chatgpt_plan_type":"team",
			"chatgpt_user_id":"user_456",
			"chatgpt_account_id":"acct_456",
			"chatgpt_subscription_active_start":10,
			"chatgpt_subscription_active_until":20
		}
	}`))
	raw, claimsMeta := decodeIDToken("header." + payload + ".signature")
	if raw["email"] != "user@example.com" {
		t.Fatalf("unexpected raw id token payload: %#v", raw)
	}
	if claimsMeta["plan_type"] != "team" || claimsMeta["chatgpt_user_id"] != "user_456" || claimsMeta["chatgpt_account_id"] != "acct_456" {
		t.Fatalf("unexpected decoded claims metadata: %#v", claimsMeta)
	}

	raw, claimsMeta = decodeIDToken("invalid")
	if raw != nil || claimsMeta != nil {
		t.Fatalf("invalid id token should return nil maps, got %#v %#v", raw, claimsMeta)
	}
}

func TestCPAAccountIDAllowsAntigravityEmailFallback(t *testing.T) {
	t.Parallel()

	account := CPAExport{
		Type:      "antigravity",
		Email:     "dysongbo@gmail.com",
		ProjectID: "sound-spirit-123",
	}
	if got := cpaAccountID("antigravity", account); got != "dysongbo@gmail.com" {
		t.Fatalf("unexpected antigravity account id: got %q", got)
	}
	if got := cpaAccountID("codex", account); got != "" {
		t.Fatalf("expected codex to reject missing account_id, got %q", got)
	}
}

func TestCPARawProfileIncludesAntigravityProjectID(t *testing.T) {
	t.Parallel()

	profile := cpaRawProfile(CPAExport{
		Email:     "dysongbo@gmail.com",
		ProjectID: "sound-spirit-123",
	})
	if profile["email"] != "dysongbo@gmail.com" {
		t.Fatalf("unexpected email profile: %#v", profile)
	}
	if profile["project_id"] != "sound-spirit-123" {
		t.Fatalf("unexpected project profile: %#v", profile)
	}
}

func TestSmallImportHelperBoundaryBranches(t *testing.T) {
	t.Parallel()

	if got := importResultStatus(" connected "); got != "queued" {
		t.Fatalf("importResultStatus connected = %q, want queued", got)
	}
	if got := importResultStatus("disabled"); got != "failed" {
		t.Fatalf("importResultStatus disabled = %q, want failed", got)
	}
	if got := safeAccountIDPrefix(" acct "); got != "acct" {
		t.Fatalf("safeAccountIDPrefix short = %q, want acct", got)
	}
	if got := safeAccountIDPrefix("acct_123456789"); got != "acct_123" {
		t.Fatalf("safeAccountIDPrefix long = %q, want acct_123", got)
	}
	if got := cpaRawProfile(CPAExport{}); got != nil {
		t.Fatalf("empty CPA raw profile = %#v, want nil", got)
	}
}
