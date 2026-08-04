package site

import (
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestBuildCredentialModelPricingsUsesKeyRatioAndAvailableModelBindings(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	newAPIID := uuid.New()
	modelID := uuid.New()
	primaryID := uuid.New()
	backupID := uuid.New()
	ignoredID := uuid.New()
	items := buildCredentialModelPricings(
		[]store.Site{{ID: siteID, SiteType: "openai"}, {ID: newAPIID, SiteType: "newapi"}},
		[]store.SiteCredential{
			{ID: primaryID, SiteID: siteID, CredentialType: "api_key:primary", DisplayName: "Primary", RoutingPriority: 5, UpstreamCostMultiplier: 1.2, Meta: store.JSON(`{"enabled":true}`)},
			{ID: backupID, SiteID: siteID, CredentialType: "api_key:backup", DisplayName: "Backup", RoutingPriority: 2, UpstreamCostMultiplier: 0.8, Meta: store.JSON(`{"enabled":true}`)},
			{ID: ignoredID, SiteID: newAPIID, CredentialType: "api_key:managed", DisplayName: "Managed", UpstreamCostMultiplier: 3, Meta: store.JSON(`{"enabled":true}`)},
		},
		[]store.SiteAPIKeyState{
			{SiteCredentialID: primaryID, SiteID: siteID, Enabled: true},
			{SiteCredentialID: backupID, SiteID: siteID, Enabled: false},
			{SiteCredentialID: ignoredID, SiteID: newAPIID, Enabled: true},
		},
		[]store.SiteAPIKeyModel{
			{SiteID: siteID, SiteCredentialID: primaryID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
			{SiteID: siteID, SiteCredentialID: backupID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
			{SiteID: siteID, SiteCredentialID: primaryID, SiteModelID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Available: false, Enabled: true},
			{SiteID: newAPIID, SiteCredentialID: ignoredID, SiteModelID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Available: true, Enabled: true},
		},
	)

	if len(items) != 2 {
		t.Fatalf("credential pricing count = %d, want 2", len(items))
	}
	if items[0].SiteCredentialID != primaryID || items[0].GroupRatio != 1.2 || !items[0].CredentialUsable {
		t.Fatalf("primary pricing = %#v", items[0])
	}
	if items[1].SiteCredentialID != backupID || items[1].GroupRatio != 0.8 || items[1].CredentialEnabled || items[1].CredentialUsable {
		t.Fatalf("backup pricing = %#v", items[1])
	}
}
