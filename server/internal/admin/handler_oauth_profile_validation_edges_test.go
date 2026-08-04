package admin

import (
	"net/http"
	"testing"
)

func TestStartAntigravityOAuthRequiresOAuthServiceBeforeDecodingRequest(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(
		handler.StartAntigravityOAuth,
		adminTestRequest(http.MethodPost, "/api/v1/oauth/providers/antigravity/authorize", `{`),
	)

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "oauth_service_unavailable")
}

func TestListAuditLogsRejectsInvalidSuccessFilterBeforeAuthService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(
		handler.ListAuditLogs,
		adminTestRequest(http.MethodGet, "/api/v1/audit-logs?success=not-bool", ""),
	)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_success")
}
