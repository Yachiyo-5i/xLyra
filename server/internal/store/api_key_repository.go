package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
)

type APIKey struct {
	ID                     uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name                   string
	KeyPrefix              string
	KeyHash                string `gorm:"column:key_hash"`
	EncryptedSecret        sql.NullString
	MaskedKey              string
	KeyKind                string `gorm:"default:generated;not null"`
	Scope                  string
	Status                 string
	ModelPolicy            string
	SitePolicy             string
	ModelMappings          JSON `gorm:"type:jsonb;default:'[]'::jsonb"`
	ImageToolBridge        JSON `gorm:"type:jsonb;default:'{}'::jsonb"`
	QuotaLimit             sql.NullFloat64
	QuotaUsed              float64
	QuotaUnlimited         bool
	QuotaDailyLimit        sql.NullFloat64 `gorm:"type:numeric(18,8)"`
	QuotaDailyUsed         float64         `gorm:"type:numeric(18,8);default:0;not null"`
	QuotaDailyUnlimited    bool            `gorm:"default:true;not null"`
	QuotaDailyWindowStart  *time.Time
	QuotaWeeklyLimit       sql.NullFloat64 `gorm:"type:numeric(18,8)"`
	QuotaWeeklyUsed        float64         `gorm:"type:numeric(18,8);default:0;not null"`
	QuotaWeeklyUnlimited   bool            `gorm:"default:true;not null"`
	QuotaWeeklyWindowStart *time.Time
	CreatedByAdminID       *uuid.UUID
	LastUsedAt             *time.Time
	ExpiresAt              *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type APIKeyImageBridge struct {
	Enabled  bool       `json:"enabled"`
	Model    string     `json:"model"`
	SiteID   *uuid.UUID `json:"site_id,omitempty"`
	MaxCalls int        `json:"max_calls,omitempty"`
}

const DefaultImageBridgeMaxCalls = 2

func (k APIKey) ImageBridge() (APIKeyImageBridge, bool) {
	if len(k.ImageToolBridge) == 0 {
		return APIKeyImageBridge{}, false
	}
	var cfg APIKeyImageBridge
	if err := json.Unmarshal(k.ImageToolBridge, &cfg); err != nil {
		return APIKeyImageBridge{}, false
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if !cfg.Enabled || cfg.Model == "" {
		return APIKeyImageBridge{}, false
	}
	if cfg.MaxCalls <= 0 {
		cfg.MaxCalls = DefaultImageBridgeMaxCalls
	}
	return cfg, true
}

type APIKeySitePermission struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	APIKeyID  uuid.UUID
	SiteID    uuid.UUID
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateAPIKeyParams struct {
	Name                 string
	KeyPrefix            string
	KeyHash              string
	EncryptedSecret      any
	MaskedKey            string
	KeyKind              string
	Scope                string
	Status               string
	ModelPolicy          string
	SitePolicy           string
	ModelMappings        any
	ImageToolBridge      any
	QuotaLimit           any
	QuotaUnlimited       bool
	QuotaDailyLimit      any
	QuotaDailyUnlimited  bool
	QuotaWeeklyLimit     any
	QuotaWeeklyUnlimited bool
	ExpiresAt            any
	CreatedByAdminID     uuid.UUID
}

type UpdateAPIKeyParams struct {
	ID                   uuid.UUID
	Name                 string
	Status               string
	ModelPolicy          string
	SitePolicy           string
	ModelMappings        any
	ImageToolBridge      any
	QuotaLimit           any
	QuotaUnlimited       bool
	QuotaDailyLimit      any
	QuotaDailyUnlimited  bool
	QuotaWeeklyLimit     any
	QuotaWeeklyUnlimited bool
	ExpiresAt            any
}

type APIKeyRepository struct {
	db *gorm.DB
}

type APIKeyQuotaResetResult struct {
	APIKey           APIKey
	DailyUsedBefore  float64
	WeeklyUsedBefore float64
}

type APIKeyQuotaExceededError struct {
	Scope   string
	ResetAt *time.Time
}

func (e *APIKeyQuotaExceededError) Error() string {
	return "api key " + e.Scope + " quota exhausted"
}

func NewAPIKeyRepository(db *gorm.DB) APIKeyRepository {
	return APIKeyRepository{db: db}
}

func (r APIKeyRepository) Create(ctx context.Context, params CreateAPIKeyParams) (APIKey, error) {
	if params.Scope == "" {
		params.Scope = "gateway"
	}
	if params.Status == "" {
		params.Status = "active"
	}
	if params.KeyKind == "" {
		params.KeyKind = "generated"
	}
	if params.ModelPolicy == "" {
		params.ModelPolicy = "allow_all"
	}
	if params.SitePolicy == "" {
		params.SitePolicy = "allow_all"
	}
	apiKey := APIKey{
		Name:                 params.Name,
		KeyPrefix:            params.KeyPrefix,
		KeyHash:              params.KeyHash,
		EncryptedSecret:      nullStringFromAny(params.EncryptedSecret),
		MaskedKey:            params.MaskedKey,
		KeyKind:              params.KeyKind,
		Scope:                params.Scope,
		Status:               params.Status,
		ModelPolicy:          params.ModelPolicy,
		SitePolicy:           params.SitePolicy,
		ModelMappings:        jsonDefault(jsonFromAny(params.ModelMappings, "[]"), "[]"),
		ImageToolBridge:      jsonDefault(jsonFromAny(params.ImageToolBridge, "{}"), "{}"),
		QuotaLimit:           nullFloatFromAny(params.QuotaLimit),
		QuotaUsed:            0,
		QuotaUnlimited:       params.QuotaUnlimited,
		QuotaDailyLimit:      nullFloatFromAny(params.QuotaDailyLimit),
		QuotaDailyUsed:       0,
		QuotaDailyUnlimited:  params.QuotaDailyUnlimited,
		QuotaWeeklyLimit:     nullFloatFromAny(params.QuotaWeeklyLimit),
		QuotaWeeklyUsed:      0,
		QuotaWeeklyUnlimited: params.QuotaWeeklyUnlimited,
		ExpiresAt:            timePtrFromAny(params.ExpiresAt),
		CreatedByAdminID:     uuidPtrFromAny(params.CreatedByAdminID),
	}
	if err := r.db.WithContext(ctx).Create(&apiKey).Error; err != nil {
		return APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return apiKey, nil
}

type activeLookup struct {
	KeyKind       string
	RequireQuota  bool
	TouchLastUsed bool
}

func (r APIKeyRepository) GetActiveByHash(ctx context.Context, keyHash string, now time.Time) (APIKey, error) {
	return r.lookupActive(ctx, keyHash, now, activeLookup{RequireQuota: true, TouchLastUsed: true})
}

func (r APIKeyRepository) GetActiveGeneratedByHash(ctx context.Context, keyHash string, now time.Time) (APIKey, error) {
	return r.lookupActive(ctx, keyHash, now, activeLookup{KeyKind: "generated", RequireQuota: true, TouchLastUsed: true})
}

func (r APIKeyRepository) GetActiveIdentityByHash(ctx context.Context, keyHash string, now time.Time) (APIKey, error) {
	return r.lookupActive(ctx, keyHash, now, activeLookup{})
}

func (r APIKeyRepository) GetActiveGeneratedIdentityByHash(ctx context.Context, keyHash string, now time.Time) (APIKey, error) {
	return r.lookupActive(ctx, keyHash, now, activeLookup{KeyKind: "generated"})
}

func (r APIKeyRepository) lookupActive(ctx context.Context, keyHash string, now time.Time, opts activeLookup) (APIKey, error) {
	var apiKey APIKey
	query := r.db.WithContext(ctx).Where(&APIKey{KeyHash: keyHash})
	if opts.KeyKind != "" {
		query = query.Where(&APIKey{KeyKind: opts.KeyKind})
	}
	if err := query.First(&apiKey).Error; err != nil {
		return APIKey{}, err
	}
	if apiKey.Status != "active" {
		return APIKey{}, gorm.ErrRecordNotFound
	}
	if apiKey.ExpiresAt != nil && !apiKey.ExpiresAt.After(now) {
		return APIKey{}, gorm.ErrRecordNotFound
	}
	if opts.RequireQuota {
		if quotaErr := apiKey.QuotaExceededErrorAt(now, config.TimeZoneOrDefault()); quotaErr != nil {
			return APIKey{}, quotaErr
		}
	}
	if opts.TouchLastUsed {
		if apiKeyLastUsed.shouldWrite(apiKey.ID, now) {
			if err := r.db.WithContext(ctx).Model(&apiKey).Updates(map[string]any{"last_used_at": now}).Error; err != nil {
				return APIKey{}, fmt.Errorf("update api key last used: %w", err)
			}
		}
		apiKey.LastUsedAt = &now
	}
	return apiKey, nil
}

func (r APIKeyRepository) ExistsByHash(ctx context.Context, keyHash string) (bool, error) {
	return r.existsByHash(ctx, keyHash, "")
}

func (r APIKeyRepository) ExistsGeneratedByHash(ctx context.Context, keyHash string) (bool, error) {
	return r.existsByHash(ctx, keyHash, "generated")
}

func (r APIKeyRepository) existsByHash(ctx context.Context, keyHash string, keyKind string) (bool, error) {
	query := r.db.WithContext(ctx).Model(&APIKey{}).Where(&APIKey{KeyHash: keyHash})
	if keyKind != "" {
		query = query.Where(&APIKey{KeyKind: keyKind})
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, fmt.Errorf("check api key hash: %w", err)
	}
	return count > 0, nil
}

func (r APIKeyRepository) GetByID(ctx context.Context, id uuid.UUID) (APIKey, error) {
	var apiKey APIKey
	if err := r.db.WithContext(ctx).Where(&APIKey{ID: id}).First(&apiKey).Error; err != nil {
		return APIKey{}, err
	}
	return apiKey, nil
}

func (r APIKeyRepository) List(ctx context.Context) ([]APIKey, error) {
	var items []APIKey
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r APIKeyRepository) ListByIDs(ctx context.Context, ids []uuid.UUID) ([]APIKey, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var items []APIKey
	if err := r.db.WithContext(ctx).Find(&items, ids).Error; err != nil {
		return nil, fmt.Errorf("list api keys by ids: %w", err)
	}
	return items, nil
}

func (r APIKeyRepository) Update(ctx context.Context, params UpdateAPIKeyParams) (APIKey, error) {
	apiKey, err := r.GetByID(ctx, params.ID)
	if err != nil {
		return APIKey{}, err
	}
	apiKey.Name = params.Name
	apiKey.Status = params.Status
	apiKey.ModelPolicy = params.ModelPolicy
	apiKey.SitePolicy = params.SitePolicy
	apiKey.ModelMappings = jsonDefault(jsonFromAny(params.ModelMappings, string(apiKey.ModelMappings)), "[]")
	apiKey.ImageToolBridge = jsonDefault(jsonFromAny(params.ImageToolBridge, string(apiKey.ImageToolBridge)), "{}")
	apiKey.QuotaLimit = nullFloatFromAny(params.QuotaLimit)
	apiKey.QuotaUnlimited = params.QuotaUnlimited
	apiKey.QuotaDailyLimit = nullFloatFromAny(params.QuotaDailyLimit)
	apiKey.QuotaDailyUnlimited = params.QuotaDailyUnlimited
	apiKey.QuotaWeeklyLimit = nullFloatFromAny(params.QuotaWeeklyLimit)
	apiKey.QuotaWeeklyUnlimited = params.QuotaWeeklyUnlimited
	apiKey.ExpiresAt = timePtrFromAny(params.ExpiresAt)
	if err := r.db.WithContext(ctx).Save(&apiKey).Error; err != nil {
		return APIKey{}, fmt.Errorf("update api key: %w", err)
	}
	return apiKey, nil
}

func (r APIKeyRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Where(&APIKey{ID: id}).Delete(&APIKey{})
	if result.Error != nil {
		return fmt.Errorf("delete api key: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("delete api key: not found")
	}
	return nil
}

func (r APIKeyRepository) AddUsage(ctx context.Context, id uuid.UUID, amount float64) (APIKey, error) {
	apiKey, err := r.GetByID(ctx, id)
	if err != nil {
		return APIKey{}, err
	}
	apiKey.QuotaUsed += amount
	if err := r.db.WithContext(ctx).Model(&apiKey).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "quota_used"}}}).
		Updates(map[string]any{
			"quota_used": gorm.Expr("quota_used + ?", amount),
		}).Error; err != nil {
		return APIKey{}, fmt.Errorf("add api key usage: %w", err)
	}
	return apiKey, nil
}

