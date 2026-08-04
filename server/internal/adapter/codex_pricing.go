package adapter

import (
	"context"
	"strings"
)

const codexPricingSourceURL = "https://openai.com/api/pricing/"

type codexPriceDefinition struct {
	ModelName           string
	DisplayName         string
	InputValue          float64
	HasInputValue       bool
	OutputValue         float64
	HasOutputValue      bool
	CacheRatio          float64
	HasCacheRatio       bool
	CreateCacheRatio    float64
	HasCreateCacheRatio bool
	ImageRatio          float64
	HasImageRatio       bool
	BillingType         string
	Description         string
	EndpointTypes       []any
}

func (a Codex) FetchPricing(_ context.Context, _ SiteConfig, auth SystemAuth) (PricingSnapshot, error) {
	return codexPricingSnapshot(stringFromMap(auth.Metadata, "plan_type")), nil
}

func (a Codex) ParsePricing(payload any) PricingSnapshot {
	root, ok := payload.(map[string]any)
	if !ok {
		return codexPricingSnapshot("")
	}
	return codexPricingSnapshot(anyString(root["plan_type"]))
}

func codexPricingPayload(planType string) map[string]any {
	items := make([]map[string]any, 0, len(codexPricingDefinitions()))
	for _, item := range codexPricingDefinitions() {
		payload := map[string]any{
			"model_name":               item.ModelName,
			"display_name":             item.DisplayName,
			"billing_type":             defaultString(item.BillingType, "tokens"),
			"currency":                 "USD",
			"description":              item.Description,
			"supported_endpoint_types": item.EndpointTypes,
			"source":                   "openai_official",
			"source_url":               codexPricingSourceURL,
		}
		if item.HasInputValue {
			payload["input_value"] = item.InputValue
		}
		if item.HasOutputValue {
			payload["output_value"] = item.OutputValue
		}
		if item.HasCacheRatio {
			payload["cache_ratio"] = item.CacheRatio
			payload["cache_input_value"] = item.InputValue * item.CacheRatio
		}
		if item.HasCreateCacheRatio {
			payload["create_cache_ratio"] = item.CreateCacheRatio
			payload["create_cache_input_value"] = item.InputValue * item.CreateCacheRatio
		}
		if item.HasImageRatio {
			payload["image_ratio"] = item.ImageRatio
			payload["image_input_value"] = item.InputValue * item.ImageRatio
		}
		items = append(items, payload)
	}
	return map[string]any{
		"provider":   "codex",
		"plan_type":  strings.TrimSpace(planType),
		"currency":   "USD",
		"group_name": "default",
		"items":      items,
		"source":     "openai_official",
		"source_url": codexPricingSourceURL,
	}
}

func codexPricingSnapshot(planType string) PricingSnapshot {
	definitions := codexPricingDefinitions()
	items := make([]ModelPricing, 0, len(definitions))
	for _, item := range definitions {
		raw := map[string]any{
			"model_name":               item.ModelName,
			"display_name":             item.DisplayName,
			"billing_type":             defaultString(item.BillingType, "tokens"),
			"currency":                 "USD",
			"description":              item.Description,
			"supported_endpoint_types": item.EndpointTypes,
			"source":                   "openai_official",
			"source_url":               codexPricingSourceURL,
		}
		if item.HasInputValue {
			raw["input_value"] = item.InputValue
		}
		if item.HasOutputValue {
			raw["output_value"] = item.OutputValue
		}
		if item.HasCacheRatio {
			raw["cache_ratio"] = item.CacheRatio
		}
		if item.HasCreateCacheRatio {
			raw["create_cache_ratio"] = item.CreateCacheRatio
		}
		if item.HasImageRatio {
			raw["image_ratio"] = item.ImageRatio
		}

		pricing := ModelPricing{
			ModelName:   item.ModelName,
			DisplayName: item.DisplayName,
			GroupName:   "default",
			QuotaType:   0,
			BillingType: defaultString(item.BillingType, "tokens"),
			Currency:    "USD",
			GroupRatio:  1,
			Description: item.Description,
			Raw:         raw,
		}
		if item.HasInputValue {
			pricing.InputValue = item.InputValue
			pricing.HasInputValue = true
		}
		if item.HasOutputValue {
			pricing.OutputValue = item.OutputValue
			pricing.HasOutputValue = true
		}
		if item.HasCacheRatio {
			pricing.CacheRatio = item.CacheRatio
			pricing.HasCacheRatio = true
		}
		if item.HasCreateCacheRatio {
			pricing.CreateCacheRatio = item.CreateCacheRatio
			pricing.HasCreateCacheRatio = true
		}
		if item.HasImageRatio {
			pricing.ImageRatio = item.ImageRatio
			pricing.HasImageRatio = true
		}
		items = append(items, pricing)
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
			},
		}},
		Items: items,
		Raw:   codexPricingPayload(planType),
	}
}

