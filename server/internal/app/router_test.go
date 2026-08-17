package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	"xlyra/server/internal/gateway"
)

func TestHealthzIncludesRequestIDHeader(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("expected X-Request-ID header to be set")
	}
}

func TestCORSExposesGatewayRouteSiteHeader(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, gateway.RouteSiteHeader) {
		t.Fatalf("Access-Control-Expose-Headers = %q, want %s", got, gateway.RouteSiteHeader)
	}
}

func TestNotFoundUsesJSONErrorEnvelope(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}

	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}

	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.Error.Code != "not_found" {
		t.Fatalf("expected error code not_found, got %q", body.Error.Code)
	}

	if body.Error.RequestID == "" {
		t.Fatal("expected request_id in error response")
	}
}

func TestMethodNotAllowedUsesJSONErrorEnvelope(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestSystemVersionIsPublic(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/version", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Version == "" {
		t.Fatal("expected version in response")
	}
}

func TestDashboardEpaperSummaryRouteIsProtected(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/epaper-summary", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertAuthUnavailable(t, rec)
}

func TestAdminLogoutRouteIsProtected(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assertAuthUnavailable(t, rec)
}

func TestProtectedAdminFeatureRoutesAreRegistered(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "api keys", method: http.MethodGet, path: "/api/v1/api-keys"},
		{name: "api key site models", method: http.MethodGet, path: "/api/v1/api-keys/11111111-1111-1111-1111-111111111111/site-models"},
		{name: "request summary", method: http.MethodGet, path: "/api/v1/requests/summary"},
		{name: "sites", method: http.MethodGet, path: "/api/v1/sites"},
		{name: "site refresh", method: http.MethodPost, path: "/api/v1/sites/11111111-1111-1111-1111-111111111111/refresh"},
		{name: "oauth authorize", method: http.MethodPost, path: "/api/v1/oauth/providers/codex/authorize"},
		{name: "models", method: http.MethodGet, path: "/api/v1/models"},
		{name: "model prices", method: http.MethodGet, path: "/api/v1/model-prices"},
		{name: "routes", method: http.MethodGet, path: "/api/v1/routes"},
		{name: "route cooldowns", method: http.MethodGet, path: "/api/v1/routes/cooldowns"},
		{name: "settings general", method: http.MethodGet, path: "/api/v1/settings/general"},
		{name: "automatic backup files", method: http.MethodGet, path: "/api/v1/settings/backup/automatic/files"},
		{name: "dashboard health", method: http.MethodGet, path: "/api/v1/dashboard/health"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assertAuthUnavailable(t, rec)
		})
	}
}

func TestGatewayRoutesAreRegisteredAndProtected(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "chat completions", method: http.MethodPost, path: "/v1/chat/completions"},
		{name: "responses", method: http.MethodPost, path: "/v1/responses"},
		{name: "responses websocket", method: http.MethodGet, path: "/v1/responses"},
		{name: "messages", method: http.MethodPost, path: "/v1/messages"},
		{name: "images", method: http.MethodPost, path: "/v1/images/generations"},
		{name: "image edits", method: http.MethodPost, path: "/v1/images/edits"},
		{name: "embeddings", method: http.MethodPost, path: "/v1/embeddings"},
		{name: "models", method: http.MethodGet, path: "/v1/models"},
		{name: "user balance", method: http.MethodGet, path: "/v1/user/balance"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assertAuthUnavailable(t, rec)
		})
	}
}

func TestContentEncodingMiddlewareDoesNotReachPlaygroundOrResponsesWebSocket(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose chi routes")
	}
	middlewareCounts := map[string]int{}
	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if route == "/v1/responses" || route == "/api/playground/v1/responses" {
			middlewareCounts[method+" "+route] = len(middlewares)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if middlewareCounts["POST /v1/responses"] != middlewareCounts["GET /v1/responses"]+2 {
		t.Fatalf("main responses middleware count = %d, websocket count = %d", middlewareCounts["POST /v1/responses"], middlewareCounts["GET /v1/responses"])
	}
	if middlewareCounts["POST /api/playground/v1/responses"] != middlewareCounts["GET /v1/responses"]+1 {
		t.Fatalf("playground responses middleware count = %d, websocket count = %d", middlewareCounts["POST /api/playground/v1/responses"], middlewareCounts["GET /v1/responses"])
	}
	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "playground responses", method: http.MethodPost, path: "/api/playground/v1/responses"},
		{name: "responses websocket", method: http.MethodGet, path: "/v1/responses"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Content-Encoding", "gzip")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assertAuthUnavailable(t, rec)
		})
	}
}

func TestAnalyticsRoutesHaveResponseCompressionMiddleware(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose chi routes")
	}
	middlewareCounts := map[string]int{}
	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && (route == "/api/v1/analytics/dataset" || route == "/api/v1/traffic-flow/topology") {
			middlewareCounts[route] = len(middlewares)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	if middlewareCounts["/api/v1/analytics/dataset"] != middlewareCounts["/api/v1/traffic-flow/topology"]+1 {
		t.Fatalf("analytics middleware count = %d, regular protected route count = %d", middlewareCounts["/api/v1/analytics/dataset"], middlewareCounts["/api/v1/traffic-flow/topology"])
	}
}

