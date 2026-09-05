package adapter

import (
	"context"
	"testing"

	"xlyra/server/internal/codexversion"
)

func TestCodexModelsFromItemsEnforcesMinimalClientVersion(t *testing.T) {
	restore := codexversion.WithFetcher(func(context.Context) (string, error) { return "0.144.1", nil })
	defer restore()

	items := []map[string]any{
		{"slug": "gpt-5.6-sol", "priority": 6, "minimal_client_version": "0.144.0"},
		{"slug": "gpt-5.5", "priority": 12, "minimal_client_version": "0.124.0"},
		{"slug": "gpt-5.4", "priority": 16, "minimal_client_version": "0.98.0"},
		{"slug": "gpt-5.7-future", "priority": 1, "minimal_client_version": "0.154.0"},
	}

	models := codexModelsFromItems(items)
	if len(models) != 3 {
		t.Fatalf("models length = %d, want 3 (future-gated model filtered): %#v", len(models), models)
	}
	for _, model := range models {
		if model.UpstreamName == "gpt-5.7-future" {
			t.Fatalf("gpt-5.7-future should be filtered out, got %#v", models)
		}
	}
}

func TestCodexImageRouteModelCarriesImageCapability(t *testing.T) {
	item := codexImageRouteModel()
	if item["id"] != codexImageSlug || item["slug"] != codexImageSlug {
		t.Fatalf("route model id/slug = %#v/%#v, want %s", item["id"], item["slug"], codexImageSlug)
	}
	if item["source"] != "codex_image_route" {
		t.Fatalf("route model source = %#v, want codex_image_route", item["source"])
	}
	endpoints, _ := item["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "openai-image" {
		t.Fatalf("route model endpoints = %#v, want only openai-image", item["supported_endpoint_types"])
	}
}

func TestHasUpstreamModelMatchesByName(t *testing.T) {
	models := []Model{
		{UpstreamName: "gpt-5.6-sol"},
		{UpstreamName: "gpt-5.5"},
	}
	if !hasUpstreamModel(models, "gpt-5.5") {
		t.Fatal("hasUpstreamModel should match existing upstream model")
	}
	if hasUpstreamModel(models, "gpt-image-2") {
		t.Fatal("hasUpstreamModel should not report a model that is absent upstream")
	}
	if hasUpstreamModel(models, "") {
		t.Fatal("hasUpstreamModel should reject empty upstream name")
	}
}

func TestVersionGreaterThan(t *testing.T) {
	cases := []struct {
		a    string
		b    string
		want bool
	}{
		{"0.154.0", "0.153.3", true},
		{"0.153.3", "0.153.3", false},
		{"0.153.2", "0.153.3", false},
		{"0.144.0", "0.98.0", true},
		{"0.98.0", "0.144.0", false},
		{"1.0.0", "0.9.9", true},
	}
	for _, tc := range cases {
		if got := versionGreaterThan(tc.a, tc.b); got != tc.want {
			t.Fatalf("versionGreaterThan(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestParseVersionRejectsMalformed(t *testing.T) {
	if _, ok := parseVersion("1.2"); ok {
		t.Fatal("parseVersion should reject two-part version")
	}
	if _, ok := parseVersion("a.b.c"); ok {
		t.Fatal("parseVersion should reject non-numeric parts")
	}
	if _, ok := parseVersion("1.2.3"); !ok {
		t.Fatal("parseVersion should accept three-part numeric version")
	}
}
