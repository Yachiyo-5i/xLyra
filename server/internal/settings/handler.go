package settings

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/config"
	"xlyra/server/internal/downloads"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/ratelimit"
	"xlyra/server/internal/store"
)

type Handler struct {
	logger      *slog.Logger
	confFile    *config.ConfigFile
	rateLimits  *ratelimit.Service
	backups     backup.Service
	autoBackups *backup.AutomaticService
	downloads   *downloads.Service
	configMu    *sync.Mutex
	httpClients *httpclient.Manager
}

func NewHandler(logger *slog.Logger, confFile *config.ConfigFile, stores ...*store.Store) Handler {
	var db *store.Store
	if len(stores) > 0 {
		db = stores[0]
	}
	handler := Handler{
		logger:      logger,
		confFile:    confFile,
		configMu:    &sync.Mutex{},
		httpClients: httpclient.NewManager(confFile),
	}
	if db != nil {
		handler.rateLimits = ratelimit.NewService(db)
	}
	return handler
}

func NewHandlerWithBackup(logger *slog.Logger, confFile *config.ConfigFile, db *store.Store, masterKey string, downloadService *downloads.Service, playgroundRoot string, preRestore func(context.Context) error, postRestore func(context.Context) error, timeZones ...config.TimeZone) Handler {
	timeZone := config.TimeZoneOrDefault(timeZones...)
	handler := NewHandler(logger, confFile, db)
	handler.backups = backup.NewService(db, confFile, masterKey, playgroundRoot, timeZone).WithRestoreHooks(preRestore, postRestore)
	handler.autoBackups = backup.NewAutomaticService(handler.backups, masterKey)
	handler.downloads = downloadService
	return handler
}

type proxyConfig = httpclient.ProxyProfile
type systemProxyConfig = httpclient.NetworkConfig

const (
	systemProxyTestURL     = "https://www.apple.com/library/test/success.html"
	systemProxyTestTimeout = 8 * time.Second
	systemProxyDialTimeout = 3 * time.Second
)

func defaultSystemProxyConfig() systemProxyConfig {
	return systemProxyConfig{Proxies: []proxyConfig{}}
}

func (h Handler) GetSystemProxy(w http.ResponseWriter, r *http.Request) {
	if h.confFile == nil {
		httpx.JSON(w, http.StatusOK, defaultSystemProxyConfig())
		return
	}
	httpx.JSON(w, http.StatusOK, httpclient.ReadNetworkConfig(h.confFile))
}

func (h Handler) UpdateSystemProxy(w http.ResponseWriter, r *http.Request) {
	if h.confFile == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "config_unavailable", "config persistence is not available")
		return
	}
	var req systemProxyConfig
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	next := httpclient.NormalizeNetworkConfig(req)
	if err := httpclient.ValidateNetworkConfig(next); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_system_proxy_config", err.Error())
		return
	}
	if h.configMu != nil {
		h.configMu.Lock()
		defer h.configMu.Unlock()
	}
	if err := h.confFile.Merge(map[string]any{"network": next}); err != nil {
		h.logError("update system proxy config", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save config")
		return
	}

	h.logInfo("system proxy config updated", "proxy_count", len(next.Proxies))

	h.GetSystemProxy(w, r)
}

type systemProxyTestRequest struct {
	Proxy proxyConfig `json:"proxy"`
}

type systemProxyTestResponse struct {
	OK        bool   `json:"ok"`
	Stage     string `json:"stage"`
	LatencyMS int64  `json:"latency_ms"`
	Message   string `json:"message"`
}

func (h Handler) TestSystemProxy(w http.ResponseWriter, r *http.Request) {
	var req systemProxyTestRequest
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	result := testSystemProxy(r.Context(), httpclient.NormalizeNetworkConfig(httpclient.NetworkConfig{Proxies: []proxyConfig{req.Proxy}}).Proxies[0])
	httpx.JSON(w, http.StatusOK, result)
}

