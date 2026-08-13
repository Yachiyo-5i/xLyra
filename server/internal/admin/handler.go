package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/dashboard"
	"xlyra/server/internal/downloads"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/newapi"
	oauthsvc "xlyra/server/internal/oauth"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
	"xlyra/server/internal/systemstats"
	"xlyra/server/internal/usage"
)

var adminTimeZoneMu sync.RWMutex
var adminTimeZoneValue = config.ResolveTimeZone()

type Handler struct {
	logger    *slog.Logger
	auth      *auth.Service
	sites     *site.Service
	catalog   *catalog.Service
	router    *routeengine.Service
	usage     *usage.Service
	dashboard *dashboard.Service
	system    *systemstats.Service
	gateway   *gateway.Handler
	newAPI    *newapi.Service
	oauth     *oauthsvc.Service
	downloads *downloads.Service
	timeZone  config.TimeZone
	trafficDB *store.Store
}

func (h Handler) WithTrafficFlowStore(db *store.Store) Handler {
	h.trafficDB = db
	return h
}

func NewHandler(logger *slog.Logger, authService *auth.Service, siteService *site.Service, catalogService *catalog.Service, routerService *routeengine.Service, usageService *usage.Service, dashboardService *dashboard.Service, systemStatsService *systemstats.Service, gatewayHandler *gateway.Handler, newAPIService *newapi.Service, oauthService *oauthsvc.Service, timeZones ...config.TimeZone) Handler {
	timeZone := config.TimeZoneOrDefault(timeZones...)
	setAdminTimeZone(timeZone)
	return Handler{
		logger:    logger,
		auth:      authService,
		sites:     siteService,
		catalog:   catalogService,
		router:    routerService,
		usage:     usageService,
		dashboard: dashboardService,
		system:    systemStatsService,
		gateway:   gatewayHandler,
		newAPI:    newAPIService,
		oauth:     oauthService,
		timeZone:  timeZone,
	}
}

func (h Handler) WithDownloadService(downloadService *downloads.Service) Handler {
	h.downloads = downloadService
	return h
}

func (h Handler) logInfo(message string, args ...any) {
	if h.logger != nil {
		h.logger.Info(message, append([]any{"scope", "admin"}, args...)...)
	}
}

func (h Handler) logWarn(message string, args ...any) {
	if h.logger != nil {
		h.logger.Warn(message, append([]any{"scope", "admin"}, args...)...)
	}
}

func (h Handler) invalidateGatewayModelsCache() {
	if h.gateway != nil {
		h.gateway.InvalidateModelsCache()
	}
}

func (h Handler) invalidateGatewayModelsCacheForAPIKey(apiKeyID uuid.UUID) {
	if h.gateway != nil {
		h.gateway.InvalidateModelsCacheForAPIKey(apiKeyID)
	}
}

func (h Handler) prewarmGatewayModelsCacheForAPIKey(apiKey store.APIKey) {
	if h.gateway == nil || apiKey.ID == uuid.Nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		h.gateway.PrewarmModelsCacheForAPIKey(ctx, apiKey)
	}()
}

type newAPIUserSummaryRequest struct {
	BaseURL     string `json:"base_url"`
	AccessToken string `json:"access_token"`
	UserID      int    `json:"user_id"`
}

type routeSelectionRequest struct {
	ModelKey            string      `json:"model_key"`
	Debug               bool        `json:"debug"`
	ExcludeSiteIDs      []uuid.UUID `json:"exclude_site_ids"`
	ExcludeSiteModelIDs []uuid.UUID `json:"exclude_site_model_ids"`
	FailedSiteIDs       []uuid.UUID `json:"failed_site_ids"`
	FailedSiteModelIDs  []uuid.UUID `json:"failed_site_model_ids"`
	Limit               int         `json:"limit"`
	FailoverLimit       int         `json:"failover_limit"`
}

