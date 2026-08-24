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

type SiteAPIKeyState struct {
	SiteCredentialID  uuid.UUID `gorm:"primaryKey"`
	SiteID            uuid.UUID
	UpstreamID        sql.NullInt64
	Name              string
	UpstreamStatus    JSON `gorm:"type:jsonb"`
	Enabled           bool
	GroupName         sql.NullString
	RemainQuota       sql.NullInt64
	UsedQuota         sql.NullInt64
	UnlimitedQuota    bool
	ExpiredTime       sql.NullInt64
	ModelLimitsEnable bool `gorm:"column:model_limits_enabled"`
	ModelLimits       JSON `gorm:"type:jsonb"`
	Usage             JSON `gorm:"type:jsonb"`
	Raw               JSON `gorm:"type:jsonb"`
	SyncStatus        string
	SyncMessage       sql.NullString
	LastSyncedAt      sql.NullTime
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type UpsertSiteAPIKeyStateParams struct {
	SiteCredentialID  uuid.UUID
	SiteID            uuid.UUID
	UpstreamID        any
	Name              string
	UpstreamStatus    JSON
	Enabled           bool
	GroupName         any
	RemainQuota       any
	UsedQuota         any
	UnlimitedQuota    bool
	ExpiredTime       any
	ModelLimitsEnable bool
	ModelLimits       JSON
	Usage             JSON
	Raw               JSON
	SyncStatus        string
	SyncMessage       any
	LastSyncedAt      any
}

type SiteAPIKeyStateRepository struct {
	db *gorm.DB
}

func NewSiteAPIKeyStateRepository(db *gorm.DB) SiteAPIKeyStateRepository {
	return SiteAPIKeyStateRepository{db: db}
}

func (r SiteAPIKeyStateRepository) Upsert(ctx context.Context, params UpsertSiteAPIKeyStateParams) (SiteAPIKeyState, error) {
	if params.SyncStatus == "" {
		params.SyncStatus = "synced"
	}
	db := r.db.WithContext(ctx)
	var state SiteAPIKeyState
	err := db.Where(&SiteAPIKeyState{SiteCredentialID: params.SiteCredentialID}).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteAPIKeyState{}, fmt.Errorf("upsert site api key state: %w", err)
	}
	state.SiteCredentialID = params.SiteCredentialID
	state.SiteID = params.SiteID
	state.UpstreamID = nullInt64FromAny(params.UpstreamID)
	state.Name = params.Name
	state.UpstreamStatus = jsonDefault(params.UpstreamStatus, "null")
	if err == gorm.ErrRecordNotFound {
		state.Enabled = params.Enabled
	}
	state.GroupName = nullStringFromAny(params.GroupName)
	state.RemainQuota = nullInt64FromAny(params.RemainQuota)
	state.UsedQuota = nullInt64FromAny(params.UsedQuota)
	state.UnlimitedQuota = params.UnlimitedQuota
	state.ExpiredTime = nullInt64FromAny(params.ExpiredTime)
	state.ModelLimitsEnable = params.ModelLimitsEnable
	state.ModelLimits = jsonDefault(params.ModelLimits, "[]")
	state.Usage = jsonDefault(params.Usage, "{}")
	state.Raw = jsonDefault(params.Raw, "{}")
	state.SyncStatus = params.SyncStatus
	state.SyncMessage = nullStringFromAny(params.SyncMessage)
	state.LastSyncedAt = nullTimeFromAny(params.LastSyncedAt)
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&state).Error; err != nil {
			return SiteAPIKeyState{}, fmt.Errorf("upsert site api key state: %w", err)
		}
		return state, nil
	}
	if err := db.Save(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("upsert site api key state: %w", err)
	}
	return state, nil
}

// UpsertSyncResult refreshes only the sync-owned columns (usage, sync status,
// message, last_synced_at) of an existing state row, preserving descriptive
// fields (name, quota, group, model limits, upstream status, enabled) that a
// single-key refresh does not recompute. When no row exists yet it falls back
// to a full Upsert so the first sync still creates the row.
func (r SiteAPIKeyStateRepository) UpsertSyncResult(ctx context.Context, params UpsertSiteAPIKeyStateParams) (SiteAPIKeyState, error) {
	if params.SyncStatus == "" {
		params.SyncStatus = "synced"
	}
	db := r.db.WithContext(ctx)
	var state SiteAPIKeyState
	err := db.Where(&SiteAPIKeyState{SiteCredentialID: params.SiteCredentialID}).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteAPIKeyState{}, fmt.Errorf("upsert site api key state sync: %w", err)
	}
	if err == gorm.ErrRecordNotFound {
		return r.Upsert(ctx, params)
	}
	state.Usage = jsonDefault(params.Usage, "{}")
	state.SyncStatus = params.SyncStatus
	state.SyncMessage = nullStringFromAny(params.SyncMessage)
	state.LastSyncedAt = nullTimeFromAny(params.LastSyncedAt)
	if err := db.Model(&state).Select("Usage", "SyncStatus", "SyncMessage", "LastSyncedAt").Updates(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("upsert site api key state sync: %w", err)
	}
	return state, nil
}

func (r SiteAPIKeyStateRepository) UpdateUsage(ctx context.Context, siteCredentialID uuid.UUID, usage JSON) error {
	if siteCredentialID == uuid.Nil {
		return nil
	}
	result := r.db.WithContext(ctx).
		Model(&SiteAPIKeyState{}).
		Where(&SiteAPIKeyState{SiteCredentialID: siteCredentialID}).
		Updates(map[string]any{"usage": jsonDefault(usage, "{}")})
	if result.Error != nil {
		return fmt.Errorf("update site api key state usage: %w", result.Error)
	}
	return nil
}

