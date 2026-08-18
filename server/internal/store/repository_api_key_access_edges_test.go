package store

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestAdminRepositoryCreateBuildsAdminOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	var captured Admin
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin create destination"))
			return
		}
		captured = *item
		item.ID = uuid.New()
		tx.Statement.RowsAffected = 1
	})

	admin, err := NewAdminRepository(db).Create(context.Background(), CreateAdminParams{
		Username:     "root",
		Nickname:     "Root",
		Avatar:       "avatar.png",
		PasswordHash: "hash",
		Role:         "owner",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if admin.ID == uuid.Nil {
		t.Fatal("Create should return ID assigned by create callback")
	}
	if captured.Username != "root" || captured.Nickname != "Root" || captured.Avatar != "avatar.png" ||
		captured.PasswordHash != "hash" || captured.Role != "owner" || captured.Status != "active" {
		t.Fatalf("captured admin = %#v", captured)
	}
}

func TestAdminRepositoryUpdateProfileSavesFetchedAdminOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin query destination"))
			return
		}
		*item = Admin{ID: adminID, Username: "old", Nickname: "Old", Avatar: "old.png", Role: "admin", Status: "active"}
		tx.Statement.RowsAffected = 1
	})
	var saved Admin
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	admin, err := NewAdminRepository(db).UpdateProfile(context.Background(), adminID, "new", "New", "new.png")
	if err != nil {
		t.Fatalf("UpdateProfile returned error: %v", err)
	}

	if admin.ID != adminID || saved.ID != adminID {
		t.Fatalf("admin IDs = item:%s saved:%s, want %s", admin.ID, saved.ID, adminID)
	}
	if saved.Username != "new" || saved.Nickname != "New" || saved.Avatar != "new.png" ||
		saved.Role != "admin" || saved.Status != "active" {
		t.Fatalf("saved admin = %#v", saved)
	}
}

func TestAdminRepositoryTOTPMethodsSaveExpectedSecretsOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	tests := []struct {
		name              string
		run               func(AdminRepository) (Admin, error)
		wantEnabled       bool
		wantSecret        string
		wantPendingSecret string
	}{
		{
			name: "save setup",
			run: func(repo AdminRepository) (Admin, error) {
				return repo.SaveTOTPSetup(context.Background(), adminID, "pending-new")
			},
			wantEnabled:       true,
			wantSecret:        "secret-old",
			wantPendingSecret: "pending-new",
		},
		{
			name: "enable",
			run: func(repo AdminRepository) (Admin, error) {
				return repo.EnableTOTP(context.Background(), adminID)
			},
			wantEnabled: true,
			wantSecret:  "pending-old",
		},
		{
			name: "disable",
			run: func(repo AdminRepository) (Admin, error) {
				return repo.DisableTOTP(context.Background(), adminID)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := storeRepositoryOfflineGorm(t)
			storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
				item, ok := tx.Statement.Dest.(*Admin)
				if !ok {
					tx.AddError(errors.New("unexpected admin totp query destination"))
					return
				}
				*item = Admin{
					ID:                         adminID,
					Username:                   "root",
					TOTPEnabled:                true,
					TOTPSecretEncrypted:        "secret-old",
					TOTPPendingSecretEncrypted: "pending-old",
				}
				tx.Statement.RowsAffected = 1
			})
			var saved Admin
			storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
				item, ok := tx.Statement.Dest.(*Admin)
				if !ok {
					tx.AddError(errors.New("unexpected admin totp update destination"))
					return
				}
				saved = *item
				tx.Statement.RowsAffected = 1
			})

			admin, err := tt.run(NewAdminRepository(db))
			if err != nil {
				t.Fatalf("%s returned error: %v", tt.name, err)
			}

			if admin.ID != adminID || saved.ID != adminID {
				t.Fatalf("admin IDs = item:%s saved:%s, want %s", admin.ID, saved.ID, adminID)
			}
			if saved.TOTPEnabled != tt.wantEnabled || saved.TOTPSecretEncrypted != tt.wantSecret ||
				saved.TOTPPendingSecretEncrypted != tt.wantPendingSecret {
				t.Fatalf("saved totp admin = %#v", saved)
			}
		})
	}
}

