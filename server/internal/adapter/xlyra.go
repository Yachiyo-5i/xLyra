package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"xlyra/server/internal/upstream"
)

type XLyra struct {
	client *http.Client
	openai OpenAICompatible
}

type xlyraModelsCacheEntry struct {
	etag   string
	models []Model
}

var xlyraModelsCache sync.Map

func NewXLyra() XLyra {
	return XLyra{
		client: &http.Client{Timeout: 30 * time.Second},
		openai: NewOpenAICompatible(),
	}
}

func (x XLyra) SiteTypes() []string {
	return []string{"xlyra"}
}

func (x XLyra) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityListAPIKeys,
		CapabilitySummarizeAPIKey,
		CapabilityFetchUserSummary,
	}
}

func (x XLyra) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	return x.openai.ValidateCredentials(ctx, site, apiKey)
}

func (x XLyra) ValidateSystemCredentials(ctx context.Context, site SiteConfig, auth SystemAuth) error {
	if strings.TrimSpace(auth.AccessToken) == "" {
		return fmt.Errorf("xlyra access token is required")
	}
	_, err := x.adminGet(ctx, site, auth.AccessToken, "/api/v1/profile")
	return err
}

func (x XLyra) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	url := strings.TrimRight(site.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-XLyra-Sync", "models")
	cacheKey := xlyraModelsCacheKey(site, apiKey)
	if cached, ok := xlyraModelsCached(cacheKey); ok && cached.etag != "" {
		req.Header.Set("If-None-Match", cached.etag)
	}

	resp, err := httpClientForSite(site, x.openai.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call upstream models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if cached, ok := xlyraModelsCached(cacheKey); ok {
			return cloneModels(cached.models), nil
		}
		return nil, fmt.Errorf("upstream returned 304 without cached models")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, upstream.NewHTTPError("upstream returned", resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data []struct {
			ID      string         `json:"id"`
			Object  string         `json:"object"`
			OwnedBy string         `json:"owned_by"`
			Meta    map[string]any `json:"metadata"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode upstream models: %w", err)
	}

	models := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		endpointTypes := openAICompatibleEndpointTypes(item.ID, site.SiteType, site.BaseURL)
		capabilities := map[string]any{
			"object":                   item.Object,
			"supported_endpoint_types": endpointTypes,
		}
		if item.OwnedBy != "" {
			capabilities["owned_by"] = item.OwnedBy
		}
		if len(item.Meta) > 0 {
			capabilities["metadata"] = item.Meta
		}
		models = append(models, Model{
			UpstreamName: item.ID,
			DisplayName:  item.ID,
			Capabilities: capabilities,
		})
	}
	if etag := strings.TrimSpace(resp.Header.Get("ETag")); etag != "" {
		xlyraModelsCache.Store(cacheKey, xlyraModelsCacheEntry{etag: etag, models: cloneModels(models)})
	}
	return models, nil
}

func (x XLyra) ListAPIKeys(ctx context.Context, site SiteConfig, auth SystemAuth) ([]APIKey, error) {
	payload, err := x.adminGet(ctx, site, auth.AccessToken, "/api/v1/api-keys?view=sync")
	if err != nil {
		return nil, err
	}

	items, _ := payload["items"].([]any)
	result := make([]APIKey, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		key := strings.TrimSpace(anyString(item["key"]))
		masked := strings.TrimSpace(anyString(item["masked_key"]))
		if key == "" && masked == "" {
			masked = strings.TrimSpace(anyString(item["key_prefix"]))
		}
		if key == "" && masked == "" {
			continue
		}
		raw := normalizeXLyraAPIKeyRaw(item)
		result = append(result, APIKey{
			ExternalID: strings.TrimSpace(anyString(item["id"])),
			Name:       strings.TrimSpace(anyString(item["name"])),
			Status:     item["status"],
			Key:        key,
			MaskedKey:  masked,
			Raw:        raw,
		})
	}
	return result, nil
}

func (x XLyra) SummarizeAPIKey(ctx context.Context, site SiteConfig, apiKey string) (APIKeySummary, error) {
	models, err := x.ListModels(ctx, site, apiKey)
	if err != nil {
		return APIKeySummary{}, err
	}
	return APIKeySummary{
		Models: models,
		Raw:    map[string]any{"model_count": len(models)},
	}, nil
}

func (x XLyra) FetchUserSummary(ctx context.Context, site SiteConfig, auth SystemAuth) (UserSummary, error) {
	profile, err := x.adminGet(ctx, site, auth.AccessToken, "/api/v1/profile")
	if err != nil {
		return UserSummary{}, err
	}
	return UserSummary{User: profile}, nil
}

func (x XLyra) adminGet(ctx context.Context, site SiteConfig, accessToken string, path string) (map[string]any, error) {
	baseURL := strings.TrimRight(site.BaseURL, "/")
	if baseURL == "" {
		return nil, fmt.Errorf("xlyra base_url is required")
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("xlyra access token is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create xlyra admin request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Access-Token", strings.TrimSpace(accessToken))

	resp, err := httpClientForSite(site, x.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call xlyra admin api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("xlyra admin api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode xlyra admin api: %w", err)
	}
	return payload, nil
}

func normalizeXLyraAPIKeyRaw(item map[string]any) map[string]any {
	raw := make(map[string]any, len(item)+4)
	for key, value := range item {
		raw[key] = value
	}
	if _, ok := raw["remain_quota"]; !ok {
		if value, ok := firstPresent(raw, "quota_available", "quota_remaining"); ok {
			raw["remain_quota"] = value
		}
	}
	if _, ok := raw["used_quota"]; !ok {
		keys := []string{"quota_total_used", "quota_used"}
		if unlimited, _ := raw["quota_unlimited"].(bool); unlimited {
			keys = []string{"quota_used", "quota_total_used"}
		}
		if value, ok := firstPresent(raw, keys...); ok {
			raw["used_quota"] = value
		}
	}
	if _, ok := raw["unlimited_quota"]; !ok {
		if value, ok := firstPresent(raw, "quota_unlimited"); ok {
			raw["unlimited_quota"] = value
		}
	}
	if _, ok := raw["expired_time"]; !ok {
		if value, ok := firstPresent(raw, "expires_at"); ok {
			raw["expired_time"] = value
		}
	}
	return raw
}

func firstPresent(values map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok && value != nil {
			return value, true
		}
	}
	return nil, false
}

func xlyraModelsCacheKey(site SiteConfig, apiKey string) string {
	input := strings.TrimRight(site.BaseURL, "/") + "\n" + strings.TrimSpace(apiKey)
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func xlyraModelsCached(cacheKey string) (xlyraModelsCacheEntry, bool) {
	value, ok := xlyraModelsCache.Load(cacheKey)
	if !ok {
		return xlyraModelsCacheEntry{}, false
	}
	entry, ok := value.(xlyraModelsCacheEntry)
	return entry, ok
}

func cloneModels(models []Model) []Model {
	if models == nil {
		return nil
	}
	cloned := make([]Model, 0, len(models))
	for _, model := range models {
		copied := model
		if model.Capabilities != nil {
			copied.Capabilities = make(map[string]any, len(model.Capabilities))
			for key, value := range model.Capabilities {
				copied.Capabilities[key] = value
			}
		}
		cloned = append(cloned, copied)
	}
	return cloned
}
