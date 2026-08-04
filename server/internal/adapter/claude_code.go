package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	claudeCodeSiteType       = "claude_code"
	claudeCodeDefaultBaseURL = "https://api.anthropic.com"
	ClaudeCodeClientVersion  = "2.1.205"
	ClaudeCodeUserAgent      = "claude-cli/" + ClaudeCodeClientVersion + " (external, cli)"
	ClaudeCodeOAuthBeta      = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,prompt-caching-scope-2026-01-05,effort-2025-11-24,context-management-2025-06-27,extended-cache-ttl-2025-04-11"
	ClaudeCodeAPIVersion     = "2023-06-01"
)

type ClaudeCode struct {
	client *http.Client
}

func NewClaudeCode() ClaudeCode {
	return ClaudeCode{client: &http.Client{Timeout: 30 * time.Second}}
}

func (ClaudeCode) SiteTypes() []string {
	return []string{claudeCodeSiteType}
}

func (ClaudeCode) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityListAPIKeys,
		CapabilitySummarizeAPIKey,
		CapabilityFetchUserSummary,
		CapabilityFetchBalance,
		CapabilityFetchMetadata,
	}
}

func (ClaudeCode) DefaultBaseURL() string {
	return claudeCodeDefaultBaseURL
}

func (a ClaudeCode) ValidateSystemCredentials(ctx context.Context, site SiteConfig, auth SystemAuth) error {
	_, err := a.ListModelsWithAuth(ctx, site, auth)
	return err
}

func (a ClaudeCode) ValidateCredentials(ctx context.Context, site SiteConfig, accessToken string) error {
	_, err := a.ListModels(ctx, site, accessToken)
	return err
}

func (a ClaudeCode) ListModelsWithAuth(ctx context.Context, site SiteConfig, auth SystemAuth) ([]Model, error) {
	return a.ListModels(ctx, site, auth.AccessToken)
}

func (a ClaudeCode) ListModels(ctx context.Context, site SiteConfig, accessToken string) ([]Model, error) {
	client := httpClientForSite(site, a.client)
	baseURL := claudeCodeBaseURL(site.BaseURL)
	models := make([]Model, 0)
	afterID := ""
	for {
		page, err := a.listModelsPage(ctx, client, baseURL, accessToken, afterID)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Data {
			modelID := strings.TrimSpace(item.ID)
			if modelID == "" {
				continue
			}
			capabilities := map[string]any{
				"source":                   claudeCodeSiteType,
				"supported_endpoint_types": []string{"anthropic-messages"},
				"type":                     item.Type,
				"tool_call":                true,
				"supports_tools":           true,
			}
			if item.CreatedAt != "" {
				capabilities["created_at"] = item.CreatedAt
			}
			models = append(models, Model{
				UpstreamName: modelID,
				DisplayName:  firstNonEmptyString(item.DisplayName, modelID),
				Capabilities: capabilities,
			})
		}
		if !page.HasMore || strings.TrimSpace(page.LastID) == "" {
			break
		}
		afterID = strings.TrimSpace(page.LastID)
	}
	return models, nil
}

func (a ClaudeCode) ListAPIKeys(_ context.Context, _ SiteConfig, auth SystemAuth) ([]APIKey, error) {
	return []APIKey{{
		ID:     1,
		Name:   defaultString(auth.Email, "Claude OAuth"),
		Status: "connected",
		Key:    auth.AccessToken,
		Raw: map[string]any{
			"provider":   claudeCodeSiteType,
			"account_id": auth.AccountID,
			"email":      auth.Email,
			"plan_type":  ClaudeCodePlanType(stringFromMap(auth.Metadata, "organization_type"), stringFromMap(auth.Metadata, "rate_limit_tier")),
		},
	}}, nil
}

func (a ClaudeCode) SummarizeAPIKey(ctx context.Context, site SiteConfig, accessToken string) (APIKeySummary, error) {
	usage, err := a.fetchUsage(ctx, site, accessToken)
	if err != nil {
		return APIKeySummary{}, err
	}
	models, err := a.ListModels(ctx, site, accessToken)
	if err != nil {
		return APIKeySummary{}, err
	}
	return APIKeySummary{Usage: usage, Models: models, Raw: usage}, nil
}