func TestAdminSessionRepositoryCreateCapturesSessionOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	var captured AdminSession
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AdminSession)
		if !ok {
			tx.AddError(errors.New("unexpected admin session create destination"))
			return
		}
		captured = *item
		item.ID = uuid.New()
		tx.Statement.RowsAffected = 1
	})

	adminID := uuid.New()
	expiresAt := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	session, err := NewAdminSessionRepository(db).Create(context.Background(), CreateAdminSessionParams{
		AdminID:          adminID,
		SessionTokenHash: "token-hash",
		ExpiresAt:        &expiresAt,
		IPAddress:        net.ParseIP("127.0.0.1"),
		UserAgent:        "session-create",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if session.ID == uuid.Nil {
		t.Fatal("Create should return ID assigned by create callback")
	}
	if captured.AdminID != adminID || captured.SessionTokenHash != "token-hash" ||
		captured.ExpiresAt == nil || !captured.ExpiresAt.Equal(expiresAt) ||
		!net.IP(captured.IPAddress).Equal(net.ParseIP("127.0.0.1")) || captured.UserAgent != "session-create" {
		t.Fatalf("captured admin session = %#v", captured)
	}
}

func TestAdminSessionRepositoryDeleteAndTouchOffline(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	adminID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})
	updateCalls := 0
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewAdminSessionRepository(db)
	if err := repo.DeleteByID(context.Background(), adminID, sessionID); err != nil {
		t.Fatalf("DeleteByID returned error: %v", err)
	}
	if err := repo.TouchLastSeen(context.Background(), sessionID, time.Date(2026, 6, 23, 12, 30, 0, 0, time.UTC), time.Minute); err != nil {
		t.Fatalf("TouchLastSeen returned error: %v", err)
	}

	if deleteCalls != 1 || updateCalls != 1 {
		t.Fatalf("callback calls = delete:%d update:%d, want 1 each", deleteCalls, updateCalls)
	}
}

