package gateway

import "strings"

type longContextRule struct {
	ThresholdTokens  int
	InputMultiplier  float64
	OutputMultiplier float64
}

var longContextExclusions = []string{"codex", "mini", "nano", "spark", "image", "audio", "realtime"}

var longContextPrefixes = []string{"gpt-5.6", "gpt-5.5", "gpt-5.4"}

func longContextRuleForModel(model string) *longContextRule {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return nil
	}
	for _, exclusion := range longContextExclusions {
		if strings.Contains(normalized, exclusion) {
			return nil
		}
	}
	for _, prefix := range longContextPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return &longContextRule{ThresholdTokens: 272000, InputMultiplier: 2, OutputMultiplier: 1.5}
		}
	}
	return nil
}

func applyLongContextPricing(usage gatewayUsage, pricing selectedPricing) selectedPricing {
	rule := pricing.LongContextRule
	if rule == nil || pricing.LongContextApplied {
		return pricing
	}
	if isPerRequestPricing(pricing) {
		return pricing
	}
	usage = usage.normalized()
	if usage.ImageCount > 0 || usage.PromptTokens <= rule.ThresholdTokens {
		return pricing
	}
	if pricing.InputValue != nil {
		value := *pricing.InputValue * rule.InputMultiplier
		pricing.InputValue = &value
	}
	if pricing.OutputValue != nil {
		value := *pricing.OutputValue * rule.OutputMultiplier
		pricing.OutputValue = &value
	}
	pricing.LongContextApplied = true
	return pricing
}
