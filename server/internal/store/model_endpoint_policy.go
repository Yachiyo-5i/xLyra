package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

type SiteModelEndpointPolicy struct {
	CatalogEndpointTypes   []string
	SupportedEndpointTypes []string
}

func siteModelEndpointPolicy(canonical CanonicalModel, model SiteModel, override SiteModelEndpointOverride) SiteModelEndpointPolicy {
	catalogTypes := NormalizeModelEndpointTypes(collectCanonicalEndpointTypes(canonical))
	base := catalogTypes
	declared := collectSupportedEndpointTypes(model)
	if len(base) == 0 {
		base = declared
	}
	selected := base
	if len(declared) > 0 {
		selected = IntersectModelEndpointTypes(base, declared)
	}
	switch override.Mode {
	case "disabled":
		selected = []string{}
	case "allowlist":
		var values []string
		if json.Unmarshal(override.EndpointTypes, &values) != nil {
			values = nil
		}
		selected = IntersectModelEndpointTypes(base, values)
	}
	return SiteModelEndpointPolicy{CatalogEndpointTypes: catalogTypes, SupportedEndpointTypes: NormalizeModelEndpointTypes(selected)}
}

func ResolveSiteModelEndpointPolicy(canonical CanonicalModel, model SiteModel, override SiteModelEndpointOverride) SiteModelEndpointPolicy {
	return siteModelEndpointPolicy(canonical, model, override)
}

func (r SiteModelRepository) EndpointPoliciesBySite(ctx context.Context, siteID uuid.UUID) (map[uuid.UUID]SiteModelEndpointPolicy, error) {
	models, err := r.ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	result := map[uuid.UUID]SiteModelEndpointPolicy{}
	if len(models) == 0 {
		return result, nil
	}
	modelIDs := make([]uuid.UUID, 0, len(models))
	canonicalIDs := make([]uuid.UUID, 0, len(models))
	for _, model := range models {
		modelIDs = append(modelIDs, model.ID)
		if model.CanonicalID.Valid {
			canonicalIDs = append(canonicalIDs, model.CanonicalID.UUID)
		}
	}
	var canonicals []CanonicalModel
	if len(canonicalIDs) > 0 {
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": canonicalIDs}).Find(&canonicals).Error; err != nil {
			return nil, err
		}
	}
	var overrides []SiteModelEndpointOverride
	if err := r.db.WithContext(ctx).Where(map[string]any{"site_model_id": modelIDs}).Find(&overrides).Error; err != nil {
		return nil, err
	}
	canonicalByID := map[uuid.UUID]CanonicalModel{}
	for _, item := range canonicals {
		canonicalByID[item.ID] = item
	}
	overrideByID := map[uuid.UUID]SiteModelEndpointOverride{}
	for _, item := range overrides {
		overrideByID[item.SiteModelID] = item
	}
	for _, model := range models {
		result[model.ID] = siteModelEndpointPolicy(canonicalByID[model.CanonicalID.UUID], model, overrideByID[model.ID])
	}
	return result, nil
}