func (r APIKeyRepository) AddUsageAt(ctx context.Context, id uuid.UUID, amount float64, now time.Time, timeZone config.TimeZone) (APIKey, error) {
	var apiKey APIKey
	db := r.db.WithContext(ctx)
	if err := db.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Where(&APIKey{ID: id}).First(&apiKey).Error; err != nil {
		return APIKey{}, err
	}
	dailyStart := timeZone.StartOfDay(now)
	if apiKey.QuotaDailyWindowStart == nil || !apiKey.QuotaDailyWindowStart.Equal(dailyStart) {
		apiKey.QuotaDailyWindowStart = &dailyStart
		apiKey.QuotaDailyUsed = 0
	}
	weeklyStart := timeZone.StartOfWeek(now)
	if apiKey.QuotaWeeklyWindowStart == nil || !apiKey.QuotaWeeklyWindowStart.Equal(weeklyStart) {
		apiKey.QuotaWeeklyWindowStart = &weeklyStart
		apiKey.QuotaWeeklyUsed = 0
	}
	apiKey.QuotaUsed += amount
	apiKey.QuotaDailyUsed += amount
	apiKey.QuotaWeeklyUsed += amount
	if err := db.Model(&apiKey).Updates(map[string]any{
		"quota_used":                apiKey.QuotaUsed,
		"quota_daily_used":          apiKey.QuotaDailyUsed,
		"quota_daily_window_start":  apiKey.QuotaDailyWindowStart,
		"quota_weekly_used":         apiKey.QuotaWeeklyUsed,
		"quota_weekly_window_start": apiKey.QuotaWeeklyWindowStart,
	}).Error; err != nil {
		return APIKey{}, fmt.Errorf("add api key usage: %w", err)
	}
	return apiKey, nil
}

