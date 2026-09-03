package agentproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

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

func TestUpdateSettingsPersistsAppearance(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://runner.example", "runner-secret", http.DefaultClient, slog.Default())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"appearance":{"background_image":"data:image/png;base64,AA==","custom_background_images":["data:image/png;base64,AA==","data:image/png;base64,AQ=="],"side_transparency":41,"side_brightness":55,"side_thickness":24,"backdrop_blur":18,"backdrop_dim":62}}`))
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
	if parsed.Data.Appearance.SideTransparency != 41 || len(parsed.Data.Appearance.CustomBackgroundImages) != 2 || !strings.HasPrefix(parsed.Data.Appearance.BackgroundImage, backgroundURLPrefix) {
		t.Fatalf("appearance = %#v", parsed.Data.Appearance)
	}
	if value, ok := confFile.Get("agent.appearance"); !ok || strings.Contains(fmt.Sprint(value), "data:image/") {
		t.Fatalf("appearance config should only contain file paths: %#v", value)
	}
	for _, image := range parsed.Data.Appearance.CustomBackgroundImages {
		if _, err := os.Stat(filepath.Join(h.backgroundDir(confFile), filepath.Base(strings.TrimPrefix(image, backgroundURLPrefix)))); err != nil {
			t.Fatalf("background source file missing for %q: %v", image, err)
		}
	}
	secondReq := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"appearance":{"background_image":"/agent-backdrop.png","custom_background_images":["data:image/png;base64,AA==","data:image/png;base64,AQ=="],"side_transparency":41,"side_brightness":55,"side_thickness":24,"backdrop_blur":18,"backdrop_dim":62}}`))
	secondResp := httptest.NewRecorder()
	h.UpdateSettings(secondResp, secondReq, confFile)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("second status = %d, body = %s", secondResp.Code, secondResp.Body.String())
	}
	var secondParsed struct {
		Data Settings `json:"data"`
	}
	if err := json.Unmarshal(secondResp.Body.Bytes(), &secondParsed); err != nil {
		t.Fatal(err)
	}
	if secondParsed.Data.Appearance.BackgroundImage != defaultBackgroundImage || len(secondParsed.Data.Appearance.CustomBackgroundImages) != 2 {
		t.Fatalf("custom image lost after switching default: %#v", secondParsed.Data.Appearance)
	}
}

func TestServeBackgroundUsesConfigDerivedDirectoryWithoutWorkdir(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://runner.example", "runner-secret", http.DefaultClient, slog.Default())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"appearance":{"background_image":"data:image/png;base64,AA==","custom_background_images":["data:image/png;base64,AA=="]}}`))
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
	name := filepath.Base(strings.TrimPrefix(parsed.Data.Appearance.BackgroundImage, backgroundURLPrefix))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("name", name)
	backgroundReq := httptest.NewRequest(http.MethodGet, "/api/v1/agent/backgrounds/"+name, nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, routeContext))
	backgroundResp := httptest.NewRecorder()
	h.ServeBackground(backgroundResp, backgroundReq)
	if backgroundResp.Code != http.StatusOK {
		t.Fatalf("serve status = %d, body = %s", backgroundResp.Code, backgroundResp.Body.String())
	}
}

func TestUpdateSettingsRejectsInvalidAppearance(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://runner.example", "runner-secret", http.DefaultClient, slog.Default())
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(`{"appearance":{"side_transparency":101}}`))
	resp := httptest.NewRecorder()
	h.UpdateSettings(resp, req, confFile)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func TestUpdateSettingsRejectsOversizedDataURLBeforeDecoding(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("http://runner.example", "runner-secret", http.DefaultClient, slog.Default())
	encoded := strings.Repeat("A", ((maxBackgroundImageBytes/3)+1)*4)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agent/settings", bytes.NewBufferString(fmt.Sprintf(`{"appearance":{"background_image":"data:image/png;base64,%s"}}`, encoded)))
	resp := httptest.NewRecorder()
	h.UpdateSettings(resp, req, confFile)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
}
