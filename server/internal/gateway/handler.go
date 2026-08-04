package gateway

import (
	"log/slog"
	"net/http"
	"time"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	"xlyra/server/internal/credential"
	"xlyra/server/internal/httpclient"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/ratelimit"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

const RouteSiteHeader = "X-Xlyra-Route-Site"

type Handler struct {
	logger              *slog.Logger
	auth                *auth.Service
	router              *routeengine.Service
	db                  *store.Store
	credentials         credential.Service
	httpClient          *http.Client
	clients             *httpclient.Manager
	limits              *upstreamConcurrencyCache
	recorder            Recorder
	oauth               *oauthsvc.Service
	rateLimits          *ratelimit.Service
	modelsCache         *modelsCache
	noRoutes            *noRouteSuppressionCache
	userBalanceThrottle *userBalanceThrottle
	credentialSelector  *credentialWindowSelector
	confFile            *config.ConfigFile
	usage               *usage.Service
	timeZone            config.TimeZone
	exposeRouteSite     bool
}

func (h Handler) WithRouteSiteHeader() Handler {
	h.exposeRouteSite = true
	return h
}

func NewHandler(logger *slog.Logger, authService *auth.Service, routerService *routeengine.Service, db *store.Store, masterKey string, confFiles ...*config.ConfigFile) Handler {
	return NewHandlerWithTimeZone(logger, authService, routerService, db, masterKey, config.ResolveTimeZone(), confFiles...)
}

func NewHandlerWithTimeZone(logger *slog.Logger, authService *auth.Service, routerService *routeengine.Service, db *store.Store, masterKey string, timeZone config.TimeZone, confFiles ...*config.ConfigFile) Handler {
	var confFile *config.ConfigFile
	if len(confFiles) > 0 {
		confFile = confFiles[0]
	}
	clientManager := httpclient.NewManager(confFile)
	defaultClient, _ := clientManager.Client(httpclient.DefaultProfile())
	var usageService *usage.Service
	if db != nil {
		usageService = usage.NewService(db, timeZone)
	}
	return Handler{
		logger:              logger,
		auth:                authService,
		router:              routerService,
		db:                  db,
		credentials:         credential.NewService(masterKey),
		httpClient:          defaultClient,
		clients:             clientManager,
		limits:              newUpstreamConcurrencyCache(),
		recorder:            NewRecorder(db, logger, timeZone),
		oauth:               oauthsvc.NewService(db, masterKey, confFile),
		rateLimits:          ratelimit.NewService(db),
		modelsCache:         newModelsCache(),
		noRoutes:            newNoRouteSuppressionCache(),
		userBalanceThrottle: newUserBalanceThrottle(),
		credentialSelector:  newCredentialWindowSelector(time.Minute),
		confFile:            confFile,
		usage:               usageService,
		timeZone:            timeZone,
	}
}
