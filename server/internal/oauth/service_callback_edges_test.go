package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestImportOAuthAccountBindsExistingConnectionToExistingSiteOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	siteID := uuid.New()
	savedConnections := []store.OAuthConnection{}
	savedSites := []store.Site{}
	db := oauthGormWithQueryUpdate(t, queryOAuthConnectionAndSite(
		store.OAuthConnection{
			ID:       connectionID,
			Provider: codexProvider,
			SiteID:   &siteID,
			Status:   "connected",
			Email:    "user@example.com",
		},
		store.Site{
			ID:      siteID,
			Name:    "Existing OAuth Site",
			Enabled: true,
			Meta:    store.JSON(`{"kept":"yes"}`),
		},
		"import existing site",
	), saveOAuthConnectionAndSite("import existing site", func(connection store.OAuthConnection) {
		savedConnections = append(savedConnections, connection)
	}, func(site store.Site) {
		savedSites = append(savedSites, site)
	}))
	service := NewImportService(oauthStoreWithGorm(t, db), "master-key", nil)

	result := service.importOAuthAccount(context.Background(), importOAuthAccountInput{
		Email:        " user@example.com ",
		Provider:     codexProvider,
		AccountID:    "acct-123",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		IDToken:      "id-token",
		Status:       "pending_sync",
		SiteEnabled:  false,
		ExpiresAt:    time.Now().Add(time.Hour),
		RawProfile:   map[string]any{"email": "user@example.com"},
		Metadata:     map[string]any{"plan_type": "plus"},
	}, false)

	if result.Status != "queued" || result.ConnectionID != connectionID.String() || result.SiteID != siteID.String() || result.SiteName != "Existing OAuth Site" {
		t.Fatalf("import result = %#v, want queued existing-site binding", result)
	}
	if result.Refreshable == nil || !*result.Refreshable || result.TokenMode != "oauth_refresh" || result.Warning != "" {
		t.Fatalf("refresh metadata result = %#v, want refreshable oauth import", result)
	}
	if len(savedConnections) != 2 {
		t.Fatalf("saved connections = %d, want upsert save and bind save", len(savedConnections))
	}
	if savedConnections[1].SiteID == nil || *savedConnections[1].SiteID != siteID {
		t.Fatalf("bound connection site = %#v, want %s", savedConnections[1].SiteID, siteID)
	}
	if len(savedSites) != 2 {
		t.Fatalf("saved sites = %d, want pending-sync disable and metadata update", len(savedSites))
	}
	if savedSites[0].Enabled {
		t.Fatalf("pending-sync site enabled = true, want disabled")
	}
	var meta map[string]any
	if err := json.Unmarshal(savedSites[1].Meta, &meta); err != nil {
		t.Fatalf("decode saved site meta: %v", err)
	}
	if meta["kept"] != "yes" || meta["oauth_connection_id"] != connectionID.String() || meta["oauth_account_id"] != "acct-123" || meta["oauth_email"] != "user@example.com" || meta["oauth_plan_type"] != "plus" {
		t.Fatalf("saved site meta = %#v, want merged oauth import metadata", meta)
	}
}

