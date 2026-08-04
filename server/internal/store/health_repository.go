package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HealthSnapshot struct {
	ID           uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID       uuid.UUID
	SiteModelID  uuid.NullUUID
	Scope        string
	Source       string
	Endpoint     string
	Method       string
	Success      bool
	StatusCode   sql.NullInt64
	LatencyMS    sql.NullInt64
	ErrorType    sql.NullString
	ErrorMessage sql.NullString
	CheckedAt    time.Time
	Metadata     JSON `gorm:"type:jsonb"`
}

type CreateHealthSnapshotParams struct {
	SiteID       uuid.UUID
	SiteModelID  any
	Scope        string
	Source       string
	Endpoint     string
	Method       string
	Success      bool
	StatusCode   any
	LatencyMS    any
	ErrorType    any
	ErrorMessage any
	CheckedAt    time.Time
	Metadata     JSON
}

type SiteHealthState struct {
	SiteID              uuid.UUID `gorm:"primaryKey"`
	Status              string
	LastSnapshotID      uuid.NullUUID
	LastSuccessAt       sql.NullTime
	LastFailureAt       sql.NullTime
	ConsecutiveFailures int
	RecentSuccessRate   sql.NullFloat64
	RecentAvgLatencyMS  sql.NullInt64
	CheckedAt           sql.NullTime
	Message             sql.NullString
	Metadata            JSON `gorm:"type:jsonb"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type SiteHealthHourlyBucket struct {
	BucketTime   time.Time
	SuccessCount int
	FailureCount int
	TotalCount   int
}

type UpsertSiteHealthStateParams struct {
	SiteID              uuid.UUID
	Status              string
	LastSnapshotID      uuid.UUID
	LastSuccessAt       any
	LastFailureAt       any
	ConsecutiveFailures int
	RecentSuccessRate   any
	RecentAvgLatencyMS  any
	CheckedAt           any
	Message             any
	Metadata            JSON
}

type HealthRepository struct {
	db *gorm.DB
}

func NewHealthRepository(db *gorm.DB) HealthRepository {
	return HealthRepository{db: db}
}

func (r HealthRepository) CreateSnapshot(ctx context.Context, params CreateHealthSnapshotParams) (HealthSnapshot, error) {
	if params.Scope == "" {
		params.Scope = "site"
	}
	if params.Source == "" {
		params.Source = "manual"
	}
	if params.Method == "" {
		params.Method = "GET"
	}
	if params.CheckedAt.IsZero() {
		params.CheckedAt = time.Now()
	}
	item := HealthSnapshot{
		SiteID:       params.SiteID,
		SiteModelID:  nullUUIDFromAny(params.SiteModelID),
		Scope:        params.Scope,
		Source:       params.Source,
		Endpoint:     params.Endpoint,
		Method:       params.Method,
		Success:      params.Success,
		StatusCode:   nullInt64FromAny(params.StatusCode),
		LatencyMS:    nullInt64FromAny(params.LatencyMS),
		ErrorType:    nullStringFromAny(params.ErrorType),
		ErrorMessage: nullStringFromAny(params.ErrorMessage),
		CheckedAt:    params.CheckedAt,
		Metadata:     jsonDefault(params.Metadata, "{}"),
	}
	if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
		return HealthSnapshot{}, fmt.Errorf("create health snapshot: %w", err)
	}
	return item, nil
}

func (r HealthRepository) UpsertSiteState(ctx context.Context, params UpsertSiteHealthStateParams) (SiteHealthState, error) {
	if params.Status == "" {
		params.Status = "unknown"
	}
	db := r.db.WithContext(ctx)
	var state SiteHealthState
	err := db.Where(&SiteHealthState{SiteID: params.SiteID}).First(&state).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return SiteHealthState{}, fmt.Errorf("upsert site health state: %w", err)
	}
	state.SiteID = params.SiteID
	state.Status = params.Status
	state.LastSnapshotID = uuid.NullUUID{UUID: params.LastSnapshotID, Valid: params.LastSnapshotID != uuid.Nil}
	if value := nullTimeFromAny(params.LastSuccessAt); value.Valid {
		state.LastSuccessAt = value
	}
	if value := nullTimeFromAny(params.LastFailureAt); value.Valid {
		state.LastFailureAt = value
	}
	state.ConsecutiveFailures = params.ConsecutiveFailures
	state.RecentSuccessRate = nullFloatFromAny(params.RecentSuccessRate)
	state.RecentAvgLatencyMS = nullInt64FromAny(params.RecentAvgLatencyMS)
	state.CheckedAt = nullTimeFromAny(params.CheckedAt)
	state.Message = nullStringFromAny(params.Message)
	state.Metadata = jsonDefault(params.Metadata, "{}")
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&state).Error; err != nil {
			return SiteHealthState{}, fmt.Errorf("upsert site health state: %w", err)
		}
		return state, nil
	}
	if err := db.Save(&state).Error; err != nil {
		return SiteHealthState{}, fmt.Errorf("upsert site health state: %w", err)
	}
	return state, nil
}

func (r HealthRepository) GetSiteState(ctx context.Context, siteID uuid.UUID) (SiteHealthState, error) {
	var state SiteHealthState
	if err := r.db.WithContext(ctx).Where(&SiteHealthState{SiteID: siteID}).First(&state).Error; err != nil {
		return SiteHealthState{}, fmt.Errorf("get site health state: %w", err)
	}
	return state, nil
}

func (r HealthRepository) ListSiteStates(ctx context.Context) (map[uuid.UUID]SiteHealthState, error) {
	var states []SiteHealthState
	if err := r.db.WithContext(ctx).Find(&states).Error; err != nil {
		return nil, fmt.Errorf("list site health states: %w", err)
	}
	result := map[uuid.UUID]SiteHealthState{}
	for _, state := range states {
		result[state.SiteID] = state
	}
	return result, nil
}

func (r HealthRepository) ListSiteSnapshots(ctx context.Context, siteID uuid.UUID, limit int) ([]HealthSnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	var items []HealthSnapshot
	if err := r.db.WithContext(ctx).Where(&HealthSnapshot{SiteID: siteID, Scope: "site"}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list site health snapshots: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CheckedAt.After(items[j].CheckedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r HealthRepository) ListSiteSnapshotsBySource(ctx context.Context, siteID uuid.UUID, limit int, source string) ([]HealthSnapshot, error) {
	if strings.TrimSpace(source) == "" {
		return r.ListSiteSnapshots(ctx, siteID, limit)
	}
	if limit <= 0 {
		limit = 20
	}
	var items []HealthSnapshot
	if err := r.db.WithContext(ctx).Where(&HealthSnapshot{SiteID: siteID, Scope: "site", Source: source}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list site health snapshots by source: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CheckedAt.After(items[j].CheckedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (r HealthRepository) ListSiteHourlyBuckets(ctx context.Context, siteID uuid.UUID, since time.Time) ([]SiteHealthHourlyBucket, error) {
	var snapshots []HealthSnapshot
	if err := r.db.WithContext(ctx).Where(&HealthSnapshot{SiteID: siteID, Scope: "site", Source: "scheduler"}).Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("list site health hourly buckets: %w", err)
	}
	buckets := map[time.Time]*SiteHealthHourlyBucket{}
	location := since.Location()
	for _, snapshot := range snapshots {
		if snapshot.CheckedAt.Before(since) {
			continue
		}
		checkedAt := snapshot.CheckedAt.In(location)
		year, month, day := checkedAt.Date()
		bucketTime := time.Date(year, month, day, checkedAt.Hour(), 0, 0, 0, location)
		bucket := buckets[bucketTime]
		if bucket == nil {
			bucket = &SiteHealthHourlyBucket{BucketTime: bucketTime}
			buckets[bucketTime] = bucket
		}
		bucket.TotalCount++
		if snapshot.Success {
			bucket.SuccessCount++
		} else {
			bucket.FailureCount++
		}
	}
	items := make([]SiteHealthHourlyBucket, 0, len(buckets))
	for _, bucket := range buckets {
		items = append(items, *bucket)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].BucketTime.Before(items[j].BucketTime)
	})
	return items, nil
}

func (r HealthRepository) DeleteSnapshotsBefore(ctx context.Context, cutoff time.Time) error {
	if err := r.db.WithContext(ctx).
		Clauses(healthCheckedAtBeforeClause(cutoff)).
		Delete(&HealthSnapshot{}).Error; err != nil {
		return fmt.Errorf("delete old health snapshots: %w", err)
	}
	return nil
}

func (r HealthRepository) DeleteSiteStatesBefore(ctx context.Context, cutoff time.Time) error {
	if err := r.db.WithContext(ctx).
		Clauses(healthCheckedAtBeforeClause(cutoff)).
		Delete(&SiteHealthState{}).Error; err != nil {
		return fmt.Errorf("delete old site health states: %w", err)
	}
	return nil
}

func healthCheckedAtBeforeClause(cutoff time.Time) clause.Where {
	return clause.Where{Exprs: []clause.Expression{
		clause.Lt{Column: clause.Column{Name: "checked_at"}, Value: cutoff.UTC()},
	}}
}