func TestAPIKeyRepositoryCreateUpdateDeleteAndUsageOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	adminID := uuid.New()
	expiresAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	db := storeTransactionGorm(t, "api key repository mutations")
	var created APIKey
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected api key create destination"))
			return
		}
		created = *item
		item.ID = apiKeyID
		tx.Statement.RowsAffected = 1
	})
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected api key query destination"))
			return
		}
		if queryCalls == 3 {
			locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
			if !ok || locking.Strength != clause.LockingStrengthUpdate {
				tx.AddError(errors.New("api key usage query must lock the row"))
				return
			}
		}
		*item = APIKey{ID: apiKeyID, Name: "old", Status: "active", ModelMappings: JSON(`{"old":true}`), QuotaUsed: 2}
		if queryCalls >= 2 {
			item.Name = "updated"
			item.Status = "paused"
			item.ModelPolicy = "deny_all"
			item.SitePolicy = "selected"
			item.QuotaLimit = sql.NullFloat64{Float64: 20, Valid: true}
		}
		tx.Statement.RowsAffected = 1
	})
	var configurationUpdates map[string]any
	usageUpdateCalled := false
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if updates, ok := tx.Statement.Dest.(map[string]any); ok {
			if _, ok := updates["name"]; ok {
				configurationUpdates = updates
			} else {
				usageUpdateCalled = true
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected api key update destination"))
	})
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewAPIKeyRepository(db)
	key, err := repo.Create(context.Background(), CreateAPIKeyParams{
		Name:             "created",
		KeyPrefix:        "sk",
		KeyHash:          "hash",
		EncryptedSecret:  "secret",
		MaskedKey:        "sk-***",
		ModelMappings:    JSON(`{"model":"ok"}`),
		QuotaLimit:       float64(10),
		QuotaUnlimited:   true,
		ExpiresAt:        expiresAt,
		CreatedByAdminID: adminID,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := repo.Update(context.Background(), UpdateAPIKeyParams{
		ID:             apiKeyID,
		Name:           "updated",
		Status:         "paused",
		ModelPolicy:    "deny_all",
		SitePolicy:     "selected",
		QuotaLimit:     float64(20),
		QuotaUnlimited: false,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	used, err := repo.AddUsage(context.Background(), apiKeyID, 3.5)
	if err != nil {
		t.Fatalf("AddUsage returned error: %v", err)
	}
	if err := repo.Delete(context.Background(), apiKeyID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if key.ID != apiKeyID || created.Scope != "gateway" || created.Status != "active" ||
		created.KeyKind != "generated" || created.ModelPolicy != "allow_all" || created.SitePolicy != "allow_all" {
		t.Fatalf("created api key defaults = key:%#v captured:%#v", key, created)
	}
	if !created.EncryptedSecret.Valid || created.EncryptedSecret.String != "secret" ||
		created.CreatedByAdminID == nil || *created.CreatedByAdminID != adminID ||
		created.ExpiresAt == nil || !created.ExpiresAt.Equal(expiresAt) ||
		!created.QuotaLimit.Valid || created.QuotaLimit.Float64 != 10 {
		t.Fatalf("created api key nullable fields = %#v", created)
	}
	if configurationUpdates == nil || !usageUpdateCalled {
		t.Fatalf("configurationUpdates = %#v, usageUpdateCalled = %v, want both updates", configurationUpdates, usageUpdateCalled)
	}
	for _, field := range []string{"quota_used", "quota_total_used", "quota_total_reset_at", "quota_daily_used", "quota_daily_window_start", "quota_weekly_used", "quota_weekly_window_start"} {
		if _, ok := configurationUpdates[field]; ok {
			t.Fatalf("configuration update contains runtime field %q: %#v", field, configurationUpdates)
		}
	}
	if updated.Name != "updated" || updated.Status != "paused" || updated.ModelPolicy != "deny_all" ||
		updated.SitePolicy != "selected" || string(updated.ModelMappings) != `{"old":true}` || updated.QuotaUsed != 2 {
		t.Fatalf("updated api key = %#v", updated)
	}
	if used.QuotaUsed != 5.5 || used.QuotaTotalUsed != 5.5 {
		t.Fatalf("used quota = cumulative:%f total:%f, want 5.5 each", used.QuotaUsed, used.QuotaTotalUsed)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
	if queryCalls != 3 {
		t.Fatalf("query calls = %d, want update lookup, updated reload, and locked usage lookup", queryCalls)
	}
}

func TestOAuthConnectionRepositorySaveAndDeleteBySiteIDOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	otherSiteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	var saved OAuthConnection
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected oauth connection update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected oauth connection query destination"))
			return
		}
		*items = []OAuthConnection{
			{ID: uuid.New(), SiteID: &siteID, Email: "delete@example.com"},
			{ID: uuid.New(), SiteID: &otherSiteID, Email: "keep@example.com"},
			{ID: uuid.New(), Email: "global@example.com"},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	var deleted []OAuthConnection
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected oauth connection delete destination"))
			return
		}
		deleted = append(deleted, *item)
		tx.Statement.RowsAffected = 1
	})

	repo := NewOAuthConnectionRepository(db)
	connection, err := repo.Save(context.Background(), OAuthConnection{ID: uuid.New(), Provider: "google", Email: "save@example.com"})
	if err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if err := repo.DeleteBySiteID(context.Background(), siteID); err != nil {
		t.Fatalf("DeleteBySiteID returned error: %v", err)
	}

	if connection.Email != "save@example.com" || saved.Provider != "google" {
		t.Fatalf("saved oauth connection = item:%#v captured:%#v", connection, saved)
	}
	if len(deleted) != 1 || deleted[0].Email != "delete@example.com" {
		t.Fatalf("deleted oauth connections = %#v", deleted)
	}
}

func TestOAuthSessionRepositoryCreateAndCompleteOffline(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	var created OAuthSession
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected oauth session create destination"))
			return
		}
		created = *item
		item.ID = sessionID
		tx.Statement.RowsAffected = 1
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected oauth session query destination"))
			return
		}
		*item = OAuthSession{ID: sessionID, State: "state", Status: "pending", Metadata: JSON(`{"old":true}`)}
		tx.Statement.RowsAffected = 1
	})
	var saved OAuthSession
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected oauth session update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	expiresAt := time.Date(2026, 6, 23, 13, 0, 0, 0, time.UTC)
	repo := NewOAuthSessionRepository(db)
	session, err := repo.Create(context.Background(), CreateOAuthSessionParams{
		Provider:           "google",
		State:              "state",
		PKCEVerifier:       "verifier",
		RedirectURI:        "https://app.example.com/oauth",
		SuccessRedirectURL: "https://app.example.com/success",
		FailureRedirectURL: "https://app.example.com/failure",
		SiteID:             siteID,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	completed, err := repo.Complete(context.Background(), sessionID, "", JSON(`{"done":true}`))
	if err != nil {
		t.Fatalf("Complete returned error: %v", err)
	}

	if session.ID != sessionID || created.Status != "pending" || created.SiteID == nil || *created.SiteID != siteID ||
		string(created.SitePayload) != "{}" || string(created.Metadata) != "{}" || !created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("created oauth session = item:%#v captured:%#v", session, created)
	}
	if completed.Status != "completed" || saved.Status != "completed" || !saved.CompletedAt.Valid ||
		string(saved.Metadata) != `{"done":true}` {
		t.Fatalf("completed oauth session = item:%#v saved:%#v", completed, saved)
	}
}

