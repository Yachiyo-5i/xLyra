package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SiteAPIKeyModelEndpointOverride struct {
	Mode          string   `json:"mode"`
	EndpointTypes []string `json:"endpoint_types"`
}

type SiteAPIKeyModelCapabilities struct {
	SupportedEndpointTypes []string                        `json:"supported_endpoint_types"`
	EndpointOverride       SiteAPIKeyModelEndpointOverride `json:"endpoint_override"`
}

func (m SiteAPIKeyModel) Capabilities() SiteAPIKeyModelCapabilities {
	var value SiteAPIKeyModelCapabilities
	if len(m.Raw) == 0 {
		return SiteAPIKeyModelCapabilities{EndpointOverride: SiteAPIKeyModelEndpointOverride{Mode: "inherit"}}
	}
	if json.Unmarshal(m.Raw, &value) != nil {
		return SiteAPIKeyModelCapabilities{EndpointOverride: SiteAPIKeyModelEndpointOverride{Mode: "disabled", EndpointTypes: []string{}}}
	}
	if value.SupportedEndpointTypes == nil {
		var nested struct {
			Raw struct {
				SupportedEndpointTypes []string `json:"supported_endpoint_types"`
			} `json:"raw"`
		}
		if json.Unmarshal(m.Raw, &nested) == nil {
			value.SupportedEndpointTypes = nested.Raw.SupportedEndpointTypes
		}
	}
	value.SupportedEndpointTypes = NormalizeModelEndpointTypes(value.SupportedEndpointTypes)
	if value.EndpointOverride.Mode == "" {
		value.EndpointOverride.Mode = "inherit"
	}
	value.EndpointOverride.EndpointTypes = NormalizeModelEndpointTypes(value.EndpointOverride.EndpointTypes)
	return value
}

func NormalizeModelEndpointTypes(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}

func IntersectModelEndpointTypes(base, selected []string) []string {
	result := []string{}
	allowed := map[string]bool{}
	for _, value := range NormalizeModelEndpointTypes(base) {
		allowed[value] = true
	}
	for _, value := range NormalizeModelEndpointTypes(selected) {
		if allowed[value] {
			result = append(result, value)
		}
	}
	return result
}

func AvailableModelEndpointTypes(values []string) []string {
	normalized := NormalizeModelEndpointTypes(values)
	hasText := false
	for _, value := range normalized {
		switch value {
		case "openai", "openai-response", "anthropic-messages", "google-gemini":
			hasText = true
		}
	}
	if !hasText {
		return normalized
	}
	result := []string{"openai", "openai-response", "anthropic-messages"}
	for _, value := range normalized {
		switch value {
		case "openai", "openai-response", "anthropic-messages", "google-gemini":
			continue
		default:
			result = append(result, value)
		}
	}
	return result
}

func (m SiteAPIKeyModel) EffectiveEndpointTypes(siteTypes []string) []string {
	capabilities := m.Capabilities()
	switch capabilities.EndpointOverride.Mode {
	case "disabled":
		return []string{}
	case "allowlist":
		return IntersectModelEndpointTypes(siteTypes, capabilities.EndpointOverride.EndpointTypes)
	case "inherit":
		if capabilities.SupportedEndpointTypes == nil {
			return NormalizeModelEndpointTypes(siteTypes)
		}
		return IntersectModelEndpointTypes(siteTypes, capabilities.SupportedEndpointTypes)
	default:
		return []string{}
	}
}

func (m SiteAPIKeyModel) Manual() bool {
	var raw struct {
		Source string `json:"source"`
		Manual bool   `json:"manual"`
	}
	return json.Unmarshal(m.Raw, &raw) == nil && (raw.Manual || raw.Source == "manual")
}