func codexPricingDefinitions() []codexPriceDefinition {
	items := []codexPriceDefinition{
		{
			ModelName:           "gpt-5.6-sol",
			DisplayName:         "GPT-5.6-Sol",
			InputValue:          5.00,
			HasInputValue:       true,
			OutputValue:         30.00,
			HasOutputValue:      true,
			CacheRatio:          0.1,
			HasCacheRatio:       true,
			CreateCacheRatio:    1.25,
			HasCreateCacheRatio: true,
			BillingType:         "tokens",
			Description:         "OpenAI preview pricing for GPT-5.6 Sol (subject to change at GA): input $5.00/M, cached input $0.50/M, cache write $6.25/M, output $30.00/M.",
			EndpointTypes:       []any{"openai", "openai-response"},
		},
		{
			ModelName:           "gpt-5.6-terra",
			DisplayName:         "GPT-5.6-Terra",
			InputValue:          2.50,
			HasInputValue:       true,
			OutputValue:         15.00,
			HasOutputValue:      true,
			CacheRatio:          0.1,
			HasCacheRatio:       true,
			CreateCacheRatio:    1.25,
			HasCreateCacheRatio: true,
			BillingType:         "tokens",
			Description:         "OpenAI preview pricing for GPT-5.6 Terra (subject to change at GA): input $2.50/M, cached input $0.25/M, cache write $3.125/M, output $15.00/M.",
			EndpointTypes:       []any{"openai", "openai-response"},
		},
		{
			ModelName:           "gpt-5.6-luna",
			DisplayName:         "GPT-5.6-Luna",
			InputValue:          1.00,
			HasInputValue:       true,
			OutputValue:         6.00,
			HasOutputValue:      true,
			CacheRatio:          0.1,
			HasCacheRatio:       true,
			CreateCacheRatio:    1.25,
			HasCreateCacheRatio: true,
			BillingType:         "tokens",
			Description:         "OpenAI preview pricing for GPT-5.6 Luna (subject to change at GA): input $1.00/M, cached input $0.10/M, cache write $1.25/M, output $6.00/M.",
			EndpointTypes:       []any{"openai", "openai-response"},
		},
		{
			ModelName:      "gpt-5.5",
			DisplayName:    "GPT 5.5",
			InputValue:     5.00,
			HasInputValue:  true,
			OutputValue:    30.00,
			HasOutputValue: true,
			CacheRatio:     0.1,
			HasCacheRatio:  true,
			BillingType:    "tokens",
			Description:    "models.dev OpenAI snapshot for GPT-5.5 in Codex: input $5.00/M, cached input $0.50/M, output $30.00/M.",
			EndpointTypes:  []any{"openai", "openai-response"},
		},
		{
			ModelName:      "gpt-5.2",
			DisplayName:    "GPT 5.2",
			InputValue:     1.75,
			HasInputValue:  true,
			OutputValue:    14.00,
			HasOutputValue: true,
			CacheRatio:     0.1,
			HasCacheRatio:  true,
			BillingType:    "tokens",
			Description:    "OpenAI official baseline price for GPT-5.2 used in Codex.",
			EndpointTypes:  []any{"openai-response"},
		},
		{
			ModelName:      "gpt-5.2-codex",
			DisplayName:    "GPT 5.2 Codex",
			InputValue:     1.75,
			HasInputValue:  true,
			OutputValue:    14.00,
			HasOutputValue: true,
			CacheRatio:     0.1,
			HasCacheRatio:  true,
			BillingType:    "tokens",
			Description:    "OpenAI official baseline price for GPT-5.2-Codex.",
			EndpointTypes:  []any{"openai-response"},
		},
		{
			ModelName:      "gpt-5.3-codex",
			DisplayName:    "GPT 5.3 Codex",
			InputValue:     1.75,
			HasInputValue:  true,
			OutputValue:    14.00,
			HasOutputValue: true,
			CacheRatio:     0.1,
			HasCacheRatio:  true,
			BillingType:    "tokens",
			Description:    "OpenAI official baseline price for GPT-5.3-Codex.",
			EndpointTypes:  []any{"openai-response"},
		},
		{
			ModelName:      "gpt-5.4",
			DisplayName:    "GPT 5.4",
			InputValue:     2.50,
			HasInputValue:  true,
			OutputValue:    15.00,
			HasOutputValue: true,
			CacheRatio:     0.1,
			HasCacheRatio:  true,
			BillingType:    "tokens",
			Description:    "OpenAI official baseline price for GPT-5.4. Long-context surcharge is not folded into this fixed baseline.",
			EndpointTypes:  []any{"openai-response"},
		},
		{
			ModelName:      "gpt-5.4-mini",
			DisplayName:    "GPT 5.4 Mini",
			InputValue:     0.75,
			HasInputValue:  true,
			OutputValue:    4.50,
			HasOutputValue: true,
			CacheRatio:     0.1,
			HasCacheRatio:  true,
			BillingType:    "tokens",
			Description:    "OpenAI official baseline price for GPT-5.4 mini.",
			EndpointTypes:  []any{"openai-response"},
		},
		{
			ModelName:      "gpt-image-2",
			DisplayName:    "GPT Image 2",
			InputValue:     5.00,
			HasInputValue:  true,
			OutputValue:    30.00,
			HasOutputValue: true,
			CacheRatio:     0.25,
			HasCacheRatio:  true,
			ImageRatio:     1.6,
			HasImageRatio:  true,
			BillingType:    "tokens",
			Description:    "OpenAI official GPT-image-2 baseline: text input $5.00/M, cached text input $1.25/M, image input $8.00/M, image output $30.00/M.",
			EndpointTypes:  []any{"openai-response", "openai-image"},
		},
	}
	items = append(items, codexPriceDefinition{
		ModelName:     "gpt-5.3-codex-spark",
		DisplayName:   "GPT 5.3 Codex Spark",
		BillingType:   "tokens",
		Description:   "Research preview in Codex. OpenAI notes that its credit rates are not final, so xLyra only syncs protocol metadata for now.",
		EndpointTypes: []any{"openai-response"},
	})
	return items
}