type routeCooldownRequest struct {
	SiteID           uuid.UUID  `json:"site_id"`
	SiteModelID      *uuid.UUID `json:"site_model_id"`
	SiteCredentialID *uuid.UUID `json:"site_credential_id"`
	Scope            string     `json:"scope"`
	Source           string     `json:"source"`
	Reason           string     `json:"reason"`
	DurationSeconds  int        `json:"duration_seconds"`
}

type routeCooldownClearRequest struct {
	SiteID           uuid.UUID  `json:"site_id"`
	SiteModelID      *uuid.UUID `json:"site_model_id"`
	SiteCredentialID *uuid.UUID `json:"site_credential_id"`
	Source           string     `json:"source"`
}

type siteUpsertRequest struct {
	Name            string               `json:"name"`
	Slug            string               `json:"slug"`
	SiteType        string               `json:"site_type"`
	BaseURL         string               `json:"base_url"`
	Enabled         *bool                `json:"enabled"`
	RoutingPriority *float64             `json:"routing_priority"`
	ProxyID         *string              `json:"proxy_id"`
	RequestHeaders  *[]siteRequestHeader `json:"request_headers"`
	Gateway         *siteGatewayRequest  `json:"gateway"`
	NewAPI          *siteNewAPIRequest   `json:"newapi"`
	XLyra           *siteXLyraRequest    `json:"xlyra"`
	APIKey          string               `json:"api_key"`
	APIKeys         []siteAPIKeyRequest  `json:"api_keys"`
	SkipRefresh     bool                 `json:"skip_refresh"`
}

type siteAPIKeyRequest struct {
	APIKey                 string   `json:"api_key"`
	Name                   string   `json:"name"`
	RoutingPriority        *float64 `json:"routing_priority"`
	UpstreamCostMultiplier *float64 `json:"upstream_cost_multiplier"`
}

type oauthAuthorizeRequest struct {
	PublicBaseURL      string              `json:"public_base_url"`
	RedirectURL        string              `json:"redirect_url"`
	FailureRedirectURL string              `json:"failure_redirect_url"`
	Site               *oauthAuthorizeSite `json:"site"`
	Metadata           map[string]any      `json:"metadata"`
}

type oauthAuthorizeSite struct {
	SiteID          string              `json:"site_id"`
	Name            string              `json:"name"`
	Slug            string              `json:"slug"`
	BaseURL         string              `json:"base_url"`
	Enabled         *bool               `json:"enabled"`
	RoutingPriority *float64            `json:"routing_priority"`
	ProxyID         *string             `json:"proxy_id"`
	Gateway         *siteGatewayRequest `json:"gateway"`
}

type oauthResetCreditConsumeRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
	CreditID       string `json:"credit_id"`
}

type siteNewAPIRequest struct {
	AccessToken string `json:"access_token"`
	UserID      int    `json:"user_id"`
}

type siteXLyraRequest struct {
	AuthMode    string `json:"auth_mode"`
	AccessToken string `json:"access_token"`
	APIKey      string `json:"api_key"`
}

type siteRequestHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type siteGatewayRequest struct {
	RequestTimeoutMS               *int     `json:"request_timeout_ms"`
	ConnectTimeoutMS               *int     `json:"connect_timeout_ms"`
	ResponseHeaderTimeoutMS        *int     `json:"response_header_timeout_ms"`
	MaxConcurrency                 *int     `json:"max_concurrency"`
	MaxModelConcurrency            *int     `json:"max_model_concurrency"`
	MaxCredentialConcurrency       *int     `json:"max_credential_concurrency"`
	MaxIdleConns                   *int     `json:"max_idle_conns"`
	MaxIdleConnsPerHost            *int     `json:"max_idle_conns_per_host"`
	MaxConnsPerHost                *int     `json:"max_conns_per_host"`
	IdleConnTimeoutMS              *int     `json:"idle_conn_timeout_ms"`
	ResponsesToolPolicy            string   `json:"responses_tool_policy"`
	DisabledResponsesTools         []string `json:"disabled_responses_tools"`
	ResponsesImageGenerationPolicy string   `json:"responses_image_generation_policy"`
	ImpersonateCodexClient         *bool    `json:"impersonate_codex_client"`
	ImpersonateClaudeCodeClient    *bool    `json:"impersonate_claude_code_client"`
	QuotaProbe                     *string  `json:"quota_probe"`
}

