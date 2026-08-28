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

func TestUpdateSettingsPersistsPoliciesAndLists(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://old.example", "runner-secret", http.DefaultClient, slog.Default())
	payload := map[string]any{
		"runner_base_url":        "http://new.example:8787",
		"site_policy":            "allow_list",
		"model_policy":           "allow_list",
		"allowed_site_ids":       []string{"s1", "s2"},
		"allowed_site_model_ids": []string{"m1"},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	h.UpdateSettings(resp, req, confFile)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", resp.Code, resp.Body.String())
	}
	t.Logf("PUT response: %s", resp.Body.String())

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/agent/settings", nil)
	getResp := httptest.NewRecorder()
	h.GetSettings(getResp, getReq, confFile)
	t.Logf("GET response: %s", getResp.Body.String())

	var parsed struct {
		Data Settings `json:"data"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Data.SitePolicy != "allow_list" || parsed.Data.ModelPolicy != "allow_list" {
		t.Fatalf("policies = %q/%q", parsed.Data.SitePolicy, parsed.Data.ModelPolicy)
	}
	if len(parsed.Data.AllowedSiteIDs) != 2 || len(parsed.Data.AllowedSiteModelIDs) != 1 {
		t.Fatalf("lists = %#v / %#v", parsed.Data.AllowedSiteIDs, parsed.Data.AllowedSiteModelIDs)
	}

	// simulate restart: fresh handler reading same conf file
	h2 := NewHandler("", "", http.DefaultClient, slog.Default())
	getResp2 := httptest.NewRecorder()
	h2.GetSettings(getResp2, getReq, confFile)
	t.Logf("GET after restart: %s", getResp2.Body.String())
}
