package agentproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
)

var (
	errEmptyRunnerURL   = errors.New("runner URL must not be empty")
	errInvalidRunnerURL = errors.New("runner URL must be an HTTP or HTTPS URL without credentials")
)

type Handler struct {
	mu                    sync.RWMutex
	baseURL               string
	token                 string
	client                *http.Client
	logger                *slog.Logger
	workdir               string
	backgroundDirFallback string

	onSettingsUpdated func()
	onRunStarted      func(context.Context, RunRegistration) error
}

type RunRegistration struct {
	AgentInstanceID string `json:"agent_instance_id"`
	SessionID       string `json:"session_id"`
	RunID           string `json:"run_id"`
	Model           string `json:"model"`
}

type Settings struct {
	RunnerBaseURL         string             `json:"runner_base_url"`
	RunnerTokenConfigured bool               `json:"runner_token_configured"`
	AllowedSiteIDs        []string           `json:"allowed_site_ids"`
	AllowedSiteModelIDs   []string           `json:"allowed_site_model_ids"`
	SitePolicy            string             `json:"site_policy"`
	ModelPolicy           string             `json:"model_policy"`
	Appearance            AppearanceSettings `json:"appearance"`
}

type AppearanceSettings struct {
	BackgroundImage        string   `json:"background_image"`
	CustomBackgroundImages []string `json:"custom_background_images"`
	SideTransparency       int      `json:"side_transparency"`
	SideBrightness         int      `json:"side_brightness"`
	SideThickness          int      `json:"side_thickness"`
	BackdropBlur           int      `json:"backdrop_blur"`
	BackdropDim            int      `json:"backdrop_dim"`
}

const (
	defaultBackgroundImage  = "/agent-backdrop.png"
	defaultSideTransparency = 49
	defaultSideBrightness   = 32
	defaultSideThickness    = 28
	defaultBackdropBlur     = 13
	defaultBackdropDim      = 69
	maxBackgroundImageBytes = 8 * 1024 * 1024
)

func defaultAppearanceSettings() AppearanceSettings {
	return AppearanceSettings{
		BackgroundImage:        defaultBackgroundImage,
		CustomBackgroundImages: []string{},
		SideTransparency:       defaultSideTransparency,
		SideBrightness:         defaultSideBrightness,
		SideThickness:          defaultSideThickness,
		BackdropBlur:           defaultBackdropBlur,
		BackdropDim:            defaultBackdropDim,
	}
}

// settingsInput uses partial-update semantics: absent fields are left unchanged,
// so runner connection and site/model scope can be saved independently.
type settingsInput struct {
	RunnerBaseURL       *string             `json:"runner_base_url"`
	RunnerToken         *string             `json:"runner_token"`
	ClearRunner         bool                `json:"clear_runner"`
	AllowedSiteIDs      *[]string           `json:"allowed_site_ids"`
	AllowedSiteModelIDs *[]string           `json:"allowed_site_model_ids"`
	SitePolicy          *string             `json:"site_policy"`
	ModelPolicy         *string             `json:"model_policy"`
	Appearance          *AppearanceSettings `json:"appearance"`
}

func NewHandler(baseURL, token string, client *http.Client, logger *slog.Logger, workdirs ...string) *Handler {
	workdir := ""
	if len(workdirs) > 0 {
		workdir = strings.TrimSpace(workdirs[0])
	}
	return &Handler{baseURL: normalizeBaseURL(baseURL), token: strings.TrimSpace(token), client: client, logger: logger, workdir: workdir}
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
	registerRun := r.Method == http.MethodPost && isRunStartPath(path)
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
	if registerRun && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices && !strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		body, readErr := io.ReadAll(response.Body)
		if readErr == nil {
			_ = response.Body.Close()
			h.notifyRunStarted(r.Context(), body)
			response.Body = io.NopCloser(bytes.NewReader(body))
		}
	}
	w.WriteHeader(response.StatusCode)
	if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
		// SSE must flush per write; io.Copy's buffer batches small events and
		// reads as stutter on the client.
		flushCopy(w, response.Body)
		return
	}
	_, _ = io.Copy(w, response.Body)
}

func isRunStartPath(path string) bool {
	return path == "/sessions" || strings.HasSuffix(path, "/retry") || strings.HasSuffix(path, "/grant-access")
}

