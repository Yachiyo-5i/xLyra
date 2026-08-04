package adapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestAntigravityValidateSystemCredentialsAndListModelsWithAuthUseQuota(t *testing.T) {
	var mu sync.Mutex
	var requests []antigravityQuotaRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:fetchAvailableModels" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, antigravityQuotaRequest{
			Method:        r.Method,
			Authorization: r.Header.Get("Authorization"),
			UserAgent:     r.Header.Get("User-Agent"),
			Project:       stringFromAny(body["project"]),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-3.1-pro": {
					"displayName": "Gemini 3.1 Pro",
					"supportsThinking": true,
					"quotaInfo": {"remainingFraction": 0.66}
				}
			}
		}`))
	}))
	defer server.Close()

	site := SiteConfig{BaseURL: server.URL + "/", Client: server.Client()}
	auth := SystemAuth{
		AccessToken: " token-a ",
		Metadata:    map[string]any{"project_id": " project-main "},
	}
	antigravity := NewAntigravity()

	if err := antigravity.ValidateSystemCredentials(context.Background(), site, auth); err != nil {
		t.Fatalf("ValidateSystemCredentials returned error: %v", err)
	}
	models, err := antigravity.ListModelsWithAuth(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("ListModelsWithAuth returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1", len(models))
	}
	model := models[0]
	if model.UpstreamName != "gemini-3.1-pro" || model.DisplayName != "Gemini 3.1 Pro" {
		t.Fatalf("unexpected model identity: %#v", model)
	}
	if model.Capabilities["available"] != true || model.Capabilities["supports_thinking"] != true {
		t.Fatalf("unexpected model capabilities: %#v", model.Capabilities)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("quota requests = %d, want 2: %#v", len(requests), requests)
	}
	for _, request := range requests {
		if request.Method != http.MethodPost {
			t.Fatalf("quota method = %s, want POST", request.Method)
		}
		if request.Authorization != "Bearer token-a" || request.UserAgent != antigravityUserAgent {
			t.Fatalf("unexpected quota headers: %#v", request)
		}
		if request.Project != "project-main" {
			t.Fatalf("quota project = %q, want project-main", request.Project)
		}
	}
}

func TestAntigravitySummaryBalanceAndMetadataFetchQuotaSnapshots(t *testing.T) {
	var mu sync.Mutex
	var projects []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:fetchAvailableModels" {
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		projects = append(projects, stringFromAny(body["project"]))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-3.1-pro": {
					"displayName": "Gemini 3.1 Pro",
					"quotaInfo": {"remainingFraction": 0.10}
				},
				"claude-sonnet-4": {
					"displayName": "Claude Sonnet 4",
					"quotaInfo": {"remainingFraction": 0}
				}
			}
		}`))
	}))
	defer server.Close()

	site := SiteConfig{
		BaseURL: server.URL,
		Client:  server.Client(),
		Meta:    map[string]any{"oauth_project_id": "summary-project"},
	}
	auth := SystemAuth{
		AccessToken: "token-b",
		Metadata:    map[string]any{"project_id": "auth-project"},
	}
	antigravity := NewAntigravity()

	summary, err := antigravity.SummarizeAPIKey(context.Background(), site, "summary-token")
	if err != nil {
		t.Fatalf("SummarizeAPIKey returned error: %v", err)
	}
	if len(summary.Models) != 2 {
		t.Fatalf("summary models length = %d, want 2", len(summary.Models))
	}
	usage, ok := summary.Usage.(map[string]any)
	if !ok || usage["type"] != "per_model" || usage["is_forbidden"] != false {
		t.Fatalf("summary usage = %#v", summary.Usage)
	}
	raw, ok := summary.Raw.(map[string]any)
	if !ok || mapFromAny(raw["models"]) == nil {
		t.Fatalf("summary raw = %#v", summary.Raw)
	}

	balance, err := antigravity.FetchBalance(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchBalance returned error: %v", err)
	}
	balanceRaw, ok := balance.Raw.(map[string]any)
	if !ok || balanceRaw["type"] != "per_model" {
		t.Fatalf("balance raw = %#v", balance.Raw)
	}

	metadata, err := antigravity.FetchMetadata(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchMetadata returned error: %v", err)
	}
	metadataRaw, ok := metadata.Raw.(map[string]any)
	if !ok {
		t.Fatalf("metadata raw = %#v", metadata.Raw)
	}
	if metadataRaw["project_id"] != "auth-project" {
		t.Fatalf("metadata project_id = %#v, want auth-project", metadataRaw["project_id"])
	}
	rawModels, ok := metadataRaw["models"].([]map[string]any)
	if !ok || len(rawModels) != 2 {
		t.Fatalf("metadata models = %#v", metadataRaw["models"])
	}

	mu.Lock()
	defer mu.Unlock()
	wantProjects := []string{"summary-project", "auth-project", "auth-project"}
	if len(projects) != len(wantProjects) {
		t.Fatalf("quota projects = %#v, want %#v", projects, wantProjects)
	}
	for index, want := range wantProjects {
		if projects[index] != want {
			t.Fatalf("quota project[%d] = %q, want %q; all projects=%#v", index, projects[index], want, projects)
		}
	}
}

