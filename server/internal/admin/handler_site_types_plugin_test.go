package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sitepkg "xlyra/server/internal/site"
)

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
	if string(meta["count"]) != "20" {
		t.Fatalf("meta.count = %s, want 20", meta["count"])
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
