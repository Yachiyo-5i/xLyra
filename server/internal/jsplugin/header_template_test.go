package jsplugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSiteProviderRequestManifestAccepts(t *testing.T) {
	t.Parallel()
	source := meta("acme.example", KindSiteProvider)
	manifest := siteProviderManifest(source)
	manifest.SiteProvider.Request = SiteProviderRequest{
		Headers: HeaderMap(map[string]string{
			"Authorization": "Bearer {{cred.api_key}}",
			"X-Client":      "xlyra",
			"X-Base":        "{{site.baseURL}}",
			"X-Repeat":      "{{cred.api_key}}-{{cred.api_key}}",
		}),
		StripDownstreamHeaders: true,
	}
	if err := validateManifest(manifest, source); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Manifest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	got := decoded.SiteProvider.Request
	if !got.StripDownstreamHeaders || got.Headers["Authorization"].Value != "Bearer {{cred.api_key}}" || got.Headers["X-Client"].Value != "xlyra" || got.Headers["X-Base"].Value != "{{site.baseURL}}" {
		t.Fatalf("request = %#v", got)
	}
	if decoded.SiteProvider.Models.Auth != "header:X-Api-Key" {
		t.Fatalf("models.auth = %q", decoded.SiteProvider.Models.Auth)
	}

	many := manifest
	headers := make(map[string]HeaderSpec, maxSiteProviderRequestHeaders)
	for i := 0; i < maxSiteProviderRequestHeaders; i++ {
		headers[sprintfHeader(i)] = HeaderSpec{Value: "x"}
	}
	many.SiteProvider.Request = SiteProviderRequest{Headers: headers}
	if err := validateManifest(many, source); err != nil {
		t.Fatalf("16 headers: %v", err)
	}
}

