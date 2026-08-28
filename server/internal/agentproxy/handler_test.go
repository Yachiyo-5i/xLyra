package agentproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/config"
)

func TestForwardInjectsRunnerTokenAndPreservesSSEHeaders(t *testing.T) {
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer runner-secret" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Path; got != "/sessions/s1/events" {
			t.Fatalf("path = %q", got)
		}
		if got := r.Header.Get("Last-Event-ID"); got != "7" {
			t.Fatalf("last event id = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: ping\ndata: {}\n\n")
	}))
	defer runner.Close()

	h := NewHandler(runner.URL, "runner-secret", runner.Client(), slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/s1/events?x=1", nil)
	req.Header.Set("Authorization", "Bearer browser-token")
	req.Header.Set("Last-Event-ID", "7")
	resp := httptest.NewRecorder()
	h.Forward(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	if got := resp.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q", got)
	}
	if got := resp.Body.String(); got != "event: ping\ndata: {}\n\n" {
		t.Fatalf("body = %q", got)
	}
}

func TestUpdateSettingsPersistsAndAppliesRunnerURL(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://old.example", "runner-secret", http.DefaultClient, slog.Default())
	body, err := json.Marshal(map[string]string{"runner_base_url": "http://new.example:8787"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	h.UpdateSettings(resp, req, confFile)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	value, ok := confFile.Get("agent.runner_base_url")
	if !ok || value != "http://new.example:8787" {
		t.Fatalf("persisted runner URL = %#v", value)
	}
	if got := h.snapshotBaseURL(); got != "http://new.example:8787" {
		t.Fatalf("active runner URL = %q", got)
	}
}

func TestUpdateSettingsRejectsInvalidRunnerURL(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://old.example", "runner-secret", http.DefaultClient, slog.Default())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"runner_base_url":"runner.local"}`))
	resp := httptest.NewRecorder()
	h.UpdateSettings(resp, req, confFile)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.Code)
	}
}

func TestForwardFailsClosedWhenNotConfigured(t *testing.T) {
	h := NewHandler("", "", http.DefaultClient, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/health", nil)
	resp := httptest.NewRecorder()
	h.Forward(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.Code)
	}
}

func TestRunnerTokenAuth(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("fails closed when token is not configured", func(t *testing.T) {
		h := NewHandler("", "", http.DefaultClient, slog.Default())
		req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/runs/register", nil)
		req.Header.Set("Authorization", "Bearer whatever")
		resp := httptest.NewRecorder()
		h.RunnerTokenAuth()(okHandler).ServeHTTP(resp, req)
		if resp.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", resp.Code)
		}
	})

	t.Run("rejects missing and mismatched tokens", func(t *testing.T) {
		h := NewHandler("", "runner-secret", http.DefaultClient, slog.Default())
		for _, header := range []string{"", "Bearer wrong", "runner-secret"} {
			req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/runs/register", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			resp := httptest.NewRecorder()
			h.RunnerTokenAuth()(okHandler).ServeHTTP(resp, req)
			if resp.Code != http.StatusUnauthorized {
				t.Fatalf("header %q: status = %d", header, resp.Code)
			}
		}
	})

	t.Run("accepts the configured token and reflects settings updates", func(t *testing.T) {
		h := NewHandler("", "runner-secret", http.DefaultClient, slog.Default())
		req := httptest.NewRequest(http.MethodPost, "/internal/agent-llm/runs/end", nil)
		req.Header.Set("Authorization", "Bearer runner-secret")
		resp := httptest.NewRecorder()
		h.RunnerTokenAuth()(okHandler).ServeHTTP(resp, req)
		if resp.Code != http.StatusNoContent {
			t.Fatalf("status = %d", resp.Code)
		}

		h.SetToken("rotated-secret")
		resp = httptest.NewRecorder()
		h.RunnerTokenAuth()(okHandler).ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("old token after rotation: status = %d", resp.Code)
		}
		req.Header.Set("Authorization", "Bearer rotated-secret")
		resp = httptest.NewRecorder()
		h.RunnerTokenAuth()(okHandler).ServeHTTP(resp, req)
		if resp.Code != http.StatusNoContent {
			t.Fatalf("rotated token: status = %d", resp.Code)
		}
	})
}
