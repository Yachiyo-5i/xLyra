package jsplugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestAccountPoolManifestAccepts(t *testing.T) {
	accountSource := poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens")
	pool := poolManifest(accountSource, poolSection())
	if err := validateManifest(pool, accountSource); err != nil {
		t.Fatal(err)
	}
	spec, err := NewAccountPoolSpec(pool.SiteProvider)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Credential.AccessTokenField != "access_token" || spec.Selection != "ordered" || spec.QuotaAggregate != "sum" {
		t.Fatalf("defaults = access %q selection %q quota %q", spec.Credential.AccessTokenField, spec.Selection, spec.QuotaAggregate)
	}
	if spec.Refresh.LeadSeconds == nil || *spec.Refresh.LeadSeconds != 300 {
		t.Fatalf("lead = %v", spec.Refresh.LeadSeconds)
	}
	if spec.DefaultBaseURL() != "https://api.example.cn" {
		t.Fatalf("base = %q", spec.DefaultBaseURL())
	}
	pool.SiteProvider.Regions[0].BaseURL = "https://evil.example"
	pool.SiteProvider.Login.SecretPaths["data.accessToken"] = "mutated"
	if spec.DefaultBaseURL() != "https://api.example.cn" || spec.Login.SecretPaths["data.accessToken"] != "access_token" {
		t.Fatalf("spec aliased the manifest: %#v", spec)
	}

	encoded, err := json.Marshal(poolManifest(accountSource, poolSection()))
	if err != nil {
		t.Fatal(err)
	}
	var decoded Manifest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := validateManifest(decoded, accountSource); err != nil {
		t.Fatal(err)
	}
	if decoded.SiteProvider.Login == nil || decoded.SiteProvider.Login.Kind != "custom_poll" || !decoded.SiteProvider.Credential.Identity.Equal("claims.sub") {
		t.Fatalf("round trip = %#v", decoded.SiteProvider)
	}

	grokSource := poolSource("acme.example", "summarizeAccount")
	grok := poolManifest(grokSource, grokSection())
	if err := validateManifest(grok, grokSource); err != nil {
		t.Fatal(err)
	}
	plugin, err := NewPlugin(grok, grokSource, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !plugin.HasHook("summarizeAccount") || plugin.HasHook("startLogin") || plugin.HasHook("refreshTokens") {
		t.Fatalf("hooks summarize=%v start=%v refresh=%v", plugin.HasHook("summarizeAccount"), plugin.HasHook("startLogin"), plugin.HasHook("refreshTokens"))
	}
	accountPoolPlugin, err := NewPlugin(poolManifest(accountSource, poolSection()), accountSource, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"summarizeAccount", "startLogin", "pollLogin", "refreshTokens"} {
		if !accountPoolPlugin.HasHook(name) {
			t.Fatalf("missing hook %s", name)
		}
	}

	for _, lead := range []int{30, 3600} {
		section := poolSection()
		section.Refresh.LeadSeconds = intPointer(lead)
		if err := validateManifest(poolManifest(accountSource, section), accountSource); err != nil {
			t.Fatalf("lead %d: %v", lead, err)
		}
	}

	for _, mode := range []string{"sum", "min", "none"} {
		apiKey := siteProviderManifest(meta("acme.example", KindSiteProvider))
		apiKey.SiteProvider.Quota = &SiteProviderQuota{Aggregate: mode}
		apiKey.SiteProvider.Request.UpstreamStreamOnly = true
		if err := validateManifest(apiKey, meta("acme.example", KindSiteProvider)); err != nil {
			t.Fatalf("api_key quota %s: %v", mode, err)
		}
	}
}

func TestAccountPoolManifestRejects(t *testing.T) {
	source := poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens")
	apiKeySource := meta("acme.example", KindSiteProvider)
	cases := []struct {
		name   string
		base   string
		mutate func(*Manifest)
		want   string
	}{
		{name: "duplicate field", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Fields[1].Name = "access_token"
		}, want: "duplicated"},
		{name: "too many fields", base: "pool", mutate: func(m *Manifest) {
			fields := make([]SiteProviderField, 17)
			for i := range fields {
				fields[i] = SiteProviderField{Name: fmt.Sprintf("f%d", i), Secret: true}
			}
			m.SiteProvider.Credential.Fields = fields
			m.SiteProvider.Credential.AccessTokenField = "f0"
			m.SiteProvider.Credential.RefreshTokenField = ""
			m.SiteProvider.Credential.ExpiresAtField = ""
			m.SiteProvider.Import.FieldMap = map[string][]string{}
			for i := range fields {
				m.SiteProvider.Import.FieldMap[fields[i].Name] = []string{fields[i].Name}
			}
			m.SiteProvider.Login.SecretPaths = map[string]string{"data.token": "f0"}
			m.SiteProvider.Refresh.SecretPaths = nil
			m.SiteProvider.UI.RowPills = nil
		}, want: "1 to 16"},
		{name: "field name", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Fields[3].Name = "Domain"
		}, want: "is invalid"},
		{name: "no secret", base: "pool", mutate: func(m *Manifest) {
			for i := range m.SiteProvider.Credential.Fields {
				m.SiteProvider.Credential.Fields[i].Secret = false
			}
		}, want: "secret field"},
		{name: "access token not secret", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Fields[0].Secret = false
		}, want: "accessTokenField"},
		{name: "expires not unix", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Fields[2].Type = "string"
		}, want: `type "unix"`},
		{name: "identity syntax", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Identity = Sources("sub")
		}, want: "identity"},
		{name: "identity secret field", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Identity = Sources("field.access_token")
		}, want: "must not be secret"},
		{name: "regions empty", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Regions = nil
		}, want: "regions must list"},
		{name: "duplicate region", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Regions = append(m.SiteProvider.Regions, m.SiteProvider.Regions[0])
		}, want: "duplicated"},
		{name: "region http", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Regions[0].BaseURL = "http://api.example.cn"
		}, want: "https"},
		{name: "region host", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Regions[0].BaseURL = "https://other.example.com"
		}, want: "not in hosts"},
		{name: "realm case", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Regions[0].Realms = []string{"CN"}
		}, want: "must be lowercase"},
		{name: "domain", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Regions[0].Domains = []string{"not a host"}
		}, want: "domains"},
		{name: "import container", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Import.Containers = []string{"bad/name"}
		}, want: "containers"},
		{name: "default region missing", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.DefaultRegion = ""
		}, want: "defaultRegion is required"},
		{name: "default region unknown", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.DefaultRegion = "us"
		}, want: "not a region id"},
		{name: "login missing", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login = nil
		}, want: "login is required"},
		{name: "login kind", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login.Kind = "password"
		}, want: "login.kind"},
		{name: "custom poll oauth field", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login.DeviceCodeURL = "https://api.example.cn/device"
		}, want: "not allowed when kind is \"custom_poll\""},
		{name: "oauth url", base: "grok", mutate: func(m *Manifest) {
			m.SiteProvider.Login.DeviceCodeURL = ""
		}, want: "deviceCodeURL is required"},
		{name: "oauth client id", base: "grok", mutate: func(m *Manifest) {
			m.SiteProvider.Login.ClientID = ""
		}, want: "clientID is required"},
		{name: "extra header", base: "grok", mutate: func(m *Manifest) {
			m.SiteProvider.Login.ExtraHeaders = map[string]string{"Authorization": "Bearer x"}
		}, want: "not allowed"},
		{name: "login param", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login.Params[0].Source = "hosts"
		}, want: "source"},
		{name: "secret path", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login.SecretPaths = map[string]string{"data.*": "access_token"}
		}, want: "invalid path"},
		{name: "secret path field", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login.SecretPaths = map[string]string{"data.token": "domain", "data.accessToken": "access_token"}
		}, want: "not a secret field"},
		{name: "secret path coverage", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Login.SecretPaths = map[string]string{"data.refreshToken": "refresh_token"}
		}, want: "must map a path to \"access_token\""},
		{name: "refresh kind", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Refresh.Kind = "later"
		}, want: "refresh.kind"},
		{name: "lead seconds", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Refresh.LeadSeconds = intPointer(10)
		}, want: "between 30 and 3600"},
		{name: "oauth refresh token field", base: "grok", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.RefreshTokenField = ""
		}, want: "refreshTokenField is required"},
		{name: "oauth refresh url", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Refresh.Kind = "oauth2_refresh"
			m.SiteProvider.Refresh.TokenURL = ""
		}, want: "refresh.tokenURL is required"},
		{name: "field map missing", base: "pool", mutate: func(m *Manifest) {
			delete(m.SiteProvider.Import.FieldMap, "access_token")
		}, want: "fieldMap must include \"access_token\""},
		{name: "field map alias", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Import.FieldMap["access_token"] = []string{"bad alias"}
		}, want: "is invalid"},
		{name: "selection", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Selection = "random"
		}, want: "selection"},
		{name: "quota", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Quota = &SiteProviderQuota{Aggregate: "avg"}
		}, want: "quota.aggregate"},
		{name: "base url from", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Request.BaseURLFrom = "account"
		}, want: "baseURLFrom"},
		{name: "header field", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Request.Headers["X-Extra"] = HeaderSpec{Value: "{{cred.nope}}"}
		}, want: `unknown field "nope"`},
		{name: "row pill secret", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.UI.RowPills[0].Field = "access_token"
		}, want: "must not be secret"},
		{name: "operation", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.UI.Operations = []string{"export"}
		}, want: "not supported"},
		{name: "import operation", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Import.Enabled = false
		}, want: "requires import.enabled"},
		{name: "ui text key", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.UI.Texts = map[string]LocalizedTextOrString{"toast": {"": "nope"}}
		}, want: "texts["},
		{name: "ui text length", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.UI.Texts = map[string]LocalizedTextOrString{"add": {"": strings.Repeat("a", 401)}}
		}, want: "longer than"},
		{name: "ui pill label", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.UI.RowPills[0].Labels = map[string]LocalizedText{"bad\nkey": {"en": "nope"}}
		}, want: "labels key"},
		{name: "api key regions", base: "api_key", mutate: func(m *Manifest) {
			m.SiteProvider.Regions = []SiteProviderRegion{{ID: "cn", BaseURL: "https://api.example.com"}}
		}, want: "regions is not allowed"},
		{name: "api key fields", base: "api_key", mutate: func(m *Manifest) {
			m.SiteProvider.Credential.Fields = []SiteProviderField{{Name: "access_token", Secret: true}}
		}, want: "credential.fields is not allowed"},
		{name: "api key quota", base: "api_key", mutate: func(m *Manifest) {
			m.SiteProvider.Quota = &SiteProviderQuota{Aggregate: "avg"}
		}, want: "quota.aggregate"},
		{name: "account pool models", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Models.Path = "/v1/models"
		}, want: "models.path"},
		{name: "account pool models auth", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Models.Auth = "bearer"
		}, want: "models.auth"},
		{name: "account pool perAccount and path", base: "pool", mutate: func(m *Manifest) {
			m.SiteProvider.Models.PerAccount = true
			m.SiteProvider.Models.Path = "/v1/models"
		}, want: "models.path"},
		{name: "api key perAccount", base: "api_key", mutate: func(m *Manifest) {
			m.SiteProvider.Models.PerAccount = true
		}, want: "perAccount is not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var manifest Manifest
			var body string
			switch tc.base {
			case "grok":
				body = poolSource("acme.example", "summarizeAccount")
				manifest = poolManifest(body, grokSection())
			case "api_key":
				body = apiKeySource
				manifest = siteProviderManifest(body)
			default:
				body = source
				manifest = poolManifest(body, poolSection())
			}
			tc.mutate(&manifest)
			err := validateManifest(manifest, body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAccountPoolEmptyModelsObject(t *testing.T) {
	source := poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens")
	encoded, err := json.Marshal(poolManifest(source, poolSection()))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	doc["siteProvider"].(map[string]any)["models"] = map[string]any{}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	err = validateManifest(manifest, source)
	if err == nil || !strings.Contains(err.Error(), "perAccount must be true") {
		t.Fatalf("err = %v", err)
	}
}

func TestAccountPoolListModelsRequiresPerAccount(t *testing.T) {
	section := poolSection()
	source := poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens", "listModels")
	_, err := NewPlugin(poolManifest(source, section), source, nil, 1)
	if err == nil || !strings.Contains(err.Error(), "listModels") || !strings.Contains(err.Error(), "perAccount must be true") {
		t.Fatalf("err = %v", err)
	}
}

func TestAccountPoolPerAccountModels(t *testing.T) {
	section := poolSection()
	section.Models.PerAccount = true
	source := poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens", "listModels")
	if err := validateManifest(poolManifest(source, section), source); err != nil {
		t.Fatal(err)
	}
	spec, err := NewAccountPoolSpec(section)
	if err != nil {
		t.Fatal(err)
	}
	if !spec.ModelsPerAccount || !spec.Clone().ModelsPerAccount {
		t.Fatal("perAccount was not copied onto the spec")
	}
	plugin, err := NewPlugin(poolManifest(source, section), source, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !plugin.HasHook("listModels") {
		t.Fatal("listModels was not loaded")
	}
}

func TestAccountPoolExports(t *testing.T) {
	section := poolSection()
	cases := []struct {
		name    string
		section SiteProviderSection
		source  string
		want    string
	}{
		{name: "summarizeAccount", section: section, source: poolSource("acme.example", "startLogin", "pollLogin", "refreshTokens"), want: "summarizeAccount"},
		{name: "startLogin", section: section, source: poolSource("acme.example", "summarizeAccount", "pollLogin", "refreshTokens"), want: "startLogin"},
		{name: "pollLogin", section: section, source: poolSource("acme.example", "summarizeAccount", "startLogin", "refreshTokens"), want: "pollLogin"},
		{name: "refreshTokens", section: section, source: poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin"), want: "refreshTokens"},
		{name: "listModels", section: func() SiteProviderSection {
			next := poolSection()
			next.Models.PerAccount = true
			return next
		}(), source: poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens"), want: "listModels"},
		{name: "not a function", section: section, source: poolSource("acme.example", "startLogin", "pollLogin", "refreshTokens") + "export const summarizeAccount = 1;\n", want: "summarizeAccount"},
		{name: "meta", section: grokSection(), source: "export const meta = { apiVersion: 1, id: \"acme.example\", kind: \"quota_probe\" };\nexport function summarizeAccount() { return {}; }\n", want: "meta.kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := poolManifest(tc.source, tc.section)
			_, err := NewPlugin(manifest, tc.source, nil, 1)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func poolSource(id string, exports ...string) string {
	var b strings.Builder
	b.WriteString(meta(id, KindSiteProvider))
	for _, name := range exports {
		fmt.Fprintf(&b, "export function %s() { return {}; }\n", name)
	}
	return b.String()
}

func poolManifest(source string, section SiteProviderSection) Manifest {
	return Manifest{
		ID: "acme.example", Name: "Example", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: KindSiteProvider,
		SiteProvider: section,
		SHA256:       map[string]string{"plugin.js": hashSource(source)},
	}
}

func poolSection() SiteProviderSection {
	return SiteProviderSection{
		SiteType: "acme.example/pool",
		Display:  SiteProviderDisplay{Name: LocalizedText{"zh": "示例", "en": "Example"}},
		Hosts:    []string{"api.example.cn"},
		Credential: SiteProviderCredential{
			Model: "account_pool",
			Fields: []SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "refresh_token", Secret: true},
				{Name: "expires_at", Type: "unix"},
				{Name: "domain"},
				{Name: "realm"},
			},
			RefreshTokenField: "refresh_token",
			ExpiresAtField:    "expires_at",
			Identity:          Sources("claims.sub"),
		},
		Protocol: SiteProviderProtocol{
			Kind: "openai_chat", Path: "/v2/chat/completions", OfficialBaseURL: "https://api.example.cn",
			DefaultEndpointTypes: []string{"openai"},
		},
		Regions: []SiteProviderRegion{{
			ID: "cn", Label: LocalizedText{"zh": "中国", "en": "China"}, BaseURL: "https://api.example.cn",
			Realms: []string{"cn"}, Domains: []string{"x.cn"},
		}},
		DefaultRegion: "cn",
		Login: &SiteProviderLogin{
			Kind: "custom_poll",
			Params: []SiteProviderLoginParam{{
				Name: "region", Type: "select", Source: "regions",
			}},
			SecretPaths: map[string]string{
				"data.accessToken":  "access_token",
				"data.refreshToken": "refresh_token",
			},
		},
		Refresh: &SiteProviderRefresh{
			Kind: "custom_steps",
			SecretPaths: map[string]string{
				"data.accessToken":  "access_token",
				"data.refreshToken": "refresh_token",
			},
		},
		Import: &SiteProviderImport{
			Enabled:    true,
			Containers: []string{"accounts", "items", "data"},
			FieldMap: map[string][]string{
				"access_token":  {"access_token", "accessToken"},
				"refresh_token": {"refresh_token", "refreshToken"},
			},
		},
		Request: SiteProviderRequest{
			Headers: HeaderMap(map[string]string{
				"Authorization": "Bearer {{cred.access_token}}",
				"X-Domain":      "{{cred.domain}}",
			}),
			StripDownstreamHeaders: true,
			UpstreamStreamOnly:     true,
			BaseURLFrom:            "region",
		},
		UI: &SiteProviderUI{
			RowPills: []SiteProviderRowPill{{
				Field:  "realm",
				Labels: map[string]LocalizedText{"intl": {"zh": "国际", "en": "International"}},
			}},
			Operations: []string{"add", "reauth", "refresh", "delete", "toggle", "models", "import"},
			Texts: map[string]LocalizedTextOrString{
				"add":        {"zh": "登录", "en": "Sign in"},
				"login_hint": {"": "用设备码"},
			},
		},
	}
}

func grokSection() SiteProviderSection {
	return SiteProviderSection{
		SiteType: "acme.example/grok",
		Display:  SiteProviderDisplay{Name: LocalizedText{"zh": "Grok", "en": "Grok"}},
		Hosts:    []string{"api.example.com", "auth.example.com"},
		Credential: SiteProviderCredential{
			Model: "account_pool",
			Fields: []SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "refresh_token", Secret: true},
			},
			AccessTokenField:  "access_token",
			RefreshTokenField: "refresh_token",
			Identity:          Sources("claims.sub"),
		},
		Protocol: SiteProviderProtocol{
			Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com",
			DefaultEndpointTypes: []string{"openai"},
		},
		Regions:       []SiteProviderRegion{{ID: "us", Label: LocalizedText{"en": "US"}, BaseURL: "https://api.example.com"}},
		DefaultRegion: "us",
		Login: &SiteProviderLogin{
			Kind:          "oauth2_device_code",
			DeviceCodeURL: "https://auth.example.com/device",
			TokenURL:      "https://auth.example.com/token",
			ClientID:      "grok-client",
			Scope:         "openid",
			GrantType:     "urn:ietf:params:oauth:grant-type:device_code",
			ExtraHeaders:  map[string]string{"X-Client": "xlyra"},
		},
		Refresh:   &SiteProviderRefresh{Kind: "oauth2_refresh", LeadSeconds: intPointer(300)},
		Selection: "quota_weighted",
		Quota:     &SiteProviderQuota{Aggregate: "min"},
		Request: SiteProviderRequest{
			Headers:            HeaderMap(map[string]string{"Authorization": "Bearer {{cred.access_token}}"}),
			UpstreamStreamOnly: true,
			BaseURLFrom:        "region",
		},
		UI: &SiteProviderUI{Operations: []string{"add", "reauth", "refresh", "delete", "toggle", "models"}},
	}
}

func TestAccountRegionID(t *testing.T) {
	regions := []SiteProviderRegion{
		{ID: "cn", BaseURL: "https://api.example.cn", Realms: []string{"cn", "china"}, Domains: []string{"www.example.cn"}},
		{ID: "intl", BaseURL: "https://api.example.com", Realms: []string{"intl", "international"}, Domains: []string{"www.example.com"}},
	}
	if got := AccountRegionID(regions, map[string]string{"region": "intl", "realm": "cn"}); got != "intl" {
		t.Fatalf("explicit region = %q", got)
	}
	if got := AccountRegionID(regions, map[string]string{"region": "eu"}); got != "eu" {
		t.Fatalf("unknown explicit region = %q", got)
	}
	if got := AccountRegionID(regions, map[string]string{"realm": "International"}); got != "intl" {
		t.Fatalf("realm alias = %q", got)
	}
	if got := AccountRegionID(regions, map[string]string{"domain": "www.example.cn"}); got != "cn" {
		t.Fatalf("domain = %q", got)
	}
	if got := AccountRegionID(regions, map[string]string{"realm": "mars", "domain": "nope.example"}); got != "" {
		t.Fatalf("unknown = %q", got)
	}
	if got := AccountRegionID(regions, nil); got != "" {
		t.Fatalf("nil fields = %q", got)
	}
}
