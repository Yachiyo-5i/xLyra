package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sitepkg "xlyra/server/internal/site"
)

func TestSiteTypeIconURLFallsBackWhenPluginTypeLeavesSnapshot(t *testing.T) {
	sitepkg.ClearPluginSiteTypes()
	t.Cleanup(sitepkg.ClearPluginSiteTypes)

	const icon = "/api/v1/plugin-icons/acme.example?v=0123456789abcdef"
	if err := sitepkg.SetPluginSiteTypes([]sitepkg.PluginSiteType{{
		Descriptor: sitepkg.SiteTypeDescriptor{
			SiteType:      "acme.example/pool",
			DisplayName:   "Acme Pool",
			CredentialKey: "acme.example/pool",
			IconURL:       icon,
		},
		PluginID: "acme.example",
		Enabled:  true,
	}}); err != nil {
		t.Fatal(err)
	}
	if got := siteTypeIconURL("acme.example/pool"); got != icon {
		t.Fatalf("in snapshot = %q, want %q", got, icon)
	}
	if got := siteTypeIconURL(" ACME.EXAMPLE/Pool "); got != icon {
		t.Fatalf("in snapshot normalized = %q, want %q", got, icon)
	}
	if got := siteTypeIconURL("openai"); got != "/brand-icons/openai.png" {
		t.Fatalf("builtin while plugin is registered = %q", got)
	}

	sitepkg.ClearPluginSiteTypes()
	for _, siteType := range []string{"acme.example/pool", " ACME.EXAMPLE/Pool ", "acme.example/chat"} {
		if got := siteTypeIconURL(siteType); got != "/api/v1/plugin-icons/acme.example" {
			t.Fatalf("siteTypeIconURL(%q) = %q, want plugin icon without cache key", siteType, got)
		}
	}
	if got := siteTypeIconURL("openai"); got != "/brand-icons/openai.png" {
		t.Fatalf("builtin after plugin left snapshot = %q", got)
	}
	if got := siteTypeIconURL("unknown"); got != "" {
		t.Fatalf("unknown builtin-shaped name = %q, want empty", got)
	}
}

