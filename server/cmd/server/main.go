package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"xlyra/server/internal/app"
	"xlyra/server/internal/backup"
	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/observability"
	"xlyra/server/internal/scheduler"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func main() {
	os.Exit(run())
}

// run holds all startup/shutdown logic so its deferred cleanup (logger, DB,
// scheduler, background tasks) always executes. Error paths return a non-zero
// code instead of calling os.Exit mid-function, which would skip every defer.
func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("load config: %w", err))
		return 1
	}

	workdir := config.ResolveWorkdir()
	confFile, err := config.LoadConfigFile(workdir)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("load config file: %w", err))
		return 1
	}

	masterKey, err := config.LoadMasterKey(workdir)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("load master key: %w", err))
		return 1
	}

	generalConfig := config.ReadGeneralConfig(confFile)
	appTimeZone := config.ResolveTimeZone()
	logDir := resolveLogDir(workdir, cfg.LogDir)
	logService, err := observability.NewLogger(observability.LoggerOptions{
		Level:          generalConfig.Log.Level,
		DebugEnabled:   cfg.LogDebugEnabled,
		Directory:      logDir,
		FilePrefix:     cfg.LogFilePrefix,
		CleanupEnabled: generalConfig.Log.CleanupEnabled,
		RetentionDays:  generalConfig.Log.RetentionDays,
		ToStdout:       cfg.LogToStdout,
		TimeZone:       appTimeZone,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("open logger: %w", err))
		return 1
	}
	defer logService.Close()

	logger := logService.With("thread", "server")
	logger.Info(
		"startup configuration loaded",
		"app", cfg.AppName,
		"env", cfg.AppEnv,
		"http_addr", fmt.Sprintf("%s:%d", cfg.HTTPHost, cfg.HTTPPort),
		"log_dir", logDir,
		"log_retention_days", generalConfig.Log.RetentionDays,
		"log_to_stdout", cfg.LogToStdout,
		"debug_enabled", cfg.LogDebugEnabled,
		"config_file", confFile.Path(),
	)
	confFile.OnChange(func(map[string]any) {
		go func() {
			updated := config.ReadGeneralConfig(confFile)
			logService.SetLevel(updated.Log.Level, cfg.LogDebugEnabled)
			logService.SetRetention(updated.Log.CleanupEnabled, updated.Log.RetentionDays)
			logger.Info("runtime log config applied", "log_level", updated.Log.Level, "cleanup_enabled", updated.Log.CleanupEnabled, "retention_days", updated.Log.RetentionDays)
		}()
	})

	logger.Info("database initialization checking", "database", cfg.DatabaseName(), "timeout", cfg.DBConnectTimeout)
	initCtx, initCancel := context.WithTimeout(context.Background(), cfg.DBConnectTimeout)
	if err := store.EnsureDatabaseInitialized(initCtx, cfg); err != nil {
		initCancel()
		logger.Error("database initialization failed", "error", err)
		return 1
	}
	initCancel()
	logger.Info("database initialization ready", "database", cfg.DatabaseName())

	dbCtx, dbCancel := context.WithTimeout(context.Background(), cfg.DBConnectTimeout)
	defer dbCancel()

	logger.Info("database connection opening", "timeout", cfg.DBConnectTimeout, "min_conns", cfg.DBMinConns, "max_conns", cfg.DBMaxConns)
	db, err := store.Open(dbCtx, cfg)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		return 1
	}
	defer db.Close()
	logger.Info("database connection ready")

	logger.Info("business services initializing")
	siteService := site.NewServiceWithTimeZone(db, masterKey, appTimeZone, confFile)
	syncService := catalog.NewSyncService(db, logger.With("thread", "models-dev-sync"), confFile)
	usageSummaryService := usage.NewSummaryService(db, confFile, appTimeZone)
	backupService := backup.NewService(db, confFile, masterKey, filepath.Join(config.ResolveWorkdir(), "playground"), appTimeZone)
	automaticBackupService := backup.NewAutomaticService(backupService, masterKey)
	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if err := catalog.ReconcileCategories(reconcileCtx, db.DB()); err != nil {
		logger.Warn("startup canonical model category reconciliation failed", "error", err)
	}
	reconcileCancel()
	router, gatewayHandler := app.NewRouterWithGateway(cfg, logger, db, confFile, masterKey)
	schedule := scheduler.New(logger.With("thread", "scheduler"), scheduler.Options{
		SiteHealthInterval: cfg.SiteHealthInterval,
		SiteHealthTimeout:  cfg.SiteHealthTimeout,
		SiteHealthWorkers:  cfg.SiteHealthWorkers,
		ConfigFile:         confFile,
	}, siteService, syncService, usageSummaryService, automaticBackupService).WithModelsCacheInvalidator(gatewayHandler.InvalidateModelsCache)
	logger.Info("business services ready")

	logger.Info("scheduler initializing", "site_health_interval", cfg.SiteHealthInterval, "site_health_timeout", cfg.SiteHealthTimeout, "site_health_workers", cfg.SiteHealthWorkers)
	schedule.RegisterDefaultJobs()
	schedule.Start()
	defer schedule.Stop()
	logger.Info("scheduler started")

	// Startup background tasks are tied to a shutdown-linked context and joined
	// before the DB is closed, so cancelling on shutdown stops them promptly and
	// they never touch a closed database. This defer runs before db.Close (LIFO).
	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	var backgroundTasks sync.WaitGroup
	defer func() {
		backgroundCancel()
		backgroundTasks.Wait()
	}()

	siteSyncWorker := site.NewSyncWorker(siteService, logger.With("thread", "site-sync-worker"), gatewayHandler.InvalidateModelsCache)
	backgroundTasks.Add(1)
	go func() {
		defer backgroundTasks.Done()
		siteSyncWorker.Run(backgroundCtx)
	}()

	backgroundTasks.Add(1)
	go func() {
		defer backgroundTasks.Done()
		ctx, cancel := context.WithTimeout(backgroundCtx, 2*time.Minute)
		defer cancel()
		if err := syncService.SyncAll(ctx); err != nil {
			logger.Warn("startup models.dev sync failed", "error", err)
		} else {
			gatewayHandler.InvalidateModelsCache()
			logger.Info("startup models.dev sync completed")
		}
	}()

	backgroundTasks.Add(1)
	go func() {
		defer backgroundTasks.Done()
		start := time.Now()
		ctx, cancel := context.WithTimeout(backgroundCtx, 30*time.Minute)
		defer cancel()
		result, err := usageSummaryService.StartupCheck(ctx, start)
		if err != nil {
			logger.Warn("startup usage summary check failed", "error", err, "duration", time.Since(start))
			return
		}
		logger.Info("startup usage summary check completed", "summarized_days", result.SummarizedDays, "backfilled_cached_usage_records", result.BackfilledCachedUsageRecords, "rebuilt_cached_token_days", result.RebuiltCachedTokenDays, "rebuilt_hourly_rows", result.RebuiltHourlyRows, "deleted_hourly_rows", result.DeletedHourlyRows, "duration", time.Since(start))
	}()

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.HTTPHost, cfg.HTTPPort),
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	logger.Info("http router initialized", "read_header_timeout", cfg.ReadHeaderTimeout, "request_timeout", cfg.RequestTimeout)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	logger.Info("http server listening", "app", cfg.AppName, "addr", server.Addr, "env", cfg.AppEnv)

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped unexpectedly", "error", err)
			return 1
		}
	case <-stopCtx.Done():
		logger.Info("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			return 1
		}

		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server stopped with error", "error", err)
			return 1
		}
	}

	logger.Info("server stopped")
	return 0
}

func resolveLogDir(workdir string, configured string) string {
	if configured == "" || configured == "logs" || configured == "data/logs" {
		return filepath.Join(workdir, "logs")
	}
	if filepath.IsAbs(configured) {
		return configured
	}
	return filepath.Join(workdir, configured)
}
