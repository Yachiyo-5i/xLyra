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
	"xlyra/server/internal/store"
)

const catalogSyncURL = "https://raw.githubusercontent.com/Yachiyo-5i/models-price/refs/heads/main/catalog.json"

type catalogPayload struct {
	SchemaVersion  int                     `json:"schema_version"`
	CatalogVersion string                  `json:"catalog_version"`
	UpdatedAt      string                  `json:"updated_at"`
	Brands         map[string]catalogBrand `json:"brands"`
}
type catalogBrand struct {
	Models map[string]catalogModel `json:"models"`
}
type catalogModel struct {
	ModelKey               string         `json:"model_key"`
	DisplayName            string         `json:"display_name"`
	Provider               string         `json:"provider"`
	Category               string         `json:"category"`
	Capabilities           map[string]any `json:"capabilities"`
	SupportedEndpointTypes []string       `json:"supported_endpoint_types"`
	Modalities             any            `json:"modalities"`
	ContextWindow          *int           `json:"context_window"`
	MaxOutputTokens        *int           `json:"max_output_tokens"`
	InputPrice             *float64       `json:"input_price"`
	OutputPrice            *float64       `json:"output_price"`
	CacheReadRatio         *float64       `json:"cache_read_ratio"`
	CacheWriteRatio        *float64       `json:"cache_write_ratio"`
	CacheWrite1hRatio      *float64       `json:"cache_write_1h_ratio"`
	PricingVariants        map[string]any `json:"pricing_variants"`
	Status                 string         `json:"status"`
	Aliases                []string       `json:"aliases"`
	Cost                   map[string]any `json:"cost"`
	Experimental           map[string]any `json:"experimental"`
}
type SyncService struct {
	db     *store.Store
	logger *slog.Logger
	client *http.Client
}

func NewSyncService(db *store.Store, logger *slog.Logger, _ ...*config.ConfigFile) *SyncService {
	c, _ := httpclient.NewManager(nil).Client(httpclient.DefaultProfile())
	return &SyncService{db: db, logger: logger, client: c}
}
func (s *SyncService) SyncAll(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("model catalog sync started")
	c, err := s.fetchGithubCatalog(ctx)
	if err != nil {
		return fmt.Errorf("fetch model catalog: %w", err)
	}
	if len(c.Brands) == 0 {
		return fmt.Errorf("catalog contains no brands")
	}
	total, synced := 0, 0
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		repo := store.NewCanonicalModelRepository(tx)
		keys := map[string]struct{}{}
		for brand, b := range c.Brands {
			for id, m := range b.Models {
				total++
				key := CanonicalModelKeyFromUpstream(strings.TrimSpace(m.ModelKey))
				if key == "" {
					key = CanonicalModelKeyFromUpstream(strings.TrimSpace(brand + "/" + id))
				}
				if key == "" {
					return fmt.Errorf("catalog model %s/%s has empty model key", brand, id)
				}
				if _, ok := keys[key]; ok {
					return fmt.Errorf("duplicate catalog model key %q", key)
				}
				keys[key] = struct{}{}
				if err := s.syncCatalogModel(ctx, repo, brand, id, m); err != nil {
					return fmt.Errorf("sync %s/%s: %w", brand, id, err)
				}
				synced++
			}
		}
		models, err := repo.ListAll(ctx)
		if err != nil {
			return err
		}
		for _, model := range models {
			if model.PricingSource == store.CanonicalPricingSourceCatalog {
				if _, ok := keys[model.ModelKey]; !ok {
					if _, err := repo.Archive(ctx, model.ID); err != nil {
						return err
					}
				}
			}
		}
		if _, err := ReconcileProviders(ctx, tx); err != nil {
			return fmt.Errorf("reconcile canonical model providers: %w", err)
		}
		return ReconcileCategories(ctx, tx)
	})
	if err != nil {
		return err
	}
	s.logger.Info("model catalog sync finished", "total", total, "synced", synced, "catalog_version", c.CatalogVersion, "duration", time.Since(start))
	return nil
}

