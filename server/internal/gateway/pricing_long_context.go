package gateway

type longContextRule struct {
	ThresholdTokens  int
	InputMultiplier  float64
	OutputMultiplier float64
}

var longContextExclusions = []string{"codex", "mini", "nano", "spark", "image", "audio", "realtime"}

var longContextPrefixes = []string{"gpt-5.6", "gpt-5.5", "gpt-5.4"}

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

func longContextRuleForVariants(raw map[string]any) *longContextRule {
	c, _ := raw["cost"].(map[string]any)
	over, _ := c["context_over_200k"].(map[string]any)
	threshold := 200000
	if tiers, ok := c["tiers"].([]any); ok {
		for _, value := range tiers {
			price, _ := value.(map[string]any)
			tier, _ := price["tier"].(map[string]any)
			if tier["type"] == "context" {
				if size, ok := tier["size"].(float64); ok {
					threshold = int(size)
					over = price
					break
				}
			}
		}
	}
	in, _ := c["input"].(float64)
	out, _ := c["output"].(float64)
	oi, _ := over["input"].(float64)
	oo, _ := over["output"].(float64)
	if in <= 0 || out <= 0 || oi <= 0 || oo <= 0 {
		return nil
	}
	return &longContextRule{ThresholdTokens: threshold, InputMultiplier: oi / in, OutputMultiplier: oo / out}
}
