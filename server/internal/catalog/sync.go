package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/store"
)

const modelsDevSyncURL = "https://models.dev/api.json"

var syncProviders = map[string]string{
	"openai":        "openai",
	"anthropic":     "anthropic",
	"google":        "google",
	"deepseek":      "deepseek",
	"xai":           "xai",
	"qwen":          "qwen",
	"zhipuai":       "zhipuai",
	"moonshotai-cn": "moonshotai-cn",
	"minimax":       "minimax",
	"xiaomi":        "xiaomi",
	"nvidia":        "nvidia",
}

type modelsDevCatalog map[string]modelsDevSyncProvider

type modelsDevSyncProvider struct {
	Models map[string]modelsDevSyncModel `json:"models"`
}

type modelsDevSyncModel struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Cost       map[string]any      `json:"cost"`
	Modalities map[string][]string `json:"modalities"`
	Limit      map[string]any      `json:"limit"`
}

type SyncService struct {
	db     *store.Store
	logger *slog.Logger
	client *http.Client
}

func NewSyncService(db *store.Store, logger *slog.Logger, confFiles ...*config.ConfigFile) *SyncService {
	var confFile *config.ConfigFile
	if len(confFiles) > 0 {
		confFile = confFiles[0]
	}
	client, _ := httpclient.NewManager(confFile).Client(httpclient.DefaultProfile())
	return &SyncService{
		db:     db,
		logger: logger,
		client: client,
	}
}

func (s *SyncService) SyncAll(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("models.dev sync started")

	catalog, err := s.fetchCatalog(ctx)
	if err != nil {
		return fmt.Errorf("fetch models.dev catalog: %w", err)
	}

	repo := store.NewCanonicalModelRepository(s.db.DB())
	total := 0
	synced := 0

	for devKey, canonicalProvider := range syncProviders {
		providerPayload, ok := catalog[devKey]
		if !ok || len(providerPayload.Models) == 0 {
			continue
		}

		for modelID, modelData := range providerPayload.Models {
			total++
			if err := s.syncModel(ctx, repo, canonicalProvider, modelID, modelData); err != nil {
				s.logger.Warn("sync model failed",
					"provider", canonicalProvider,
					"model_id", modelID,
					"error", err,
				)
				continue
			}
			synced++
		}
	}

	s.logger.Info("models.dev sync finished",
		"total", total,
		"synced", synced,
		"duration", time.Since(start),
	)

	if err := s.syncLiteLLM(ctx, repo); err != nil {
		s.logger.Warn("litellm price repo sync failed, kept models.dev prices", "error", err)
	}

	if filled := s.syncCuratedFallbackPrices(ctx, repo); filled > 0 {
		s.logger.Info("curated fallback prices applied", "filled", filled)
	}
	if updated, err := ReconcileProviders(ctx, s.db.DB()); err != nil {
		return fmt.Errorf("reconcile canonical model providers: %w", err)
	} else if updated > 0 {
		s.logger.Info("canonical model providers reconciled", "updated", updated)
	}
	if err := ReconcileCategories(ctx, s.db.DB()); err != nil {
		return fmt.Errorf("reconcile canonical model categories: %w", err)
	}
	return nil
}

func (s *SyncService) fetchCatalog(ctx context.Context) (modelsDevCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevSyncURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xLyra/1.0")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models.dev returned %d", resp.StatusCode)
	}

	catalog := modelsDevCatalog{}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

