package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const antigravityModelsDevSourceURL = "https://models.dev/api.json"

type antigravityModelsDevCatalog map[string]antigravityModelsDevProvider

type antigravityModelsDevProvider struct {
	Models map[string]antigravityModelsDevModel `json:"models"`
}

type antigravityModelsDevModel struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Cost       map[string]any      `json:"cost"`
	Modalities map[string][]string `json:"modalities"`
	Limit      map[string]any      `json:"limit"`
}

type antigravityModelsDevRef struct {
	Provider string
	ModelID  string
}

func (a Antigravity) FetchPricing(ctx context.Context, site SiteConfig, auth SystemAuth) (PricingSnapshot, error) {
	quota, err := a.fetchQuota(ctx, site, auth.AccessToken, stringFromMap(auth.Metadata, "project_id"))
	if err != nil {
		return PricingSnapshot{}, err
	}
	catalog, err := a.fetchModelsDevCatalog(ctx, site)
	if err != nil {
		return PricingSnapshot{}, err
	}
	return antigravityPricingFromModelsDev(quota.Models, catalog), nil
}

func (a Antigravity) fetchModelsDevCatalog(ctx context.Context, site SiteConfig) (antigravityModelsDevCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, antigravityModelsDevSourceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create models.dev request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xLyra/1.0")
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev pricing: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models.dev pricing returned %d", resp.StatusCode)
	}
	catalog := antigravityModelsDevCatalog{}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, fmt.Errorf("decode models.dev pricing: %w", err)
	}
	return catalog, nil
}

func antigravityPricingFromModelsDev(models []antigravityModelQuota, catalog antigravityModelsDevCatalog) PricingSnapshot {
	items := make([]ModelPricing, 0, len(models))
	rawItems := make([]map[string]any, 0, len(models))
	for _, model := range models {
		provider, sourceID, source, ok := antigravityModelsDevLookup(catalog, model.Name)
		if !ok {
			continue
		}
		item := antigravityModelPricingFromModelsDev(model, provider, sourceID, source)
		items = append(items, item)
		rawItems = append(rawItems, item.Raw)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModelName < items[j].ModelName
	})
	sort.Slice(rawItems, func(i, j int) bool {
		return anyString(rawItems[i]["model_name"]) < anyString(rawItems[j]["model_name"])
	})
	return PricingSnapshot{
		Groups: []PricingGroup{{
			GroupName:   "default",
			DisplayName: "default",
			Ratio:       1,
			IsAuto:      true,
			Raw: map[string]any{
				"group_name": "default",
				"ratio":      1,
				"source":     "models_dev",
			},
		}},
		Items: items,
		Raw: map[string]any{
			"provider":   antigravitySiteType,
			"currency":   "USD",
			"group_name": "default",
			"items":      rawItems,
			"source":     "models_dev",
			"source_url": antigravityModelsDevSourceURL,
		},
	}
}

func antigravityModelPricingFromModelsDev(model antigravityModelQuota, provider string, sourceID string, source antigravityModelsDevModel) ModelPricing {
	input, hasInput := antigravityModelsDevCost(source.Cost, "input")
	output, hasOutput := antigravityModelsDevCost(source.Cost, "output")
	cacheRead, hasCacheRead := antigravityModelsDevCost(source.Cost, "cache_read")
	cacheWrite, hasCacheWrite := antigravityModelsDevCost(source.Cost, "cache_write")
	endpoints := anySliceFromAny(antigravitySupportedEndpointTypes(model))
	description := fmt.Sprintf("models.dev %s snapshot for %s.", provider, sourceID)
	if hasInput && hasOutput {
		description = fmt.Sprintf("models.dev %s snapshot for %s: input $%.2f/M, output $%.2f/M.", provider, sourceID, input, output)
	}
	raw := map[string]any{
		"model_name":               model.Name,
		"display_name":             firstNonEmptyString(model.DisplayName, source.Name, model.Name),
		"billing_type":             "tokens",
		"currency":                 "USD",
		"description":              description,
		"supported_endpoint_types": endpoints,
		"source":                   "models_dev",
		"source_url":               antigravityModelsDevSourceURL,
		"source_provider":          provider,
		"source_model_id":          sourceID,
		"cost":                     source.Cost,
		"modalities":               source.Modalities,
		"limit":                    source.Limit,
	}
	pricing := ModelPricing{
		ModelName:   model.Name,
		DisplayName: firstNonEmptyString(model.DisplayName, source.Name, model.Name),
		GroupName:   "default",
		QuotaType:   0,
		BillingType: "tokens",
		Currency:    "USD",
		GroupRatio:  1,
		Description: description,
		Raw:         raw,
	}
	if hasInput {
		raw["input_value"] = input
		pricing.InputValue = input
		pricing.HasInputValue = true
	}
	if hasOutput {
		raw["output_value"] = output
		pricing.OutputValue = output
		pricing.HasOutputValue = true
	}
	if hasInput && hasCacheRead {
		ratio := cacheRead / input
		raw["cache_ratio"] = ratio
		raw["cache_input_value"] = cacheRead
		pricing.CacheRatio = ratio
		pricing.HasCacheRatio = true
	}
	if hasInput && hasCacheWrite {
		ratio := cacheWrite / input
		raw["create_cache_ratio"] = ratio
		raw["create_cache_input_value"] = cacheWrite
		pricing.CreateCacheRatio = ratio
		pricing.HasCreateCacheRatio = true

		cacheWrite1h, hasCacheWrite1h := antigravityModelsDevCost(source.Cost, "cache_write_1h")
		if hasCacheWrite1h {
			ratio1h := cacheWrite1h / input
			raw["create_cache_1h_ratio"] = ratio1h
			raw["create_cache_1h_input_value"] = cacheWrite1h
			pricing.CreateCache1hRatio = ratio1h
			pricing.HasCreateCache1hRatio = true
		} else if containsEndpointType(endpoints, "anthropic-messages") {
			raw["create_cache_1h_ratio"] = 2.0
			raw["create_cache_1h_input_value"] = input * 2.0
			pricing.CreateCache1hRatio = 2.0
			pricing.HasCreateCache1hRatio = true
		}
	}
	return pricing
}

