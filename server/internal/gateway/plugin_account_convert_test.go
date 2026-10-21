package gateway

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInvalidateModelsCacheDropsEntries(t *testing.T) {
	cache := newModelsCache()
	apiKeyID := uuid.New()
	cache.items[apiKeyID] = modelsCacheEntry{payload: map[string]any{"object": "list"}, cached: time.Now(), revision: "rev"}
	handler := Handler{modelsCache: cache}
	if _, ok := cache.get(apiKeyID); !ok {
		t.Fatal("seeded models cache was not readable")
	}
	handler.InvalidateModelsCache()
	if _, ok := cache.get(apiKeyID); ok {
		t.Fatal("models cache survived invalidation")
	}
}
