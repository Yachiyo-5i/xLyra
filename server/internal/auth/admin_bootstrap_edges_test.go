package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestCreateAndBootstrapAdminInitialBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	adminID := uuid.New()
	sessionID := uuid.New()
	service := NewService(authTransactionOnlyGorm(t), "bootstrap-master-key")
	countQueries := 0
	var createdAdmins []store.Admin
	var createdSessions []store.AdminSession

	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected auth query destination"))
			return
		}
		countQueries++
		*count = 0
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Row().Replace("gorm:row", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected auth row destination"))
			return
		}
		countQueries++
		*count = 0
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace row callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Admin:
			dest.ID = adminID
			createdAdmins = append(createdAdmins, *dest)
		case *store.AdminSession:
			dest.ID = sessionID
			dest.CreatedAt = time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
			createdSessions = append(createdSessions, *dest)
		default:
			tx.AddError(errors.New("unexpected auth create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}

	admin, err := service.CreateAdmin(ctx, "  root-owner  ", "StrongPass123!", "")
	if err != nil {
		t.Fatalf("CreateAdmin returned error: %v", err)
	}
	result, err := service.BootstrapAdmin(ctx, "  bootstrap-owner  ", "StrongPass123!", "  Nanako  ", "  avatar.png  ", "bootstrap-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("BootstrapAdmin returned error: %v", err)
	}

	if admin.ID != adminID || admin.Username != "root-owner" || admin.Role != "owner" || admin.Status != "active" {
		t.Fatalf("CreateAdmin result = %#v, want trimmed active owner admin", admin)
	}
	if result.Admin.ID != adminID || result.SessionID != sessionID || result.Token == "" || result.CSRFToken == "" {
		t.Fatalf("BootstrapAdmin result = %#v, want token and captured session", result)
	}
	if len(createdAdmins) != 2 || createdAdmins[1].Username != "bootstrap-owner" ||
		createdAdmins[1].Nickname != "Nanako" || createdAdmins[1].Avatar != "avatar.png" {
		t.Fatalf("createdAdmins = %#v, want create and bootstrap admins with trimmed profile fields", createdAdmins)
	}
	if len(createdSessions) != 1 || createdSessions[0].AdminID != adminID ||
		createdSessions[0].UserAgent != "bootstrap-agent" || string(createdSessions[0].IPAddress) == "" ||
		createdSessions[0].SessionTokenHash == "" || createdSessions[0].ExpiresAt == nil {
		t.Fatalf("createdSessions = %#v, want bootstrap session payload", createdSessions)
	}
	if countQueries != 2 {
		t.Fatalf("countQueries = %d, want one initial-admin count per create path", countQueries)
	}
}

func TestCreateAndBootstrapAdminRejectAlreadyInitialized(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		call func(*Service) error
	}{
		{
			name: "create admin",
			call: func(service *Service) error {
				_, err := service.CreateAdmin(context.Background(), "owner", "StrongPass123!", "")
				return err
			},
		},
		{
			name: "bootstrap admin",
			call: func(service *Service) error {
				_, err := service.BootstrapAdmin(context.Background(), "owner", "StrongPass123!", "", "", "", "")
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := NewService(authTransactionOnlyGorm(t), "bootstrap-master-key")
			if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					tx.AddError(errors.New("unexpected initialized query destination"))
					return
				}
				*count = 1
				tx.Statement.RowsAffected = 1
			}); err != nil {
				t.Fatalf("replace query callback: %v", err)
			}
			if err := service.db.Callback().Row().Replace("gorm:row", func(tx *gorm.DB) {
				count, ok := tx.Statement.Dest.(*int64)
				if !ok {
					tx.AddError(errors.New("unexpected initialized row destination"))
					return
				}
				*count = 1
				tx.Statement.RowsAffected = 1
			}); err != nil {
				t.Fatalf("replace row callback: %v", err)
			}

			if err := tc.call(service); !errors.Is(err, ErrAlreadyInitialized) {
				t.Fatalf("%s error = %v, want ErrAlreadyInitialized", tc.name, err)
			}
		})
	}
}
