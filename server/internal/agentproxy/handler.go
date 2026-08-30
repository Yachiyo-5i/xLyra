package agentproxy

import (
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
)

var (
	errEmptyRunnerURL   = errors.New("runner URL must not be empty")
	errInvalidRunnerURL = errors.New("runner URL must be an HTTP or HTTPS URL without credentials")
)

type Handler struct {
	mu      sync.RWMutex
	baseURL string
	token   string
	client  *http.Client
	logger  *slog.Logger

	onSettingsUpdated func()
}

type Settings struct {
	RunnerBaseURL         string   `json:"runner_base_url"`
	RunnerTokenConfigured bool     `json:"runner_token_configured"`
	AllowedSiteIDs        []string `json:"allowed_site_ids"`
	AllowedSiteModelIDs   []string `json:"allowed_site_model_ids"`
	SitePolicy            string   `json:"site_policy"`
	ModelPolicy           string   `json:"model_policy"`
}

// settingsInput uses partial-update semantics: absent fields are left unchanged,
// so runner connection and site/model scope can be saved independently.
type settingsInput struct {
	RunnerBaseURL       *string   `json:"runner_base_url"`
	RunnerToken         *string   `json:"runner_token"`
	ClearRunner         bool      `json:"clear_runner"`
	AllowedSiteIDs      *[]string `json:"allowed_site_ids"`
	AllowedSiteModelIDs *[]string `json:"allowed_site_model_ids"`
	SitePolicy          *string   `json:"site_policy"`
	ModelPolicy         *string   `json:"model_policy"`
}

func NewHandler(baseURL, token string, client *http.Client, logger *slog.Logger) *Handler {
	return &Handler{baseURL: normalizeBaseURL(baseURL), token: strings.TrimSpace(token), client: client, logger: logger}
}

func (h *Handler) Forward(w http.ResponseWriter, r *http.Request) {
	baseURL, token, client := h.snapshot()
	if baseURL == "" {
		httpx.Error(w, r, http.StatusServiceUnavailable, "agent_unavailable", "agent runner URL is not configured")
		return
	}
	if token == "" {
		httpx.Error(w, r, http.StatusServiceUnavailable, "agent_unavailable", "agent runner internal token is not configured")
		return
	}
	if client == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "agent_unavailable", "agent runner is not configured")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agent")
	if path == "" {
		path = "/"
	}
	// Public xLyra paths that differ from the runner's internal paths are mapped here.
	switch path {
	case "/config":
		path = "/internal/agent/config"
	case "/version":
		path = "/internal/agent/version"
	case "/upgrade":
		path = "/internal/agent/upgrade"
	case "/skills":
		path = "/internal/agent/skills"
	case "/workspace/file":
		path = "/internal/agent/workspace/file"
	default:
		if strings.HasPrefix(path, "/skills/") {
			path = "/internal/agent" + path
		}
	}
	target, err := url.Parse(baseURL + path)
	if err != nil {
		httpx.Error(w, r, http.StatusBadGateway, "agent_runner_unavailable", "agent runner URL is invalid")
		return
	}
	target.RawQuery = r.URL.RawQuery
	request, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		httpx.Error(w, r, http.StatusBadGateway, "agent_runner_unavailable", "unable to create runner request")
		return
	}
	copyRequestHeaders(request.Header, r.Header)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		httpx.Error(w, r, http.StatusBadGateway, "agent_runner_unavailable", "unable to reach agent runner")
		return
	}
	defer response.Body.Close()
	copyResponseHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		// SSE must flush per write; io.Copy's buffer batches small events and
		// reads as stutter on the client.
		flushCopy(w, response.Body)
		return
	}
	_, _ = io.Copy(w, response.Body)
}

