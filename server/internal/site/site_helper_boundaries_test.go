package site

import (
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type siteTypeOnlyModule struct{}

func (siteTypeOnlyModule) SiteTypes() []string {
	return []string{"metadata_only_site"}
}

func TestRunSiteHealthCheckRejectsRegisteredModuleWithoutHealthSupport(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	service.adapters.Register(siteTypeOnlyModule{})

	check := service.runSiteHealthCheck(t.Context(), store.Site{
		ID:       uuid.New(),
		SiteType: "metadata_only_site",
		BaseURL:  "https://example.invalid",
	})

	if check.success {
		t.Fatal("registered module without health interfaces should not pass health check")
	}
	if check.errorType != "unsupported_health_check" {
		t.Fatalf("error type = %q, want unsupported_health_check", check.errorType)
	}
	if check.endpoint != "detect" || check.method != "GET" {
		t.Fatalf("endpoint/method = %q/%q, want detect/GET", check.endpoint, check.method)
	}
}

func TestMergeGatewayConfigNilPatchReturnsExistingConfig(t *testing.T) {
	t.Parallel()

	existing := &GatewayConfig{
		ResponsesToolPolicy:    ResponsesToolPolicyCompatibility,
		DisabledResponsesTools: []string{ResponsesHostedToolImageGeneration},
	}

	if got := mergeGatewayConfig(existing, nil); got != existing {
		t.Fatalf("nil patch should return existing config pointer, got %#v", got)
	}
	if got := mergeGatewayConfig(nil, nil); got != nil {
		t.Fatalf("nil existing and nil patch = %#v, want nil", got)
	}
}
