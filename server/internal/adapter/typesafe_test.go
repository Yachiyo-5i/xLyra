package adapter

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"xlyra/server/internal/upstream"
)

func TestTypeSafeListModelsParsesModelsEnvelope(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("models path = %q, want /v1/models", r.URL.Path)
		}
		if authorization := r.Header.Get("Authorization"); authorization != "Bearer ts-key" {
			t.Fatalf("Authorization = %q, want Bearer ts-key", authorization)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"jev-latest","description":"latest","release_date":"2026-09-10T18:38:01.391457+00:00"},{"name":"jev-preview","description":"preview"},{"name":" "}]}`))
	}))
	t.Cleanup(server.Close)

	models, err := NewTypeSafe().ListModels(t.Context(), SiteConfig{BaseURL: server.URL + "/"}, "ts-key")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels returned %d models, want 2", len(models))
	}
	if models[0].UpstreamName != "jev-latest" || models[1].UpstreamName != "jev-preview" {
		t.Fatalf("model names = %q, %q", models[0].UpstreamName, models[1].UpstreamName)
	}
	for _, model := range models {
		got, _ := model.Capabilities["supported_endpoint_types"].([]string)
		if !reflect.DeepEqual(got, []string{"typesafe-systemone"}) {
			t.Fatalf("model %q endpoint types = %#v", model.UpstreamName, got)
		}
		if model.Capabilities["source"] != "typesafe" {
			t.Fatalf("model %q source = %#v", model.UpstreamName, model.Capabilities["source"])
		}
	}
	if models[0].Capabilities["release_date"] != "2026-09-10T18:38:01.391457+00:00" {
		t.Fatalf("release_date = %#v", models[0].Capabilities["release_date"])
	}
}

func TestTypeSafeValidateCredentialsReturnsHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid api key"}`))
	}))
	t.Cleanup(server.Close)

	err := NewTypeSafe().ValidateCredentials(t.Context(), SiteConfig{BaseURL: server.URL}, "bad-key")
	var httpErr *upstream.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Failure.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ValidateCredentials error = %v, want upstream 401", err)
	}
}

func TestTypeSafeModuleRegistration(t *testing.T) {
	t.Parallel()

	module, ok := NewRegistry().ModuleForSiteType(TypeSafeSiteType)
	if !ok {
		t.Fatal("TypeSafe module is not registered")
	}
	if _, ok := AsCredentialValidator(module); !ok {
		t.Fatal("TypeSafe module must validate credentials")
	}
	provider, ok := AsDefaultBaseURLProvider(module)
	if !ok || provider.DefaultBaseURL() != "https://api.typesafe.ai" {
		t.Fatal("TypeSafe module must provide the official base URL")
	}
}
