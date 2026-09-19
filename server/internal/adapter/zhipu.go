package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/upstream"
)

const (
	zhipuSiteType         = "zhipu"
	glmCodeSiteType       = "glm_code"
	zhipuDefaultBaseURL   = "https://open.bigmodel.cn/api/paas/v4"
	glmCodeDefaultBaseURL = "https://open.bigmodel.cn/api/coding/paas/v4"
)

type Zhipu struct {
	client         *http.Client
	siteType       string
	defaultBaseURL string
}

func NewZhipu() Zhipu {
	return Zhipu{
		client:         &http.Client{Timeout: 20 * time.Second},
		siteType:       zhipuSiteType,
		defaultBaseURL: zhipuDefaultBaseURL,
	}
}

func NewGLMCode() Zhipu {
	return Zhipu{
		client:         &http.Client{Timeout: 20 * time.Second},
		siteType:       glmCodeSiteType,
		defaultBaseURL: glmCodeDefaultBaseURL,
	}
}

func (z Zhipu) SiteTypes() []string {
	return []string{z.siteType}
}

func (z Zhipu) DefaultBaseURL() string {
	return z.defaultBaseURL
}

func (z Zhipu) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
	}
}

func (z Zhipu) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := z.ListModels(ctx, site, apiKey)
	return err
}

func (z Zhipu) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("api key is required")
	}
	baseURL := strings.TrimSpace(site.BaseURL)
	if baseURL == "" {
		baseURL = z.defaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Accept", "application/json")

	resp, err := httpClientForSite(site, z.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call upstream models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, upstream.NewHTTPError("upstream returned", resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
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
		capabilities := map[string]any{
			"supported_endpoint_types": zhipuEndpointTypes(item.ID),
			"source":                   "upstream",
			"tool_call":                true,
			"supports_tools":           true,
		}
		if item.OwnedBy != "" {
			capabilities["owned_by"] = item.OwnedBy
		}
		models = append(models, Model{
			UpstreamName: item.ID,
			DisplayName:  item.ID,
			Capabilities: capabilities,
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("upstream models list is empty")
	}
	return models, nil
}

func zhipuEndpointTypes(modelID string) []string {
	normalized := strings.ToLower(strings.TrimSpace(modelID))
	if isEmbeddingModelName(normalized) {
		return []string{"openai-embedding"}
	}
	if strings.Contains(normalized, "image") || strings.HasPrefix(normalized, "cogview") {
		return []string{"openai-image"}
	}
	return []string{"openai", "anthropic-messages"}
}
