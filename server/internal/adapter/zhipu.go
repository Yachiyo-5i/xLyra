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
	// /models 接口未写入智谱官方文档（2026-09 实测可用），路径为 BaseURL + "/models"。
	models, err := z.fetchUpstreamModels(ctx, site, apiKey)
	if err != nil {
		return nil, err
	}
	// 上游 /models 只返回聊天模型，embedding、图像等静态 curated 列表补齐。
	return mergeZhipuModels(models, zhipuStaticModels(site.SiteType)), nil
}

func (z Zhipu) fetchUpstreamModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
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

// mergeZhipuModels 以动态列表为准（上游按新到旧排序），静态列表只补充上游未返回的
// 模型（embedding、图像、长文本等不在 /models 返回值里）。
func mergeZhipuModels(upstreamModels, staticModels []Model) []Model {
	seen := make(map[string]bool, len(upstreamModels))
	merged := make([]Model, 0, len(upstreamModels)+len(staticModels))
	for _, model := range upstreamModels {
		seen[strings.ToLower(model.UpstreamName)] = true
		merged = append(merged, model)
	}
	for _, model := range staticModels {
		if !seen[strings.ToLower(model.UpstreamName)] {
			merged = append(merged, model)
		}
	}
	return merged
}

func zhipuStaticModels(siteType string) []Model {
	modelIDs := []string{
		"glm-5.3",
		"glm-5.3-flash",
		"glm-5.2",
		"glm-5.1",
		"glm-5",
		"glm-5-turbo",
		"glm-4.7",
		"glm-4.7-flashx",
		"glm-4.7-flash",
		"glm-4.6",
		"glm-4.5-air",
		"glm-4.5-airx",
		"glm-4-long",
		"embedding-3",
		"embedding-2",
		"glm-image",
	}
	if strings.EqualFold(strings.TrimSpace(siteType), glmCodeSiteType) {
		modelIDs = []string{
			"glm-5.3",
			"glm-5.3-flash",
			"glm-5.1",
			"glm-5-turbo",
			"glm-4.7",
			"glm-4.5-air",
		}
	}

	models := make([]Model, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		capabilities := map[string]any{
			"supported_endpoint_types": zhipuEndpointTypes(modelID),
			"source":                   "curated",
			"tool_call":                true,
			"supports_tools":           true,
		}
		models = append(models, Model{
			UpstreamName: modelID,
			DisplayName:  modelID,
			Capabilities: capabilities,
		})
	}
	return models
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