func TestAntigravityFetchUserSummaryPrefersLoadCodeAssistProjectAndTier(t *testing.T) {
	var mu sync.Mutex
	var loadBody map[string]any
	var quotaBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			loadBody = body
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"cloudaicompanionProject": "loaded-project",
				"paidTier": {"name": "pro-tier"}
			}`))
		case "/v1internal:fetchAvailableModels":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			quotaBody = body
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"models": {
					"gemini-3.1-pro": {
						"displayName": "Gemini 3.1 Pro",
						"quotaInfo": {"remainingFraction": 0.50}
					}
				}
			}`))
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	summary, err := NewAntigravity().FetchUserSummary(context.Background(), SiteConfig{
		BaseURL: server.URL,
		Client:  server.Client(),
	}, SystemAuth{
		AccessToken: "token-c",
		AccountID:   "acct-1",
		Email:       "user@example.com",
		Metadata: map[string]any{
			"project_id":        "metadata-project",
			"subscription_tier": "free-tier",
		},
	})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}

	mu.Lock()
	metadata, ok := mapFromAny(loadBody["metadata"])["ideType"].(string)
	quotaProject := stringFromAny(quotaBody["project"])
	mu.Unlock()
	if !ok || metadata != "ANTIGRAVITY" {
		t.Fatalf("loadCodeAssist body = %#v", loadBody)
	}
	if quotaProject != "loaded-project" {
		t.Fatalf("quota project = %q, want loaded-project", quotaProject)
	}

	user, ok := summary.User.(map[string]any)
	if !ok {
		t.Fatalf("summary user = %#v", summary.User)
	}
	if user["project_id"] != "loaded-project" || user["subscription_tier"] != "pro-tier" || user["plan_type"] != "pro-tier" {
		t.Fatalf("summary user project/tier = %#v", user)
	}
	if user["email"] != "user@example.com" || user["account_id"] != "acct-1" {
		t.Fatalf("summary user identity = %#v", user)
	}
	apiKeys, ok := summary.APIKeys.(map[string]any)
	if !ok || apiKeys["count"] != 1 || apiKeys["mode"] != "oauth_bearer" {
		t.Fatalf("summary api keys = %#v", summary.APIKeys)
	}
	userModels, ok := summary.UserModels.(map[string]any)
	if !ok {
		t.Fatalf("summary user models = %#v", summary.UserModels)
	}
	models, ok := userModels["data"].([]map[string]any)
	if !ok || len(models) != 1 || models[0]["name"] != "gemini-3.1-pro" {
		t.Fatalf("summary user model data = %#v", userModels["data"])
	}
}