func (h *Handler) notifyRunStarted(ctx context.Context, body []byte) {
	var payload struct {
		Data RunRegistration `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}
	if payload.Data.AgentInstanceID == "" || payload.Data.SessionID == "" || payload.Data.RunID == "" || payload.Data.Model == "" {
		return
	}
	h.mu.RLock()
	hook := h.onRunStarted
	h.mu.RUnlock()
	if hook == nil {
		return
	}
	if err := hook(ctx, payload.Data); err != nil && h.logger != nil {
		h.logger.Warn("failed to register Agent run", "session_id", payload.Data.SessionID, "run_id", payload.Data.RunID, "error", err)
	}
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
	if confFile != nil {
		h.backgroundDir(confFile)
	}
	settings := Settings{RunnerBaseURL: h.snapshotBaseURL(), RunnerTokenConfigured: h.snapshotToken() != "", AllowedSiteIDs: []string{}, AllowedSiteModelIDs: []string{}, SitePolicy: "allow_all", ModelPolicy: "allow_all", Appearance: defaultAppearanceSettings()}
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
		settings.Appearance = readAppearance(confFile)
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
	if input.Appearance != nil {
		appearance, err := normalizeAppearance(*input.Appearance)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_appearance", err.Error())
			return
		}
		appearance, err = h.persistAppearanceImages(appearance, confFile)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_appearance", err.Error())
			return
		}
		if err := confFile.Set("agent.appearance", map[string]any{
			"background_image":         appearance.BackgroundImage,
			"custom_background_images": appearance.CustomBackgroundImages,
			"side_transparency":        appearance.SideTransparency,
			"side_brightness":          appearance.SideBrightness,
			"side_thickness":           appearance.SideThickness,
			"backdrop_blur":            appearance.BackdropBlur,
			"backdrop_dim":             appearance.BackdropDim,
		}); err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save Agent appearance settings")
			return
		}
		_ = cleanupBackgroundFiles(h.backgroundDir(confFile), appearance)
	}
	if scopeUpdated {
		h.notifySettingsUpdated()
	}
	h.GetSettings(w, r, confFile)
}

func readAppearance(confFile *config.ConfigFile) AppearanceSettings {
	appearance := defaultAppearanceSettings()
	value, ok := confFile.Get("agent.appearance")
	if !ok {
		return appearance
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return appearance
	}
	stored := defaultAppearanceSettings()
	if err := json.Unmarshal(encoded, &stored); err != nil {
		return appearance
	}
	if normalized, err := normalizeAppearance(stored); err == nil {
		return normalized
	}
	return appearance
}

func normalizeAppearance(value AppearanceSettings) (AppearanceSettings, error) {
	if strings.TrimSpace(value.BackgroundImage) == "" {
		value.BackgroundImage = defaultBackgroundImage
	}
	if isDataURLTooLarge(value.BackgroundImage) || len(value.BackgroundImage) > maxBackgroundImageBytes && !strings.HasPrefix(value.BackgroundImage, "data:image/") {
		return AppearanceSettings{}, errors.New("background image is too large")
	}
	if len(value.CustomBackgroundImages) > 20 {
		return AppearanceSettings{}, errors.New("too many custom background images")
	}
	for _, image := range value.CustomBackgroundImages {
		if isDataURLTooLarge(image) || len(image) > maxBackgroundImageBytes && !strings.HasPrefix(image, "data:image/") {
			return AppearanceSettings{}, errors.New("custom background image is too large")
		}
		if image != "" && !strings.HasPrefix(image, "data:image/") && !isBackgroundPath(image) {
			return AppearanceSettings{}, errors.New("custom background image must be a stored background path or image data URL")
		}
	}
	if value.BackgroundImage != defaultBackgroundImage && !strings.HasPrefix(value.BackgroundImage, "data:image/") && !isBackgroundPath(value.BackgroundImage) {
		return AppearanceSettings{}, errors.New("background image must be the default image or a stored background path")
	}
	for _, item := range []struct {
		name  string
		value int
	}{
		{"side_transparency", value.SideTransparency},
		{"side_brightness", value.SideBrightness},
		{"side_thickness", value.SideThickness},
		{"backdrop_blur", value.BackdropBlur},
		{"backdrop_dim", value.BackdropDim},
	} {
		if item.value < 0 || item.value > 100 {
			return AppearanceSettings{}, errors.New(item.name + " must be between 0 and 100")
		}
	}
	return value, nil
}

func isDataURLTooLarge(value string) bool {
	if !strings.HasPrefix(value, "data:image/") {
		return false
	}
	_, encoded, ok := strings.Cut(value, ",")
	if !ok {
		return false
	}
	padding := 0
	if strings.HasSuffix(encoded, "=") {
		padding++
		if strings.HasSuffix(encoded, "==") {
			padding++
		}
	}
	return len(encoded)/4*3-padding > maxBackgroundImageBytes
}

const backgroundURLPrefix = "/api/v1/agent/backgrounds/"

func isBackgroundPath(value string) bool {
	name := strings.TrimPrefix(value, backgroundURLPrefix)
	return strings.HasPrefix(value, backgroundURLPrefix) && name != "" && filepath.Base(name) == name && strings.HasPrefix(name, "agent-bg-")
}

func (h *Handler) backgroundDir(confFile *config.ConfigFile) string {
	if h.workdir != "" {
		return filepath.Join(h.workdir, "assets", "agent-backgrounds")
	}
	if confFile != nil {
		dir := filepath.Join(filepath.Dir(filepath.Dir(confFile.Path())), "assets", "agent-backgrounds")
		h.mu.Lock()
		h.backgroundDirFallback = dir
		h.mu.Unlock()
		return dir
	}
	h.mu.RLock()
	dir := h.backgroundDirFallback
	h.mu.RUnlock()
	return dir
}

func decodeBackgroundDataURL(value string) ([]byte, string, error) {
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(header, "data:image/") || !strings.Contains(header, ";base64") {
		return nil, "", errors.New("custom background image must be a base64 image data URL")
	}
	mimeType := strings.TrimPrefix(strings.SplitN(header, ";", 2)[0], "data:")
	extension := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
		"image/gif":  ".gif",
		"image/avif": ".avif",
	}[mimeType]
	if extension == "" {
		return nil, "", errors.New("unsupported custom background image type")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil, "", errors.New("custom background image data is invalid")
	}
	if len(data) > maxBackgroundImageBytes {
		return nil, "", errors.New("custom background image is too large")
	}
	return data, extension, nil
}

func (h *Handler) persistAppearanceImages(value AppearanceSettings, confFile *config.ConfigFile) (AppearanceSettings, error) {
	dir := h.backgroundDir(confFile)
	if dir == "" {
		return AppearanceSettings{}, errors.New("background image storage is unavailable")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return AppearanceSettings{}, errors.New("failed to create background image storage")
	}
	if strings.HasPrefix(value.BackgroundImage, "data:image/") {
		found := false
		for _, image := range value.CustomBackgroundImages {
			if image == value.BackgroundImage {
				found = true
				break
			}
		}
		if !found {
			if len(value.CustomBackgroundImages) >= 20 {
				return AppearanceSettings{}, errors.New("too many custom background images")
			}
			value.CustomBackgroundImages = append(value.CustomBackgroundImages, value.BackgroundImage)
		}
	}
	stored := make(map[string]string, len(value.CustomBackgroundImages))
	for _, image := range value.CustomBackgroundImages {
		if !strings.HasPrefix(image, "data:image/") {
			continue
		}
		data, extension, err := decodeBackgroundDataURL(image)
		if err != nil {
			return AppearanceSettings{}, err
		}
		digest := sha256.Sum256(data)
		name := "agent-bg-" + hex.EncodeToString(digest[:8]) + extension
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return AppearanceSettings{}, errors.New("failed to save background image")
		}
		stored[image] = backgroundURLPrefix + name
	}
	for index, image := range value.CustomBackgroundImages {
		if replacement, ok := stored[image]; ok {
			value.CustomBackgroundImages[index] = replacement
		}
	}
	if replacement, ok := stored[value.BackgroundImage]; ok {
		value.BackgroundImage = replacement
	}
	return value, nil
}

func cleanupBackgroundFiles(dir string, appearance AppearanceSettings) error {
	if dir == "" {
		return nil
	}
	keep := make(map[string]struct{}, len(appearance.CustomBackgroundImages)+1)
	for _, image := range appearance.CustomBackgroundImages {
		if isBackgroundPath(image) {
			keep[filepath.Base(strings.TrimPrefix(image, backgroundURLPrefix))] = struct{}{}
		}
	}
	if isBackgroundPath(appearance.BackgroundImage) {
		keep[filepath.Base(strings.TrimPrefix(appearance.BackgroundImage, backgroundURLPrefix))] = struct{}{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "agent-bg-") {
			continue
		}
		if _, ok := keep[entry.Name()]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (h *Handler) ServeBackground(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if filepath.Base(name) != name || !strings.HasPrefix(name, "agent-bg-") {
		http.NotFound(w, r)
		return
	}
	dir := h.backgroundDir(nil)
	if dir == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(dir, name)
	http.ServeFile(w, r, path)
}

// SetOnSettingsUpdated registers a hook invoked after settings persist, so the
// agent LLM entrypoint can resync the internal gateway key policy.
func (h *Handler) SetOnSettingsUpdated(fn func()) {
	h.mu.Lock()
	h.onSettingsUpdated = fn
	h.mu.Unlock()
}

func (h *Handler) SetOnRunStarted(fn func(context.Context, RunRegistration) error) {
	h.mu.Lock()
	h.onRunStarted = fn
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
