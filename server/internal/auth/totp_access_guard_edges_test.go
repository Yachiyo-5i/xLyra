package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

func TestTOTPSetupEnableAndDisablePersistExpectedAdminFieldsOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	passwordHash, err := crypto.HashPassword("current-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	queryAdmin := store.Admin{ID: adminID, Username: "root", PasswordHash: passwordHash}
	var saved []store.Admin
	service := authServiceWithAdminTOTPCallbacks(t, &queryAdmin, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	setup, err := service.SetupTOTP(context.Background(), adminID)
	if err != nil {
		t.Fatalf("SetupTOTP returned error: %v", err)
	}
	if setup.Secret == "" || !strings.Contains(setup.OtpauthURL, "otpauth://totp/xLyra:root?") {
		t.Fatalf("SetupTOTP result = %#v, want secret and root otpauth URL", setup)
	}
	if len(saved) != 1 || saved[0].ID != adminID || saved[0].TOTPPendingSecretEncrypted == "" {
		t.Fatalf("saved setup admins = %#v, want pending secret save", saved)
	}

	queryAdmin.TOTPPendingSecretEncrypted = saved[0].TOTPPendingSecretEncrypted
	if _, err := service.EnableTOTP(context.Background(), adminID, "not-code"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("EnableTOTP invalid code error = %v, want ErrTOTPInvalid", err)
	}

	queryAdmin.TOTPEnabled = false
	queryAdmin.TOTPPendingSecretEncrypted = ""
	if _, err := service.DisableTOTP(context.Background(), adminID, "current-password", ""); err != nil {
		t.Fatalf("DisableTOTP without enabled secret returned error: %v", err)
	}
	if len(saved) != 2 || saved[1].TOTPEnabled || saved[1].TOTPSecretEncrypted != "" || saved[1].TOTPPendingSecretEncrypted != "" {
		t.Fatalf("saved disable admin = %#v, want TOTP fields cleared", saved)
	}
}

func TestTOTPGuardFailuresStopBeforeAdminWritesOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	queryAdmin := store.Admin{ID: adminID, Username: "root"}
	service := authServiceWithAdminTOTPCallbacks(t, &queryAdmin, func(tx *gorm.DB) {
		tx.AddError(errors.New("update callback should not run for guard failures"))
	})

	if _, err := service.EnableTOTP(context.Background(), adminID, "123456"); err == nil || err.Error() != "totp setup is required" {
		t.Fatalf("EnableTOTP missing setup error = %v, want setup required", err)
	}
	if _, err := service.DisableTOTP(context.Background(), adminID, "bad-password", "123456"); err == nil || err.Error() != "current password is invalid" {
		t.Fatalf("DisableTOTP bad password error = %v, want current password invalid", err)
	}
}

func TestDeleteAdminAccessTokenIssuesExplicitDeleteOffline(t *testing.T) {
	t.Parallel()

	service := NewService(authPostgresGorm(t), "totp-access-guard-test-master-key")
	deleteCalls := 0
	if err := service.db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}

	if err := service.DeleteAdminAccessToken(context.Background()); err != nil {
		t.Fatalf("DeleteAdminAccessToken returned error: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want explicit delete", deleteCalls)
	}
}

func TestCheckSiteAccessAllowAllSkipsPermissionLookupOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteID := uuid.New()
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		if tx.Statement != nil && strings.Contains(tx.Statement.Table, "api_key_site_permissions") {
			tx.AddError(errors.New("permission lookup should not run for allow_all"))
			return
		}
		item, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected api key query destination"))
			return
		}
		*item = store.APIKey{ID: apiKeyID, SitePolicy: "allow_all"}
		tx.Statement.RowsAffected = 1
	})

	allowed, err := service.CheckSiteAccess(context.Background(), apiKeyID, siteID)
	if err != nil {
		t.Fatalf("CheckSiteAccess returned error: %v", err)
	}
	if !allowed {
		t.Fatal("CheckSiteAccess allow_all = false, want true")
	}
}

func authServiceWithAdminTOTPCallbacks(t *testing.T, queryAdmin *store.Admin, update func(*gorm.DB)) *Service {
	t.Helper()

	service := NewService(authPostgresGorm(t), "totp-access-guard-test-master-key")
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin query destination"))
			return
		}
		*item = *queryAdmin
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if update != nil {
		if err := service.db.Callback().Update().Replace("gorm:update", update); err != nil {
			t.Fatalf("replace update callback: %v", err)
		}
	}
	return service
}
