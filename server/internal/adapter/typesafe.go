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
	TypeSafeSiteType              = "typesafe"
	TypeSafeDefaultBaseURL        = "https://api.typesafe.ai"
	TypeSafeSystemOneEndpointType = "typesafe-systemone"
)

type TypeSafe struct {
	client *http.Client
}

func NewTypeSafe() TypeSafe {
	return TypeSafe{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (a TypeSafe) SiteTypes() []string {
	return []string{TypeSafeSiteType}
}

func (a TypeSafe) DefaultBaseURL() string {
	return TypeSafeDefaultBaseURL
}

func (a TypeSafe) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
	}
}

func (a TypeSafe) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := a.ListModels(ctx, site, apiKey)
	return err
}

func (a TypeSafe) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
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
		Models []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			ReleaseDate string `json:"release_date"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode upstream models: %w", err)
	}

	models := make([]Model, 0, len(payload.Models))
	for _, item := range payload.Models {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			continue
		}
		capabilities := map[string]any{
			"source":                   TypeSafeSiteType,
			"supported_endpoint_types": []string{TypeSafeSystemOneEndpointType},
		}
		if item.Description != "" {
			capabilities["description"] = item.Description
		}
		if item.ReleaseDate != "" {
			capabilities["release_date"] = item.ReleaseDate
		}
		models = append(models, Model{
			UpstreamName: name,
			DisplayName:  name,
			Capabilities: capabilities,
		})
	}

	return models, nil
}