func (a ClaudeCode) FetchUserSummary(ctx context.Context, site SiteConfig, auth SystemAuth) (UserSummary, error) {
	usage, err := a.fetchUsage(ctx, site, auth.AccessToken)
	if err != nil {
		return UserSummary{}, err
	}
	models, err := a.ListModelsWithAuth(ctx, site, auth)
	if err != nil {
		return UserSummary{}, err
	}
	modelItems := make([]map[string]any, 0, len(models))
	for _, model := range models {
		modelItems = append(modelItems, map[string]any{
			"id":           model.UpstreamName,
			"display_name": model.DisplayName,
			"capabilities": model.Capabilities,
		})
	}
	return UserSummary{
		User: map[string]any{
			"provider":   claudeCodeSiteType,
			"email":      auth.Email,
			"account_id": auth.AccountID,
			"plan_type":  ClaudeCodePlanType(stringFromMap(auth.Metadata, "organization_type"), stringFromMap(auth.Metadata, "rate_limit_tier")),
			"quota":      ClaudeCodeQuotaSummary(usage),
		},
		APIKeys: map[string]any{
			"count": 1,
			"mode":  "oauth_bearer",
		},
		UserModels: map[string]any{"data": modelItems},
	}, nil
}

func (a ClaudeCode) FetchBalance(ctx context.Context, site SiteConfig, auth SystemAuth) (BalanceSnapshot, error) {
	usage, err := a.fetchUsage(ctx, site, auth.AccessToken)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	return BalanceSnapshot{Raw: usage}, nil
}

func (a ClaudeCode) FetchMetadata(ctx context.Context, site SiteConfig, auth SystemAuth) (MetadataSnapshot, error) {
	usage, err := a.fetchUsage(ctx, site, auth.AccessToken)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	models, err := a.ListModelsWithAuth(ctx, site, auth)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	return MetadataSnapshot{Raw: map[string]any{"quota": usage, "models": models}}, nil
}

type claudeCodeModelsPage struct {
	Data []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	} `json:"data"`
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
}

func (a ClaudeCode) listModelsPage(ctx context.Context, client *http.Client, baseURL string, accessToken string, afterID string) (claudeCodeModelsPage, error) {
	query := url.Values{}
	query.Set("limit", "1000")
	if afterID != "" {
		query.Set("after_id", afterID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models?"+query.Encode(), nil)
	if err != nil {
		return claudeCodeModelsPage{}, fmt.Errorf("create claude code models request: %w", err)
	}
	applyClaudeCodeOAuthHeaders(req, accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return claudeCodeModelsPage{}, fmt.Errorf("call claude code models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return claudeCodeModelsPage{}, fmt.Errorf("claude code models returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var page claudeCodeModelsPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return claudeCodeModelsPage{}, fmt.Errorf("decode claude code models: %w", err)
	}
	return page, nil
}

func (a ClaudeCode) fetchUsage(ctx context.Context, site SiteConfig, accessToken string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeCodeBaseURL(site.BaseURL)+"/api/oauth/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("create claude code usage request: %w", err)
	}
	applyClaudeCodeOAuthHeaders(req, accessToken)
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call claude code usage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("claude code usage returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	result := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode claude code usage: %w", err)
	}
	return result, nil
}

func applyClaudeCodeOAuthHeaders(req *http.Request, accessToken string) {
	req.Header.Del("x-api-key")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("anthropic-version", ClaudeCodeAPIVersion)
	req.Header.Set("anthropic-beta", ClaudeCodeOAuthBeta)
	req.Header.Set("User-Agent", ClaudeCodeUserAgent)
	req.Header.Set("X-App", "cli")
	req.Header.Set("Accept", "application/json")
}

func claudeCodeBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return claudeCodeDefaultBaseURL
	}
	return base
}

func ClaudeCodePlanType(organizationType string, rateLimitTier string) string {
	tier := strings.TrimSpace(strings.ToLower(rateLimitTier))
	switch {
	case strings.Contains(tier, "max_20x") || strings.Contains(tier, "max20x"):
		return "max20x"
	case strings.Contains(tier, "max_5x") || strings.Contains(tier, "max5x"):
		return "max5x"
	}
	switch strings.TrimSpace(strings.ToLower(organizationType)) {
	case "claude_max":
		return "max"
	case "claude_pro":
		return "pro"
	case "claude_enterprise":
		return "enterprise"
	case "claude_team":
		return "team"
	default:
		return ""
	}
}

