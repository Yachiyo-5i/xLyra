package site

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"

	"xlyra/server/internal/jsplugin"
)

// SiteTypeDescriptor is the built-in site type registry entry.
// APICredentialType is the public credential_type field. CredentialCategory is
// the value CredentialTypeForSiteType returns. Those differ for grok.
type SiteTypeDescriptor struct {
	SiteType                     string
	DisplayName                  string
	Subtitle                     string
	DisplayNameI18n              map[string]string `json:"display_name_i18n,omitempty"`
	SubtitleI18n                 map[string]string `json:"subtitle_i18n,omitempty"`
	CredentialKey                string
	APICredentialType            string
	CredentialCategory           string
	RequiresBaseURL              bool
	OfficialBaseURL              string
	OfficialBaseURLProvider      string
	ShowInCreateDialog           bool
	IconURL                      string
	SupportsAPIKeyCostMultiplier bool
	Accounts                     *AccountCapabilities
}

// AccountCapabilities is the account-pool surface of a site type.
// LoginKind is oauth2_device_code or custom_poll.
// UI is omitted when the manifest does not declare account chrome.
type AccountCapabilities struct {
	LoginKind       string
	SupportsImport  bool
	SupportsRegions bool
	UI              *AccountUI
}

// AccountUI is the admin chrome for one account-pool site type.
// JSON names match the admin site-type payload.
type AccountUI struct {
	RowPills   []AccountRowPill                  `json:"row_pills,omitempty"`
	Operations []string                          `json:"operations,omitempty"`
	Texts      map[string]jsplugin.LocalizedText `json:"texts,omitempty"`
}

// AccountRowPill is one non-secret field shown on an account row.
// Labels optionally renames field values.
type AccountRowPill struct {
	Field  string                            `json:"field"`
	Labels map[string]jsplugin.LocalizedText `json:"labels,omitempty"`
}

// PluginSiteRequest is the upstream header template for a plugin site type.
// Headers maps a header name to a template. A zero value means the package
// did not declare request headers.
type PluginSiteRequest struct {
	Headers                map[string]jsplugin.HeaderSpec
	StripDownstreamHeaders bool
	UpstreamStreamOnly     bool
	BaseURLFrom            string
}

type PluginSiteType struct {
	Descriptor SiteTypeDescriptor
	PluginID   string
	Enabled    bool
	Hosts      []string
	Request    PluginSiteRequest
	// QuotaAggregate is siteProvider.quota.aggregate for an API-key plugin.
	// Empty keeps the historical probe summary. Account-pool sites leave this
	// empty and use Pool.QuotaAggregate.
	QuotaAggregate string
	// Pool is set for account_pool plugin site types and nil for api_key.
	Pool *jsplugin.AccountPoolSpec
}

