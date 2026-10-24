package site

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

// Account models carry the same endpoint information as API key models so the
// protocol editor can be shared.

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func newAccountModel(model store.SiteAPIKeyModel, display string) GrokAccountModel {
	capabilities := model.Capabilities()
	siteModelID := ""
	if model.SiteModelID.Valid {
		siteModelID = model.SiteModelID.UUID.String()
	}
	return GrokAccountModel{
		UpstreamName:           model.UpstreamModelName,
		DisplayName:            display,
		Enabled:                model.Enabled,
		SiteModelID:            siteModelID,
		SupportedEndpointTypes: nonNilStrings(capabilities.SupportedEndpointTypes),
		EffectiveEndpointTypes: nonNilStrings(capabilities.SupportedEndpointTypes),
		AvailableEndpointTypes: store.AvailableModelEndpointTypes(capabilities.SupportedEndpointTypes),
		EndpointOverride:       capabilities.EndpointOverride,
		record:                 model,
	}
}

// resolveAccountModelEndpoints computes the protocols each model is effectively
// served on once the site-level policy and the account override are applied.
func (s *Service) resolveAccountModelEndpoints(ctx context.Context, models []GrokAccountModel) []GrokAccountModel {
	cache := map[uuid.UUID][]string{}
	for index := range models {
		model := &models[index]
		if !model.record.SiteModelID.Valid {
			continue
		}
		siteModelID := model.record.SiteModelID.UUID
		siteTypes, ok := cache[siteModelID]
		if !ok {
			resolved, err := s.SiteModelEndpointTypes(ctx, siteModelID)
			if err != nil {
				continue
			}
			siteTypes = resolved
			cache[siteModelID] = siteTypes
		}
		model.EffectiveEndpointTypes = nonNilStrings(model.record.EffectiveEndpointTypes(siteTypes))
		model.AvailableEndpointTypes = store.AvailableModelEndpointTypes(model.EffectiveEndpointTypes)
	}
	return models
}

// setAccountModelEndpointOverride stores the per-account protocol override of
// one model. The allowlist must stay within what the site model supports.
func (s *Service) setAccountModelEndpointOverride(ctx context.Context, siteID, credentialID, siteModelID uuid.UUID, mode string, endpointTypes []string, accountType func(string) bool) (store.SiteCredential, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "inherit"
	}
	if mode != "inherit" && mode != "allowlist" && mode != "disabled" {
		return store.SiteCredential{}, fmt.Errorf("invalid endpoint override mode %q", mode)
	}
	if mode == "allowlist" && len(endpointTypes) == 0 {
		return store.SiteCredential{}, fmt.Errorf("endpoint types are required for allowlist")
	}
	credential, err := store.NewSiteCredentialRepository(s.db.DB()).GetByID(ctx, credentialID)
	if err != nil {
		return store.SiteCredential{}, err
	}
	if credential.SiteID != siteID || !accountType(credential.CredentialType) {
		return store.SiteCredential{}, fmt.Errorf("account was not found")
	}
	siteModel, err := store.NewSiteModelRepository(s.db.DB()).GetByID(ctx, siteModelID)
	if err != nil || siteModel.SiteID != siteID {
		return store.SiteCredential{}, fmt.Errorf("site model was not found")
	}
	modelRepo := store.NewSiteAPIKeyModelRepository(s.db.DB())
	if _, err := modelRepo.GetByCredentialModel(ctx, siteID, credentialID, siteModelID); err != nil {
		return store.SiteCredential{}, fmt.Errorf("account model was not found")
	}
	supported, err := s.SiteModelEndpointTypes(ctx, siteModelID)
	if err != nil {
		return store.SiteCredential{}, err
	}
	endpointTypes = store.NormalizeModelEndpointTypes(endpointTypes)
	if mode == "allowlist" && len(store.IntersectModelEndpointTypes(supported, endpointTypes)) != len(endpointTypes) {
		return store.SiteCredential{}, fmt.Errorf("endpoint types exceed site model capabilities")
	}
	if _, err := modelRepo.UpdateEndpointOverride(ctx, credentialID, siteModelID, store.SiteAPIKeyModelEndpointOverride{Mode: mode, EndpointTypes: endpointTypes}); err != nil {
		return store.SiteCredential{}, err
	}
	return credential, nil
}
