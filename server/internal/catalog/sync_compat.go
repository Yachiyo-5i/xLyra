package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/store"
)

const modelsDevSyncURL = catalogSyncURL

type modelsDevSyncModel struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Cost       map[string]any      `json:"cost"`
	Modalities map[string][]string `json:"modalities"`
	Limit      map[string]any      `json:"limit"`
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
	for _, v := range types {
		if v == target {
			return true
		}
	}
	return false
}
func extractFloat(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}
func extractCost(m map[string]any, key string) sql.NullFloat64 {
	v := extractFloat(m, key)
	if v <= 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: v, Valid: true}
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
	}
	return sql.NullInt32{}
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

type modelsDevCatalog map[string]modelsDevSyncProvider
type modelsDevSyncProvider struct {
	Models map[string]modelsDevSyncModel `json:"models"`
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
	var c modelsDevCatalog
	err = json.NewDecoder(resp.Body).Decode(&c)
	return c, err
}
