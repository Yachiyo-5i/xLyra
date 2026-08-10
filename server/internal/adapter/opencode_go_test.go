package adapter

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestOpenCodeGoListModelsUsesSpecEndpointTypes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("models path = %q, want /v1/models", r.URL.Path)
		}
		if authorization := r.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("models request Authorization = %q, want empty", authorization)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-luna","object":"model","created":1,"owned_by":"openai"},{"id":"minimax-m3","object":"model"},{"id":"kimi-k3","object":"model"},{"id":"unknown-next-model","object":"model"}]}`))
	}))
	t.Cleanup(server.Close)

	models, err := NewOpenCodeGo().ListModels(t.Context(), SiteConfig{BaseURL: server.URL}, "unused-key")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 4 {
		t.Fatalf("ListModels returned %d models, want 4", len(models))
	}

	wants := map[string][]string{
		"gpt-5.6-luna":       {"openai-response"},
		"minimax-m3":         {"anthropic-messages"},
		"kimi-k3":            {"openai"},
		"unknown-next-model": {"openai"},
	}
	for _, model := range models {
		got, _ := model.Capabilities["supported_endpoint_types"].([]string)
		if !reflect.DeepEqual(got, wants[model.UpstreamName]) {
			t.Fatalf("model %q endpoint types = %#v, want %#v", model.UpstreamName, got, wants[model.UpstreamName])
		}
		if model.Capabilities["source"] != "opencode_go_spec" {
			t.Fatalf("model %q source = %#v", model.UpstreamName, model.Capabilities["source"])
		}
	}
	if models[3].Capabilities["protocol_mapping_status"] != "fallback" {
		t.Fatalf("unknown model mapping status = %#v", models[3].Capabilities["protocol_mapping_status"])
	}
}

func TestOpenCodeGoModuleRegistration(t *testing.T) {
	t.Parallel()

	module, ok := NewRegistry().ModuleForSiteType(openCodeGoSiteType)
	if !ok {
		t.Fatal("OpenCode Go module is not registered")
	}
	if _, ok := AsCredentialValidator(module); ok {
		t.Fatal("OpenCode Go public model listing must not be treated as credential validation")
	}
	if _, ok := AsHealthProbe(module); !ok {
		t.Fatal("OpenCode Go must expose health probe")
	}
	if got := ModuleCapabilities(module); !reflect.DeepEqual(got, []Capability{CapabilityHealthProbe, CapabilityListModels, CapabilityFetchPricing}) {
		t.Fatalf("OpenCode Go capabilities = %#v", got)
	}
	if got := module.(OpenCodeGo).DefaultBaseURL(); got != openCodeGoDefaultBaseURL {
		t.Fatalf("DefaultBaseURL = %q, want %q", got, openCodeGoDefaultBaseURL)
	}
}
