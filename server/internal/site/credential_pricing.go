package site

import (
	"context"
	"sort"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type CredentialModelPricing struct {
	SiteID            uuid.UUID
	SiteModelID       uuid.UUID
	SiteCredentialID  uuid.UUID
	CredentialName    string
	RoutingPriority   float64
	GroupRatio        float64
	CredentialEnabled bool
	CredentialUsable  bool
	ModelEnabled      bool
	ModelAvailable    bool
}

func (s *Service) SiteCredentialModelPricings(ctx context.Context, siteID uuid.UUID) ([]CredentialModelPricing, error) {
	siteItem, err := store.NewSiteRepository(s.db.DB()).GetByID(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return s.credentialModelPricings(ctx, []store.Site{siteItem}, siteID)
}

func (s *Service) AllSiteCredentialModelPricings(ctx context.Context) ([]CredentialModelPricing, error) {
	sites, err := store.NewSiteRepository(s.db.DB()).List(ctx)
	if err != nil {
		return nil, err
	}
	return s.credentialModelPricings(ctx, sites, uuid.Nil)
}

func (s *Service) credentialModelPricings(ctx context.Context, sites []store.Site, siteID uuid.UUID) ([]CredentialModelPricing, error) {
	apiKeySites := make(map[uuid.UUID]struct{}, len(sites))
	for _, item := range sites {
		if normalizeSiteType(item.SiteType) == "openai" {
			apiKeySites[item.ID] = struct{}{}
		}
	}
	if len(apiKeySites) == 0 {
		return nil, nil
	}

	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	stateRepo := store.NewSiteAPIKeyStateRepository(s.db.DB())
	modelRepo := store.NewSiteAPIKeyModelRepository(s.db.DB())

	var credentials []store.SiteCredential
	var states []store.SiteAPIKeyState
	var models []store.SiteAPIKeyModel
	var err error
	if siteID == uuid.Nil {
		credentials, err = credentialRepo.ListAll(ctx)
	} else {
		credentials, err = credentialRepo.ListBySite(ctx, siteID)
	}
	if err != nil {
		return nil, err
	}
	if siteID == uuid.Nil {
		states, err = stateRepo.ListAll(ctx)
	} else {
		states, err = stateRepo.ListBySite(ctx, siteID)
	}
	if err != nil {
		return nil, err
	}
	if siteID == uuid.Nil {
		models, err = modelRepo.ListAll(ctx)
	} else {
		models, err = modelRepo.ListBySite(ctx, siteID)
	}
	if err != nil {
		return nil, err
	}
	return buildCredentialModelPricings(sites, credentials, states, models), nil
}

func buildCredentialModelPricings(sites []store.Site, credentials []store.SiteCredential, states []store.SiteAPIKeyState, models []store.SiteAPIKeyModel) []CredentialModelPricing {
	apiKeySites := make(map[uuid.UUID]struct{}, len(sites))
	for _, item := range sites {
		if normalizeSiteType(item.SiteType) == "openai" {
			apiKeySites[item.ID] = struct{}{}
		}
	}
	credentialsByID := make(map[uuid.UUID]store.SiteCredential, len(credentials))
	for _, credential := range credentials {
		if _, ok := apiKeySites[credential.SiteID]; !ok || !isSiteAPIKeyCredentialType(credential.CredentialType) {
			continue
		}
		credentialsByID[credential.ID] = credential
	}
	statesByCredentialID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}

	result := make([]CredentialModelPricing, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if !model.Available || !model.SiteModelID.Valid {
			continue
		}
		credential, ok := credentialsByID[model.SiteCredentialID]
		if !ok {
			continue
		}
		key := model.SiteModelID.UUID.String() + "\x1f" + credential.ID.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		state := statesByCredentialID[credential.ID]
		credentialEnabled := manualCredentialEnabled(credential, state)
		result = append(result, CredentialModelPricing{
			SiteID:            credential.SiteID,
			SiteModelID:       model.SiteModelID.UUID,
			SiteCredentialID:  credential.ID,
			CredentialName:    store.SiteCredentialDisplayName(credential, state),
			RoutingPriority:   store.SiteCredentialRoutingPriority(credential),
			GroupRatio:        store.SiteCredentialUpstreamCostMultiplier(credential),
			CredentialEnabled: credentialEnabled,
			CredentialUsable:  credentialEnabled && store.CredentialUsable(credential) && store.CredentialStateUsableForCredential(credential, state),
			ModelEnabled:      model.Enabled,
			ModelAvailable:    model.Available,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SiteID != result[j].SiteID {
			return result[i].SiteID.String() < result[j].SiteID.String()
		}
		if result[i].SiteModelID != result[j].SiteModelID {
			return result[i].SiteModelID.String() < result[j].SiteModelID.String()
		}
		if result[i].RoutingPriority != result[j].RoutingPriority {
			return result[i].RoutingPriority > result[j].RoutingPriority
		}
		if strings.TrimSpace(result[i].CredentialName) != strings.TrimSpace(result[j].CredentialName) {
			return result[i].CredentialName < result[j].CredentialName
		}
		return result[i].SiteCredentialID.String() < result[j].SiteCredentialID.String()
	})
	return result
}