func TestAntigravityFetchUserSummaryFallsBackWhenLoadCodeAssistFails(t *testing.T) {
	var mu sync.Mutex
	var quotaProject string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1internal:loadCodeAssist":
			http.Error(w, "load failed", http.StatusInternalServerError)
		case "/v1internal:fetchAvailableModels":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			quotaProject = stringFromAny(body["project"])
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models": {}}`))
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	summary, err := NewAntigravity().FetchUserSummary(context.Background(), SiteConfig{
		BaseURL: server.URL,
		Client:  server.Client(),
	}, SystemAuth{
		AccessToken: "token-d",
		Metadata: map[string]any{
			"project_id":        "metadata-project",
			"subscription_tier": "metadata-tier",
		},
	})
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}

	mu.Lock()
	gotQuotaProject := quotaProject
	mu.Unlock()
	if gotQuotaProject != "metadata-project" {
		t.Fatalf("quota project = %q, want metadata-project", gotQuotaProject)
	}
	user, ok := summary.User.(map[string]any)
	if !ok {
		t.Fatalf("summary user = %#v", summary.User)
	}
	if user["project_id"] != "metadata-project" || user["subscription_tier"] != "metadata-tier" {
		t.Fatalf("summary user fallback fields = %#v", user)
	}
}

func TestAntigravityReadQuotaResponseForbiddenErrorAndDecodeBranches(t *testing.T) {
	forbidden, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("forbidden")),
	})
	if err != nil {
		t.Fatalf("forbidden response returned error: %v", err)
	}
	if forbidden["is_forbidden"] != true || mapFromAny(forbidden["models"]) == nil {
		t.Fatalf("forbidden payload = %#v", forbidden)
	}

	decoded, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusCreated,
		Body:       io.NopCloser(strings.NewReader(`{"models":{"gemini-3.1-pro":{}}}`)),
	})
	if err != nil || mapFromAny(decoded["models"]) == nil {
		t.Fatalf("created response decoded = %#v, err=%v", decoded, err)
	}

	if _, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Body:       io.NopCloser(strings.NewReader("quota offline")),
	}); err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "quota offline") {
		t.Fatalf("503 error = %v, want status/body", err)
	}

	if _, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{`)),
	}); err == nil || !strings.Contains(err.Error(), "decode antigravity response") {
		t.Fatalf("decode error = %v, want decode failure", err)
	}
}

func TestAntigravityFetchQuotaUsesCustomBaseEndpoint(t *testing.T) {
	var requestedURL string
	var requestedProject string
	client := &http.Client{Transport: adapterRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestedURL = req.URL.String()
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return antigravityResponse(http.StatusBadRequest, err.Error(), req), nil
		}
		requestedProject = stringFromAny(body["project"])
		return antigravityResponse(http.StatusOK, `{
			"models": {
				"gemini-3.1-flash": {
					"displayName": "Gemini 3.1 Flash",
					"quotaInfo": {"remainingFraction": 1}
				}
			}
		}`, req), nil
	})}

	snapshot, err := NewAntigravity().fetchQuota(context.Background(), SiteConfig{
		BaseURL: " https://quota.example/custom/ ",
		Client:  client,
	}, "token-e", " custom-project ")
	if err != nil {
		t.Fatalf("fetchQuota returned error: %v", err)
	}
	if requestedURL != "https://quota.example/custom/v1internal:fetchAvailableModels" {
		t.Fatalf("requested URL = %q, want custom base endpoint", requestedURL)
	}
	if requestedProject != "custom-project" {
		t.Fatalf("requested project = %q, want custom-project", requestedProject)
	}
	if snapshot.ProjectID != "custom-project" || len(snapshot.Models) != 1 {
		t.Fatalf("custom quota snapshot = %#v", snapshot)
	}
}

type antigravityQuotaRequest struct {
	Method        string
	Authorization string
	UserAgent     string
	Project       string
}

func antigravityResponse(status int, body string, req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
