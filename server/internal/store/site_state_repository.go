package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteState struct {
	SiteID            uuid.UUID `gorm:"primaryKey"`
	ValidationOK      sql.NullBool
	ValidationMessage sql.NullString
	SyncStatus        string
	SyncMessage       sql.NullString
	LastSyncStartedAt sql.NullTime
	LastSyncedAt      sql.NullTime
	APIKeyCount       int
	ModelCount        int
	RawStatus         JSON `gorm:"type:jsonb"`
	UserSummary       JSON `gorm:"type:jsonb"`
	Pricing           JSON `gorm:"type:jsonb"`
	Checkin           JSON `gorm:"type:jsonb"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type UpsertSiteStateParams struct {
	SiteID            uuid.UUID
	ValidationOK      any
	ValidationMessage any
	SyncStatus        string
	SyncMessage       any
	LastSyncStartedAt any
	LastSyncedAt      any
	APIKeyCount       int
	ModelCount        int
	RawStatus         JSON
	UserSummary       JSON
	Pricing           JSON
	Checkin           JSON
}

type SiteStateRepository struct {
	db *gorm.DB
}

func NewSiteStateRepository(db *gorm.DB) SiteStateRepository {
	return SiteStateRepository{db: db}
}

func (r SiteStateRepository) Upsert(ctx context.Context, params UpsertSiteStateParams) (SiteState, error) {
	if params.SyncStatus == "" {
		params.SyncStatus = "pending"
	}
	db := r.db.WithContext(ctx)
	var state SiteState
	err := db.Where(&SiteState{SiteID: params.SiteID}).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteState{}, fmt.Errorf("upsert site state: %w", err)
	}
	state.SiteID = params.SiteID
	state.ValidationOK = nullBoolFromAny(params.ValidationOK)
	state.ValidationMessage = nullStringFromAny(params.ValidationMessage)
	state.SyncStatus = params.SyncStatus
	state.SyncMessage = nullStringFromAny(params.SyncMessage)
	state.LastSyncStartedAt = nullTimeFromAny(params.LastSyncStartedAt)
	state.LastSyncedAt = nullTimeFromAny(params.LastSyncedAt)
	state.APIKeyCount = params.APIKeyCount
	state.ModelCount = params.ModelCount
	state.RawStatus = jsonDefault(params.RawStatus, "{}")
	if len(params.UserSummary) > 0 {
		state.UserSummary = params.UserSummary
	} else if err == gorm.ErrRecordNotFound {
		state.UserSummary = jsonDefault(nil, "{}")
	}
	if len(params.Pricing) > 0 {
		state.Pricing = params.Pricing
	} else if err == gorm.ErrRecordNotFound {
		state.Pricing = jsonDefault(nil, "{}")
	}
	if len(params.Checkin) > 0 {
		state.Checkin = params.Checkin
	} else if err == gorm.ErrRecordNotFound {
		state.Checkin = jsonDefault(nil, "{}")
	}
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&state).Error; err != nil {
			return SiteState{}, fmt.Errorf("upsert site state: %w", err)
		}
		return state, nil
	}
	if err := db.Save(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("upsert site state: %w", err)
	}
	return state, nil
}

func (r SiteStateRepository) MarkSyncStarted(ctx context.Context, siteID uuid.UUID) (SiteState, error) {
	db := r.db.WithContext(ctx)
	var state SiteState
	err := db.Where(&SiteState{SiteID: siteID}).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteState{}, fmt.Errorf("mark site sync started: %w", err)
	}
	state.SiteID = siteID
	state.SyncStatus = "syncing"
	state.SyncMessage = sql.NullString{}
	state.LastSyncStartedAt = sql.NullTime{Time: time.Now(), Valid: true}
	state.RawStatus = jsonDefault(state.RawStatus, "{}")
	state.UserSummary = jsonDefault(state.UserSummary, "{}")
	state.Pricing = jsonDefault(state.Pricing, "{}")
	state.Checkin = jsonDefault(state.Checkin, "{}")
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&state).Error; err != nil {
			return SiteState{}, fmt.Errorf("mark site sync started: %w", err)
		}
		return state, nil
	}
	if err := db.Save(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("mark site sync started: %w", err)
	}
	return state, nil
}

func (r SiteStateRepository) MarkSyncFailed(ctx context.Context, siteID uuid.UUID, message string) (SiteState, error) {
	db := r.db.WithContext(ctx)
	var state SiteState
	if err := db.Where(&SiteState{SiteID: siteID}).First(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("mark site sync failed: %w", err)
	}
	state.SyncStatus = "failed"
	state.SyncMessage = sql.NullString{String: message, Valid: message != ""}
	if err := db.Save(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("mark site sync failed: %w", err)
	}
	return state, nil
}

func (r SiteStateRepository) MarkPricingGap(ctx context.Context, siteID uuid.UUID, message string) (SiteState, error) {
	db := r.db.WithContext(ctx)
	var state SiteState
	if err := db.Where(&SiteState{SiteID: siteID}).First(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("mark site pricing gap: %w", err)
	}
	if state.SyncStatus == "synced" {
		state.SyncStatus = "partial"
	}
	if state.SyncMessage.Valid && state.SyncMessage.String != "" {
		state.SyncMessage = sql.NullString{String: state.SyncMessage.String + "; " + message, Valid: true}
	} else {
		state.SyncMessage = sql.NullString{String: message, Valid: true}
	}
	if err := db.Save(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("mark site pricing gap: %w", err)
	}
	return state, nil
}

func (r SiteStateRepository) GetBySite(ctx context.Context, siteID uuid.UUID) (SiteState, error) {
	var state SiteState
	if err := r.db.WithContext(ctx).Where(&SiteState{SiteID: siteID}).First(&state).Error; err != nil {
		return SiteState{}, fmt.Errorf("get site state: %w", err)
	}
	return state, nil
}

func (r SiteStateRepository) List(ctx context.Context) (map[uuid.UUID]SiteState, error) {
	var states []SiteState
	if err := r.db.WithContext(ctx).Find(&states).Error; err != nil {
		return nil, fmt.Errorf("list site states: %w", err)
	}
	result := map[uuid.UUID]SiteState{}
	for _, state := range states {
		result[state.SiteID] = state
	}
	return result, nil
}