var siteTypeRegistry = []SiteTypeDescriptor{
	{
		SiteType:                     "openai",
		DisplayName:                  "OpenAI",
		Subtitle:                     "OpenAI 平台 API Key 接入",
		CredentialKey:                "openai",
		APICredentialType:            "api_key",
		CredentialCategory:           "api_key",
		OfficialBaseURL:              "https://api.openai.com",
		ShowInCreateDialog:           true,
		IconURL:                      "/brand-icons/openai.png",
		SupportsAPIKeyCostMultiplier: true,
	},
	{
		SiteType:                     "anthropic",
		DisplayName:                  "Anthropic Claude",
		Subtitle:                     "Anthropic Messages API Key 接入",
		CredentialKey:                "anthropic",
		APICredentialType:            "api_key",
		CredentialCategory:           "api_key",
		OfficialBaseURL:              "https://api.anthropic.com",
		ShowInCreateDialog:           true,
		IconURL:                      "/brand-icons/anthropic.png",
		SupportsAPIKeyCostMultiplier: true,
	},
	{
		SiteType:                     "google_gemini",
		DisplayName:                  "Google Gemini",
		Subtitle:                     "Google AI Studio API Key 接入",
		CredentialKey:                "google_gemini",
		APICredentialType:            "api_key",
		CredentialCategory:           "api_key",
		OfficialBaseURL:              "https://generativelanguage.googleapis.com",
		ShowInCreateDialog:           true,
		IconURL:                      "/brand-icons/google.png",
		SupportsAPIKeyCostMultiplier: true,
	},
	{
		SiteType:           "grok",
		DisplayName:        "Grok",
		Subtitle:           "通过 xAI OAuth 设备登录接入 Grok",
		CredentialKey:      "grok",
		APICredentialType:  "grok_oauth",
		CredentialCategory: "grok_sso",
		OfficialBaseURL:    "https://cli-chat-proxy.grok.com",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/grok.png",
		Accounts: &AccountCapabilities{
			LoginKind: accountLoginDeviceCode,
		},
	},
	{
		SiteType:           "opencode_go",
		DisplayName:        "OpenCode Go",
		Subtitle:           "OpenCode Go 套餐 API Key 接入",
		CredentialKey:      "opencode_go",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://opencode.ai/zen/go",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/opencode.png",
	},
	{
		SiteType:           "deepseek",
		DisplayName:        "DeepSeek",
		Subtitle:           "DeepSeek 平台 API Key 接入",
		CredentialKey:      "deepseek",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://api.deepseek.com",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/deepseek.png",
	},
	{
		SiteType:           "minimax",
		DisplayName:        "MiniMax",
		Subtitle:           "MiniMax 平台 API Key 接入",
		CredentialKey:      "minimax",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://api.minimaxi.com",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/minimax.png",
	},
	{
		SiteType:           "xiaomi_mimo",
		DisplayName:        "Xiaomi MiMo",
		Subtitle:           "小米 MiMo API Key 接入",
		CredentialKey:      "xiaomi_mimo",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://token-plan-cn.xiaomimimo.com",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/xiaomi.png",
	},
	{
		SiteType:           "moonshot",
		DisplayName:        "Moonshot",
		Subtitle:           "月之暗面平台 API Key 接入",
		CredentialKey:      "moonshot",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://api.moonshot.cn",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/moonshot.png",
	},
	{
		SiteType:           "kimi_code",
		DisplayName:        "Kimi Code",
		Subtitle:           "Kimi 编程模型 API Key 接入",
		CredentialKey:      "kimi_code",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://api.kimi.com/coding",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/moonshot.png",
	},
	{
		SiteType:           "zhipu",
		DisplayName:        "GLM",
		Subtitle:           "智谱 GLM 通用 API Key 接入",
		CredentialKey:      "zhipu",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://open.bigmodel.cn/api/paas/v4",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/zhipu.png",
	},
	{
		SiteType:           "glm_code",
		DisplayName:        "GLM Code",
		Subtitle:           "GLM Coding Plan API Key 接入",
		CredentialKey:      "glm_code",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://open.bigmodel.cn/api/coding/paas/v4",
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/zhipu.png",
	},
	{
		SiteType:           "typesafe",
		DisplayName:        "TypeSafe",
		Subtitle:           "TypeSafe Jev 决策模型 API Key 接入",
		CredentialKey:      "typesafe",
		APICredentialType:  "api_key",
		CredentialCategory: "api_key",
		OfficialBaseURL:    "https://api.typesafe.ai",
		ShowInCreateDialog: true,
	},
	{
		SiteType:           "newapi",
		DisplayName:        "NewAPI",
		Subtitle:           "自建或第三方面板，使用系统令牌",
		CredentialKey:      "newapi",
		APICredentialType:  "system_token",
		CredentialCategory: "system_token",
		RequiresBaseURL:    true,
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/newapi.png",
	},
	{
		SiteType:           "xlyra",
		DisplayName:        "xLyra",
		Subtitle:           "xLyra 网关接入，可使用后台访问令牌或单个下游 API Key",
		CredentialKey:      "xlyra",
		APICredentialType:  "xlyra",
		CredentialCategory: "xlyra",
		RequiresBaseURL:    true,
		ShowInCreateDialog: true,
		IconURL:            "/brand-icons/xlyra.png",
	},
	{
		SiteType:           "codex",
		DisplayName:        "Codex OAuth",
		Subtitle:           "通过 OAuth 接入 OpenAI Codex 账号",
		CredentialKey:      "codex",
		APICredentialType:  "oauth",
		CredentialCategory: "oauth",
		IconURL:            "/oauth-icons/codex.svg",
	},
	{
		SiteType:           "antigravity",
		DisplayName:        "Antigravity OAuth",
		Subtitle:           "通过 OAuth 接入 Antigravity 账号",
		CredentialKey:      "antigravity",
		APICredentialType:  "oauth",
		CredentialCategory: "oauth",
		IconURL:            "/oauth-icons/antigravity.png",
	},
	{
		SiteType:           "claude_code",
		DisplayName:        "Claude Code OAuth",
		Subtitle:           "通过 OAuth 接入 Claude Code 订阅账号",
		CredentialKey:      "claude_code",
		APICredentialType:  "oauth",
		CredentialCategory: "oauth",
		IconURL:            "/oauth-icons/claudecode.png",
	},
}

