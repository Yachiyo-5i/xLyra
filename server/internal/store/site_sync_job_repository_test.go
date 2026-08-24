package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestApplySiteSyncJobFinishRequeuesRequestedRerun(t *testing.T) {
	activeKey := "api_key_refresh:site:credential"
	job := SiteSyncJob{
		ID:             uuid.New(),
		Status:         SiteSyncJobStatusRunning,
		ActiveKey:      &activeKey,
		RerunRequested: true,
		StartedAt:      sql.NullTime{Time: time.Now(), Valid: true},
	}

	applySiteSyncJobFinish(&job, SiteSyncJobStatusSucceeded, "", time.Now())

	if job.Status != SiteSyncJobStatusQueued || job.ActiveKey == nil || *job.ActiveKey != activeKey {
		t.Fatalf("rerun job = %#v", job)
	}
	if job.RerunRequested || job.StartedAt.Valid || job.FinishedAt.Valid || job.Error.Valid {
		t.Fatalf("rerun job retained terminal fields = %#v", job)
	}
}

func TestApplySiteSyncJobFinishCompletesWithoutRerun(t *testing.T) {
	activeKey := "site_refresh:site"
	finishedAt := time.Now()
	job := SiteSyncJob{
		ID:        uuid.New(),
		Status:    SiteSyncJobStatusRunning,
		ActiveKey: &activeKey,
	}

	applySiteSyncJobFinish(&job, SiteSyncJobStatusFailed, "upstream unavailable", finishedAt)

	if job.Status != SiteSyncJobStatusFailed || job.ActiveKey != nil {
		t.Fatalf("finished job = %#v", job)
	}
	if !job.Error.Valid || job.Error.String != "upstream unavailable" || !job.FinishedAt.Valid || !job.FinishedAt.Time.Equal(finishedAt) {
		t.Fatalf("finished job fields = %#v", job)
	}
}