func testSystemProxy(ctx context.Context, item proxyConfig) systemProxyTestResponse {
	startedAt := time.Now()
	result := systemProxyTestResponse{Stage: "validate"}
	fail := func(stage string, message string) systemProxyTestResponse {
		result.OK = false
		result.Stage = stage
		result.LatencyMS = time.Since(startedAt).Milliseconds()
		result.Message = message
		return result
	}

	if err := httpclient.ValidateNetworkConfig(httpclient.NetworkConfig{Proxies: []proxyConfig{item}}); err != nil {
		return fail("validate", err.Error())
	}
	parsed, err := url.Parse(strings.TrimSpace(item.URL))
	if err != nil || parsed.Host == "" {
		return fail("validate", "proxy url is invalid")
	}
	conn, err := httpclient.NewGuardedDialer(systemProxyDialTimeout).DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return fail("tcp_connect", err.Error())
	}
	_ = conn.Close()

	client, err := systemProxyTestHTTPClient(item, parsed)
	if err != nil {
		return fail("proxy_client", err.Error())
	}
	requestCtx, cancel := context.WithTimeout(ctx, systemProxyTestTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(requestCtx, http.MethodGet, systemProxyTestURL, nil)
	if err != nil {
		return fail("proxy_request", err.Error())
	}
	httpReq.Header.Set("User-Agent", "xLyra proxy tester")
	resp, err := client.Do(httpReq)
	if err != nil {
		return fail("proxy_request", err.Error())
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return fail("proxy_request", fmt.Sprintf("probe returned HTTP %d", resp.StatusCode))
	}
	result.OK = true
	result.Stage = "proxy_request"
	result.LatencyMS = time.Since(startedAt).Milliseconds()
	result.Message = "proxy is reachable"
	return result
}

func systemProxyTestHTTPClient(item proxyConfig, parsed *url.URL) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = httpclient.NewGuardedDialer(systemProxyDialTimeout).DialContext
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
	case "socks5":
		dialer, err := proxy.SOCKS5("tcp", parsed.Host, systemProxyAuth(parsed), httpclient.NewGuardedDialer(systemProxyDialTimeout))
		if err != nil {
			return nil, fmt.Errorf("create socks5 proxy dialer: %w", err)
		}
		transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
			type contextDialer interface {
				DialContext(context.Context, string, string) (net.Conn, error)
			}
			if ctxDialer, ok := dialer.(contextDialer); ok {
				return ctxDialer.DialContext(ctx, network, address)
			}
			connCh := make(chan net.Conn, 1)
			errCh := make(chan error, 1)
			go func() {
				conn, err := dialer.Dial(network, address)
				if err != nil {
					errCh <- err
					return
				}
				connCh <- conn
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case err := <-errCh:
				return nil, err
			case conn := <-connCh:
				return conn, nil
			}
		}
	default:
		return nil, fmt.Errorf("unsupported proxy type %q", item.Type)
	}
	transport.ResponseHeaderTimeout = systemProxyTestTimeout
	transport.TLSHandshakeTimeout = 5 * time.Second
	return &http.Client{Timeout: systemProxyTestTimeout, Transport: transport}, nil
}

func systemProxyAuth(parsed *url.URL) *proxy.Auth {
	if parsed == nil || parsed.User == nil {
		return nil
	}
	password, _ := parsed.User.Password()
	return &proxy.Auth{
		User:     parsed.User.Username(),
		Password: password,
	}
}

type rateLimitConfig struct {
	Status   string `json:"status"`
	RPMLimit *int64 `json:"rpm_limit"`
	TPMLimit *int64 `json:"tpm_limit"`
}

type rateLimitConfigEnvelope struct {
	RateLimit rateLimitConfig `json:"rate_limit"`
}

func (h Handler) GetRateLimits(w http.ResponseWriter, r *http.Request) {
	config := ratelimit.DefaultConfig()
	if h.rateLimits != nil {
		if value, err := h.rateLimits.GetGlobal(r.Context()); err == nil {
			config = value
		}
	}
	httpx.JSON(w, http.StatusOK, rateLimitConfigEnvelope{RateLimit: rateLimitConfigPayload(config)})
}

func (h Handler) UpdateRateLimits(w http.ResponseWriter, r *http.Request) {
	if h.rateLimits == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "rate_limit_unavailable", "rate limit persistence is not available")
		return
	}
	var req rateLimitConfigEnvelope
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	updated, err := h.rateLimits.SetGlobal(r.Context(), ratelimit.ConfigInput{
		Status:   req.RateLimit.Status,
		RPMLimit: req.RateLimit.RPMLimit,
		TPMLimit: req.RateLimit.TPMLimit,
	})
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "rate_limit_update_failed", err.Error())
		return
	}
	h.logInfo("rate limit config updated", "status", updated.Status)
	httpx.JSON(w, http.StatusOK, rateLimitConfigEnvelope{RateLimit: rateLimitConfigPayload(updated)})
}

func rateLimitConfigPayload(config ratelimit.Config) rateLimitConfig {
	status := config.Status
	if status == "" {
		status = store.RateLimitStatusDisabled
	}
	return rateLimitConfig{
		Status:   status,
		RPMLimit: config.RPMLimit,
		TPMLimit: config.TPMLimit,
	}
}

func (h Handler) logInfo(message string, args ...any) {
	if h.logger != nil {
		h.logger.Info(message, args...)
	}
}

func (h Handler) logError(message string, args ...any) {
	if h.logger != nil {
		h.logger.Error(message, args...)
	}
}