var pluginSiteTypes atomic.Pointer[[]PluginSiteType]

func ValidatePluginSiteTypeID(id string) error {
	if err := jsplugin.ValidateSiteTypeID(id); err != nil {
		return fmt.Errorf("invalid plugin site type id %q", id)
	}
	return nil
}

func IsPluginSiteTypeID(id string) bool {
	return ValidatePluginSiteTypeID(id) == nil
}

func accountUIFromPool(ui jsplugin.SiteProviderUI) *AccountUI {
	if len(ui.RowPills) == 0 && len(ui.Operations) == 0 && len(ui.Texts) == 0 {
		return nil
	}
	out := &AccountUI{}
	if len(ui.Operations) > 0 {
		out.Operations = append([]string(nil), ui.Operations...)
	}
	if len(ui.RowPills) > 0 {
		out.RowPills = make([]AccountRowPill, len(ui.RowPills))
		for i, pill := range ui.RowPills {
			out.RowPills[i] = AccountRowPill{Field: pill.Field, Labels: copyLocalizedLabels(pill.Labels)}
		}
	}
	if len(ui.Texts) > 0 {
		out.Texts = make(map[string]jsplugin.LocalizedText, len(ui.Texts))
		for key, text := range ui.Texts {
			out.Texts[key] = copyLocalizedText(text)
		}
	}
	return out
}

func copyLocalizedLabels(in map[string]jsplugin.LocalizedText) map[string]jsplugin.LocalizedText {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]jsplugin.LocalizedText, len(in))
	for key, text := range in {
		out[key] = copyLocalizedText(text)
	}
	return out
}

