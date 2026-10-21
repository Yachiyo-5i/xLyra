package site

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestAccountCountsSeparatesValidFromTotal(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	okID, reauthID, disabledID, failedID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	prefix := store.CredentialPrefixGrok
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{
					{ID: okID, SiteID: siteID, CredentialType: prefix + uuid.NewString(), Meta: store.JSON(`{"enabled":true}`)},
					{ID: reauthID, SiteID: siteID, CredentialType: prefix + uuid.NewString(), Meta: store.JSON(`{"enabled":false,"reauth_required":true}`)},
					{ID: disabledID, SiteID: siteID, CredentialType: prefix + uuid.NewString(), Meta: store.JSON(`{"enabled":true}`)},
					{ID: failedID, SiteID: siteID, CredentialType: prefix + uuid.NewString(), Meta: store.JSON(`{"enabled":true}`)},
					{ID: uuid.New(), SiteID: siteID, CredentialType: "api_key"},
				}
			case *[]store.SiteAPIKeyState:
				*dest = []store.SiteAPIKeyState{
					{SiteCredentialID: okID, SiteID: siteID, Enabled: true, SyncStatus: "synced"},
					{SiteCredentialID: reauthID, SiteID: siteID, Enabled: false, SyncStatus: "reauth_required"},
					{SiteCredentialID: disabledID, SiteID: siteID, Enabled: false, SyncStatus: "synced"},
					{SiteCredentialID: failedID, SiteID: siteID, Enabled: true, SyncStatus: "failed"},
				}
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})

	counts, err := service.AccountCounts(context.Background(), siteID, "grok")
	if err != nil {
		t.Fatalf("AccountCounts() error = %v", err)
	}
	if counts.Total != 4 || counts.Valid != 1 {
		t.Fatalf("counts = %+v, want valid 1 of total 4", counts)
	}
	other, err := service.AccountCounts(context.Background(), siteID, "openai")
	if err != nil || other.Total != 0 || other.Valid != 0 {
		t.Fatalf("non-account site counts = %+v err=%v, want zero", other, err)
	}
}