func (r SiteAPIKeyStateRepository) ListBySite(ctx context.Context, siteID uuid.UUID) ([]SiteAPIKeyState, error) {
	var result []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteID: siteID}).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site api key states: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].UpstreamID
		right := result[j].UpstreamID
		if left.Valid != right.Valid {
			return left.Valid
		}
		if left.Valid && left.Int64 != right.Int64 {
			return left.Int64 < right.Int64
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (r SiteAPIKeyStateRepository) ListAll(ctx context.Context) ([]SiteAPIKeyState, error) {
	var result []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list site api key states: %w", err)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left := result[i].UpstreamID
		right := result[j].UpstreamID
		if result[i].SiteID != result[j].SiteID {
			return result[i].SiteID.String() < result[j].SiteID.String()
		}
		if left.Valid != right.Valid {
			return left.Valid
		}
		if left.Valid && left.Int64 != right.Int64 {
			return left.Int64 < right.Int64
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (r SiteAPIKeyStateRepository) DeleteByCredential(ctx context.Context, siteCredentialID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteCredentialID: siteCredentialID}).Delete(&SiteAPIKeyState{}).Error; err != nil {
		return fmt.Errorf("delete site api key state by credential: %w", err)
	}
	return nil
}

func (r SiteAPIKeyStateRepository) UpdateEnabled(ctx context.Context, siteCredentialID uuid.UUID, enabled bool) (SiteAPIKeyState, error) {
	var state SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteCredentialID: siteCredentialID}).First(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("update site api key state enabled: %w", err)
	}
	state.Enabled = enabled
	if err := r.db.WithContext(ctx).Save(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("update site api key state enabled: %w", err)
	}
	return state, nil
}

func (r SiteAPIKeyStateRepository) MarkStale(ctx context.Context, siteCredentialID uuid.UUID, syncedAt time.Time) (SiteAPIKeyState, error) {
	var state SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteCredentialID: siteCredentialID}).First(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("mark site api key state stale: %w", err)
	}
	state.SyncStatus = "stale"
	state.SyncMessage = sql.NullString{String: "api key missing from upstream inventory", Valid: true}
	state.LastSyncedAt = sql.NullTime{Time: syncedAt, Valid: true}
	if err := r.db.WithContext(ctx).Save(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("mark site api key state stale: %w", err)
	}
	return state, nil
}

func (r SiteAPIKeyStateRepository) RecoverPending(ctx context.Context, siteCredentialID uuid.UUID) (SiteAPIKeyState, error) {
	var state SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteCredentialID: siteCredentialID}).First(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("recover site api key state: %w", err)
	}
	state.Enabled = true
	state.SyncStatus = "pending"
	state.SyncMessage = sql.NullString{}
	if err := r.db.WithContext(ctx).Save(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("recover site api key state: %w", err)
	}
	return state, nil
}

func (r SiteAPIKeyStateRepository) MarkSyncPending(ctx context.Context, siteID uuid.UUID, siteCredentialID uuid.UUID, enabled bool) (SiteAPIKeyState, error) {
	return r.markSyncStatus(ctx, siteID, siteCredentialID, enabled, "pending", "")
}

func (r SiteAPIKeyStateRepository) MarkSyncStarted(ctx context.Context, siteID uuid.UUID, siteCredentialID uuid.UUID, enabled bool) (SiteAPIKeyState, error) {
	return r.markSyncStatus(ctx, siteID, siteCredentialID, enabled, "syncing", "")
}

func (r SiteAPIKeyStateRepository) MarkSyncFailed(ctx context.Context, siteID uuid.UUID, siteCredentialID uuid.UUID, enabled bool, message string) (SiteAPIKeyState, error) {
	return r.markSyncStatus(ctx, siteID, siteCredentialID, enabled, "failed", message)
}

func (r SiteAPIKeyStateRepository) markSyncStatus(ctx context.Context, siteID uuid.UUID, siteCredentialID uuid.UUID, enabled bool, status string, message string) (SiteAPIKeyState, error) {
	db := r.db.WithContext(ctx)
	var state SiteAPIKeyState
	err := db.Where(&SiteAPIKeyState{SiteCredentialID: siteCredentialID}).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteAPIKeyState{}, fmt.Errorf("mark site api key sync status: %w", err)
	}
	state.SiteCredentialID = siteCredentialID
	state.SiteID = siteID
	state.SyncStatus = status
	state.SyncMessage = sql.NullString{String: message, Valid: message != ""}
	if err == gorm.ErrRecordNotFound {
		state.Enabled = enabled
		state.UpstreamStatus = jsonDefault(nil, "null")
		state.ModelLimits = jsonDefault(nil, "[]")
		state.Usage = jsonDefault(nil, "{}")
		state.Raw = jsonDefault(nil, "{}")
		if err := db.Create(&state).Error; err != nil {
			return SiteAPIKeyState{}, fmt.Errorf("mark site api key sync status: %w", err)
		}
		return state, nil
	}
	if err := db.Save(&state).Error; err != nil {
		return SiteAPIKeyState{}, fmt.Errorf("mark site api key sync status: %w", err)
	}
	return state, nil
}