func (r *siteGatewayRequest) toSiteGatewayConfig() *site.GatewayConfig {
	if r == nil {
		return nil
	}
	return &site.GatewayConfig{
		RequestTimeoutMS:               r.RequestTimeoutMS,
		ConnectTimeoutMS:               r.ConnectTimeoutMS,
		ResponseHeaderTimeoutMS:        r.ResponseHeaderTimeoutMS,
		MaxConcurrency:                 r.MaxConcurrency,
		MaxModelConcurrency:            r.MaxModelConcurrency,
		MaxCredentialConcurrency:       r.MaxCredentialConcurrency,
		MaxIdleConns:                   r.MaxIdleConns,
		MaxIdleConnsPerHost:            r.MaxIdleConnsPerHost,
		MaxConnsPerHost:                r.MaxConnsPerHost,
		IdleConnTimeoutMS:              r.IdleConnTimeoutMS,
		ResponsesToolPolicy:            r.ResponsesToolPolicy,
		DisabledResponsesTools:         r.DisabledResponsesTools,
		ResponsesImageGenerationPolicy: r.ResponsesImageGenerationPolicy,
		ImpersonateCodexClient:         r.ImpersonateCodexClient,
		ImpersonateClaudeCodeClient:    r.ImpersonateClaudeCodeClient,
		QuotaProbe:                     r.QuotaProbe,
	}
}

type newAPIAPIKeySummaryRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type newAPICheckinRequest struct {
	BaseURL     string `json:"base_url"`
	AccessToken string `json:"access_token"`
	UserID      int    `json:"user_id"`
}

func siteIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	siteID, err := uuid.Parse(chi.URLParam(r, "siteID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_site_id", "site id must be a valid UUID")
		return uuid.Nil, false
	}

	return siteID, true
}

func modelIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	modelID, err := uuid.Parse(chi.URLParam(r, "modelID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_model_id", "model id must be a valid UUID")
		return uuid.Nil, false
	}

	return modelID, true
}

func siteModelIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	modelID, err := uuid.Parse(chi.URLParam(r, "siteModelID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_site_model_id", "site model id must be a valid UUID")
		return uuid.Nil, false
	}

	return modelID, true
}

func aliasIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	aliasID, err := uuid.Parse(chi.URLParam(r, "aliasID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_alias_id", "alias id must be a valid UUID")
		return uuid.Nil, false
	}

	return aliasID, true
}

func apiKeyIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	apiKeyID, err := uuid.Parse(chi.URLParam(r, "apiKeyID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_api_key_id", "api key id must be a valid UUID")
		return uuid.Nil, false
	}

	return apiKeyID, true
}

func siteGroupIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	siteGroupID, err := uuid.Parse(chi.URLParam(r, "siteGroupID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_site_group_id", "site group id must be a valid UUID")
		return uuid.Nil, false
	}

	return siteGroupID, true
}

func connectionIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	connectionID, err := uuid.Parse(chi.URLParam(r, "connectionID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_connection_id", "connection id must be a valid UUID")
		return uuid.Nil, false
	}
	return connectionID, true
}

func metadataMap(source map[string]any, key string) any {
	value, ok := source[key].(map[string]any)
	if !ok {
		return nil
	}
	return value
}

