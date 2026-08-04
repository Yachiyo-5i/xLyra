package adapter

import (
	"encoding/json"
	"strconv"
	"strings"
)

type newAPIVendor struct {
	ID          int64
	Name        string
	Description string
	Icon        string
}

func (a NewAPI) ParsePricing(payload any) PricingSnapshot {
	groups, items := parseNewAPIPricingPayload(payload)
	return PricingSnapshot{
		Groups: groups,
		Items:  items,
		Raw:    payload,
	}
}

func parseNewAPIPricingPayload(payload any) ([]PricingGroup, []ModelPricing) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, nil
	}

	groupRatios := map[string]float64{}
	if groupRatioMap, ok := root["group_ratio"].(map[string]any); ok {
		for groupName, rawRatio := range groupRatioMap {
			if ratio, ok := float64FromAny(rawRatio); ok {
				groupRatios[groupName] = ratio
			}
		}
	}

	usableGroupNames := map[string]string{}
	if usableGroups, ok := root["usable_group"].(map[string]any); ok {
		for groupName, rawLabel := range usableGroups {
			usableGroupNames[groupName] = anyString(rawLabel)
		}
	}

	autoGroups := stringSliceFromAny(root["auto_groups"])
	autoGroupSet := map[string]struct{}{}
	for _, groupName := range autoGroups {
		autoGroupSet[groupName] = struct{}{}
	}

	vendorsByID := map[int64]newAPIVendor{}
	if vendors, ok := root["vendors"].([]any); ok {
		for _, rawVendor := range vendors {
			vendorMap, ok := rawVendor.(map[string]any)
			if !ok {
				continue
			}
			id, ok := int64FromAny(vendorMap["id"])
			if !ok {
				continue
			}
			vendorsByID[id] = newAPIVendor{
				ID:          id,
				Name:        anyString(vendorMap["name"]),
				Description: anyString(vendorMap["description"]),
				Icon:        anyString(vendorMap["icon"]),
			}
		}
	}

	groups := make([]PricingGroup, 0, len(groupRatios)+len(usableGroupNames))
	groupSeen := map[string]struct{}{}
	for groupName, ratio := range groupRatios {
		groups = append(groups, PricingGroup{
			GroupName:   groupName,
			DisplayName: defaultString(usableGroupNames[groupName], groupName),
			Ratio:       ratio,
			IsAuto:      hasString(autoGroupSet, groupName),
			Raw:         map[string]any{"group_name": groupName, "ratio": ratio},
		})
		groupSeen[groupName] = struct{}{}
	}
	for groupName, label := range usableGroupNames {
		if _, ok := groupSeen[groupName]; ok {
			continue
		}
		groups = append(groups, PricingGroup{
			GroupName:   groupName,
			DisplayName: defaultString(label, groupName),
			Ratio:       1,
			IsAuto:      hasString(autoGroupSet, groupName),
			Raw:         map[string]any{"group_name": groupName, "ratio": 1},
		})
		groupSeen[groupName] = struct{}{}
	}
	for _, groupName := range autoGroups {
		if _, ok := groupSeen[groupName]; ok {
			continue
		}
		groups = append(groups, PricingGroup{
			GroupName:   groupName,
			DisplayName: groupName,
			Ratio:       1,
			IsAuto:      true,
			Raw:         map[string]any{"group_name": groupName, "ratio": 1},
		})
		groupSeen[groupName] = struct{}{}
	}
	if len(groups) == 0 {
		groups = append(groups, PricingGroup{
			GroupName:   "default",
			DisplayName: "default",
			Ratio:       1,
			Raw:         map[string]any{"group_name": "default", "ratio": 1},
		})
		groupRatios["default"] = 1
		groupSeen["default"] = struct{}{}
	}

	quotaPerUnit := 500000.0
	if value, ok := float64FromAny(root["quota_per_unit"]); ok && value > 0 {
		quotaPerUnit = value
	}
	usdPerMillionTokens := 1000000.0 / quotaPerUnit

	pricingRows := []any{}
	switch data := root["data"].(type) {
	case []any:
		pricingRows = data
	case map[string]any:
		for modelName, raw := range data {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := row["model_name"]; !exists {
				row["model_name"] = modelName
			}
			pricingRows = append(pricingRows, row)
		}
	}

	pricings := make([]ModelPricing, 0)
	for _, item := range pricingRows {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}

		modelName := strings.TrimSpace(anyString(raw["model_name"]))
		if modelName == "" {
			modelName = strings.TrimSpace(anyString(raw["model"]))
		}
		if modelName == "" {
			continue
		}

		enableGroups := stringSliceFromAny(raw["enable_group"])
		if len(enableGroups) == 0 {
			enableGroups = stringSliceFromAny(raw["enable_groups"])
		}
		if len(enableGroups) == 0 {
			enableGroups = mapKeys(groupSeen)
		}

		quotaType64, hasQuotaType := int64FromAny(raw["quota_type"])
		quotaType := 0
		if hasQuotaType {
			quotaType = int(quotaType64)
		}
		billingType := defaultString(anyString(raw["billing_type"]), "tokens")
		if quotaType == 1 {
			billingType = defaultString(anyString(raw["billing_type"]), "per_request")
		}

		modelRatio, hasModelRatio := float64FromAny(raw["model_ratio"])
		completionRatio, hasCompletionRatio := float64FromAny(raw["completion_ratio"])
		cacheRatio, hasCacheRatio := float64FromAny(raw["cache_ratio"])
		createCacheRatio, hasCreateCacheRatio := float64FromAny(raw["create_cache_ratio"])
		createCache1hRatio, hasCreateCache1hRatio := float64FromAny(raw["create_cache_1h_ratio"])
		imageRatio, hasImageRatio := float64FromAny(raw["image_ratio"])
		audioRatio, hasAudioRatio := float64FromAny(raw["audio_ratio"])
		audioCompletionRatio, hasAudioCompletionRatio := float64FromAny(raw["audio_completion_ratio"])
		modelPrice, hasModelPrice := float64FromAny(raw["model_price"])
		vendorID, hasVendorID := int64FromAny(raw["vendor_id"])
		vendor := vendorsByID[vendorID]

		for _, groupName := range enableGroups {
			groupRatio, ok := groupRatios[groupName]
			if !ok {
				groupRatio = 1
			}
			inputValue, hasInputValue, outputValue, hasOutputValue, perRequestValue, hasPerRequestValue := pricingValues(
				quotaType,
				groupRatio,
				modelRatio,
				hasModelRatio,
				completionRatio,
				hasCompletionRatio,
				modelPrice,
				hasModelPrice,
				usdPerMillionTokens,
			)

			pricings = append(pricings, ModelPricing{
				ModelName:               modelName,
				DisplayName:             firstNonEmptyString(anyString(raw["display_name"]), anyString(raw["name"]), modelName),
				GroupName:               groupName,
				QuotaType:               quotaType,
				BillingType:             billingType,
				Currency:                defaultString(anyString(raw["currency"]), "USD"),
				GroupRatio:              groupRatio,
				ModelRatio:              modelRatio,
				HasModelRatio:           hasModelRatio,
				CompletionRatio:         completionRatio,
				HasCompletionRatio:      hasCompletionRatio,
				CacheRatio:              cacheRatio,
				HasCacheRatio:           hasCacheRatio,
				CreateCacheRatio:        createCacheRatio,
				HasCreateCacheRatio:     hasCreateCacheRatio,
				CreateCache1hRatio:      createCache1hRatio,
				HasCreateCache1hRatio:   hasCreateCache1hRatio,
				ImageRatio:              imageRatio,
				HasImageRatio:           hasImageRatio,
				AudioRatio:              audioRatio,
				HasAudioRatio:           hasAudioRatio,
				AudioCompletionRatio:    audioCompletionRatio,
				HasAudioCompletionRatio: hasAudioCompletionRatio,
				ModelPrice:              modelPrice,
				HasModelPrice:           hasModelPrice,
				PerRequestValue:         perRequestValue,
				HasPerRequestValue:      hasPerRequestValue,
				InputValue:              inputValue,
				HasInputValue:           hasInputValue,
				OutputValue:             outputValue,
				HasOutputValue:          hasOutputValue,
				VendorID:                vendorID,
				HasVendorID:             hasVendorID,
				VendorName:              defaultString(vendor.Name, anyString(raw["vendor_name"])),
				VendorIcon:              defaultString(vendor.Icon, anyString(raw["vendor_icon"])),
				Description:             defaultString(anyString(raw["description"]), vendor.Description),
				OwnerBy:                 anyString(raw["owner_by"]),
				Raw:                     raw,
			})
		}
	}

	return groups, pricings
}

