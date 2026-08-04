package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RateLimitScopeGlobal = "global"
	RateLimitScopeAPIKey = "api_key"

	RateLimitStatusEnabled  = "enabled"
	RateLimitStatusDisabled = "disabled"
)

type GatewayRateLimit struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Scope     string
	APIKeyID  uuid.NullUUID
	Status    string
	RPMLimit  sql.NullInt64
	TPMLimit  sql.NullInt64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type GatewayRateLimitWindow struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	ScopeKey    string
	WindowStart time.Time
	RPMUsed     int64
	TPMReserved int64
	TPMActual   int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UpsertGatewayRateLimitParams struct {
	Scope    string
	APIKeyID any
	Status   string
	RPMLimit any
	TPMLimit any
}

type RateLimitWindowUpdate struct {
	ScopeKey        string
	WindowStart     time.Time
	RPMDelta        int64
	TPMReserveDelta int64
	TPMActualDelta  int64
}

type GatewayRateLimitRepository struct {
	db *gorm.DB
}

func NewGatewayRateLimitRepository(db *gorm.DB) GatewayRateLimitRepository {
	return GatewayRateLimitRepository{db: db}
}

func (r GatewayRateLimitRepository) GetGlobal(ctx context.Context) (GatewayRateLimit, error) {
	var item GatewayRateLimit
	if err := r.db.WithContext(ctx).Where(&GatewayRateLimit{Scope: RateLimitScopeGlobal}).First(&item).Error; err != nil {
		return GatewayRateLimit{}, err
	}
	return item, nil
}

func (r GatewayRateLimitRepository) GetByAPIKey(ctx context.Context, apiKeyID uuid.UUID) (GatewayRateLimit, error) {
	var item GatewayRateLimit
	if err := r.db.WithContext(ctx).Where(&GatewayRateLimit{
		Scope:    RateLimitScopeAPIKey,
		APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true},
	}).First(&item).Error; err != nil {
		return GatewayRateLimit{}, err
	}
	return item, nil
}

func (r GatewayRateLimitRepository) ListAPIKeyLimits(ctx context.Context) ([]GatewayRateLimit, error) {
	var items []GatewayRateLimit
	if err := r.db.WithContext(ctx).Where(&GatewayRateLimit{Scope: RateLimitScopeAPIKey}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list api key rate limits: %w", err)
	}
	return items, nil
}

func (r GatewayRateLimitRepository) Upsert(ctx context.Context, params UpsertGatewayRateLimitParams) (GatewayRateLimit, error) {
	scope := normalizeRateLimitScope(params.Scope)
	if scope == "" {
		return GatewayRateLimit{}, fmt.Errorf("rate limit scope is required")
	}
	status := normalizeRateLimitStatus(params.Status)
	if status == "" {
		return GatewayRateLimit{}, fmt.Errorf("rate limit status must be enabled or disabled")
	}
	apiKeyID := nullUUIDFromAny(params.APIKeyID)
	if scope == RateLimitScopeGlobal {
		apiKeyID = uuid.NullUUID{}
	} else if !apiKeyID.Valid {
		return GatewayRateLimit{}, fmt.Errorf("api_key_id is required for api_key rate limit")
	}

	var item GatewayRateLimit
	query := r.db.WithContext(ctx).Where(&GatewayRateLimit{Scope: scope})
	if scope == RateLimitScopeAPIKey {
		query = query.Where(map[string]any{"api_key_id": apiKeyID.UUID})
	}
	err := query.First(&item).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return GatewayRateLimit{}, fmt.Errorf("upsert gateway rate limit: %w", err)
	}

	item.Scope = scope
	item.APIKeyID = apiKeyID
	item.Status = status
	item.RPMLimit = nullInt64FromAny(params.RPMLimit)
	item.TPMLimit = nullInt64FromAny(params.TPMLimit)
	if err == gorm.ErrRecordNotFound {
		if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
			return GatewayRateLimit{}, fmt.Errorf("upsert gateway rate limit: %w", err)
		}
		return item, nil
	}
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return GatewayRateLimit{}, fmt.Errorf("upsert gateway rate limit: %w", err)
	}
	return item, nil
}

func (r GatewayRateLimitRepository) GetOrCreateWindowForUpdate(ctx context.Context, scopeKey string, windowStart time.Time) (GatewayRateLimitWindow, error) {
	windowStart = windowStart.UTC()
	item := GatewayRateLimitWindow{ScopeKey: scopeKey, WindowStart: windowStart}
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "scope_key"}, {Name: "window_start"}},
			DoNothing: true,
		}).
		Create(&item).Error; err != nil {
		return GatewayRateLimitWindow{}, fmt.Errorf("create gateway rate limit window: %w", err)
	}

	var locked GatewayRateLimitWindow
	if err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where(&GatewayRateLimitWindow{ScopeKey: scopeKey, WindowStart: windowStart}).
		First(&locked).Error; err != nil {
		return GatewayRateLimitWindow{}, fmt.Errorf("lock gateway rate limit window: %w", err)
	}
	return locked, nil
}

func (r GatewayRateLimitRepository) ApplyWindowUpdate(ctx context.Context, update RateLimitWindowUpdate) (GatewayRateLimitWindow, error) {
	item, err := r.GetOrCreateWindowForUpdate(ctx, update.ScopeKey, update.WindowStart)
	if err != nil {
		return GatewayRateLimitWindow{}, err
	}
	item.RPMUsed += update.RPMDelta
	item.TPMReserved += update.TPMReserveDelta
	item.TPMActual += update.TPMActualDelta
	if item.TPMReserved < 0 {
		item.TPMReserved = 0
	}
	if item.RPMUsed < 0 {
		item.RPMUsed = 0
	}
	if item.TPMActual < 0 {
		item.TPMActual = 0
	}
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return GatewayRateLimitWindow{}, fmt.Errorf("update gateway rate limit window: %w", err)
	}
	return item, nil
}

func (r GatewayRateLimitRepository) DeleteWindowsBefore(ctx context.Context, cutoff time.Time) error {
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Lt{Column: clause.Column{Name: "window_start"}, Value: cutoff.UTC()},
		}}).
		Delete(&GatewayRateLimitWindow{}).Error; err != nil {
		return fmt.Errorf("delete old gateway rate limit windows: %w", err)
	}
	return nil
}

func normalizeRateLimitScope(value string) string {
	switch value {
	case RateLimitScopeGlobal, RateLimitScopeAPIKey:
		return value
	default:
		return ""
	}
}

func normalizeRateLimitStatus(value string) string {
	switch value {
	case RateLimitStatusEnabled, RateLimitStatusDisabled:
		return value
	default:
		return ""
	}
}
