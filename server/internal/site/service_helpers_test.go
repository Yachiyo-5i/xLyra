package site

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestNewServiceInitializesDependenciesAndTimeZone(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	service := NewServiceWithTimeZone(nil, siteTestMasterKey, timeZone)
	if service == nil {
		t.Fatal("service should be initialized")
	}
	if service.db != nil {
		t.Fatalf("db = %#v, want nil", service.db)
	}
	if service.timeZone.Name != "UTC" || service.timeZone.Location == nil {
		t.Fatalf("timezone = %#v, want loaded UTC timezone", service.timeZone)
	}
	if service.httpClients == nil || service.modelCaps == nil || service.oauth == nil {
		t.Fatalf("expected collaborators to be initialized: %#v", service)
	}
	if modules := service.adapters.Modules(); len(modules) == 0 {
		t.Fatal("adapter registry should include default modules")
	}
	if _, ok := service.adapters.ModuleForSiteType("openai"); !ok {
		t.Fatal("adapter registry should resolve openai module")
	}

	fallback := NewServiceWithTimeZone(nil, siteTestMasterKey, config.TimeZone{})
	if fallback == nil || fallback.timeZone.Location == nil {
		t.Fatalf("fallback service timezone = %#v, want resolved location", fallback)
	}

	defaultService := NewService(nil, siteTestMasterKey)
	if defaultService == nil || defaultService.timeZone.Location == nil {
		t.Fatalf("default service timezone = %#v, want resolved location", defaultService)
	}
}

func TestServiceCapabilitiesExposeRegisteredAdapterCapabilities(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	codexCapabilities := service.Capabilities("codex")
	if !containsCapability(codexCapabilities, string(adapter.CapabilityListModels)) || !containsCapability(codexCapabilities, string(adapter.CapabilityFetchUserSummary)) {
		t.Fatalf("codex capabilities missing expected gateway/OAuth sync features: %#v", codexCapabilities)
	}

	newAPICapabilities := service.Capabilities("newapi")
	if !containsCapability(newAPICapabilities, string(adapter.CapabilityDetect)) || !containsCapability(newAPICapabilities, string(adapter.CapabilityCheckin)) {
		t.Fatalf("newapi capabilities missing detect/checkin features: %#v", newAPICapabilities)
	}

	if got := service.Capabilities("unknown-site-type"); len(got) != 0 {
		t.Fatalf("unknown site type capabilities = %#v, want empty", got)
	}
}

func TestToAdapterSiteCarriesIdentityMetaAndHTTPClient(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	siteID := uuid.New()
	adapterSite := service.toAdapterSite(context.Background(), store.Site{
		ID:       siteID,
		Name:     "Primary OpenAI",
		SiteType: "openai",
		BaseURL:  "https://api.openai.example.com",
		Meta:     store.JSON(`{"proxy_id":"proxy-a","request_headers":[{"key":"X-Team","value":"core"}]}`),
	})

	if adapterSite.ID != siteID.String() || adapterSite.Name != "Primary OpenAI" || adapterSite.SiteType != "openai" || adapterSite.BaseURL != "https://api.openai.example.com" {
		t.Fatalf("unexpected adapter site identity: %#v", adapterSite)
	}
	if adapterSite.Client == nil {
		t.Fatal("adapter site should include an HTTP client")
	}
	if adapterSite.Meta["proxy_id"] != "proxy-a" {
		t.Fatalf("adapter site meta was not decoded: %#v", adapterSite.Meta)
	}
	if _, ok := adapterSite.Meta["request_headers"].([]any); !ok {
		t.Fatalf("adapter site request headers meta missing: %#v", adapterSite.Meta)
	}
}

