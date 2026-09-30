package store

import (
	"reflect"
	"testing"
)

func TestResolveSiteModelEndpointPolicyMatchesCatalogSystemOneAlias(t *testing.T) {
	policy := ResolveSiteModelEndpointPolicy(
		CanonicalModel{SupportedEndpointTypes: JSON(`["systemone"]`)},
		SiteModel{Capabilities: JSON(`{"supported_endpoint_types":["typesafe-systemone"]}`)},
		SiteModelEndpointOverride{},
	)

	if !reflect.DeepEqual(policy.CatalogEndpointTypes, []string{"typesafe-systemone"}) {
		t.Fatalf("catalog endpoint types = %#v", policy.CatalogEndpointTypes)
	}
	if !reflect.DeepEqual(policy.SupportedEndpointTypes, []string{"typesafe-systemone"}) {
		t.Fatalf("supported endpoint types = %#v", policy.SupportedEndpointTypes)
	}
}

func TestNormalizeModelEndpointTypesDeduplicatesSystemOneAliases(t *testing.T) {
	got := NormalizeModelEndpointTypes([]string{"systemone", "typesafe-systemone", "SYSTEMONE"})
	if !reflect.DeepEqual(got, []string{"typesafe-systemone"}) {
		t.Fatalf("normalized endpoint types = %#v", got)
	}
}