func pricingValues(
	quotaType int,
	groupRatio float64,
	modelRatio float64,
	hasModelRatio bool,
	completionRatio float64,
	hasCompletionRatio bool,
	modelPrice float64,
	hasModelPrice bool,
	usdPerMillionTokens float64,
) (float64, bool, float64, bool, float64, bool) {
	// Derive by which pricing field is actually present, using quota_type only to
	// pick the intended mode. This keeps well-formed data identical while ensuring
	// that whenever a ratio/price column is persisted a value column is derived —
	// otherwise a row with model_ratio/model_price but no input_value/output_value
	// bills zero, because estimateCost reads only the value columns (F12).
	perRequest := func() (float64, bool, float64, bool, float64, bool) {
		return 0, false, 0, false, modelPrice * groupRatio, true
	}
	tokenValues := func() (float64, bool, float64, bool, float64, bool) {
		effectiveCompletionRatio := 1.0
		if hasCompletionRatio {
			effectiveCompletionRatio = completionRatio
		}
		inputValue := modelRatio * groupRatio * usdPerMillionTokens
		return inputValue, true, inputValue * effectiveCompletionRatio, true, 0, false
	}

	if quotaType == 1 {
		if hasModelPrice {
			return perRequest()
		}
		if hasModelRatio {
			// Per-request quota_type but only a token ratio was provided; bill on
			// tokens rather than silently dropping to zero.
			return tokenValues()
		}
		return 0, false, 0, false, 0, false
	}

	if hasModelRatio {
		return tokenValues()
	}
	if hasModelPrice {
		// Token quota_type but only a per-request price was provided; bill per
		// request rather than silently dropping to zero.
		return perRequest()
	}
	return 0, false, 0, false, 0, false
}

func float64FromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func int64FromAny(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case json.Number:
		parsed, err := strconv.ParseInt(v.String(), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func stringSliceFromAny(value any) []string {
	switch v := value.(type) {
	case []string:
		items := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(anyString(item))
			if text != "" {
				items = append(items, text)
			}
		}
		return items
	case string:
		items := make([]string, 0)
		for _, item := range strings.Split(v, ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				items = append(items, item)
			}
		}
		return items
	default:
		return nil
	}
}

func anySliceFromAny(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []string:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
		return items
	default:
		return []any{}
	}
}

func anyString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func hasString(values map[string]struct{}, key string) bool {
	_, ok := values[key]
	return ok
}

func mapKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for key := range values {
		items = append(items, key)
	}
	return items
}