func metadataString(source map[string]any, key string) any {
	value, ok := source[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func valueOrFallback(value any, fallback any) any {
	if value != nil {
		return value
	}
	if text, ok := fallback.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return text
	}
	return fallback
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func nullBoolValue(value sql.NullBool) any {
	if !value.Valid {
		return nil
	}
	return value.Bool
}

func nullTimeValue(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return adminTimeZone().Format(value.Time, time.RFC3339)
}

func timeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return timeString(value)
}

func timePtrValue(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return timeString(*value)
}

func timeString(value time.Time) string {
	return adminTimeZone().Format(value, time.RFC3339)
}

func pointerStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func pointerUUIDValue(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func (h Handler) decodeRouteSelectionRequest(w http.ResponseWriter, r *http.Request) (routeengine.CandidateQuery, bool) {
	var payload routeSelectionRequest
	if !h.decodeJSON(w, r, &payload) {
		return routeengine.CandidateQuery{}, false
	}

	query, err := payload.toCandidateQuery()
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_route_query", err.Error())
		return routeengine.CandidateQuery{}, false
	}
	return query, true
}

func (r routeSelectionRequest) toCandidateQuery() (routeengine.CandidateQuery, error) {
	if strings.TrimSpace(r.ModelKey) == "" {
		return routeengine.CandidateQuery{}, fmt.Errorf("model_key is required")
	}

	query := routeengine.CandidateQuery{
		ModelKey:            r.ModelKey,
		Debug:               r.Debug,
		ExcludeSiteIDs:      append([]uuid.UUID(nil), r.ExcludeSiteIDs...),
		ExcludeSiteModelIDs: append([]uuid.UUID(nil), r.ExcludeSiteModelIDs...),
		Limit:               r.Limit,
		FailoverLimit:       r.FailoverLimit,
	}

	if query.Limit < 0 {
		return routeengine.CandidateQuery{}, fmt.Errorf("limit must be >= 0")
	}
	if query.FailoverLimit < 0 {
		return routeengine.CandidateQuery{}, fmt.Errorf("failover_limit must be >= 0")
	}

	return query, nil
}

func uuidStrings(items []uuid.UUID) []string {
	if len(items) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.String())
	}
	return result
}

func pointerFloat64Value(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func pointerInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func emptyStringAsNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullFloat64Value(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}

func scaledNullFloat(base sql.NullFloat64, ratio sql.NullFloat64) any {
	if !base.Valid || !ratio.Valid {
		return nil
	}
	return base.Float64 * ratio.Float64
}

func chainedScaledNullFloat(base sql.NullFloat64, ratioA sql.NullFloat64, ratioB sql.NullFloat64) any {
	if !base.Valid || !ratioA.Valid {
		return nil
	}
	completion := 1.0
	if ratioB.Valid {
		completion = ratioB.Float64
	}
	return base.Float64 * ratioA.Float64 * completion
}

func nullInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func nullUUIDValue(value uuid.NullUUID) any {
	if !value.Valid {
		return nil
	}
	return value.UUID.String()
}

func optionalUUIDQuery(w http.ResponseWriter, r *http.Request, key string) (*uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, true
	}
	value, err := uuid.Parse(raw)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_"+key, key+" must be a valid UUID")
		return nil, false
	}
	return &value, true
}

func optionalTimeQuery(w http.ResponseWriter, r *http.Request, key string) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, true
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_"+key, key+" must be an RFC3339 timestamp")
		return nil, false
	}
	return &value, true
}

func requestLogIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, "requestLogID"))
	value, err := uuid.Parse(raw)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_request_log_id", "requestLogID must be a valid UUID")
		return uuid.Nil, false
	}
	return value, true
}

func uuidPtrString(value *uuid.UUID) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func timePtrString(value *time.Time) any {
	if value == nil {
		return nil
	}
	return adminTimeZone().Format(*value, time.RFC3339Nano)
}

func adminTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	if len(timeZones) > 0 && timeZones[0].Location != nil {
		return timeZones[0]
	}
	adminTimeZoneMu.RLock()
	defer adminTimeZoneMu.RUnlock()
	return adminTimeZoneValue
}

func setAdminTimeZone(timeZone config.TimeZone) {
	if timeZone.Location == nil {
		timeZone = config.ResolveTimeZone()
	}
	adminTimeZoneMu.Lock()
	defer adminTimeZoneMu.Unlock()
	adminTimeZoneValue = timeZone
}

func jsonRaw(raw []byte) any {
	if len(raw) == 0 {
		return map[string]any{}
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}

	return value
}

func requestLogMetadata(raw []byte) map[string]any {
	value, ok := jsonRaw(raw).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return value
}
