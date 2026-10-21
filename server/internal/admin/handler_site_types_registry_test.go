package admin

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// headSiteTypeInfos is the ListSiteTypes payload from before the registry.
var headSiteTypeInfos = []siteTypeInfo{
	{SiteType: "openai", DisplayName: "OpenAI", Subtitle: "OpenAI 平台 API Key 接入", CredentialKey: "openai", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.openai.com", ShowInCreateDialog: true, IconURL: "/brand-icons/openai.png"},
	{SiteType: "anthropic", DisplayName: "Anthropic Claude", Subtitle: "Anthropic Messages API Key 接入", CredentialKey: "anthropic", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.anthropic.com", ShowInCreateDialog: true, IconURL: "/brand-icons/anthropic.png"},
	{SiteType: "google_gemini", DisplayName: "Google Gemini", Subtitle: "Google AI Studio API Key 接入", CredentialKey: "google_gemini", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://generativelanguage.googleapis.com", ShowInCreateDialog: true, IconURL: "/brand-icons/google.png"},
	{SiteType: "grok", DisplayName: "Grok", Subtitle: "通过 xAI OAuth 设备登录接入 Grok", CredentialKey: "grok", CredentialType: "grok_oauth", RequiresBaseURL: false, OfficialBaseURL: "https://cli-chat-proxy.grok.com", ShowInCreateDialog: true, IconURL: "/brand-icons/grok.png"},
	{SiteType: "opencode_go", DisplayName: "OpenCode Go", Subtitle: "OpenCode Go 套餐 API Key 接入", CredentialKey: "opencode_go", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://opencode.ai/zen/go", ShowInCreateDialog: true, IconURL: "/brand-icons/opencode.png"},
	{SiteType: "deepseek", DisplayName: "DeepSeek", Subtitle: "DeepSeek 平台 API Key 接入", CredentialKey: "deepseek", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.deepseek.com", ShowInCreateDialog: true, IconURL: "/brand-icons/deepseek.png"},
	{SiteType: "minimax", DisplayName: "MiniMax", Subtitle: "MiniMax 平台 API Key 接入", CredentialKey: "minimax", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.minimaxi.com", ShowInCreateDialog: true, IconURL: "/brand-icons/minimax.png"},
	{SiteType: "xiaomi_mimo", DisplayName: "Xiaomi MiMo", Subtitle: "小米 MiMo API Key 接入", CredentialKey: "xiaomi_mimo", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://token-plan-cn.xiaomimimo.com", ShowInCreateDialog: true, IconURL: "/brand-icons/xiaomi.png"},
	{SiteType: "moonshot", DisplayName: "Moonshot", Subtitle: "月之暗面平台 API Key 接入", CredentialKey: "moonshot", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.moonshot.cn", ShowInCreateDialog: true, IconURL: "/brand-icons/moonshot.png"},
	{SiteType: "kimi_code", DisplayName: "Kimi Code", Subtitle: "Kimi 编程模型 API Key 接入", CredentialKey: "kimi_code", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.kimi.com/coding", ShowInCreateDialog: true, IconURL: "/brand-icons/moonshot.png"},
	{SiteType: "zhipu", DisplayName: "GLM", Subtitle: "智谱 GLM 通用 API Key 接入", CredentialKey: "zhipu", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://open.bigmodel.cn/api/paas/v4", ShowInCreateDialog: true, IconURL: "/brand-icons/zhipu.png"},
	{SiteType: "glm_code", DisplayName: "GLM Code", Subtitle: "GLM Coding Plan API Key 接入", CredentialKey: "glm_code", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://open.bigmodel.cn/api/coding/paas/v4", ShowInCreateDialog: true, IconURL: "/brand-icons/zhipu.png"},
	{SiteType: "typesafe", DisplayName: "TypeSafe", Subtitle: "TypeSafe Jev 决策模型 API Key 接入", CredentialKey: "typesafe", CredentialType: "api_key", RequiresBaseURL: false, OfficialBaseURL: "https://api.typesafe.ai", ShowInCreateDialog: true},
	{SiteType: "newapi", DisplayName: "NewAPI", Subtitle: "自建或第三方面板，使用系统令牌", CredentialKey: "newapi", CredentialType: "system_token", RequiresBaseURL: true, ShowInCreateDialog: true, IconURL: "/brand-icons/newapi.png"},
	{SiteType: "xlyra", DisplayName: "xLyra", Subtitle: "xLyra 网关接入，可使用后台访问令牌或单个下游 API Key", CredentialKey: "xlyra", CredentialType: "xlyra", RequiresBaseURL: true, ShowInCreateDialog: true, IconURL: "/brand-icons/xlyra.png"},
	{SiteType: "codex", DisplayName: "Codex OAuth", Subtitle: "通过 OAuth 接入 OpenAI Codex 账号", CredentialKey: "codex", CredentialType: "oauth", RequiresBaseURL: false, ShowInCreateDialog: false, IconURL: "/oauth-icons/codex.svg"},
	{SiteType: "antigravity", DisplayName: "Antigravity OAuth", Subtitle: "通过 OAuth 接入 Antigravity 账号", CredentialKey: "antigravity", CredentialType: "oauth", RequiresBaseURL: false, ShowInCreateDialog: false, IconURL: "/oauth-icons/antigravity.png"},
	{SiteType: "claude_code", DisplayName: "Claude Code OAuth", Subtitle: "通过 OAuth 接入 Claude Code 订阅账号", CredentialKey: "claude_code", CredentialType: "oauth", RequiresBaseURL: false, ShowInCreateDialog: false, IconURL: "/oauth-icons/claudecode.png"},
}

func TestListSiteTypesJSONMatchesHeadValues(t *testing.T) {
	t.Parallel()

	rec := adminPerform((Handler{}).ListSiteTypes, adminTestRequest(http.MethodGet, "/api/v1/site-types", ""))
	adminAssertStatus(t, rec, http.StatusOK)

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	assertExactJSONKeys(t, envelope, "site type envelope", "items", "meta")

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(envelope["meta"], &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	assertExactJSONKeys(t, meta, "site type meta", "count")
	if string(meta["count"]) != "18" {
		t.Fatalf("meta.count = %s, want 18", meta["count"])
	}

	var items []json.RawMessage
	if err := json.Unmarshal(envelope["items"], &items); err != nil {
		t.Fatalf("decode items: %v", err)
	}
	if len(items) != len(headSiteTypeInfos) {
		t.Fatalf("item count = %d, want %d", len(items), len(headSiteTypeInfos))
	}
	for index, raw := range items {
		want := headSiteTypeInfos[index]
		var got siteTypeInfo
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode item %d: %v", index, err)
		}
		if got != want {
			t.Fatalf("item %d %s = %#v, want %#v", index, want.SiteType, got, want)
		}
		assertExactJSONKeys(t, mapFromRawObject(t, raw), want.SiteType, siteTypeJSONKeys(want)...)
	}
}

func TestSiteTypeIconURLReadsRegistry(t *testing.T) {
	t.Parallel()

	for _, item := range headSiteTypeInfos {
		for _, siteType := range []string{item.SiteType, strings.ToUpper(item.SiteType), " " + item.SiteType + " "} {
			if got := siteTypeIconURL(siteType); got != item.IconURL {
				t.Fatalf("siteTypeIconURL(%q) = %q, want %q", siteType, got, item.IconURL)
			}
		}
	}
	for _, siteType := range []string{"not_a_site_type", " NOT_A_SITE_TYPE ", ""} {
		if got := siteTypeIconURL(siteType); got != "" {
			t.Fatalf("siteTypeIconURL(%q) = %q, want empty", siteType, got)
		}
	}
}

func siteTypeJSONKeys(item siteTypeInfo) []string {
	keys := []string{
		"site_type",
		"display_name",
		"subtitle",
		"credential_key",
		"credential_type",
		"requires_base_url",
		"show_in_create_dialog",
		"icon_url",
	}
	if item.OfficialBaseURL != "" {
		keys = append(keys, "official_base_url")
	}
	return keys
}

func mapFromRawObject(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()

	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	return object
}

func assertExactJSONKeys(t *testing.T, object map[string]json.RawMessage, label string, want ...string) {
	t.Helper()

	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	slices.Sort(got)
	slices.Sort(want)
	if len(got) != len(want) {
		t.Fatalf("%s keys = %v, want %v", label, got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("%s keys = %v, want %v", label, got, want)
		}
	}
}