func TestPlaygroundGatewayRoutesAreRegisteredAndProtected(t *testing.T) {
	t.Parallel()

	router := NewRouter(testConfig(), slog.Default(), nil, nil, "test-master-key")
	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/playground/v1/chat/completions"},
		{method: http.MethodPost, path: "/api/playground/v1/responses"},
		{method: http.MethodPost, path: "/api/playground/v1/messages"},
		{method: http.MethodPost, path: "/api/playground/v1/images/generations"},
		{method: http.MethodPost, path: "/api/playground/v1/images/edits"},
		{method: http.MethodGet, path: "/api/playground/v1/models"},
	} {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			assertAuthUnavailable(t, rec)
		})
	}
}

func assertAuthUnavailable(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "auth_unavailable" {
		t.Fatalf("expected auth_unavailable, got %q", body.Error.Code)
	}
}

func TestAuthMiddlewareSelectorsUseConfiguredAuthService(t *testing.T) {
	t.Parallel()

	authService := auth.NewService(nil, "test-master-key")
	for _, tc := range []struct {
		name       string
		middleware func(*auth.Service) func(http.Handler) http.Handler
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "admin session",
			middleware: requireAdmin,
			method:     http.MethodGet,
			path:       "/api/v1/profile",
			wantStatus: http.StatusUnauthorized,
			wantCode:   "unauthorized",
		},
		{
			name:       "api key",
			middleware: requireAPIKey,
			method:     http.MethodPost,
			path:       "/v1/chat/completions",
			wantStatus: http.StatusUnauthorized,
			wantCode:   "unauthorized",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := tc.middleware(authService)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, rec.Code)
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error.Code != tc.wantCode {
				t.Fatalf("expected error code %q, got %q", tc.wantCode, body.Error.Code)
			}
		})
	}
}

func TestRequireAdminCSRFSelectorUsesConfiguredAuthService(t *testing.T) {
	t.Parallel()

	authService := auth.NewService(nil, "test-master-key")
	handler := requireAdminCSRF(authService)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/profile/account", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestRouteAwareTimeoutExtendsBackupTransfers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: backupImportPath},
		{method: http.MethodPost, path: backupExportPath},
		{method: http.MethodPost, path: automaticBackupRunPath},
		{method: http.MethodPost, path: automaticBackupRestorePath},
		{method: http.MethodGet, path: downloadPathPrefix + "download-id"},
	} {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var remaining time.Duration
			handler := routeAwareTimeout(30 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				deadline, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("expected request context deadline")
				}
				remaining = time.Until(deadline)
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
			}
			if remaining < 9*time.Minute || remaining > backupTransferRequestTimeout {
				t.Fatalf("backup transfer timeout = %s, want close to %s", remaining, backupTransferRequestTimeout)
			}
		})
	}
}

func TestRouteAwareTimeoutKeepsDefaultForRegularRoutes(t *testing.T) {
	t.Parallel()

	defaultTimeout := 30 * time.Second
	var remaining time.Duration
	handler := routeAwareTimeout(defaultTimeout)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok {
			t.Fatal("expected request context deadline")
		}
		remaining = time.Until(deadline)
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/general", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if remaining < 20*time.Second || remaining > defaultTimeout {
		t.Fatalf("regular route timeout = %s, want close to %s", remaining, defaultTimeout)
	}
}

func TestRouteAwareTimeoutLeavesStreamingRoutesWithoutDeadline(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method  string
		path    string
		upgrade bool
	}{
		{method: http.MethodGet, path: "/api/v1/dashboard/resources/stream"},
		{method: http.MethodGet, path: "/api/v1/traffic-flow/stream"},
		{method: http.MethodPost, path: "/v1/chat/completions"},
		{method: http.MethodPost, path: "/v1/responses"},
		{method: http.MethodGet, path: "/v1/responses", upgrade: true},
		{method: http.MethodPost, path: "/v1/images/generations"},
		{method: http.MethodPost, path: "/v1/images/edits"},
		{method: http.MethodPost, path: "/v1/messages"},
		{method: http.MethodPost, path: "/api/playground/v1/chat/completions"},
		{method: http.MethodPost, path: "/api/playground/v1/responses"},
		{method: http.MethodPost, path: "/api/playground/v1/images/generations"},
		{method: http.MethodPost, path: "/api/playground/v1/images/edits"},
		{method: http.MethodPost, path: "/api/playground/v1/messages"},
	} {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			handler := routeAwareTimeout(30 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := r.Context().Deadline(); ok {
					t.Fatal("expected streaming request to keep original context without deadline")
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(tc.method, tc.path, nil).WithContext(context.Background())
			if tc.upgrade {
				req.Header.Set("Upgrade", "websocket")
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
			}
		})
	}
}

func testConfig() config.Config {
	return config.Config{
		AppEnv:             "test",
		AppName:            "xlyra-server",
		HTTPHost:           "127.0.0.1",
		HTTPPort:           5801,
		LogLevel:           "debug",
		ReadHeaderTimeout:  5 * time.Second,
		RequestTimeout:     30 * time.Second,
		ShutdownTimeout:    10 * time.Second,
		DBConnectTimeout:   30 * time.Second,
		DBMinConns:         2,
		DBMaxConns:         10,
		CORSAllowedOrigins: []string{"http://localhost:5173"},
		DBHost:             "postgres",
		DBPort:             5432,
		DBName:             "xlyra",
		DBUser:             "postgres",
		DBPassword:         "postgres",
		DBSSLMode:          "disable",
	}
}
