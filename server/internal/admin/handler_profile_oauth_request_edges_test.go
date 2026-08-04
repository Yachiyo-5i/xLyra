package admin

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/downloads"
	"xlyra/server/internal/store"
)

func TestProfileAccessTokenGetReturnsNullOrMaskedToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	item := store.AdminAccessToken{
		ID:          uuid.New(),
		AdminID:     uuid.New(),
		MaskedToken: "xlyra_admin_****",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	for _, tc := range []struct {
		name      string
		tokens    []store.AdminAccessToken
		wantNil   bool
		wantToken string
	}{
		{name: "not found returns explicit null", wantNil: true},
		{name: "found returns redacted token", tokens: []store.AdminAccessToken{item}, wantToken: item.MaskedToken},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := offlineAccessTokenHandler(t, tc.tokens)
			rec := adminPerform(
				handler.GetProfileAccessToken,
				adminTestRequest(http.MethodGet, "/api/v1/profile/access-token", ""),
			)

			adminAssertStatus(t, rec, http.StatusOK)
			body := adminDecodeJSON[map[string]any](t, rec)
			if tc.wantNil {
				if body["access_token"] != nil {
					t.Fatalf("access_token = %#v, want nil", body["access_token"])
				}
				return
			}
			payload, ok := body["access_token"].(map[string]any)
			if !ok {
				t.Fatalf("access_token payload = %#v, want object", body["access_token"])
			}
			if payload["id"] != item.ID.String() || payload["token"] != nil || payload["masked_token"] != tc.wantToken {
				t.Fatalf("access token payload = %#v", payload)
			}
		})
	}
}

func TestProfileAccessTokenAndOAuthExportValidateRequestBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		code   string
	}{
		{
			name:   "enabled update invalid json",
			method: http.MethodPatch,
			target: "/api/v1/profile/access-token",
			body:   `{"enabled":`,
			call:   handler.UpdateProfileAccessTokenEnabled,
			code:   "invalid_json",
		},
		{
			name:   "oauth export invalid connection id after services available",
			method: http.MethodPost,
			target: "/api/v1/oauth/connections/not-a-uuid/export",
			call:   Handler{oauth: adminOAuthService(), downloads: downloads.NewService()}.ExportOAuthConnection,
			code:   "invalid_connection_id",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(tc.method, tc.target, tc.body)
			if strings.Contains(tc.target, "/oauth/connections/") {
				req = adminRequestWithRouteParam(tc.method, tc.target, tc.body, "connectionID", "not-a-uuid")
			}
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestOAuthRequestPublicBaseURLUsesTLSAndRequiresHost(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/oauth/providers/codex/callback", "")
	req.Host = "tls.example.test"
	req.TLS = &tls.ConnectionState{}
	if got := requestPublicBaseURL(req); got != "https://tls.example.test" {
		t.Fatalf("TLS requestPublicBaseURL = %q, want https://tls.example.test", got)
	}

	req = adminTestRequest(http.MethodGet, "/api/v1/oauth/providers/codex/callback", "")
	req.Host = ""
	if got := requestPublicBaseURL(req); got != "" {
		t.Fatalf("hostless requestPublicBaseURL = %q, want empty", got)
	}
}

func offlineAccessTokenHandler(t *testing.T, tokens []store.AdminAccessToken) Handler {
	t.Helper()

	db := adminOfflineGorm(t, func(db *gorm.DB) error {
		return db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.AdminAccessToken:
				*dest = append([]store.AdminAccessToken(nil), tokens...)
				tx.RowsAffected = int64(len(tokens))
				tx.Statement.RowsAffected = int64(len(tokens))
			default:
				tx.AddError(fmt.Errorf("unexpected access token query destination %T", tx.Statement.Dest))
			}
		})
	})
	return Handler{auth: auth.NewService(db, adminTestMasterKey)}
}
