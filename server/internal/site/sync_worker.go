package site

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"xlyra/server/internal/store"
)

const (
	siteSyncWorkerPollInterval   = 500 * time.Millisecond
	siteSyncWorkerConcurrency    = 3
	siteSyncJobTimeout           = 10 * time.Minute
	siteSyncJobStaleAfter        = 15 * time.Minute
	siteSyncMaintenanceInterval  = 5 * time.Minute
	siteSyncFinishedJobRetention = 7 * 24 * time.Hour
)

type SyncWorker struct {
	service      *Service
	logger       *slog.Logger
	invalidate   func()
	pollInterval time.Duration
}

func NewSyncWorker(service *Service, logger *slog.Logger, invalidate func()) *SyncWorker {
	return &SyncWorker{
		service:      service,
		logger:       logger,
		invalidate:   invalidate,
		pollInterval: siteSyncWorkerPollInterval,
	}
}

func (w *SyncWorker) Run(ctx context.Context) {
	if w == nil || w.service == nil || w.service.db == nil {
		return
	}
	if w.logger == nil {
		w.logger = slog.Default()
	}
	w.runMaintenance(ctx)

	var wg sync.WaitGroup
	for i := 0; i < siteSyncWorkerConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.supervise(ctx, "job", w.runJobLoop)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.supervise(ctx, "maintenance", w.runMaintenanceLoop)
	}()
	wg.Wait()
}

func (w *SyncWorker) supervise(ctx context.Context, name string, fn func(context.Context)) {
	for ctx.Err() == nil {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					w.logger.Error("site sync worker loop panicked; restarting", "loop", name, "panic", recovered)
				}
			}()
			fn(ctx)
		}()
	}
}

func (w *SyncWorker) runJobLoop(ctx context.Context) {
	for {
		processed, err := w.processNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Error("site sync worker failed", "error", err)
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *SyncWorker) runMaintenanceLoop(ctx context.Context) {
	ticker := time.NewTicker(siteSyncMaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runMaintenance(ctx)
		}
	}
}

func (w *SyncWorker) runMaintenance(ctx context.Context) {
	repo := store.NewSiteSyncJobRepository(w.service.db.DB())
	if err := repo.RequeueStale(ctx, time.Now().Add(-siteSyncJobStaleAfter)); err != nil {
		w.logger.Error("site sync worker stale job recovery failed", "error", err)
	}
	if err := repo.PurgeFinished(ctx, time.Now().Add(-siteSyncFinishedJobRetention)); err != nil {
		w.logger.Error("site sync worker finished job purge failed", "error", err)
	}
}

func (w *SyncWorker) processNext(ctx context.Context) (bool, error) {
	repo := store.NewSiteSyncJobRepository(w.service.db.DB())
	job, ok, err := repo.ClaimNext(ctx, time.Now())
	if err != nil || !ok {
		return ok, err
	}

	jobCtx, cancel := context.WithTimeout(ctx, siteSyncJobTimeout)
	defer cancel()
	runErr := w.executeJob(ctx, jobCtx, job)
	status := store.SiteSyncJobStatusSucceeded
	message := ""
	if runErr != nil {
		status = store.SiteSyncJobStatusFailed
		message = runErr.Error()
	}
	if err := repo.Finish(ctx, job.ID, status, message, time.Now()); err != nil {
		return true, err
	}
	if w.invalidate != nil {
		w.invalidate()
	}
	if runErr != nil {
		w.logger.Warn("site sync job failed", "job_id", job.ID, "kind", job.Kind, "site_id", job.SiteID, "error", runErr)
	} else {
		w.logger.Info("site sync job completed", "job_id", job.ID, "kind", job.Kind, "site_id", job.SiteID)
	}
	return true, nil
}

func (w *SyncWorker) executeJob(workerCtx context.Context, jobCtx context.Context, job store.SiteSyncJob) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("site sync job panicked: %v", recovered)
		}
		if err != nil {
			w.markStateFailedBestEffort(workerCtx, job, err)
		}
	}()
	return w.runJob(jobCtx, job)
}

func (w *SyncWorker) markStateFailedBestEffort(ctx context.Context, job store.SiteSyncJob, jobErr error) {
	switch job.Kind {
	case store.SiteSyncJobKindSiteRefresh:
		stateRepo := store.NewSiteStateRepository(w.service.db.DB())
		state, err := stateRepo.GetBySite(ctx, job.SiteID)
		if err != nil || (state.SyncStatus != "pending" && state.SyncStatus != "syncing") {
			return
		}
		if _, err := stateRepo.MarkSyncFailed(ctx, job.SiteID, jobErr.Error()); err != nil {
			w.logger.Warn("record site sync failure failed", "job_id", job.ID, "site_id", job.SiteID, "error", err)
		}
	case store.SiteSyncJobKindAPIKeyRefresh:
		if job.SiteCredentialID == nil {
			return
		}
		credential, err := store.NewSiteCredentialRepository(w.service.db.DB()).GetByID(ctx, *job.SiteCredentialID)
		if err != nil {
			return
		}
		enabled := credentialEnabledFromMeta(credential.Meta)
		if _, err := store.NewSiteAPIKeyStateRepository(w.service.db.DB()).MarkSyncFailed(ctx, job.SiteID, *job.SiteCredentialID, enabled, jobErr.Error()); err != nil {
			w.logger.Warn("record api key sync failure failed", "job_id", job.ID, "site_id", job.SiteID, "credential_id", *job.SiteCredentialID, "error", err)
		}
	}
}

func (w *SyncWorker) runJob(ctx context.Context, job store.SiteSyncJob) error {
	switch job.Kind {
	case store.SiteSyncJobKindSiteRefresh:
		_, err := w.service.RefreshState(ctx, job.SiteID)
		return err
	case store.SiteSyncJobKindAPIKeyRefresh:
		if job.SiteCredentialID == nil {
			return fmt.Errorf("site credential id is required")
		}
		credential, err := store.NewSiteCredentialRepository(w.service.db.DB()).GetByID(ctx, *job.SiteCredentialID)
		if err != nil {
			return fmt.Errorf("get credential: %w", err)
		}
		enabled := credentialEnabledFromMeta(credential.Meta)
		if _, err := store.NewSiteAPIKeyStateRepository(w.service.db.DB()).MarkSyncStarted(ctx, job.SiteID, *job.SiteCredentialID, enabled); err != nil {
			return err
		}
		result, err := w.service.RefreshSingleAPIKey(ctx, job.SiteID, *job.SiteCredentialID)
		if err != nil {
			return fmt.Errorf("refresh api key: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(result.State.SyncStatus), "failed") {
			if result.State.SyncMessage.Valid && strings.TrimSpace(result.State.SyncMessage.String) != "" {
				return fmt.Errorf("%s", result.State.SyncMessage.String)
			}
			return fmt.Errorf("api key refresh failed")
		}
		return nil
	default:
		return fmt.Errorf("unsupported site sync job kind %q", job.Kind)
	}
}