func copyLocalizedText(in jsplugin.LocalizedText) jsplugin.LocalizedText {
	if in == nil {
		return nil
	}
	out := make(jsplugin.LocalizedText, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func builtinSiteType(id string) bool {
	for _, desc := range siteTypeRegistry {
		if desc.SiteType == id {
			return true
		}
	}
	return false
}

func SetPluginSiteTypes(items []PluginSiteType) error {
	next := make([]PluginSiteType, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := strings.ToLower(strings.TrimSpace(item.Descriptor.SiteType))
		if builtinSiteType(id) {
			return fmt.Errorf("plugin site type %q conflicts with a built-in site type", id)
		}
		if err := ValidatePluginSiteTypeID(id); err != nil {
			return err
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate plugin site type %q", id)
		}
		seen[id] = struct{}{}
		next = append(next, normalizePluginSiteType(item, id))
	}
	sort.Slice(next, func(i, j int) bool {
		return next[i].Descriptor.SiteType < next[j].Descriptor.SiteType
	})
	pluginSiteTypes.Store(&next)
	return nil
}

func ClearPluginSiteTypes() {
	pluginSiteTypes.Store(nil)
}

func normalizePluginSiteType(item PluginSiteType, id string) PluginSiteType {
	desc := item.Descriptor
	desc.SiteType = id
	if len(desc.DisplayNameI18n) > 0 {
		desc.DisplayNameI18n = cloneStringMap(desc.DisplayNameI18n)
	}
	if len(desc.SubtitleI18n) > 0 {
		desc.SubtitleI18n = cloneStringMap(desc.SubtitleI18n)
	}
	desc.SupportsAPIKeyCostMultiplier = false
	desc.OfficialBaseURLProvider = ""
	item.PluginID = strings.TrimSpace(item.PluginID)
	item.Hosts = append([]string(nil), item.Hosts...)
	item.Request = clonePluginSiteRequest(item.Request)
	if item.Pool != nil {
		pool := item.Pool.Clone()
		item.Pool = &pool
		desc.APICredentialType = "account"
		desc.CredentialCategory = "account"
		desc.RequiresBaseURL = false
		if base := strings.TrimRight(strings.TrimSpace(pool.DefaultBaseURL()), "/"); base != "" {
			desc.OfficialBaseURL = base
		}
		desc.Accounts = &AccountCapabilities{
			LoginKind:       pool.Login.Kind,
			SupportsImport:  pool.Import.Enabled,
			SupportsRegions: len(pool.Regions) > 0,
			UI:              accountUIFromPool(pool.UI),
		}
		item.QuotaAggregate = ""
	} else {
		desc.APICredentialType = "api_key"
		desc.CredentialCategory = "api_key"
		desc.Accounts = nil
		item.Pool = nil
		item.QuotaAggregate = strings.TrimSpace(item.QuotaAggregate)
	}
	item.Descriptor = desc
	return item
}

func pluginAccountPoolBaseURL(siteType string) (string, bool) {
	item, ok := LookupPluginSiteType(siteType)
	if !ok || item.Pool == nil {
		return "", false
	}
	base := strings.TrimRight(strings.TrimSpace(item.Pool.DefaultBaseURL()), "/")
	if base == "" {
		return "", false
	}
	return base, true
}

func clonePluginSiteRequest(in PluginSiteRequest) PluginSiteRequest {
	out := PluginSiteRequest{
		StripDownstreamHeaders: in.StripDownstreamHeaders,
		UpstreamStreamOnly:     in.UpstreamStreamOnly,
		BaseURLFrom:            in.BaseURLFrom,
	}
	if len(in.Headers) == 0 {
		return out
	}
	out.Headers = make(map[string]jsplugin.HeaderSpec, len(in.Headers))
	for key, value := range in.Headers {
		out.Headers[key] = value
	}
	return out
}

func loadPluginSiteTypes() []PluginSiteType {
	snapshot := pluginSiteTypes.Load()
	if snapshot == nil {
		return nil
	}
	return *snapshot
}

func SiteTypeDescriptors() []SiteTypeDescriptor {
	plugins := loadPluginSiteTypes()
	descriptors := make([]SiteTypeDescriptor, 0, len(siteTypeRegistry)+len(plugins))
	descriptors = append(descriptors, siteTypeRegistry...)
	for _, item := range plugins {
		descriptors = append(descriptors, item.Descriptor)
	}
	return descriptors
}

func LookupSiteType(siteType string) (SiteTypeDescriptor, bool) {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if key == "" {
		return SiteTypeDescriptor{}, false
	}
	for _, desc := range siteTypeRegistry {
		if desc.SiteType == key {
			return desc, true
		}
	}
	for _, item := range loadPluginSiteTypes() {
		if item.Descriptor.SiteType == key {
			return item.Descriptor, true
		}
	}
	return SiteTypeDescriptor{}, false
}

func LookupPluginSiteType(siteType string) (PluginSiteType, bool) {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if key == "" {
		return PluginSiteType{}, false
	}
	for _, item := range loadPluginSiteTypes() {
		if item.Descriptor.SiteType != key {
			continue
		}
		item.Hosts = append([]string(nil), item.Hosts...)
		item.Request = clonePluginSiteRequest(item.Request)
		if item.Pool != nil {
			pool := item.Pool.Clone()
			item.Pool = &pool
		}
		if item.Descriptor.Accounts != nil {
			accounts := *item.Descriptor.Accounts
			item.Descriptor.Accounts = &accounts
		}
		return item, true
	}
	return PluginSiteType{}, false
}

// SiteTypeAvailable reports whether a site type may take part in routing and
// the downstream model catalog. A type without "/" is built-in and always
// available. A type with "/" is available only when that type is present and
// enabled in the plugin snapshot, the same rule as CheckPluginHost.
func SiteTypeAvailable(siteType string) bool {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if !strings.Contains(key, "/") {
		return true
	}
	item, ok := LookupPluginSiteType(key)
	return ok && item.Enabled
}

// pluginProviderUnavailable reports that siteType names a plugin provider
// (it contains "/") and that provider is absent from the current snapshot.
// A slash-free type, including an unknown built-in name, is not this case.
// A plugin type that is still listed, even when disabled, is not this case.
func pluginProviderUnavailable(siteType string) bool {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if !strings.Contains(key, "/") {
		return false
	}
	_, ok := LookupPluginSiteType(key)
	return !ok
}

// ProviderStatus is the computed admin field for a site's provider. Built-in
// types return an empty string. Plugin types return "enabled" or "unavailable".
func ProviderStatus(siteType string) string {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if !strings.Contains(key, "/") {
		return ""
	}
	if SiteTypeAvailable(key) {
		return "enabled"
	}
	return "unavailable"
}
