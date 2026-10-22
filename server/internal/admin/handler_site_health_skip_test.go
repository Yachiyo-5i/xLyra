package admin

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSiteHealthPayloadSkippedIsNotFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	payload := siteHealthResultPayload(sitepkg.HealthResult{
		Site: store.Site{
			ID:        siteID,
			Name:      "Acme",
			SiteType:  "acme.example/pool",
			CreatedAt: now,
			UpdatedAt: now,
		},
		State: store.SiteHealthState{
			SiteID:    siteID,
			Status:    "healthy",
			CreatedAt: now,
			UpdatedAt: now,
		},
		Skipped:    true,
		SkipReason: `provider plugin for site_type "acme.example/pool" is unavailable; health check skipped`,
		Snapshot: store.HealthSnapshot{
			SiteID:       siteID,
			Scope:        "site",
			Source:       "manual",
			Success:      false,
			ErrorType:    sql.NullString{String: "provider_unavailable", Valid: true},
			ErrorMessage: sql.NullString{String: "provider plugin is unavailable", Valid: true},
			CheckedAt:    now,
		},
	})
	if payload["ok"] != true {
		t.Fatalf("ok = %#v, want true for a skipped check", payload["ok"])
	}
	if payload["skipped"] != true {
		t.Fatalf("skipped = %#v", payload["skipped"])
	}
	reason, _ := payload["skip_reason"].(string)
	if reason == "" {
		t.Fatalf("skip_reason = %#v", payload["skip_reason"])
	}
	if _, ok := payload["snapshot"]; ok {
		t.Fatalf("skipped payload includes a snapshot: %#v", payload["snapshot"])
	}
}
