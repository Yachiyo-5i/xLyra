package app

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"xlyra/server/internal/admin"
	"xlyra/server/internal/agentllm"
	"xlyra/server/internal/agentproxy"
	"xlyra/server/internal/analytics"
	"xlyra/server/internal/auth"
	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/dashboard"
	"xlyra/server/internal/downloads"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/health"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/newapi"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/observability"
	"xlyra/server/internal/playground"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/settings"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
	"xlyra/server/internal/systemstats"
	"xlyra/server/internal/usage"
	"xlyra/server/internal/version"
)

func NewRouter(cfg config.Config, logger *slog.Logger, db *store.Store, confFile *config.ConfigFile, masterKey string) http.Handler {
	router, _ := NewRouterWithGateway(cfg, logger, db, confFile, masterKey)
	return router
}

func NewRouterWithGateway(cfg config.Config, logger *slog.Logger, db *store.Store, confFile *config.ConfigFile, masterKey string) (http.Handler, *gateway.Handler) {
	appTimeZone := config.ResolveTimeZone()
	var authService *auth.Service
	if db != nil {
		authService = auth.NewService(db.DB(), masterKey, confFile)
	}

	httpClients := httpclient.NewManager(confFile)
	defaultHTTPClient, _ := httpClients.Client(httpclient.DefaultProfile())
	streamingHTTPClient, _ := httpClients.Client(httpclient.StreamingProfile(httpclient.DefaultProfile()))
	newAPIService := newapi.NewServiceWithHTTPClient(defaultHTTPClient)
	var oauthService *oauthsvc.Service
	var siteService *site.Service
	var catalogService *catalog.Service
	var dashboardService *dashboard.Service
	var analyticsService *analytics.Service
	var routerService *routeengine.Service
	var usageService *usage.Service
	if db != nil {
		oauthService = oauthsvc.NewService(db, masterKey, confFile)
		siteService = site.NewServiceWithTimeZone(db, masterKey, appTimeZone, confFile)
		catalogService = catalog.NewService(db, confFile)
		dashboardService = dashboard.NewService(db, appTimeZone)
		analyticsService = analytics.NewService(db, appTimeZone)
		routerService = routeengine.NewService(db)
		usageService = usage.NewService(db, appTimeZone)
	}
	systemStatsService := systemstats.NewService(appTimeZone)
	downloadService := downloads.NewService()
	gatewayHandler := gateway.NewHandlerWithTimeZone(logger.With("thread", "gateway"), authService, routerService, db, masterKey, appTimeZone, confFile)
	playgroundGatewayHandler := gatewayHandler.WithRouteSiteHeader()
	playgroundRoot := filepath.Join(config.ResolveWorkdir(), "playground")
	playgroundService := playground.NewService(logger.With("thread", "playground"), db, gatewayHandler, playgroundRoot)
	playgroundHandler := playground.NewHandler(playgroundService)
	agentLLMHandler := agentllm.NewHandler(logger.With("thread", "agent-llm"), db, gatewayHandler)
	agentProxyHandler := agentproxy.NewHandler(cfg.AgentRunnerBaseURL, cfg.AgentRunnerToken, streamingHTTPClient, logger.With("thread", "agent-proxy"))
	if confFile != nil {
		if value, ok := confFile.Get("agent.runner_base_url"); ok {
			if baseURL, valid := value.(string); valid && strings.TrimSpace(baseURL) != "" {
				agentProxyHandler.SetBaseURL(baseURL)
			}
		}
		if value, ok := confFile.Get("agent.runner_internal_token"); ok {
			if token, valid := value.(string); valid && strings.TrimSpace(token) != "" {
				agentProxyHandler.SetToken(token)
			}
		}
	}
	syncAgentAccessPolicy := func() {
		if db == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sitePolicy, modelPolicy, siteIDs, siteModelIDs := agentproxy.AccessPolicyFromConfig(confFile)
		if err := agentLLMHandler.SyncAccessPolicy(ctx, agentllm.AccessPolicy{SitePolicy: sitePolicy, ModelPolicy: modelPolicy, SiteIDs: siteIDs, SiteModelIDs: siteModelIDs}); err != nil {
			logger.Warn("failed to sync agent access policy", "error", err)
		}
	}
	agentProxyHandler.SetOnSettingsUpdated(syncAgentAccessPolicy)
	if db != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			gatewayHandler.PrewarmModelsCache(ctx)
		}()
		go syncAgentAccessPolicy()
	}
	adminHandler := admin.NewHandler(logger.With("thread", "admin"), authService, siteService, catalogService, routerService, usageService, dashboardService, systemStatsService, &gatewayHandler, newAPIService, oauthService, appTimeZone).WithDownloadService(downloadService).WithTrafficFlowStore(db).WithAnalyticsService(analyticsService)
	healthHandler := health.NewHandler(cfg, db)
	var preRestore, postRestore func(context.Context) error
	if playgroundService != nil {
		preRestore = playgroundService.QuiesceForRestore
		postRestore = playgroundService.RecoverAfterRestore
	}
	settingsHandler := settings.NewHandlerWithBackup(logger.With("thread", "settings"), confFile, db, masterKey, downloadService, playgroundRoot, preRestore, postRestore, appTimeZone)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(httpx.WithRequestIDHeader)
	r.Use(middleware.RealIP)
	r.Use(httpx.IPWhitelist(confFile))
	r.Use(middleware.Recoverer)
	r.Use(routeAwareTimeout(cfg.RequestTimeout))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Access-Token", "X-CSRF-Token"},
		ExposedHeaders:   []string{gateway.RouteSiteHeader},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(observability.RequestLogger(logger))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusNotFound, "not_found", "route not found")
	})
	if cfg.StaticDir != "" {
		r.Handle("/*", spaHandler(cfg.StaticDir))
	}
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	r.Get("/healthz", healthHandler.Healthz)
	r.Get("/readyz", healthHandler.Readyz)
	r.Get("/debug/stats", healthHandler.Stats)

	r.Route("/api", func(api chi.Router) {
		api.Route("/v1", func(v1 chi.Router) {
			v1.Get("/bootstrap/status", adminHandler.BootstrapStatus)
			v1.Get("/auth/state", adminHandler.AuthState)
			v1.Post("/bootstrap/register", adminHandler.BootstrapRegister)
			v1.Post("/auth/session", adminHandler.CreateSession)
			v1.Get("/system/version", func(w http.ResponseWriter, _ *http.Request) {
				httpx.JSON(w, http.StatusOK, version.Current())
			})
			v1.Get("/oauth/providers/codex/callback", adminHandler.CodexOAuthCallback)
			v1.Get("/oauth/providers/antigravity/callback", adminHandler.AntigravityOAuthCallback)
			v1.Get("/site-types", adminHandler.ListSiteTypes)

			v1.Group(func(protected chi.Router) {
				protected.Use(requireAdmin(authService))
				protected.Use(requireAdminCSRF(authService))
				protected.Use(adminHandler.AuditAdminMutation)
				protected.Route("/agent", func(agentRouter chi.Router) {
					agentRouter.Get("/settings", func(w http.ResponseWriter, r *http.Request) {
						agentProxyHandler.GetSettings(w, r, confFile)
					})
					agentRouter.Put("/settings", func(w http.ResponseWriter, r *http.Request) {
						agentProxyHandler.UpdateSettings(w, r, confFile)
					})
					agentRouter.Get("/health", agentProxyHandler.Forward)
					agentRouter.Get("/meta", agentProxyHandler.Forward)
					agentRouter.Get("/model-memory", agentLLMHandler.ModelMemory)
					agentRouter.Get("/config", agentProxyHandler.Forward)
					agentRouter.Put("/config", agentProxyHandler.Forward)
					agentRouter.Get("/sessions", agentProxyHandler.Forward)
					agentRouter.Post("/sessions", agentProxyHandler.Forward)
					agentRouter.Get("/sessions/{id}/transcript", agentProxyHandler.Forward)
					agentRouter.Patch("/sessions/{id}", agentProxyHandler.Forward)
					agentRouter.Delete("/sessions/{id}", agentProxyHandler.Forward)
					agentRouter.Post("/sessions/{id}/retry", agentProxyHandler.Forward)
					agentRouter.Post("/sessions/{id}/stop", agentProxyHandler.Forward)
					agentRouter.Post("/sessions/{id}/compact-context", agentProxyHandler.Forward)
					agentRouter.Post("/sessions/{id}/grant-access", agentProxyHandler.Forward)
					agentRouter.Get("/sessions/{id}/events", agentProxyHandler.Forward)
				})
				protected.Get("/auth/session", adminHandler.CurrentSession)
				protected.Delete("/auth/session", adminHandler.DeleteSession)
				protected.Route("/playground", func(playgroundRouter chi.Router) {
					playgroundRouter.Get("/models", playgroundHandler.Models)
					playgroundRouter.Get("/conversations", playgroundHandler.List)
					playgroundRouter.Get("/conversations/{conversationID}", playgroundHandler.Get)
					playgroundRouter.With(httpx.LimitRequestBody(64*1024*1024)).Post("/conversations/{conversationID}/turns", playgroundHandler.StartTurn)
					playgroundRouter.Get("/conversations/{conversationID}/events", playgroundHandler.Events)
					playgroundRouter.Delete("/conversations/{conversationID}", playgroundHandler.Delete)
					playgroundRouter.Post("/runs/{runID}/cancel", playgroundHandler.Cancel)
					playgroundRouter.Get("/assets/{assetID}", playgroundHandler.Asset)
				})

				protected.Get("/profile", adminHandler.GetProfile)
				protected.Put("/profile/account", adminHandler.UpdateProfileAccount)
				protected.Put("/profile/password", adminHandler.UpdateProfilePassword)
				protected.Get("/profile/sessions", adminHandler.ListProfileSessions)
				protected.Delete("/profile/sessions", adminHandler.DeleteOtherProfileSessions)
				protected.Delete("/profile/sessions/{sessionID}", adminHandler.DeleteProfileSession)
				protected.Get("/profile/access-token", adminHandler.GetProfileAccessToken)
				protected.Post("/profile/access-token", adminHandler.CreateProfileAccessToken)
				protected.Put("/profile/access-token/enabled", adminHandler.UpdateProfileAccessTokenEnabled)
				protected.Delete("/profile/access-token", adminHandler.DeleteProfileAccessToken)
				protected.Post("/profile/totp/setup", adminHandler.SetupProfileTOTP)
				protected.Post("/profile/totp/enable", adminHandler.EnableProfileTOTP)
				protected.Delete("/profile/totp", adminHandler.DisableProfileTOTP)
				protected.Get("/audit-logs", adminHandler.ListAuditLogs)
				protected.Get("/downloads/{downloadID}", downloadService.Download)
				protected.Get("/dashboard/usage", adminHandler.DashboardUsage)
				protected.Get("/dashboard/cooldowns", adminHandler.DashboardCooldowns)
				protected.Get("/dashboard/health", adminHandler.DashboardHealth)
				protected.Get("/dashboard/insights", adminHandler.DashboardInsights)
				protected.Get("/dashboard/epaper-summary", adminHandler.DashboardEpaperSummary)
				protected.Get("/dashboard/resources/stream", adminHandler.DashboardResourceStream)
				protected.Route("/analytics", func(analyticsRouter chi.Router) {
					analyticsRouter.Use(middleware.Compress(5, "application/json"))
					analyticsRouter.Get("/usage", adminHandler.AnalyticsUsage)
					analyticsRouter.Get("/options", adminHandler.AnalyticsOptions)
					analyticsRouter.Get("/api-key-contributions", adminHandler.AnalyticsAPIKeyContributions)
					analyticsRouter.Get("/dataset", adminHandler.AnalyticsDataset)
				})
				protected.Get("/traffic-flow/topology", adminHandler.TrafficFlowTopology)
				protected.Get("/traffic-flow/stream", adminHandler.TrafficFlowStream)
				protected.Get("/settings/site-groups", adminHandler.ListSiteGroups)
				protected.Post("/settings/site-groups", adminHandler.CreateSiteGroup)
				protected.Get("/settings/site-groups/{siteGroupID}", adminHandler.GetSiteGroup)
				protected.Put("/settings/site-groups/{siteGroupID}", adminHandler.UpdateSiteGroup)
				protected.Delete("/settings/site-groups/{siteGroupID}", adminHandler.DeleteSiteGroup)
				protected.Get("/api-keys", adminHandler.ListAPIKeys)
				protected.Post("/api-keys", adminHandler.CreateAPIKey)
				protected.Get("/api-keys/{apiKeyID}", adminHandler.GetAPIKey)
				protected.Get("/api-keys/{apiKeyID}/reveal", adminHandler.RevealAPIKey)
				protected.Post("/api-keys/{apiKeyID}/rotate", adminHandler.RotateAPIKey)
				protected.Put("/api-keys/{apiKeyID}", adminHandler.UpdateAPIKey)
				protected.Post("/api-keys/{apiKeyID}/quota/increase", adminHandler.IncreaseAPIKeyQuota)
				protected.Post("/api-keys/{apiKeyID}/quota/reset", adminHandler.ResetAPIKeyQuota)
				protected.Delete("/api-keys/{apiKeyID}", adminHandler.DeleteAPIKey)
				protected.Get("/api-keys/{apiKeyID}/site-models", adminHandler.ListAPIKeySiteModels)
				protected.Put("/api-keys/{apiKeyID}/site-models", adminHandler.UpdateAPIKeySiteModels)
				protected.Get("/api-keys/{apiKeyID}/sites", adminHandler.ListAPIKeySites)
				protected.Put("/api-keys/{apiKeyID}/sites", adminHandler.UpdateAPIKeySites)
				protected.Get("/api-keys/{apiKeyID}/site-groups", adminHandler.ListAPIKeySiteGroups)
				protected.Put("/api-keys/{apiKeyID}/site-groups", adminHandler.UpdateAPIKeySiteGroups)
				protected.Put("/api-keys/{apiKeyID}/model-mappings", adminHandler.UpdateAPIKeyModelMappings)
				protected.Post("/api-keys/{apiKeyID}/model-check", adminHandler.CheckAPIKeyModel)
				protected.Get("/requests", adminHandler.ListRequestLogs)
				protected.Get("/requests/summary", adminHandler.RequestLogSummary)
				protected.Get("/requests/channel-split", adminHandler.RequestChannelSplit)
				protected.Get("/requests/{requestLogID}", adminHandler.GetRequestLog)
				protected.Post("/site-types/detect", adminHandler.DetectSiteType)
				protected.Get("/sites", adminHandler.ListSites)
				protected.Post("/sites", adminHandler.CreateSite)
				protected.Post("/sites/refresh", adminHandler.RefreshAllSites)
				protected.Get("/sites/{siteID}", adminHandler.GetSite)
				protected.Put("/sites/{siteID}", adminHandler.UpdateSite)
				protected.Put("/sites/{siteID}/enabled", adminHandler.UpdateSiteEnabled)
				protected.Delete("/sites/{siteID}", adminHandler.DeleteSite)
				protected.Post("/sites/{siteID}/refresh", adminHandler.RefreshSite)
				protected.Post("/sites/{siteID}/validate", adminHandler.ValidateSite)
				protected.Get("/sites/{siteID}/health", adminHandler.GetSiteHealth)
				protected.Get("/sites/{siteID}/health/history", adminHandler.GetSiteHealthHistory)
				protected.Get("/sites/{siteID}/health/hourly", adminHandler.GetSiteHealthHourly)
				protected.Post("/sites/{siteID}/health-check", adminHandler.CheckSiteHealth)
				protected.Get("/sites/{siteID}/api-keys", adminHandler.ListSiteAPIKeys)
				protected.Get("/sites/{siteID}/api-keys/{apiKeyID}/reveal", adminHandler.RevealSiteAPIKey)
				protected.Post("/sites/{siteID}/api-keys", adminHandler.CreateSiteAPIKey)
				protected.Put("/sites/{siteID}/api-keys/{apiKeyID}", adminHandler.UpdateSiteAPIKey)
				protected.Delete("/sites/{siteID}/api-keys/{apiKeyID}", adminHandler.DeleteSiteAPIKey)
				protected.Put("/sites/{siteID}/api-keys/{apiKeyID}/secret", adminHandler.UpdateSiteAPIKeySecret)
				protected.Put("/sites/{siteID}/api-keys/{apiKeyID}/models", adminHandler.UpdateSiteAPIKeyModel)
				protected.Post("/sites/{siteID}/api-keys/{apiKeyID}/refresh", adminHandler.RefreshSiteAPIKey)
				protected.Get("/sites/{siteID}/grok/accounts", adminHandler.ListGrokAccounts)
				protected.Post("/sites/{siteID}/grok/device/start", adminHandler.StartGrokDeviceLogin)
				protected.Post("/sites/{siteID}/grok/device/poll", adminHandler.PollGrokDeviceLogin)
				protected.Post("/sites/{siteID}/grok/accounts/refresh", adminHandler.RefreshGrokAccounts)
				protected.Put("/sites/{siteID}/grok/accounts/{credentialID}", adminHandler.UpdateGrokAccount)
				protected.Put("/sites/{siteID}/grok/accounts/{credentialID}/models", adminHandler.UpdateGrokAccountModel)
				protected.Delete("/sites/{siteID}/grok/accounts/{credentialID}", adminHandler.DeleteGrokAccount)
				protected.Post("/sites/{siteID}/grok/accounts/{credentialID}/refresh", adminHandler.RefreshGrokAccount)
				protected.Get("/sites/{siteID}/models", adminHandler.ListSiteModels)
				protected.Post("/sites/{siteID}/models", adminHandler.CreateSiteModel)
				protected.Get("/sites/{siteID}/pricing", adminHandler.ListSitePricing)
				protected.Put("/sites/{siteID}/models/status", adminHandler.UpdateSiteModelsStatus)
				protected.Put("/sites/{siteID}/models/{modelID}", adminHandler.UpdateSiteModel)
				protected.Delete("/sites/{siteID}/models/{modelID}", adminHandler.DeleteSiteModel)
				protected.Post("/sites/{siteID}/models/{modelID}/test", adminHandler.TestSiteModel)
				protected.Post("/sites/{siteID}/sync-models", adminHandler.SyncSiteModels)
				protected.Post("/sites/{siteID}/newapi/user-summary", adminHandler.SiteNewAPIUserSummary)
				protected.Post("/sites/{siteID}/newapi/api-key-summary", adminHandler.SiteNewAPIAPIKeySummary)
				protected.Post("/sites/{siteID}/newapi/checkin", adminHandler.SiteNewAPICheckin)
				protected.Post("/oauth/providers/codex/authorize", adminHandler.StartCodexOAuth)
				protected.Post("/oauth/providers/antigravity/authorize", adminHandler.StartAntigravityOAuth)
				protected.Post("/oauth/providers/claude_code/authorize", adminHandler.StartClaudeCodeOAuth)
				protected.Post("/oauth/providers/claude_code/complete", adminHandler.CompleteClaudeCodeOAuth)
				protected.Post("/oauth/providers/{provider}/callback-url", adminHandler.CompleteOAuthCallbackURL)
				protected.Post("/oauth/import", adminHandler.ImportOAuthAccounts)
				protected.Get("/oauth/connections", adminHandler.ListOAuthConnections)
				protected.Get("/oauth/connections/{connectionID}", adminHandler.GetOAuthConnection)
				protected.Post("/oauth/connections/{connectionID}/refresh", adminHandler.RefreshOAuthConnection)
				protected.Get("/oauth/connections/{connectionID}/reset-credits", adminHandler.ListOAuthConnectionResetCredits)
				protected.Post("/oauth/connections/{connectionID}/reset-credit/consume", adminHandler.ConsumeOAuthConnectionResetCredit)
				protected.Post("/oauth/connections/{connectionID}/export", adminHandler.ExportOAuthConnection)
				protected.Put("/oauth/connections/{connectionID}/models", adminHandler.UpdateOAuthConnectionModel)
				protected.Put("/oauth/connections/{connectionID}/models/status", adminHandler.UpdateOAuthConnectionModelsStatus)
				protected.Post("/newapi/user-summary", adminHandler.NewAPIUserSummary)
				protected.Post("/newapi/api-key-summary", adminHandler.NewAPIAPIKeySummary)
				protected.Post("/newapi/checkin", adminHandler.NewAPICheckin)
				protected.Get("/models", adminHandler.ListModels)
				protected.Post("/models", adminHandler.CreateModel)
				protected.Put("/models/{modelID}", adminHandler.UpdateModel)
				protected.Delete("/models/{modelID}", adminHandler.DeleteModel)
				protected.Get("/models/{modelID}/matrix", adminHandler.GetModelMatrix)
				protected.Put("/models/{modelID}/pricing", adminHandler.UpdateModelPricing)
				protected.Post("/models/{modelID}/pricing/reset", adminHandler.ResetModelPricing)
				protected.Post("/models/{modelID}/aliases", adminHandler.CreateModelAlias)
				protected.Delete("/models/{modelID}/aliases/{aliasID}", adminHandler.DeleteModelAlias)
				protected.Get("/model-prices", adminHandler.ListModelPrices)
				protected.Put("/model-prices/bulk", adminHandler.BulkUpdateModelPrices)
				protected.Put("/site-models/{siteModelID}/canonical", adminHandler.BindSiteModelCanonical)
				protected.Put("/site-models/{siteModelID}/pricing", adminHandler.UpdateSiteModelPricing)
				protected.Post("/site-models/{siteModelID}/pricing/reset", adminHandler.ResetSiteModelPricing)
				protected.Post("/sites/{siteID}/pricing/reset-all", adminHandler.ResetSitePricing)
				protected.Get("/site-pricings", adminHandler.ListAllSitePricings)
				protected.Get("/health/sites", adminHandler.ListSiteHealth)
				protected.Get("/routes", adminHandler.ListRoutes)
				protected.Get("/routes/traces", adminHandler.ListRouteTraces)
				protected.Post("/routes/select", adminHandler.SelectRoute)
				protected.Post("/routes/failover", adminHandler.FailoverRoute)
				protected.Get("/routes/cooldowns", adminHandler.ListRouteCooldowns)
				protected.Post("/routes/cooldowns", adminHandler.CreateRouteCooldown)
				protected.Post("/routes/cooldowns/clear", adminHandler.ClearRouteCooldown)
				protected.Get("/routes/candidates", adminHandler.ListRouteCandidates)
				protected.Get("/settings/system-proxy", settingsHandler.GetSystemProxy)
				protected.Put("/settings/system-proxy", settingsHandler.UpdateSystemProxy)
				protected.Post("/settings/system-proxy/test", settingsHandler.TestSystemProxy)
				protected.Get("/settings/general", settingsHandler.GetGeneral)
				protected.Put("/settings/general", settingsHandler.UpdateGeneral)
				protected.Get("/settings/portal", settingsHandler.GetPortal)
				protected.Put("/settings/portal", settingsHandler.UpdatePortal)
				protected.Get("/settings/rate-limits", settingsHandler.GetRateLimits)
				protected.Put("/settings/rate-limits", settingsHandler.UpdateRateLimits)
				protected.Post("/settings/backup/export", settingsHandler.ExportBackup)
				protected.Post("/settings/backup/import", settingsHandler.ImportBackup)
				protected.Get("/settings/backup/automatic", settingsHandler.GetAutomaticBackup)
				protected.Put("/settings/backup/automatic", settingsHandler.UpdateAutomaticBackup)
				protected.Post("/settings/backup/automatic/test", settingsHandler.TestAutomaticBackup)
				protected.Get("/settings/backup/automatic/files", settingsHandler.ListAutomaticBackupFiles)
				protected.Post("/settings/backup/automatic/run", settingsHandler.RunAutomaticBackup)
				protected.Post("/settings/backup/automatic/files/restore", settingsHandler.RestoreAutomaticBackupFile)
				protected.Get("/settings/backup/automatic/files/restore/active", settingsHandler.GetActiveAutomaticBackupRestoreTask)
				protected.Get("/settings/backup/automatic/files/restore/{taskID}", settingsHandler.GetAutomaticBackupRestoreTask)
				protected.Delete("/settings/backup/automatic/files/restore/{taskID}", settingsHandler.CancelAutomaticBackupRestoreTask)
				protected.Delete("/settings/backup/automatic/files", settingsHandler.DeleteAutomaticBackupFile)
			})
		})
	})

	r.Route("/v1", func(v1 chi.Router) {
		v1.Group(func(identity chi.Router) {
			identity.Use(requireAPIKeyIdentity(authService))
			identity.Get("/user/balance", gatewayHandler.UserBalance)
			identity.Get("/portal/overview", gatewayHandler.PortalOverview)
			identity.Get("/portal/summary", gatewayHandler.PortalSummary)
			identity.Get("/portal/requests", gatewayHandler.PortalRequests)
			identity.Get("/portal/models", gatewayHandler.PortalModels)
		})
		v1.Get("/portal/settings", gatewayHandler.PortalSettings)
		v1.Group(func(protected chi.Router) {
			protected.Use(requireAPIKey(authService))
			// Cap JSON text endpoints; image endpoints keep their own upload limit.
			limitBody := httpx.LimitRequestBody(cfg.MaxRequestBodyBytes)
			protected.With(limitBody).Post("/chat/completions", gatewayHandler.ChatCompletions)
			protected.With(limitBody).Post("/embeddings", gatewayHandler.Embeddings)
			protected.With(limitBody).Post("/audio/speech", gatewayHandler.AudioSpeech)
			protected.Post("/images/generations", gatewayHandler.ImagesGenerations)
			protected.Post("/images/edits", gatewayHandler.ImagesEdits)
			protected.With(limitBody).Post("/messages", gatewayHandler.Messages)
			protected.With(httpx.DecompressRequestBody(cfg.MaxRequestBodyBytes, logger), limitBody).Post("/responses", gatewayHandler.Responses)
			protected.Get("/responses", gatewayHandler.ResponsesWebSocket(cfg.MaxRequestBodyBytes))
			protected.Get("/models", gatewayHandler.Models)
		})
	})

	r.Route("/internal/agent-llm", func(internal chi.Router) {
		limitBody := httpx.LimitRequestBody(cfg.MaxRequestBodyBytes)
		internal.With(limitBody).Post("/credential", agentLLMHandler.Credential)
		internal.With(limitBody).Post("/credential/renew", agentLLMHandler.Renew)
		internal.Group(func(runs chi.Router) {
			runs.Use(agentProxyHandler.RunnerTokenAuth())
			runs.With(limitBody).Post("/runs/register", agentLLMHandler.Register)
			runs.With(limitBody).Post("/runs/end", agentLLMHandler.End)
		})
		internal.With(limitBody).Post("/v1/responses", agentLLMHandler.Responses)
		internal.With(limitBody).Post("/v1/messages", agentLLMHandler.Messages)
		internal.Get("/v1/models", agentLLMHandler.Models)
	})

	r.Route("/api/playground/v1", func(playground chi.Router) {
		playground.Use(requireAPIKey(authService))
		limitBody := httpx.LimitRequestBody(cfg.MaxRequestBodyBytes)
		playground.With(limitBody).Post("/chat/completions", playgroundGatewayHandler.ChatCompletions)
		playground.Post("/images/generations", playgroundGatewayHandler.ImagesGenerations)
		playground.Post("/images/edits", playgroundGatewayHandler.ImagesEdits)
		playground.With(limitBody).Post("/messages", playgroundGatewayHandler.Messages)
		playground.With(limitBody).Post("/responses", playgroundGatewayHandler.Responses)
		playground.Get("/models", playgroundGatewayHandler.Models)
	})

	return r, &gatewayHandler
}

