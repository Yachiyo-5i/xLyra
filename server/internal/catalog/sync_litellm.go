package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/store"
)

const litellmPriceRepoURL = "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json"

const litellmPricingSource = "litellm_repo"

type litellmCatalog map[string]litellmModel

type litellmModel struct {
	LiteLLMProvider                     string  `json:"litellm_provider"`
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	InputCostPerCharacter               float64 `json:"input_cost_per_character"`
	OutputCostPerAudioToken             float64 `json:"output_cost_per_audio_token"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
}

func litellmProviderToCanonical(provider string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai", "text-completion-openai":
		return "openai", true
	case "anthropic":
		return "anthropic", true
	case "gemini", "vertex_ai-language-models", "vertex_ai-embedding-models":
		return "google", true
	case "xai":
		return "xai", true
	case "deepseek":
		return "deepseek", true
	default:
		return "", false
	}
}

func (s *SyncService) syncLiteLLM(ctx context.Context, repo store.CanonicalModelRepository) error {
	catalog, err := s.fetchLiteLLMCatalog(ctx)
	if err != nil {
		return err
	}

	total := 0
	synced := 0
	for modelKey, data := range catalog {
		provider, ok := litellmProviderToCanonical(data.LiteLLMProvider)
		if !ok {
			continue
		}
		total++
		if err := s.syncLiteLLMModel(ctx, repo, provider, modelKey, data); err != nil {
			s.logger.Warn("litellm sync model failed",
				"provider", provider,
				"model_id", modelKey,
				"error", err,
			)
			continue
		}
		synced++
	}

	s.logger.Info("litellm price repo sync finished", "total", total, "synced", synced)
	return nil
}

func (s *SyncService) fetchLiteLLMCatalog(ctx context.Context) (litellmCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, litellmPriceRepoURL, nil)
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
		return nil, fmt.Errorf("litellm price repo returned %d", resp.StatusCode)
	}

	catalog := litellmCatalog{}
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

func (s *SyncService) syncLiteLLMModel(ctx context.Context, repo store.CanonicalModelRepository, provider string, modelKey string, data litellmModel) error {
	params, ok := litellmModelParams(provider, modelKey, data, time.Now())
	if !ok {
		return nil
	}
	_, err := repo.SyncPricingUpsert(ctx, params)
	return err
}

func litellmModelParams(provider string, modelKey string, data litellmModel, now time.Time) (store.SyncCanonicalModelPricingParams, bool) {
	modelKey = strings.TrimSpace(modelKey)
	if modelKey == "" {
		return store.SyncCanonicalModelPricingParams{}, false
	}

	cacheReadRatio := sql.NullFloat64{}
	cacheWriteRatio := sql.NullFloat64{}
	cacheWrite1hRatio := sql.NullFloat64{}
	if data.InputCostPerToken > 0 {
		if data.CacheReadInputTokenCost > 0 {
			cacheReadRatio = sql.NullFloat64{Float64: data.CacheReadInputTokenCost / data.InputCostPerToken, Valid: true}
		}
		if data.CacheCreationInputTokenCost > 0 {
			cacheWriteRatio = sql.NullFloat64{Float64: data.CacheCreationInputTokenCost / data.InputCostPerToken, Valid: true}
		}
		if data.CacheCreationInputTokenCostAbove1hr > 0 {
			cacheWrite1hRatio = sql.NullFloat64{Float64: data.CacheCreationInputTokenCostAbove1hr / data.InputCostPerToken, Valid: true}
		}
	}

	inputPrice := litellmPerMillionPrice(data.InputCostPerToken)
	if !inputPrice.Valid && data.InputCostPerCharacter > 0 {
		inputPrice = litellmPerMillionPrice(data.InputCostPerCharacter)
	}

	audioRatio := sql.NullFloat64{}
	audioCompletionRatio := sql.NullFloat64{}
	if data.InputCostPerToken > 0 && data.OutputCostPerAudioToken > 0 {
		audioRatio = sql.NullFloat64{Float64: data.OutputCostPerAudioToken / data.InputCostPerToken, Valid: true}
		audioCompletionRatio = sql.NullFloat64{Float64: 1, Valid: true}
	}

	return store.SyncCanonicalModelPricingParams{
		ModelKey:             modelKey,
		Provider:             provider,
		Category:             InferCategory(modelKey),
		Status:               "active",
		InputPrice:           inputPrice,
		OutputPrice:          litellmPerMillionPrice(data.OutputCostPerToken),
		CacheReadRatio:       cacheReadRatio,
		CacheWriteRatio:      cacheWriteRatio,
		CacheWrite1hRatio:    cacheWrite1hRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		PricingSource:        litellmPricingSource,
		LastPricingSyncedAt:  sql.NullTime{Time: now, Valid: true},
	}, true
}

func litellmPerMillionPrice(perToken float64) sql.NullFloat64 {
	if perToken <= 0 {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: perToken * 1_000_000, Valid: true}
}
