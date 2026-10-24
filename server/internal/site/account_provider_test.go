package site

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAccountProviderFor(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	seen := map[string]bool{}
	for _, desc := range SiteTypeDescriptors() {
		seen[desc.SiteType] = true
		for _, siteType := range []string{desc.SiteType, strings.ToUpper(desc.SiteType), " " + desc.SiteType + " "} {
			provider, ok := service.AccountProviderFor(siteType)
			// Plugin account pools need an injected runtime. A descriptor alone
			// does not expose /accounts.
			if desc.Accounts == nil || strings.Contains(desc.SiteType, "/") {
				if ok || provider != nil {
					t.Fatalf("AccountProviderFor(%q) = %v, %v, want none", siteType, provider, ok)
				}
				continue
			}
			if !ok || provider == nil {
				t.Fatalf("AccountProviderFor(%q) missing provider", siteType)
			}
		}
	}
	if !seen["grok"] {
		t.Fatal("registry is missing grok")
	}
	for _, siteType := range []string{"", " ", "not_a_site_type"} {
		if _, ok := service.AccountProviderFor(siteType); ok {
			t.Fatalf("AccountProviderFor(%q) = true, want false", siteType)
		}
	}
}

func TestGrokAccountProviderHasNoRegionsOrImport(t *testing.T) {
	t.Parallel()

	provider, ok := NewService(nil, "test-master-key").AccountProviderFor("grok")
	if !ok {
		t.Fatal("missing grok provider")
	}
	items, defaultID, err := provider.Regions(context.Background(), uuid.Nil)
	if err != nil || defaultID != "" || sliceLen(items) != 0 {
		t.Fatalf("regions = %#v %q %v", items, defaultID, err)
	}
	if _, err := provider.Import(context.Background(), uuid.Nil, []AccountImportFile{{Name: "a.json", Content: "{}"}}); !errors.Is(err, ErrAccountImportUnsupported) {
		t.Fatalf("import err = %v, want ErrAccountImportUnsupported", err)
	}
}

func sliceLen(items any) int {
	switch typed := items.(type) {
	case []AccountRegion:
		return len(typed)
	default:
		return -1
	}
}
