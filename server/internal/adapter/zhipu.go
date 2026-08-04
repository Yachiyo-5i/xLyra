package adapter

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	zhipuSiteType          = "zhipu"
	glmCodeSiteType        = "glm_code"
	zhipuDefaultBaseURL    = "https://open.bigmodel.cn/api/paas/v4"
	glmCodeDefaultBaseURL  = "https://open.bigmodel.cn/api/coding/paas/v4"
	zhipuModelsDevProvider = "zhipuai"
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
		CapabilityFetchPricing,
	}
}

func (z Zhipu) ValidateCredentials(_ context.Context, _ SiteConfig, apiKey string) error {
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("api key is required")
	}
	return nil
}

func (z Zhipu) ListModels(_ context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("api key is required")
	}
	return zhipuStaticModels(site.SiteType), nil
}

func (z Zhipu) FetchPricing(ctx context.Context, site SiteConfig, _ SystemAuth) (PricingSnapshot, error) {
	catalog, err := (OpenAICompatible{client: z.client}).fetchModelsDevCatalog(ctx, site)
	if err != nil {
		return PricingSnapshot{}, err
	}
	models := zhipuStaticModels(site.SiteType)
	return openAICompatiblePricingFromModelsDev(zhipuModelsDevProvider, models, catalog), nil
}

func zhipuStaticModels(siteType string) []Model {
	modelIDs := []string{
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
