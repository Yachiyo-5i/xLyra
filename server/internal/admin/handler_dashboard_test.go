package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/systemstats"
)

func TestDashboardHandlersReturnUnavailableWithoutDashboardService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "usage", call: handler.DashboardUsage},
		{name: "cooldowns", call: handler.DashboardCooldowns},
		{name: "health", call: handler.DashboardHealth},
		{name: "insights", call: handler.DashboardInsights},
		{name: "epaper_summary", call: handler.DashboardEpaperSummary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(http.MethodGet, "/api/v1/dashboard/"+tc.name, ""))

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "dashboard_service_unavailable")
		})
	}
}

func TestDashboardResourceStreamReturnsUnavailableWithoutSystemService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.DashboardResourceStream, adminTestRequest(http.MethodGet, "/api/v1/dashboard/resources/stream", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "system_stats_unavailable")
}

func TestDashboardResourceStreamRejectsWritersWithoutFlusher(t *testing.T) {
	t.Parallel()

	handler := Handler{system: systemstats.NewService()}
	req := adminTestRequest(http.MethodGet, "/api/v1/dashboard/resources/stream", "")
	w := newNonFlushingResponseWriter()

	handler.DashboardResourceStream(w, req)

	if w.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", w.status, http.StatusInternalServerError, w.body.String())
	}
	assertDashboardErrorCode(t, w.status, w.body.String(), http.StatusInternalServerError, "stream_unsupported")
}

func TestDashboardWriteServerSentEventFormatsSSE(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	payload := struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}{
		OK:    true,
		Count: 2,
	}

	if err := writeServerSentEvent(rec, "resource", payload); err != nil {
		t.Fatalf("write server-sent event: %v", err)
	}

	want := "event: resource\n" +
		"data: {\"ok\":true,\"count\":2}\n\n"
	if rec.Body.String() != want {
		t.Fatalf("SSE output = %q, want %q", rec.Body.String(), want)
	}
}

func TestDashboardWriteServerSentEventOmitsBlankEventName(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if err := writeServerSentEvent(rec, "", map[string]bool{"ok": true}); err != nil {
		t.Fatalf("write server-sent event without event name: %v", err)
	}

	if got, want := rec.Body.String(), "data: {\"ok\":true}\n\n"; got != want {
		t.Fatalf("SSE output = %q, want %q", got, want)
	}
}

func TestDashboardWriteServerSentEventReturnsMarshalError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	if err := writeServerSentEvent(rec, "resource", math.Inf(1)); err == nil {
		t.Fatal("expected json marshal error for non-finite float")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("SSE body after marshal error = %q, want empty", rec.Body.String())
	}
}

func TestDashboardWriteServerSentEventReturnsWriteError(t *testing.T) {
	t.Parallel()

	w := failingResponseWriter{header: make(http.Header)}

	if err := writeServerSentEvent(w, "resource", map[string]bool{"ok": true}); err == nil {
		t.Fatal("expected write error")
	}
}

type nonFlushingResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newNonFlushingResponseWriter() *nonFlushingResponseWriter {
	return &nonFlushingResponseWriter{header: make(http.Header)}
}

func (w *nonFlushingResponseWriter) Header() http.Header {
	return w.header
}

func (w *nonFlushingResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(payload)
}

func (w *nonFlushingResponseWriter) WriteHeader(status int) {
	w.status = status
}

type failingResponseWriter struct {
	header http.Header
}

func (w failingResponseWriter) Header() http.Header {
	return w.header
}

func (w failingResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w failingResponseWriter) WriteHeader(int) {}

func assertDashboardErrorCode(t *testing.T, status int, body string, wantStatus int, wantCode string) {
	t.Helper()

	if status != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", status, wantStatus, body)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q", payload.Error.Code, wantCode)
	}
}
