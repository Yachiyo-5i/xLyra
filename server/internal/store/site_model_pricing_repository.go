package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteModelPricing struct {
	ID                   uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID               uuid.UUID
	SiteModelID          uuid.NullUUID
	ModelName            string
	GroupName            string
	QuotaType            int
	BillingType          string
	Currency             string
	GroupRatio           float64
	ModelRatio           sql.NullFloat64
	CompletionRatio      sql.NullFloat64
	CacheRatio           sql.NullFloat64
	CreateCacheRatio     sql.NullFloat64
	CreateCache1hRatio   sql.NullFloat64 `gorm:"column:create_cache_1h_ratio"`
	ImageRatio           sql.NullFloat64
	AudioRatio           sql.NullFloat64
	AudioCompletionRatio sql.NullFloat64
	ModelPrice           sql.NullFloat64
	PerRequestValue      sql.NullFloat64
	InputValue           sql.NullFloat64
	OutputValue          sql.NullFloat64
	VendorID             sql.NullInt64
	VendorName           sql.NullString
	VendorIcon           sql.NullString
	Description          sql.NullString
	OwnerBy              sql.NullString
	PricingSource        string
	ManualOverride       bool
	ManualUpdatedAt      sql.NullTime
	ManualNote           sql.NullString
	Available            bool
	Raw                  JSON `gorm:"type:jsonb"`
	LastSyncedAt         sql.NullTime
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type UpsertSiteModelPricingParams struct {
	SiteID               uuid.UUID
	SiteModelID          any
	ModelName            string
	GroupName            string
	QuotaType            int
	BillingType          string
	Currency             string
	GroupRatio           float64
	ModelRatio           any
	CompletionRatio      any
	CacheRatio           any
	CreateCacheRatio     any
	CreateCache1hRatio   any
	ImageRatio           any
	AudioRatio           any
	AudioCompletionRatio any
	ModelPrice           any
	PerRequestValue      any
	InputValue           any
	OutputValue          any
	VendorID             any
	VendorName           any
	VendorIcon           any
	Description          any
	OwnerBy              any
	PricingSource        string
	ManualOverride       bool
	ManualUpdatedAt      any
	ManualNote           any
	PreserveManual       bool
	Available            bool
	Raw                  JSON
	LastSyncedAt         any
}

type SiteModelPricingRepository struct {
	db *gorm.DB
}

func NewSiteModelPricingRepository(db *gorm.DB) SiteModelPricingRepository {
	return SiteModelPricingRepository{db: db}
}

func (r SiteModelPricingRepository) Upsert(ctx context.Context, params UpsertSiteModelPricingParams) (SiteModelPricing, error) {
	db := r.db.WithContext(ctx)
	var item SiteModelPricing
	err := db.Where(&SiteModelPricing{SiteID: params.SiteID, ModelName: params.ModelName, GroupName: params.GroupName}).First(&item).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteModelPricing{}, fmt.Errorf("upsert site model pricing: %w", err)
	}
	if err == nil && params.PreserveManual && item.ManualOverride {
		return item, nil
	}
	item.SiteID = params.SiteID
	item.SiteModelID = nullUUIDFromAny(params.SiteModelID)
	item.ModelName = params.ModelName
	item.GroupName = params.GroupName
	item.QuotaType = params.QuotaType
	item.BillingType = params.BillingType
	item.Currency = params.Currency
	item.GroupRatio = params.GroupRatio
	item.ModelRatio = nullFloatFromAny(params.ModelRatio)
	item.CompletionRatio = nullFloatFromAny(params.CompletionRatio)
	item.CacheRatio = nullFloatFromAny(params.CacheRatio)
	item.CreateCacheRatio = nullFloatFromAny(params.CreateCacheRatio)
	item.CreateCache1hRatio = nullFloatFromAny(params.CreateCache1hRatio)
	item.ImageRatio = nullFloatFromAny(params.ImageRatio)
	item.AudioRatio = nullFloatFromAny(params.AudioRatio)
	item.AudioCompletionRatio = nullFloatFromAny(params.AudioCompletionRatio)
	item.ModelPrice = nullFloatFromAny(params.ModelPrice)
	item.PerRequestValue = nullFloatFromAny(params.PerRequestValue)
	item.InputValue = nullFloatFromAny(params.InputValue)
	item.OutputValue = nullFloatFromAny(params.OutputValue)
	item.VendorID = nullInt64FromAny(params.VendorID)
	item.VendorName = nullStringFromAny(params.VendorName)
	item.VendorIcon = nullStringFromAny(params.VendorIcon)
	item.Description = nullStringFromAny(params.Description)
	item.OwnerBy = nullStringFromAny(params.OwnerBy)
	item.PricingSource = stringDefault(params.PricingSource, "unknown")
	item.ManualOverride = params.ManualOverride
	item.ManualUpdatedAt = nullTimeFromAny(params.ManualUpdatedAt)
	item.ManualNote = nullStringFromAny(params.ManualNote)
	item.Available = params.Available
	item.Raw = jsonDefault(params.Raw, "{}")
	item.LastSyncedAt = nullTimeFromAny(params.LastSyncedAt)
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&item).Error; err != nil {
			return SiteModelPricing{}, fmt.Errorf("upsert site model pricing: %w", err)
		}
		return item, nil
	}
	if err := db.Save(&item).Error; err != nil {
		return SiteModelPricing{}, fmt.Errorf("upsert site model pricing: %w", err)
	}
	return item, nil
}

