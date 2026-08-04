package admin

import (
	"net/http"

	"xlyra/server/internal/httpx"
)

type siteTypeInfo struct {
	SiteType           string `json:"site_type"`
	DisplayName        string `json:"display_name"`
	Subtitle           string `json:"subtitle"`
	CredentialKey      string `json:"credential_key"`
	CredentialType     string `json:"credential_type"`
	RequiresBaseURL    bool   `json:"requires_base_url"`
	OfficialBaseURL    string `json:"official_base_url,omitempty"`
	ShowInCreateDialog bool   `json:"show_in_create_dialog"`
	IconURL            string `json:"icon_url"`
}

func (h Handler) ListSiteTypes(w http.ResponseWriter, r *http.Request) {
	types := []siteTypeInfo{
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
		{SiteType: "newapi", DisplayName: "NewAPI", Subtitle: "自建或第三方面板，使用系统令牌", CredentialKey: "newapi", CredentialType: "system_token", RequiresBaseURL: true, ShowInCreateDialog: true, IconURL: "/brand-icons/newapi.png"},
		{SiteType: "xlyra", DisplayName: "xLyra", Subtitle: "xLyra 网关接入，可使用后台访问令牌或单个下游 API Key", CredentialKey: "xlyra", CredentialType: "xlyra", RequiresBaseURL: true, ShowInCreateDialog: true, IconURL: "/brand-icons/xlyra.png"},
		{SiteType: "codex", DisplayName: "Codex OAuth", Subtitle: "通过 OAuth 接入 OpenAI Codex 账号", CredentialKey: "codex", CredentialType: "oauth", RequiresBaseURL: false, ShowInCreateDialog: false, IconURL: "/oauth-icons/codex.svg"},
		{SiteType: "antigravity", DisplayName: "Antigravity OAuth", Subtitle: "通过 OAuth 接入 Antigravity 账号", CredentialKey: "antigravity", CredentialType: "oauth", RequiresBaseURL: false, ShowInCreateDialog: false, IconURL: "/oauth-icons/antigravity.png"},
		{SiteType: "claude_code", DisplayName: "Claude Code OAuth", Subtitle: "通过 OAuth 接入 Claude Code 订阅账号", CredentialKey: "claude_code", CredentialType: "oauth", RequiresBaseURL: false, ShowInCreateDialog: false, IconURL: "/oauth-icons/claudecode.png"},
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"items": types,
		"meta":  map[string]any{"count": len(types)},
	})
}
