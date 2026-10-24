package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xlyra/server/internal/site"
)

func TestAccountPoolRuntimeAdapterListModelsNormalizesEntries(t *testing.T) {
	runtime := auditListModelsRuntime(t, `
export function listModels() {
  return { result: { models: [
    { name: " grok-4 ", displayName: " Grok 4 " },
    { name: "plain" },
    { name: "spaced", displayName: "   " },
    { name: "vision", displayName: "Vision", capabilities: { vision: true } }
  ] } };
}
`)
	models, err := runtime.ListModels(context.Background(), map[string]string{"access_token": "ACCESS-list-9f3c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 4 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].UpstreamName != "grok-4" || models[0].DisplayName != "Grok 4" {
		t.Fatalf("trimmed = %+v", models[0])
	}
	if models[1].UpstreamName != "plain" || models[1].DisplayName != "plain" {
		t.Fatalf("display fallback = %+v", models[1])
	}
	if models[2].UpstreamName != "spaced" || models[2].DisplayName != "spaced" {
		t.Fatalf("blank display = %+v", models[2])
	}
	if models[3].UpstreamName != "vision" || models[3].Capabilities["vision"] != true {
		t.Fatalf("capabilities = %+v", models[3])
	}
	for _, model := range models {
		if model.UpstreamName == "" || model.DisplayName == "" || strings.Contains(model.UpstreamName, "ACCESS-list-9f3c") {
			t.Fatalf("model = %+v", model)
		}
	}
}

// A blank model name is a plugin bug. The hook result contract rejects a
// required string that is empty after trimming, so the whole list fails and the
// error names the bad entry. The adapter does not silently drop it.
func TestAccountPoolRuntimeAdapterRejectsBlankModelNames(t *testing.T) {
	runtime := auditListModelsRuntime(t, `
export function listModels() {
  return { result: { models: [
    { name: " ", displayName: "skip" },
    { name: "kept", displayName: "Kept" }
  ] } };
}
`)
	models, err := runtime.ListModels(context.Background(), nil)
	if err == nil || models != nil || !strings.Contains(err.Error(), "result.models[0].name") {
		t.Fatalf("blank model name: models=%v err=%v", models, err)
	}
}

func TestAccountPoolRuntimeAdapterListModelsPassesErrorsThrough(t *testing.T) {
	runtime := auditListModelsRuntime(t, `
export function listModels() { return { error: "model list failed" }; }
`)
	models, err := runtime.ListModels(context.Background(), map[string]string{"access_token": "ACCESS-list-9f3c"})
	if err == nil || models != nil || !strings.Contains(err.Error(), "model list failed") || strings.Contains(err.Error(), "ACCESS-list-9f3c") {
		t.Fatalf("models=%v err=%v", models, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.ListModels(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list = %v", err)
	}

	missing, err := newAccountPoolRuntime(auditAccountPoolPlugin(t, "acme.example", "acme.example/pool", ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.ListModels(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "listModels hook is missing") {
		t.Fatalf("missing hook = %v", err)
	}
}

func auditListModelsRuntime(t *testing.T, listModels string) site.PluginAccountRuntime {
	t.Helper()
	runtime, err := newAccountPoolRuntime(auditAccountPoolPlugin(t, "acme.example", "acme.example/pool", listModels), nil)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
