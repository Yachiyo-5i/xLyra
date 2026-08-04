package admin

import (
	"net/http"
	"strings"
	"testing"

	oauthsvc "xlyra/server/internal/oauth"
)

func TestShouldDowngradeImportedOAuthToAccessTokenOnly(t *testing.T) {
	t.Parallel()

	refreshable := true
	if shouldDowngradeImportedOAuthToAccessTokenOnly(&refreshable) {
		t.Fatal("refreshable imports should keep oauth_refresh mode even if a later refresh attempt fails")
	}

	accessTokenOnly := false
	if !shouldDowngradeImportedOAuthToAccessTokenOnly(&accessTokenOnly) {
		t.Fatal("imports without a refresh token should be marked access_token_only")
	}

	if shouldDowngradeImportedOAuthToAccessTokenOnly(nil) {
		t.Fatal("unknown refreshability should not clear an existing refresh token")
	}
}

func TestOAuthImportProxyIDReadsQueryFormAndDirect(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/oauth/import?proxy_id=%20proxy-main%20", "")
	if got := oauthImportProxyID(req); got != "proxy-main" {
		t.Fatalf("query proxy id = %q, want proxy-main", got)
	}

	req = adminTestRequest(http.MethodPost, "/api/v1/oauth/import", "proxy_id=form-proxy")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if got := oauthImportProxyID(req); got != "form-proxy" {
		t.Fatalf("form proxy id = %q, want form-proxy", got)
	}

	req = adminTestRequest(http.MethodPost, "/api/v1/oauth/import?proxy_id=direct", "")
	if got := oauthImportProxyID(req); got != "" {
		t.Fatalf("direct proxy id = %q, want empty", got)
	}
}

func TestRecomputeOAuthImportMetaCountsQueuedAsAccepted(t *testing.T) {
	t.Parallel()

	result := oauthsvc.ImportResult{
		Meta: oauthsvc.ImportMeta{
			Total:    99,
			Accepted: 99,
			Queued:   99,
		},
		Items: []oauthsvc.ImportAccountResult{
			{Status: "queued"},
			{Status: "failed"},
			{Status: "skipped"},
		},
	}

	recomputeOAuthImportMeta(&result)

	if result.Meta.Total != 3 || result.Meta.Accepted != 1 || result.Meta.Queued != 1 || result.Meta.Succeeded != 1 || result.Meta.Failed != 2 {
		t.Fatalf("unexpected recomputed import meta: %#v", result.Meta)
	}
}

func TestOAuthImportSmallValueHelpers(t *testing.T) {
	t.Parallel()

	truePtr := boolPtr(true)
	if truePtr == nil || *truePtr != true {
		t.Fatalf("boolPtr(true) = %#v", truePtr)
	}
	falsePtr := boolPtr(false)
	if falsePtr == nil || *falsePtr != false {
		t.Fatalf("boolPtr(false) = %#v", falsePtr)
	}
	if warning := oauthAccessTokenOnlyWarning(); !strings.Contains(warning, "access token") {
		t.Fatalf("access-token-only warning = %q, want access token guidance", warning)
	}
}
