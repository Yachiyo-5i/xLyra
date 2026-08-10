package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/protocolspec"
	"xlyra/server/internal/upstream"
)

const (
	openCodeGoSiteType       = "opencode_go"
	openCodeGoDefaultBaseURL = "https://opencode.ai/zen/go"
	openCodeGoUserAgent      = "xLyra/1.0 (OpenCode-Go)"
)

type OpenCodeGo struct {
	client *http.Client
}

func NewOpenCodeGo() OpenCodeGo {
	return OpenCodeGo{client: &http.Client{Timeout: 20 * time.Second}}
}

func (OpenCodeGo) SiteTypes() []string {
	return []string{openCodeGoSiteType}
}

func (OpenCodeGo) Capabilities() []Capability {
	return []Capability{CapabilityHealthProbe, CapabilityListModels, CapabilityFetchPricing}
}

func (OpenCodeGo) DefaultBaseURL() string {
	return openCodeGoDefaultBaseURL
}

func (OpenCodeGo) Scope() HealthProbeScope {
	return HealthProbeSiteScope
}

type openCodeGoModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (a OpenCodeGo) ProbeHealth(ctx context.Context, site SiteConfig, _ string) ([]Model, error) {
	items, err := a.fetchModelItems(ctx, site)
	if err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(items))
	for _, item := range items {
		if modelID := strings.TrimSpace(item.ID); modelID != "" {
			models = append(models, Model{UpstreamName: modelID, DisplayName: modelID})
		}
	}
	return models, nil
}

func (a OpenCodeGo) ListModels(ctx context.Context, site SiteConfig, _ string) ([]Model, error) {
	items, err := a.fetchModelItems(ctx, site)
	if err != nil {
		return nil, err
	}

	specVersion, err := protocolspec.Version()
	if err != nil {
		return nil, fmt.Errorf("load OpenCode Go protocol spec: %w", err)
	}
	models := make([]Model, 0, len(items))
	for _, item := range items {
		modelID := strings.TrimSpace(item.ID)
		if modelID == "" {
			continue
		}
		endpointTypes, matched, err := protocolspec.ResolveModelEndpointTypes(openCodeGoSiteType, modelID)
		if err != nil {
			return nil, fmt.Errorf("resolve OpenCode Go model protocol for %q: %w", modelID, err)
		}
		capabilities := map[string]any{
			"source":                   "opencode_go_spec",
			"protocol_spec_version":    specVersion,
			"supported_endpoint_types": endpointTypes,
		}
		if matched {
			capabilities["protocol_mapping_status"] = "mapped"
		} else {
			capabilities["protocol_mapping_status"] = "fallback"
		}
		if item.Object != "" {
			capabilities["object"] = item.Object
		}
		if item.Created > 0 {
			capabilities["created"] = item.Created
		}
		if item.OwnedBy != "" {
			capabilities["owned_by"] = item.OwnedBy
		}
		models = append(models, Model{
			UpstreamName: modelID,
			DisplayName:  modelID,
			Capabilities: capabilities,
		})
	}
	return models, nil
}

func (a OpenCodeGo) fetchModelItems(ctx context.Context, site SiteConfig) ([]openCodeGoModelItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openCodeGoBaseURL(site.BaseURL)+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create OpenCode Go models request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", openCodeGoUserAgent)

	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call OpenCode Go models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, upstream.NewHTTPError("OpenCode Go models returned", resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data []openCodeGoModelItem `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode OpenCode Go models: %w", err)
	}
	return payload.Data, nil
}

func openCodeGoBaseURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return openCodeGoDefaultBaseURL
	}
	return baseURL
}