func (r APIKeyRepository) IncreaseQuotaLimit(ctx context.Context, id uuid.UUID, amount float64) (APIKey, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return APIKey{}, fmt.Errorf("quota increase amount must be a finite number greater than 0")
	}
	var updated APIKey
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var apiKey APIKey
		if err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Where(&APIKey{ID: id}).First(&apiKey).Error; err != nil {
			return err
		}
		if apiKey.QuotaUnlimited || !apiKey.QuotaLimit.Valid {
			return fmt.Errorf("total quota limit is not enabled")
		}
		increased := apiKey.QuotaLimit.Float64 + amount
		if math.IsNaN(increased) || math.IsInf(increased, 0) {
			return fmt.Errorf("increased total quota limit is out of range")
		}
		apiKey.QuotaLimit.Float64 = increased
		if err := tx.Save(&apiKey).Error; err != nil {
			return fmt.Errorf("increase api key quota limit: %w", err)
		}
		updated = apiKey
		return nil
	})
	return updated, err
}

func (r APIKeyRepository) ResetPeriodicQuota(ctx context.Context, id uuid.UUID, resetDaily bool, resetWeekly bool, now time.Time, timeZone config.TimeZone) (APIKeyQuotaResetResult, error) {
	var result APIKeyQuotaResetResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var apiKey APIKey
		if err := tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).Where(&APIKey{ID: id}).First(&apiKey).Error; err != nil {
			return err
		}
		result.DailyUsedBefore = apiKey.EffectiveDailyQuotaUsed(now, timeZone)
		result.WeeklyUsedBefore = apiKey.EffectiveWeeklyQuotaUsed(now, timeZone)
		if resetDaily {
			dailyStart := timeZone.StartOfDay(now)
			apiKey.QuotaDailyWindowStart = &dailyStart
			apiKey.QuotaDailyUsed = 0
		}
		if resetWeekly {
			weeklyStart := timeZone.StartOfWeek(now)
			apiKey.QuotaWeeklyWindowStart = &weeklyStart
			apiKey.QuotaWeeklyUsed = 0
		}
		if err := tx.Save(&apiKey).Error; err != nil {
			return fmt.Errorf("reset api key periodic quota: %w", err)
		}
		result.APIKey = apiKey
		return nil
	})
	return result, err
}