func TestSiteCredentialRepositoryDeleteAndUpdatesOffline(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential query destination"))
			return
		}
		*item = SiteCredential{ID: credentialID, SiteID: siteID, CredentialType: " api_key ", Meta: JSON(`{"old":true}`)}
		tx.Statement.RowsAffected = 1
	})
	var saved []SiteCredential
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteCredentialRepository(db)
	metaUpdated, err := repo.UpdateMeta(context.Background(), credentialID, nil)
	if err != nil {
		t.Fatalf("UpdateMeta returned error: %v", err)
	}
	typeUpdated, err := repo.UpdateCredentialType(context.Background(), credentialID, " api_key:secondary ")
	if err != nil {
		t.Fatalf("UpdateCredentialType returned error: %v", err)
	}
	if err := repo.DeleteBySite(context.Background(), siteID); err != nil {
		t.Fatalf("DeleteBySite returned error: %v", err)
	}

	if len(saved) != 2 {
		t.Fatalf("saved credentials = %d, want 2", len(saved))
	}
	if string(metaUpdated.Meta) != "{}" || string(saved[0].Meta) != "{}" {
		t.Fatalf("meta update = item:%#v saved:%#v", metaUpdated, saved[0])
	}
	if typeUpdated.CredentialType != "api_key:secondary" || saved[1].CredentialType != "api_key:secondary" {
		t.Fatalf("type update = item:%#v saved:%#v", typeUpdated, saved[1])
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
}

