package catalog

import (
	"context"
	"database/sql"
	"time"

	"xlyra/server/internal/store"
)

const curatedPricingSource = "curated"

type curatedFallbackPrice struct {
	InputPrice           float64
	OutputPrice          float64
	AudioRatio           float64
	AudioCompletionRatio float64
	Override             bool
}

var curatedFallbackPrices = map[string]curatedFallbackPrice{
	"tts-1":                      {InputPrice: 15},
	"tts-1-1106":                 {InputPrice: 15},
	"tts-1-hd":                   {InputPrice: 30},
	"tts-1-hd-1106":              {InputPrice: 30},
	"gpt-4o-mini-tts":            {InputPrice: 0.60, AudioRatio: 20, AudioCompletionRatio: 1, Override: true},
	"gpt-4o-mini-tts-2025-03-20": {InputPrice: 0.60, AudioRatio: 20, AudioCompletionRatio: 1, Override: true},
	"gpt-4o-mini-tts-2025-12-15": {InputPrice: 0.60, AudioRatio: 20, AudioCompletionRatio: 1, Override: true},
}

func (s *SyncService) syncCuratedFallbackPrices(ctx context.Context, repo store.CanonicalModelRepository) int {
	now := time.Now()
	filled := 0
	for modelKey, price := range curatedFallbackPrices {
		params := store.FallbackPriceParams{
			ModelKey:   modelKey,
			InputPrice: sql.NullFloat64{Float64: price.InputPrice, Valid: price.InputPrice > 0},
			Override:   price.Override,
			Source:     curatedPricingSource,
			SyncedAt:   now,
		}
		if price.OutputPrice > 0 {
			params.OutputPrice = sql.NullFloat64{Float64: price.OutputPrice, Valid: true}
		}
		if price.AudioRatio > 0 {
			params.AudioRatio = sql.NullFloat64{Float64: price.AudioRatio, Valid: true}
			completion := price.AudioCompletionRatio
			if completion <= 0 {
				completion = 1
			}
			params.AudioCompletionRatio = sql.NullFloat64{Float64: completion, Valid: true}
		}
		ok, err := repo.FillMissingPrice(ctx, params)
		if err != nil {
			s.logger.Warn("curated fallback price fill failed", "model_id", modelKey, "error", err)
			continue
		}
		if ok {
			filled++
		}
	}
	return filled
}