func (k APIKey) QuotaExceeded() bool {
	return !k.QuotaUnlimited && k.QuotaLimit.Valid && k.QuotaUsed >= k.QuotaLimit.Float64
}

func (k APIKey) EffectiveDailyQuotaUsed(now time.Time, timeZone config.TimeZone) float64 {
	if k.QuotaDailyWindowStart == nil || !k.QuotaDailyWindowStart.Equal(timeZone.StartOfDay(now)) {
		return 0
	}
	return k.QuotaDailyUsed
}

func (k APIKey) EffectiveWeeklyQuotaUsed(now time.Time, timeZone config.TimeZone) float64 {
	if k.QuotaWeeklyWindowStart == nil || !k.QuotaWeeklyWindowStart.Equal(timeZone.StartOfWeek(now)) {
		return 0
	}
	return k.QuotaWeeklyUsed
}

func (k APIKey) QuotaExceededAt(now time.Time, timeZone config.TimeZone) bool {
	return k.QuotaExceededErrorAt(now, timeZone) != nil
}

func (k APIKey) QuotaExceededErrorAt(now time.Time, timeZone config.TimeZone) *APIKeyQuotaExceededError {
	if !k.QuotaUnlimited && (!k.QuotaLimit.Valid || k.QuotaUsed >= k.QuotaLimit.Float64) {
		return &APIKeyQuotaExceededError{Scope: "total"}
	}
	if !k.QuotaDailyUnlimited && k.QuotaDailyLimit.Valid && k.EffectiveDailyQuotaUsed(now, timeZone) >= k.QuotaDailyLimit.Float64 {
		resetAt := timeZone.StartOfDay(now).AddDate(0, 0, 1)
		return &APIKeyQuotaExceededError{Scope: "daily", ResetAt: &resetAt}
	}
	if !k.QuotaWeeklyUnlimited && k.QuotaWeeklyLimit.Valid && k.EffectiveWeeklyQuotaUsed(now, timeZone) >= k.QuotaWeeklyLimit.Float64 {
		resetAt := timeZone.StartOfWeek(now).AddDate(0, 0, 7)
		return &APIKeyQuotaExceededError{Scope: "weekly", ResetAt: &resetAt}
	}
	return nil
}

