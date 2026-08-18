package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestIssueAdminSessionCreatesTokenAndPersistsSessionOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	sessionID := uuid.New()
	var created store.AdminSession
	service := authServiceWithGormCallbacks(t, nil, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.AdminSession)
		if !ok {
			tx.AddError(errors.New("unexpected admin session create destination"))
			return
		}
		created = *session
		session.ID = sessionID
		tx.Statement.RowsAffected = 1
	}, nil)

	result, err := service.issueAdminSession(context.Background(), store.Admin{ID: adminID, Username: "root"}, "agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("issueAdminSession returned error: %v", err)
	}
	if !strings.HasPrefix(result.Token, "xlyra_session_") || result.CSRFToken == "" {
		t.Fatalf("session token/csrf = %q/%q, want generated values", result.Token, result.CSRFToken)
	}
	if result.SessionID != sessionID || result.Admin.ID != adminID || result.ExpiresAt == nil {
		t.Fatalf("login result = %#v, want created session details", result)
	}
	if created.AdminID != adminID || created.SessionTokenHash == "" || created.UserAgent != "agent" || created.IPAddress.NetIP().String() != "127.0.0.1" {
		t.Fatalf("created session = %#v, want populated create params", created)
	}
}

func TestIssueAdminSessionPropagatesCreateErrorOffline(t *testing.T) {
	t.Parallel()

	createErr := errors.New("create session stopped")
	service := authServiceWithGormCallbacks(t, nil, func(tx *gorm.DB) {
		tx.AddError(createErr)
	}, nil)

	result, err := service.issueAdminSession(context.Background(), store.Admin{ID: uuid.New()}, "agent", "127.0.0.1")
	if result != (LoginResult{}) {
		t.Fatalf("issueAdminSession result = %#v, want zero on create error", result)
	}
	if err == nil || !strings.Contains(err.Error(), "create admin session") || !errors.Is(err, createErr) {
		t.Fatalf("issueAdminSession error = %v, want wrapped create error", err)
	}
}

func TestTouchAdminSessionUsesOneConditionalUpdatePerTouchOffline(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	queryCount := 0
	updateCount := 0
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		queryCount++
		tx.AddError(errors.New("touch should not query before updating"))
	}, nil, func(tx *gorm.DB) {
		updateCount++
		tx.Statement.RowsAffected = 1
	})

	service.TouchAdminSession(context.Background(), sessionID)
	service.TouchAdminSession(context.Background(), sessionID)

	if queryCount != 0 {
		t.Fatalf("query count = %d, want no touch lookups", queryCount)
	}
	if updateCount != 2 {
		t.Fatalf("update count = %d, want one conditional update per touch", updateCount)
	}
}

func TestAPIKeyPlaintextRequiresEncryptedSecretOffline(t *testing.T) {
	t.Parallel()

	service := NewService(authPostgresGorm(t), "admin-session-plaintext-test-master-key")
	for _, apiKey := range []store.APIKey{
		{},
		{EncryptedSecret: sql.NullString{String: " \t ", Valid: true}},
	} {
		plaintext, err := service.APIKeyPlaintext(apiKey)
		if plaintext != "" || err == nil || err.Error() != "api key plaintext is unavailable" {
			t.Fatalf("APIKeyPlaintext(%#v) = %q, %v; want unavailable error", apiKey, plaintext, err)
		}
	}
}