// flushCopy streams src to w, flushing after every write so SSE events reach
// the client as they arrive instead of accumulating in buffers.
func flushCopy(w io.Writer, src io.Reader) {
	flusher, canFlush := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request, confFile *config.ConfigFile) {
	settings := Settings{RunnerBaseURL: h.snapshotBaseURL(), RunnerTokenConfigured: h.snapshotToken() != "", AllowedSiteIDs: []string{}, AllowedSiteModelIDs: []string{}, SitePolicy: "allow_all", ModelPolicy: "allow_all"}
	if confFile != nil {
		if value, ok := confFile.Get("agent.runner_base_url"); ok {
			if baseURL, valid := value.(string); valid && strings.TrimSpace(baseURL) != "" {
				settings.RunnerBaseURL = normalizeBaseURL(baseURL)
			}
		}
		if value, ok := confFile.Get("agent.runner_internal_token"); ok {
			if token, valid := value.(string); valid && strings.TrimSpace(token) != "" {
				settings.RunnerTokenConfigured = true
			}
		}
		settings.AllowedSiteIDs = readStringList(confFile, "agent.allowed_site_ids")
		settings.AllowedSiteModelIDs = readStringList(confFile, "agent.allowed_site_model_ids")
		if value, ok := confFile.Get("agent.site_policy"); ok {
			if policy, valid := value.(string); valid && (policy == "allow_all" || policy == "allow_list") {
				settings.SitePolicy = policy
			}
		}
		if value, ok := confFile.Get("agent.model_policy"); ok {
			if policy, valid := value.(string); valid && (policy == "allow_all" || policy == "allow_list") {
				settings.ModelPolicy = policy
			}
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request, confFile *config.ConfigFile) {
	if confFile == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "config_unavailable", "config persistence is not available")
		return
	}
	var input settingsInput
	if err := httpx.DecodeJSONBody(r, &input); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if input.ClearRunner {
		if err := confFile.Delete("agent.runner_base_url"); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to clear Agent runner settings")
			return
		}
		if err := confFile.Delete("agent.runner_internal_token"); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to clear Agent runner settings")
			return
		}
		h.SetBaseURL("")
		h.SetToken("")
	}
	if input.RunnerBaseURL != nil {
		baseURL, err := validateBaseURL(*input.RunnerBaseURL)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_runner_url", err.Error())
			return
		}
		if err := confFile.Set("agent.runner_base_url", baseURL); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent runner settings")
			return
		}
		h.SetBaseURL(baseURL)
	}
	if input.RunnerToken != nil {
		if token := strings.TrimSpace(*input.RunnerToken); token != "" {
			if err := confFile.Set("agent.runner_internal_token", token); err != nil {
				httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent runner token")
				return
			}
			h.SetToken(token)
		}
	}
	scopeUpdated := false
	if input.AllowedSiteIDs != nil {
		if err := confFile.Set("agent.allowed_site_ids", normalizeStringList(*input.AllowedSiteIDs)); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent site settings")
			return
		}
		scopeUpdated = true
	}
	if input.AllowedSiteModelIDs != nil {
		if err := confFile.Set("agent.allowed_site_model_ids", normalizeStringList(*input.AllowedSiteModelIDs)); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent model settings")
			return
		}
		scopeUpdated = true
	}
	if input.SitePolicy != nil {
		sitePolicy := *input.SitePolicy
		if sitePolicy != "allow_list" {
			sitePolicy = "allow_all"
		}
		if err := confFile.Set("agent.site_policy", sitePolicy); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent site policy")
			return
		}
		scopeUpdated = true
	}
	if input.ModelPolicy != nil {
		modelPolicy := *input.ModelPolicy
		if modelPolicy != "allow_list" {
			modelPolicy = "allow_all"
		}
		if err := confFile.Set("agent.model_policy", modelPolicy); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent model policy")
			return
		}
		scopeUpdated = true
	}
	if scopeUpdated {
		h.notifySettingsUpdated()
	}
	h.GetSettings(w, r, confFile)
}