func TestHandleOAuthCallbacksRejectNonPendingMismatchedOrExpiredSessionsOffline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		session store.OAuthSession
		call    func(*Service) error
		want    string
	}{
		{
			name: "codex completed session",
			session: store.OAuthSession{
				ID:        uuid.New(),
				Provider:  codexProvider,
				State:     "completed-session-state",
				Status:    "completed",
				ExpiresAt: time.Now().Add(time.Hour),
			},
			call: func(service *Service) error {
				_, _, _, err := service.HandleCodexCallback(context.Background(), " completed-session-state ", " code ")
				return err
			},
			want: "oauth session is no longer pending",
		},
		{
			name: "antigravity provider mismatch",
			session: store.OAuthSession{
				ID:        uuid.New(),
				Provider:  codexProvider,
				State:     "provider-mismatch-state",
				Status:    "pending",
				ExpiresAt: time.Now().Add(time.Hour),
			},
			call: func(service *Service) error {
				_, _, _, err := service.HandleAntigravityCallback(context.Background(), "provider-mismatch-state", "code")
				return err
			},
			want: "oauth session provider mismatch",
		},
		{
			name: "antigravity expired session",
			session: store.OAuthSession{
				ID:        uuid.New(),
				Provider:  antigravityProvider,
				State:     "expired-session-state",
				Status:    "pending",
				ExpiresAt: time.Now().Add(-time.Minute),
			},
			call: func(service *Service) error {
				_, _, _, err := service.HandleAntigravityCallback(context.Background(), "expired-session-state", "code")
				return err
			},
			want: "oauth session has expired",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := oauthServiceWithQueryUpdate(t, queryOAuthSession(tc.session, "callback session guard"), func(tx *gorm.DB) {
				tx.AddError(errors.New("callback guard should not save"))
			})

			err := tc.call(service)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("callback error = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

func TestMarkConnectionUnavailableRecordsErrorAndDisablesBoundSiteOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	siteID := uuid.New()
	var savedConnection store.OAuthConnection
	var savedSite store.Site
	service := oauthServiceWithQueryUpdate(t, queryOAuthConnectionAndSite(
		store.OAuthConnection{
			ID:       connectionID,
			Provider: codexProvider,
			SiteID:   &siteID,
			Status:   "connected",
			Metadata: store.JSON(`{"kept":"yes"}`),
		},
		store.Site{ID: siteID, Name: "Bound Site", Enabled: true},
		"connection unavailable",
	), saveOAuthConnectionAndSite("connection unavailable", func(connection store.OAuthConnection) {
		savedConnection = connection
	}, func(site store.Site) {
		savedSite = site
	}))

	if err := service.MarkConnectionUnavailable(context.Background(), connectionID, "refresh failed"); err != nil {
		t.Fatalf("MarkConnectionUnavailable returned error: %v", err)
	}

	if savedConnection.Status != "reconnect_required" {
		t.Fatalf("saved connection status = %q, want reconnect_required", savedConnection.Status)
	}
	var meta map[string]any
	if err := json.Unmarshal(savedConnection.Metadata, &meta); err != nil {
		t.Fatalf("decode saved connection meta: %v", err)
	}
	if meta["kept"] != "yes" || meta["last_error"] != "refresh failed" || strings.TrimSpace(stringFromAny(meta["last_error_at"])) == "" {
		t.Fatalf("saved connection meta = %#v, want merged last_error fields", meta)
	}
	if savedSite.ID != siteID || savedSite.Enabled {
		t.Fatalf("saved site = %#v, want disabled bound site %s", savedSite, siteID)
	}
}

func TestImportWrappersReportValidationFailuresInSummaryOffline(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil)
	cases := []struct {
		name string
		run  func() ImportResult
		want string
	}{
		{
			name: "sub2api all failed",
			run: func() ImportResult {
				return service.ImportAccounts(context.Background(), Sub2APIExport{Accounts: []Sub2APIAccount{
					{Name: "Unsupported", Platform: "unknown"},
					{Platform: "openai", Credentials: Sub2APICredentials{Email: "missing-token@example.com"}},
				}}, false)
			},
			want: "access_token is required",
		},
		{
			name: "cpa missing access",
			run: func() ImportResult {
				return service.ImportCPAAccounts(context.Background(), CPAExport{Type: "codex", Email: "cpa@example.com", AccountID: "acct-123"}, false)
			},
			want: "access_token is required",
		},
		{
			name: "chatgpt token missing account",
			run: func() ImportResult {
				return service.ImportChatGPTTokenAccounts(context.Background(), ChatGPTTokenExport{Tokens: ChatGPTTokenDetails{AccessToken: "access-token"}}, false)
			},
			want: "account_id is required",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := tc.run()
			if result.Meta.Total == 0 || result.Meta.Failed != result.Meta.Total || result.Meta.Accepted != 0 || result.Meta.Queued != 0 || result.Meta.Succeeded != 0 {
				t.Fatalf("import meta = %#v, want all items failed", result.Meta)
			}
			last := result.Items[len(result.Items)-1]
			if last.Status != "failed" || !strings.Contains(last.Error, tc.want) {
				t.Fatalf("last import item = %#v, want failed error containing %q", last, tc.want)
			}
		})
	}
}

func TestRefreshCodexConnectionReturnsUnexpiredAccessOnlyDetailsOffline(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	encryptedAccess, _, err := service.credentials.Encrypt("access-only-token")
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}
	connection := store.OAuthConnection{
		ID:                   uuid.New(),
		Provider:             codexProvider,
		Email:                "user@example.com",
		AccountID:            "acct-123",
		EncryptedAccessToken: encryptedAccess,
		ExpiresAt:            sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
	}
	service = oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected access-only refresh query destination"))
			return
		}
		*dest = connection
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("access-only refresh should not save"))
	})

	details, err := service.RefreshCodexConnection(context.Background(), connection.ID)
	if err != nil {
		t.Fatalf("RefreshCodexConnection returned error: %v", err)
	}
	if details.AccessToken != "access-only-token" || details.Connection.ID != connection.ID {
		t.Fatalf("connection details = %#v, want decrypted unexpired access-only connection", details)
	}
}

func queryOAuthConnectionAndSite(connection store.OAuthConnection, site store.Site, label string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.OAuthConnection:
			*dest = connection
			tx.Statement.RowsAffected = 1
		case *store.Site:
			*dest = site
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected " + label + " query destination"))
		}
	}
}

func queryOAuthSession(session store.OAuthSession, label string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*store.OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected " + label + " query destination"))
			return
		}
		*dest = session
		tx.Statement.RowsAffected = 1
	}
}

func saveOAuthConnectionAndSite(label string, saveConnection func(store.OAuthConnection), saveSite func(store.Site)) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.OAuthConnection:
			saveConnection(*dest)
			tx.Statement.RowsAffected = 1
		case *store.Site:
			saveSite(*dest)
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected " + label + " save destination"))
		}
	}
}
