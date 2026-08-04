package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDevPostgresSiteModelRepositoryWriteSmoke(t *testing.T) {
	requireDevPostgresWriteSmoke(t)
	ctx, gormDB := openDevPostgresWriteSmoke(t)

	repo := NewSiteModelRepository(gormDB)
	site := createSmokeSite(t, ctx, gormDB)
	defer deleteSmokeRecord(t, gormDB, &Site{}, site.ID)
	canonical := createSmokeCanonicalModel(t, ctx, gormDB)
	defer deleteSmokeRecord(t, gormDB, &CanonicalModel{}, canonical.ID)
	matchedAt := time.Now().UTC().Truncate(time.Microsecond)

	created, err := repo.Upsert(ctx, UpsertSiteModelParams{
		SiteID:       site.ID,
		UpstreamName: "xlyra-coverage-smoke-model",
		DisplayName:  "Coverage Smoke Model",
		Capabilities: JSON(`{"source":"smoke"}`),
		Status:       "available",
	})
	if err != nil {
		t.Fatalf("create site model: %v", err)
	}
	defer deleteSmokeRecord(t, gormDB, &SiteModel{}, created.ID)

	updated, err := repo.Upsert(ctx, UpsertSiteModelParams{
		SiteID:       site.ID,
		UpstreamName: created.UpstreamName,
		DisplayName:  "Coverage Smoke Model Updated",
		Status:       "available",
	})
	if err != nil {
		t.Fatalf("update site model: %v", err)
	}
	if updated.ID != created.ID || updated.DisplayName != "Coverage Smoke Model Updated" || string(updated.Capabilities) != "{}" {
		t.Fatalf("unexpected updated site model: %#v", updated)
	}

	if _, err := repo.UpdateStatus(ctx, site.ID, created.ID, "disabled"); err != nil {
		t.Fatalf("disable site model: %v", err)
	}
	if _, err := repo.Upsert(ctx, UpsertSiteModelParams{
		SiteID:       site.ID,
		UpstreamName: created.UpstreamName,
		DisplayName:  "Coverage Smoke Model Still Disabled",
		Status:       "available",
	}); err != nil {
		t.Fatalf("upsert disabled site model: %v", err)
	}
	disabled, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("get disabled site model: %v", err)
	}
	if disabled.Status != "disabled" {
		t.Fatalf("disabled site model status = %q, want disabled", disabled.Status)
	}

	mapped, err := repo.UpdateCanonical(ctx, created.ID, canonical.ID, "smoke", 91, matchedAt)
	if err != nil {
		t.Fatalf("update canonical mapping: %v", err)
	}
	if !mapped.CanonicalID.Valid || mapped.CanonicalID.UUID != canonical.ID || mapped.MatchSource != "smoke" || mapped.MatchConfidence != 91 {
		t.Fatalf("unexpected canonical mapping: %#v", mapped)
	}
	matches, err := repo.ListByCanonical(ctx, canonical.ID)
	if err != nil {
		t.Fatalf("list by canonical: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != created.ID {
		t.Fatalf("canonical matches = %#v, want created model", matches)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete site model: %v", err)
	}
	if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get deleted site model error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestDevPostgresUsageRecordRepositoryCreateSmoke(t *testing.T) {
	requireDevPostgresWriteSmoke(t)
	ctx, gormDB := openDevPostgresWriteSmoke(t)

	repo := NewUsageRecordRepository(gormDB)
	requestLog := createSmokeRequestLog(t, ctx, gormDB)
	defer deleteSmokeRecord(t, gormDB, &RequestLog{}, requestLog.ID)

	item, err := repo.Create(ctx, CreateUsageRecordParams{
		RequestLogID:     requestLog.ID,
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
		EstimatedCost:    0.0123,
	})
	if err != nil {
		t.Fatalf("create usage record: %v", err)
	}
	defer deleteSmokeRecord(t, gormDB, &UsageRecord{}, item.ID)

	if item.ID == uuid.Nil || item.RequestLogID != requestLog.ID {
		t.Fatalf("unexpected usage record identity: %#v", item)
	}
	if item.APIKeyID.Valid || item.SiteID.Valid || item.CanonicalModelID.Valid {
		t.Fatalf("unexpected usage record nullable ids: %#v", item)
	}
	if item.Currency != "USD" || !item.EstimatedCost.Valid || item.EstimatedCost.Float64 != 0.0123 {
		t.Fatalf("unexpected usage record cost: %#v", item)
	}
}

func TestDevPostgresSiteCredentialRepositoryWriteSmoke(t *testing.T) {
	requireDevPostgresWriteSmoke(t)
	ctx, gormDB := openDevPostgresWriteSmoke(t)

	repo := NewSiteCredentialRepository(gormDB)
	site := createSmokeSite(t, ctx, gormDB)
	defer deleteSmokeRecord(t, gormDB, &Site{}, site.ID)

	created, err := repo.Upsert(ctx, UpsertSiteCredentialParams{
		SiteID:          site.ID,
		CredentialType:  "api_key",
		EncryptedSecret: "encrypted-smoke",
		MaskedSecret:    "sk-...smoke",
	})
	if err != nil {
		t.Fatalf("create site credential: %v", err)
	}
	defer deleteSmokeRecord(t, gormDB, &SiteCredential{}, created.ID)

	if string(created.Meta) != "{}" {
		t.Fatalf("created meta = %s, want default object", string(created.Meta))
	}
	updated, err := repo.Upsert(ctx, UpsertSiteCredentialParams{
		SiteID:          site.ID,
		CredentialType:  "api_key",
		EncryptedSecret: "encrypted-smoke-updated",
		MaskedSecret:    "sk-...updated",
		Meta:            JSON(`{"enabled":true}`),
	})
	if err != nil {
		t.Fatalf("update site credential: %v", err)
	}
	if updated.ID != created.ID || updated.EncryptedSecret != "encrypted-smoke-updated" || string(updated.Meta) != `{"enabled":true}` {
		t.Fatalf("unexpected updated credential: %#v", updated)
	}

	renamed, err := repo.UpdateCredentialType(ctx, created.ID, " api_key:1 ")
	if err != nil {
		t.Fatalf("update credential type: %v", err)
	}
	if renamed.CredentialType != "api_key:1" {
		t.Fatalf("credential type = %q, want trimmed api_key:1", renamed.CredentialType)
	}
	metaUpdated, err := repo.UpdateMeta(ctx, created.ID, nil)
	if err != nil {
		t.Fatalf("update credential meta: %v", err)
	}
	if string(metaUpdated.Meta) != "{}" {
		t.Fatalf("meta update default = %s, want empty object", string(metaUpdated.Meta))
	}

	if err := repo.DeleteBySiteAndType(ctx, site.ID, "api_key:1"); err != nil {
		t.Fatalf("delete by site and type: %v", err)
	}
	if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("get deleted credential error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func createSmokeSite(t *testing.T, ctx context.Context, db *gorm.DB) Site {
	t.Helper()

	site, err := NewSiteRepository(db).Create(ctx, CreateSiteParams{
		Name:            "xLyra Coverage Smoke",
		Slug:            "xlyra-coverage-smoke-" + uuid.NewString(),
		SiteType:        "smoke",
		BaseURL:         "https://example.invalid",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatalf("create smoke site: %v", err)
	}
	return site
}

func createSmokeCanonicalModel(t *testing.T, ctx context.Context, db *gorm.DB) CanonicalModel {
	t.Helper()

	model, err := NewCanonicalModelRepository(db).Create(ctx, UpsertCanonicalModelParams{
		ModelKey:    "xlyra-coverage-smoke-" + uuid.NewString(),
		DisplayName: "xLyra Coverage Smoke",
		Provider:    "smoke",
		Category:    "chat",
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create smoke canonical model: %v", err)
	}
	return model
}

func createSmokeRequestLog(t *testing.T, ctx context.Context, db *gorm.DB) RequestLog {
	t.Helper()

	log, err := NewRequestLogRepository(db).Create(ctx, CreateRequestLogParams{
		RequestID:  "xlyra-coverage-smoke-" + uuid.NewString(),
		Endpoint:   "/v1/smoke",
		StatusCode: 200,
		Success:    true,
	})
	if err != nil {
		t.Fatalf("create smoke request log: %v", err)
	}
	return log
}

func openDevPostgresWriteSmoke(t *testing.T) (context.Context, *gorm.DB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	cfg, err := devPostgresSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactDatabaseOpenError(err, cfg))
	}
	t.Cleanup(db.Close)

	return ctx, db.DB()
}

func requireDevPostgresWriteSmoke(t *testing.T) {
	t.Helper()

	if strings.TrimSpace(os.Getenv("XLYRA_DEV_POSTGRES_WRITE_SMOKE")) != "1" {
		t.Skip("set XLYRA_DEV_POSTGRES_WRITE_SMOKE=1 to run dev PostgreSQL write smoke tests")
	}
}

func deleteSmokeRecord(t *testing.T, db *gorm.DB, model any, id uuid.UUID) {
	t.Helper()

	if id == uuid.Nil {
		return
	}
	if err := db.Delete(model, id).Error; err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cleanup smoke record %s: %v", id, err)
	}
}
