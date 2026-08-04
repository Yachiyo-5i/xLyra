package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xlyra/server/internal/modelcapabilities"
)

const (
	anthropicSiteType       = "anthropic"
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicAPIVersion     = "2023-06-01"
	anthropicUserAgent      = "xLyra/1.0 (Anthropic-API)"
)

type Anthropic struct {
	client *http.Client
}

func NewAnthropic() Anthropic {
	return Anthropic{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a Anthropic) SiteTypes() []string {
	return []string{anthropicSiteType}
}

func (a Anthropic) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
	}
}

func (a Anthropic) DefaultBaseURL() string {
	return anthropicDefaultBaseURL
}

func (a Anthropic) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := a.ListModels(ctx, site, apiKey)
	return err
}

func (a Anthropic) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	client := httpClientForSite(site, a.client)
	baseURL := anthropicBaseURL(site.BaseURL)
	models := make([]Model, 0)
	afterID := ""
	for {
		page, err := a.listModelsPage(ctx, client, baseURL, apiKey, afterID)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Data {
			modelID := strings.TrimSpace(item.ID)
			if modelID == "" {
				continue
			}
			capabilities := map[string]any{
				"supported_endpoint_types": modelcapabilities.InferEndpointTypesForModel(modelID),
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

type anthropicModelsPage struct {
	Data []struct {
		ID          string `json:"id"`
		Type        string `json:"type"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	} `json:"data"`
	HasMore bool   `json:"has_more"`
	LastID  string `json:"last_id"`
}

func (a Anthropic) listModelsPage(ctx context.Context, client *http.Client, baseURL string, apiKey string, afterID string) (anthropicModelsPage, error) {
	query := url.Values{}
	query.Set("limit", "1000")
	if afterID != "" {
		query.Set("after_id", afterID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models?"+query.Encode(), nil)
	if err != nil {
		return anthropicModelsPage{}, fmt.Errorf("create anthropic models request: %w", err)
	}
	applyAnthropicHeaders(req, apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return anthropicModelsPage{}, fmt.Errorf("call anthropic models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return anthropicModelsPage{}, fmt.Errorf("anthropic models returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var page anthropicModelsPage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return anthropicModelsPage{}, fmt.Errorf("decode anthropic models: %w", err)
	}
	return page, nil
}

func anthropicBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return anthropicDefaultBaseURL
	}
	return base
}

func applyAnthropicHeaders(req *http.Request, apiKey string) {
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("x-api-key", strings.TrimSpace(apiKey))
	}
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", anthropicUserAgent)
}