type SiteAPIKeyModel struct {
	ID                uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID            uuid.UUID
	SiteCredentialID  uuid.UUID
	SiteModelID       uuid.NullUUID
	UpstreamModelName string
	DisplayName       string
	Available         bool
	Enabled           bool
	Raw               JSON `gorm:"type:jsonb"`
	LastSeenAt        sql.NullTime
	LastSyncedAt      sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type UpsertSiteAPIKeyModelParams struct {
	SiteID            uuid.UUID
	SiteCredentialID  uuid.UUID
	SiteModelID       any
	UpstreamModelName string
	DisplayName       string
	Available         bool
	Enabled           bool
	Raw               JSON
	LastSeenAt        any
	LastSyncedAt      any
}

type SiteAPIKeyModelRepository struct {
	db *gorm.DB
}

func NewSiteAPIKeyModelRepository(db *gorm.DB) SiteAPIKeyModelRepository {
	return SiteAPIKeyModelRepository{db: db}
}

func (r SiteAPIKeyModelRepository) Upsert(ctx context.Context, params UpsertSiteAPIKeyModelParams) (SiteAPIKeyModel, error) {
	db := r.db.WithContext(ctx)
	var model SiteAPIKeyModel
	err := db.Where(&SiteAPIKeyModel{SiteCredentialID: params.SiteCredentialID, UpstreamModelName: params.UpstreamModelName}).First(&model).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteAPIKeyModel{}, fmt.Errorf("upsert site api key model: %w", err)
	}
	model.SiteID = params.SiteID
	model.SiteCredentialID = params.SiteCredentialID
	if binding := nullUUIDFromAny(params.SiteModelID); binding.Valid || err == gorm.ErrRecordNotFound {
		model.SiteModelID = binding
	}
	model.UpstreamModelName = params.UpstreamModelName
	model.DisplayName = params.DisplayName
	model.Available = params.Available
	if err == gorm.ErrRecordNotFound {
		model.Enabled = params.Enabled
	}
	model.Raw = mergeSiteAPIKeyModelRaw(model.Raw, jsonDefault(params.Raw, "{}"))
	model.LastSeenAt = nullTimeFromAny(params.LastSeenAt)
	model.LastSyncedAt = nullTimeFromAny(params.LastSyncedAt)
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&model).Error; err != nil {
			return SiteAPIKeyModel{}, fmt.Errorf("upsert site api key model: %w", err)
		}
		return model, nil
	}
	if err := db.Save(&model).Error; err != nil {
		return SiteAPIKeyModel{}, fmt.Errorf("upsert site api key model: %w", err)
	}
	return model, nil
}

func mergeSiteAPIKeyModelRaw(existing, incoming JSON) JSON {
	oldMeta := map[string]json.RawMessage{}
	newMeta := map[string]json.RawMessage{}
	_ = json.Unmarshal(existing, &oldMeta)
	if json.Unmarshal(incoming, &newMeta) != nil || newMeta == nil {
		newMeta = map[string]json.RawMessage{}
	}
	delete(newMeta, "endpoint_override")
	if override, ok := oldMeta["endpoint_override"]; ok {
		newMeta["endpoint_override"] = override
	}
	if (SiteAPIKeyModel{Raw: existing}).Manual() {
		newMeta["manual"] = json.RawMessage("true")
	}
	raw, _ := json.Marshal(newMeta)
	return JSON(raw)
}

func (r SiteAPIKeyModelRepository) ListBySite(ctx context.Context, siteID uuid.UUID) ([]SiteAPIKeyModel, error) {
	var result []SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyModel{SiteID: siteID}).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site api key models: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].UpstreamModelName < result[j].UpstreamModelName
	})
	return result, nil
}

func (r SiteAPIKeyModelRepository) ListAll(ctx context.Context) ([]SiteAPIKeyModel, error) {
	var result []SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site api key models: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].SiteID != result[j].SiteID {
			return result[i].SiteID.String() < result[j].SiteID.String()
		}
		if result[i].SiteCredentialID != result[j].SiteCredentialID {
			return result[i].SiteCredentialID.String() < result[j].SiteCredentialID.String()
		}
		return result[i].UpstreamModelName < result[j].UpstreamModelName
	})
	return result, nil
}

func (r SiteAPIKeyModelRepository) ListByCredential(ctx context.Context, siteCredentialID uuid.UUID) ([]SiteAPIKeyModel, error) {
	var result []SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyModel{SiteCredentialID: siteCredentialID}).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site api key models by credential: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].UpstreamModelName < result[j].UpstreamModelName
	})
	return result, nil
}

func (r SiteAPIKeyModelRepository) DeleteByCredential(ctx context.Context, siteCredentialID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyModel{SiteCredentialID: siteCredentialID}).Delete(&SiteAPIKeyModel{}).Error; err != nil {
		return fmt.Errorf("delete site api key models by credential: %w", err)
	}
	return nil
}

