package admin

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

func TestProfileHandlersRequireCurrentAdmin(t *testing.T) {
	t.Parallel()

	handler := Handler{auth: adminAuthService()}
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "update account", method: http.MethodPatch, target: "/api/v1/profile/account", body: `{"username":"alice"}`, call: handler.UpdateProfileAccount},
		{name: "update password", method: http.MethodPatch, target: "/api/v1/profile/password", body: `{"current_password":"old","new_password":"new"}`, call: handler.UpdateProfilePassword},
		{name: "list sessions", method: http.MethodGet, target: "/api/v1/profile/sessions", call: handler.ListProfileSessions},
		{name: "delete session", method: http.MethodDelete, target: "/api/v1/profile/sessions/" + uuid.NewString(), call: handler.DeleteProfileSession},
		{name: "enable totp", method: http.MethodPost, target: "/api/v1/profile/totp/enable", body: `{"code":"123456"}`, call: handler.EnableProfileTOTP},
		{name: "disable totp", method: http.MethodPost, target: "/api/v1/profile/totp/disable", body: `{"current_password":"pw","code":"123456"}`, call: handler.DisableProfileTOTP},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(tc.method, tc.target, tc.body))

			assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestProfileHandlersRejectInvalidJSONAfterCurrentAdmin(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	handler := profileAuthHandlerWithCurrentAdmin(t, store.Admin{ID: adminID, Username: "alice", Role: "owner", Status: "active"})
	for _, tc := range []struct {
		name   string
		method string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "update account", method: http.MethodPatch, target: "/api/v1/profile/account", call: handler.UpdateProfileAccount},
		{name: "update password", method: http.MethodPatch, target: "/api/v1/profile/password", call: handler.UpdateProfilePassword},
		{name: "enable totp", method: http.MethodPost, target: "/api/v1/profile/totp/enable", call: handler.EnableProfileTOTP},
		{name: "disable totp", method: http.MethodPost, target: "/api/v1/profile/totp/disable", call: handler.DisableProfileTOTP},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(tc.method, tc.target, `{`)
			req = req.WithContext(auth.WithAdminActor(req.Context(), auth.AdminActor{Type: "session", AdminID: adminID, SessionID: uuid.New()}))
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
		})
	}
}

