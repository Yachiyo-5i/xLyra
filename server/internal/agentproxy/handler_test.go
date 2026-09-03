package agentproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/config"
)

func TestForwardRegistersRunFromSessionResponse(t *testing.T) {
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/s1/grant-access" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"data":{"session_id":"s1","run_id":"run-2","model":"gpt-5","agent_instance_id":"agent-1"}}`)
	}))
	defer runner.Close()

	h := NewHandler(runner.URL, "runner-secret", runner.Client(), slog.Default())
	registered := make(chan RunRegistration, 1)
	h.SetOnRunStarted(func(_ context.Context, run RunRegistration) error {
		registered <- run
		return nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/s1/grant-access", bytes.NewBufferString(`{"granted":true}`))
	resp := httptest.NewRecorder()
	h.Forward(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	select {
	case run := <-registered:
		if run.AgentInstanceID != "agent-1" || run.SessionID != "s1" || run.RunID != "run-2" || run.Model != "gpt-5" {
			t.Fatalf("registration = %#v", run)
		}
	default:
		t.Fatal("run was not registered")
	}
}

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

func TestForwardMapsVersionAndUpgradeToInternalPaths(t *testing.T) {
	paths := make(chan string, 2)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer runner.Close()

	h := NewHandler(runner.URL, "runner-secret", runner.Client(), slog.Default())
	for _, tc := range []struct {
		method   string
		in       string
		expected string
	}{
		{method: http.MethodGet, in: "/api/v1/agent/version?refresh=true", expected: "/internal/agent/version"},
		{method: http.MethodPost, in: "/api/v1/agent/upgrade", expected: "/internal/agent/upgrade"},
		{method: http.MethodGet, in: "/api/v1/agent/skills", expected: "/internal/agent/skills"},
		{method: http.MethodGet, in: "/api/v1/agent/skills/demo-skill", expected: "/internal/agent/skills/demo-skill"},
		{method: http.MethodGet, in: "/api/v1/agent/skills/demo-skill/file?path=scripts/run.sh", expected: "/internal/agent/skills/demo-skill/file"},
		{method: http.MethodGet, in: "/api/v1/agent/workspace/file?path=AGENTS.md", expected: "/internal/agent/workspace/file"},
		{method: http.MethodPut, in: "/api/v1/agent/workspace/file", expected: "/internal/agent/workspace/file"},
		{method: http.MethodDelete, in: "/api/v1/agent/workspace/file?path=AGENTS.md", expected: "/internal/agent/workspace/file"},
	} {
		req := httptest.NewRequest(tc.method, tc.in, nil)
		resp := httptest.NewRecorder()
		h.Forward(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("%s %s: status = %d", tc.method, tc.in, resp.Code)
		}
		if got := <-paths; got != tc.expected {
			t.Fatalf("%s %s: runner path = %q, want %q", tc.method, tc.in, got, tc.expected)
		}
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

func TestUpdateSettingsPartialScopeKeepsRunnerConfig(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://old.example", "runner-secret", http.DefaultClient, slog.Default())

	// Save the runner connection first, then submit scope only: runner config must stay unchanged.
	runnerReq := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"runner_base_url":"http://new.example:8787"}`))
	runnerResp := httptest.NewRecorder()
	h.UpdateSettings(runnerResp, runnerReq, confFile)
	if runnerResp.Code != http.StatusOK {
		t.Fatalf("runner save status = %d", runnerResp.Code)
	}

	scopeReq := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"site_policy":"allow_list","allowed_site_ids":["s1"]}`))
	scopeResp := httptest.NewRecorder()
	h.UpdateSettings(scopeResp, scopeReq, confFile)
	if scopeResp.Code != http.StatusOK {
		t.Fatalf("scope save status = %d", scopeResp.Code)
	}

	if value, _ := confFile.Get("agent.runner_base_url"); value != "http://new.example:8787" {
		t.Fatalf("runner URL overwritten by scope save: %#v", value)
	}
	if got := h.snapshotBaseURL(); got != "http://new.example:8787" {
		t.Fatalf("active runner URL = %q", got)
	}
	if value, _ := confFile.Get("agent.site_policy"); value != "allow_list" {
		t.Fatalf("site policy = %#v", value)
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
