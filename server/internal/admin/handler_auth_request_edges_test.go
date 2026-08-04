package admin

import (
	"net/http"
	"testing"

	"xlyra/server/internal/auth"
)

func TestAuthHandlersRejectInvalidJSONBeforeServiceWork(t *testing.T) {
	t.Parallel()

	handler := Handler{auth: &auth.Service{}}
	for _, tc := range []struct {
		name   string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "bootstrap_register", target: "/api/v1/bootstrap/register", call: handler.BootstrapRegister},
		{name: "create_session", target: "/api/v1/auth/session", call: handler.CreateSession},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(http.MethodPost, tc.target, `{`))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
		})
	}
}

func TestCurrentSessionRejectsNilAdminActorBeforeServiceWork(t *testing.T) {
	t.Parallel()

	handler := Handler{auth: &auth.Service{}}
	req := adminTestRequest(http.MethodGet, "/api/v1/auth/session", "")
	req = req.WithContext(auth.WithAdminActor(req.Context(), auth.AdminActor{Type: "session"}))
	rec := adminPerform(handler.CurrentSession, req)

	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}
