package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const dashboardResourceStreamInterval = 2 * time.Second

func (h Handler) DashboardUsage(w http.ResponseWriter, r *http.Request) {
	if h.dashboard == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "dashboard_service_unavailable", "dashboard service is not available")
		return
	}

	payload, err := h.dashboard.Usage(r.Context(), time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "dashboard_usage_failed", "failed to load dashboard usage")
		return
	}

	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) DashboardCooldowns(w http.ResponseWriter, r *http.Request) {
	if h.dashboard == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "dashboard_service_unavailable", "dashboard service is not available")
		return
	}

	payload, err := h.dashboard.Cooldowns(r.Context(), time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "dashboard_cooldowns_failed", "failed to load dashboard cooldowns")
		return
	}

	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) DashboardHealth(w http.ResponseWriter, r *http.Request) {
	if h.dashboard == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "dashboard_service_unavailable", "dashboard service is not available")
		return
	}

	payload, err := h.dashboard.Health(r.Context(), time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "dashboard_health_failed", "failed to load dashboard health")
		return
	}

	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) DashboardInsights(w http.ResponseWriter, r *http.Request) {
	if h.dashboard == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "dashboard_service_unavailable", "dashboard service is not available")
		return
	}

	payload, err := h.dashboard.Insights(r.Context(), time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "dashboard_insights_failed", "failed to load dashboard insights")
		return
	}

	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) DashboardEpaperSummary(w http.ResponseWriter, r *http.Request) {
	if h.dashboard == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "dashboard_service_unavailable", "dashboard service is not available")
		return
	}

	payload, err := h.dashboard.EpaperSummary(r.Context(), time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "dashboard_epaper_summary_failed", "failed to load dashboard epaper summary")
		return
	}

	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) DashboardResourceStream(w http.ResponseWriter, r *http.Request) {
	if h.system == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "system_stats_unavailable", "system resource service is not available")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, r, http.StatusInternalServerError, "stream_unsupported", "streaming is not supported")
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")

	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	flusher.Flush()

	writeSnapshot := func() bool {
		snapshot := h.system.Sample(r.Context(), time.Now())
		if err := writeServerSentEvent(w, "resource", snapshot); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !writeSnapshot() {
		return
	}

	ticker := time.NewTicker(dashboardResourceStreamInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !writeSnapshot() {
				return
			}
		}
	}
}

func writeServerSentEvent(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
