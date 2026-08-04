package adapter

import (
	"context"
	"strings"

	"xlyra/server/internal/newapi"
)

type NewAPI struct {
	service *newapi.Service
}

func NewNewAPI() NewAPI {
	return NewAPI{service: newapi.NewService()}
}

func (a NewAPI) SiteTypes() []string {
	return []string{"newapi"}
}

func (a NewAPI) Capabilities() []Capability {
	return []Capability{
		CapabilityDetect,
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityListAPIKeys,
		CapabilitySummarizeAPIKey,
		CapabilityFetchUserSummary,
		CapabilityFetchBalance,
		CapabilityFetchPricing,
		CapabilityCheckin,
		CapabilityFetchMetadata,
	}
}

func (a NewAPI) Detect(ctx context.Context, baseURL string) (DetectResult, error) {
	result, err := a.service.Detect(ctx, baseURL)
	if err != nil {
		return DetectResult{}, err
	}

	return DetectResult{
		SiteType:   result.SiteType,
		Matched:    result.Matched,
		Confidence: result.Confidence,
		Features:   result.Features,
		RawStatus:  result.RawStatus,
	}, nil
}

func (a NewAPI) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := a.serviceForSite(site).APIKeySummary(ctx, site.BaseURL, apiKey)
	return err
}

func (a NewAPI) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	summary, err := a.serviceForSite(site).APIKeySummary(ctx, site.BaseURL, apiKey)
	if err != nil {
		return nil, err
	}

	return modelsFromPayload(summary.Models), nil
}

func (a NewAPI) ListAPIKeys(ctx context.Context, site SiteConfig, auth SystemAuth) ([]APIKey, error) {
	keys, err := a.serviceForSite(site).UserAPIKeys(ctx, site.BaseURL, auth.AccessToken, auth.UserID)
	if err != nil {
		return nil, err
	}

	result := make([]APIKey, 0, len(keys))
	for _, key := range keys {
		result = append(result, APIKey{
			ID:        key.ID,
			Name:      strings.TrimSpace(key.Name),
			Status:    key.Status,
			Key:       key.Key,
			MaskedKey: key.MaskedKey,
			Raw:       key.Raw,
		})
	}

	return result, nil
}

func (a NewAPI) SummarizeAPIKey(ctx context.Context, site SiteConfig, apiKey string) (APIKeySummary, error) {
	summary, err := a.serviceForSite(site).APIKeySummary(ctx, site.BaseURL, apiKey)
	if err != nil {
		return APIKeySummary{}, err
	}

	return APIKeySummary{
		Usage:  summary.Usage,
		Models: modelsFromPayload(summary.Models),
		Raw:    summary.Models,
	}, nil
}

func (a NewAPI) FetchBalance(ctx context.Context, site SiteConfig, auth SystemAuth) (BalanceSnapshot, error) {
	summary, err := a.FetchUserSummary(ctx, site, auth)
	if err != nil {
		return BalanceSnapshot{}, err
	}

	return BalanceSnapshot{Raw: summary.User}, nil
}

func (a NewAPI) FetchPricing(ctx context.Context, site SiteConfig, auth SystemAuth) (PricingSnapshot, error) {
	summary, err := a.FetchUserSummary(ctx, site, auth)
	if err != nil {
		return PricingSnapshot{}, err
	}

	return a.ParsePricing(summary.Pricing), nil
}

func (a NewAPI) ExecuteCheckin(ctx context.Context, site SiteConfig, auth SystemAuth) (CheckinResult, error) {
	result, err := a.serviceForSite(site).DoCheckin(ctx, site.BaseURL, auth.AccessToken, auth.UserID)
	if err != nil {
		return CheckinResult{}, err
	}

	return CheckinResult{Raw: result.Result}, nil
}

func (a NewAPI) FetchMetadata(ctx context.Context, site SiteConfig, auth SystemAuth) (MetadataSnapshot, error) {
	summary, err := a.FetchUserSummary(ctx, site, auth)
	if err != nil {
		return MetadataSnapshot{}, err
	}

	return MetadataSnapshot{Raw: map[string]any{
		"user":        summary.User,
		"user_models": summary.UserModels,
		"checkin":     summary.Checkin,
	}}, nil
}

func (a NewAPI) FetchUserSummary(ctx context.Context, site SiteConfig, auth SystemAuth) (UserSummary, error) {
	summary, err := a.serviceForSite(site).UserSummary(ctx, site.BaseURL, auth.AccessToken, auth.UserID)
	if err != nil {
		return UserSummary{}, err
	}

	return UserSummary{
		User:         summary.User,
		APIKeys:      summary.APIKeys,
		UserModels:   summary.UserModels,
		Pricing:      summary.Pricing,
		Checkin:      summary.Checkin,
		CheckinReady: summary.CheckinReady,
	}, nil
}

func (a NewAPI) serviceForSite(site SiteConfig) *newapi.Service {
	if site.Client != nil {
		return newapi.NewServiceWithHTTPClient(site.Client)
	}
	return a.service
}

func modelsFromPayload(payload any) []Model {
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		return nil
	}
	data, ok := payloadMap["data"].([]any)
	if !ok {
		return nil
	}

	models := make([]Model, 0, len(data))
	for _, item := range data {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := raw["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		capabilities := map[string]any{
			"source": "newapi",
			"raw":    raw,
		}
		if endpoints := stringSliceFromAny(raw["supported_endpoint_types"]); len(endpoints) > 0 {
			capabilities["supported_endpoint_types"] = endpoints
		}
		if object, _ := raw["object"].(string); object != "" {
			capabilities["object"] = object
		}
		if ownedBy, _ := raw["owned_by"].(string); ownedBy != "" {
			capabilities["owned_by"] = ownedBy
		}

		models = append(models, Model{
			UpstreamName: id,
			DisplayName:  id,
			Capabilities: capabilities,
		})
	}

	return models
}
