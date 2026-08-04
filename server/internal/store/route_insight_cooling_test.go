package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRouteInsightCoolingHelpersRespectScopeAndSource(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	otherSiteID := uuid.New()
	siteModelID := uuid.New()
	otherSiteModelID := uuid.New()
	credentialID := uuid.New()

	cooldowns := []RouteCooldown{
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Source:      "gateway",
			Reason:      "upstream_transport_error",
		},
		{
			SiteID:      otherSiteID,
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Source:      "manual",
		},
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: otherSiteModelID, Valid: true},
			Source:      "manual",
		},
	}

	if routeCooling(cooldowns, siteID, siteModelID) {
		t.Fatal("transient and non-matching cooldowns should not block model routing")
	}

	if !routeCooling(append(cooldowns, RouteCooldown{
		SiteID: siteID,
		Source: "manual",
	}), siteID, siteModelID) {
		t.Fatal("site-level manual cooldown should block model routing")
	}

	if !credentialCooling([]RouteCooldown{{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Source:           "manual",
	}}, siteID, siteModelID, credentialID) {
		t.Fatal("credential cooldown without a model scope should block a matching credential")
	}

	if credentialCooling([]RouteCooldown{{
		SiteID:           siteID,
		SiteModelID:      uuid.NullUUID{UUID: otherSiteModelID, Valid: true},
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Source:           "manual",
	}}, siteID, siteModelID, credentialID) {
		t.Fatal("credential cooldown scoped to another model should not block this model")
	}

	if !credentialCooling([]RouteCooldown{{
		SiteID:           siteID,
		SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Source:           "gateway",
	}}, siteID, uuid.Nil, credentialID) {
		t.Fatal("fallback credential lookup should respect model-specific credential cooldowns")
	}
}

