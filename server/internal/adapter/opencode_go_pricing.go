package adapter

import (
	"context"
	"fmt"
)

const (
	openCodeGoPricingSourceURL  = "https://opencode.ai/docs/go/"
	openCodeGoPricingSourceDate = "2026-08-03"
)

type openCodeGoPriceDefinition struct {
	ModelName          string
	DisplayName        string
	InputValue         float64
	OutputValue        float64
	CacheInputValue    float64
	CacheWriteValue    float64
	HasCacheWriteValue bool
	IncludedMonthlyUSD float64
	EndpointType       string
}

func (OpenCodeGo) FetchPricing(_ context.Context, _ SiteConfig, _ SystemAuth) (PricingSnapshot, error) {
	return openCodeGoPricingSnapshot(), nil
}

func openCodeGoPricingSnapshot() PricingSnapshot {
	definitions := openCodeGoPricingDefinitions()
	items := make([]ModelPricing, 0, len(definitions))
	rawItems := make([]map[string]any, 0, len(definitions))
	for _, item := range definitions {
		cacheRatio := item.CacheInputValue / item.InputValue
		raw := map[string]any{
			"model_name":               item.ModelName,
			"display_name":             item.DisplayName,
			"billing_type":             "tokens",
			"currency":                 "USD",
			"input_value":              item.InputValue,
			"output_value":             item.OutputValue,
			"cache_input_value":        item.CacheInputValue,
			"cache_ratio":              cacheRatio,
			"included_monthly_usd":     item.IncludedMonthlyUSD,
			"supported_endpoint_types": []string{item.EndpointType},
			"source":                   "opencode_go_official",
			"source_url":               openCodeGoPricingSourceURL,
			"source_date":              openCodeGoPricingSourceDate,
		}
		pricing := ModelPricing{
			ModelName:      item.ModelName,
			DisplayName:    item.DisplayName,
			GroupName:      "default",
			QuotaType:      0,
			BillingType:    "tokens",
			Currency:       "USD",
			GroupRatio:     1,
			InputValue:     item.InputValue,
			HasInputValue:  true,
			OutputValue:    item.OutputValue,
			HasOutputValue: true,
			CacheRatio:     cacheRatio,
			HasCacheRatio:  true,
			Description:    fmt.Sprintf("OpenCode Go official pricing per 1M tokens; monthly included usage $%.0f.", item.IncludedMonthlyUSD),
			Raw:            raw,
		}
		if item.HasCacheWriteValue {
			pricing.CreateCacheRatio = item.CacheWriteValue / item.InputValue
			pricing.HasCreateCacheRatio = true
			raw["cache_write_value"] = item.CacheWriteValue
			raw["create_cache_ratio"] = pricing.CreateCacheRatio
		}
		items = append(items, pricing)
		rawItems = append(rawItems, raw)
	}
	return PricingSnapshot{
		Groups: []PricingGroup{{
			GroupName:   "default",
			DisplayName: "OpenCode Go",
			Ratio:       1,
			IsAuto:      true,
			Raw: map[string]any{
				"group_name": "default",
				"ratio":      1,
				"source":     "opencode_go_official",
			},
		}},
		Items: items,
		Raw: map[string]any{
			"provider":    "opencode_go",
			"currency":    "USD",
			"group_name":  "default",
			"items":       rawItems,
			"source":      "opencode_go_official",
			"source_url":  openCodeGoPricingSourceURL,
			"source_date": openCodeGoPricingSourceDate,
		},
	}
}