func (r APIKeyRepository) SetSitePermissions(ctx context.Context, apiKeyID uuid.UUID, siteIDs []uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where(&APIKeySitePermission{APIKeyID: apiKeyID}).Delete(&APIKeySitePermission{}).Error; err != nil {
			return fmt.Errorf("clear api key site permissions: %w", err)
		}
		for _, siteID := range siteIDs {
			permission := APIKeySitePermission{
				APIKeyID: apiKeyID,
				SiteID:   siteID,
				Enabled:  true,
			}
			if err := tx.Create(&permission).Error; err != nil {
				return fmt.Errorf("set api key site permission: %w", err)
			}
		}
		return nil
	})
}

func (r APIKeyRepository) ListSitePermissions(ctx context.Context, apiKeyID uuid.UUID) ([]APIKeySitePermission, error) {
	var items []APIKeySitePermission
	if err := r.db.WithContext(ctx).Where(&APIKeySitePermission{APIKeyID: apiKeyID}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list api key site permissions: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].SiteID.String() < items[j].SiteID.String()
	})
	return items, nil
}

func (r APIKeyRepository) ListAllSitePermissions(ctx context.Context) ([]APIKeySitePermission, error) {
	var items []APIKeySitePermission
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list all api key site permissions: %w", err)
	}
	return items, nil
}

func (r APIKeyRepository) IsSiteAllowed(ctx context.Context, apiKeyID uuid.UUID, siteID uuid.UUID) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&APIKeySitePermission{}).Where(&APIKeySitePermission{
		APIKeyID: apiKeyID,
		SiteID:   siteID,
		Enabled:  true,
	}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check api key site permission: %w", err)
	}
	return count > 0, nil
}

func (r APIKeyRepository) AllowedSiteIDs(ctx context.Context, apiKeyID uuid.UUID) ([]uuid.UUID, error) {
	var items []APIKeySitePermission
	if err := r.db.WithContext(ctx).Where(&APIKeySitePermission{APIKeyID: apiKeyID, Enabled: true}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list allowed site ids: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.SiteID)
	}
	return ids, nil
}
