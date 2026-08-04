package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authcrypto "xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

func TestRequireAdminSessionAcceptsCookieSessionOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	sessionID := uuid.New()
	touchCount := 0
	service := NewService(authPostgresGorm(t), "test-master-key")
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		expiresAt := time.Now().Add(time.Hour)
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.AdminSession:
			*dest = []store.AdminSession{{
				ID:               sessionID,
				AdminID:          adminID,
				SessionTokenHash: hashToken("admin-session-token"),
				ExpiresAt:        &expiresAt,
			}}
			tx.Statement.RowsAffected = 1
		case *store.AdminSession:
			*dest = store.AdminSession{
				ID:               sessionID,
				AdminID:          adminID,
				SessionTokenHash: hashToken("admin-session-token"),
				ExpiresAt:        &expiresAt,
			}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected session query destination"))
		}
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		touchCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}

	req := authTestRequest(http.MethodGet, "/admin")
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: "admin-session-token"})
	rec := authRecorder()
	called := false
	service.RequireAdminSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		session, ok := AdminSessionFromContext(r.Context())
		if !ok || session.ID != sessionID || session.AdminID != adminID {
			t.Fatalf("admin session context = %#v/%v, want created session", session, ok)
		}
		actor, ok := AdminActorFromContext(r.Context())
		if !ok || actor.Type != "session" || actor.SessionID != sessionID || actor.AdminID != adminID {
			t.Fatalf("admin actor context = %#v/%v, want session actor", actor, ok)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("handler called/code = %v/%d, want successful pass-through", called, rec.Code)
	}
	if touchCount != 1 {
		t.Fatalf("touch count = %d, want session touch", touchCount)
	}
}

func TestRequireAdminSessionAcceptsAccessTokenAndAuditsOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	accessTokenID := uuid.New()
	var createdAudit store.AdminAuditLog
	service := NewService(authPostgresGorm(t), "test-master-key")
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.AdminAccessToken:
			*dest = store.AdminAccessToken{ID: accessTokenID, AdminID: adminID, Enabled: true}
			tx.Statement.RowsAffected = 1
		case *int64:
			*dest = 1
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected access token query destination"))
		}
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		audit, ok := tx.Statement.Dest.(*store.AdminAuditLog)
		if !ok {
			tx.AddError(errors.New("unexpected audit create destination"))
			return
		}
		createdAudit = *audit
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}

	req := authTestRequest(http.MethodPost, "/admin/api")
	req.Header.Set("X-Access-Token", "admin-access-token")
	req.Header.Set("User-Agent", "admin-api-agent")
	rec := authRecorder()
	called := false
	service.RequireAdminSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		actor, ok := AdminActorFromContext(r.Context())
		if !ok || actor.Type != "access_token" || actor.AdminID != adminID || actor.AccessTokenID != accessTokenID {
			t.Fatalf("admin actor context = %#v/%v, want access-token actor", actor, ok)
		}
		w.WriteHeader(http.StatusAccepted)
	})).ServeHTTP(rec, req)

	if !called || rec.Code != http.StatusAccepted {
		t.Fatalf("handler called/code = %v/%d, want access-token pass-through", called, rec.Code)
	}
	if createdAudit.ActorType != "access_token" || !createdAudit.Success || createdAudit.Action != "admin_api.access" || createdAudit.UserAgent != "admin-api-agent" {
		t.Fatalf("created audit = %#v, want successful access audit", createdAudit)
	}
}

func TestRequireAdminSessionRejectsMissingCredentialsOffline(t *testing.T) {
	t.Parallel()

	service := NewService(authPostgresGorm(t), "test-master-key")
	called := false
	handler := service.RequireAdminSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	rec := authPerform(handler, authTestRequest(http.MethodGet, "/admin"))

	if called {
		t.Fatal("handler was called without admin credentials")
	}
	assertAuthJSONError(t, rec, http.StatusUnauthorized, "unauthorized", "admin session is required")
}

func TestAdminServiceValidationGuardsOffline(t *testing.T) {
	t.Parallel()

	service := NewService(authPostgresGorm(t), "test-master-key")
	ctx := context.Background()

	_, err := service.CreateAdmin(ctx, "root", "short", "")
	assertAuthErrorContains(t, "CreateAdmin short password", err, "at least")
	_, err = service.BootstrapAdmin(ctx, "root", "root", "", "", "", "")
	assertAuthErrorContains(t, "BootstrapAdmin password", err, "at least")
	if _, err := service.CreateAPIKey(ctx, CreateAPIKeyInput{Name: " \t "}, uuid.New()); err == nil || err.Error() != "api key name is required" {
		t.Fatalf("CreateAPIKey blank name error = %v, want name required", err)
	}
	if key, kind, err := service.createGatewayAPIKeyValue(ctx, "bad key with spaces"); key != "" || kind != "" {
		t.Fatalf("createGatewayAPIKeyValue = %q/%q/%v, want custom-key validation error", key, kind, err)
	} else {
		assertAuthErrorContains(t, "createGatewayAPIKeyValue", err, "custom api key")
	}
}

func TestLoginAndPasswordErrorsOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	passwordHash, err := authcrypto.HashPassword("current-password-123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	service := NewService(authPostgresGorm(t), "test-master-key")
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		admin, ok := tx.Statement.Dest.(*store.Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin query destination"))
			return
		}
		*admin = store.Admin{ID: adminID, Username: "root", PasswordHash: passwordHash, Status: "disabled"}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.AddError(errors.New("disabled login should not create a session"))
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}

	if _, err := service.Login(context.Background(), "root", "current-password-123", "", "agent", "127.0.0.1"); err == nil || err.Error() != "invalid credentials" {
		t.Fatalf("Login disabled admin error = %v, want invalid credentials", err)
	}
	if _, err := service.ChangeAdminPassword(context.Background(), adminID, "wrong-password", "new-password-123", uuid.New()); err == nil || err.Error() != "current password is invalid" {
		t.Fatalf("ChangeAdminPassword wrong current error = %v, want current password invalid", err)
	}
}

func TestRecordAuditNilServiceIsNoop(t *testing.T) {
	t.Parallel()

	var service *Service
	service.RecordAudit(context.Background(), AuditInput{Action: "audit.noop"})
}
