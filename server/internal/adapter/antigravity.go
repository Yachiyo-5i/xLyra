package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/modelcapabilities"
)

const (
	antigravitySiteType       = "antigravity"
	antigravityDefaultBaseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	antigravityUserAgent      = "antigravity/1.0.0 xLyra/1.0"
)

var antigravityQuotaEndpoints = []string{
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
	"https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
}

type Antigravity struct {
	client *http.Client
}

func NewAntigravity() Antigravity {
	return Antigravity{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a Antigravity) SiteTypes() []string {
	return []string{antigravitySiteType}
}

func (a Antigravity) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityListAPIKeys,
		CapabilitySummarizeAPIKey,
		CapabilityFetchUserSummary,
		CapabilityFetchBalance,
		CapabilityFetchPricing,
		CapabilityFetchMetadata,
	}
}

func (a Antigravity) ValidateSystemCredentials(ctx context.Context, site SiteConfig, auth SystemAuth) error {
	_, err := a.fetchQuota(ctx, site, auth.AccessToken, stringFromMap(auth.Metadata, "project_id"))
	return err
}

func (a Antigravity) ListModelsWithAuth(ctx context.Context, site SiteConfig, auth SystemAuth) ([]Model, error) {
	snapshot, err := a.fetchQuota(ctx, site, auth.AccessToken, stringFromMap(auth.Metadata, "project_id"))
	if err != nil {
		return nil, err
	}
	return antigravityModelsFromQuota(snapshot), nil
}

func (a Antigravity) ListAPIKeys(ctx context.Context, site SiteConfig, auth SystemAuth) ([]APIKey, error) {
	return []APIKey{{
		ID:        1,
		Name:      defaultAntigravityAPIKeyName(auth.Email),
		Status:    "connected",
		Key:       auth.AccessToken,
		MaskedKey: "",
		Raw: map[string]any{
			"provider":          antigravitySiteType,
			"account_id":        auth.AccountID,
			"email":             auth.Email,
			"project_id":        stringFromMap(auth.Metadata, "project_id"),
			"subscription_tier": stringFromMap(auth.Metadata, "subscription_tier"),
			"group":             "antigravity",
		},
	}}, nil
}

func (a Antigravity) SummarizeAPIKey(ctx context.Context, site SiteConfig, apiKey string) (APIKeySummary, error) {
	snapshot, err := a.fetchQuota(ctx, site, apiKey, stringFromMap(site.Meta, "oauth_project_id"))
	if err != nil {
		return APIKeySummary{}, err
	}
	return APIKeySummary{
		Usage:  snapshot.Quota,
		Models: antigravityModelsFromQuota(snapshot),
		Raw:    snapshot.Raw,
	}, nil
}

func (a Antigravity) FetchUserSummary(ctx context.Context, site SiteConfig, auth SystemAuth) (UserSummary, error) {
	projectID := stringFromMap(auth.Metadata, "project_id")
	tier := stringFromMap(auth.Metadata, "subscription_tier")
	project, err := a.loadCodeAssist(ctx, site, auth.AccessToken)
	if err == nil {
		projectID = firstNonEmptyString(project.ProjectID, projectID)
		tier = firstNonEmptyString(project.SubscriptionTier, tier)
	}
	quota, quotaErr := a.fetchQuota(ctx, site, auth.AccessToken, projectID)
	if quotaErr != nil {
		return UserSummary{}, quotaErr
	}
	if quota.ProjectID == "" {
		quota.ProjectID = projectID
	}
	if quota.SubscriptionTier == "" {
		quota.SubscriptionTier = tier
	}
	user := map[string]any{
		"provider":          antigravitySiteType,
		"email":             auth.Email,
		"account_id":        auth.AccountID,
		"project_id":        quota.ProjectID,
		"subscription_tier": quota.SubscriptionTier,
		"plan_type":         quota.SubscriptionTier,
		"quota":             quota.Quota,
	}
	return UserSummary{
		User: user,
		APIKeys: map[string]any{
			"count": 1,
			"mode":  "oauth_bearer",
		},
		UserModels: map[string]any{
			"data": antigravityRawModels(quota.Models),
		},
	}, nil
}

