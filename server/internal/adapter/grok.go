package adapter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	GrokChatBaseURL     = "https://cli-chat-proxy.grok.com"
	GrokAPIBaseURL      = "https://api.x.ai"
	GrokClientVersion   = "0.2.93"
	GrokClientSurface   = "grok-build"
	GrokTokenAuthHeader = "xai-grok-cli"
	GrokModelHeader     = "x-grok-model-override"
	GrokUserAgent       = "xai-grok-workspace/" + GrokClientVersion

	grokModelsPath    = "/v1/models"
	grokResponseLimit = 1 << 20
)

var grokHiddenModels = []Model{
	{
		UpstreamName: "grok-imagine-image-quality",
		DisplayName:  "Grok Imagine Image",
		Capabilities: map[string]any{
			"source":                   "grok",
			"supported_endpoint_types": []string{"openai-image"},
			"hidden_upstream":          true,
		},
	},
}

var ErrGrokUnauthorized = errors.New("grok upstream unauthorized")

func IsGrokUnauthorizedError(err error) bool {
	return errors.Is(err, ErrGrokUnauthorized)
}

type Grok struct {
	client *http.Client
}

func NewGrok() Grok {
	return Grok{client: &http.Client{Timeout: 30 * time.Second}}
}

func (Grok) SiteTypes() []string {
	return []string{"grok"}
}

func (Grok) DefaultBaseURL() string {
	return GrokChatBaseURL
}

func (Grok) Capabilities() []Capability {
	return []Capability{CapabilityHealthProbe, CapabilityValidateCredential, CapabilityListModels, CapabilitySummarizeAPIKey}
}

func (Grok) Scope() HealthProbeScope {
	return HealthProbeCredentialScope
}

func (a Grok) ProbeHealth(ctx context.Context, site SiteConfig, token string) ([]Model, error) {
	return a.fetchModels(ctx, site, token)
}

func (a Grok) ValidateCredentials(ctx context.Context, site SiteConfig, token string) error {
	_, err := a.fetchModels(ctx, site, token)
	return err
}

func (a Grok) ListModels(ctx context.Context, site SiteConfig, token string) ([]Model, error) {
	return a.fetchModels(ctx, site, token)
}

func (a Grok) SummarizeAPIKey(ctx context.Context, site SiteConfig, token string) (APIKeySummary, error) {
	models, err := a.fetchModels(ctx, site, token)
	if err != nil {
		return APIKeySummary{}, err
	}
	profile, profileErr := a.fetchUser(ctx, site, token)
	tier := grokTierFromToken(token)
	email := ""
	if profileErr == nil {
		if value := strings.TrimSpace(anyString(profile["subscriptionTier"])); value != "" {
			tier = value
		}
		email = strings.TrimSpace(anyString(profile["email"]))
	}
	models = appendGrokEntitledModels(models, profile)
	usage := map[string]any{
		"tier":         tier,
		"email":        email,
		"quota_status": "unknown",
		"unlimited":    false,
	}
	return APIKeySummary{
		Usage:  usage,
		Models: models,
		Raw:    map[string]any{"tier": tier, "email": email, "model_count": len(models), "user": profile},
	}, nil
}

func (a Grok) fetchUser(ctx context.Context, site SiteConfig, token string) (map[string]any, error) {
	endpoint, err := grokChatEndpoint(site.BaseURL, "/v1/user")
	if err != nil {
		return nil, err
	}
	endpoint += "?include=subscription"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	ApplyGrokUpstreamHeaders(req, token)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, grokResponseLimit))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("grok user endpoint returned status %d", resp.StatusCode)
	}
	profile := map[string]any{}
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, err
	}
	return profile, nil
}

func (a Grok) fetchModels(ctx context.Context, site SiteConfig, token string) ([]Model, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("grok access token is required")
	}
	endpoint, err := grokChatEndpoint(site.BaseURL, grokModelsPath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create grok models request: %w", err)
	}
	ApplyGrokUpstreamHeaders(req, token)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("grok models request failed: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, grokResponseLimit))
	if readErr != nil {
		return nil, fmt.Errorf("read grok models response: %w", readErr)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: status %d: %s", ErrGrokUnauthorized, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("grok upstream returned status %d", resp.StatusCode)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode grok models response: %w", err)
	}
	items := grokModelItems(payload)
	models := make([]Model, 0, len(items))
	for _, item := range items {
		id := firstNonEmptyString(
			anyString(item["model"]),
			anyString(item["modelId"]),
			anyString(item["model_id"]),
			anyString(item["id"]),
			grokNestedString(item, "_meta", "name"),
			anyString(item["name"]),
		)
		if id == "" {
			continue
		}
		visibility := strings.ToLower(anyString(item["visibility"]))
		if visibility == "hide" || visibility == "hidden" {
			continue
		}
		if available, ok := item["available"].(bool); ok && !available {
			continue
		}
		display := firstNonEmptyString(anyString(item["display_name"]), anyString(item["displayName"]), anyString(item["title"]), anyString(item["name"]), id)
		models = append(models, Model{
			UpstreamName: id,
			DisplayName:  display,
			Capabilities: map[string]any{
				"source":                   "grok",
				"supported_endpoint_types": grokSupportedEndpointTypes(item, id),
				"raw":                      item,
			},
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("grok returned no models")
	}
	return models, nil
}

func grokModelItems(payload any) []map[string]any {
	for {
		container, ok := payload.(map[string]any)
		if !ok {
			break
		}
		if data, exists := container["data"]; exists {
			payload = data
			continue
		}
		if models, exists := container["models"]; exists {
			payload = models
			continue
		}
		return nil
	}
	values, ok := payload.([]any)
	if !ok {
		return nil
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case map[string]any:
			items = append(items, typed)
		case string:
			if id := strings.TrimSpace(typed); id != "" {
				items = append(items, map[string]any{"id": id})
			}
		}
	}
	return items
}

