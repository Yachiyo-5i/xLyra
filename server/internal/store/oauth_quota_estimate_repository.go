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
	OAuthQuotaWindowFiveHour = "five_hour"
	OAuthQuotaWindowWeekly   = "weekly"

	OAuthQuotaRoundCurrent  = "current"
	OAuthQuotaRoundPrevious = "previous"

	OAuthQuotaConfidenceBaseline   = "baseline"
	OAuthQuotaConfidenceSampling   = "sampling"
	OAuthQuotaConfidenceReady      = "ready"
	OAuthQuotaConfidenceLowSample  = "low_sample"
)

type OAuthQuotaSnapshot struct {
	ID                         uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	OAuthConnectionID          uuid.UUID `gorm:"column:oauth_connection_id"`
	SiteID                     uuid.UUID
	ObservedAt                 time.Time
	FiveHourUsedPercent        sql.NullFloat64
	FiveHourRemainingPercent   sql.NullFloat64
	FiveHourResetAt            sql.NullTime
	WeeklyUsedPercent          sql.NullFloat64
	WeeklyRemainingPercent     sql.NullFloat64
	WeeklyResetAt              sql.NullTime
	Available                  bool
	RawQuota                   JSON `gorm:"type:jsonb"`
	CreatedAt                  time.Time
}

func (OAuthQuotaSnapshot) TableName() string { return "oauth_quota_snapshots" }

type OAuthQuotaWindowRound struct {
	ID                      uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	OAuthConnectionID       uuid.UUID `gorm:"column:oauth_connection_id"`
	SiteID                  uuid.UUID
	WindowType              string
	Status                  string
	StartedAt               time.Time
	EndedAt                 sql.NullTime
	StartUsedPercent        sql.NullFloat64
	LatestUsedPercent       sql.NullFloat64
	StartRemainingPercent   sql.NullFloat64
	LatestRemainingPercent  sql.NullFloat64
	StartResetAt            sql.NullTime
	LatestResetAt           sql.NullTime
	StartSnapshotID         uuid.NullUUID
	LatestSnapshotID        uuid.NullUUID
	CumulativeUsedPercent   float64
	CumulativeSystemCost    float64 `gorm:"type:numeric(18,8)"`
	RequestCount            int64
	SampleCount             int
	EstimatedTotal          sql.NullFloat64 `gorm:"type:numeric(18,8)"`
	Confidence              string
	ExternalUsageHint       bool
	Currency                string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

func (OAuthQuotaWindowRound) TableName() string { return "oauth_quota_window_rounds" }

type CreateOAuthQuotaSnapshotParams struct {
	OAuthConnectionID        uuid.UUID
	SiteID                   uuid.UUID
	ObservedAt               time.Time
	FiveHourUsedPercent      any
	FiveHourRemainingPercent any
	FiveHourResetAt          any
	WeeklyUsedPercent        any
	WeeklyRemainingPercent   any
	WeeklyResetAt            any
	Available                bool
	RawQuota                 JSON
}

type SiteQuotaCostSummary struct {
	Cost         float64
	RequestCount int64
}

type OAuthQuotaEstimateRepository struct {
	db *gorm.DB
}

func NewOAuthQuotaEstimateRepository(db *gorm.DB) OAuthQuotaEstimateRepository {
	return OAuthQuotaEstimateRepository{db: db}
}

func (r OAuthQuotaEstimateRepository) CreateSnapshot(ctx context.Context, params CreateOAuthQuotaSnapshotParams) (OAuthQuotaSnapshot, error) {
	observedAt := params.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	item := OAuthQuotaSnapshot{
		OAuthConnectionID:        params.OAuthConnectionID,
		SiteID:                   params.SiteID,
		ObservedAt:               observedAt,
		FiveHourUsedPercent:      nullFloatFromAny(params.FiveHourUsedPercent),
		FiveHourRemainingPercent: nullFloatFromAny(params.FiveHourRemainingPercent),
		FiveHourResetAt:          nullTimeFromAny(params.FiveHourResetAt),
		WeeklyUsedPercent:        nullFloatFromAny(params.WeeklyUsedPercent),
		WeeklyRemainingPercent:   nullFloatFromAny(params.WeeklyRemainingPercent),
		WeeklyResetAt:            nullTimeFromAny(params.WeeklyResetAt),
		Available:                params.Available,
		RawQuota:                 jsonDefault(params.RawQuota, "{}"),
	}
	if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
		return OAuthQuotaSnapshot{}, fmt.Errorf("create oauth quota snapshot: %w", err)
	}
	return item, nil
}

func (r OAuthQuotaEstimateRepository) GetRound(ctx context.Context, connectionID uuid.UUID, windowType string, status string) (OAuthQuotaWindowRound, error) {
	var item OAuthQuotaWindowRound
	err := r.db.WithContext(ctx).
		Where(&OAuthQuotaWindowRound{
			OAuthConnectionID: connectionID,
			WindowType:        windowType,
			Status:            status,
		}).
		First(&item).Error
	if err != nil {
		return OAuthQuotaWindowRound{}, fmt.Errorf("get oauth quota window round: %w", err)
	}
	return item, nil
}

func (r OAuthQuotaEstimateRepository) ListRoundsByConnection(ctx context.Context, connectionID uuid.UUID) ([]OAuthQuotaWindowRound, error) {
	var items []OAuthQuotaWindowRound
	if err := r.db.WithContext(ctx).
		Where(&OAuthQuotaWindowRound{OAuthConnectionID: connectionID}).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list oauth quota window rounds: %w", err)
	}
	return items, nil
}