func TestSiteTypeAndCredentialTypeHelpersNormalizeDefaults(t *testing.T) {
	t.Parallel()

	if got := normalizeSiteType("  "); got != "openai" {
		t.Fatalf("empty site type = %q, want openai", got)
	}
	if got := normalizeSiteType("  newapi  "); got != "newapi" {
		t.Fatalf("trimmed site type = %q, want newapi", got)
	}

	for _, tc := range []struct {
		siteType string
		want     string
	}{
		{siteType: "newapi", want: "system_token"},
		{siteType: "xlyra", want: "xlyra"},
		{siteType: "codex", want: "oauth"},
		{siteType: " antigravity ", want: "oauth"},
		{siteType: "openai", want: "api_key"},
	} {
		if got := CredentialTypeForSiteType(tc.siteType); got != tc.want {
			t.Fatalf("credential type for %q = %q, want %q", tc.siteType, got, tc.want)
		}
	}

	if got := normalizeCredentialType("  "); got != defaultCredentialType {
		t.Fatalf("empty credential type = %q, want %q", got, defaultCredentialType)
	}
	if got := normalizeCredentialType(" api_key:abc "); got != "api_key:abc" {
		t.Fatalf("trimmed credential type = %q, want api_key:abc", got)
	}
	for _, siteType := range []string{"openai", "anthropic", "google_gemini", "deepseek"} {
		if !SupportsMultipleAPIKeys(siteType) {
			t.Fatalf("site type %q should support multiple api keys", siteType)
		}
	}
	for _, siteType := range []string{"newapi", "xlyra", "codex", "antigravity", "claude_code", "grok"} {
		if SupportsMultipleAPIKeys(siteType) {
			t.Fatalf("site type %q should not support multiple api keys", siteType)
		}
	}
	if !SupportsAPIKeyCostMultiplier(" OpenAI ") {
		t.Fatal("openai should support api key cost multiplier")
	}
	for _, siteType := range []string{"anthropic", "google_gemini", "deepseek", "newapi", "grok"} {
		if SupportsAPIKeyCostMultiplier(siteType) {
			t.Fatalf("site type %q should not support api key cost multiplier", siteType)
		}
	}
}

func TestShouldDeleteXLyraAccessTokenOnlyForDefaultAPIKeyReplacement(t *testing.T) {
	t.Parallel()

	if !shouldDeleteXLyraAccessToken(" xlyra ", "xlyra", []CredentialInput{{Type: defaultCredentialType}}) {
		t.Fatal("expected plain api_key replacement to delete xlyra access token")
	}
	if shouldDeleteXLyraAccessToken("xlyra", "xlyra", []CredentialInput{{Type: defaultCredentialType + ":scoped"}}) {
		t.Fatal("expected scoped api_key replacement to preserve xlyra access token")
	}
	if shouldDeleteXLyraAccessToken("xlyra", "xlyra", []CredentialInput{{Type: xlyraAccessTokenCredential}}) {
		t.Fatal("expected xlyra access token credential update to preserve existing access token cleanup path")
	}
	if shouldDeleteXLyraAccessToken("openai", "xlyra", []CredentialInput{{Type: defaultCredentialType}}) {
		t.Fatal("expected non-xlyra existing site to skip xlyra access token deletion")
	}
	if shouldDeleteXLyraAccessToken("xlyra", "openai", []CredentialInput{{Type: defaultCredentialType}}) {
		t.Fatal("expected non-xlyra next site to skip xlyra access token deletion")
	}
}

