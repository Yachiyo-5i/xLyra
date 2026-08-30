package agentproxy

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/config"
)

func TestUpdateSettingsClearRunner(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := confFile.Set("agent.runner_base_url", "http://runner.example"); err != nil {
		t.Fatal(err)
	}
	if err := confFile.Set("agent.runner_internal_token", "secret"); err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://runner.example", "secret", http.DefaultClient, slog.Default())

	body, _ := json.Marshal(map[string]any{"clear_runner": true})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	h.UpdateSettings(resp, req, confFile)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var parsed struct {
		Data Settings `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data.RunnerBaseURL != "" || parsed.Data.RunnerTokenConfigured {
		t.Fatalf("settings = %#v", parsed.Data)
	}
	if _, ok := confFile.Get("agent.runner_base_url"); ok {
		t.Fatal("runner_base_url should be removed from config file")
	}
	if _, ok := confFile.Get("agent.runner_internal_token"); ok {
		t.Fatal("runner_internal_token should be removed from config file")
	}
	if got := h.snapshotBaseURL(); got != "" {
		t.Fatalf("runtime base URL = %q", got)
	}

	// Forwarding must fail closed after the clear.
	forwardReq := httptest.NewRequest(http.MethodGet, "/api/v1/agent/health", nil)
	forwardResp := httptest.NewRecorder()
	h.Forward(forwardResp, forwardReq)
	if forwardResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("forward status = %d", forwardResp.Code)
	}
}