func (r OAuthQuotaEstimateRepository) SaveRound(ctx context.Context, round OAuthQuotaWindowRound) (OAuthQuotaWindowRound, error) {
	round.UpdatedAt = time.Now()
	if round.CreatedAt.IsZero() {
		round.CreatedAt = round.UpdatedAt
	}
	if err := r.db.WithContext(ctx).Save(&round).Error; err != nil {
		return OAuthQuotaWindowRound{}, fmt.Errorf("save oauth quota window round: %w", err)
	}
	return round, nil
}

func (r OAuthQuotaEstimateRepository) UpsertRound(ctx context.Context, round OAuthQuotaWindowRound) (OAuthQuotaWindowRound, error) {
	now := time.Now()
	if round.CreatedAt.IsZero() {
		round.CreatedAt = now
	}
	round.UpdatedAt = now
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "oauth_connection_id"},
			{Name: "window_type"},
			{Name: "status"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"site_id",
			"started_at",
			"ended_at",
			"start_used_percent",
			"latest_used_percent",
			"start_remaining_percent",
			"latest_remaining_percent",
			"start_reset_at",
			"latest_reset_at",
			"start_snapshot_id",
			"latest_snapshot_id",
			"cumulative_used_percent",
			"cumulative_system_cost",
			"request_count",
			"sample_count",
			"estimated_total",
			"confidence",
			"external_usage_hint",
			"currency",
			"updated_at",
		}),
	}).Create(&round).Error; err != nil {
		return OAuthQuotaWindowRound{}, fmt.Errorf("upsert oauth quota window round: %w", err)
	}
	return round, nil
}

func (r OAuthQuotaEstimateRepository) DeleteRound(ctx context.Context, connectionID uuid.UUID, windowType string, status string) error {
	result := r.db.WithContext(ctx).
		Where(&OAuthQuotaWindowRound{
			OAuthConnectionID: connectionID,
			WindowType:        windowType,
			Status:            status,
		}).
		Delete(&OAuthQuotaWindowRound{})
	if result.Error != nil {
		return fmt.Errorf("delete oauth quota window round: %w", result.Error)
	}
	return nil
}

// RotateRoundOnReset promotes the current round to previous and inserts a new
// baseline current round in one transaction, so a mid-reset failure cannot leave
// the window without a current round.
func (r OAuthQuotaEstimateRepository) RotateRoundOnReset(ctx context.Context, current OAuthQuotaWindowRound, endedAt time.Time, baseline OAuthQuotaWindowRound) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewOAuthQuotaEstimateRepository(tx)
		if err := repo.DeleteRound(ctx, current.OAuthConnectionID, current.WindowType, OAuthQuotaRoundPrevious); err != nil {
			return err
		}
		current.Status = OAuthQuotaRoundPrevious
		current.EndedAt = sql.NullTime{Time: endedAt, Valid: true}
		if _, err := repo.SaveRound(ctx, current); err != nil {
			return err
		}
		baseline.Status = OAuthQuotaRoundCurrent
		baseline.WindowType = current.WindowType
		baseline.OAuthConnectionID = current.OAuthConnectionID
		_, err := repo.UpsertRound(ctx, baseline)
		return err
	})
}

// SumSiteQuotaCost returns the system cost used for Codex quota estimation.
// Prefers request_logs.metadata.cost_calculation.base_estimated_cost (excludes
// API-key / credential billing multipliers). Falls back to usage_records.estimated_cost.
// Group ratio is not applied to normal token pricing; for per-request pricing that
// baked in group_ratio, the base cost is divided by group_ratio when available.
func (r OAuthQuotaEstimateRepository) SumSiteQuotaCost(ctx context.Context, siteID uuid.UUID, from time.Time, to time.Time) (SiteQuotaCostSummary, error) {
	if siteID == uuid.Nil || !to.After(from) {
		return SiteQuotaCostSummary{}, nil
	}
	type row struct {
		Cost         float64
		RequestCount int64
	}
	var result row
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COALESCE(SUM(
				CASE
					WHEN base_cost IS NOT NULL
						AND group_ratio IS NOT NULL
						AND group_ratio > 0
						AND formula = 'per_request_value'
						AND per_request_value IS NULL
					THEN base_cost / group_ratio
					WHEN base_cost IS NOT NULL THEN base_cost
					ELSE estimated_cost
				END
			), 0) AS cost,
			COUNT(*)::bigint AS request_count
		FROM (
			SELECT
				ur.estimated_cost AS estimated_cost,
				NULLIF(rl.metadata #>> '{cost_calculation,base_estimated_cost}', '')::double precision AS base_cost,
				NULLIF(rl.metadata #>> '{cost_calculation,group_ratio}', '')::double precision AS group_ratio,
				NULLIF(rl.metadata #>> '{cost_calculation,formula}', '') AS formula,
				NULLIF(rl.metadata #>> '{cost_calculation,per_request_value}', '')::double precision AS per_request_value
			FROM request_logs rl
			INNER JOIN usage_records ur ON ur.request_log_id = rl.id
			WHERE rl.site_id = ?
			  AND rl.success = TRUE
			  AND rl.created_at >= ?
			  AND rl.created_at < ?
			  AND ur.estimated_cost IS NOT NULL
		) priced
	`, siteID, from, to).Scan(&result).Error
	if err != nil {
		return SiteQuotaCostSummary{}, fmt.Errorf("sum site quota cost: %w", err)
	}
	return SiteQuotaCostSummary{Cost: result.Cost, RequestCount: result.RequestCount}, nil
}