func (r SiteAPIKeyModelRepository) MarkUnavailableExcept(ctx context.Context, siteCredentialID uuid.UUID, modelNames []string) error {
	models, err := r.ListByCredential(ctx, siteCredentialID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, name := range modelNames {
		seen[name] = struct{}{}
	}
	now := time.Now()
	db := r.db.WithContext(ctx)
	for _, model := range models {
		if _, ok := seen[model.UpstreamModelName]; ok || model.Manual() {
			continue
		}
		model.Available = false
		model.LastSyncedAt = sql.NullTime{Time: now, Valid: true}
		if err := db.Save(&model).Error; err != nil {
			return fmt.Errorf("mark stale api key models unavailable: %w", err)
		}
	}
	return nil
}

// BindSiteModel links every api-key model row of a site that shares the given
// upstream model name to the site model. Credential selection
// (ListCredentialsForSiteModel) filters by site_model_id, so a row left unbound
// (e.g. after a single-key refresh) is invisible to routing and the caller falls
// back to an arbitrary credential. Only site_model_id is updated.
func (r SiteAPIKeyModelRepository) BindSiteModel(ctx context.Context, siteID uuid.UUID, upstreamModelName string, siteModelID uuid.UUID) error {
	if err := r.db.WithContext(ctx).
		Model(&SiteAPIKeyModel{}).
		Where(&SiteAPIKeyModel{SiteID: siteID, UpstreamModelName: upstreamModelName}).
		Updates(map[string]any{"site_model_id": siteModelID}).Error; err != nil {
		return fmt.Errorf("bind api key models to site model: %w", err)
	}
	return nil
}

func (r SiteAPIKeyModelRepository) BackfillEndpointTypesInTx(ctx context.Context, siteID uuid.UUID, upstreamModelName string, endpointTypes []string) error {
	db := r.db.WithContext(ctx)
	var models []SiteAPIKeyModel
	if err := db.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where(&SiteAPIKeyModel{SiteID: siteID, UpstreamModelName: upstreamModelName}).Find(&models).Error; err != nil {
		return fmt.Errorf("list api key models for endpoint backfill: %w", err)
	}
	for _, model := range models {
		if len(model.Capabilities().SupportedEndpointTypes) > 0 {
			continue
		}
		meta := map[string]json.RawMessage{}
		if len(model.Raw) > 0 && json.Unmarshal(model.Raw, &meta) != nil {
			return fmt.Errorf("invalid model metadata for endpoint backfill")
		}
		if meta == nil {
			meta = map[string]json.RawMessage{}
		}
		meta["supported_endpoint_types"], _ = json.Marshal(endpointTypes)
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if err := db.Model(&SiteAPIKeyModel{}).Where(&SiteAPIKeyModel{ID: model.ID}).
			Updates(map[string]any{"raw": JSON(raw)}).Error; err != nil {
			return fmt.Errorf("backfill api key model endpoint types: %w", err)
		}
	}
	return nil
}

func (r SiteAPIKeyModelRepository) UpdateEnabled(ctx context.Context, siteCredentialID uuid.UUID, upstreamModelName string, enabled bool) (SiteAPIKeyModel, error) {
	var model SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyModel{SiteCredentialID: siteCredentialID, UpstreamModelName: upstreamModelName}).First(&model).Error; err != nil {
		return SiteAPIKeyModel{}, fmt.Errorf("update site api key model enabled: %w", err)
	}
	model.Enabled = enabled
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return SiteAPIKeyModel{}, fmt.Errorf("update site api key model enabled: %w", err)
	}
	return model, nil
}

func (r SiteAPIKeyModelRepository) GetByCredentialModel(ctx context.Context, siteID, credentialID, modelID uuid.UUID) (SiteAPIKeyModel, error) {
	var item SiteAPIKeyModel
	err := r.db.WithContext(ctx).Where(&SiteAPIKeyModel{SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}}).First(&item).Error
	return item, err
}

func (r SiteAPIKeyModelRepository) UpdateEndpointOverride(ctx context.Context, siteCredentialID, siteModelID uuid.UUID, override SiteAPIKeyModelEndpointOverride) (SiteAPIKeyModel, error) {
	var model SiteAPIKeyModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&SiteAPIKeyModel{SiteCredentialID: siteCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}}).First(&model).Error; err != nil {
			return err
		}
		meta := map[string]json.RawMessage{}
		if json.Unmarshal(model.Raw, &meta) != nil || meta == nil {
			return fmt.Errorf("invalid model metadata")
		}
		if override.Mode == "inherit" {
			delete(meta, "endpoint_override")
		} else {
			meta["endpoint_override"], _ = json.Marshal(override)
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		model.Raw = JSON(raw)
		return tx.Model(&SiteAPIKeyModel{}).Where(&SiteAPIKeyModel{ID: model.ID}).Updates(map[string]any{"raw": model.Raw}).Error
	})
	return model, err
}

func (r SiteAPIKeyModelRepository) MarkUnavailable(ctx context.Context, siteCredentialID uuid.UUID, siteModelID uuid.UUID) error {
	var model SiteAPIKeyModel
	err := r.db.WithContext(ctx).Where(&SiteAPIKeyModel{
		SiteCredentialID: siteCredentialID,
		SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
	}).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mark site api key model unavailable: %w", err)
	}
	if !model.Available {
		return nil
	}
	model.Available = false
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return fmt.Errorf("mark site api key model unavailable: %w", err)
	}
	return nil
}
