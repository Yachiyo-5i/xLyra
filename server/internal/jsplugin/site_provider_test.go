package jsplugin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidateSiteTypeID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id string
		ok bool
	}{
		{id: "acme.example/chat", ok: true},
		{id: "a.b/c", ok: true},
		{id: "a1.b-c/chat_v2", ok: true},
		{id: "ab1/a", ok: true},
		{id: "a" + strings.Repeat("b", 63) + "/a", ok: true},
		{id: "acme.example/" + "a" + strings.Repeat("b", 31), ok: true},
		{id: "ACME.EXAMPLE/chat", ok: false},
		{id: "acme.example/Chat", ok: false},
		{id: "acme.example/chat/extra", ok: false},
		{id: "acme.example/chat.v2", ok: false},
		{id: "acme.example/chat:v2", ok: false},
		{id: "ab/chat", ok: false},
		{id: "a/chat", ok: false},
		{id: "a" + strings.Repeat("b", 64) + "/a", ok: false},
		{id: "acme.example/" + "a" + strings.Repeat("b", 32), ok: false},
		{id: "a_b/chat", ok: false},
		{id: "ab./chat", ok: false},
		{id: ".ab/chat", ok: false},
		{id: "-ab/chat", ok: false},
		{id: "acme.example", ok: false},
		{id: "openai", ok: false},
		{id: "acme.example/", ok: false},
		{id: "/chat", ok: false},
		{id: "acme.example/1chat", ok: false},
		{id: "acme.example/chat-v2", ok: false},
		{id: " acme.example/chat", ok: false},
		{id: "", ok: false},
	}
	for _, tc := range cases {
		err := ValidateSiteTypeID(tc.id)
		if tc.ok && err != nil {
			t.Fatalf("ValidateSiteTypeID(%q) = %v", tc.id, err)
		}
		if !tc.ok && (err == nil || !strings.Contains(err.Error(), "invalid site type id")) {
			t.Fatalf("ValidateSiteTypeID(%q) = %v", tc.id, err)
		}
	}
	pluginIDs := []struct {
		id string
		ok bool
	}{
		{id: "abc", ok: true},
		{id: "a.b", ok: true},
		{id: "ab", ok: false},
		{id: "ABC", ok: false},
		{id: "ab.", ok: false},
		{id: "a_b", ok: false},
		{id: "a" + strings.Repeat("b", 63), ok: true},
		{id: "a" + strings.Repeat("b", 64), ok: false},
	}
	for _, sample := range pluginIDs {
		id := sample.id + "/a"
		if (ValidateSiteTypeID(id) == nil) != sample.ok {
			t.Fatalf("ValidateSiteTypeID(%q) ok=%v, want %v", id, ValidateSiteTypeID(id) == nil, sample.ok)
		}
	}
	if SiteTypeIDPattern.String() != `^[a-z0-9][a-z0-9.-]{1,62}[a-z0-9]/[a-z][a-z0-9_]{0,31}$` {
		t.Fatalf("SiteTypeIDPattern = %s", SiteTypeIDPattern.String())
	}
}

func TestSiteProviderManifestAccepts(t *testing.T) {
	t.Parallel()
	source := meta("acme.example", KindSiteProvider)
	manifest := siteProviderManifest(source)
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
	if !bytes.Equal(bytes.TrimSpace(decoded.SiteProvider.Protocol.RequestParams), []byte(`{"temperature":0}`)) {
		t.Fatalf("requestParams = %s", decoded.SiteProvider.Protocol.RequestParams)
	}
	if strings.Contains(string(encoded), `"quotaProbe"`) && strings.Contains(string(encoded), `"baseURLMode":"as_is"`) {
		t.Fatal("zero quota probe was filled in")
	}

	withIcon := manifest
	withIcon.SiteProvider.Display.Icon = "assets/icon.png"
	if err := validateManifest(withIcon, source); err != nil {
		t.Fatalf("icon path without package bytes: %v", err)
	}
	withIcon.SiteProvider.Models = SiteProviderModels{}
	withIcon.SiteProvider.Protocol.OfficialBaseURL = "https://API.EXAMPLE.COM:443/v1"
	if err := validateManifest(withIcon, source); err != nil {
		t.Fatalf("defaults and port: %v", err)
	}

	hosts := make([]string, maxSiteProviderHosts)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("h%02d.example.com", i)
	}
	many := manifest
	many.SiteProvider.Hosts = hosts
	many.SiteProvider.Protocol.OfficialBaseURL = "https://h00.example.com"
	if err := validateManifest(many, source); err != nil {
		t.Fatalf("16 hosts: %v", err)
	}

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0}
	webp := []byte("RIFF\x00\x00\x00\x00WEBP")
	for _, item := range []struct {
		name string
		body []byte
	}{{name: "icon.png", body: png}, {name: "icon.webp", body: webp}} {
		packed := manifest
		packed.SiteProvider.Display.Icon = item.name
		raw := siteProviderZip(t, packed, source, map[string][]byte{item.name: item.body})
		if _, err := ReadPackage(raw, true); err != nil {
			t.Fatalf("ReadPackage %s: %v", item.name, err)
		}
	}
}

