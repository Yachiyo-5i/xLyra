package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestClearActiveMatchingBuildsScopedWhereOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	db := storeRepositoryOfflineGorm(t)

	var captured string
	var vars []any
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.Build("UPDATE", "SET", "WHERE")
		captured = tx.Statement.SQL.String()
		vars = tx.Statement.Vars
		tx.Statement.RowsAffected = 1
	})

	affected, err := NewRouteCooldownRepository(db).ClearActiveMatching(context.Background(), ClearActiveCooldownFilter{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Reasons:          CodexQuotaCooldownReasons(),
	})
	if err != nil {
		t.Fatalf("ClearActiveMatching returned error: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}

	lower := strings.ToLower(captured)
	// A single reason collapses IN(...) to "=" in GORM; assert the column is present
	// and the reason value is bound (checked below) rather than the operator.
	for _, want := range []string{`"site_id"`, `"cleared_at" is null`, `"site_credential_id"`, `"reason"`} {
		if !strings.Contains(lower, want) {
			t.Fatalf("generated SQL %q missing %q", captured, want)
		}
	}

	foundReason := false
	for _, v := range vars {
		if s, ok := v.(string); ok && s == CooldownReasonCodexUsageLimitReached {
			foundReason = true
		}
	}
	if !foundReason {
		t.Fatalf("bound vars %#v missing quota reason", vars)
	}
}

func TestUpdateActiveUntilBuildsScopedUpdateOffline(t *testing.T) {
	t.Parallel()

	cooldownID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	var captured string
	var vars []any
	var destination map[string]any
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if updates, ok := tx.Statement.Dest.(map[string]any); ok {
			destination = updates
		}
		tx.Statement.Build("WHERE")
		captured = tx.Statement.SQL.String()
		vars = tx.Statement.Vars
		tx.Statement.RowsAffected = 1
	})

	metadata := JSON(`{"limit_window":"weekly","reset_at":"2026-08-21T14:29:08Z"}`)
	err := NewRouteCooldownRepository(db).UpdateActiveUntil(context.Background(), cooldownID, time.Date(2026, 8, 21, 14, 29, 8, 0, time.UTC), metadata)
	if err != nil {
		t.Fatalf("UpdateActiveUntil returned error: %v", err)
	}
	lower := strings.ToLower(captured)
	for _, want := range []string{"id =", "cleared_at is null"} {
		if !strings.Contains(lower, want) {
			t.Fatalf("generated SQL %q missing %q", captured, want)
		}
	}
	if _, ok := destination["active_until"]; !ok {
		t.Fatalf("update destination %#v missing active_until", destination)
	}
	if _, ok := destination["metadata"]; !ok {
		t.Fatalf("update destination %#v missing metadata", destination)
	}
	foundID := false
	for _, value := range vars {
		if got, ok := value.(uuid.UUID); ok && got == cooldownID {
			foundID = true
		}
	}
	if !foundID {
		t.Fatalf("bound vars %#v missing cooldown id %s", vars, cooldownID)
	}
}

func TestCountRecentActivationsBuildsScopedWhereOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)

	var captured string
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.Build("WHERE")
		captured = tx.Statement.SQL.String()
		if dest, ok := tx.Statement.Dest.(*int64); ok {
			*dest = 2
		}
		tx.Statement.RowsAffected = 1
	})

	count, err := NewRouteCooldownRepository(db).CountRecentActivations(context.Background(), CountRouteCooldownActivationsParams{
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		Source:      "gateway",
		Since:       time.Now().Add(-30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CountRecentActivations returned error: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	lower := strings.ToLower(captured)
	for _, want := range []string{`"site_id"`, `"created_at"`, `"site_model_id"`, `"site_credential_id" is null`, `"source"`} {
		if !strings.Contains(lower, want) {
			t.Fatalf("generated SQL %q missing %q", captured, want)
		}
	}
}

func TestCountRecentActivationsWithoutSiteIsNoOpOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(context.Canceled)
	})

	count, err := NewRouteCooldownRepository(db).CountRecentActivations(context.Background(), CountRouteCooldownActivationsParams{})
	if err != nil {
		t.Fatalf("CountRecentActivations returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0 for empty site", count)
	}
}

func TestTransientRouteCooldownClassification(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	model := uuid.NullUUID{UUID: modelID, Valid: true}
	credential := uuid.NullUUID{UUID: credentialID, Valid: true}

	if !TransientRouteCooldown(RouteCooldown{SiteID: siteID, SiteModelID: model, Source: "gateway", Reason: "upstream_timeout"}) {
		t.Fatal("gateway model cooldown with recoverable reason should be transient")
	}
	if TransientRouteCooldown(RouteCooldown{SiteID: siteID, SiteModelID: model, Source: "gateway", Reason: CooldownReasonUpstreamModelNotFound}) {
		t.Fatal("model-not-found cooldown must stay persistent")
	}
	if TransientRouteCooldown(RouteCooldown{SiteID: siteID, SiteModelID: model, SiteCredentialID: credential, Source: "gateway", Reason: "upstream_timeout"}) {
		t.Fatal("credential cooldown must not be classified as transient model cooldown")
	}
	if TransientRouteCooldown(RouteCooldown{SiteID: siteID, SiteModelID: model, Source: "manual", Reason: "upstream_timeout"}) {
		t.Fatal("manual cooldown must stay a hard exclusion")
	}
	if TransientRouteCooldown(RouteCooldown{SiteID: siteID, Source: "gateway", Reason: "upstream_timeout"}) {
		t.Fatal("site-scope cooldown must stay a hard exclusion")
	}
}

func TestClearActiveMatchingWithoutSiteIsNoOpOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(context.Canceled) // must never run
	})

	affected, err := NewRouteCooldownRepository(db).ClearActiveMatching(context.Background(), ClearActiveCooldownFilter{})
	if err != nil {
		t.Fatalf("ClearActiveMatching returned error: %v", err)
	}
	if affected != 0 {
		t.Fatalf("affected = %d, want 0 for empty site", affected)
	}
}