func TestProfileAuthDeleteHandlersReturnDatabaseErrors(t *testing.T) {
	t.Parallel()

	handler := Handler{auth: auth.NewService(profileAuthDeleteErrorDB(t), adminTestMasterKey)}
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		req    func() *http.Request
		call   func(http.ResponseWriter, *http.Request)
		status int
		code   string
	}{
		{
			name:   "delete access token",
			method: http.MethodDelete,
			target: "/api/v1/profile/access-token",
			call:   handler.DeleteProfileAccessToken,
			status: http.StatusInternalServerError,
			code:   "access_token_delete_failed",
		},
		{
			name:   "delete auth session with cookie",
			method: http.MethodDelete,
			target: "/api/v1/auth/session",
			req: func() *http.Request {
				req := adminTestRequest(http.MethodDelete, "/api/v1/auth/session", "")
				req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: "session-token"})
				return req
			},
			call:   handler.DeleteSession,
			status: http.StatusInternalServerError,
			code:   "logout_failed",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(tc.method, tc.target, tc.body)
			if tc.req != nil {
				req = tc.req()
			}
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestProfileSessionRejectsInvalidRouteIDAfterAdmin(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	handler := profileAuthHandlerWithCurrentAdmin(t, store.Admin{ID: adminID, Username: "alice", Role: "owner", Status: "active"})
	req := adminRequestWithRouteParam(http.MethodDelete, "/api/v1/profile/sessions/not-a-uuid", "", "sessionID", "not-a-uuid")
	req = req.WithContext(auth.WithAdminActor(req.Context(), auth.AdminActor{Type: "session", AdminID: adminID, SessionID: uuid.New()}))
	rec := adminPerform(handler.DeleteProfileSession, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_session_id")
}

func TestOAuthConnectionHandlersReturnGuardErrors(t *testing.T) {
	t.Parallel()

	validConnectionID := uuid.NewString()
	for _, tc := range []struct {
		name    string
		handler Handler
		req     *http.Request
		call    func(Handler, http.ResponseWriter, *http.Request)
		status  int
		code    string
	}{
		{
			name:    "list requires oauth service",
			handler: Handler{},
			req:     adminTestRequest(http.MethodGet, "/api/v1/oauth/connections", ""),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.ListOAuthConnections(w, r) },
			status:  http.StatusServiceUnavailable,
			code:    "oauth_service_unavailable",
		},
		{
			name:    "refresh requires oauth service before id parse",
			handler: Handler{},
			req:     adminRequestWithRouteParam(http.MethodPost, "/api/v1/oauth/connections/not-a-uuid/refresh", "", "connectionID", "not-a-uuid"),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.RefreshOAuthConnection(w, r) },
			status:  http.StatusServiceUnavailable,
			code:    "oauth_service_unavailable",
		},
		{
			name:    "update model requires sites service before id parse",
			handler: Handler{oauth: adminOAuthService()},
			req:     adminRequestWithRouteParam(http.MethodPut, "/api/v1/oauth/connections/not-a-uuid/models", `{"enabled":true}`, "connectionID", "not-a-uuid"),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.UpdateOAuthConnectionModel(w, r) },
			status:  http.StatusServiceUnavailable,
			code:    "oauth_service_unavailable",
		},
		{
			name:    "update model rejects invalid id",
			handler: Handler{oauth: adminOAuthService(), sites: adminSiteService()},
			req:     adminRequestWithRouteParam(http.MethodPut, "/api/v1/oauth/connections/not-a-uuid/models", `{"enabled":true}`, "connectionID", "not-a-uuid"),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.UpdateOAuthConnectionModel(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_connection_id",
		},
		{
			name:    "update model rejects invalid json after id parse",
			handler: Handler{oauth: adminOAuthService(), sites: adminSiteService()},
			req:     adminRequestWithRouteParam(http.MethodPut, "/api/v1/oauth/connections/"+validConnectionID+"/models", `{"enabled":`, "connectionID", validConnectionID),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.UpdateOAuthConnectionModel(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_json",
		},
		{
			name:    "update model missing enabled",
			handler: Handler{oauth: adminOAuthService(), sites: adminSiteService()},
			req:     adminRequestWithRouteParam(http.MethodPut, "/api/v1/oauth/connections/"+validConnectionID+"/models", `{"model":"gpt-5"}`, "connectionID", validConnectionID),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.UpdateOAuthConnectionModel(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_enabled",
		},
		{
			name:    "refresh returns service error",
			handler: Handler{oauth: oauthsvc.NewService(profileAuthStoreWithQuery(t, func(tx *gorm.DB) { tx.AddError(fmt.Errorf("connection lookup failed")) }), adminTestMasterKey)},
			req:     adminRequestWithRouteParam(http.MethodPost, "/api/v1/oauth/connections/"+validConnectionID+"/refresh", "", "connectionID", validConnectionID),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.RefreshOAuthConnection(w, r) },
			status:  http.StatusBadRequest,
			code:    "oauth_connection_refresh_failed",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(
				func(w http.ResponseWriter, r *http.Request) { tc.call(tc.handler, w, r) },
				tc.req,
			)

			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func profileAuthHandlerWithCurrentAdmin(t *testing.T, admin store.Admin) Handler {
	t.Helper()

	db := profileAuthGormDB(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Admin:
			*dest = admin
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(fmt.Errorf("unexpected profile auth query destination %T", tx.Statement.Dest))
		}
	})
	return Handler{auth: auth.NewService(db, adminTestMasterKey)}
}

func profileAuthDeleteErrorDB(t *testing.T) *gorm.DB {
	t.Helper()

	return profileAuthGormDB(t, func(tx *gorm.DB) {
		tx.AddError(fmt.Errorf("unexpected profile auth query destination %T", tx.Statement.Dest))
	})
}

func profileAuthStoreWithQuery(t *testing.T, query func(*gorm.DB)) *store.Store {
	t.Helper()

	return adminStoreWithCallbacks(t, adminGormCallbacks{
		query: query,
		deleteCallback: func(tx *gorm.DB) {
			tx.AddError(fmt.Errorf("delete failed"))
		},
	})
}

func profileAuthGormDB(t *testing.T, query func(*gorm.DB)) *gorm.DB {
	t.Helper()

	return adminGormWithCallbacks(t, adminGormCallbacks{
		query: query,
		deleteCallback: func(tx *gorm.DB) {
			tx.AddError(fmt.Errorf("delete failed"))
		},
	})
}