func TestSiteProviderManifestRejects(t *testing.T) {
	t.Parallel()
	source := meta("acme.example", KindSiteProvider)
	cases := []struct {
		name    string
		mutate  func(*Manifest)
		files   map[string][]byte
		archive bool
		want    string
	}{
		{name: "site type prefix", mutate: func(m *Manifest) { m.SiteProvider.SiteType = "other.example/example" }, want: "must start with"},
		{name: "site type longer plugin id", mutate: func(m *Manifest) {
			m.ID = "acme"
			m.SiteProvider.SiteType = "acme.example/chat"
		}, want: `must start with "acme/"`},
		{name: "hosts empty", mutate: func(m *Manifest) { m.SiteProvider.Hosts = nil }, want: "hosts must list"},
		{name: "host port", mutate: func(m *Manifest) { m.SiteProvider.Hosts = []string{"api.example.com:443"} }, want: "port"},
		{name: "host scheme", mutate: func(m *Manifest) { m.SiteProvider.Hosts = []string{"https://api.example.com"} }, want: "scheme"},
		{name: "host ip", mutate: func(m *Manifest) { m.SiteProvider.Hosts = []string{"127.0.0.1"} }, want: "IP"},
		{name: "host uppercase", mutate: func(m *Manifest) { m.SiteProvider.Hosts = []string{"API.example.com"} }, want: "lowercase"},
		{name: "host duplicate", mutate: func(m *Manifest) {
			m.SiteProvider.Hosts = []string{"api.example.com", "api.example.com"}
		}, want: "duplicate"},
		{name: "hosts over 16", mutate: func(m *Manifest) {
			hosts := make([]string, maxSiteProviderHosts+1)
			for i := range hosts {
				hosts[i] = fmt.Sprintf("h%02d.example.com", i)
			}
			m.SiteProvider.Hosts = hosts
		}, want: "hosts must list"},
		{name: "official http", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.OfficialBaseURL = "http://api.example.com"
		}, want: "https"},
		{name: "official host outside", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.OfficialBaseURL = "https://other.example.com"
		}, want: "not in hosts"},
		{name: "official userinfo", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.OfficialBaseURL = "https://user:pass@api.example.com"
		}, want: "userinfo"},
		{name: "official query", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.OfficialBaseURL = "https://api.example.com?q=1"
		}, want: "query"},
		{name: "official fragment", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.OfficialBaseURL = "https://api.example.com#frag"
		}, want: "fragment"},
		{name: "protocol kind", mutate: func(m *Manifest) { m.SiteProvider.Protocol.Kind = "openai" }, want: "protocol.kind"},
		{name: "endpoint types empty", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.DefaultEndpointTypes = nil
		}, want: "defaultEndpointTypes is required"},
		{name: "endpoint type illegal", mutate: func(m *Manifest) {
			m.SiteProvider.Protocol.DefaultEndpointTypes = []string{"chat"}
		}, want: "not supported"},
		{name: "path dotdot", mutate: func(m *Manifest) { m.SiteProvider.Protocol.Path = "/v1/../chat" }, want: "path"},
		{name: "path encoded dotdot", mutate: func(m *Manifest) { m.SiteProvider.Protocol.Path = "/%2e%2e/chat" }, want: "path"},
		{name: "path missing slash", mutate: func(m *Manifest) { m.SiteProvider.Protocol.Path = "v1/chat/completions" }, want: "path"},
		{name: "models path dotdot", mutate: func(m *Manifest) { m.SiteProvider.Models.Path = "/v1/../models" }, want: "models.path"},
		{name: "credential model", mutate: func(m *Manifest) { m.SiteProvider.Credential.Model = "oauth" }, want: "api_key"},
		{name: "models auth basic", mutate: func(m *Manifest) { m.SiteProvider.Models.Auth = "basic" }, want: "header:<Name>"},
		{name: "models auth empty header", mutate: func(m *Manifest) { m.SiteProvider.Models.Auth = "header:" }, want: "header:<Name>"},
		{name: "icon extension", mutate: func(m *Manifest) { m.SiteProvider.Display.Icon = "icon.gif" }, want: ".png or .webp"},
		{name: "icon escape", mutate: func(m *Manifest) { m.SiteProvider.Display.Icon = "../icon.png" }, want: "relative path"},
		{name: "brand color", mutate: func(m *Manifest) { m.SiteProvider.Display.BrandColor = "#fff" }, want: "#RRGGBB"},
		{name: "display name empty", mutate: func(m *Manifest) { m.SiteProvider.Display.Name = nil }, want: "display.name is required"},
		{name: "quota probe section", mutate: func(m *Manifest) { m.QuotaProbe.BaseURLMode = "as_is" }, want: "quotaProbe"},
		{name: "site section", mutate: func(m *Manifest) { m.Site.PricingPath = "/prices" }, want: "site is not allowed"},
		{name: "protocol section", mutate: func(m *Manifest) { m.Protocol.Name = "acme" }, want: "protocol is not allowed"},
		{name: "automation section", mutate: func(m *Manifest) { m.Automation.Subscribes = []string{"schedule.tick"} }, want: "automation"},
		{name: "icon magic", archive: true, mutate: func(m *Manifest) { m.SiteProvider.Display.Icon = "icon.png" }, files: map[string][]byte{"icon.png": []byte("not a png")}, want: "not a valid .png"},
		{name: "icon missing", archive: true, mutate: func(m *Manifest) { m.SiteProvider.Display.Icon = "icon.png" }, want: "not in the package"},
		{name: "icon too big", archive: true, mutate: func(m *Manifest) { m.SiteProvider.Display.Icon = "icon.png" }, files: map[string][]byte{"icon.png": oversizedPNG()}, want: "exceeds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest := siteProviderManifest(source)
			tc.mutate(&manifest)
			var err error
			if tc.archive {
				err = readErr(siteProviderZip(t, manifest, source, tc.files))
			} else {
				err = validateManifest(manifest, source)
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSiteProviderExports(t *testing.T) {
	t.Parallel()
	source := meta("acme.example", KindSiteProvider)
	manifest := siteProviderManifest(source)
	plugin, err := NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.SelfTest(context.Background()); err != nil {
		t.Fatal(err)
	}

	withHook := source + "export function probe() { return { error: \"later\" }; }\n"
	manifest = siteProviderManifest(withHook)
	plugin, err = NewPlugin(manifest, withHook, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !plugin.HasHook("probe") {
		t.Fatal("probe was not recorded")
	}
	if err := plugin.SelfTest(context.Background()); err != nil {
		t.Fatal(err)
	}

	badMeta := `export const meta = { apiVersion: 1, id: "acme.example", kind: "quota_probe" };` + "\n"
	manifest = siteProviderManifest(badMeta)
	if _, err := NewPlugin(manifest, badMeta, nil, 1); err == nil || !strings.Contains(err.Error(), "meta.kind") {
		t.Fatalf("err = %v, want meta.kind", err)
	}
	notFunction := source + "export const probe = 1;\n"
	manifest = siteProviderManifest(notFunction)
	if _, err := NewPlugin(manifest, notFunction, nil, 1); err == nil || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("err = %v, want a probe export error", err)
	}
}

func TestSiteProviderScope(t *testing.T) {
	t.Parallel()
	if ScopeOf(KindSiteProvider) != ScopeSiteType || SiteBound(KindSiteProvider) || GlobalKind(KindSiteProvider) {
		t.Fatalf("scope=%s siteBound=%v global=%v", ScopeOf(KindSiteProvider), SiteBound(KindSiteProvider), GlobalKind(KindSiteProvider))
	}
}

func TestEnableSiteProviderRequiresTrustAndHosts(t *testing.T) {
	st := openTestStore(t)
	mgr := NewManager(st, nil)
	ctx := context.Background()
	id := "acme.example"
	hosts := []string{"api.example.com", "files.example.com"}

	unsigned := siteProviderManifest(meta(id, KindSiteProvider))
	if _, err := mgr.Upload(ctx, "", siteProviderZip(t, unsigned, meta(id, KindSiteProvider), nil)); err != nil {
		t.Fatal(err)
	}
	err := mgr.Enable(ctx, id, "1.0.0", EnableOptions{ConfirmUntrusted: true})
	var untrusted *SiteProviderUntrustedError
	if !errors.As(err, &untrusted) || !errors.Is(err, ErrSiteProviderUntrusted) || untrusted.Trust != TrustUnsigned {
		t.Fatalf("unsigned enable: %v", err)
	}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signedSource := meta(id, KindSiteProvider) + "// signed\n"
	signedManifest := siteProviderManifest(signedSource)
	signedManifest.Version = "1.1.0"
	signed, err := AttachSignature(siteProviderZip(t, signedManifest, signedSource, nil), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Upload(ctx, "", signed); err != nil {
		t.Fatal(err)
	}
	err = mgr.Enable(ctx, id, "1.1.0", EnableOptions{ConfirmUntrusted: true})
	untrusted = nil
	if !errors.As(err, &untrusted) || !errors.Is(err, ErrSiteProviderUntrusted) || untrusted.Trust != TrustUntrustedSigner {
		t.Fatalf("untrusted signer: %v", err)
	}
	if _, err := mgr.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}

	err = mgr.Enable(ctx, id, "1.1.0", EnableOptions{})
	var hostsErr *HostsConfirmationRequiredError
	if !errors.As(err, &hostsErr) || strings.Join(hostsErr.Hosts, ",") != strings.Join(hosts, ",") {
		t.Fatalf("missing hosts: %v", err)
	}
	err = mgr.Enable(ctx, id, "1.1.0", EnableOptions{ConfirmedHosts: []string{"api.example.com"}})
	hostsErr = nil
	if !errors.As(err, &hostsErr) || strings.Join(hostsErr.Hosts, ",") != strings.Join(hosts, ",") {
		t.Fatalf("partial hosts: %#v err=%v", hostsErr, err)
	}
	if err := mgr.Enable(ctx, id, "1.1.0", EnableOptions{ConfirmedHosts: []string{"files.example.com", "api.example.com", "api.example.com"}}); err != nil {
		t.Fatal(err)
	}
	row, err := mgr.repo.GetVersion(ctx, id, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	var granted []string
	if err := json.Unmarshal(row.GrantedPermissions, &granted); err != nil {
		t.Fatal(err)
	}
	if strings.Join(granted, ",") != "host:api.example.com,host:files.example.com" {
		t.Fatalf("granted = %#v", granted)
	}

	nextSource := meta(id, KindSiteProvider) + "// next\n"
	next := siteProviderManifest(nextSource)
	next.Version = "1.2.0"
	nextRaw, err := AttachSignature(siteProviderZip(t, next, nextSource, nil), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Upload(ctx, "", nextRaw); err != nil {
		t.Fatal(err)
	}
	err = mgr.Enable(ctx, id, "1.2.0", EnableOptions{})
	hostsErr = nil
	if !errors.As(err, &hostsErr) || len(hostsErr.Hosts) != len(hosts) {
		t.Fatalf("upgrade without confirmation: %v", err)
	}
	if err := mgr.Enable(ctx, id, "1.2.0", EnableOptions{ConfirmedHosts: hosts}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.CheckSiteBinding(ctx, id, "1.2.0"); err == nil || !strings.Contains(err.Error(), "does not support per-site binding") {
		t.Fatalf("CheckSiteBinding: %v", err)
	}
}

func siteProviderManifest(source string) Manifest {
	return Manifest{
		ID: "acme.example", Name: "Example", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: KindSiteProvider,
		SiteProvider: SiteProviderSection{
			SiteType: "acme.example/example",
			Display: SiteProviderDisplay{
				Name:       LocalizedText{"zh": "示例", "en": "Example"},
				Subtitle:   LocalizedText{"en": "API"},
				BrandColor: "#336699",
			},
			Hosts:      []string{"api.example.com", "files.example.com"},
			Credential: SiteProviderCredential{Model: "api_key"},
			Protocol: SiteProviderProtocol{
				Kind:                 "openai_chat",
				Path:                 "/v1/chat/completions",
				OfficialBaseURL:      "https://api.example.com/v1",
				DefaultEndpointTypes: []string{"openai"},
				RequestParams:        json.RawMessage(`{"temperature":0}`),
			},
			Models: SiteProviderModels{Path: "/v1/models", Auth: "header:X-Api-Key"},
		},
		SHA256: map[string]string{"plugin.js": hashSource(source)},
	}
}

func siteProviderZip(t *testing.T, manifest Manifest, source string, extra map[string][]byte) []byte {
	t.Helper()
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"manifest.json": encoded, "plugin.js": []byte(source)}
	for name, body := range extra {
		files[name] = body
	}
	raw, err := BuildPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func readErr(raw []byte) error {
	_, err := ReadPackage(raw, true)
	return err
}

func oversizedPNG() []byte {
	body := make([]byte, maxSiteProviderIconBytes+8)
	copy(body, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	return body
}
