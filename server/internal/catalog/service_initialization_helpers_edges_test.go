package catalog

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestNewServicesInitializeWithoutDatabase(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	if service == nil {
		t.Fatal("NewService returned nil")
	}
	if service.db != nil {
		t.Fatalf("NewService db = %#v, want nil", service.db)
	}
	if service.capabilities == nil {
		t.Fatal("NewService should initialize capabilities")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	syncService := NewSyncService(nil, logger)
	if syncService == nil {
		t.Fatal("NewSyncService returned nil")
	}
	if syncService.db != nil {
		t.Fatalf("NewSyncService db = %#v, want nil", syncService.db)
	}
	if syncService.logger != logger {
		t.Fatal("NewSyncService did not preserve logger")
	}
	if syncService.client == nil {
		t.Fatal("NewSyncService should initialize an HTTP client")
	}
}

func TestCanonicalModelHelpersNormalizeAdditionalInputs(t *testing.T) {
	t.Parallel()

	normalizeCases := map[string]string{
		"":                                "",
		" MODEL/google/Gemini 2.5 Pro!! ": "gemini-2.5-pro",
		"moonshotai/Kimi K2+":             "kimi-k2-plus",
		"nvidia/bge m3 inference":         "bge-m3-inference",
		"...GPT///4o...":                  "gpt-4o",
	}
	for input, want := range normalizeCases {
		if got := NormalizeModelKey(input); got != want {
			t.Fatalf("NormalizeModelKey(%q) = %q, want %q", input, got, want)
		}
	}

	canonicalCases := map[string]string{
		"vendor-openai-gpt-4o-inference-search-business": "gpt-4o",
		"official-nvidia-bge-m3-inference":               "bge-m3",
		"moonshotai/kimi-k2-search":                      "kimi-k2",
		"business":                                       "business",
		"   ":                                            "",
	}
	for input, want := range canonicalCases {
		if got := CanonicalModelKeyFromUpstream(input); got != want {
			t.Fatalf("CanonicalModelKeyFromUpstream(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanonicalModelParamsTrimsExplicitFieldsAndEncodesNestedCapabilities(t *testing.T) {
	t.Parallel()

	params, err := canonicalModelParams(UpsertCanonicalModelInput{
		ModelKey:    " models/openai/GPT 4o ",
		DisplayName: " GPT Four Oh ",
		Provider:    " openai-compatible ",
		Category:    " chat ",
		Status:      " disabled ",
		Capabilities: map[string]any{
			"vision": true,
			"limits": map[string]any{"context": 128000},
		},
	})
	if err != nil {
		t.Fatalf("canonicalModelParams: %v", err)
	}

	if params.ModelKey != "gpt-4o" {
		t.Fatalf("ModelKey = %q, want gpt-4o", params.ModelKey)
	}
	if params.DisplayName != "GPT Four Oh" {
		t.Fatalf("DisplayName = %q, want GPT Four Oh", params.DisplayName)
	}
	if params.Provider != "openai-compatible" {
		t.Fatalf("Provider = %q, want openai-compatible", params.Provider)
	}
	if params.Category != "chat" {
		t.Fatalf("Category = %q, want chat", params.Category)
	}
	if params.Status != "disabled" {
		t.Fatalf("Status = %q, want disabled", params.Status)
	}

	var capabilities map[string]any
	if err := json.Unmarshal(params.Capabilities, &capabilities); err != nil {
		t.Fatalf("capabilities should be JSON: %v", err)
	}
	if capabilities["vision"] != true {
		t.Fatalf("vision capability missing: %#v", capabilities)
	}
	limits, ok := capabilities["limits"].(map[string]any)
	if !ok || limits["context"] != float64(128000) {
		t.Fatalf("nested capability missing: %#v", capabilities)
	}
}

func TestCreateAndUpdateValidationErrorsAvoidRepositoryCalls(t *testing.T) {
	t.Parallel()

	service := &Service{}
	ctx := context.Background()
	modelID := uuid.New()

	_, err := service.Create(ctx, UpsertCanonicalModelInput{ModelKey: "   "})
	assertCatalogErrorContains(t, "Create blank model key", err, "model_key is required")

	_, err = service.Create(ctx, UpsertCanonicalModelInput{
		ModelKey:     "gpt-4o",
		Capabilities: map[string]any{"bad": make(chan int)},
	})
	assertCatalogErrorContains(t, "Create capabilities", err, "marshal canonical model capabilities")

	_, err = service.Update(ctx, modelID, UpsertCanonicalModelInput{ModelKey: "   "})
	assertCatalogErrorContains(t, "Update blank model key", err, "model_key is required")

	_, err = service.Update(ctx, modelID, UpsertCanonicalModelInput{
		ModelKey:     "gpt-4o",
		Capabilities: map[string]any{"bad": func() {}},
	})
	assertCatalogErrorContains(t, "Update capabilities", err, "marshal canonical model capabilities")
}

func TestAliasValidationRejectsNamesThatNormalizeEmpty(t *testing.T) {
	t.Parallel()

	_, err := (&Service{}).AddAlias(context.Background(), uuid.New(), "///...---")
	assertCatalogErrorContains(t, "AddAlias", err, "alias is required")
}

func TestSyncModelSkipsBlankModelIDBeforeRepository(t *testing.T) {
	t.Parallel()

	err := (&SyncService{}).syncModel(context.Background(), store.CanonicalModelRepository{}, "openai", " \t ", modelsDevSyncModel{
		Name: "Ignored",
		Cost: map[string]any{"input": 1},
	})
	if err != nil {
		t.Fatalf("syncModel blank model id should be skipped before repository access: %v", err)
	}
}