func (a Antigravity) FetchBalance(ctx context.Context, site SiteConfig, auth SystemAuth) (BalanceSnapshot, error) {
	snapshot, err := a.fetchQuota(ctx, site, auth.AccessToken, stringFromMap(auth.Metadata, "project_id"))
	if err != nil {
		return BalanceSnapshot{}, err
	}
	return BalanceSnapshot{Raw: snapshot.Quota}, nil
}

func (a Antigravity) FetchMetadata(ctx context.Context, site SiteConfig, auth SystemAuth) (MetadataSnapshot, error) {
	snapshot, err := a.fetchQuota(ctx, site, auth.AccessToken, stringFromMap(auth.Metadata, "project_id"))
	if err != nil {
		return MetadataSnapshot{}, err
	}
	return MetadataSnapshot{Raw: map[string]any{
		"project_id":        snapshot.ProjectID,
		"subscription_tier": snapshot.SubscriptionTier,
		"quota":             snapshot.Quota,
		"models":            antigravityRawModels(snapshot.Models),
	}}, nil
}

type antigravityProjectSnapshot struct {
	ProjectID        string
	SubscriptionTier string
	Raw              map[string]any
}

type antigravityQuotaSnapshot struct {
	ProjectID        string
	SubscriptionTier string
	Quota            map[string]any
	Models           []antigravityModelQuota
	Raw              map[string]any
}

type antigravityModelQuota struct {
	Name               string         `json:"name"`
	DisplayName        string         `json:"display_name,omitempty"`
	RemainingPercent   int            `json:"remaining_percent"`
	UsedPercent        int            `json:"used_percent"`
	ResetAt            any            `json:"reset_at,omitempty"`
	ResetTime          string         `json:"reset_time,omitempty"`
	SupportsImages     any            `json:"supports_images,omitempty"`
	SupportsThinking   any            `json:"supports_thinking,omitempty"`
	ThinkingBudget     any            `json:"thinking_budget,omitempty"`
	Recommended        any            `json:"recommended,omitempty"`
	MaxTokens          any            `json:"max_tokens,omitempty"`
	MaxOutputTokens    any            `json:"max_output_tokens,omitempty"`
	SupportedMimeTypes map[string]any `json:"supported_mime_types,omitempty"`
	Raw                map[string]any `json:"raw,omitempty"`
}

func (a Antigravity) loadCodeAssist(ctx context.Context, site SiteConfig, accessToken string) (antigravityProjectSnapshot, error) {
	base := antigravityBaseURL(site.BaseURL)
	payload := map[string]any{"metadata": map[string]any{"ideType": "ANTIGRAVITY"}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1internal:loadCodeAssist", bytes.NewReader(body))
	if err != nil {
		return antigravityProjectSnapshot{}, fmt.Errorf("create antigravity loadCodeAssist request: %w", err)
	}
	antigravitySetHeaders(req, accessToken)
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return antigravityProjectSnapshot{}, fmt.Errorf("call antigravity loadCodeAssist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return antigravityProjectSnapshot{}, fmt.Errorf("antigravity loadCodeAssist returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	raw, err := decodeJSONMap(resp.Body)
	if err != nil {
		return antigravityProjectSnapshot{}, err
	}
	return antigravityProjectFromRaw(raw), nil
}

func (a Antigravity) fetchQuota(ctx context.Context, site SiteConfig, accessToken string, projectID string) (antigravityQuotaSnapshot, error) {
	payload := map[string]any{}
	if strings.TrimSpace(projectID) != "" {
		payload["project"] = strings.TrimSpace(projectID)
	}
	body, _ := json.Marshal(payload)
	var lastErr error
	for _, endpoint := range antigravityQuotaEndpointURLs(site.BaseURL) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return antigravityQuotaSnapshot{}, fmt.Errorf("create antigravity quota request: %w", err)
		}
		antigravitySetHeaders(req, accessToken)
		resp, err := httpClientForSite(site, a.client).Do(req)
		if err != nil {
			lastErr = fmt.Errorf("call antigravity quota: %w", err)
			continue
		}
		raw, readErr := readAntigravityQuotaResponse(resp)
		if readErr != nil {
			lastErr = readErr
			continue
		}
		snapshot := antigravityQuotaFromRaw(raw)
		snapshot.ProjectID = strings.TrimSpace(projectID)
		return snapshot, nil
	}
	if lastErr != nil {
		return antigravityQuotaSnapshot{}, lastErr
	}
	return antigravityQuotaSnapshot{}, fmt.Errorf("antigravity quota endpoint list is empty")
}

