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
}
type SyncService struct {
	db     *store.Store
	logger *slog.Logger
	client *http.Client
	url    string
	token  string
}

func NewSyncService(db *store.Store, logger *slog.Logger, confFiles ...*config.ConfigFile) *SyncService {
	var cf *config.ConfigFile
	if len(confFiles) > 0 {
		cf = confFiles[0]
	}
	c, _ := httpclient.NewManager(cf).Client(httpclient.DefaultProfile())
	url, token := catalogSyncURL, ""
	if cf != nil {
		if value, ok := cf.Get("model_catalog.url"); ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				url = strings.TrimSpace(text)
			}
		}
		if value, ok := cf.Get("model_catalog.token"); ok {
			token, _ = value.(string)
		}
	}
	return &SyncService{db: db, logger: logger, client: c, url: url, token: strings.TrimSpace(token)}
}
func (s *SyncService) SyncAll(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("model catalog sync started")
	c, err := s.fetchGithubCatalog(ctx)
	if err != nil {
		return fmt.Errorf("fetch model catalog: %w", err)
	}
	repo := store.NewCanonicalModelRepository(s.db.DB())
	total, synced := 0, 0
	for brand, b := range c.Brands {
		for id, m := range b.Models {
			total++
			if err := s.syncCatalogModel(ctx, repo, brand, id, m); err != nil {
				s.logger.Warn("catalog model sync failed", "brand", brand, "model_id", id, "error", err)
				continue
			}
			synced++
		}
	}
	s.logger.Info("model catalog sync finished", "total", total, "synced", synced, "catalog_version", c.CatalogVersion, "duration", time.Since(start))
	if _, err := ReconcileProviders(ctx, s.db.DB()); err != nil {
		return fmt.Errorf("reconcile canonical model providers: %w", err)
	}
	return ReconcileCategories(ctx, s.db.DB())
}
func (s *SyncService) fetchGithubCatalog(ctx context.Context) (catalogPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return catalogPayload{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xLyra/1.0")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
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
	if c.SchemaVersion != 1 && len(c.Brands) > 0 {
		return c, fmt.Errorf("invalid catalog schema or empty brands")
	}
	return c, nil
}
func (s *SyncService) syncCatalogModel(ctx context.Context, repo store.CanonicalModelRepository, brand, id string, m catalogModel) error {
	key := strings.TrimSpace(m.ModelKey)
	if key == "" {
		key = strings.TrimSpace(brand + "/" + id)
	}
	if key == "" {
		return nil
	}
	caps := m.Capabilities
	if caps == nil {
		caps = map[string]any{}
	}
	if len(m.PricingVariants) > 0 {
		caps["pricing_variants"] = m.PricingVariants
	}
	encoded, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	modalities, _ := json.Marshal(m.Modalities)
	endpoints, _ := json.Marshal(m.SupportedEndpointTypes)
	model, err := repo.SyncUpsert(ctx, store.UpsertCanonicalModelParams{ModelKey: key, DisplayName: m.DisplayName, Provider: defaultString(m.Provider, brand), Category: defaultString(m.Category, InferCategory(key)), Capabilities: encoded, Status: defaultString(m.Status, "active"), SupportedEndpointTypes: store.JSON(endpoints), Modalities: store.JSON(modalities), InputPrice: nullFloat(m.InputPrice), OutputPrice: nullFloat(m.OutputPrice), CacheReadRatio: nullFloat(m.CacheReadRatio), CacheWriteRatio: nullFloat(m.CacheWriteRatio), CacheWrite1hRatio: nullFloat(m.CacheWrite1hRatio), ContextWindow: nullInt(m.ContextWindow), MaxOutputTokens: nullInt(m.MaxOutputTokens), PricingSource: store.CanonicalPricingSourceManual, LastPricingSyncedAt: sql.NullTime{Time: time.Now(), Valid: true}})
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
