package site

import (
	"context"
	"strings"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

const grokImagePerRequestUSD = 0.05

func seedDefaultModelPricing(ctx context.Context, tx store.Tx, site store.Site, siteModel store.SiteModel, model adapter.Model) error {
	value, ok := defaultModelPricing(site, model)
	if !ok {
		return nil
	}
	_, err := store.NewSiteModelPricingRepository(tx).Upsert(ctx, store.UpsertSiteModelPricingParams{
		SiteID:          site.ID,
		SiteModelID:     siteModel.ID,
		ModelName:       siteModel.UpstreamName,
		GroupName:       "default",
		QuotaType:       1,
		BillingType:     "per_request",
		Currency:        "USD",
		GroupRatio:      1,
		PerRequestValue: value,
		PricingSource:   "default",
		PreserveManual:  true,
		Available:       true,
	})
	return err
}

func defaultModelPricing(site store.Site, model adapter.Model) (float64, bool) {
	if !strings.EqualFold(strings.TrimSpace(site.SiteType), "grok") {
		return 0, false
	}
	if !modelSupportsImageEndpoint(model) {
		return 0, false
	}
	return grokImagePerRequestUSD, true
}

func modelSupportsImageEndpoint(model adapter.Model) bool {
	raw, ok := model.Capabilities["supported_endpoint_types"]
	if !ok {
		return false
	}
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value), "openai-image") {
				return true
			}
		}
	case []any:
		for _, value := range values {
			text, _ := value.(string)
			if strings.EqualFold(strings.TrimSpace(text), "openai-image") {
				return true
			}
		}
	}
	return false
}
