package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestCreateAdminRejectsBlankUsernameBeforeHashingOrRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.CreateAdmin(context.Background(), " \t\n ", "Admin123", "")
	if err == nil || err.Error() != "username is required" {
		t.Fatalf("CreateAdmin error = %v, want username is required", err)
	}
}

func TestBootstrapAdminRejectsBlankUsernameBeforeHashingOrRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.BootstrapAdmin(context.Background(), " \t\n ", "Admin123", "", "", "", "")
	if err == nil || err.Error() != "username is required" {
		t.Fatalf("BootstrapAdmin error = %v, want username is required", err)
	}
}

func TestRequireAPIKeyUnauthorizedResponseDoesNotCallNext(t *testing.T) {
	t.Parallel()

	service := &Service{}
	called := false
	handler := service.RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := authPerform(handler, authTestRequest(http.MethodGet, "/v1/models"))

	if called {
		t.Fatal("next handler was called for a missing api key")
	}
	assertAuthJSONError(t, rec, http.StatusUnauthorized, "unauthorized", "valid api key is required")
}

func TestRequireAPIKeyReportsDailyQuotaExhaustionAsUnauthorized(t *testing.T) {
	t.Parallel()

	now := time.Now()
	timeZone := config.ResolveTimeZone()
	dailyStart := timeZone.StartOfDay(now)
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*item = store.APIKey{
			ID:                    uuid.New(),
			Status:                "active",
			QuotaUnlimited:        true,
			QuotaDailyLimit:       sql.NullFloat64{Float64: 10, Valid: true},
			QuotaDailyUsed:        10,
			QuotaDailyUnlimited:   false,
			QuotaDailyWindowStart: &dailyStart,
			QuotaWeeklyUnlimited:  true,
		}
		tx.Statement.RowsAffected = 1
	})
	handler := service.RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := authTestRequest(http.MethodPost, "/v1/chat/completions")
	req.Header.Set("Authorization", "Bearer custom-exhausted-key")
	req = req.WithContext(context.WithValue(req.Context(), chimiddleware.RequestIDKey, "req-quota"))
	rec := authPerform(handler, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode quota response: %v", err)
	}
	if body.Error["type"] != "authentication_error" || body.Error["code"] != "api_key_daily_quota_exhausted" || body.Error["scope"] != "daily" {
		t.Fatalf("quota error = %#v", body.Error)
	}
	if body.Error["param"] != nil || body.Error["request_id"] != "req-quota" || body.Error["reset_at"] == nil {
		t.Fatalf("quota error metadata = %#v", body.Error)
	}
}

func TestRequireAPIKeyIdentityAllowsExhaustedActiveKey(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		*item = store.APIKey{
			ID:             apiKeyID,
			Status:         "active",
			QuotaLimit:     sql.NullFloat64{Float64: 10, Valid: true},
			QuotaUsed:      12,
			QuotaUnlimited: false,
		}
		tx.Statement.RowsAffected = 1
	})
	authReplaceUpdateCallback(t, service.db, func(tx *gorm.DB) {
		t.Fatalf("identity lookup must not touch last_used_at (unexpected update: %#v)", tx.Statement.Dest)
	})

	called := false
	handler := service.RequireAPIKeyIdentity(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		apiKey, ok := APIKeyFromContext(r.Context())
		if !ok || apiKey.ID != apiKeyID {
			t.Fatalf("api key context = %#v ok %v, want %s", apiKey, ok, apiKeyID)
		}
		if apiKey.LastUsedAt != nil {
			t.Fatalf("identity lookup set LastUsedAt = %v, want nil", apiKey.LastUsedAt)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := authTestRequest(http.MethodGet, "/v1/user/balance")
	req.Header.Set("Authorization", "Bearer exhausted-key")
	rec := authPerform(handler, req)

	if !called {
		t.Fatal("next handler was not called")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestRequireAdminSessionUnauthorizedResponseDoesNotCallNext(t *testing.T) {
	t.Parallel()

	service := &Service{}
	called := false
	handler := service.RequireAdminSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := authPerform(handler, authTestRequest(http.MethodGet, "/api/admin/profile"))

	if called {
		t.Fatal("next handler was called for missing admin credentials")
	}
	assertAuthJSONError(t, rec, http.StatusUnauthorized, "unauthorized", "admin session is required")
}

func TestRequireAdminSessionTreatsBlankAccessTokenHeaderAsMissingCredentials(t *testing.T) {
	t.Parallel()

	service := &Service{}
	called := false
	handler := service.RequireAdminSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := authTestRequest(http.MethodGet, "/api/admin/profile")
	req.Header.Set("X-Access-Token", " \t\n ")
	rec := authPerform(handler, req)

	if called {
		t.Fatal("next handler was called for blank access token credentials")
	}
	assertAuthJSONError(t, rec, http.StatusUnauthorized, "unauthorized", "admin session is required")
}

func TestAdminSessionTokenFromRequestTrimsCookieToEmpty(t *testing.T) {
	t.Parallel()

	req := authTestRequest(http.MethodGet, "/")
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: " \t\n "})

	if got := AdminSessionTokenFromRequest(req); got != "" {
		t.Fatalf("AdminSessionTokenFromRequest blank cookie = %q, want empty", got)
	}
}

func TestAPIKeyFromRequestFallsBackWhenBearerValueTrimsToEmpty(t *testing.T) {
	t.Parallel()

	req := authTestRequest(http.MethodPost, "/v1/chat/completions")
	req.Header.Set("Authorization", "Bearer \t\n ")
	req.Header.Set("X-API-Key", " fallback-key ")

	if got := apiKeyFromRequest(req); got != "fallback-key" {
		t.Fatalf("apiKeyFromRequest = %q, want fallback-key", got)
	}
}

func TestBearerTokenDoesNotTrimBeforePrefixCheck(t *testing.T) {
	t.Parallel()

	for _, value := range []string{" Bearer token-value", "\tBearer token-value", strings.ToLower("Bearer token-value")} {
		if got := bearerToken(value); got != "" {
			t.Fatalf("bearerToken(%q) = %q, want empty", value, got)
		}
	}
}