func antigravityModelsDevLookup(catalog antigravityModelsDevCatalog, name string) (string, string, antigravityModelsDevModel, bool) {
	for _, ref := range antigravityModelsDevCandidates(name) {
		provider := catalog[strings.ToLower(ref.Provider)]
		if provider.Models == nil {
			continue
		}
		model, ok := provider.Models[strings.ToLower(ref.ModelID)]
		if !ok {
			continue
		}
		return strings.ToLower(ref.Provider), strings.ToLower(ref.ModelID), model, true
	}
	return "", "", antigravityModelsDevModel{}, false
}

func antigravityModelsDevCandidates(name string) []antigravityModelsDevRef {
	normalized := strings.ToLower(strings.TrimSpace(name))
	withoutThinking := strings.TrimSuffix(normalized, "-thinking")
	candidates := []antigravityModelsDevRef{}
	add := func(provider string, modelID string) {
		provider = strings.TrimSpace(provider)
		modelID = strings.TrimSpace(modelID)
		if provider == "" || modelID == "" {
			return
		}
		for _, existing := range candidates {
			if existing.Provider == provider && existing.ModelID == modelID {
				return
			}
		}
		candidates = append(candidates, antigravityModelsDevRef{Provider: provider, ModelID: modelID})
	}

	switch {
	case strings.HasPrefix(withoutThinking, "gemini-3.1-pro"):
		add("google", "gemini-3.1-pro-preview")
	case strings.HasPrefix(withoutThinking, "gemini-3.1-flash-lite"):
		add("google", "gemini-3.1-flash-lite-preview")
	case strings.HasPrefix(withoutThinking, "gemini-3.1-flash-image"):
		add("google", "gemini-3.1-flash-image-preview")
	case strings.HasPrefix(withoutThinking, "gemini-3-flash"):
		add("google", "gemini-3-flash-preview")
	case strings.HasPrefix(withoutThinking, "gemini-2.5-flash-lite"):
		add("google", "gemini-2.5-flash-lite")
	case strings.HasPrefix(withoutThinking, "gemini-2.5-flash"):
		add("google", "gemini-2.5-flash")
	case strings.HasPrefix(withoutThinking, "gemini-2.5-pro"):
		add("google", "gemini-2.5-pro")
	}

	if strings.HasPrefix(withoutThinking, "gemini") || strings.HasPrefix(withoutThinking, "imagen") {
		add("google", withoutThinking)
		add("google", normalized)
	}
	if strings.HasPrefix(withoutThinking, "claude") {
		add("anthropic", withoutThinking)
		add("anthropic", normalized)
	}
	if strings.HasPrefix(withoutThinking, "gpt") || strings.HasPrefix(withoutThinking, "o") {
		add("openai", withoutThinking)
		add("openai", normalized)
	}
	return candidates
}

func antigravityModelsDevCost(cost map[string]any, key string) (float64, bool) {
	value := antigravityFloat(cost[key])
	return value, value > 0
}
