package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func zhipuFindModel(t *testing.T, models []Model, name string) Model {
	t.Helper()
	for _, model := range models {
		if model.UpstreamName == name {
			return model
		}
	}
	t.Fatalf("model %q not found in %v", name, models)
	return Model{}
}

func TestZhipuListModelsUsesOnlyUpstreamModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("models path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-zhipu" {
			t.Errorf("Authorization = %q, want Bearer sk-zhipu", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"object":"list","data":[{"id":"glm-5.3","owned_by":"z-ai"},{"id":"glm-4.7","owned_by":"z-ai"},{"id":""},{"id":"glm-5.3-flash"}]}`))
	}))
	defer server.Close()

	models, err := NewZhipu().ListModels(context.Background(), SiteConfig{BaseURL: server.URL + "/", SiteType: "zhipu"}, "sk-zhipu")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if got := zhipuFindModel(t, models, "glm-5.3").Capabilities["source"]; got != "upstream" {
		t.Errorf("glm-5.3 source = %v, want upstream", got)
	}
	if got := zhipuFindModel(t, models, "glm-5.3").Capabilities["owned_by"]; got != "z-ai" {
		t.Errorf("glm-5.3 owned_by = %v, want z-ai", got)
	}

	wantNames := []string{"glm-5.3", "glm-4.7", "glm-5.3-flash"}
	if len(models) != len(wantNames) {
		t.Fatalf("ListModels returned %d models, want only %d upstream models", len(models), len(wantNames))
	}
	for i, name := range wantNames {
		if models[i].UpstreamName != name {
			t.Errorf("model[%d] = %q, want %q", i, models[i].UpstreamName, name)
		}
		if got := models[i].Capabilities["source"]; got != "upstream" {
			t.Errorf("model[%d] source = %v, want upstream", i, got)
		}
	}
}

func TestGLMCodeListModelsUsesOnlyUpstreamModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("models path = %q, want /models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"glm-4.5-air"}]}`))
	}))
	defer server.Close()

	models, err := NewGLMCode().ListModels(context.Background(), SiteConfig{BaseURL: server.URL, SiteType: "glm_code"}, "sk-glm")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("ListModels returned %d models, want only 1 upstream model", len(models))
	}
	if got := zhipuFindModel(t, models, "glm-4.5-air").Capabilities["source"]; got != "upstream" {
		t.Errorf("glm-4.5-air source = %v, want upstream", got)
	}
}

func TestZhipuListModelsPropagatesUpstreamErrors(t *testing.T) {
	closedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedServer.Close()

	cases := map[string]func(http.ResponseWriter, *http.Request){
		"server error":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"not found":     func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		"unauthorized":  func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) },
		"bad json":      func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`not json`)) },
		"empty data":    func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"data":[]}`)) },
		"network error": nil,
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			site := SiteConfig{BaseURL: closedServer.URL, SiteType: "zhipu"}
			if handler != nil {
				server := httptest.NewServer(http.HandlerFunc(handler))
				defer server.Close()
				site.BaseURL = server.URL
			}

			models, err := NewZhipu().ListModels(context.Background(), site, "sk-zhipu")
			if err == nil {
				t.Fatal("ListModels should propagate upstream errors, got nil")
			}
			if len(models) != 0 {
				t.Errorf("ListModels returned %d models alongside error, want none", len(models))
			}
		})
	}
}

func TestZhipuValidateCredentialsUsesListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-zhipu" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"glm-5.3","owned_by":"z-ai"}]}`))
	}))
	defer server.Close()

	site := SiteConfig{BaseURL: server.URL, SiteType: "zhipu"}
	if err := NewZhipu().ValidateCredentials(context.Background(), site, "sk-zhipu"); err != nil {
		t.Fatalf("ValidateCredentials returned error for valid key: %v", err)
	}
	if err := NewZhipu().ValidateCredentials(context.Background(), site, "sk-wrong"); err == nil {
		t.Fatal("ValidateCredentials should fail for rejected key")
	}
	if err := NewZhipu().ValidateCredentials(context.Background(), site, "  "); err == nil {
		t.Fatal("ValidateCredentials should fail for empty key")
	}
}

func TestZhipuListModelsRequiresAPIKey(t *testing.T) {
	if _, err := NewZhipu().ListModels(context.Background(), SiteConfig{BaseURL: "http://unused.example", SiteType: "zhipu"}, "  "); err == nil {
		t.Fatal("expected error for empty api key")
	}
	if _, err := NewGLMCode().ListModels(context.Background(), SiteConfig{BaseURL: "http://unused.example", SiteType: "glm_code"}, ""); err == nil {
		t.Fatal("expected error for empty api key")
	}
}
