package site

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
)

type CreateManualSiteModelParams struct {
	SiteID                 uuid.UUID
	UpstreamModelName      string
	CanonicalModelID       uuid.UUID
	SupportedEndpointTypes []string
	SiteCredentialIDs      []uuid.UUID
	Enabled                bool
}

var allowedManualEndpointTypes = map[string]struct{}{
	"openai":              {},
	"openai-response":     {},
	"openai-image":        {},
	"openai-embedding":    {},
	"openai-audio-speech": {},
	"anthropic-messages":  {},
	"google-gemini":       {},
}

func (s *Service) CreateManualSiteModel(ctx context.Context, params CreateManualSiteModelParams) (store.SiteModel, error) {
	if params.SiteID == uuid.Nil {
		return store.SiteModel{}, fmt.Errorf("site id is required")
	}
	upstreamName := strings.TrimSpace(params.UpstreamModelName)
	if upstreamName == "" {
		return store.SiteModel{}, fmt.Errorf("upstream_model_name is required")
	}
	if params.CanonicalModelID == uuid.Nil {
		return store.SiteModel{}, fmt.Errorf("canonical_model_id is required")
	}
	endpointTypes, err := normalizeManualEndpointTypes(params.SupportedEndpointTypes)
	if err != nil {
		return store.SiteModel{}, err
	}

	status := "disabled"
	if params.Enabled {
		status = "active"
	}

	var created store.SiteModel
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		siteRepo := store.NewSiteRepository(tx)
		siteModelRepo := store.NewSiteModelRepository(tx)
		canonicalRepo := store.NewCanonicalModelRepository(tx)

		site, err := siteRepo.GetByID(ctx, params.SiteID)
		if err != nil {
			return err
		}

		canonical, err := canonicalRepo.GetByID(ctx, params.CanonicalModelID)
		if err != nil {
			return err
		}
		if canonical.Status != "active" {
			return fmt.Errorf("canonical model must be active")
		}

		if normalizeSiteType(site.SiteType) == "newapi" && len(uniqueUUIDs(params.SiteCredentialIDs)) == 0 {
			return fmt.Errorf("site_credential_ids is required for newapi sites")
		}

		displayName := strings.TrimSpace(canonical.DisplayName)
		if displayName == "" {
			displayName = upstreamName
		}

		capabilities := store.JSON(jsonBytes(manualSiteModelCapabilities(endpointTypes)))
		model, err := siteModelRepo.Upsert(ctx, store.UpsertSiteModelParams{
			SiteID:       site.ID,
			UpstreamName: upstreamName,
			DisplayName:  displayName,
			Capabilities: capabilities,
			Status:       status,
		})
		if err != nil {
			return err
		}

		model, err = siteModelRepo.UpdateStatus(ctx, site.ID, model.ID, status)
		if err != nil {
			return err
		}

		now := time.Now()
		model, err = siteModelRepo.UpdateCanonical(ctx, model.ID, canonical.ID, "manual", 100, now)
		if err != nil {
			return err
		}

		if normalizeSiteType(site.SiteType) == "newapi" {
			if err := syncManualNewAPISiteAPIKeyModels(ctx, tx, site.ID, model, params.SiteCredentialIDs, now); err != nil {
				return err
			}
		}

		created = model
		return catalog.ReconcileCanonicalCategories(ctx, tx, params.CanonicalModelID)
	})
	if err != nil {
		return store.SiteModel{}, err
	}
	return created, nil
}

func (s *Service) DeleteManualSiteModel(ctx context.Context, siteID uuid.UUID, modelID uuid.UUID) error {
	if siteID == uuid.Nil {
		return fmt.Errorf("site id is required")
	}
	if modelID == uuid.Nil {
		return fmt.Errorf("model id is required")
	}

	canonicalID := uuid.Nil
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		repo := store.NewSiteModelRepository(tx)
		cooldownRepo := store.NewRouteCooldownRepository(tx)
		model, err := repo.GetByID(ctx, modelID)
		if err != nil {
			return err
		}
		if model.SiteID != siteID {
			return fmt.Errorf("site model was not found")
		}
		if !store.SiteModelManual(model) {
			return fmt.Errorf("only manually added site models can be deleted")
		}
		if model.CanonicalID.Valid {
			canonicalID = model.CanonicalID.UUID
		}
		if err := cooldownRepo.DeleteBySiteModel(ctx, siteID, modelID); err != nil {
			return err
		}
		if err := repo.Delete(ctx, modelID); err != nil {
			return err
		}
		return catalog.ReconcileCanonicalCategories(ctx, tx, canonicalID)
	})
	if err != nil {
		return err
	}
	return nil
}

func normalizeManualEndpointTypes(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := allowedManualEndpointTypes[value]; !ok {
			return nil, fmt.Errorf("unsupported endpoint type %q", value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("supported_endpoint_types is required")
	}
	return result, nil
}

func manualSiteModelCapabilities(endpointTypes []string) map[string]any {
	return map[string]any{
		"source":                   "manual",
		"manual":                   true,
		"supported_endpoint_types": endpointTypes,
		"raw": map[string]any{
			"source": "manual",
		},
	}
}

func syncManualNewAPISiteAPIKeyModels(ctx context.Context, tx store.Tx, siteID uuid.UUID, model store.SiteModel, credentialIDs []uuid.UUID, now time.Time) error {
	credentialIDs = uniqueUUIDs(credentialIDs)
	if len(credentialIDs) == 0 {
		return fmt.Errorf("site_credential_ids is required for newapi sites")
	}

	credentialRepo := store.NewSiteCredentialRepository(tx)
	stateRepo := store.NewSiteAPIKeyStateRepository(tx)
	modelRepo := store.NewSiteAPIKeyModelRepository(tx)

	states, err := stateRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	statesByCredentialID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}

	for _, credentialID := range credentialIDs {
		credential, err := credentialRepo.GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if credential.SiteID != siteID {
			return fmt.Errorf("site credential %s does not belong to site %s", credentialID, siteID)
		}
		if !isSiteAPIKeyCredentialType(credential.CredentialType) {
			return fmt.Errorf("site credential %s is not an api key", credentialID)
		}
		if !manualCredentialEnabled(credential, statesByCredentialID[credential.ID]) {
			return fmt.Errorf("site credential %s is disabled", credentialID)
		}

		if _, err := modelRepo.Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
			SiteID:            siteID,
			SiteCredentialID:  credential.ID,
			SiteModelID:       model.ID,
			UpstreamModelName: model.UpstreamName,
			DisplayName:       model.DisplayName,
			Available:         true,
			Enabled:           true,
			Raw:               store.JSON(jsonBytes(map[string]any{"source": "manual"})),
			LastSeenAt:        now,
			LastSyncedAt:      now,
		}); err != nil {
			return err
		}
		if _, err := modelRepo.UpdateEnabled(ctx, credential.ID, model.UpstreamName, true); err != nil {
			return err
		}
	}

	return nil
}

func isSiteAPIKeyCredentialType(value string) bool {
	return value == defaultCredentialType || strings.HasPrefix(value, defaultCredentialType+":")
}

func manualCredentialEnabled(credential store.SiteCredential, state store.SiteAPIKeyState) bool {
	enabled := credentialEnabledFromMeta(credential.Meta)
	if state.SiteCredentialID != uuid.Nil {
		enabled = state.Enabled
	}
	return enabled
}
