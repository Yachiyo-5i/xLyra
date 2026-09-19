package catalog

import (
	"reflect"
	"testing"

	"gorm.io/gorm"
	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/store"
)

func TestCatalogEmptyProtocolsPreserveCodexImageCapability(t *testing.T) {
	for _, test := range []struct {
		endpoints string
		want      []string
	}{
		{"null", []string{"openai-image"}},
		{"[]", []string{"openai-image"}},
		{`["openai-image"]`, []string{"openai-image"}},
		{`["openai-response"]`, []string{"openai-response"}},
	} {
		t.Run(test.endpoints, func(t *testing.T) {
			service := catalogServiceWithQueryCallback(t, func(tx *gorm.DB) {
				models := tx.Statement.Dest.(*[]store.CanonicalModel)
				*models = []store.CanonicalModel{{ModelKey: "gpt-image-2", PricingSource: store.CanonicalPricingSourceCatalog, Capabilities: store.JSON(`{}`), SupportedEndpointTypes: store.JSON(test.endpoints)}}
				tx.RowsAffected = 1
			})
			result := NewCapabilityService(service.db).Enrich(t.Context(), modelcapabilities.Input{
				Provider: "openai", SiteType: "codex", ModelID: "gpt-image-2",
				BaseCapabilities: map[string]any{"source": "codex_image_route", "supported_endpoint_types": []string{"openai-image"}},
			})
			if !reflect.DeepEqual(result.Capabilities["supported_endpoint_types"], test.want) {
				t.Fatalf("enriched protocols = %#v", result.Capabilities["supported_endpoint_types"])
			}
		})
	}
}
