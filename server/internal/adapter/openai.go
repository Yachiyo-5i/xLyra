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

	"xlyra/server/internal/modelcapabilities"
)

type OpenAICompatible struct {
	client *http.Client
}

func NewOpenAICompatible() OpenAICompatible {
	return OpenAICompatible{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (a OpenAICompatible) SiteTypes() []string {
	return []string{"openai", "deepseek", "minimax", "xiaomi_mimo", "moonshot", "kimi_code"}
}

func (a OpenAICompatible) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityFetchPricing,
	}
}

func (a OpenAICompatible) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := a.ListModels(ctx, site, apiKey)
	return err
}

func (a OpenAICompatible) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	url := strings.TrimRight(site.BaseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClientForSite(site, a.client).Do(req)
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
		if item.ID == "" {
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

	return models, nil
}

func openAICompatibleEndpointTypes(modelID string, siteType string, siteBaseURL string) []string {
	if modelcapabilities.UsesModelNameEndpointInference(siteType) {
		return modelcapabilities.InferEndpointTypesForSiteModel(modelID, siteType, siteBaseURL)
	}
	if endpointTypes := modelcapabilities.EndpointTypesForModel(modelID); len(endpointTypes) > 0 {
		return endpointTypes
	}
	endpointTypes := []string{"openai"}
	if strings.TrimSpace(siteType) != "openai" {
		endpointTypes = append(endpointTypes, "anthropic-messages")
	}
	return endpointTypes
}

func isEmbeddingModelName(modelID string) bool {
	return strings.Contains(modelID, "embedding") ||
		strings.Contains(modelID, "embed-") ||
		strings.HasPrefix(modelID, "text-embedding") ||
		strings.Contains(modelID, "-embed")
}