func openCodeGoPricingDefinitions() []openCodeGoPriceDefinition {
	return []openCodeGoPriceDefinition{
		{ModelName: "grok-4.5", DisplayName: "Grok 4.5", InputValue: 2, OutputValue: 6, CacheInputValue: 0.30, IncludedMonthlyUSD: 15, EndpointType: "openai"},
		{ModelName: "gpt-5.6-luna", DisplayName: "GPT 5.6 Luna", InputValue: 0.20, OutputValue: 1.20, CacheInputValue: 0.02, CacheWriteValue: 0.25, HasCacheWriteValue: true, IncludedMonthlyUSD: 15, EndpointType: "openai-response"},
		{ModelName: "glm-5.2", DisplayName: "GLM-5.2", InputValue: 1.40, OutputValue: 4.40, CacheInputValue: 0.26, IncludedMonthlyUSD: 60, EndpointType: "openai"},
		{ModelName: "glm-5.1", DisplayName: "GLM-5.1", InputValue: 1.40, OutputValue: 4.40, CacheInputValue: 0.26, IncludedMonthlyUSD: 60, EndpointType: "openai"},
		{ModelName: "kimi-k3", DisplayName: "Kimi K3", InputValue: 3, OutputValue: 15, CacheInputValue: 0.30, IncludedMonthlyUSD: 15, EndpointType: "openai"},
		{ModelName: "kimi-k2.7-code", DisplayName: "Kimi K2.7 Code", InputValue: 0.95, OutputValue: 4, CacheInputValue: 0.19, IncludedMonthlyUSD: 60, EndpointType: "openai"},
		{ModelName: "kimi-k2.6", DisplayName: "Kimi K2.6", InputValue: 0.95, OutputValue: 4, CacheInputValue: 0.16, IncludedMonthlyUSD: 60, EndpointType: "openai"},
		{ModelName: "mimo-v2.5", DisplayName: "MiMo V2.5", InputValue: 0.14, OutputValue: 0.28, CacheInputValue: 0.0028, IncludedMonthlyUSD: 60, EndpointType: "openai"},
		{ModelName: "mimo-v2.5-pro", DisplayName: "MiMo V2.5 Pro", InputValue: 0.435, OutputValue: 0.87, CacheInputValue: 0.003625, IncludedMonthlyUSD: 15, EndpointType: "openai"},
		{ModelName: "minimax-m3", DisplayName: "MiniMax M3", InputValue: 0.30, OutputValue: 1.20, CacheInputValue: 0.06, IncludedMonthlyUSD: 60, EndpointType: "anthropic-messages"},
		{ModelName: "minimax-m2.7", DisplayName: "MiniMax M2.7", InputValue: 0.30, OutputValue: 1.20, CacheInputValue: 0.06, CacheWriteValue: 0.375, HasCacheWriteValue: true, IncludedMonthlyUSD: 60, EndpointType: "anthropic-messages"},
		{ModelName: "minimax-m2.5", DisplayName: "MiniMax M2.5", InputValue: 0.30, OutputValue: 1.20, CacheInputValue: 0.06, CacheWriteValue: 0.375, HasCacheWriteValue: true, IncludedMonthlyUSD: 60, EndpointType: "anthropic-messages"},
		{ModelName: "qwen3.7-max", DisplayName: "Qwen3.7 Max", InputValue: 2.50, OutputValue: 7.50, CacheInputValue: 0.50, CacheWriteValue: 3.125, HasCacheWriteValue: true, IncludedMonthlyUSD: 60, EndpointType: "anthropic-messages"},
		{ModelName: "qwen3.7-plus", DisplayName: "Qwen3.7 Plus", InputValue: 0.40, OutputValue: 1.60, CacheInputValue: 0.04, CacheWriteValue: 0.50, HasCacheWriteValue: true, IncludedMonthlyUSD: 60, EndpointType: "anthropic-messages"},
		{ModelName: "qwen3.6-plus", DisplayName: "Qwen3.6 Plus", InputValue: 0.50, OutputValue: 3, CacheInputValue: 0.05, CacheWriteValue: 0.625, HasCacheWriteValue: true, IncludedMonthlyUSD: 60, EndpointType: "anthropic-messages"},
		{ModelName: "deepseek-v4-pro", DisplayName: "DeepSeek V4 Pro", InputValue: 0.435, OutputValue: 0.87, CacheInputValue: 0.003625, IncludedMonthlyUSD: 15, EndpointType: "openai"},
		{ModelName: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", InputValue: 0.14, OutputValue: 0.28, CacheInputValue: 0.0028, IncludedMonthlyUSD: 60, EndpointType: "openai"},
		{ModelName: "hy3", DisplayName: "Hy3", InputValue: 0.14, OutputValue: 0.58, CacheInputValue: 0.035, IncludedMonthlyUSD: 60, EndpointType: "openai"},
	}
}
