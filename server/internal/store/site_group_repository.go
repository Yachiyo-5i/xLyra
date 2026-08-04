package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteGroup struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string
	Slug        string
	Description string
	Enabled     bool
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SiteGroupSite struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	GroupID   uuid.UUID
	SiteID    uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpsertSiteGroupParams struct {
	ID          uuid.UUID
	Name        string
	Slug        string
	Description string
	Enabled     bool
	SortOrder   int
}

type SiteGroupRepository struct {
	db *gorm.DB
}

func NewSiteGroupRepository(db *gorm.DB) SiteGroupRepository {
	return SiteGroupRepository{db: db}
}

func (r SiteGroupRepository) List(ctx context.Context) ([]SiteGroup, error) {
	var items []SiteGroup
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list site groups: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (r SiteGroupRepository) GetByID(ctx context.Context, id uuid.UUID) (SiteGroup, error) {
	var item SiteGroup
	if err := r.db.WithContext(ctx).Where(&SiteGroup{ID: id}).First(&item).Error; err != nil {
		return SiteGroup{}, fmt.Errorf("get site group: %w", err)
	}
	return item, nil
}

func (r SiteGroupRepository) Create(ctx context.Context, params UpsertSiteGroupParams) (SiteGroup, error) {
	item := SiteGroup{
		Name:        strings.TrimSpace(params.Name),
		Slug:        strings.TrimSpace(params.Slug),
		Description: strings.TrimSpace(params.Description),
		Enabled:     params.Enabled,
		SortOrder:   params.SortOrder,
	}
	if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
		return SiteGroup{}, fmt.Errorf("create site group: %w", err)
	}
	return item, nil
}

func (r SiteGroupRepository) Update(ctx context.Context, params UpsertSiteGroupParams) (SiteGroup, error) {
	item, err := r.GetByID(ctx, params.ID)
	if err != nil {
		return SiteGroup{}, err
	}
	item.Name = strings.TrimSpace(params.Name)
	item.Slug = strings.TrimSpace(params.Slug)
	item.Description = strings.TrimSpace(params.Description)
	item.Enabled = params.Enabled
	item.SortOrder = params.SortOrder
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return SiteGroup{}, fmt.Errorf("update site group: %w", err)
	}
	return item, nil
}

func (r SiteGroupRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Where(&SiteGroup{ID: id}).Delete(&SiteGroup{})
	if result.Error != nil {
		return fmt.Errorf("delete site group: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("delete site group: not found")
	}
	return nil
}

func (r SiteGroupRepository) ListGroupSites(ctx context.Context, groupID uuid.UUID) ([]SiteGroupSite, error) {
	var items []SiteGroupSite
	if err := r.db.WithContext(ctx).Where(&SiteGroupSite{GroupID: groupID}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list site group sites: %w", err)
	}
	return items, nil
}

func (r SiteGroupRepository) ListAllGroupSites(ctx context.Context) ([]SiteGroupSite, error) {
	var items []SiteGroupSite
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list site group sites: %w", err)
	}
	return items, nil
}

func (r SiteGroupRepository) SetGroupSites(ctx context.Context, groupID uuid.UUID, siteIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&SiteGroupSite{GroupID: groupID}).Delete(&SiteGroupSite{}).Error; err != nil {
			return fmt.Errorf("clear site group sites: %w", err)
		}
		seen := map[uuid.UUID]struct{}{}
		for _, siteID := range siteIDs {
			if siteID == uuid.Nil {
				continue
			}
			if _, ok := seen[siteID]; ok {
				continue
			}
			seen[siteID] = struct{}{}
			if err := tx.Create(&SiteGroupSite{GroupID: groupID, SiteID: siteID}).Error; err != nil {
				return fmt.Errorf("set site group site: %w", err)
			}
		}
		return nil
	})
}
