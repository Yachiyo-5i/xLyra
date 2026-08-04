package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const openAICompatibleModelsDevSourceURL = "https://models.dev/api.json"

func (a OpenAICompatible) FetchPricing(ctx context.Context, site SiteConfig, auth SystemAuth) (PricingSnapshot, error) {
	provider := openAICompatibleModelsDevProvider(site)
	if provider == "" {
		return PricingSnapshot{}, nil
	}

	catalog, err := a.fetchModelsDevCatalog(ctx, site)
	if err != nil {
		return PricingSnapshot{}, err
	}

	models, err := a.ListModels(ctx, site, auth.AccessToken)
	if err != nil {
		return openAICompatiblePricingFromModelsDevOnly(provider, catalog), nil
	}
	return openAICompatiblePricingFromModelsDev(provider, models, catalog), nil
}

func (a OpenAICompatible) fetchModelsDevCatalog(ctx context.Context, site SiteConfig) (antigravityModelsDevCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openAICompatibleModelsDevSourceURL, nil)
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

func openAICompatibleModelsDevProvider(site SiteConfig) string {
	siteType := strings.TrimSpace(site.SiteType)
	haystack := strings.ToLower(strings.Join([]string{site.Name, site.BaseURL, site.SiteType}, " "))
	switch {
	case strings.Contains(haystack, "deepseek"):
		return "deepseek"
	case strings.Contains(haystack, "minimax"):
		return "minimax"
	case strings.Contains(haystack, "xiaomi") || strings.Contains(haystack, "mimo"):
		return "xiaomi"
	case strings.Contains(haystack, "zhipu") || strings.Contains(haystack, "glm"):
		return "zhipuai"
	case strings.Contains(haystack, "moonshot") || strings.Contains(haystack, "kimi"):
		return "moonshotai-cn"
	case strings.Contains(haystack, "kimi_code") || strings.Contains(haystack, "kimi.com"):
		return "moonshotai-cn"
	case strings.Contains(haystack, "openai") || siteType == "openai":
		return "openai"
	default:
		return normalizeAdapterModelsDevProvider(siteType)
	}
}

func normalizeAdapterModelsDevProvider(siteType string) string {
	switch strings.ToLower(strings.TrimSpace(siteType)) {
	case "zhipu":
		return "zhipuai"
	case "moonshot":
		return "moonshotai-cn"
	case "kimi_code":
		return "moonshotai-cn"
	case "xiaomi_mimo":
		return "xiaomi"
	default:
		return strings.ToLower(strings.TrimSpace(siteType))
	}
}

func endpointInferenceSiteTypeForProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "zhipuai":
		return "zhipu"
	case "moonshotai-cn":
		return "moonshot"
	case "xiaomi":
		return "xiaomi_mimo"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func openAICompatiblePricingFromModelsDev(provider string, models []Model, catalog antigravityModelsDevCatalog) PricingSnapshot {
	providerPayload := catalog[strings.ToLower(provider)]
	if providerPayload.Models == nil {
		return PricingSnapshot{}
	}

	items := make([]ModelPricing, 0, len(models))
	rawItems := make([]map[string]any, 0, len(models))
	for _, model := range models {
		sourceID := strings.ToLower(strings.TrimSpace(model.UpstreamName))
		source, ok := providerPayload.Models[sourceID]
		if !ok {
			continue
		}
		item := openAICompatibleModelPricingFromModelsDev(model, strings.ToLower(provider), sourceID, source)
		items = append(items, item)
		rawItems = append(rawItems, item.Raw)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModelName < items[j].ModelName
	})
	sort.Slice(rawItems, func(i, j int) bool {
		return anyString(rawItems[i]["model_name"]) < anyString(rawItems[j]["model_name"])
	})
	if len(items) == 0 {
		return PricingSnapshot{}
	}
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
			"provider":   provider,
			"currency":   "USD",
			"group_name": "default",
			"items":      rawItems,
			"source":     "models_dev",
			"source_url": openAICompatibleModelsDevSourceURL,
		},
	}
}

func openAICompatibleModelPricingFromModelsDev(model Model, provider string, sourceID string, source antigravityModelsDevModel) ModelPricing {
	input, hasInput := antigravityModelsDevCost(source.Cost, "input")
	output, hasOutput := antigravityModelsDevCost(source.Cost, "output")
	cacheRead, hasCacheRead := antigravityModelsDevCost(source.Cost, "cache_read")
	cacheWrite, hasCacheWrite := antigravityModelsDevCost(source.Cost, "cache_write")
	endpoints := openAICompatibleEndpointTypes(model.UpstreamName, endpointInferenceSiteTypeForProvider(provider), "")
	description := fmt.Sprintf("models.dev %s snapshot for %s.", provider, sourceID)
	if hasInput && hasOutput {
		description = fmt.Sprintf("models.dev %s snapshot for %s: input $%.2f/M, output $%.2f/M.", provider, sourceID, input, output)
	}
	raw := map[string]any{
		"model_name":               model.UpstreamName,
		"display_name":             firstNonEmptyString(model.DisplayName, source.Name, model.UpstreamName),
		"billing_type":             "tokens",
		"currency":                 "USD",
		"description":              description,
		"supported_endpoint_types": endpoints,
		"source":                   "models_dev",
		"source_url":               openAICompatibleModelsDevSourceURL,
		"source_provider":          provider,
		"source_model_id":          sourceID,
		"cost":                     source.Cost,
		"modalities":               source.Modalities,
		"limit":                    source.Limit,
	}
	pricing := ModelPricing{
		ModelName:   model.UpstreamName,
		DisplayName: firstNonEmptyString(model.DisplayName, source.Name, model.UpstreamName),
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

func openAICompatiblePricingFromModelsDevOnly(provider string, catalog antigravityModelsDevCatalog) PricingSnapshot {
	providerPayload := catalog[strings.ToLower(provider)]
	if providerPayload.Models == nil {
		return PricingSnapshot{}
	}

	items := make([]ModelPricing, 0, len(providerPayload.Models))
	rawItems := make([]map[string]any, 0, len(providerPayload.Models))
	for sourceID, source := range providerPayload.Models {
		model := Model{
			UpstreamName: sourceID,
			DisplayName:  firstNonEmptyString(source.Name, sourceID),
		}
		item := openAICompatibleModelPricingFromModelsDev(model, strings.ToLower(provider), sourceID, source)
		items = append(items, item)
		rawItems = append(rawItems, item.Raw)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ModelName < items[j].ModelName
	})
	sort.Slice(rawItems, func(i, j int) bool {
		return anyString(rawItems[i]["model_name"]) < anyString(rawItems[j]["model_name"])
	})
	if len(items) == 0 {
		return PricingSnapshot{}
	}
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
			"provider":   provider,
			"currency":   "USD",
			"group_name": "default",
			"items":      rawItems,
			"source":     "models_dev",
			"source_url": openAICompatibleModelsDevSourceURL,
		},
	}
}