// SetOnSettingsUpdated registers a hook invoked after settings persist, so the
// agent LLM entrypoint can resync the internal gateway key policy.
func (h *Handler) SetOnSettingsUpdated(fn func()) {
	h.mu.Lock()
	h.onSettingsUpdated = fn
	h.mu.Unlock()
}

func (h *Handler) notifySettingsUpdated() {
	h.mu.RLock()
	fn := h.onSettingsUpdated
	h.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// AccessPolicyFromConfig reads the agent site/model allowlist settings from
// the config file so they can be synced onto the internal gateway key.
func AccessPolicyFromConfig(confFile *config.ConfigFile) (sitePolicy, modelPolicy string, siteIDs, siteModelIDs []string) {
	sitePolicy, modelPolicy = "allow_all", "allow_all"
	siteIDs, siteModelIDs = []string{}, []string{}
	if confFile == nil {
		return sitePolicy, modelPolicy, siteIDs, siteModelIDs
	}
	siteIDs = readStringList(confFile, "agent.allowed_site_ids")
	siteModelIDs = readStringList(confFile, "agent.allowed_site_model_ids")
	if value, ok := confFile.Get("agent.site_policy"); ok {
		if policy, valid := value.(string); valid && policy == "allow_list" {
			sitePolicy = policy
		}
	}
	if value, ok := confFile.Get("agent.model_policy"); ok {
		if policy, valid := value.(string); valid && policy == "allow_list" {
			modelPolicy = policy
		}
	}
	return sitePolicy, modelPolicy, siteIDs, siteModelIDs
}

func readStringList(confFile *config.ConfigFile, key string) []string {
	value, ok := confFile.Get(key)
	if !ok {
		return []string{}
	}
	result := make([]string, 0)
	switch items := value.(type) {
	case []string:
		result = append(result, items...)
	case []any:
		for _, item := range items {
			if text, valid := item.(string); valid {
				result = append(result, text)
			}
		}
	default:
		return []string{}
	}
	return normalizeStringList(result)
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (h *Handler) SetBaseURL(baseURL string) {
	h.mu.Lock()
	h.baseURL = normalizeBaseURL(baseURL)
	h.mu.Unlock()
}

func (h *Handler) SetToken(token string) {
	h.mu.Lock()
	h.token = strings.TrimSpace(token)
	h.mu.Unlock()
}

func (h *Handler) snapshot() (string, string, *http.Client) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.baseURL, h.token, h.client
}

func (h *Handler) snapshotBaseURL() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.baseURL
}

func (h *Handler) snapshotToken() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.token
}

// RunnerTokenAuth authenticates runner-originated control calls
// (/internal/agent-llm/runs/*) against the configured runner internal token.
// The token is read from the handler snapshot on every request, so updates via
// the settings page take effect without a restart. A missing configuration
// fails closed: these endpoints stay unusable until a token is set.
func (h *Handler) RunnerTokenAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			expected := h.snapshotToken()
			if expected == "" {
				httpx.Error(w, r, http.StatusServiceUnavailable, "agent_unavailable", "agent runner internal token is not configured")
				return
			}
			provided := bearerToken(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
				httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "invalid agent runner token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func normalizeBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func validateBaseURL(value string) (string, error) {
	baseURL := normalizeBaseURL(value)
	if baseURL == "" {
		return "", &url.Error{Op: "parse", URL: value, Err: errEmptyRunnerURL}
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", &url.Error{Op: "parse", URL: value, Err: errInvalidRunnerURL}
	}
	return baseURL, nil
}

func copyRequestHeaders(dst, src http.Header) {
	for _, key := range []string{"Accept", "Content-Type", "Last-Event-ID", "User-Agent"} {
		if values := src.Values(key); len(values) > 0 {
			dst[key] = append([]string(nil), values...)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for _, key := range []string{"Content-Type", "Cache-Control", "Connection", "X-Accel-Buffering", "Last-Event-ID"} {
		if values := src.Values(key); len(values) > 0 {
			dst[key] = append([]string(nil), values...)
		}
	}
}
