package site

import "strings"

// SiteTypeDescriptor is the built-in site type registry entry.
// APICredentialType is the public credential_type field. CredentialCategory is
// the value CredentialTypeForSiteType returns. Those differ for grok.
type SiteTypeDescriptor struct {
	SiteType                     string
	DisplayName                  string
	Subtitle                     string
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
type AccountCapabilities struct {
	LoginKind       string
	SupportsImport  bool
	SupportsRegions bool
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

func SiteTypeDescriptors() []SiteTypeDescriptor {
	descriptors := make([]SiteTypeDescriptor, len(siteTypeRegistry))
	copy(descriptors, siteTypeRegistry)
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
	return SiteTypeDescriptor{}, false
}
