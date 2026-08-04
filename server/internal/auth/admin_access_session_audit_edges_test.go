package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestAdminAccessTokenManagementPropagatesRepositoryReadErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("access token read stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	ctx := context.Background()

	if accessToken, err := service.GetAdminAccessToken(ctx); accessToken.ID != uuid.Nil {
		t.Fatalf("GetAdminAccessToken = %#v, %v; want zero token on repository read error", accessToken, err)
	} else {
		assertAuthErrorIs(t, "GetAdminAccessToken", err, queryErr)
	}
	if accessToken, err := service.SetAdminAccessTokenEnabled(ctx, false); accessToken.ID != uuid.Nil {
		t.Fatalf("SetAdminAccessTokenEnabled = %#v, %v; want zero token on repository read error", accessToken, err)
	} else {
		assertAuthErrorIs(t, "SetAdminAccessTokenEnabled", err, queryErr)
	}
}

func TestAdminSessionDeleteOperationsPropagateRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("session delete lookup stopped")
	service := authServiceWithRepositoryReadAndDeleteError(t, queryErr)
	ctx := context.Background()
	adminID := uuid.New()

	assertAuthErrorIs(t, "DeleteAdminSession", service.DeleteAdminSession(ctx, adminID, uuid.New()), queryErr)
	assertAuthErrorIs(t, "DeleteOtherAdminSessions", service.DeleteOtherAdminSessions(ctx, adminID, uuid.New()), queryErr)
}

func TestListAuditLogsPropagatesRepositoryReadError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("audit log list stopped")
	service := authServiceWithRepositoryError(t, queryErr)

	logs, total, err := service.ListAuditLogs(context.Background(), store.AdminAuditLogFilters{
		Action:   "admin_api.access",
		Page:     2,
		PageSize: 10,
	})
	if logs != nil || total != 0 {
		t.Fatalf("ListAuditLogs = %#v, %d, %v; want nil logs and zero total on repository read error", logs, total, err)
	}
	assertAuthErrorIs(t, "ListAuditLogs", err, queryErr)
}

func TestWithAdminActorWithoutAdminIDDoesNotSetAdminID(t *testing.T) {
	t.Parallel()

	accessTokenID := uuid.New()
	ctx := WithAdminActor(context.Background(), AdminActor{
		Type:          "access_token",
		AccessTokenID: accessTokenID,
	})

	if adminID, ok := AdminIDFromContext(ctx); ok || adminID != uuid.Nil {
		t.Fatalf("AdminIDFromContext = %s, %v; want no admin id when actor admin id is nil", adminID, ok)
	}
	actor, ok := AdminActorFromContext(ctx)
	if !ok || actor.Type != "access_token" || actor.AccessTokenID != accessTokenID {
		t.Fatalf("AdminActorFromContext = %#v, %v; want access-token actor retained", actor, ok)
	}
}

func TestCSRFValidationTrimsProvidedTokenOnly(t *testing.T) {
	t.Parallel()

	service := &Service{masterKey: "csrf-trim-secret"}
	sessionToken := "xlyra_session_trimmed"
	token := service.CSRFTokenForSession(sessionToken)

	if !service.ValidateCSRFToken(sessionToken, " \t"+token+"\n ") {
		t.Fatal("ValidateCSRFToken rejected a valid token with surrounding whitespace")
	}
	if service.ValidateCSRFToken(sessionToken+"-other", token) {
		t.Fatal("ValidateCSRFToken accepted a token for a different session value")
	}
	if service.ValidateCSRFToken(sessionToken, strings.TrimSuffix(token, token[len(token)-1:])+"x") {
		t.Fatal("ValidateCSRFToken accepted a same-length mismatched token")
	}
}

func authServiceWithRepositoryReadAndDeleteError(t *testing.T, queryErr error) *Service {
	t.Helper()

	service := authServiceWithRepositoryError(t, queryErr)
	authReplaceDeleteCallback(t, service.db, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})
	return service
}