func TestListSiteTypesIncludesPluginIconURL(t *testing.T) {
	sitepkg.ClearPluginSiteTypes()
	t.Cleanup(sitepkg.ClearPluginSiteTypes)

	const icon = "/api/v1/plugin-icons/acme.example?v=0123456789abcdef"
	if err := sitepkg.SetPluginSiteTypes([]sitepkg.PluginSiteType{{
		Descriptor: sitepkg.SiteTypeDescriptor{
			SiteType:           "acme.example/chat",
			DisplayName:        "Acme Chat",
			CredentialKey:      "acme.example/chat",
			ShowInCreateDialog: true,
			IconURL:            icon,
		},
		PluginID: "acme.example",
		Enabled:  true,
		Hosts:    []string{"api.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}

	rec := adminPerform((Handler{}).ListSiteTypes, adminTestRequest(http.MethodGet, "/api/v1/site-types", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	var payload struct {
		Items []siteTypeInfo `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != len(headSiteTypeInfos)+1 {
		t.Fatalf("item count = %d", len(payload.Items))
	}
	if payload.Items[0].SiteType != "openai" || payload.Items[0].IconURL != "/brand-icons/openai.png" {
		t.Fatalf("builtin openai = %#v", payload.Items[0])
	}
	got := payload.Items[len(payload.Items)-1]
	if got.SiteType != "acme.example/chat" || got.IconURL != icon {
		t.Fatalf("plugin item = %#v", got)
	}
	if siteTypeIconURL("acme.example/chat") != icon {
		t.Fatalf("siteTypeIconURL = %q", siteTypeIconURL("acme.example/chat"))
	}
}

func TestListSiteTypesAppendsEnabledPluginSiteTypes(t *testing.T) {
	sitepkg.ClearPluginSiteTypes()
	t.Cleanup(sitepkg.ClearPluginSiteTypes)

	if err := sitepkg.SetPluginSiteTypes([]sitepkg.PluginSiteType{
		{
			Descriptor: sitepkg.SiteTypeDescriptor{
				SiteType:           "zzz.example/off",
				DisplayName:        "Off",
				Subtitle:           "已停用",
				CredentialKey:      "off",
				RequiresBaseURL:    true,
				OfficialBaseURL:    "https://off.example.com",
				ShowInCreateDialog: true,
				IconURL:            "/brand-icons/off.png",
			},
			PluginID: "zzz.example",
			Enabled:  false,
			Hosts:    []string{"off.example.com"},
		},
		{
			Descriptor: sitepkg.SiteTypeDescriptor{
				SiteType:           " ACME.EXAMPLE/Chat ",
				DisplayName:        "Acme Chat",
				Subtitle:           "插件站点",
				CredentialKey:      "acme",
				RequiresBaseURL:    true,
				OfficialBaseURL:    "https://api.example.com",
				ShowInCreateDialog: true,
				IconURL:            "/brand-icons/acme.png",
			},
			PluginID: "acme.example",
			Enabled:  true,
			Hosts:    []string{"api.example.com"},
		},
	}); err != nil {
		t.Fatalf("set plugin site types: %v", err)
	}

	rec := adminPerform((Handler{}).ListSiteTypes, adminTestRequest(http.MethodGet, "/api/v1/site-types", ""))
	adminAssertStatus(t, rec, http.StatusOK)

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(envelope["meta"], &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if string(meta["count"]) != "19" {
		t.Fatalf("meta.count = %s, want 19", meta["count"])
	}
	var items []json.RawMessage
	if err := json.Unmarshal(envelope["items"], &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(items) != len(headSiteTypeInfos)+1 {
		t.Fatalf("item count = %d, want %d", len(items), len(headSiteTypeInfos)+1)
	}
	for index, raw := range items[:len(headSiteTypeInfos)] {
		want := headSiteTypeInfos[index]
		var got siteTypeInfo
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode item %d: %v", index, err)
		}
		if !sameSiteTypeInfo(got, want) {
			t.Fatalf("item %d %s = %#v, want %#v", index, want.SiteType, got, want)
		}
		assertExactJSONKeys(t, mapFromRawObject(t, raw), want.SiteType, siteTypeJSONKeys(want)...)
		if strings.Contains(string(raw), "plugin_id") || strings.Contains(string(raw), "provider_status") {
			t.Fatalf("built-in %s gained plugin fields: %s", want.SiteType, raw)
		}
	}

	wantPlugin := siteTypeInfo{
		SiteType:           "acme.example/chat",
		DisplayName:        "Acme Chat",
		Subtitle:           "插件站点",
		CredentialKey:      "acme",
		CredentialType:     "api_key",
		RequiresBaseURL:    true,
		OfficialBaseURL:    "https://api.example.com",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/acme.png",
		PluginID:           "acme.example",
		ProviderStatus:     "enabled",
	}
	var gotPlugin siteTypeInfo
	rawPlugin := items[len(items)-1]
	if err := json.Unmarshal(rawPlugin, &gotPlugin); err != nil {
		t.Fatalf("decode plugin item: %v", err)
	}
	if !sameSiteTypeInfo(gotPlugin, wantPlugin) {
		t.Fatalf("plugin item = %#v, want %#v", gotPlugin, wantPlugin)
	}
	keys := append(siteTypeJSONKeys(wantPlugin), "plugin_id", "provider_status")
	assertExactJSONKeys(t, mapFromRawObject(t, rawPlugin), wantPlugin.SiteType, keys...)
	for _, raw := range items {
		if strings.Contains(string(raw), "zzz.example/off") {
			t.Fatalf("disabled plugin was listed: %s", raw)
		}
	}
	if got := siteTypeIconURL(" ACME.EXAMPLE/Chat "); got != "/brand-icons/acme.png" {
		t.Fatalf("siteTypeIconURL(enabled) = %q", got)
	}
	if got := siteTypeIconURL("zzz.example/off"); got != "/brand-icons/off.png" {
		t.Fatalf("siteTypeIconURL(disabled) = %q", got)
	}
}

func TestListSiteTypesPluginDisplayI18n(t *testing.T) {
	sitepkg.ClearPluginSiteTypes()
	t.Cleanup(sitepkg.ClearPluginSiteTypes)

	if err := sitepkg.SetPluginSiteTypes([]sitepkg.PluginSiteType{
		{
			Descriptor: sitepkg.SiteTypeDescriptor{
				SiteType:           "acme.example/pool",
				DisplayName:        "池",
				Subtitle:           "Account pool",
				DisplayNameI18n:    map[string]string{"zh": "池", "en": "Pool", "jp": "プール"},
				SubtitleI18n:       map[string]string{"en": "Account pool"},
				CredentialKey:      "acme.example/pool",
				ShowInCreateDialog: true,
			},
			PluginID: "acme.example",
			Enabled:  true,
		},
		{
			Descriptor: sitepkg.SiteTypeDescriptor{
				SiteType:           "acme.sample/chat",
				DisplayName:        "示例",
				Subtitle:           "对话",
				DisplayNameI18n:    map[string]string{"zh": "示例", "en": "Sample"},
				SubtitleI18n:       map[string]string{"zh": "对话", "en": "Chat", "jp": "チャット"},
				CredentialKey:      "acme.sample/chat",
				ShowInCreateDialog: true,
			},
			PluginID: "acme.sample",
			Enabled:  true,
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := adminPerform((Handler{}).ListSiteTypes, adminTestRequest(http.MethodGet, "/api/v1/site-types", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Items) != len(headSiteTypeInfos)+2 {
		t.Fatalf("item count = %d", len(envelope.Items))
	}

	found := map[string]json.RawMessage{}
	for index, raw := range envelope.Items {
		var got siteTypeInfo
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode item %d: %v", index, err)
		}
		found[got.SiteType] = raw
		if index < len(headSiteTypeInfos) {
			want := headSiteTypeInfos[index]
			if !sameSiteTypeInfo(got, want) {
				t.Fatalf("builtin %s = %#v", want.SiteType, got)
			}
			if strings.Contains(string(raw), "display_name_i18n") || strings.Contains(string(raw), "subtitle_i18n") {
				t.Fatalf("builtin %s includes i18n fields: %s", want.SiteType, raw)
			}
		}
	}

	wantPool := siteTypeInfo{
		SiteType:           "acme.example/pool",
		DisplayName:        "池",
		Subtitle:           "Account pool",
		DisplayNameI18n:    map[string]string{"zh": "池", "en": "Pool", "jp": "プール"},
		SubtitleI18n:       map[string]string{"en": "Account pool"},
		CredentialKey:      "acme.example/pool",
		CredentialType:     "api_key",
		ShowInCreateDialog: true,
		PluginID:           "acme.example",
		ProviderStatus:     "enabled",
	}
	wantChat := siteTypeInfo{
		SiteType:           "acme.sample/chat",
		DisplayName:        "示例",
		Subtitle:           "对话",
		DisplayNameI18n:    map[string]string{"zh": "示例", "en": "Sample"},
		SubtitleI18n:       map[string]string{"zh": "对话", "en": "Chat", "jp": "チャット"},
		CredentialKey:      "acme.sample/chat",
		CredentialType:     "api_key",
		ShowInCreateDialog: true,
		PluginID:           "acme.sample",
		ProviderStatus:     "enabled",
	}
	for _, want := range []siteTypeInfo{wantPool, wantChat} {
		raw := found[want.SiteType]
		if raw == nil {
			t.Fatalf("missing %s", want.SiteType)
		}
		var got siteTypeInfo
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !sameSiteTypeInfo(got, want) {
			t.Fatalf("%s = %#v, want %#v", want.SiteType, got, want)
		}
		keys := append(siteTypeJSONKeys(want), "plugin_id", "provider_status")
		assertExactJSONKeys(t, mapFromRawObject(t, raw), want.SiteType, keys...)
		if strings.Contains(string(raw), `"fr"`) || strings.Contains(string(raw), `"  "`) {
			t.Fatalf("%s kept a blank language: %s", want.SiteType, raw)
		}
	}
}