func readAntigravityQuotaResponse(resp *http.Response) (map[string]any, error) {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return map[string]any{
			"models":       map[string]any{},
			"is_forbidden": true,
		}, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 || resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("antigravity quota returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("antigravity quota returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return decodeJSONMap(resp.Body)
}

func antigravitySetHeaders(req *http.Request, accessToken string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", antigravityUserAgent)
	req.Header.Set("x-client-name", "antigravity")
	req.Header.Set("x-client-version", "1.0.0")
}

func decodeJSONMap(reader io.Reader) (map[string]any, error) {
	body, err := io.ReadAll(io.LimitReader(reader, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read antigravity response: %w", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode antigravity response: %w", err)
	}
	return payload, nil
}

func antigravityBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return antigravityDefaultBaseURL
	}
	return base
}

func antigravityQuotaEndpointURLs(baseURL string) []string {
	base := antigravityBaseURL(baseURL)
	defaultBase := strings.TrimSuffix(antigravityDefaultBaseURL, "/")
	if base != defaultBase {
		return append([]string{base + "/v1internal:fetchAvailableModels"}, antigravityQuotaEndpoints...)
	}
	return antigravityQuotaEndpoints
}

func antigravityProjectFromRaw(raw map[string]any) antigravityProjectSnapshot {
	tier := antigravityTierName(mapFromAny(raw["paidTier"]))
	if tier == "" {
		tier = antigravityTierName(mapFromAny(raw["currentTier"]))
	}
	if tier == "" {
		for _, item := range sliceFromAny(raw["allowedTiers"]) {
			tierMap := mapFromAny(item)
			if boolFromAnyValue(tierMap["is_default"]) {
				tier = firstNonEmptyString(antigravityTierName(tierMap), "default")
				break
			}
		}
	}
	return antigravityProjectSnapshot{
		ProjectID:        stringFromAny(raw["cloudaicompanionProject"]),
		SubscriptionTier: tier,
		Raw:              raw,
	}
}

func antigravityTierName(raw map[string]any) string {
	if raw == nil {
		return ""
	}
	return firstNonEmptyString(stringFromAny(raw["name"]), stringFromAny(raw["id"]), stringFromAny(raw["quotaTier"]))
}

func antigravityQuotaFromRaw(raw map[string]any) antigravityQuotaSnapshot {
	modelMap := mapFromAny(raw["models"])
	models := make([]antigravityModelQuota, 0, len(modelMap))
	for name, value := range modelMap {
		if !antigravityKeepModel(name) {
			continue
		}
		info := mapFromAny(value)
		quotaInfo := mapFromAny(info["quotaInfo"])
		remaining := int(antigravityFloat(quotaInfo["remainingFraction"]) * 100)
		if remaining < 0 {
			remaining = 0
		}
		if remaining > 100 {
			remaining = 100
		}
		resetTime := stringFromAny(quotaInfo["resetTime"])
		item := antigravityModelQuota{
			Name:               name,
			DisplayName:        firstNonEmptyString(stringFromAny(info["displayName"]), name),
			RemainingPercent:   remaining,
			UsedPercent:        100 - remaining,
			ResetAt:            antigravityResetAt(resetTime),
			ResetTime:          resetTime,
			SupportsImages:     info["supportsImages"],
			SupportsThinking:   info["supportsThinking"],
			ThinkingBudget:     info["thinkingBudget"],
			Recommended:        info["recommended"],
			MaxTokens:          info["maxTokens"],
			MaxOutputTokens:    info["maxOutputTokens"],
			SupportedMimeTypes: mapStringAny(info["supportedMimeTypes"]),
			Raw:                info,
		}
		models = append(models, item)
	}
	quota := map[string]any{
		"type":         "per_model",
		"models":       antigravityQuotaModelPayloads(models),
		"raw":          raw,
		"is_forbidden": boolFromAnyValue(raw["is_forbidden"]),
	}
	return antigravityQuotaSnapshot{
		Quota:  quota,
		Models: models,
		Raw:    raw,
	}
}

