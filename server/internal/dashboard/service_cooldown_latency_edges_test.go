package dashboard

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestCooldownsBuildsModelAndCredentialMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	siteID := uuid.New()
	deletedSiteID := uuid.New()
	siteModelID := uuid.New()
	canonicalID := uuid.New()
	credentialID := uuid.New()
	cooldownID := uuid.New()
	service := NewService(dashboardStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.RouteCooldown:
			*dest = []store.RouteCooldown{
				{
					ID:               cooldownID,
					SiteID:           siteID,
					SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
					SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
					Scope:            "site_model",
					Source:           "gateway",
					Reason:           "rate_limited",
					ActiveUntil:      now.Add(90 * time.Second),
					Metadata:         store.JSON(`{"attempts":3}`),
				},
				{ID: uuid.New(), SiteID: deletedSiteID, ActiveUntil: now.Add(time.Minute)},
			}
			tx.RowsAffected = int64(len(*dest))
		case *[]store.Site:
			*dest = []store.Site{
				{ID: siteID, Name: "Primary", Slug: "primary", Status: "active", Enabled: true},
				{ID: deletedSiteID, Name: "Deleted", Status: store.SiteStatusDeleted, Enabled: true},
			}
			tx.RowsAffected = int64(len(*dest))
		case *[]store.SiteModel:
			*dest = []store.SiteModel{{ID: siteModelID, CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}, UpstreamName: "gpt-upstream"}}
			tx.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: canonicalID, ModelKey: "gpt-canonical"}}
			tx.RowsAffected = 1
		case *[]store.SiteCredential:
			*dest = []store.SiteCredential{{ID: credentialID, CredentialType: "api_key", MaskedSecret: "sk-...abcd"}}
			tx.RowsAffected = 1
		case *[]store.SiteAPIKeyState:
			*dest = []store.SiteAPIKeyState{{SiteCredentialID: credentialID, Name: "Production Key"}}
			tx.RowsAffected = 1
		default:
			tx.AddError(fmt.Errorf("unexpected dashboard cooldown query destination %T", tx.Statement.Dest))
		}
	}), config.LoadTimeZone("UTC"))
	window := service.newTimeWindow(1, now)

	items, err := service.cooldowns(context.Background(), window)
	if err != nil {
		t.Fatalf("cooldowns: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %#v, want only non-deleted site cooldown", items)
	}
	item := items[0]
	if item.ID != cooldownID.String() || item.SiteName != "Primary" || item.RemainingSeconds != 90 {
		t.Fatalf("cooldown identity = %#v", item)
	}
	if item.CanonicalModel == nil || *item.CanonicalModel != "gpt-canonical" || item.UpstreamModelName == nil || *item.UpstreamModelName != "gpt-upstream" {
		t.Fatalf("model metadata = %#v", item)
	}
	if item.CredentialName == nil || *item.CredentialName != "Production Key" || item.MaskedKey == nil || *item.MaskedKey != "sk-...abcd" {
		t.Fatalf("credential metadata = %#v", item)
	}
	if item.Metadata["attempts"] != float64(3) {
		t.Fatalf("metadata = %#v, want attempts", item.Metadata)
	}
}

func TestHighLatencyAggregatesAndSortsRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	siteID := uuid.New()
	modelID := uuid.New()
	otherModelID := uuid.New()
	service := NewService(dashboardStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.RequestLog:
			*dest = []store.RequestLog{
				{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CreatedAt: now.Add(-time.Hour), LatencyMS: sql.NullInt64{Int64: 100, Valid: true}},
				{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CreatedAt: now.Add(-30 * time.Minute), LatencyMS: sql.NullInt64{Int64: 1000, Valid: true}},
				{CanonicalModelID: uuid.NullUUID{UUID: otherModelID, Valid: true}, CreatedAt: now.Add(-20 * time.Minute), LatencyMS: sql.NullInt64{Int64: 700, Valid: true}},
				{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CreatedAt: now.Add(-48 * time.Hour), LatencyMS: sql.NullInt64{Int64: 9999, Valid: true}},
				{SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, CreatedAt: now.Add(-10 * time.Minute)},
			}
			tx.RowsAffected = int64(len(*dest))
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, Name: "Primary"}}
			tx.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: modelID, ModelKey: "gpt-main"}}
			tx.RowsAffected = 1
		default:
			tx.AddError(fmt.Errorf("unexpected dashboard high latency query destination %T", tx.Statement.Dest))
		}
	}), config.LoadTimeZone("UTC"))
	window := service.newTimeWindow(1, now)

	items, err := service.highLatency(context.Background(), window)
	if err != nil {
		t.Fatalf("highLatency: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("high latency items = %#v, want two aggregates", items)
	}
	if items[0].ModelKey != "gpt-main" || items[0].SiteName != "Primary" || items[0].RequestCount != 2 {
		t.Fatalf("first aggregate = %#v, want primary model aggregate first", items[0])
	}
	if items[0].AvgLatencyMS != 550 {
		t.Fatalf("avg latency = %v, want 550", items[0].AvgLatencyMS)
	}
	if items[1].ModelKey != "unknown" || items[1].SiteID != nil || items[1].RequestCount != 1 {
		t.Fatalf("second aggregate = %#v, want unknown model without site", items[1])
	}
}
