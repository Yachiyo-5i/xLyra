package site

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestSetModelsEnabledValidatesIDsBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	_, err := service.SetModelsEnabled(t.Context(), uuid.Nil, []uuid.UUID{uuid.New()}, true)
	assertSiteErrorContains(t, "SetModelsEnabled nil site id", err, "site id is required")
	_, err = service.SetModelsEnabled(t.Context(), uuid.New(), nil, true)
	assertSiteErrorContains(t, "SetModelsEnabled empty model ids", err, "model_ids is required")
	_, err = service.SetModelsEnabled(t.Context(), uuid.New(), []uuid.UUID{uuid.Nil, uuid.Nil}, false)
	assertSiteErrorContains(t, "SetModelsEnabled nil-only model ids", err, "model_ids is required")
}

func TestSaveCredentialInputRejectsUnmarshalableMetaBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	_, err := service.saveCredentialInput(t.Context(), store.SiteCredentialRepository{}, uuid.New(), CredentialInput{
		Type:   defaultCredentialType,
		Secret: "sk-test",
		Meta: map[string]any{
			"bad": make(chan int),
		},
	})
	assertSiteErrorContains(t, "saveCredentialInput bad meta", err, "marshal credential meta")
}

func TestCreateValidationGuardsNormalizeBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	_, _, err := service.Create(t.Context(), CreateSiteParams{
		SiteType: "openai",
		BaseURL:  " ",
	})
	assertSiteErrorContains(t, "Create missing required fields", err, "name, slug, and base_url are required")
	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name:     "Primary",
		Slug:     "primary",
		SiteType: "unsupported",
		BaseURL:  "https://api.example.com/",
	})
	assertSiteErrorContains(t, "Create unsupported site type", err, `unsupported site_type "unsupported"`)

	priority := 6.0
	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name:            "Primary",
		Slug:            "primary",
		SiteType:        "openai",
		BaseURL:         "https://api.example.com/",
		RoutingPriority: &priority,
	})
	assertSiteErrorContains(t, "Create invalid routing priority", err, "routing_priority must be between 1.0 and 5.0")
}

func TestMergeStringMetaRecoversFromInvalidExistingJSON(t *testing.T) {
	t.Parallel()

	value := " proxy-main "
	merged := mergeStringMeta(store.JSON(`{`), "proxy_id", &value)
	if got := proxyIDFromMeta(merged); got != "proxy-main" {
		t.Fatalf("proxyIDFromMeta(merged invalid meta) = %q, want proxy-main", got)
	}

	blank := " "
	merged = mergeStringMeta(merged, "proxy_id", &blank)
	if got := proxyIDFromMeta(merged); got != "" {
		t.Fatalf("blank proxy id merge should remove key, got %q", got)
	}
}

func TestEnrichModelCapabilitiesPureGuardBranches(t *testing.T) {
	t.Parallel()

	model := adapter.Model{UpstreamName: "gpt-test", Capabilities: map[string]any{"existing": true}}
	if got := (*Service)(nil).enrichModelCapabilities(t.Context(), store.Site{SiteType: "openai"}, model); got.UpstreamName != model.UpstreamName {
		t.Fatalf("nil service enrich result = %#v, want original model", got)
	}

	service := siteServiceWithoutStore()
	blank := service.enrichModelCapabilities(t.Context(), store.Site{SiteType: "openai"}, adapter.Model{UpstreamName: " "})
	if blank.UpstreamName != " " || blank.Capabilities != nil {
		t.Fatalf("blank upstream model should be unchanged, got %#v", blank)
	}

	unknownProvider := service.enrichModelCapabilities(t.Context(), store.Site{SiteType: "unknown"}, model)
	if unknownProvider.Capabilities["existing"] != true {
		t.Fatalf("unknown provider model should be unchanged, got %#v", unknownProvider)
	}
}