func antigravityModelsFromQuota(snapshot antigravityQuotaSnapshot) []Model {
	models := make([]Model, 0, len(snapshot.Models))
	for _, item := range snapshot.Models {
		models = append(models, Model{
			UpstreamName: item.Name,
			DisplayName:  firstNonEmptyString(item.DisplayName, item.Name),
			Capabilities: map[string]any{
				"source":                   antigravitySiteType,
				"quota":                    antigravityQuotaModelPayload(item),
				"available":                item.RemainingPercent > 0,
				"supports_images":          item.SupportsImages,
				"supports_thinking":        item.SupportsThinking,
				"thinking_budget":          item.ThinkingBudget,
				"recommended":              item.Recommended,
				"max_tokens":               item.MaxTokens,
				"max_output_tokens":        item.MaxOutputTokens,
				"supported_mime_types":     item.SupportedMimeTypes,
				"supported_endpoint_types": antigravitySupportedEndpointTypes(item),
				"raw":                      item.Raw,
			},
		})
	}
	return models
}

func antigravitySupportedEndpointTypes(model antigravityModelQuota) []string {
	endpoints := []string{"openai", "openai-response", "google-gemini"}
	if antigravityImageGenerationModel(model.Name) {
		endpoints = append(endpoints, "openai-image")
	}
	return endpoints
}

func antigravityImageGenerationModel(name string) bool {
	return modelcapabilities.IsImageGenerationModel(name)
}

func antigravityRawModels(models []antigravityModelQuota) []map[string]any {
	items := make([]map[string]any, 0, len(models))
	for _, model := range models {
		payload := antigravityQuotaModelPayload(model)
		payload["id"] = model.Name
		payload["name"] = model.Name
		payload["display_name"] = firstNonEmptyString(model.DisplayName, model.Name)
		payload["display"] = payload["display_name"]
		payload["upstream_model_name"] = model.Name
		items = append(items, payload)
	}
	return items
}

func antigravityQuotaModelPayloads(models []antigravityModelQuota) []map[string]any {
	items := make([]map[string]any, 0, len(models))
	for _, model := range models {
		items = append(items, antigravityQuotaModelPayload(model))
	}
	return items
}

func antigravityQuotaModelPayload(model antigravityModelQuota) map[string]any {
	return map[string]any{
		"name":                 model.Name,
		"display_name":         firstNonEmptyString(model.DisplayName, model.Name),
		"remaining_percent":    model.RemainingPercent,
		"used_percent":         model.UsedPercent,
		"reset_at":             model.ResetAt,
		"reset_time":           model.ResetTime,
		"supports_images":      model.SupportsImages,
		"supports_thinking":    model.SupportsThinking,
		"thinking_budget":      model.ThinkingBudget,
		"recommended":          model.Recommended,
		"max_tokens":           model.MaxTokens,
		"max_output_tokens":    model.MaxOutputTokens,
		"supported_mime_types": model.SupportedMimeTypes,
	}
}

func antigravityKeepModel(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(normalized, "gemini") ||
		strings.HasPrefix(normalized, "claude") ||
		strings.HasPrefix(normalized, "gpt") ||
		strings.HasPrefix(normalized, "image") ||
		strings.HasPrefix(normalized, "imagen")
}

func antigravityResetAt(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.Unix()
	}
	return value
}

func antigravityFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func boolFromAnyValue(value any) bool {
	parsed, _ := value.(bool)
	return parsed
}

func mapFromAny(value any) map[string]any {
	parsed, _ := value.(map[string]any)
	return parsed
}

func sliceFromAny(value any) []any {
	parsed, _ := value.([]any)
	return parsed
}

func mapStringAny(value any) map[string]any {
	parsed, _ := value.(map[string]any)
	return parsed
}

func defaultAntigravityAPIKeyName(email string) string {
	if strings.TrimSpace(email) != "" {
		return strings.TrimSpace(email)
	}
	return "Antigravity OAuth"
}
