package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SiteSyncJobKindSiteRefresh   = "site_refresh"
	SiteSyncJobKindAPIKeyRefresh = "api_key_refresh"
	SiteSyncJobStatusQueued      = "queued"
	SiteSyncJobStatusRunning     = "running"
	SiteSyncJobStatusSucceeded   = "succeeded"
	SiteSyncJobStatusFailed      = "failed"
)

type SiteSyncJob struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Kind             string
	SiteID           uuid.UUID `gorm:"index:site_sync_jobs_site_id_idx"`
	SiteCredentialID *uuid.UUID
	Status           string  `gorm:"index:site_sync_jobs_status_created_idx,priority:1"`
	ActiveKey        *string `gorm:"uniqueIndex:site_sync_jobs_active_key_idx"`
	Attempts         int
	RerunRequested   bool `gorm:"default:false"`
	Error            sql.NullString
	StartedAt        sql.NullTime
	FinishedAt       sql.NullTime
	CreatedAt        time.Time `gorm:"index:site_sync_jobs_status_created_idx,priority:2"`
	UpdatedAt        time.Time
	Site             Site            `gorm:"foreignKey:SiteID;references:ID;constraint:OnDelete:CASCADE"`
	SiteCredential   *SiteCredential `gorm:"foreignKey:SiteCredentialID;references:ID;constraint:OnDelete:CASCADE"`
}

type EnqueueSiteSyncJobParams struct {
	Kind             string
	SiteID           uuid.UUID
	SiteCredentialID *uuid.UUID
}

type SiteSyncJobRepository struct {
	db *gorm.DB
}

func NewSiteSyncJobRepository(db *gorm.DB) SiteSyncJobRepository {
	return SiteSyncJobRepository{db: db}
}

func (r SiteSyncJobRepository) Enqueue(ctx context.Context, params EnqueueSiteSyncJobParams) (SiteSyncJob, error) {
	if params.SiteID == uuid.Nil {
		return SiteSyncJob{}, fmt.Errorf("enqueue site sync job: site id is required")
	}
	if params.Kind != SiteSyncJobKindSiteRefresh && params.Kind != SiteSyncJobKindAPIKeyRefresh {
		return SiteSyncJob{}, fmt.Errorf("enqueue site sync job: unsupported kind %q", params.Kind)
	}
	if params.Kind == SiteSyncJobKindAPIKeyRefresh && (params.SiteCredentialID == nil || *params.SiteCredentialID == uuid.Nil) {
		return SiteSyncJob{}, fmt.Errorf("enqueue site sync job: site credential id is required")
	}

	activeKey := siteSyncJobActiveKey(params)
	for attempt := 0; attempt < 3; attempt++ {
		job := SiteSyncJob{
			Kind:             params.Kind,
			SiteID:           params.SiteID,
			SiteCredentialID: params.SiteCredentialID,
			Status:           SiteSyncJobStatusQueued,
			ActiveKey:        &activeKey,
		}
		result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "active_key"}},
			DoNothing: true,
		}).Create(&job)
		if result.Error != nil {
			return SiteSyncJob{}, fmt.Errorf("enqueue site sync job: %w", result.Error)
		}
		if result.RowsAffected > 0 {
			return job, nil
		}
		result = r.db.WithContext(ctx).
			Model(&SiteSyncJob{}).
			Where(clause.And(
				clause.Eq{Column: "active_key", Value: activeKey},
				clause.Eq{Column: "status", Value: SiteSyncJobStatusRunning},
			)).
			Update("rerun_requested", true)
		if result.Error != nil {
			return SiteSyncJob{}, fmt.Errorf("request site sync job rerun: %w", result.Error)
		}
		var existing SiteSyncJob
		err := r.db.WithContext(ctx).Where(&SiteSyncJob{ActiveKey: &activeKey}).First(&existing).Error
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return SiteSyncJob{}, fmt.Errorf("get active site sync job: %w", err)
		}
	}
	return SiteSyncJob{}, fmt.Errorf("enqueue site sync job: conflicting active job disappeared before it could be read")
}

func (r SiteSyncJobRepository) ClaimNext(ctx context.Context, now time.Time) (SiteSyncJob, bool, error) {
	var claimed SiteSyncJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(
			clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"},
			clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "created_at"}}}},
		).Where(&SiteSyncJob{Status: SiteSyncJobStatusQueued})
		if err := query.First(&claimed).Error; err != nil {
			return err
		}
		claimed.Status = SiteSyncJobStatusRunning
		claimed.Attempts++
		claimed.StartedAt = sql.NullTime{Time: now, Valid: true}
		claimed.FinishedAt = sql.NullTime{}
		claimed.Error = sql.NullString{}
		if err := tx.Save(&claimed).Error; err != nil {
			return fmt.Errorf("mark site sync job running: %w", err)
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SiteSyncJob{}, false, nil
	}
	if err != nil {
		return SiteSyncJob{}, false, fmt.Errorf("claim site sync job: %w", err)
	}
	return claimed, true, nil
}

func (r SiteSyncJobRepository) Finish(ctx context.Context, id uuid.UUID, status string, message string, now time.Time) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job SiteSyncJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&SiteSyncJob{ID: id}).First(&job).Error; err != nil {
			return fmt.Errorf("get site sync job to finish: %w", err)
		}
		applySiteSyncJobFinish(&job, status, message, now)
		if err := tx.Save(&job).Error; err != nil {
			return fmt.Errorf("finish site sync job: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (r SiteSyncJobRepository) PurgeFinished(ctx context.Context, cutoff time.Time) error {
	result := r.db.WithContext(ctx).
		Where(map[string]any{"status": []string{SiteSyncJobStatusSucceeded, SiteSyncJobStatusFailed}}).
		Where(clause.Lt{Column: "finished_at", Value: cutoff}).
		Delete(&SiteSyncJob{})
	if result.Error != nil {
		return fmt.Errorf("purge finished site sync jobs: %w", result.Error)
	}
	return nil
}

func (r SiteSyncJobRepository) RequeueStale(ctx context.Context, cutoff time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&SiteSyncJob{}).
		Where(clause.And(
			clause.Eq{Column: "status", Value: SiteSyncJobStatusRunning},
			clause.Lt{Column: "started_at", Value: cutoff},
		)).
		Updates(map[string]any{
			"status":          SiteSyncJobStatusQueued,
			"rerun_requested": false,
			"error":           nil,
			"started_at":      nil,
			"finished_at":     nil,
		})
	if result.Error != nil {
		return fmt.Errorf("requeue stale site sync jobs: %w", result.Error)
	}
	return nil
}

func applySiteSyncJobFinish(job *SiteSyncJob, status string, message string, now time.Time) {
	if job.RerunRequested {
		job.Status = SiteSyncJobStatusQueued
		job.RerunRequested = false
		job.Error = sql.NullString{}
		job.StartedAt = sql.NullTime{}
		job.FinishedAt = sql.NullTime{}
		return
	}
	job.Status = status
	job.ActiveKey = nil
	job.Error = sql.NullString{String: message, Valid: message != ""}
	job.FinishedAt = sql.NullTime{Time: now, Valid: true}
}

func siteSyncJobActiveKey(params EnqueueSiteSyncJobParams) string {
	if params.SiteCredentialID == nil {
		return params.Kind + ":" + params.SiteID.String()
	}
	return params.Kind + ":" + params.SiteID.String() + ":" + params.SiteCredentialID.String()
}
