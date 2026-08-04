package admin

import (
	"database/sql"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestGetProfileRequiresAdminSession(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.GetProfile, adminTestRequest(http.MethodGet, "/api/v1/profile", ""))

	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestDeleteProfileSessionRequiresAdminSession(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminRequestWithRouteParam(http.MethodDelete, "/api/v1/profile/sessions/bad-id", "", "sessionID", "bad-id")
	rec := adminPerform(handler.DeleteProfileSession, req)

	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestAuditLogFiltersRejectInvalidQueryValues(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name   string
		target string
		code   string
	}{
		{name: "success", target: "/api/v1/audit-logs?success=maybe", code: "invalid_success"},
		{name: "date_from", target: "/api/v1/audit-logs?date_from=not-a-date", code: "invalid_date_from"},
		{name: "date_to", target: "/api/v1/audit-logs?date_to=not-a-date", code: "invalid_date_to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodGet, tc.target, "")
			rec := adminRecorder()

			_, ok := handler.auditLogFiltersFromRequest(rec, req)

			adminAssertParserError(t, rec, ok, tc.code)
		})
	}
}

func TestAuditLogFiltersApplyDefaultsAndParseValues(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminTestRequest(http.MethodGet, "/api/v1/audit-logs?action=login&actor_type=session&page=2&page_size=25&success=true&date_from=2026-06-21T00:00:00Z&date_to=2026-06-22T00:00:00Z", "")
	rec := adminRecorder()

	filters, ok := handler.auditLogFiltersFromRequest(rec, req)

	adminRequireParserOK(t, rec, ok, "audit filters")
	if filters.Action != "login" || filters.ActorType != "session" || filters.Page != 2 || filters.PageSize != 25 {
		t.Fatalf("unexpected audit filters: %#v", filters)
	}
	if filters.Success == nil || *filters.Success != true {
		t.Fatalf("success filter = %#v, want true", filters.Success)
	}
	if filters.DateFrom == nil || filters.DateTo == nil {
		t.Fatalf("expected date filters, got %#v", filters)
	}

	req = adminTestRequest(http.MethodGet, "/api/v1/audit-logs?page=0&page_size=0", "")
	rec = adminRecorder()
	filters, ok = handler.auditLogFiltersFromRequest(rec, req)
	if !ok || filters.Page != 1 || filters.PageSize != 50 {
		t.Fatalf("default audit filters = %#v ok=%v", filters, ok)
	}
}

func TestProfilePayloadHelpers(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	sessionID := uuid.New()
	accessTokenID := uuid.New()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)

	sessionPayload := adminSessionPayload(store.AdminSession{
		ID:        sessionID,
		AdminID:   adminID,
		ExpiresAt: &now,
		LastSeenAt: sql.NullTime{
			Time:  now,
			Valid: true,
		},
		IPAddress: store.IPAddress(net.ParseIP("127.0.0.1")),
		UserAgent: "Codex",
		CreatedAt: now,
		UpdatedAt: now,
	}, true)
	if sessionPayload["id"] != sessionID.String() || sessionPayload["admin_id"] != adminID.String() || sessionPayload["current"] != true {
		t.Fatalf("unexpected session payload: %#v", sessionPayload)
	}
	if sessionPayload["ip_address"] != "127.0.0.1" {
		t.Fatalf("session ip = %#v, want 127.0.0.1", sessionPayload["ip_address"])
	}

	accessPayload := adminAccessTokenPayload(store.AdminAccessToken{
		ID:                accessTokenID,
		AdminID:           adminID,
		MaskedToken:       "xlyra-admin-****",
		Enabled:           true,
		LastUsedAt:        sql.NullTime{Time: now, Valid: true},
		LastUsedIP:        store.IPAddress(net.ParseIP("10.0.0.1")),
		LastUsedUserAgent: "Browser",
		CreatedAt:         now,
		UpdatedAt:         now,
	}, true)
	if accessPayload["id"] != accessTokenID.String() || accessPayload["token_returned_once"] != true || accessPayload["last_used_ip"] != "10.0.0.1" {
		t.Fatalf("unexpected access token payload: %#v", accessPayload)
	}

	auditPayload := auditLogPayload(store.AdminAuditLog{
		ID:         uuid.New(),
		ActorType:  "session",
		Action:     "profile.update",
		Success:    true,
		IPAddress:  store.IPAddress(net.ParseIP("127.0.0.1")),
		Metadata:   store.JSON(`{"field":"nickname"}`),
		CreatedAt:  now,
		ResourceID: "resource-id",
	})
	metadata, ok := auditPayload["metadata"].(map[string]any)
	if !ok || metadata["field"] != "nickname" {
		t.Fatalf("unexpected audit metadata payload: %#v", auditPayload["metadata"])
	}
	if auditPayload["ip_address"] != "127.0.0.1" || auditPayload["success"] != true {
		t.Fatalf("unexpected audit payload: %#v", auditPayload)
	}
}