func (s *SyncService) syncModel(ctx context.Context, repo store.CanonicalModelRepository, provider string, modelID string, data modelsDevSyncModel) error {
	modelKey := strings.TrimSpace(modelID)
	if modelKey == "" {
		return nil
	}

	displayName := data.Name
	if displayName == "" {
		displayName = modelKey
	}

	category := InferCategory(modelKey)
	endpointTypes := inferEndpointTypes(provider, modelKey, category)

	inputPrice := extractCost(data.Cost, "input")
	outputPrice := extractCost(data.Cost, "output")
	cacheReadRatio := sql.NullFloat64{}
	cacheWriteRatio := sql.NullFloat64{}
	cacheWrite1hRatio := sql.NullFloat64{}

	if inputPrice.Valid && inputPrice.Float64 > 0 {
		if cacheRead := extractFloat(data.Cost, "cache_read"); cacheRead > 0 {
			cacheReadRatio = sql.NullFloat64{Float64: cacheRead / inputPrice.Float64, Valid: true}
		}
		if cacheWrite := extractFloat(data.Cost, "cache_write"); cacheWrite > 0 {
			cacheWriteRatio = sql.NullFloat64{Float64: cacheWrite / inputPrice.Float64, Valid: true}
		}
		if cacheWrite1h := extractFloat(data.Cost, "cache_write_1h"); cacheWrite1h > 0 {
			cacheWrite1hRatio = sql.NullFloat64{Float64: cacheWrite1h / inputPrice.Float64, Valid: true}
		}
		if !cacheWrite1hRatio.Valid && cacheWriteRatio.Valid && hasEndpointType(endpointTypes, "anthropic-messages") {
			cacheWrite1hRatio = sql.NullFloat64{Float64: 2.0, Valid: true}
		}
	}

	contextWindow := extractInt(data.Limit, "context")
	maxOutput := extractInt(data.Limit, "output")

	modalitiesJSON := []byte("[]")
	if len(data.Modalities) > 0 {
		if encoded, err := json.Marshal(data.Modalities); err == nil {
			modalitiesJSON = encoded
		}
	}

	endpointTypesJSON := []byte("[]")
	if len(endpointTypes) > 0 {
		if encoded, err := json.Marshal(endpointTypes); err == nil {
			endpointTypesJSON = encoded
		}
	}

	now := time.Now()
	_, err := repo.SyncUpsert(ctx, store.UpsertCanonicalModelParams{
		ModelKey:               modelKey,
		DisplayName:            displayName,
		Provider:               provider,
		Category:               category,
		Capabilities:           []byte("{}"),
		Status:                 "active",
		InputPrice:             inputPrice,
		OutputPrice:            outputPrice,
		CacheReadRatio:         cacheReadRatio,
		CacheWriteRatio:        cacheWriteRatio,
		CacheWrite1hRatio:      cacheWrite1hRatio,
		SupportedEndpointTypes: store.JSON(endpointTypesJSON),
		Modalities:             store.JSON(modalitiesJSON),
		ContextWindow:          contextWindow,
		MaxOutputTokens:        maxOutput,
		PricingSource:          store.CanonicalPricingSourceModelsDev,
		LastPricingSyncedAt:    sql.NullTime{Time: now, Valid: true},
	})
	return err
}

func inferEndpointTypes(provider string, modelKey string, category string) []string {
	if endpointTypes := modelcapabilities.ModelNameEndpointTypes(modelKey); len(endpointTypes) > 0 {
		return endpointTypes
	}

	switch category {
	case "image":
		return []string{"openai-image"}
	case "embedding":
		return []string{"openai"}
	case "audio":
		return []string{"openai"}
	}

	modelKey = strings.ToLower(strings.TrimSpace(modelKey))
	if strings.Contains(modelKey, "codex") {
		return []string{"openai-response"}
	}

	switch provider {
	case "anthropic":
		return []string{"anthropic-messages"}
	default:
		return []string{"openai"}
	}
}

func hasEndpointType(types []string, target string) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}

func extractCost(cost map[string]any, key string) sql.NullFloat64 {
	if cost == nil {
		return sql.NullFloat64{}
	}
	value := extractFloat(cost, key)
	if value > 0 {
		return sql.NullFloat64{Float64: value, Valid: true}
	}
	return sql.NullFloat64{}
}

func extractFloat(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func extractInt(m map[string]any, key string) sql.NullInt32 {
	if m == nil {
		return sql.NullInt32{}
	}
	switch v := m[key].(type) {
	case float64:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	case float32:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	case int:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	case int64:
		return sql.NullInt32{Int32: int32(v), Valid: true}
	default:
		return sql.NullInt32{}
	}
}
