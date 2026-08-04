package health

import (
	"context"
	"net/http"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/inflight"
)

type readinessChecker interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	cfg     config.Config
	checker readinessChecker
	started time.Time
}

func NewHandler(cfg config.Config, checker readinessChecker) Handler {
	return Handler{
		cfg:     cfg,
		checker: checker,
		started: time.Now(),
	}
}

func (h Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	if h.checker != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := h.checker.Ping(ctx); err != nil {
			httpx.Error(w, r, http.StatusServiceUnavailable, "database_unavailable", "database is not ready")
			return
		}
	}

	httpx.JSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"env":      h.cfg.AppEnv,
		"database": "ok",
	})
}

func (h Handler) Stats(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"uptime_seconds":     int(time.Since(h.started).Seconds()),
		"app_env":            h.cfg.AppEnv,
		"http_port":          h.cfg.HTTPPort,
		"in_flight_requests": inflight.Count(),
	})
}
