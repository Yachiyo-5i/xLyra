package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteModelEndpointOverride struct {
	ID            uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteModelID   uuid.UUID `gorm:"uniqueIndex"`
	Mode          string
	EndpointTypes JSON `gorm:"type:jsonb"`
	Reason        string
}

type SiteModelEndpointOverrideRepository struct{ db *gorm.DB }

func NewSiteModelEndpointOverrideRepository(db *gorm.DB) SiteModelEndpointOverrideRepository {
	return SiteModelEndpointOverrideRepository{db: db}
}
func (SiteModelEndpointOverride) TableName() string { return "site_model_endpoint_overrides" }
func (r SiteModelEndpointOverrideRepository) Get(ctx context.Context, siteModelID uuid.UUID) (SiteModelEndpointOverride, error) {
	var item SiteModelEndpointOverride
	err := r.db.WithContext(ctx).Where(&SiteModelEndpointOverride{SiteModelID: siteModelID}).First(&item).Error
	return item, err
}
func (r SiteModelEndpointOverrideRepository) Upsert(ctx context.Context, siteModelID uuid.UUID, mode string, endpoints []string, reason string) (SiteModelEndpointOverride, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "inherit"
	}
	if mode != "inherit" && mode != "allowlist" && mode != "disabled" {
		return SiteModelEndpointOverride{}, fmt.Errorf("invalid endpoint override mode %q", mode)
	}
	if mode == "allowlist" && len(endpoints) == 0 {
		return SiteModelEndpointOverride{}, fmt.Errorf("endpoint_types is required for allowlist")
	}
	raw, _ := json.Marshal(endpoints)
	var item SiteModelEndpointOverride
	err := r.db.WithContext(ctx).Where(&SiteModelEndpointOverride{SiteModelID: siteModelID}).First(&item).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return item, err
	}
	item.SiteModelID = siteModelID
	item.Mode = mode
	item.EndpointTypes = JSON(raw)
	item.Reason = reason
	if err == gorm.ErrRecordNotFound {
		err = r.db.WithContext(ctx).Create(&item).Error
	} else {
		err = r.db.WithContext(ctx).Save(&item).Error
	}
	return item, err
}
func (r SiteModelEndpointOverrideRepository) Delete(ctx context.Context, siteModelID uuid.UUID) error {
	return r.db.WithContext(ctx).Where(&SiteModelEndpointOverride{SiteModelID: siteModelID}).Delete(&SiteModelEndpointOverride{}).Error
}