func ClaudeCodeQuotaSummary(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	result := map[string]any{
		"type": "claude_code",
		"raw":  raw,
	}
	if fiveHour := claudeCodeUsageWindow(mapFromAny(raw["five_hour"]), "5h"); fiveHour != nil {
		result["five_hour"] = fiveHour
	}
	models := claudeCodeLimitWindows(raw["limits"])
	if len(models) == 0 {
		// Older usage payloads carry per-model weekly quotas as top-level
		// seven_day_* windows instead of the limits array.
		models = claudeCodeScopedUsageWindows(raw)
	}
	if len(models) > 0 {
		result["models"] = models
		if weekly := firstClaudeCodeWeeklyWindow(models); weekly != nil {
			result["weekly"] = weekly
		}
	}
	if weekly := claudeCodeUsageWindow(firstNonNilMap(raw, "seven_day", "seven_day_sonnet", "seven_day_opus", "seven_day_cowork", "seven_day_omelette", "seven_day_oauth_apps"), "weekly"); weekly != nil {
		result["weekly"] = weekly
	}
	if spend := mapFromAny(raw["spend"]); len(spend) > 0 {
		result["spend"] = spend
	}
	if extraUsage := mapFromAny(raw["extra_usage"]); len(extraUsage) > 0 {
		result["extra_usage"] = extraUsage
	}
	return result
}

func claudeCodeUsageWindow(raw map[string]any, name string) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	used, ok := floatFromAny(raw["utilization"])
	if !ok {
		used, ok = floatFromAny(raw["percent"])
	}
	if !ok {
		return nil
	}
	return map[string]any{
		"name":              name,
		"used_percent":      clampPercentage(used),
		"remaining_percent": clampPercentage(100 - used),
		"reset_at":          raw["resets_at"],
		"raw":               raw,
	}
}

func claudeCodeLimitWindows(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		limit := mapFromAny(item)
		if len(limit) == 0 {
			continue
		}
		used, ok := floatFromAny(limit["percent"])
		if !ok {
			continue
		}
		name := firstNonEmptyString(stringFromMap(limit, "group"), stringFromMap(limit, "kind"), "limit")
		result = append(result, map[string]any{
			"name":              name,
			"display_name":      claudeCodeLimitDisplayName(limit),
			"used_percent":      clampPercentage(used),
			"remaining_percent": clampPercentage(100 - used),
			"reset_at":          limit["resets_at"],
			"active":            limit["is_active"],
			"raw":               limit,
		})
	}
	return result
}

func firstClaudeCodeWeeklyWindow(models []map[string]any) map[string]any {
	for _, item := range models {
		if strings.EqualFold(stringFromMap(item, "name"), "weekly") {
			return item
		}
	}
	return nil
}

// claudeCodeScopedUsageWindows adapts top-level model-scoped usage windows
// (seven_day_opus, seven_day_fable, ...) that are not part of the limits array.
func claudeCodeScopedUsageWindows(raw map[string]any) []map[string]any {
	reserved := map[string]bool{
		"five_hour":   true,
		"seven_day":   true,
		"limits":      true,
		"spend":       true,
		"extra_usage": true,
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		if reserved[key] {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		name := key
		if strings.HasPrefix(key, "seven_day") {
			name = "weekly"
		}
		window := claudeCodeUsageWindow(mapFromAny(raw[key]), name)
		if window == nil {
			continue
		}
		window["display_name"] = claudeCodeScopedWindowDisplayName(key)
		result = append(result, window)
	}
	return result
}

func claudeCodeScopedWindowDisplayName(key string) string {
	words := strings.Split(strings.TrimPrefix(key, "seven_day_"), "_")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func claudeCodeLimitDisplayName(limit map[string]any) string {
	scope := mapFromAny(limit["scope"])
	model := mapFromAny(scope["model"])
	displayName := stringFromMap(model, "display_name")
	if displayName != "" {
		return displayName
	}
	return firstNonEmptyString(stringFromMap(limit, "group"), stringFromMap(limit, "kind"), "limit")
}

func firstNonNilMap(source map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if item := mapFromAny(source[key]); len(item) > 0 {
			return item
		}
	}
	return nil
}

func floatFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func clampPercentage(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
