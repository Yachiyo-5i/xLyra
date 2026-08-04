package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteModel struct {
	ID              uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID          uuid.UUID
	CanonicalID     uuid.NullUUID `gorm:"column:canonical_model_id"`
	UpstreamName    string        `gorm:"column:upstream_model_name"`
	DisplayName     string
	Capabilities    JSON `gorm:"type:jsonb"`
	Status          string
	MatchSource     string       `gorm:"column:canonical_match_source"`
	MatchConfidence int          `gorm:"column:canonical_match_confidence"`
	MatchedAt       sql.NullTime `gorm:"column:canonical_matched_at"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type UpsertSiteModelParams struct {
	SiteID       uuid.UUID
	UpstreamName string
	DisplayName  string
	Capabilities JSON
	Status       string
}

type SiteModelRepository struct {
	db *gorm.DB
}

func NewSiteModelRepository(db *gorm.DB) SiteModelRepository {
	return SiteModelRepository{db: db}
}

func (r SiteModelRepository) Upsert(ctx context.Context, params UpsertSiteModelParams) (SiteModel, error) {
	db := r.db.WithContext(ctx)
	var model SiteModel
	err := db.Where(&SiteModel{SiteID: params.SiteID, UpstreamName: params.UpstreamName}).First(&model).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteModel{}, fmt.Errorf("upsert site model: %w", err)
	}
	model.SiteID = params.SiteID
	model.UpstreamName = params.UpstreamName
	model.DisplayName = params.DisplayName
	model.Capabilities = jsonDefault(params.Capabilities, "{}")
	if model.Status != "disabled" {
		model.Status = params.Status
	}
	if err == gorm.ErrRecordNotFound {
		model.Status = params.Status
		if err := db.Create(&model).Error; err != nil {
			return SiteModel{}, fmt.Errorf("upsert site model: %w", err)
		}
		return model, nil
	}
	if err := db.Save(&model).Error; err != nil {
		return SiteModel{}, fmt.Errorf("upsert site model: %w", err)
	}
	return model, nil
}

func (r SiteModelRepository) MarkUnavailableExcept(ctx context.Context, siteID uuid.UUID, upstreamNames []string) error {
	models, err := r.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, name := range upstreamNames {
		seen[name] = struct{}{}
	}
	db := r.db.WithContext(ctx)
	for _, model := range models {
		if model.Status == "disabled" {
			continue
		}
		if SiteModelManual(model) {
			continue
		}
		if _, ok := seen[model.UpstreamName]; ok {
			continue
		}
		model.Status = "unavailable"
		if err := db.Save(&model).Error; err != nil {
			return fmt.Errorf("mark stale site models unavailable: %w", err)
		}
	}
	return nil
}

func (r SiteModelRepository) ListBySite(ctx context.Context, siteID uuid.UUID) ([]SiteModel, error) {
	var models []SiteModel
	if err := r.db.WithContext(ctx).Where(&SiteModel{SiteID: siteID}).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list site models: %w", err)
	}
	sortSiteModels(models)
	return models, nil
}

func (r SiteModelRepository) ListAll(ctx context.Context) ([]SiteModel, error) {
	var models []SiteModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list site models: %w", err)
	}
	sortSiteModels(models)
	return models, nil
}

func (r SiteModelRepository) UpdateStatus(ctx context.Context, siteID uuid.UUID, modelID uuid.UUID, status string) (SiteModel, error) {
	var model SiteModel
	if err := r.db.WithContext(ctx).Where(&SiteModel{ID: modelID, SiteID: siteID}).First(&model).Error; err != nil {
		return SiteModel{}, fmt.Errorf("update site model status: %w", err)
	}
	model.Status = status
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return SiteModel{}, fmt.Errorf("update site model status: %w", err)
	}
	return model, nil
}

func (r SiteModelRepository) UpdateStatuses(ctx context.Context, siteID uuid.UUID, modelIDs []uuid.UUID, status string) ([]SiteModel, error) {
	updated := make([]SiteModel, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		model, err := r.UpdateStatus(ctx, siteID, modelID, status)
		if err != nil {
			return nil, err
		}
		updated = append(updated, model)
	}
	sortSiteModels(updated)
	return updated, nil
}

func (r SiteModelRepository) GetByID(ctx context.Context, modelID uuid.UUID) (SiteModel, error) {
	var model SiteModel
	if err := r.db.WithContext(ctx).Where(&SiteModel{ID: modelID}).First(&model).Error; err != nil {
		return SiteModel{}, fmt.Errorf("get site model: %w", err)
	}
	return model, nil
}

func (r SiteModelRepository) Delete(ctx context.Context, modelID uuid.UUID) error {
	result := r.db.WithContext(ctx).Where(&SiteModel{ID: modelID}).Delete(&SiteModel{})
	if result.Error != nil {
		return fmt.Errorf("delete site model: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("site model was not found")
	}
	return nil
}

func (r SiteModelRepository) UpdateCanonical(ctx context.Context, modelID uuid.UUID, canonicalID any, source string, confidence int, matchedAt any) (SiteModel, error) {
	model, err := r.GetByID(ctx, modelID)
	if err != nil {
		return SiteModel{}, err
	}
	model.CanonicalID = nullUUIDFromAny(canonicalID)
	model.MatchSource = source
	model.MatchConfidence = confidence
	model.MatchedAt = nullTimeFromAny(matchedAt)
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return SiteModel{}, fmt.Errorf("update site model canonical mapping: %w", err)
	}
	return model, nil
}

func (r SiteModelRepository) ListByCanonical(ctx context.Context, canonicalID uuid.UUID) ([]SiteModel, error) {
	var models []SiteModel
	if err := r.db.WithContext(ctx).Where(map[string]any{"canonical_model_id": canonicalID}).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list site models by canonical model: %w", err)
	}
	sortSiteModels(models)
	return models, nil
}

func sortSiteModels(models []SiteModel) {
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].UpstreamName < models[j].UpstreamName
	})
}

func SiteModelManual(model SiteModel) bool {
	if len(model.Capabilities) == 0 {
		return false
	}
	values := map[string]any{}
	if err := json.Unmarshal(model.Capabilities, &values); err != nil {
		return false
	}
	if manual, ok := values["manual"].(bool); ok && manual {
		return true
	}
	source, _ := values["source"].(string)
	return strings.EqualFold(strings.TrimSpace(source), "manual")
}
