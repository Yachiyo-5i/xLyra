package gateway

import (
	"context"
	"encoding/json"
	"strings"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

type selectedPricing struct {
	GroupName            string
	Currency             string
	InputValue           *float64
	OutputValue          *float64
	CacheRatio           *float64
	CacheWriteRatio      *float64
	CacheWrite1hRatio    *float64
	GroupRatio           *float64
	ModelRatio           *float64
	CompletionRatio      *float64
	ImageRatio           *float64
	AudioRatio           *float64
	AudioCompletionRatio *float64
	ModelPrice           *float64
	PerRequestValue      *float64
	BillingType          string
	QuotaType            *int64
	LongContextRule      *longContextRule
	LongContextApplied   bool
}

type billingAdjustment struct {
	ServiceTier string
	Mode        string
	Multiplier  float64
	Reason      string
}

type gatewayUsage struct {
	PromptTokens               int                  `json:"prompt_tokens"`
	CompletionTokens           int                  `json:"completion_tokens"`
	TotalTokens                int                  `json:"total_tokens"`
	ImageCount                 int                  `json:"image_count,omitempty"`
	CachedPromptTokens         int                  `json:"cached_tokens,omitempty"`
	PromptCacheHitTokens       int                  `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens      int                  `json:"prompt_cache_miss_tokens,omitempty"`
	CacheWriteTokens           int                  `json:"cache_write_tokens,omitempty"`
	CacheCreationInputTokens   int                  `json:"cache_creation_input_tokens,omitempty"`
	CacheCreation5mInputTokens int                  `json:"cache_creation_5m_input_tokens,omitempty"`
	CacheCreation1hInputTokens int                  `json:"cache_creation_1h_input_tokens,omitempty"`
	ReasoningTokens            int                  `json:"reasoning_tokens,omitempty"`
	InputTextTokens            int                  `json:"input_text_tokens,omitempty"`
	InputImageTokens           int                  `json:"input_image_tokens,omitempty"`
	OutputImageTokens          int                  `json:"output_image_tokens,omitempty"`
	AudioOutputTokens          int                  `json:"audio_output_tokens,omitempty"`
	PromptTokensDetail         *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type completionUsage = gatewayUsage

type promptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

func (h Handler) gatewayPricing(ctx context.Context, candidate routeengine.Candidate, groupName string) selectedPricing {
	fallback := selectedPricing{
		GroupName:       stringValue(candidate.Pricing.GroupName, ""),
		Currency:        stringValue(candidate.Pricing.Currency, "USD"),
		InputValue:      firstPricingValue(candidate.Pricing.BaseInputValue, candidate.Pricing.InputValue),
		OutputValue:     firstPricingValue(candidate.Pricing.BaseOutputValue, candidate.Pricing.OutputValue),
		PerRequestValue: firstPricingValue(candidate.Pricing.BasePerRequestValue, candidate.Pricing.PerRequestValue),
	}
	if candidate.Pricing.BillingType != nil {
		fallback.BillingType = *candidate.Pricing.BillingType
	}
	if candidate.Pricing.QuotaType != nil {
		fallback.QuotaType = candidate.Pricing.QuotaType
	}
	fallback.LongContextRule = longContextRuleForModel(candidate.Model.UpstreamName)

	pricing, err := store.NewGatewayRepository(h.db.DB()).GetPricingForSiteModel(ctx, candidate.Model.SiteModelID, strings.TrimSpace(groupName))
	if err != nil {
		return fallback
	}
	if pricing.GroupName.Valid {
		fallback.GroupName = pricing.GroupName.String
	}
	if pricing.Currency.Valid && strings.TrimSpace(pricing.Currency.String) != "" {
		fallback.Currency = strings.TrimSpace(pricing.Currency.String)
	}
	if pricing.InputValue.Valid {
		value := pricing.InputValue.Float64
		fallback.InputValue = &value
	}
	if pricing.OutputValue.Valid {
		value := pricing.OutputValue.Float64
		fallback.OutputValue = &value
	}
	if pricing.CacheRatio.Valid {
		value := pricing.CacheRatio.Float64
		fallback.CacheRatio = &value
	}
	if pricing.CreateCacheRatio.Valid {
		value := pricing.CreateCacheRatio.Float64
		fallback.CacheWriteRatio = &value
	}
	if pricing.CreateCache1hRatio.Valid {
		value := pricing.CreateCache1hRatio.Float64
		fallback.CacheWrite1hRatio = &value
	}
	if pricing.GroupRatio.Valid {
		value := pricing.GroupRatio.Float64
		fallback.GroupRatio = &value
	}
	if pricing.ModelRatio.Valid {
		value := pricing.ModelRatio.Float64
		fallback.ModelRatio = &value
	}
	if pricing.CompletionRatio.Valid {
		value := pricing.CompletionRatio.Float64
		fallback.CompletionRatio = &value
	}
	if pricing.ImageRatio.Valid {
		value := pricing.ImageRatio.Float64
		fallback.ImageRatio = &value
	}
	if pricing.AudioRatio.Valid {
		value := pricing.AudioRatio.Float64
		fallback.AudioRatio = &value
	}
	if pricing.AudioCompletionRatio.Valid {
		value := pricing.AudioCompletionRatio.Float64
		fallback.AudioCompletionRatio = &value
	}
	if pricing.ModelPrice.Valid {
		value := pricing.ModelPrice.Float64
		fallback.ModelPrice = &value
	}
	if pricing.PerRequestValue.Valid {
		value := pricing.PerRequestValue.Float64
		fallback.PerRequestValue = &value
	}
	if pricing.BillingType.Valid {
		fallback.BillingType = pricing.BillingType.String
	}
	if pricing.QuotaType.Valid {
		value := pricing.QuotaType.Int64
		fallback.QuotaType = &value
	}
	return fallback
}

func parseCompletionUsage(body []byte) gatewayUsage {
	var envelope struct {
		Usage gatewayUsage `json:"usage"`
	}
	_ = json.Unmarshal(body, &envelope)
	return envelope.Usage.normalized()
}

func estimateCost(usage gatewayUsage, pricing selectedPricing) *float64 {
	usage = usage.normalized()
	if isPerRequestPricing(pricing) {
		if value, ok := perRequestCost(pricing); ok {
			return &value
		}
		return nil
	}

	if usage.ImageCount > 0 {
		if total, ok := imageTokenCost(usage, pricing); ok {
			return &total
		}
		value, ok := imageUnitCost(pricing)
		if ok {
			total := float64(usage.ImageCount) * value
			return &total
		}
		return nil
	}

	var total float64
	usedPrice := false
	uncachedPromptTokens := usage.uncachedPromptTokens()
	if pricing.InputValue != nil && uncachedPromptTokens > 0 {
		total += float64(uncachedPromptTokens) * *pricing.InputValue / 1_000_000
		usedPrice = true
	}
	if pricing.InputValue != nil && pricing.CacheRatio != nil && usage.CachedPromptTokens > 0 {
		total += float64(usage.CachedPromptTokens) * *pricing.InputValue * *pricing.CacheRatio / 1_000_000
		usedPrice = true
	}
	if pricing.InputValue != nil {
		cacheWriteCost, hasCacheWriteCost := cacheWriteTokenCost(usage, pricing)
		if hasCacheWriteCost {
			total += cacheWriteCost
			usedPrice = true
		}
	}
	if pricing.OutputValue != nil && usage.CompletionTokens > 0 {
		total += float64(usage.CompletionTokens) * *pricing.OutputValue / 1_000_000
		usedPrice = true
	}
	if audioOutputValue, ok := audioOutputUnitValue(pricing); ok && usage.AudioOutputTokens > 0 {
		total += float64(usage.AudioOutputTokens) * audioOutputValue / 1_000_000
		usedPrice = true
	}
	if !usedPrice {
		return nil
	}
	return &total
}

func audioOutputUnitValue(pricing selectedPricing) (float64, bool) {
	if pricing.InputValue == nil || pricing.AudioRatio == nil {
		return 0, false
	}
	value := *pricing.InputValue * *pricing.AudioRatio
	if pricing.AudioCompletionRatio != nil {
		value *= *pricing.AudioCompletionRatio
	}
	return value, true
}

func applyUpstreamBillingMetadata(result gatewayAttemptResult, payload map[string]any, candidate routeengine.Candidate) gatewayAttemptResult {
	adjustment := billingAdjustmentFromPayload(payload, candidate)
	result.serviceTier = adjustment.ServiceTier
	result.billingMode = adjustment.Mode
	result.costMultiplier = adjustment.Multiplier
	result.multiplierReason = adjustment.Reason
	return result
}

func applyRequestBillingMetadata(result gatewayAttemptResult, upstreamPayload, requestPayload map[string]any, candidate routeengine.Candidate) gatewayAttemptResult {
	result = applyUpstreamBillingMetadata(result, upstreamPayload, candidate)
	requested := billingAdjustmentFromPayload(requestPayload, candidate)
	if result.billingMode == "" && requested.Mode == "standard" {
		result.serviceTier = requested.ServiceTier
		result.billingMode = requested.Mode
		result.costMultiplier = requested.Multiplier
		result.multiplierReason = requested.Reason
	}
	return result
}

func billingAdjustmentFromPayload(payload map[string]any, candidate routeengine.Candidate) billingAdjustment {
	serviceTier := strings.TrimSpace(strings.ToLower(anyString(payload["service_tier"])))
	adjustment := billingAdjustment{ServiceTier: serviceTier, Multiplier: 1}
	if serviceTier == "standard" {
		adjustment.Mode = "standard"
		return adjustment
	}
	if !isFastServiceTier(serviceTier) {
		return adjustment
	}

	adjustment.Mode = "fast"
	model := strings.TrimSpace(anyString(payload["model"]))
	if model == "" {
		model = candidate.Model.UpstreamName
	}
	if multiplier, ok := codexFastCostMultiplier(model); ok {
		adjustment.Multiplier = multiplier
		adjustment.Reason = "codex_fast_mode"
	}
	return adjustment
}

func isFastServiceTier(serviceTier string) bool {
	switch strings.TrimSpace(strings.ToLower(serviceTier)) {
	case "fast", "priority":
		return true
	default:
		return false
	}
}

func codexFastCostMultiplier(model string) (float64, bool) {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return 1, false
	}
	if strings.Contains(normalized, "gpt-5.5") {
		return 2.5, true
	}
	if strings.Contains(normalized, "gpt-5.4-mini") {
		return 1, false
	}
	if strings.Contains(normalized, "gpt-5.4") {
		return 2, true
	}
	return 1, false
}

func applyEstimatedCostBillingAdjustment(result gatewayAttemptResult) gatewayAttemptResult {
	if result.estimatedCost == nil {
		return result
	}
	baseCost := *result.estimatedCost
	result.baseEstimatedCost = &baseCost
	multiplier := gatewayBillingMultiplier(result)
	if multiplier == 1 {
		return result
	}
	finalCost := baseCost * multiplier
	result.estimatedCost = &finalCost
	return result
}

func gatewayBillingMultiplier(result gatewayAttemptResult) float64 {
	credentialMultiplier := result.credentialCostMultiplier
	if credentialMultiplier <= 0 {
		credentialMultiplier = 1
	}
	serviceMultiplier := 1.0
	if result.billingMode == "fast" && result.costMultiplier > 1 {
		serviceMultiplier = result.costMultiplier
	}
	return credentialMultiplier * serviceMultiplier
}

func firstPricingValue(primary *float64, fallback *float64) *float64 {
	if primary != nil {
		return primary
	}
	return fallback
}

func pricingMetadata(pricing selectedPricing) map[string]any {
	return map[string]any{
		"group_name":                 emptyToNil(pricing.GroupName),
		"currency":                   emptyToNil(pricing.Currency),
		"input_value":                float64PtrValue(pricing.InputValue),
		"output_value":               float64PtrValue(pricing.OutputValue),
		"cache_ratio":                float64PtrValue(pricing.CacheRatio),
		"cache_input_value":          scaledFloat64Value(pricing.InputValue, pricing.CacheRatio),
		"cache_write_ratio":          float64PtrValue(pricing.CacheWriteRatio),
		"cache_write_input_value":    scaledFloat64Value(pricing.InputValue, pricing.CacheWriteRatio),
		"cache_write_1h_ratio":       float64PtrValue(pricing.CacheWrite1hRatio),
		"cache_write_1h_input_value": scaledFloat64Value(pricing.InputValue, pricing.CacheWrite1hRatio),
		"group_ratio":                float64PtrValue(pricing.GroupRatio),
		"model_ratio":                float64PtrValue(pricing.ModelRatio),
		"completion_ratio":           float64PtrValue(pricing.CompletionRatio),
		"image_ratio":                float64PtrValue(pricing.ImageRatio),
		"model_price":                float64PtrValue(pricing.ModelPrice),
		"per_request_value":          float64PtrValue(pricing.PerRequestValue),
		"billing_type":               emptyToNil(pricing.BillingType),
		"quota_type":                 int64PtrValue(pricing.QuotaType),
	}
}

func costCalculationMetadata(usage gatewayUsage, pricing selectedPricing, total *float64, adjustment billingAdjustment, baseTotal *float64) map[string]any {
	usage = usage.normalized()
	if isPerRequestPricing(pricing) {
		var perRequestCostValue any
		if value, ok := perRequestCost(pricing); ok {
			perRequestCostValue = value
		}
		return withBillingAdjustmentMetadata(map[string]any{
			"formula":           "per_request_value",
			"billing_type":      emptyToNil(pricing.BillingType),
			"quota_type":        int64PtrValue(pricing.QuotaType),
			"model_price":       float64PtrValue(pricing.ModelPrice),
			"group_ratio":       float64PtrValue(pricing.GroupRatio),
			"per_request_value": float64PtrValue(pricing.PerRequestValue),
			"per_request_cost":  perRequestCostValue,
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"image_count":       usage.ImageCount,
			"estimated_cost":    float64PtrValue(total),
			"currency":          emptyToNil(pricing.Currency),
		}, adjustment, baseTotal)
	}

	if usage.ImageCount > 0 {
		if _, ok := imageTokenCost(usage, pricing); ok {
			return withBillingAdjustmentMetadata(map[string]any{
				"formula":             "input_text_tokens * input_value / 1000000 + input_image_tokens * image_input_value / 1000000 + output_image_tokens * output_value / 1000000",
				"image_count":         usage.ImageCount,
				"input_text_tokens":   usage.InputTextTokens,
				"input_image_tokens":  usage.InputImageTokens,
				"output_image_tokens": usage.outputImageTokensOrCompletion(),
				"input_value":         float64PtrValue(pricing.InputValue),
				"output_value":        float64PtrValue(pricing.OutputValue),
				"image_ratio":         float64PtrValue(pricing.ImageRatio),
				"image_input_value":   scaledFloat64Value(pricing.InputValue, pricing.ImageRatio),
				"group_ratio":         float64PtrValue(pricing.GroupRatio),
				"estimated_cost":      float64PtrValue(total),
				"currency":            emptyToNil(pricing.Currency),
			}, adjustment, baseTotal)
		}
		var imageUnitValue any
		if value, ok := imageUnitCost(pricing); ok {
			imageUnitValue = value
		}
		return withBillingAdjustmentMetadata(map[string]any{
			"formula":         "image_count * input_value * image_ratio",
			"image_count":     usage.ImageCount,
			"input_value":     float64PtrValue(pricing.InputValue),
			"image_ratio":     float64PtrValue(pricing.ImageRatio),
			"image_unit_cost": imageUnitValue,
			"group_ratio":     float64PtrValue(pricing.GroupRatio),
			"estimated_cost":  float64PtrValue(total),
			"currency":        emptyToNil(pricing.Currency),
		}, adjustment, baseTotal)
	}

	var promptCost any
	if pricing.InputValue != nil && usage.uncachedPromptTokens() > 0 {
		promptCost = float64(usage.uncachedPromptTokens()) * *pricing.InputValue / 1_000_000
	}

	var completionCost any
	if pricing.OutputValue != nil && usage.CompletionTokens > 0 {
		completionCost = float64(usage.CompletionTokens) * *pricing.OutputValue / 1_000_000
	}

	var cacheInputValue any
	if pricing.InputValue != nil && pricing.CacheRatio != nil {
		cacheInputValue = *pricing.InputValue * *pricing.CacheRatio
	}

	var cacheCost any
	if pricing.InputValue != nil && pricing.CacheRatio != nil && usage.CachedPromptTokens > 0 {
		cacheCost = float64(usage.CachedPromptTokens) * *pricing.InputValue * *pricing.CacheRatio / 1_000_000
	}

	var cacheWriteInputValue any
	if pricing.InputValue != nil && pricing.CacheWriteRatio != nil {
		cacheWriteInputValue = *pricing.InputValue * *pricing.CacheWriteRatio
	}

	var cacheWrite1hInputValue any
	if pricing.InputValue != nil && pricing.CacheWrite1hRatio != nil {
		cacheWrite1hInputValue = *pricing.InputValue * *pricing.CacheWrite1hRatio
	}

	var cacheWriteCost any
	if cost, ok := cacheWriteTokenCost(usage, pricing); ok {
		cacheWriteCost = cost
	}

	var cacheWrite5mCost any
	if pricing.InputValue != nil && usage.CacheCreation5mInputTokens > 0 {
		cacheWrite5mCost = float64(usage.CacheCreation5mInputTokens) * *pricing.InputValue * cacheWriteRatioValue(pricing.CacheWriteRatio) / 1_000_000
	}

	var cacheWrite1hCost any
	if pricing.InputValue != nil && usage.CacheCreation1hInputTokens > 0 {
		ratio := pricing.CacheWrite1hRatio
		if ratio == nil {
			ratio = pricing.CacheWriteRatio
		}
		cacheWrite1hCost = float64(usage.CacheCreation1hInputTokens) * *pricing.InputValue * cacheWriteRatioValue(ratio) / 1_000_000
	}

	var audioOutputValue any
	var audioOutputCost any
	if value, ok := audioOutputUnitValue(pricing); ok {
		audioOutputValue = value
		if usage.AudioOutputTokens > 0 {
			audioOutputCost = float64(usage.AudioOutputTokens) * value / 1_000_000
		}
	}

	meta := withBillingAdjustmentMetadata(map[string]any{
		"formula":                    "prompt_tokens * input_value / 1000000 + cache_tokens * cache_input_value / 1000000 + cache_creation_5m_tokens * cache_write_input_value / 1000000 + cache_creation_1h_tokens * cache_write_1h_input_value / 1000000 + completion_tokens * output_value / 1000000 + audio_output_tokens * audio_output_value / 1000000",
		"input_tokens":               usage.PromptTokens,
		"prompt_tokens":              usage.uncachedPromptTokens(),
		"completion_tokens":          usage.CompletionTokens,
		"cache_tokens":               usage.CachedPromptTokens,
		"cached_tokens":              usage.CachedPromptTokens,
		"cache_write_tokens":         usage.CacheWriteTokens,
		"cache_creation_tokens":      usage.CacheCreationInputTokens,
		"cache_creation_5m_tokens":   usage.CacheCreation5mInputTokens,
		"cache_creation_1h_tokens":   usage.CacheCreation1hInputTokens,
		"input_value":                float64PtrValue(pricing.InputValue),
		"output_value":               float64PtrValue(pricing.OutputValue),
		"cache_ratio":                float64PtrValue(pricing.CacheRatio),
		"cache_input_value":          cacheInputValue,
		"cache_write_ratio":          float64PtrValue(pricing.CacheWriteRatio),
		"cache_write_input_value":    cacheWriteInputValue,
		"cache_write_1h_ratio":       float64PtrValue(pricing.CacheWrite1hRatio),
		"cache_write_1h_input_value": cacheWrite1hInputValue,
		"group_ratio":                float64PtrValue(pricing.GroupRatio),
		"prompt_cost":                promptCost,
		"completion_cost":            completionCost,
		"cache_cost":                 cacheCost,
		"cache_input_cost":           cacheCost,
		"cached_cost":                cacheCost,
		"cache_write_cost":           cacheWriteCost,
		"cache_write_5m_cost":        cacheWrite5mCost,
		"cache_write_1h_cost":        cacheWrite1hCost,
		"audio_output_tokens":        zeroIntToNil(usage.AudioOutputTokens),
		"audio_ratio":                float64PtrValue(pricing.AudioRatio),
		"audio_completion_ratio":     float64PtrValue(pricing.AudioCompletionRatio),
		"audio_output_value":         audioOutputValue,
		"audio_output_cost":          audioOutputCost,
		"estimated_cost":             float64PtrValue(total),
		"currency":                   emptyToNil(pricing.Currency),
	}, adjustment, baseTotal)
	return withLongContextMetadata(meta, pricing)
}

func withLongContextMetadata(meta map[string]any, pricing selectedPricing) map[string]any {
	if pricing.LongContextRule == nil {
		return meta
	}
	meta["long_context"] = pricing.LongContextApplied
	if pricing.LongContextApplied {
		meta["long_context_threshold_tokens"] = pricing.LongContextRule.ThresholdTokens
		meta["long_context_input_multiplier"] = pricing.LongContextRule.InputMultiplier
		meta["long_context_output_multiplier"] = pricing.LongContextRule.OutputMultiplier
	}
	return meta
}

func withBillingAdjustmentMetadata(meta map[string]any, adjustment billingAdjustment, baseTotal *float64) map[string]any {
	if strings.TrimSpace(adjustment.ServiceTier) != "" {
		meta["service_tier"] = adjustment.ServiceTier
	}
	if strings.TrimSpace(adjustment.Mode) != "" {
		meta["billing_mode"] = adjustment.Mode
	}
	if adjustment.Multiplier > 1 {
		meta["base_estimated_cost"] = float64PtrValue(baseTotal)
		meta["cost_multiplier"] = adjustment.Multiplier
		meta["cost_multiplier_reason"] = emptyToNil(adjustment.Reason)
	}
	return meta
}

func cacheWriteTokenCost(usage gatewayUsage, pricing selectedPricing) (float64, bool) {
	if pricing.InputValue == nil {
		return 0, false
	}
	usage = usage.normalized()
	var total float64
	usedPrice := false
	// OpenAI reports a flat cache_write_tokens count (no 5m/1h TTL breakdown),
	// billed at the model's cache-write ratio like the uncached input rate.
	if usage.CacheWriteTokens > 0 {
		total += float64(usage.CacheWriteTokens) * *pricing.InputValue * cacheWriteRatioValue(pricing.CacheWriteRatio) / 1_000_000
		usedPrice = true
	}
	if usage.hasCacheCreationBreakdown() {
		if usage.CacheCreation5mInputTokens > 0 {
			total += float64(usage.CacheCreation5mInputTokens) * *pricing.InputValue * cacheWriteRatioValue(pricing.CacheWriteRatio) / 1_000_000
			usedPrice = true
		}
		if usage.CacheCreation1hInputTokens > 0 {
			ratio := pricing.CacheWrite1hRatio
			if ratio == nil {
				ratio = pricing.CacheWriteRatio
			}
			total += float64(usage.CacheCreation1hInputTokens) * *pricing.InputValue * cacheWriteRatioValue(ratio) / 1_000_000
			usedPrice = true
		}
		return total, usedPrice
	}
	if usage.CacheCreationInputTokens > 0 {
		total += float64(usage.CacheCreationInputTokens) * *pricing.InputValue * cacheWriteRatioValue(pricing.CacheWriteRatio) / 1_000_000
		usedPrice = true
	}
	return total, usedPrice
}

func cacheWriteCostForAttempt(usage gatewayUsage, result gatewayAttemptResult) *float64 {
	cost, ok := cacheWriteTokenCost(usage, result.pricing)
	if !ok {
		return nil
	}
	cost *= gatewayBillingMultiplier(result)
	return &cost
}

func cacheWriteRatioValue(ratio *float64) float64 {
	if ratio == nil {
		return 1
	}
	return *ratio
}

func imageUnitCost(pricing selectedPricing) (float64, bool) {
	if pricing.InputValue == nil || pricing.ImageRatio == nil {
		return 0, false
	}
	return *pricing.InputValue * *pricing.ImageRatio, true
}

func imageTokenCost(usage gatewayUsage, pricing selectedPricing) (float64, bool) {
	var total float64
	usedPrice := false

	if pricing.InputValue != nil {
		inputTextTokens := usage.inputTextTokensOrPrompt()
		if inputTextTokens > 0 {
			total += float64(inputTextTokens) * *pricing.InputValue / 1_000_000
			usedPrice = true
		}
	}
	if pricing.InputValue != nil && pricing.ImageRatio != nil && usage.InputImageTokens > 0 {
		total += float64(usage.InputImageTokens) * *pricing.InputValue * *pricing.ImageRatio / 1_000_000
		usedPrice = true
	}
	if pricing.OutputValue != nil {
		outputImageTokens := usage.outputImageTokensOrCompletion()
		if outputImageTokens > 0 {
			total += float64(outputImageTokens) * *pricing.OutputValue / 1_000_000
			usedPrice = true
		}
	}
	return total, usedPrice
}

func isPerRequestPricing(pricing selectedPricing) bool {
	if pricing.QuotaType != nil && *pricing.QuotaType == 1 {
		return true
	}
	switch strings.TrimSpace(strings.ToLower(pricing.BillingType)) {
	case "per_request", "fixed":
		return true
	default:
		return false
	}
}

func perRequestCost(pricing selectedPricing) (float64, bool) {
	if pricing.PerRequestValue != nil {
		return *pricing.PerRequestValue, true
	}
	if pricing.ModelPrice == nil {
		return 0, false
	}
	groupRatio := 1.0
	if pricing.GroupRatio != nil {
		groupRatio = *pricing.GroupRatio
	}
	return *pricing.ModelPrice * groupRatio, true
}

func float64PtrValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func scaledFloat64Value(base *float64, ratio *float64) any {
	if base == nil || ratio == nil {
		return nil
	}
	return *base * *ratio
}

func (u gatewayUsage) normalized() gatewayUsage {
	if u.PromptCacheHitTokens < 0 {
		u.PromptCacheHitTokens = 0
	}
	if u.PromptCacheMissTokens < 0 {
		u.PromptCacheMissTokens = 0
	}
	if u.PromptTokens == 0 && (u.PromptCacheHitTokens > 0 || u.PromptCacheMissTokens > 0) {
		u.PromptTokens = u.PromptCacheHitTokens + u.PromptCacheMissTokens
	}
	if u.CachedPromptTokens == 0 && u.PromptCacheHitTokens > 0 {
		u.CachedPromptTokens = u.PromptCacheHitTokens
	}
	if u.CachedPromptTokens == 0 && u.PromptTokensDetail != nil {
		u.CachedPromptTokens = u.PromptTokensDetail.CachedTokens
	}
	if u.CachedPromptTokens < 0 {
		u.CachedPromptTokens = 0
	}
	if u.CacheWriteTokens == 0 && u.PromptTokensDetail != nil {
		u.CacheWriteTokens = u.PromptTokensDetail.CacheWriteTokens
	}
	if u.CacheWriteTokens < 0 {
		u.CacheWriteTokens = 0
	}
	if u.CacheCreationInputTokens == 0 {
		u.CacheCreationInputTokens = u.CacheCreation5mInputTokens + u.CacheCreation1hInputTokens
	}
	if u.CacheCreation5mInputTokens == 0 && u.CacheCreation1hInputTokens == 0 && u.CacheCreationInputTokens > 0 {
		u.CacheCreation5mInputTokens = u.CacheCreationInputTokens
	}
	if u.PromptTokens > 0 && u.CachedPromptTokens > u.PromptTokens {
		u.CachedPromptTokens = u.PromptTokens
	}
	if u.PromptTokens > 0 && u.CacheWriteTokens > u.PromptTokens {
		u.CacheWriteTokens = u.PromptTokens
	}
	if u.TotalTokens == 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	return u
}

func (u gatewayUsage) hasCacheCreationBreakdown() bool {
	return u.CacheCreation5mInputTokens > 0 || u.CacheCreation1hInputTokens > 0
}

func (u gatewayUsage) uncachedPromptTokens() int {
	uncached := u.PromptTokens - u.CachedPromptTokens - u.CacheCreationInputTokens - u.CacheWriteTokens
	if uncached < 0 {
		return 0
	}
	return uncached
}

func (u gatewayUsage) inputTextTokensOrPrompt() int {
	if u.InputTextTokens > 0 {
		return u.InputTextTokens
	}
	if u.InputImageTokens == 0 {
		return u.PromptTokens
	}
	return 0
}

func (u gatewayUsage) outputImageTokensOrCompletion() int {
	if u.OutputImageTokens > 0 {
		return u.OutputImageTokens
	}
	return u.CompletionTokens
}
