package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UsageRecord struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	RequestLogID     uuid.UUID
	APIKeyID         uuid.NullUUID
	SiteID           uuid.NullUUID
	CanonicalModelID uuid.NullUUID
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     sql.NullInt64 `gorm:"type:bigint"`
	EstimatedCost    sql.NullFloat64
	Currency         string
	CreatedAt        time.Time
}

type CreateUsageRecordParams struct {
	RequestLogID     uuid.UUID
	APIKeyID         any
	SiteID           any
	CanonicalModelID any
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     any
	EstimatedCost    any
	Currency         string
}

type UsageRecordRepository struct {
	db *gorm.DB
}

func NewUsageRecordRepository(db *gorm.DB) UsageRecordRepository {
	return UsageRecordRepository{db: db}
}

func (r UsageRecordRepository) Create(ctx context.Context, params CreateUsageRecordParams) (UsageRecord, error) {
	if params.Currency == "" {
		params.Currency = "USD"
	}
	item := UsageRecord{
		RequestLogID:     params.RequestLogID,
		APIKeyID:         nullUUIDFromAny(params.APIKeyID),
		SiteID:           nullUUIDFromAny(params.SiteID),
		CanonicalModelID: nullUUIDFromAny(params.CanonicalModelID),
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TotalTokens:      params.TotalTokens,
		CachedTokens:     nullInt64FromAny(params.CachedTokens),
		EstimatedCost:    nullFloatFromAny(params.EstimatedCost),
		Currency:         params.Currency,
	}
	if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
		return UsageRecord{}, fmt.Errorf("create usage record: %w", err)
	}
	return item, nil
}
