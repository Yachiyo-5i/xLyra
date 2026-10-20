package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestModelsPayloadDropsUnavailablePluginSiteTypes(t *testing.T) {
	builtinSite := uuid.New()
	enabledSite := uuid.New()
	pausedSite := uuid.New()
	builtinModel := uuid.New()
	pausedModel := uuid.New()
	sharedModel := uuid.New()
	enabledModel := uuid.New()
	sites := []store.Site{
		{ID: builtinSite, Status: "active", Enabled: true, SiteType: "openai"},
		{ID: enabledSite, Status: "active", Enabled: true, SiteType: " ACME.EXAMPLE/Chat "},
		{ID: pausedSite, Status: "active", Enabled: true, SiteType: "paused.example/chat"},
	}
	canonicals := []store.CanonicalModel{
		{ID: builtinModel, ModelKey: "builtin-only", Status: "active"},
		{ID: pausedModel, ModelKey: "plugin-only", Status: "active"},
		{ID: sharedModel, ModelKey: "shared", Status: "active"},
		{ID: enabledModel, ModelKey: "enabled-plugin", Status: "active"},
	}
	siteModels := []store.SiteModel{
		{ID: uuid.New(), SiteID: builtinSite, Status: "active", CanonicalID: uuid.NullUUID{UUID: builtinModel, Valid: true}},
		{ID: uuid.New(), SiteID: pausedSite, Status: "active", CanonicalID: uuid.NullUUID{UUID: pausedModel, Valid: true}},
		{ID: uuid.New(), SiteID: builtinSite, Status: "active", CanonicalID: uuid.NullUUID{UUID: sharedModel, Valid: true}},
		{ID: uuid.New(), SiteID: pausedSite, Status: "active", CanonicalID: uuid.NullUUID{UUID: sharedModel, Valid: true}},
		{ID: uuid.New(), SiteID: enabledSite, Status: "active", CanonicalID: uuid.NullUUID{UUID: enabledModel, Valid: true}},
	}
	credentials := make([]store.SiteCredential, 0, len(sites))
	for _, site := range sites {
		credentials = append(credentials, store.SiteCredential{
			ID: uuid.New(), SiteID: site.ID, CredentialType: "api_key", Meta: store.JSON(`{"enabled":true}`),
		})
	}
	db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.CanonicalModel:
			*dest = append([]store.CanonicalModel(nil), canonicals...)
		case *[]store.SiteModel:
			*dest = append([]store.SiteModel(nil), siteModels...)
		case *[]store.Site:
			*dest = append([]store.Site(nil), sites...)
		case *[]store.SiteCredential:
			*dest = append([]store.SiteCredential(nil), credentials...)
		case *[]store.SiteAPIKeyState, *[]store.SiteAPIKeyModel, *[]store.RouteCooldown:
		default:
			tx.AddError(fmt.Errorf("unexpected models query %T", dest))
		}
	})
	access := auth.APIKeyAccessSets{APIKey: store.APIKey{ID: uuid.New(), SitePolicy: "allow_all", ModelPolicy: "allow_all"}}
	h := Handler{db: gatewayStoreWithGorm(t, db)}

	unfiltered, err := h.buildModelsPayloadForAccess(context.Background(), access)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelCatalogIDs(unfiltered); got != "builtin-only,plugin-only,shared,enabled-plugin" {
		t.Fatalf("nil router catalog = %s", got)
	}

	h.router = routeengine.NewService(nil)
	stillOpen, err := h.buildModelsPayloadForAccess(context.Background(), access)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelCatalogIDs(stillOpen); got != "builtin-only,plugin-only,shared,enabled-plugin" {
		t.Fatalf("nil filter catalog = %s", got)
	}

	h.router.SetSiteTypeFilter(func(siteType string) bool {
		key := strings.ToLower(strings.TrimSpace(siteType))
		if !strings.Contains(key, "/") {
			return true
		}
		return key == "acme.example/chat"
	})
	filtered, err := h.buildModelsPayloadForAccess(context.Background(), access)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelCatalogIDs(filtered); got != "builtin-only,shared,enabled-plugin" {
		t.Fatalf("filtered catalog = %s", got)
	}
	sharedTypes := modelCatalogSiteTypes(filtered, "shared")
	if sharedTypes != "openai" {
		t.Fatalf("shared site types = %s, want openai", sharedTypes)
	}
	if got := modelCatalogSiteTypes(filtered, "enabled-plugin"); got != "acme.example/chat" {
		t.Fatalf("enabled plugin site types = %s", got)
	}
	if got := modelCatalogSiteTypes(filtered, "builtin-only"); got != "openai" {
		t.Fatalf("builtin site types = %s", got)
	}
}

func modelCatalogIDs(payload map[string]any) string {
	rows, _ := payload["data"].([]map[string]any)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		ids = append(ids, id)
	}
	return strings.Join(ids, ",")
}

func modelCatalogSiteTypes(payload map[string]any, modelID string) string {
	rows, _ := payload["data"].([]map[string]any)
	for _, row := range rows {
		id, _ := row["id"].(string)
		if id != modelID {
			continue
		}
		metadata, _ := row["metadata"].(map[string]any)
		types, _ := metadata["site_types"].([]string)
		return strings.Join(types, ",")
	}
	return ""
}
