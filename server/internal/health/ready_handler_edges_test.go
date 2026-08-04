package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/config"
)

type readinessCheckFunc func(context.Context) error

func (f readinessCheckFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

func TestReadyzReportsReadyWhenCheckerPasses(t *testing.T) {
	t.Parallel()

	called := false
	sawDeadline := false
	handler := NewHandler(config.Config{AppEnv: "worker-test"}, readinessCheckFunc(func(ctx context.Context) error {
		called = true
		_, sawDeadline = ctx.Deadline()
		return nil
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	handler.Readyz(rec, req)

	if !called {
		t.Fatal("expected readiness checker to be called")
	}
	if !sawDeadline {
		t.Fatal("expected readiness checker context to have a timeout deadline")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Status   string `json:"status"`
		Env      string `json:"env"`
		Database string `json:"database"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ready" || body.Env != "worker-test" || body.Database != "ok" {
		t.Fatalf("unexpected readyz body: %#v", body)
	}
}

func TestReadyzReportsReadyWithoutChecker(t *testing.T) {
	t.Parallel()

	handler := NewHandler(config.Config{AppEnv: "worker-test"}, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)

	handler.Readyz(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
