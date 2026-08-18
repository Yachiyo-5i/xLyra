package admin

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func TestAuthStateReturnsRegistrationStateWithoutSessionLookup(t *testing.T) {
	t.Parallel()

	queryCount := 0
	db := adminGormWithCallbacks(t, adminGormCallbacks{query: func(tx *gorm.DB) {
		queryCount++
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(fmt.Errorf("unexpected authentication state destination %T", tx.Statement.Dest))
			return
		}
		*count = 0
		tx.Statement.RowsAffected = 1
	}})
	handler := Handler{auth: auth.NewService(db, adminTestMasterKey)}
	rec := adminPerform(handler.AuthState, adminTestRequest(http.MethodGet, "/api/v1/auth/state", ""))

	adminAssertStatus(t, rec, http.StatusOK)
	assertAuthStateCacheHeaders(t, rec)
	body := adminDecodeJSON[struct {
		Initialized   bool `json:"initialized"`
		CanRegister   bool `json:"can_register"`
		Authenticated bool `json:"authenticated"`
		AdminCount    int  `json:"admin_count"`
	}](t, rec)
	if body.Initialized || !body.CanRegister || body.Authenticated || body.AdminCount != 0 {
		t.Fatalf("authentication state = %#v, want registration required", body)
	}
	if queryCount != 1 {
		t.Fatalf("query count = %d, want one bootstrap query", queryCount)
	}
}

func TestAuthStateReturnsAuthenticatedSessionWithoutBootstrapQuery(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	sessionID := uuid.New()
	expiresAt := time.Now().Add(time.Hour)
	queryCount := 0
	db := adminGormWithCallbacks(t, adminGormCallbacks{query: func(tx *gorm.DB) {
		queryCount++
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.AdminSession:
			*dest = []store.AdminSession{{ID: sessionID, AdminID: adminID, ExpiresAt: &expiresAt}}
		case *store.Admin:
			*dest = store.Admin{ID: adminID, Username: "owner", Role: "owner", Status: "active"}
		case *int64:
			tx.AddError(fmt.Errorf("bootstrap query should not run for an authenticated session"))
			return
		default:
			tx.AddError(fmt.Errorf("unexpected authentication state destination %T", tx.Statement.Dest))
			return
		}
		tx.Statement.RowsAffected = 1
	}})
	handler := Handler{auth: auth.NewService(db, adminTestMasterKey)}
	req := adminTestRequest(http.MethodGet, "/api/v1/auth/state", "")
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: "session-token"})
	rec := adminPerform(handler.AuthState, req)

	adminAssertStatus(t, rec, http.StatusOK)
	assertAuthStateCacheHeaders(t, rec)
	body := adminDecodeJSON[struct {
		Initialized   bool   `json:"initialized"`
		CanRegister   bool   `json:"can_register"`
		Authenticated bool   `json:"authenticated"`
		AuthType      string `json:"auth_type"`
		CSRFToken     string `json:"csrf_token"`
		Admin         struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"admin"`
	}](t, rec)
	if !body.Initialized || body.CanRegister || !body.Authenticated || body.AuthType != "session" {
		t.Fatalf("authentication state = %#v, want authenticated session", body)
	}
	if body.Admin.ID != adminID.String() || body.Admin.Username != "owner" || body.CSRFToken == "" {
		t.Fatalf("authenticated payload = %#v, want admin and CSRF token", body)
	}
	if queryCount != 2 {
		t.Fatalf("query count = %d, want session and admin queries", queryCount)
	}
}

func TestAuthStateRequiresAuthService(t *testing.T) {
	t.Parallel()

	rec := adminPerform((Handler{}).AuthState, adminTestRequest(http.MethodGet, "/api/v1/auth/state", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "auth_unavailable")
	assertAuthStateCacheHeaders(t, rec)
}

func assertAuthStateCacheHeaders(t *testing.T, rec interface{ Header() http.Header }) {
	t.Helper()

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	vary := rec.Header().Values("Vary")
	if !headerValuesContain(vary, "Cookie") || !headerValuesContain(vary, "X-Access-Token") {
		t.Fatalf("Vary = %v, want Cookie and X-Access-Token", vary)
	}
}

func headerValuesContain(values []string, target string) bool {
	for _, value := range values {
		for item := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(item), target) {
				return true
			}
		}
	}
	return false
}