func (s *SyncService) fetchGithubCatalog(ctx context.Context) (catalogPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogSyncURL, nil)
	if err != nil {
		return catalogPayload{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xLyra/1.0")
	resp, err := s.client.Do(req)
	if err != nil {
		return catalogPayload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return catalogPayload{}, fmt.Errorf("catalog returned %d", resp.StatusCode)
	}
	var c catalogPayload
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return c, err
	}
	if c.SchemaVersion != 1 {
		return c, fmt.Errorf("invalid catalog schema or empty brands")
	}
	return c, nil
}
func (s *SyncService) syncCatalogModel(ctx context.Context, repo store.CanonicalModelRepository, brand, id string, m catalogModel) error {
	key := CanonicalModelKeyFromUpstream(strings.TrimSpace(m.ModelKey))
	if key == "" {
		key = CanonicalModelKeyFromUpstream(strings.TrimSpace(brand + "/" + id))
	}
	if key == "" {
		return nil
	}
	caps := m.Capabilities
	if caps == nil {
		caps = map[string]any{}
	}
	pricingVariants := map[string]any{}
	if len(m.PricingVariants) > 0 {
		pricingVariants = m.PricingVariants
	}
	if len(m.Cost) > 0 {
		pricingVariants["cost"] = m.Cost
	}
	if len(m.Experimental) > 0 {
		pricingVariants["experimental"] = m.Experimental
	}
	if len(pricingVariants) > 0 {
		caps["pricing_variants"] = pricingVariants
	}
	encoded, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	modalities, _ := json.Marshal(m.Modalities)
	endpoints, _ := json.Marshal(m.SupportedEndpointTypes)
	cacheReadRatio := m.CacheReadRatio
	cacheWriteRatio := m.CacheWriteRatio
	cacheWrite1hRatio := m.CacheWrite1hRatio
	if m.InputPrice != nil && *m.InputPrice > 0 {
		if v, ok := m.Cost["cache_read"].(float64); ok && cacheReadRatio == nil {
			x := v / *m.InputPrice
			cacheReadRatio = &x
		}
		if v, ok := m.Cost["cache_write"].(float64); ok && cacheWriteRatio == nil {
			x := v / *m.InputPrice
			cacheWriteRatio = &x
		}
		if v, ok := m.Cost["cache_write_1h"].(float64); ok && cacheWrite1hRatio == nil {
			x := v / *m.InputPrice
			cacheWrite1hRatio = &x
		}
	}
	model, err := repo.SyncUpsert(ctx, store.UpsertCanonicalModelParams{ModelKey: key, DisplayName: m.DisplayName, Provider: defaultString(m.Provider, brand), Category: defaultString(m.Category, InferCategory(key)), Capabilities: encoded, Status: defaultString(m.Status, "active"), SupportedEndpointTypes: store.JSON(endpoints), Modalities: store.JSON(modalities), InputPrice: nullFloat(m.InputPrice), OutputPrice: nullFloat(m.OutputPrice), CacheReadRatio: nullFloat(cacheReadRatio), CacheWriteRatio: nullFloat(cacheWriteRatio), CacheWrite1hRatio: nullFloat(cacheWrite1hRatio), ContextWindow: nullInt(m.ContextWindow), MaxOutputTokens: nullInt(m.MaxOutputTokens), PricingVariants: mustJSON(pricingVariants), PricingSource: store.CanonicalPricingSourceCatalog, LastPricingSyncedAt: sql.NullTime{Time: time.Now(), Valid: true}})
	if err != nil {
		return err
	}
	for _, a := range m.Aliases {
		if strings.TrimSpace(a) != "" {
			_, _ = repo.CreateAlias(ctx, store.CreateCanonicalModelAliasParams{CanonicalModelID: model.ID, Alias: a, NormalizedAlias: strings.ToLower(strings.TrimSpace(a)), Source: "catalog"})
		}
	}
	return nil
}
func nullFloat(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}
func nullInt(v *int) sql.NullInt32 {
	if v == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*v), Valid: true}
}
func defaultString(v, f string) string {
	if strings.TrimSpace(v) == "" {
		return f
	}
	return v
}

func mustJSON(v any) store.JSON {
	if v == nil {
		return store.JSON([]byte("{}"))
	}
	b, _ := json.Marshal(v)
	return store.JSON(b)
}
