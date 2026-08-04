package modelcapabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	modelsDevURL      = "https://models.dev/api.json"
	modelsDevCacheTTL = 6 * time.Hour
)

type modelsDevSource struct {
	client *http.Client

	mu       sync.Mutex
	loaded   bool
	loadedAt time.Time
	payload  map[string]modelsDevProvider
	group    singleflight.Group
}

type modelsDevProvider struct {
	Models map[string]map[string]any `json:"models"`
}

func newModelsDevSource(client *http.Client) *modelsDevSource {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &modelsDevSource{
		client: client,
	}
}

func (s *modelsDevSource) Name() string {
	return sourceModelsDev
}

func (s *modelsDevSource) Lookup(ctx context.Context, input Input) (map[string]any, bool, error) {
	provider := normalizeProvider(input.Provider)
	if provider == "" {
		return nil, false, nil
	}

	payload, err := s.ensureLoaded(ctx)
	if err != nil {
		return nil, false, err
	}

	providerPayload, ok := payload[provider]
	if !ok {
		return nil, false, nil
	}

	models := providerPayload.Models
	if len(models) == 0 {
		return nil, false, nil
	}

	modelID := normalizeModelID(input.ModelID)
	if modelID == "" {
		return nil, false, nil
	}

	if model, ok := models[modelID]; ok {
		return cloneMap(model), true, nil
	}
	return nil, false, nil
}

func (s *modelsDevSource) LookupName(ctx context.Context, provider string, modelID string) (string, bool) {
	provider = normalizeProvider(provider)
	if provider == "" {
		return "", false
	}

	payload, err := s.ensureLoaded(ctx)
	if err != nil {
		return "", false
	}

	providerPayload, ok := payload[provider]
	if !ok {
		return "", false
	}

	modelID = normalizeModelID(modelID)
	if modelID == "" {
		return "", false
	}

	model, ok := providerPayload.Models[modelID]
	if !ok {
		return "", false
	}

	name, _ := model["name"].(string)
	return name, name != ""
}

func (s *modelsDevSource) cachedPayload() (map[string]modelsDevProvider, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded && time.Since(s.loadedAt) < modelsDevCacheTTL {
		return s.payload, true
	}
	return nil, false
}

func (s *modelsDevSource) ensureLoaded(ctx context.Context) (map[string]modelsDevProvider, error) {
	if payload, ok := s.cachedPayload(); ok {
		return payload, nil
	}
	// Collapse concurrent (re)loads into one upstream fetch; the rest reuse it.
	result, err, _ := s.group.Do("models.dev", func() (any, error) {
		if payload, ok := s.cachedPayload(); ok {
			return payload, nil
		}
		payload, err := s.fetch(ctx)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.payload = payload
		s.loaded = true
		s.loadedAt = time.Now()
		s.mu.Unlock()
		return payload, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]modelsDevProvider), nil
}

func (s *modelsDevSource) fetch(ctx context.Context) (map[string]modelsDevProvider, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create models.dev request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xLyra/1.0 (+https://github.com/nanako/xLyra)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models.dev returned %d", resp.StatusCode)
	}

	payload := map[string]modelsDevProvider{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode models.dev catalog: %w", err)
	}
	return payload, nil
}