func TestSiteModelRepositoryUpsertListGetDeleteStatusAndCanonicalOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	canonicalID := uuid.New()
	matchedAt := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch dest := tx.Statement.Dest.(type) {
		case *SiteModel:
			*dest = SiteModel{ID: modelID, SiteID: siteID, UpstreamName: "gpt-b", DisplayName: "old", Status: "active"}
		case *[]SiteModel:
			*dest = []SiteModel{
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "gpt-b"},
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "gpt-a"},
			}
		default:
			tx.AddError(errors.New("unexpected site model query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	var saved []SiteModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteModel)
		if !ok {
			tx.AddError(errors.New("unexpected site model update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteModelRepository(db)
	upserted, err := repo.Upsert(context.Background(), UpsertSiteModelParams{
		SiteID:       siteID,
		UpstreamName: "gpt-b",
		DisplayName:  "GPT B",
		Capabilities: nil,
		Status:       "available",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	listed, err := repo.ListBySite(context.Background(), siteID)
	if err != nil {
		t.Fatalf("ListBySite returned error: %v", err)
	}
	got, err := repo.GetByID(context.Background(), modelID)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	statusUpdated, err := repo.UpdateStatus(context.Background(), siteID, modelID, "disabled")
	if err != nil {
		t.Fatalf("UpdateStatus returned error: %v", err)
	}
	canonicalUpdated, err := repo.UpdateCanonical(context.Background(), modelID, canonicalID, "manual", 95, matchedAt)
	if err != nil {
		t.Fatalf("UpdateCanonical returned error: %v", err)
	}
	if err := repo.Delete(context.Background(), modelID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if queryCalls < 5 {
		t.Fatalf("query calls = %d, want at least 5", queryCalls)
	}
	if upserted.DisplayName != "GPT B" || upserted.Status != "available" || string(saved[0].Capabilities) != "{}" {
		t.Fatalf("upserted site model = item:%#v saved:%#v", upserted, saved[0])
	}
	if len(listed) != 2 || listed[0].UpstreamName != "gpt-a" || listed[1].UpstreamName != "gpt-b" {
		t.Fatalf("listed site models = %#v", listed)
	}
	if got.ID != modelID {
		t.Fatalf("got site model = %#v", got)
	}
	if statusUpdated.Status != "disabled" || saved[1].Status != "disabled" {
		t.Fatalf("status update = item:%#v saved:%#v", statusUpdated, saved[1])
	}
	if !canonicalUpdated.CanonicalID.Valid || canonicalUpdated.CanonicalID.UUID != canonicalID ||
		saved[2].MatchSource != "manual" || saved[2].MatchConfidence != 95 ||
		!saved[2].MatchedAt.Valid || !saved[2].MatchedAt.Time.Equal(matchedAt) {
		t.Fatalf("canonical update = item:%#v saved:%#v", canonicalUpdated, saved[2])
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
}

func TestRouteCooldownRepositoryActivateAndClearActiveOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	activeUntil := time.Date(2026, 6, 23, 15, 0, 0, 0, time.UTC)
	db := storeTransactionGorm(t, "route cooldown")
	var clearWheres []string
	var clearVars [][]any
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		values, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			tx.AddError(errors.New("unexpected route cooldown update destination"))
			return
		}
		cleared, ok := values["cleared_at"].(sql.NullTime)
		if !ok || !cleared.Valid {
			tx.AddError(errors.New("route cooldown update missing cleared_at"))
			return
		}
		tx.Statement.Build("WHERE")
		clearWheres = append(clearWheres, tx.Statement.SQL.String())
		clearVars = append(clearVars, append([]any(nil), tx.Statement.Vars...))
		tx.Statement.RowsAffected = 1
	})
	var created RouteCooldown
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*RouteCooldown)
		if !ok {
			tx.AddError(errors.New("unexpected route cooldown create destination"))
			return
		}
		created = *item
		tx.Statement.RowsAffected = 1
	})

	repo := NewRouteCooldownRepository(db)
	item, err := repo.Activate(context.Background(), ActivateRouteCooldownParams{
		SiteID:           siteID,
		SiteModelID:      modelID,
		SiteCredentialID: credentialID,
		ActiveUntil:      activeUntil,
	})
	if err != nil {
		t.Fatalf("Activate returned error: %v", err)
	}
	if err := repo.ClearActive(context.Background(), siteID, modelID, credentialID, "manual"); err != nil {
		t.Fatalf("ClearActive returned error: %v", err)
	}

	if item.Scope != "credential" || created.Scope != "credential" || created.Source != "manual" ||
		created.Reason != "cooldown" || !created.ActiveUntil.Equal(activeUntil) || string(created.Metadata) != "{}" {
		t.Fatalf("created route cooldown = item:%#v captured:%#v", item, created)
	}
	if len(clearWheres) != 2 {
		t.Fatalf("cooldown clear updates = %d, want activate clear and explicit clear", len(clearWheres))
	}
	if !strings.Contains(strings.ToLower(clearWheres[0]), `"reason"`) {
		t.Fatalf("activation clear where %q must preserve dedicated subscription cooldowns", clearWheres[0])
	}
	protected := false
	for _, value := range clearVars[0] {
		if value == CooldownReasonUpstreamSubscriptionLimitExceeded {
			protected = true
		}
	}
	if !protected {
		t.Fatalf("activation clear vars %#v missing dedicated subscription cooldown reason", clearVars[0])
	}
	for _, where := range clearWheres {
		lower := strings.ToLower(where)
		for _, want := range []string{`"site_id"`, `"cleared_at" is null`, `"site_model_id"`, `"site_credential_id"`, `"source"`} {
			if !strings.Contains(lower, want) {
				t.Fatalf("clear where %q missing %q", where, want)
			}
		}
	}
}

func TestAdminSessionRepositoryTouchUsesConditionalUpdateOffline(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	now := time.Date(2026, 6, 23, 16, 0, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(errors.New("touch should not query before updating"))
	})
	updateCount := 0
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateCount++
		if _, ok := tx.Statement.Clauses["WHERE"]; !ok {
			tx.AddError(errors.New("touch update should include a condition"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	if err := NewAdminSessionRepository(db).TouchLastSeen(context.Background(), sessionID, now, time.Minute); err != nil {
		t.Fatalf("TouchLastSeen returned error: %v", err)
	}
	if updateCount != 1 {
		t.Fatalf("update count = %d, want one conditional update", updateCount)
	}
}
