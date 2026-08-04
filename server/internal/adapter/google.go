package adapter

import (
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
	googleSiteType       = "google_gemini"
	googleDefaultBaseURL = "https://generativelanguage.googleapis.com"
	googleUserAgent      = "xLyra/1.0 (Gemini-API)"
)

type Google struct {
	client *http.Client
}

func NewGoogle() Google {
	return Google{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g Google) SiteTypes() []string {
	return []string{googleSiteType}
}

func (g Google) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
	}
}

func (g Google) DefaultBaseURL() string {
	return googleDefaultBaseURL
}

func (g Google) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := g.ListModels(ctx, site, apiKey)
	return err
}

func (g Google) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	base := googleBaseURL(site.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1beta/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create google models request: %w", err)
	}
	req.Header.Set("x-goog-api-key", strings.TrimSpace(apiKey))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", googleUserAgent)

	resp, err := httpClientForSite(site, g.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call google models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("google models returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			Version                    string   `json:"version"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode google models: %w", err)
	}

	models := make([]Model, 0, len(payload.Models))
	for _, item := range payload.Models {
		modelID := googleModelID(item.Name)
		if modelID == "" {
			continue
		}
		capabilities := map[string]any{
			"supported_endpoint_types": googleSupportedEndpointTypes(modelID),
			"input_token_limit":        item.InputTokenLimit,
			"output_token_limit":       item.OutputTokenLimit,
			"version":                  item.Version,
			"description":              item.Description,
		}
		models = append(models, Model{
			UpstreamName: modelID,
			DisplayName:  firstNonEmptyString(item.DisplayName, modelID),
			Capabilities: capabilities,
		})
	}

	return models, nil
}

func googleSupportedEndpointTypes(modelID string) []string {
	endpointTypes := []string{"google-gemini"}
	if modelcapabilities.IsImageGenerationModel(modelID) {
		endpointTypes = append(endpointTypes, "openai-image")
	}
	return endpointTypes
}

func googleBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return googleDefaultBaseURL
	}
	return base
}

func googleModelID(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