func grokNestedString(item map[string]any, container string, key string) string {
	nested, _ := item[container].(map[string]any)
	return anyString(nested[key])
}

func IsGrokImageModel(upstreamName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(upstreamName)), "grok-imagine-image")
}

func IsGrokVideoModel(upstreamName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(upstreamName)), "grok-imagine-video")
}

func appendGrokEntitledModels(models []Model, profile map[string]any) []Model {
	if !grokProfileHasEntitlement(profile, "image_generation") {
		return models
	}
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		seen[model.UpstreamName] = struct{}{}
	}
	for _, hidden := range grokHiddenModels {
		if _, ok := seen[hidden.UpstreamName]; !ok {
			models = append(models, hidden)
		}
	}
	return models
}

func grokProfileHasEntitlement(profile map[string]any, entitlement string) bool {
	keys := map[string]struct{}{
		strings.ReplaceAll(strings.ToLower(entitlement), "_", ""): {},
		"imagineimage": {},
	}
	var inspect func(any) bool
	inspect = func(value any) bool {
		switch typed := value.(type) {
		case map[string]any:
			for key, nested := range typed {
				normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
				if _, ok := keys[normalized]; ok && grokEntitlementEnabled(nested) {
					return true
				}
				if inspect(nested) {
					return true
				}
			}
		case []any:
			for _, nested := range typed {
				if name, ok := nested.(string); ok {
					normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(name)))
					if _, exists := keys[normalized]; exists {
						return true
					}
				}
				if inspect(nested) {
					return true
				}
			}
		}
		return false
	}
	return inspect(profile)
}

func grokEntitlementEnabled(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "active", "available", "enabled", "included", "true":
			return true
		}
	case map[string]any:
		for _, key := range []string{"active", "available", "enabled", "included"} {
			if enabled, ok := typed[key].(bool); ok && enabled {
				return true
			}
		}
	}
	return false
}

func ApplyGrokUpstreamHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	if !IsGrokCLIProxyURL(req.URL) {
		return
	}
	req.Header.Set("X-XAI-Token-Auth", GrokTokenAuthHeader)
	req.Header.Set("x-grok-client-version", GrokClientVersion)
	req.Header.Set("x-grok-client-surface", GrokClientSurface)
	req.Header.Set("User-Agent", GrokUserAgent)
}

func IsGrokCLIProxyURL(value *url.URL) bool {
	return value != nil && strings.EqualFold(value.Scheme, "https") && strings.EqualFold(value.Hostname(), "cli-chat-proxy.grok.com")
}

func IsGrokAPIURL(value *url.URL) bool {
	return value != nil && strings.EqualFold(value.Scheme, "https") && strings.EqualFold(value.Hostname(), "api.x.ai")
}

func grokSupportedEndpointTypes(item map[string]any, modelID string) []string {
	values := stringSliceFromAny(grokModelField(item, "supported_endpoint_types"))
	if backend := anyString(grokModelField(item, "api_backend")); backend != "" {
		values = append(values, backend)
	}
	endpoints := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		var endpoint string
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "responses", "response", "openai-response", "openai-responses":
			endpoint = "openai-response"
		case "chat", "chat_completions", "chat-completions", "openai":
			endpoint = "openai"
		case "image", "images", "openai-image":
			endpoint = "openai-image"
		case "video", "videos", "openai-video":
			endpoint = "openai-video"
		}
		if endpoint == "" {
			continue
		}
		if _, ok := seen[endpoint]; ok {
			continue
		}
		seen[endpoint] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}
	if len(endpoints) > 0 {
		return endpoints
	}
	if strings.Contains(strings.ToLower(modelID), "imagine-image") {
		return []string{"openai-image"}
	}
	if strings.Contains(strings.ToLower(modelID), "imagine-video") {
		return []string{"openai-video"}
	}
	return []string{"openai-response"}
}

func grokModelField(item map[string]any, key string) any {
	if value, ok := item[key]; ok {
		return value
	}
	for _, container := range []string{"capabilities", "_meta"} {
		nested, _ := item[container].(map[string]any)
		if value, ok := nested[key]; ok {
			return value
		}
	}
	return nil
}

func grokChatEndpoint(_ string, path string) (string, error) {
	parsed, err := url.Parse(GrokChatBaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid grok base url")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func normalizeGrokTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "premium_plus", "premium-plus", "supergrok_heavy", "supergrok-heavy", "heavy", "max":
		return "premium_plus"
	case "premium", "supergrok", "pro", "paid":
		return "premium"
	default:
		return "free"
	}
}

func grokTierFromToken(token string) string {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return "free"
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "free"
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "free"
	}
	if tier := strings.TrimSpace(anyString(claims["tier"])); tier != "" {
		return normalizeGrokTier(tier)
	}
	return "free"
}
