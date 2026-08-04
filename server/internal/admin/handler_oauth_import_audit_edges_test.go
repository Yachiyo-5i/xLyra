package admin

import (
	"net/http"
	"testing"
)

func TestRecordAuditSkipsWithoutAuthService(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/profile", "")

	Handler{}.recordAudit(req, currentAdminActor(req), "profile.update", "admin", "admin-id", true, "", map[string]any{
		"field": "nickname",
	})
}

func TestShouldAuditAdminMutationMethods(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !shouldAuditAdminMutation(method) {
			t.Fatalf("shouldAuditAdminMutation(%q) = false, want true", method)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, "post"} {
		if shouldAuditAdminMutation(method) {
			t.Fatalf("shouldAuditAdminMutation(%q) = true, want false", method)
		}
	}
}

func TestSiteAPIKeyDefaultNameBoundsIndex(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		index int
		want  string
	}{
		{index: -10, want: "Apikey 1"},
		{index: 0, want: "Apikey 1"},
		{index: 2, want: "Apikey 3"},
	} {
		if got := siteAPIKeyDefaultName(tc.index); got != tc.want {
			t.Fatalf("siteAPIKeyDefaultName(%d) = %q, want %q", tc.index, got, tc.want)
		}
	}
}

func TestImportOAuthAccountsRequiresOAuthServiceBeforeBody(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/oauth/import", `{`)
	req.Header.Set("Content-Type", "application/json")
	rec := adminPerform(Handler{}.ImportOAuthAccounts, req)

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "oauth_service_unavailable")
}

func TestCompleteOAuthCallbackRequiresServicesBeforeProvider(t *testing.T) {
	t.Parallel()

	req := adminRequestWithRouteParam(http.MethodPost, "/api/v1/oauth/providers/unsupported/callback/complete", `{`, "provider", "unsupported")
	rec := adminPerform(Handler{}.CompleteOAuthCallbackURL, req)

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "oauth_service_unavailable")
}
