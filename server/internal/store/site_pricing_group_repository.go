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

type SitePricingGroup struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID       uuid.UUID
	GroupName    string
	DisplayName  sql.NullString
	Ratio        float64
	IsAuto       bool
	Available    bool
	Raw          JSON `gorm:"type:jsonb"`
	LastSyncedAt sql.NullTime
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UpsertSitePricingGroupParams struct {
	SiteID       uuid.UUID
	GroupName    string
	DisplayName  any
	Ratio        float64
	IsAuto       bool
	Available    bool
	Raw          JSON
	LastSyncedAt any
}

type SitePricingGroupRepository struct {
	db *gorm.DB
}

func NewSitePricingGroupRepository(db *gorm.DB) SitePricingGroupRepository {
	return SitePricingGroupRepository{db: db}
}

func (r SitePricingGroupRepository) Upsert(ctx context.Context, params UpsertSitePricingGroupParams) (SitePricingGroup, error) {
	return upsertByKey(ctx, r.db, SitePricingGroup{SiteID: params.SiteID, GroupName: params.GroupName}, "upsert site pricing group", func(item *SitePricingGroup) {
		item.SiteID = params.SiteID
		item.GroupName = params.GroupName
		item.DisplayName = nullStringFromAny(params.DisplayName)
		item.Ratio = params.Ratio
		item.IsAuto = params.IsAuto
		item.Available = params.Available
		item.Raw = jsonDefault(params.Raw, "{}")
		item.LastSyncedAt = nullTimeFromAny(params.LastSyncedAt)
	})
}

func (r SitePricingGroupRepository) ListBySite(ctx context.Context, siteID uuid.UUID) ([]SitePricingGroup, error) {
	var result []SitePricingGroup
	if err := r.db.WithContext(ctx).Where(&SitePricingGroup{SiteID: siteID}).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site pricing groups: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].GroupName < result[j].GroupName
	})
	return result, nil
}

func (r SitePricingGroupRepository) MarkUnavailableExcept(ctx context.Context, siteID uuid.UUID, groupNames []string) error {
	items, err := r.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, name := range groupNames {
		seen[name] = struct{}{}
	}
	now := time.Now()
	db := r.db.WithContext(ctx)
	for _, item := range items {
		if _, ok := seen[item.GroupName]; ok {
			continue
		}
		item.Available = false
		item.LastSyncedAt = sql.NullTime{Time: now, Valid: true}
		if err := db.Save(&item).Error; err != nil {
			return fmt.Errorf("mark stale site pricing groups unavailable: %w", err)
		}
	}
	return nil
}