func TestApplyModelNameEndpointTypesPreservesGrokDeclaration(t *testing.T) {
	model := applyModelNameEndpointTypes(store.Site{SiteType: "grok"}, adapter.Model{
		UpstreamName: "grok-4.5",
		Capabilities: map[string]any{
			"source":                   "grok",
			"supported_endpoint_types": []string{"openai-response"},
		},
	})
	endpoints, _ := model.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "openai-response" {
		t.Fatalf("Grok endpoints = %#v, want upstream declaration", endpoints)
	}
}

func TestGrokSiteModelCapabilitiesPreservesStoredDeclaration(t *testing.T) {
	encoded := grokSiteModelCapabilities(store.JSON(`{"source":"grok","supported_endpoint_types":["openai-response"]}`), "grok-4.5")
	capabilities := map[string]any{}
	if err := json.Unmarshal(encoded, &capabilities); err != nil {
		t.Fatal(err)
	}
	endpoints, _ := capabilities["supported_endpoint_types"].([]any)
	if len(endpoints) != 1 || endpoints[0] != "openai-response" {
		t.Fatalf("stored Grok endpoints = %#v", capabilities["supported_endpoint_types"])
	}
}

func TestSiteHealthHourlyBuildsMissingAndMixedBuckets(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	now := time.Now().UTC().Truncate(time.Hour)
	snapshots := []store.HealthSnapshot{
		{SiteID: siteID, Scope: "site", Source: "scheduler", Success: true, CheckedAt: now.Add(-2 * time.Hour)},
		{SiteID: siteID, Scope: "site", Source: "scheduler", Success: false, CheckedAt: now.Add(-2*time.Hour + 10*time.Minute)},
		{SiteID: siteID, Scope: "site", Source: "scheduler", Success: true, CheckedAt: now},
		{SiteID: uuid.New(), Scope: "site", Source: "scheduler", Success: false, CheckedAt: now},
	}
	service := NewServiceWithTimeZone(siteStoreWithHealthSnapshots(t, siteID, snapshots), siteTestMasterKey, config.TimeZone{
		Name:     "UTC",
		Location: time.UTC,
	})

	buckets, err := service.SiteHealthHourly(context.Background(), siteID, 3)
	if err != nil {
		t.Fatalf("SiteHealthHourly() error = %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("SiteHealthHourly() returned %d buckets, want 3: %#v", len(buckets), buckets)
	}
	if buckets[0].Status != "degraded" || buckets[0].SuccessCount != 1 || buckets[0].FailureCount != 1 || buckets[0].TotalCount != 2 {
		t.Fatalf("first bucket = %#v, want mixed degraded bucket", buckets[0])
	}
	if buckets[1].Status != "idle" || buckets[1].TotalCount != 0 {
		t.Fatalf("middle bucket = %#v, want idle empty bucket", buckets[1])
	}
	if buckets[2].Status != "healthy" || buckets[2].SuccessCount != 1 || buckets[2].FailureCount != 0 || buckets[2].TotalCount != 1 {
		t.Fatalf("last bucket = %#v, want healthy bucket", buckets[2])
	}
}

func siteStoreWithHealthSnapshots(t *testing.T, siteID uuid.UUID, snapshots []store.HealthSnapshot) *store.Store {
	t.Helper()

	db := siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{
				ID:       siteID,
				Name:     "Primary",
				Slug:     "primary",
				SiteType: "openai",
				BaseURL:  "https://api.example.com",
				Status:   "active",
				Enabled:  true,
			}
			tx.RowsAffected = 1
		case *[]store.HealthSnapshot:
			filtered := (*dest)[:0]
			for _, snapshot := range snapshots {
				if snapshot.SiteID == siteID && snapshot.Scope == "site" && snapshot.Source == "scheduler" {
					filtered = append(filtered, snapshot)
				}
			}
			*dest = filtered
			tx.RowsAffected = int64(len(filtered))
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}})
	return siteStoreWithGorm(t, db)
}