func TestShouldDeleteAPIKeyCredentialsForReplacementVariants(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name             string
		existingSiteType string
		nextSiteType     string
		credentials      []CredentialInput
		want             bool
	}{
		{
			name:             "xlyra access token update preserves api keys",
			existingSiteType: "xlyra",
			nextSiteType:     " xlyra ",
			credentials:      []CredentialInput{{Type: xlyraAccessTokenCredential}},
			want:             false,
		},
		{
			name:             "scoped api key update preserves api keys",
			existingSiteType: "openai",
			nextSiteType:     "openai",
			credentials:      []CredentialInput{{Type: defaultCredentialType + ":secondary"}},
			want:             false,
		},
		{
			name:             "non-api-key same-type update preserves api keys",
			existingSiteType: "newapi",
			nextSiteType:     "newapi",
			credentials:      []CredentialInput{{Type: newAPIAccessTokenCredential}},
			want:             false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldDeleteAPIKeyCredentials(tc.existingSiteType, tc.nextSiteType, tc.credentials)
			if got != tc.want {
				t.Fatalf("shouldDeleteAPIKeyCredentials() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestErrorRoundTripperReturnsConfiguredErrorWithoutNetwork(t *testing.T) {
	t.Parallel()

	want := errors.New("proxy unavailable")
	resp, err := (errorRoundTripper{err: want}).RoundTrip(httptest.NewRequest(http.MethodGet, "https://example.invalid", nil))
	if !errors.Is(err, want) {
		t.Fatalf("RoundTrip error = %v, want %v", err, want)
	}
	if resp != nil {
		t.Fatalf("RoundTrip response = %#v, want nil", resp)
	}
}

func TestNormalizeRoutingPriorityBoundsAndPrecision(t *testing.T) {
	t.Parallel()

	got, err := normalizeRoutingPriorityPointer(nil)
	if err != nil || got != 1.0 {
		t.Fatalf("nil routing priority = %v, %v; want 1.0 nil", got, err)
	}
	got, err = normalizeRoutingPriorityWithFallback(nil, 2.5)
	if err != nil || got != 2.5 {
		t.Fatalf("fallback routing priority = %v, %v; want 2.5 nil", got, err)
	}
	got, err = normalizeRoutingPriority(3.499999999999)
	if err != nil || got != 3.5 {
		t.Fatalf("rounded routing priority = %v, %v; want 3.5 nil", got, err)
	}

	for _, value := range []float64{0.9, 5.1, 1.25} {
		if _, err := normalizeRoutingPriority(value); err == nil {
			t.Fatalf("routing priority %v should be rejected", value)
		}
	}
}

func TestNormalizeRoutingPriorityHandlesNonFiniteValues(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{math.Inf(1), math.Inf(-1)} {
		if _, err := normalizeRoutingPriorityWithFallback(&value, 2.5); err == nil {
			t.Fatalf("routing priority %v should be rejected", value)
		}
	}

	nan := math.NaN()
	if _, err := normalizeRoutingPriorityWithFallback(&nan, 2.5); err == nil {
		t.Fatal("NaN routing priority should be rejected")
	}
}

func TestHTTPClientForSiteUsesGatewayTimeoutMeta(t *testing.T) {
	t.Parallel()

	requestTimeout := 1500
	raw, err := MergeSiteGatewayConfig(nil, &GatewayConfig{RequestTimeoutMS: &requestTimeout})
	if err != nil {
		t.Fatalf("merge gateway config: %v", err)
	}
	service := siteServiceWithoutStore()

	client, err := service.httpClientForSite(context.Background(), store.Site{Meta: raw}, false)
	if err != nil {
		t.Fatalf("http client for site: %v", err)
	}
	if client == nil {
		t.Fatal("expected http client")
	}
	if client.Timeout != 1500*time.Millisecond {
		t.Fatalf("client timeout = %v, want 1500ms", client.Timeout)
	}

	streamClient, err := service.httpClientForSite(context.Background(), store.Site{Meta: raw}, true)
	if err != nil {
		t.Fatalf("streaming http client for site: %v", err)
	}
	if streamClient == nil {
		t.Fatal("expected streaming http client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("streaming client timeout = %v, want 0", streamClient.Timeout)
	}
}

func TestHTTPClientProfileFromGatewayConfigUsesTimeoutsAndProxy(t *testing.T) {
	t.Parallel()

	requestTimeout := 1200
	connectTimeout := 700
	responseHeaderTimeout := 1300
	maxIdle := 11
	maxIdlePerHost := 7
	maxConnsPerHost := 5
	idleTimeout := 9000

	profile := httpclientProfileFromGatewayConfig(&GatewayConfig{
		RequestTimeoutMS:        &requestTimeout,
		ConnectTimeoutMS:        &connectTimeout,
		ResponseHeaderTimeoutMS: &responseHeaderTimeout,
		MaxIdleConns:            &maxIdle,
		MaxIdleConnsPerHost:     &maxIdlePerHost,
		MaxConnsPerHost:         &maxConnsPerHost,
		IdleConnTimeoutMS:       &idleTimeout,
	}, "proxy-main")

	if profile.ProxyID != "proxy-main" {
		t.Fatalf("proxy id = %q, want proxy-main", profile.ProxyID)
	}
	if profile.RequestTimeout != 1200*time.Millisecond || profile.ConnectTimeout != 700*time.Millisecond || profile.ResponseHeaderTimeout != 1300*time.Millisecond {
		t.Fatalf("unexpected timeout profile: %#v", profile)
	}
	if profile.MaxIdleConns != maxIdle || profile.MaxIdleConnsPerHost != maxIdlePerHost || profile.MaxConnsPerHost != maxConnsPerHost {
		t.Fatalf("unexpected connection pool profile: %#v", profile)
	}
	if profile.IdleConnTimeout != 9*time.Second {
		t.Fatalf("idle conn timeout = %v, want 9s", profile.IdleConnTimeout)
	}
}

func TestSiteMetaHelpersReadProxyAndMergeHeaders(t *testing.T) {
	t.Parallel()

	if got := proxyIDFromMeta(store.JSON(`{"proxy_id":" proxy-a "}`)); got != "proxy-a" {
		t.Fatalf("proxy id = %q, want proxy-a", got)
	}
	if got := proxyIDFromMeta(store.JSON(`{"proxy_id":123}`)); got != "" {
		t.Fatalf("non-string proxy id = %q, want empty", got)
	}

	merged := mergeRequestHeadersMeta(store.JSON(`{"proxy_id":"proxy-a"}`), map[string]string{
		"X-Trace": "enabled",
	})
	root := siteMustJSONMap(t, merged)
	if root["proxy_id"] != "proxy-a" {
		t.Fatalf("expected existing proxy id to be preserved, got %#v", root)
	}
	headers, ok := root["request_headers"].([]any)
	if !ok || len(headers) != 1 {
		t.Fatalf("expected one request header entry, got %#v", root["request_headers"])
	}
	header, _ := headers[0].(map[string]any)
	if header["key"] != "X-Trace" || header["value"] != "enabled" {
		t.Fatalf("unexpected request header payload: %#v", header)
	}

	existing := store.JSON(`{"keep":true}`)
	if got := mergeRequestHeadersMeta(existing, nil); string(got) != string(existing) {
		t.Fatalf("empty headers should preserve existing meta, got %s", string(got))
	}

	merged = mergeRequestHeadersMeta(store.JSON(`{`), map[string]string{"X-Trace": "enabled"})
	root = siteMustJSONMap(t, merged)
	headers, ok = root["request_headers"].([]any)
	if !ok || len(headers) != 1 {
		t.Fatalf("expected one request header entry over invalid meta, got %#v", root["request_headers"])
	}
}

func TestCredentialMetaHelpersHideMissingRawKeys(t *testing.T) {
	t.Parallel()

	meta := map[string]any{
		"raw_key_missing":     true,
		"upstream_masked_key": " sk-upstream ",
	}
	if got := secretFromCredentialMeta("secret", meta); got != "" {
		t.Fatalf("secret with raw_key_missing = %q, want empty", got)
	}
	if got := maskedSecretFromCredentialMeta("local-mask", meta); got != "sk-upstream" {
		t.Fatalf("masked secret = %q, want upstream masked key", got)
	}
	if got := secretFromCredentialMeta("secret", map[string]any{}); got != "secret" {
		t.Fatalf("secret without raw_key_missing = %q, want secret", got)
	}
	if got := maskedSecretFromCredentialMeta("local-mask", map[string]any{"raw_key_missing": true}); got != "local-mask" {
		t.Fatalf("masked secret without upstream mask = %q, want local-mask", got)
	}
}

func TestUpdateCredentialMetaRejectsUnmarshalableMetadataBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	_, err := updateCredentialMeta(context.Background(), store.SiteCredentialRepository{}, uuid.New(), map[string]any{
		"bad": make(chan int),
	})
	if err == nil || !strings.Contains(err.Error(), "marshal credential meta") {
		t.Fatalf("updateCredentialMeta error = %v, want marshal credential meta", err)
	}
}

func TestCredentialInputsAndTokenScopedDetection(t *testing.T) {
	t.Parallel()

	primary := &CredentialInput{Type: "api_key", Secret: "primary"}
	second := CredentialInput{Type: "api_key:secondary", Secret: "secondary"}
	got := credentialInputs(primary, []CredentialInput{second})
	if len(got) != 2 || got[0].Secret != "primary" || got[1].Secret != "secondary" {
		t.Fatalf("credential inputs = %#v", got)
	}
	if len(credentialInputs(nil, []CredentialInput{second})) != 1 {
		t.Fatal("nil primary should leave only explicit credentials")
	}

	if !hasTokenScopedCredentials([]store.SiteCredential{{CredentialType: "api_key:one"}}) {
		t.Fatal("expected api_key:<id> credential to be token-scoped")
	}
	if hasTokenScopedCredentials([]store.SiteCredential{{CredentialType: "api_key"}}) {
		t.Fatal("expected default api_key credential not to be token-scoped")
	}

	scoped := scopedAPIKeyCredentialType()
	if len(scoped) <= len(defaultCredentialType)+1 || scoped[:len(defaultCredentialType)+1] != defaultCredentialType+":" {
		t.Fatalf("scoped credential type = %q, want api_key:<uuid>", scoped)
	}
	if _, err := uuid.Parse(scoped[len(defaultCredentialType)+1:]); err != nil {
		t.Fatalf("scoped credential suffix should be a uuid: %v", err)
	}
}

func TestPrepareCreateCredentialInputsScopesAndValidatesAPIKeys(t *testing.T) {
	t.Parallel()

	primaryName := " Primary "
	secondaryName := "Secondary"
	primaryPriority := 5.0
	secondaryPriority := 2.5
	multiplier := 1.2
	prepared, err := prepareCreateCredentialInputs("openai", []CredentialInput{
		{Type: "api_key", Secret: "first", DisplayName: &primaryName, RoutingPriority: &primaryPriority, UpstreamCostMultiplier: &multiplier},
		{Type: "api_key", Secret: "second", DisplayName: &secondaryName, RoutingPriority: &secondaryPriority, UpstreamCostMultiplier: &multiplier},
	})
	if err != nil {
		t.Fatalf("prepare create credential inputs: %v", err)
	}
	if len(prepared) != 2 || prepared[0].Type == prepared[1].Type {
		t.Fatalf("prepared credential types = %q, %q, want distinct scoped types", prepared[0].Type, prepared[1].Type)
	}
	for _, item := range prepared {
		if !strings.HasPrefix(item.Type, defaultCredentialType+":") {
			t.Fatalf("prepared credential type = %q, want scoped api key", item.Type)
		}
	}
	if prepared[0].DisplayName == nil || *prepared[0].DisplayName != "Primary" {
		t.Fatalf("prepared display name = %#v, want trimmed name", prepared[0].DisplayName)
	}

	if _, err := prepareCreateCredentialInputs("anthropic", []CredentialInput{
		{Type: "api_key", Secret: "first"},
		{Type: "api_key", Secret: "second"},
	}); err != nil {
		t.Fatalf("anthropic multi-key preparation: %v", err)
	}
	if _, err := prepareCreateCredentialInputs("anthropic", []CredentialInput{
		{Type: "api_key", Secret: "first", UpstreamCostMultiplier: &multiplier},
	}); err == nil || !strings.Contains(err.Error(), "does not support api key cost multiplier") {
		t.Fatalf("anthropic multiplier error = %v", err)
	}
}

func containsCapability(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestSiteModelCollectionHelpers(t *testing.T) {
	t.Parallel()

	firstID := uuid.New()
	secondID := uuid.New()
	visible := visibleSiteModels([]store.SiteModel{
		{ID: firstID, Status: "active"},
		{ID: uuid.New(), Status: "unavailable"},
		{ID: secondID, Status: "inactive"},
	})
	if len(visible) != 2 || visible[0].ID != firstID || visible[1].ID != secondID {
		t.Fatalf("visible models = %#v, want active and inactive only", visible)
	}

	gotIDs := uniqueUUIDs([]uuid.UUID{uuid.Nil, firstID, secondID, firstID})
	if len(gotIDs) != 2 || gotIDs[0] != firstID || gotIDs[1] != secondID {
		t.Fatalf("unique uuids = %#v, want first and second", gotIDs)
	}
}

func TestSiteAPIKeyModelMatchesSiteModelByIDOrName(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	model := store.SiteModel{ID: modelID, UpstreamName: " gpt-4o "}
	if !siteAPIKeyModelMatchesSiteModel(store.SiteAPIKeyModel{
		SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
	}, model) {
		t.Fatal("expected site api key model to match by site model id")
	}
	if !siteAPIKeyModelMatchesSiteModel(store.SiteAPIKeyModel{UpstreamModelName: "gpt-4o"}, model) {
		t.Fatal("expected site api key model to match by trimmed upstream model name")
	}
	if siteAPIKeyModelMatchesSiteModel(store.SiteAPIKeyModel{UpstreamModelName: "gpt-4o-mini"}, model) {
		t.Fatal("expected different upstream model name not to match")
	}
}

func TestAvailableSiteModelPricingsFiltersUnavailableRows(t *testing.T) {
	t.Parallel()

	first := store.SiteModelPricing{ID: uuid.New(), Available: true}
	second := store.SiteModelPricing{ID: uuid.New(), Available: false}
	third := store.SiteModelPricing{
		ID:         uuid.New(),
		Available:  true,
		InputValue: sql.NullFloat64{Float64: 0.2, Valid: true},
	}

	got := availableSiteModelPricings([]store.SiteModelPricing{first, second, third})
	if len(got) != 2 || got[0].ID != first.ID || got[1].ID != third.ID {
		t.Fatalf("available pricings = %#v, want first and third", got)
	}
}

func TestCoalesceModelCapabilitiesMarksFirstUpstreamSourceOnlyForNewModels(t *testing.T) {
	t.Parallel()

	merged := coalesceModelCapabilities(adapter.Model{}, adapter.Model{UpstreamName: "gpt-test"}, "key-a")
	if merged.UpstreamName != "gpt-test" {
		t.Fatalf("merged upstream name = %q, want gpt-test", merged.UpstreamName)
	}
	if merged.Capabilities["source"] != "upstream" || merged.Capabilities["first_api_key"] != "key-a" {
		t.Fatalf("new model capabilities = %#v", merged.Capabilities)
	}

	existing := adapter.Model{
		UpstreamName: "gpt-existing",
		Capabilities: map[string]any{"source": "manual"},
	}
	merged = coalesceModelCapabilities(existing, adapter.Model{UpstreamName: "gpt-next"}, "key-b")
	if merged.UpstreamName != "gpt-existing" || merged.Capabilities["source"] != "manual" {
		t.Fatalf("existing model should be preserved, got %#v", merged)
	}
	if _, ok := merged.Capabilities["first_api_key"]; ok {
		t.Fatalf("existing model should not gain first_api_key, got %#v", merged.Capabilities)
	}
}

func TestSmallJSONAndSetHelpers(t *testing.T) {
	t.Parallel()

	if got := stringFromMap(nil, "name"); got != "" {
		t.Fatalf("nil map string = %q, want empty", got)
	}
	if got := stringFromMap(map[string]any{"name": 10}, "name"); got != "" {
		t.Fatalf("non-string map value = %q, want empty", got)
	}
	if got := stringFromMap(map[string]any{"name": " value "}, "name"); got != "value" {
		t.Fatalf("trimmed map string = %q, want value", got)
	}

	value := "  stored value  "
	merged := mergeStringMeta(store.JSON(`{"keep":true}`), "name", &value)
	root := siteMustJSONMap(t, merged)
	if root["name"] != "stored value" || root["keep"] != true {
		t.Fatalf("unexpected merged string meta: %#v", root)
	}

	blank := " "
	merged = mergeStringMeta(merged, "name", &blank)
	root = siteMustJSONMap(t, merged)
	if _, ok := root["name"]; ok {
		t.Fatalf("blank string should remove key, got %#v", root)
	}

	set := stringSetFromAny([]any{" b ", "", "a", 10})
	sorted := sortedStringSet(set)
	if len(sorted) != 2 || sorted[0] != "a" || sorted[1] != "b" {
		t.Fatalf("sorted string set = %#v, want [a b]", sorted)
	}
	set = stringSetFromAny([]string{" c ", "", "a"})
	sorted = sortedStringSet(set)
	if len(sorted) != 2 || sorted[0] != "a" || sorted[1] != "c" {
		t.Fatalf("sorted string set from []string = %#v, want [a c]", sorted)
	}
	if got := appendUnique([]string{"a"}, " a "); len(got) != 1 || got[0] != "a" {
		t.Fatalf("append existing value = %#v, want [a]", got)
	}
	if got := appendUnique([]string{"a"}, "b"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("append new value = %#v, want [a b]", got)
	}
}

func TestCapabilityProviderForSiteMapsSupportedAdapters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		siteType string
		want     string
	}{
		{siteType: "openai", want: "openai"},
		{siteType: "deepseek", want: "openai"},
		{siteType: "minimax", want: "openai"},
		{siteType: "newapi", want: "openai"},
		{siteType: "xiaomi_mimo", want: "xiaomi_mimo"},
		{siteType: "moonshot", want: "openai"},
		{siteType: "kimi_code", want: "openai"},
		{siteType: "zhipu", want: "zhipuai"},
		{siteType: "glm_code", want: "zhipuai"},
		{siteType: "codex", want: "openai"},
		{siteType: "antigravity", want: "google"},
		{siteType: "anthropic", want: "anthropic"},
		{siteType: " unknown ", want: ""},
	} {
		if got := capabilityProviderForSite(tc.siteType); got != tc.want {
			t.Fatalf("provider for %q = %q, want %q", tc.siteType, got, tc.want)
		}
	}
}