func TestRouteInsightFallbackCredentialCountFiltersUnusableCredentials(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	usableCredentialID := uuid.New()
	disabledCredentialID := uuid.New()
	missingRawKeyCredentialID := uuid.New()
	nonAPIKeyCredentialID := uuid.New()
	coolingCredentialID := uuid.New()

	count := siteFallbackCredentialCount(siteID, []SiteCredential{
		{ID: usableCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)},
		{ID: disabledCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)},
		{ID: missingRawKeyCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{"raw_key_missing":true}`)},
		{ID: nonAPIKeyCredentialID, SiteID: siteID, CredentialType: "oauth", Meta: JSON(`{}`)},
		{ID: coolingCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)},
	}, map[uuid.UUID]SiteAPIKeyState{
		usableCredentialID:   {SiteCredentialID: usableCredentialID, Enabled: true, SyncStatus: "synced"},
		disabledCredentialID: {SiteCredentialID: disabledCredentialID, Enabled: false, SyncStatus: "synced"},
		coolingCredentialID:  {SiteCredentialID: coolingCredentialID, Enabled: true, SyncStatus: "synced"},
	}, []RouteCooldown{{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: coolingCredentialID, Valid: true},
		Source:           "manual",
	}})

	if count != 1 {
		t.Fatalf("fallback credential count = %d, want 1", count)
	}
}

func TestRouteInsightGatewayCredentialTypeHelpersRequireExactMatches(t *testing.T) {
	t.Parallel()

	if !isAPIKeyCredentialType("api_key") || !isAPIKeyCredentialType("api_key:7") {
		t.Fatal("api key credential types should be recognized")
	}
	if isAPIKeyCredentialType("oauth") || isAPIKeyCredentialType(" api_key") || isAPIKeyCredentialType("API_KEY") {
		t.Fatal("non-exact api key credential types should not be recognized")
	}
	if !isNewAPISite("newapi") {
		t.Fatal("newapi site type should be recognized")
	}
	if isNewAPISite("NewAPI") || isNewAPISite(" newapi ") {
		t.Fatal("newapi site type matching should remain exact")
	}
}

func TestRouteInsightSortHelpersUseStableSemanticOrder(t *testing.T) {
	t.Parallel()

	firstBeta := uuid.New()
	secondBeta := uuid.New()
	models := []SiteModel{
		{ID: firstBeta, UpstreamName: "beta", DisplayName: "first beta"},
		{ID: uuid.New(), UpstreamName: "alpha", DisplayName: "alpha"},
		{ID: secondBeta, UpstreamName: "beta", DisplayName: "second beta"},
	}

	sortSiteModels(models)

	if models[0].UpstreamName != "alpha" || models[1].ID != firstBeta || models[2].ID != secondBeta {
		t.Fatalf("unexpected site model order: %#v", models)
	}

	lowSiteID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	highSiteID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	items := []SiteModelPricing{
		{SiteID: highSiteID, ModelName: "alpha", GroupName: "vip"},
		{SiteID: lowSiteID, ModelName: "beta", GroupName: "default"},
		{SiteID: lowSiteID, ModelName: "alpha", GroupName: "vip"},
		{SiteID: lowSiteID, ModelName: "alpha", GroupName: "default"},
	}

	sortSiteModelPricings(items)

	got := []string{
		items[0].SiteID.String() + "/" + items[0].ModelName + "/" + items[0].GroupName,
		items[1].SiteID.String() + "/" + items[1].ModelName + "/" + items[1].GroupName,
		items[2].SiteID.String() + "/" + items[2].ModelName + "/" + items[2].GroupName,
		items[3].SiteID.String() + "/" + items[3].ModelName + "/" + items[3].GroupName,
	}
	want := []string{
		lowSiteID.String() + "/alpha/default",
		lowSiteID.String() + "/alpha/vip",
		lowSiteID.String() + "/beta/default",
		highSiteID.String() + "/alpha/vip",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortSiteModelPricings[%d] = %q, want %q; all = %#v", i, got[i], want[i], got)
		}
	}
}

func TestRouteInsightCandidateHelpersKeepRecentGatewayHealthAndDefaultPricing(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	otherModelID := uuid.New()
	now := time.Now()
	row := RouteCandidateRow{SiteModelID: modelID}
	fillModelHealth(&row, []HealthSnapshot{
		{
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			Scope:       "model",
			Source:      "gateway",
			Success:     true,
			LatencyMS:   sql.NullInt64{Int64: 100, Valid: true},
			CheckedAt:   now.Add(-time.Hour),
		},
		{
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			Scope:       "model",
			Source:      "gateway",
			Success:     false,
			LatencyMS:   sql.NullInt64{Int64: 300, Valid: true},
			CheckedAt:   now.Add(-2 * time.Hour),
		},
		{SiteModelID: uuid.NullUUID{UUID: otherModelID, Valid: true}, Scope: "model", Source: "gateway", CheckedAt: now},
		{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Scope: "site", Source: "gateway", CheckedAt: now},
		{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Scope: "model", Source: "sync", CheckedAt: now},
		{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Scope: "model", Source: "gateway", CheckedAt: now.Add(-25 * time.Hour)},
	})

	if row.ModelRequestCount != 2 || !row.ModelSuccessRate.Valid || row.ModelSuccessRate.Float64 != 0.5 ||
		!row.ModelAvgLatencyMS.Valid || row.ModelAvgLatencyMS.Int64 != 200 {
		t.Fatalf("model health row = %#v, want only recent matching gateway model snapshots", row)
	}

	priced := RouteCandidateRow{SiteModelID: modelID}
	fillRoutePricing(&priced, []SiteModelPricing{
		{SiteModelID: uuid.NullUUID{UUID: otherModelID, Valid: true}, GroupName: "default", Currency: "USD", InputValue: sql.NullFloat64{Float64: 0.1, Valid: true}},
		{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, GroupName: "premium", Currency: "EUR", InputValue: sql.NullFloat64{Float64: 0.5, Valid: true}},
		{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, GroupName: "default", Currency: "JPY", PerRequestValue: sql.NullFloat64{Float64: 0.2, Valid: true}, BillingType: "request", QuotaType: 3},
	})
	if !priced.PricingGroupName.Valid || priced.PricingGroupName.String != "default" ||
		!priced.PricingCurrency.Valid || priced.PricingCurrency.String != "JPY" ||
		!priced.PricingPerRequestValue.Valid || priced.PricingBillingType.String != "request" ||
		!priced.PricingQuotaType.Valid || priced.PricingQuotaType.Int64 != 3 {
		t.Fatalf("priced row = %#v, want default pricing selected and copied", priced)
	}
}