func (r SiteModelPricingRepository) Save(ctx context.Context, item SiteModelPricing) error {
	return r.db.WithContext(ctx).Save(&item).Error
}

func (r SiteModelPricingRepository) DeleteBySiteModelID(ctx context.Context, siteModelID uuid.UUID) error {
	return r.db.WithContext(ctx).Where(&SiteModelPricing{SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}}).Delete(&SiteModelPricing{}).Error
}

func (r SiteModelPricingRepository) ListBySite(ctx context.Context, siteID uuid.UUID) ([]SiteModelPricing, error) {
	var result []SiteModelPricing
	if err := r.db.WithContext(ctx).Where(&SiteModelPricing{SiteID: siteID}).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site model pricings: %w", err)
	}
	sortSiteModelPricings(result)
	return result, nil
}

func (r SiteModelPricingRepository) ListBySiteModelID(ctx context.Context, siteModelID uuid.UUID) ([]SiteModelPricing, error) {
	var result []SiteModelPricing
	if err := r.db.WithContext(ctx).Where(&SiteModelPricing{SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}}).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site model pricings by model: %w", err)
	}
	return result, nil
}

func (r SiteModelPricingRepository) ListAll(ctx context.Context) ([]SiteModelPricing, error) {
	var result []SiteModelPricing
	if err := r.db.WithContext(ctx).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list all site model pricings: %w", err)
	}
	sortSiteModelPricings(result)
	return result, nil
}

func (r SiteModelPricingRepository) MarkUnavailableExcept(ctx context.Context, siteID uuid.UUID, compositeKeys []string) error {
	items, err := r.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, key := range compositeKeys {
		seen[key] = struct{}{}
	}
	now := time.Now()
	db := r.db.WithContext(ctx)
	for _, item := range items {
		if item.ManualOverride {
			continue
		}
		if !item.Available {
			continue
		}
		key := item.ModelName + string(rune(31)) + item.GroupName
		if _, ok := seen[key]; ok {
			continue
		}
		item.Available = false
		item.LastSyncedAt = sql.NullTime{Time: now, Valid: true}
		if err := db.Save(&item).Error; err != nil {
			return fmt.Errorf("mark stale site model pricings unavailable: %w", err)
		}
	}
	return nil
}

func sortSiteModelPricings(items []SiteModelPricing) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SiteID != items[j].SiteID {
			return items[i].SiteID.String() < items[j].SiteID.String()
		}
		if items[i].ModelName != items[j].ModelName {
			return items[i].ModelName < items[j].ModelName
		}
		return items[i].GroupName < items[j].GroupName
	})
}

type SyncCanonicalFallbackParams struct {
	CanonicalModelID     uuid.UUID
	InputValue           sql.NullFloat64
	OutputValue          sql.NullFloat64
	CacheRatio           sql.NullFloat64
	CreateCacheRatio     sql.NullFloat64
	CreateCache1hRatio   sql.NullFloat64
	AudioRatio           sql.NullFloat64
	AudioCompletionRatio sql.NullFloat64
}

func (r SiteModelPricingRepository) SyncCanonicalFallbackPricing(ctx context.Context, params SyncCanonicalFallbackParams) (int, error) {
	var items []SiteModelPricing
	if err := r.db.WithContext(ctx).Where(&SiteModelPricing{PricingSource: "canonical_fallback"}).Find(&items).Error; err != nil {
		return 0, fmt.Errorf("sync canonical fallback pricing: %w", err)
	}

	canonicalIDStr := params.CanonicalModelID.String()
	now := time.Now()
	db := r.db.WithContext(ctx)
	updated := 0

	for _, item := range items {
		if item.ManualOverride {
			continue
		}
		if canonicalFallbackModelID(item.Raw) != canonicalIDStr {
			continue
		}
		item.InputValue = params.InputValue
		item.OutputValue = params.OutputValue
		item.CacheRatio = params.CacheRatio
		item.CreateCacheRatio = params.CreateCacheRatio
		item.CreateCache1hRatio = params.CreateCache1hRatio
		item.AudioRatio = params.AudioRatio
		item.AudioCompletionRatio = params.AudioCompletionRatio
		item.LastSyncedAt = sql.NullTime{Time: now, Valid: true}
		if err := db.Save(&item).Error; err != nil {
			return updated, fmt.Errorf("sync canonical fallback pricing: %w", err)
		}
		updated++
	}
	return updated, nil
}

func canonicalFallbackModelID(raw JSON) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	id, _ := m["canonical_model_id"].(string)
	return id
}