func requireAdmin(authService *auth.Service) func(http.Handler) http.Handler {
	if authService == nil {
		return authUnavailable
	}

	return authService.RequireAdminSession
}

func requireAdminCSRF(authService *auth.Service) func(http.Handler) http.Handler {
	if authService == nil {
		return authUnavailable
	}

	return authService.RequireAdminCSRF
}

func requireAPIKey(authService *auth.Service) func(http.Handler) http.Handler {
	if authService == nil {
		return authUnavailable
	}

	return authService.RequireAPIKey
}

func requireAPIKeyIdentity(authService *auth.Service) func(http.Handler) http.Handler {
	if authService == nil {
		return authUnavailable
	}

	return authService.RequireAPIKeyIdentity
}

func authUnavailable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.Error(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
	})
}

const (
	backupImportPath             = "/api/v1/settings/backup/import"
	backupExportPath             = "/api/v1/settings/backup/export"
	automaticBackupRunPath       = "/api/v1/settings/backup/automatic/run"
	automaticBackupRestorePath   = "/api/v1/settings/backup/automatic/files/restore"
	downloadPathPrefix           = "/api/v1/downloads/"
	backupTransferRequestTimeout = 10 * time.Minute
)

func routeAwareTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	base := middleware.Timeout(timeout)
	backupTransfer := middleware.Timeout(backupTransferRequestTimeout)
	return func(next http.Handler) http.Handler {
		timeoutHandler := base(next)
		backupTransferTimeoutHandler := backupTransfer(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && r.URL.Path == "/v1/responses" && strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
				next.ServeHTTP(w, r)
				return
			}
			if isBackupTransferRequest(r) {
				backupTransferTimeoutHandler.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/dashboard/resources/stream" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/traffic-flow/stream" {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/playground/conversations/") && strings.HasSuffix(r.URL.Path, "/events") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/agent/sessions/") && strings.HasSuffix(r.URL.Path, "/events") {
				next.ServeHTTP(w, r)
				return
			}
			if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/internal/agent-llm/v1/") {
				next.ServeHTTP(w, r)
				return
			}
			gatewayPath := strings.TrimPrefix(r.URL.Path, "/api/playground")
			if r.Method == http.MethodPost && (gatewayPath == "/v1/chat/completions" || gatewayPath == "/v1/responses" || gatewayPath == "/v1/images/generations" || gatewayPath == "/v1/images/edits" || gatewayPath == "/v1/messages" || gatewayPath == "/v1/audio/speech") {
				next.ServeHTTP(w, r)
				return
			}
			timeoutHandler.ServeHTTP(w, r)
		})
	}
}

func isBackupTransferRequest(r *http.Request) bool {
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, downloadPathPrefix) {
		return true
	}
	return r.Method == http.MethodPost && (r.URL.Path == backupImportPath || r.URL.Path == backupExportPath || r.URL.Path == automaticBackupRunPath || r.URL.Path == automaticBackupRestorePath)
}

func spaHandler(staticDir string) http.Handler {
	fs := http.FileServer(http.Dir(staticDir))
	index := filepath.Join(staticDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(staticDir, filepath.Clean("/"+r.URL.Path))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			http.ServeFile(w, r, index)
			return
		}
		fs.ServeHTTP(w, r)
	})
}
