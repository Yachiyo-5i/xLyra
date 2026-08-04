package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/config"
)

type fakeReadinessChecker struct {
	err error
}

func (f fakeReadinessChecker) Ping(context.Context) error {
	return f.err
}

func TestHealthzReturnsOK(t *testing.T) {
	t.Parallel()

	handler := NewHandler(config.Config{AppEnv: "test", HTTPPort: 5801}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	handler.Healthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status body = %#v", body)
	}
}

func TestReadyzReportsDatabaseUnavailable(t *testing.T) {
	t.Parallel()

	handler := NewHandler(config.Config{AppEnv: "test"}, fakeReadinessChecker{err: errors.New("down")})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	handler.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "database_unavailable" {
		t.Fatalf("error code = %q, want database_unavailable", body.Error.Code)
	}
}

func TestStatsIncludesRuntimeFields(t *testing.T) {
	t.Parallel()

	handler := NewHandler(config.Config{AppEnv: "test", HTTPPort: 5801}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/debug/stats", nil)

	handler.Stats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		UptimeSeconds int    `json:"uptime_seconds"`
		AppEnv        string `json:"app_env"`
		HTTPPort      int    `json:"http_port"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.AppEnv != "test" || body.HTTPPort != 5801 {
		t.Fatalf("unexpected stats body: %#v", body)
	}
	if body.UptimeSeconds < 0 {
		t.Fatalf("uptime should not be negative: %#v", body)
	}
}
