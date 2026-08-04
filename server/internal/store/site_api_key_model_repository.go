package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
	model.SiteModelID = nullUUIDFromAny(params.SiteModelID)
	model.UpstreamModelName = params.UpstreamModelName
	model.DisplayName = params.DisplayName
	model.Available = params.Available
	if err == gorm.ErrRecordNotFound {
		model.Enabled = params.Enabled
	}
	model.Raw = jsonDefault(params.Raw, "{}")
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
		if _, ok := seen[model.UpstreamModelName]; ok {
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