func TestSiteProviderRequestManifestRejects(t *testing.T) {
	t.Parallel()
	source := meta("acme.example", KindSiteProvider)
	forbidden := []string{
		"Host", "Content-Length", "Transfer-Encoding", "Connection", "Upgrade", "TE", "Trailer",
		"Proxy-Authorization", "Proxy-Connection", "Cookie", "Set-Cookie", "cookie", "CONTENT-TYPE", "Accept",
		"X-Forwarded-For", "X-Forwarded-Host", "x-forwarded-proto",
	}
	for _, name := range forbidden {
		t.Run("forbidden "+name, func(t *testing.T) {
			err := validateRequestManifest(source, map[string]string{name: "x"}, false)
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	cases := []struct {
		name    string
		headers map[string]string
		strip   bool
		want    string
	}{
		{name: "space inside", headers: map[string]string{"X-Client": "{{ cred.api_key }}"}, want: "spaces"},
		{name: "space after field", headers: map[string]string{"X-Client": "{{cred.api_key }}"}, want: "spaces"},
		{name: "nested", headers: map[string]string{"X-Client": "{{cred.{{cred.api_key}}}}"}, want: "nested"},
		{name: "unclosed", headers: map[string]string{"X-Client": "Bearer {{cred.api_key"}, want: "unclosed"},
		{name: "unknown prefix", headers: map[string]string{"X-Client": "{{secret.x}}"}, want: "unknown placeholder"},
		{name: "unknown field", headers: map[string]string{"X-Client": "{{cred.domain}}"}, want: `unknown field "domain"`},
		{name: "wrong site key", headers: map[string]string{"X-Client": "{{site.baseurl}}"}, want: "unknown placeholder"},
		{name: "hyphen field", headers: map[string]string{"X-Client": "{{cred.api-key}}"}, want: "unknown placeholder"},
		{name: "crlf literal", headers: map[string]string{"X-Client": "a\r\nb"}, want: "control character"},
		{name: "lf literal", headers: map[string]string{"X-Client": "a\nb"}, want: "control character"},
		{name: "nul literal", headers: map[string]string{"X-Client": "a\x00b"}, want: "control character"},
		{name: "bad token", headers: map[string]string{"Bad Header": "x"}, want: "not a valid HTTP token"},
		{name: "duplicate case", headers: map[string]string{"X-Client": "a", "x-client": "b"}, want: "duplicate header"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateRequestManifest(source, tc.headers, tc.strip)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	t.Run("over 16", func(t *testing.T) {
		headers := make(map[string]string, maxSiteProviderRequestHeaders+1)
		for i := 0; i < maxSiteProviderRequestHeaders+1; i++ {
			headers[sprintfHeader(i)] = "x"
		}
		err := validateRequestManifest(source, headers, false)
		if err == nil || !strings.Contains(err.Error(), "at most 16") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("other kinds", func(t *testing.T) {
		for _, kind := range []string{KindQuotaProbe, KindProtocol} {
			manifest := siteProviderManifest(source)
			manifest.Kind = kind
			if kind == KindQuotaProbe {
				manifest.QuotaProbe.BaseURLMode = "as_is"
			}
			manifest.SiteProvider.Request = SiteProviderRequest{Headers: HeaderMap(map[string]string{"X-Client": "xlyra"})}
			err := validateManifest(manifest, source)
			if err == nil || !strings.Contains(err.Error(), "request is not allowed for "+kind) {
				t.Fatalf("%s err = %v", kind, err)
			}
			manifest.SiteProvider.Request = SiteProviderRequest{StripDownstreamHeaders: true}
			err = validateManifest(manifest, source)
			if err == nil || !strings.Contains(err.Error(), "request is not allowed for "+kind) {
				t.Fatalf("%s strip err = %v", kind, err)
			}
		}
	})
}

func TestValidateHeaderTemplateAllowedFields(t *testing.T) {
	t.Parallel()
	apiKey := map[string]struct{}{"api_key": {}}
	if err := ValidateHeaderTemplate("Bearer {{cred.api_key}} @ {{site.baseURL}}", apiKey, nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHeaderTemplate("{{cred.domain}}", apiKey, nil); err == nil || !strings.Contains(err.Error(), `unknown field "domain"`) {
		t.Fatalf("api_key model err = %v", err)
	}
	if err := ValidateHeaderTemplate("{{cred.api_key}}", map[string]struct{}{}, nil); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("empty set err = %v", err)
	}
	pool := map[string]struct{}{"access_token": {}, "domain": {}}
	if err := ValidateHeaderTemplate("{{cred.access_token}} {{cred.domain}}", pool, nil); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHeaderTemplate("{{cred.api_key}}", pool, nil); err == nil || !strings.Contains(err.Error(), `unknown field "api_key"`) {
		t.Fatalf("pool set err = %v", err)
	}
	section := SiteProviderRequest{Headers: HeaderMap(map[string]string{"X-Domain": "{{cred.domain}}"})}
	if err := ValidateSiteProviderRequest(section, apiKey, nil); err == nil || !strings.Contains(err.Error(), `unknown field "domain"`) {
		t.Fatalf("request err = %v", err)
	}
	if err := ValidateSiteProviderRequest(section, pool, nil); err != nil {
		t.Fatal(err)
	}
}

func TestHeaderSpecConditionsAndRegionAttrs(t *testing.T) {
	t.Parallel()
	pool := map[string]struct{}{"domain": {}, "access_token": {}}
	regions := map[string]struct{}{"origin": {}, "referer": {}}
	raw := []byte(`{
		"X-Domain": {"value": "{{cred.domain}}", "when": "cred.domain"},
		"X-No-Department-Info": {"value": "1", "unless": "cred.domain"},
		"Origin": "{{region.origin}}"
	}`)
	var headers map[string]HeaderSpec
	if err := json.Unmarshal(raw, &headers); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSiteProviderRequest(SiteProviderRequest{Headers: headers}, pool, regions); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(headers["Origin"])
	if err != nil || string(encoded) != `"{{region.origin}}"` {
		t.Fatalf("unconditional header = %s err=%v", encoded, err)
	}
	both := HeaderSpec{Value: "1", When: "cred.domain", Unless: "cred.domain"}
	if err := validateHeaderSpec(both, pool, regions); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("both conditions = %v", err)
	}
	if err := ValidateHeaderTemplate("{{region.theme}}", pool, regions); err == nil {
		t.Fatal("theme key was accepted")
	}
	if err := ValidateHeaderTemplate("{{region.origin}}", pool, nil); err == nil || !strings.Contains(err.Error(), "unknown region attribute") {
		t.Fatalf("missing region attr = %v", err)
	}
}

func validateRequestManifest(source string, headers map[string]string, strip bool) error {
	manifest := siteProviderManifest(source)
	manifest.SiteProvider.Request = SiteProviderRequest{Headers: HeaderMap(headers), StripDownstreamHeaders: strip}
	return validateManifest(manifest, source)
}

func sprintfHeader(i int) string {
	return "X-H" + string(rune('A'+i/26)) + string(rune('A'+i%26))
}
