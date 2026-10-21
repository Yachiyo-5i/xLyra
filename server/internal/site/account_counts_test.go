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

func TestSiteTypeHasAccounts(t *testing.T) {
	t.Parallel()

	if !siteTypeHasAccounts(SiteTypeDescriptor{Accounts: &AccountCapabilities{LoginKind: "custom_poll"}}) {
		t.Fatal("descriptor with Accounts should be an account pool")
	}
	if siteTypeHasAccounts(SiteTypeDescriptor{}) || siteTypeHasAccounts(SiteTypeDescriptor{CredentialCategory: "account"}) {
		t.Fatal("descriptor without Accounts should not be an account pool")
	}
	if !IsAccountLoginSite("grok") || !IsAccountLoginSite(" GROK ") {
		t.Fatal("built-in grok should stay an account-login site")
	}
	if IsAccountLoginSite("openai") || IsAccountLoginSite("acme.example/chat") {
		t.Fatal("api key and unregistered plugin site types should not be account-login sites")
	}
}

func TestIsAccountPoolCategory(t *testing.T) {
	t.Parallel()

	for _, cat := range []string{"grok_sso", "account"} {
		if !isAccountPoolCategory(cat) {
			t.Fatalf("isAccountPoolCategory(%q) = false, want true", cat)
		}
	}
	for _, cat := range []string{"api_key", "oauth", "system_token", "xlyra", ""} {
		if isAccountPoolCategory(cat) {
			t.Fatalf("isAccountPoolCategory(%q) = true, want false", cat)
		}
	}
}

func TestAccountCredentialForSite(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	grok := "grok_sso:" + id.String()
	plugin := store.NewAccountCredentialType("acme.example/chat", id)
	other := store.NewAccountCredentialType("other.example/chat", id)

	if !accountCredentialForSite("grok", grok) || accountCredentialForSite("grok", "grok_sso:abc") {
		t.Fatal("grok site should keep the grok credential predicate")
	}
	if accountCredentialForSite("grok", plugin) || accountCredentialForSite("grok", "api_key") {
		t.Fatal("grok site should ignore other credential types")
	}
	if !accountCredentialForSite("acme.example/chat", plugin) || !accountCredentialForSite(" ACME.EXAMPLE/Chat ", plugin) {
		t.Fatal("plugin site should accept its own account credential")
	}
	if accountCredentialForSite("acme.example/chat", other) || accountCredentialForSite("acme.example/chat", grok) || accountCredentialForSite("acme.example/chat", "api_key") || accountCredentialForSite("acme.example/chat", "account:x") {
		t.Fatal("plugin site should ignore other and unparseable credentials")
	}
}

func TestAccountPoolCredentialCountUsesPluginPrefix(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			dest, ok := tx.Statement.Dest.(*[]store.SiteCredential)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			*dest = []store.SiteCredential{
				{ID: uuid.New(), SiteID: siteID, CredentialType: store.NewAccountCredentialType("acme.example/chat", id)},
				{ID: uuid.New(), SiteID: siteID, CredentialType: store.NewAccountCredentialType("acme.example/chat", uuid.New())},
				{ID: uuid.New(), SiteID: siteID, CredentialType: store.NewAccountCredentialType("other.example/chat", uuid.New())},
				{ID: uuid.New(), SiteID: siteID, CredentialType: "grok_sso:" + uuid.NewString()},
				{ID: uuid.New(), SiteID: siteID, CredentialType: "api_key"},
				{ID: uuid.New(), SiteID: siteID, CredentialType: "account:x"},
			}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})

	count, err := service.accountPoolCredentialCount(context.Background(), siteID, "acme.example/chat")
	if err != nil {
		t.Fatalf("accountPoolCredentialCount() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("accountPoolCredentialCount() = %d, want 2", count)
	}
}
