package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const apiKeyAccessDetailBatchSize = 1000

type APIKeySiteGroupPermission struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	APIKeyID  uuid.UUID
	GroupID   uuid.UUID
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type APIKeySiteModelPermission struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	APIKeyID    uuid.UUID
	SiteModelID uuid.UUID
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type APIKeySiteModelPermissionDetail struct {
	ID                uuid.UUID
	APIKeyID          uuid.UUID
	SiteModelID       uuid.UUID
	Enabled           bool
	SiteID            uuid.UUID
	SiteName          string
	SiteSlug          string
	SiteType          string
	UpstreamModelName string
	DisplayName       string
	CanonicalModelID  uuid.NullUUID
	CanonicalModelKey sql.NullString
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type APIKeyAccessRepository struct {
	db *gorm.DB
}

func NewAPIKeyAccessRepository(db *gorm.DB) APIKeyAccessRepository {
	return APIKeyAccessRepository{db: db}
}

func (r APIKeyAccessRepository) SetSiteGroupPermissions(ctx context.Context, apiKeyID uuid.UUID, groupIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&APIKeySiteGroupPermission{APIKeyID: apiKeyID}).Delete(&APIKeySiteGroupPermission{}).Error; err != nil {
			return fmt.Errorf("clear api key site group permissions: %w", err)
		}
		seen := map[uuid.UUID]struct{}{}
		for _, groupID := range groupIDs {
			if groupID == uuid.Nil {
				continue
			}
			if _, ok := seen[groupID]; ok {
				continue
			}
			seen[groupID] = struct{}{}
			item := APIKeySiteGroupPermission{APIKeyID: apiKeyID, GroupID: groupID, Enabled: true}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("set api key site group permission: %w", err)
			}
		}
		return nil
	})
}

func (r APIKeyAccessRepository) ListSiteGroupPermissions(ctx context.Context, apiKeyID uuid.UUID) ([]APIKeySiteGroupPermission, error) {
	var items []APIKeySiteGroupPermission
	if err := r.db.WithContext(ctx).Where(&APIKeySiteGroupPermission{APIKeyID: apiKeyID}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list api key site group permissions: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r APIKeyAccessRepository) ListAllSiteGroupPermissions(ctx context.Context) ([]APIKeySiteGroupPermission, error) {
	var items []APIKeySiteGroupPermission
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list all api key site group permissions: %w", err)
	}
	return items, nil
}

func (r APIKeyAccessRepository) EnabledSiteIDsForGroups(ctx context.Context, groupIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	var groupSites []SiteGroupSite
	if err := r.db.WithContext(ctx).Where(map[string]any{"group_id": groupIDs}).Find(&groupSites).Error; err != nil {
		return nil, fmt.Errorf("list api key group site ids: %w", err)
	}
	var groups []SiteGroup
	if err := r.db.WithContext(ctx).Where(map[string]any{"id": groupIDs}).Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("list api key group site ids: %w", err)
	}
	enabledGroups := map[uuid.UUID]struct{}{}
	for _, group := range groups {
		if group.Enabled {
			enabledGroups[group.ID] = struct{}{}
		}
	}
	seen := map[uuid.UUID]struct{}{}
	ids := make([]uuid.UUID, 0, len(groupSites))
	for _, item := range groupSites {
		if _, ok := enabledGroups[item.GroupID]; !ok {
			continue
		}
		if _, ok := seen[item.SiteID]; ok {
			continue
		}
		seen[item.SiteID] = struct{}{}
		ids = append(ids, item.SiteID)
	}
	return ids, nil
}

func (r APIKeyAccessRepository) SetSiteModelPermissions(ctx context.Context, apiKeyID uuid.UUID, siteModelIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&APIKeySiteModelPermission{APIKeyID: apiKeyID}).Delete(&APIKeySiteModelPermission{}).Error; err != nil {
			return fmt.Errorf("clear api key site model permissions: %w", err)
		}
		seen := map[uuid.UUID]struct{}{}
		for _, siteModelID := range siteModelIDs {
			if siteModelID == uuid.Nil {
				continue
			}
			if _, ok := seen[siteModelID]; ok {
				continue
			}
			seen[siteModelID] = struct{}{}
			item := APIKeySiteModelPermission{APIKeyID: apiKeyID, SiteModelID: siteModelID, Enabled: true}
			if err := tx.Create(&item).Error; err != nil {
				return fmt.Errorf("set api key site model permission: %w", err)
			}
		}
		return nil
	})
}

