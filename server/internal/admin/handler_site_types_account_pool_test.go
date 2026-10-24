package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"xlyra/server/internal/jsplugin"
	sitepkg "xlyra/server/internal/site"
)

func TestListSiteTypesAccountPoolPlugin(t *testing.T) {
	sitepkg.ClearPluginSiteTypes()
	t.Cleanup(sitepkg.ClearPluginSiteTypes)

	spec := jsplugin.AccountPoolSpec{
		Regions:       []jsplugin.SiteProviderRegion{{ID: "cn", BaseURL: "https://api.example.cn"}},
		DefaultRegion: "cn",
		Login:         jsplugin.SiteProviderLogin{Kind: "custom_poll"},
		Import:        jsplugin.SiteProviderImport{Enabled: true},
		UI: jsplugin.SiteProviderUI{
			RowPills: []jsplugin.SiteProviderRowPill{{
				Field:  "realm",
				Labels: map[string]jsplugin.LocalizedText{"cn": {"zh": "国内", "en": "China", "jp": "中国"}},
			}},
			Operations: []string{"add", "refresh"},
			Texts: map[string]jsplugin.LocalizedTextOrString{
				"add":        {"": "登录"},
				"login_hint": {"zh": "用设备码", "en": "Use a device code", "jp": "デバイスコード"},
			},
		},
	}
	if err := sitepkg.SetPluginSiteTypes([]sitepkg.PluginSiteType{{
		Descriptor: sitepkg.SiteTypeDescriptor{
			SiteType:           "acme.example/pool",
			DisplayName:        "账号池",
			Subtitle:           "插件账号池",
			CredentialKey:      "acme.example/pool",
			RequiresBaseURL:    true,
			OfficialBaseURL:    "https://wrong.example",
			ShowInCreateDialog: true,
			IconURL:            "/brand-icons/acme.png",
		},
		PluginID: "acme.example",
		Enabled:  true,
		Hosts:    []string{"api.example.cn"},
		Pool:     &spec,
	}}); err != nil {
		t.Fatal(err)
	}

	rec := adminPerform((Handler{}).ListSiteTypes, adminTestRequest(http.MethodGet, "/api/v1/site-types", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(envelope["items"], &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != len(headSiteTypeInfos)+1 {
		t.Fatalf("item count = %d", len(items))
	}
	for index, raw := range items[:len(headSiteTypeInfos)] {
		want := headSiteTypeInfos[index]
		var got siteTypeInfo
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !sameSiteTypeInfo(got, want) {
			t.Fatalf("builtin %s = %#v, want %#v", want.SiteType, got, want)
		}
		if strings.Contains(string(raw), "plugin_id") || strings.Contains(string(raw), "provider_status") {
			t.Fatalf("builtin %s gained plugin fields: %s", want.SiteType, raw)
		}
	}
	want := siteTypeInfo{
		SiteType:           "acme.example/pool",
		DisplayName:        "账号池",
		Subtitle:           "插件账号池",
		CredentialKey:      "acme.example/pool",
		CredentialType:     "account",
		RequiresBaseURL:    false,
		OfficialBaseURL:    "https://api.example.cn",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/acme.png",
		Accounts: &siteTypeAccountCapabilities{
			LoginKind: "custom_poll", SupportsImport: true, SupportsRegions: true,
			UI: &sitepkg.AccountUI{
				RowPills: []sitepkg.AccountRowPill{{
					Field:  "realm",
					Labels: map[string]jsplugin.LocalizedText{"cn": {"zh": "国内", "en": "China", "jp": "中国"}},
				}},
				Operations: []string{"add", "refresh"},
				Texts: map[string]jsplugin.LocalizedText{
					"add":        {"": "登录"},
					"login_hint": {"zh": "用设备码", "en": "Use a device code", "jp": "デバイスコード"},
				},
			},
		},
		PluginID:       "acme.example",
		ProviderStatus: "enabled",
	}
	var got siteTypeInfo
	raw := items[len(items)-1]
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !sameSiteTypeInfo(got, want) {
		t.Fatalf("plugin = %#v, want %#v", got, want)
	}
	keys := append(siteTypeJSONKeys(want), "plugin_id", "provider_status")
	assertExactJSONKeys(t, mapFromRawObject(t, raw), want.SiteType, keys...)
	accounts := mapFromRawObject(t, mapFromRawObject(t, raw)["accounts"])
	assertExactJSONKeys(t, accounts, "plugin accounts", "login_kind", "supports_import", "supports_regions", "ui")
	ui := mapFromRawObject(t, accounts["ui"])
	assertExactJSONKeys(t, ui, "plugin accounts.ui", "row_pills", "operations", "texts")
	if strings.Contains(string(raw), "rowPills") || strings.Contains(string(accounts["ui"]), `"add":{"`) {
		t.Fatalf("accounts.ui did not use the admin shape: %s", accounts["ui"])
	}
}
