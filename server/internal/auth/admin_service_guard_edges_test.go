package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestLoginMasksAdminLookupErrorsAsInvalidCredentials(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("admin lookup stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	result, err := service.Login(context.Background(), " admin ", "password", "", "agent", "127.0.0.1")
	if result.Token != "" || result.Admin.ID != uuid.Nil {
		t.Fatalf("Login result = %#v, want zero result on lookup error", result)
	}
	assertAuthErrorString(t, "Login", err, "invalid credentials")
}

func TestAdminReadAndSessionMethodsPropagateRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("admin repository stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})
	adminID := uuid.New()

	if admin, err := service.GetAdminByID(context.Background(), adminID); admin.ID != uuid.Nil {
		t.Fatalf("GetAdminByID = %#v, %v; want zero admin on repository error", admin, err)
	} else {
		assertAuthErrorIs(t, "GetAdminByID", err, queryErr)
	}
	if admin, err := service.ChangeAdminPassword(context.Background(), adminID, "old-password", "new-password", uuid.New()); admin.ID != uuid.Nil {
		t.Fatalf("ChangeAdminPassword = %#v, %v; want zero admin on repository error", admin, err)
	} else {
		assertAuthErrorIs(t, "ChangeAdminPassword", err, queryErr)
	}
	if sessions, err := service.ListAdminSessions(context.Background(), adminID); sessions != nil {
		t.Fatalf("ListAdminSessions = %#v, %v; want nil sessions on repository error", sessions, err)
	} else {
		assertAuthErrorIs(t, "ListAdminSessions", err, queryErr)
	}
}

func TestUpdateAdminProfileReturnsZeroAdminForBlankUsernameBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	admin, err := service.UpdateAdminProfile(context.Background(), uuid.New(), " \t\n ", "nick", "avatar")
	if admin.ID != uuid.Nil {
		t.Fatalf("UpdateAdminProfile admin = %#v, want zero on validation error", admin)
	}
	assertAuthErrorString(t, "UpdateAdminProfile", err, "username is required")
}

func TestAdminTokenBlankGuardsAvoidRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.ValidateAdminSession(context.Background(), "")
	assertAuthErrorContains(t, "ValidateAdminSession blank", err, "missing session token")
	_, err = service.ValidateAdminAccessToken(context.Background(), " \t ", "agent", "127.0.0.1")
	assertAuthErrorContains(t, "ValidateAdminAccessToken blank", err, "missing access token")
	if err := service.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout blank token error = %v, want nil", err)
	}
}