func (r APIKeyAccessRepository) ListSiteModelPermissions(ctx context.Context, apiKeyID uuid.UUID) ([]APIKeySiteModelPermission, error) {
	var items []APIKeySiteModelPermission
	if err := r.db.WithContext(ctx).Where(&APIKeySiteModelPermission{APIKeyID: apiKeyID}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list api key site model permissions: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func (r APIKeyAccessRepository) ListSiteModelPermissionDetails(ctx context.Context, apiKeyID uuid.UUID) ([]APIKeySiteModelPermissionDetail, error) {
	var permissions []APIKeySiteModelPermission
	if err := r.db.WithContext(ctx).Where(&APIKeySiteModelPermission{APIKeyID: apiKeyID}).Find(&permissions).Error; err != nil {
		return nil, fmt.Errorf("list api key site model permission details: %w", err)
	}
	return r.siteModelPermissionDetails(ctx, permissions)
}

func (r APIKeyAccessRepository) ListAllSiteModelPermissionDetails(ctx context.Context) ([]APIKeySiteModelPermissionDetail, error) {
	var permissions []APIKeySiteModelPermission
	if err := r.db.WithContext(ctx).Find(&permissions).Error; err != nil {
		return nil, fmt.Errorf("list all api key site model permission details: %w", err)
	}
	return r.siteModelPermissionDetails(ctx, permissions)
}

func (r APIKeyAccessRepository) siteModelPermissionDetails(ctx context.Context, permissions []APIKeySiteModelPermission) ([]APIKeySiteModelPermissionDetail, error) {
	siteModelIDs := make([]uuid.UUID, 0, len(permissions))
	for _, permission := range permissions {
		siteModelIDs = append(siteModelIDs, permission.SiteModelID)
	}
	siteModels, sites, canonicalModels, err := r.siteModelDetailMaps(ctx, siteModelIDs)
	if err != nil {
		return nil, fmt.Errorf("list api key site model permission details: %w", err)
	}
	rows := make([]APIKeySiteModelPermissionDetail, 0, len(permissions))
	for _, permission := range permissions {
		model := siteModels[permission.SiteModelID]
		site := sites[model.SiteID]
		canonical := CanonicalModel{}
		if model.CanonicalID.Valid {
			canonical = canonicalModels[model.CanonicalID.UUID]
		}
		rows = append(rows, APIKeySiteModelPermissionDetail{
			ID:                permission.ID,
			APIKeyID:          permission.APIKeyID,
			SiteModelID:       permission.SiteModelID,
			Enabled:           permission.Enabled,
			SiteID:            model.SiteID,
			SiteName:          site.Name,
			SiteSlug:          site.Slug,
			SiteType:          site.SiteType,
			UpstreamModelName: model.UpstreamName,
			DisplayName:       model.DisplayName,
			CanonicalModelID:  model.CanonicalID,
			CanonicalModelKey: sql.NullString{String: canonical.ModelKey, Valid: canonical.ID != uuid.Nil},
			CreatedAt:         permission.CreatedAt,
			UpdatedAt:         permission.UpdatedAt,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].SiteName != rows[j].SiteName {
			return rows[i].SiteName < rows[j].SiteName
		}
		return rows[i].UpstreamModelName < rows[j].UpstreamModelName
	})
	return rows, nil
}

func (r APIKeyAccessRepository) EnabledSiteModelIDsForCanonical(ctx context.Context, apiKeyID uuid.UUID, canonicalModelID uuid.UUID) ([]uuid.UUID, error) {
	var permissions []APIKeySiteModelPermission
	if err := r.db.WithContext(ctx).Where(&APIKeySiteModelPermission{APIKeyID: apiKeyID, Enabled: true}).Find(&permissions).Error; err != nil {
		return nil, fmt.Errorf("list api key site model ids: %w", err)
	}
	siteModelIDs := make([]uuid.UUID, 0, len(permissions))
	for _, permission := range permissions {
		siteModelIDs = append(siteModelIDs, permission.SiteModelID)
	}
	siteModels, _, _, err := r.siteModelDetailMaps(ctx, siteModelIDs)
	if err != nil {
		return nil, fmt.Errorf("list api key site model ids: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(permissions))
	for _, permission := range permissions {
		model := siteModels[permission.SiteModelID]
		if model.CanonicalID.Valid && model.CanonicalID.UUID == canonicalModelID {
			ids = append(ids, permission.SiteModelID)
		}
	}
	return ids, nil
}

func (r APIKeyAccessRepository) EnabledSiteModelIDs(ctx context.Context, apiKeyID uuid.UUID) ([]uuid.UUID, error) {
	var rows []APIKeySiteModelPermission
	if err := r.db.WithContext(ctx).Where(&APIKeySiteModelPermission{APIKeyID: apiKeyID, Enabled: true}).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list api key site model ids: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SiteModelID)
	}
	return ids, nil
}

func (r APIKeyAccessRepository) siteModelDetailMaps(ctx context.Context, siteModelIDs []uuid.UUID) (map[uuid.UUID]SiteModel, map[uuid.UUID]Site, map[uuid.UUID]CanonicalModel, error) {
	siteModels := map[uuid.UUID]SiteModel{}
	sites := map[uuid.UUID]Site{}
	canonicalModels := map[uuid.UUID]CanonicalModel{}
	uniqueSiteModelIDs := uniqueUUIDs(siteModelIDs)
	if len(uniqueSiteModelIDs) == 0 {
		return siteModels, sites, canonicalModels, nil
	}
	modelItems := make([]SiteModel, 0, len(uniqueSiteModelIDs))
	if err := forEachUUIDBatch(uniqueSiteModelIDs, func(ids []uuid.UUID) error {
		var items []SiteModel
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": ids}).Find(&items).Error; err != nil {
			return err
		}
		modelItems = append(modelItems, items...)
		return nil
	}); err != nil {
		return nil, nil, nil, err
	}
	siteIDs := make([]uuid.UUID, 0, len(modelItems))
	canonicalIDs := make([]uuid.UUID, 0, len(modelItems))
	for _, model := range modelItems {
		siteModels[model.ID] = model
		siteIDs = append(siteIDs, model.SiteID)
		if model.CanonicalID.Valid {
			canonicalIDs = append(canonicalIDs, model.CanonicalID.UUID)
		}
	}
	if err := forEachUUIDBatch(uniqueUUIDs(siteIDs), func(ids []uuid.UUID) error {
		var items []Site
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": ids}).Find(&items).Error; err != nil {
			return err
		}
		for _, site := range items {
			sites[site.ID] = site
		}
		return nil
	}); err != nil {
		return nil, nil, nil, err
	}
	if err := forEachUUIDBatch(uniqueUUIDs(canonicalIDs), func(ids []uuid.UUID) error {
		var items []CanonicalModel
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": ids}).Find(&items).Error; err != nil {
			return err
		}
		for _, model := range items {
			canonicalModels[model.ID] = model
		}
		return nil
	}); err != nil {
		return nil, nil, nil, err
	}
	return siteModels, sites, canonicalModels, nil
}

func uniqueUUIDs(values []uuid.UUID) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(values))
	seen := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func forEachUUIDBatch(values []uuid.UUID, fn func([]uuid.UUID) error) error {
	for start := 0; start < len(values); start += apiKeyAccessDetailBatchSize {
		end := min(start+apiKeyAccessDetailBatchSize, len(values))
		if err := fn(values[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (r APIKeyAccessRepository) DeleteBySiteModelIDs(ctx context.Context, siteModelIDs []uuid.UUID) (int64, error) {
	if len(siteModelIDs) == 0 {
		return 0, nil
	}
	result := r.db.WithContext(ctx).Where(map[string]any{"site_model_id": siteModelIDs}).Delete(&APIKeySiteModelPermission{})
	if result.Error != nil {
		return 0, fmt.Errorf("delete api key site model permissions by site model ids: %w", result.Error)
	}
	return result.RowsAffected, nil
}
