package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const CanonicalPricingSourceManual = "manual"

type CanonicalModel struct {
	ID                     uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	ModelKey               string
	DisplayName            string
	Provider               string
	Category               string
	Capabilities           JSON `gorm:"type:jsonb"`
	Status                 string
	InputPrice             sql.NullFloat64
	OutputPrice            sql.NullFloat64
	CacheReadRatio         sql.NullFloat64
	CacheWriteRatio        sql.NullFloat64
	CacheWrite1hRatio      sql.NullFloat64 `gorm:"column:cache_write_1h_ratio"`
	AudioRatio             sql.NullFloat64
	AudioCompletionRatio   sql.NullFloat64
	SupportedEndpointTypes JSON `gorm:"type:jsonb"`
	Modalities             JSON `gorm:"type:jsonb"`
	ContextWindow          sql.NullInt32
	MaxOutputTokens        sql.NullInt32
	PricingSource          string
	LastPricingSyncedAt    sql.NullTime
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type CanonicalModelWithStats struct {
	CanonicalModel
	SiteModelCount int `gorm:"-"`
	SiteCount      int `gorm:"-"`
}

type UpsertCanonicalModelParams struct {
	ModelKey               string
	DisplayName            string
	Provider               string
	Category               string
	Capabilities           JSON
	Status                 string
	InputPrice             sql.NullFloat64
	OutputPrice            sql.NullFloat64
	CacheReadRatio         sql.NullFloat64
	CacheWriteRatio        sql.NullFloat64
	CacheWrite1hRatio      sql.NullFloat64
	AudioRatio             sql.NullFloat64
	AudioCompletionRatio   sql.NullFloat64
	SupportedEndpointTypes JSON
	Modalities             JSON
	ContextWindow          sql.NullInt32
	MaxOutputTokens        sql.NullInt32
	PricingSource          string
	LastPricingSyncedAt    sql.NullTime
}

type UpdateCanonicalModelParams struct {
	ID                     uuid.UUID
	ModelKey               string
	DisplayName            string
	Provider               string
	Category               string
	Capabilities           JSON
	Status                 string
	InputPrice             sql.NullFloat64
	OutputPrice            sql.NullFloat64
	CacheReadRatio         sql.NullFloat64
	CacheWriteRatio        sql.NullFloat64
	CacheWrite1hRatio      sql.NullFloat64
	SupportedEndpointTypes JSON
	Modalities             JSON
	ContextWindow          sql.NullInt32
	MaxOutputTokens        sql.NullInt32
	PricingSource          string
	LastPricingSyncedAt    sql.NullTime
}

type UpdateCanonicalModelPricingParams struct {
	ID                uuid.UUID
	Manual            bool
	InputPrice        sql.NullFloat64
	OutputPrice       sql.NullFloat64
	CacheReadRatio    sql.NullFloat64
	CacheWriteRatio   sql.NullFloat64
	CacheWrite1hRatio sql.NullFloat64
}

type SyncCanonicalModelPricingParams struct {
	ModelKey             string
	Provider             string
	Category             string
	Status               string
	InputPrice           sql.NullFloat64
	OutputPrice          sql.NullFloat64
	CacheReadRatio       sql.NullFloat64
	CacheWriteRatio      sql.NullFloat64
	CacheWrite1hRatio    sql.NullFloat64
	AudioRatio           sql.NullFloat64
	AudioCompletionRatio sql.NullFloat64
	PricingSource        string
	LastPricingSyncedAt  sql.NullTime
}

type CanonicalModelAlias struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CanonicalModelID uuid.UUID
	Alias            string
	NormalizedAlias  string
	Source           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateCanonicalModelAliasParams struct {
	CanonicalModelID uuid.UUID
	Alias            string
	NormalizedAlias  string
	Source           string
}

type CanonicalModelMatrixRow struct {
	SiteID               uuid.UUID
	SiteName             string
	SiteSlug             string
	SiteType             string
	SiteEnabled          bool
	SiteModelID          uuid.UUID
	UpstreamModelName    string
	DisplayName          string
	ModelStatus          string
	MatchSource          string
	MatchConfidence      int
	MatchedAt            sql.NullTime
	APIKeyCount          int
	AvailableAPIKeyCount int
	GroupName            sql.NullString
	Currency             sql.NullString
	InputValue           sql.NullFloat64
	OutputValue          sql.NullFloat64
	PerRequestValue      sql.NullFloat64
	BillingType          sql.NullString
	PricingAvailable     sql.NullBool
	PricingLastSyncedAt  sql.NullTime
	SiteModelCreatedAt   time.Time
	SiteModelUpdatedAt   time.Time
}

type CanonicalModelRepository struct {
	db *gorm.DB
}

func NewCanonicalModelRepository(db *gorm.DB) CanonicalModelRepository {
	return CanonicalModelRepository{db: db}
}

func (r CanonicalModelRepository) List(ctx context.Context) ([]CanonicalModelWithStats, error) {
	var models []CanonicalModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list canonical models: %w", err)
	}
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].ModelKey < models[j].ModelKey
	})
	var siteModels []SiteModel
	if err := r.db.WithContext(ctx).Find(&siteModels).Error; err != nil {
		return nil, fmt.Errorf("list canonical model site stats: %w", err)
	}
	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("list canonical model site stats: %w", err)
	}
	activeSites := map[uuid.UUID]struct{}{}
	for _, site := range filterActiveSites(sites) {
		activeSites[site.ID] = struct{}{}
	}
	result := make([]CanonicalModelWithStats, 0, len(models))
	for _, model := range models {
		item := CanonicalModelWithStats{CanonicalModel: model}
		sites := map[uuid.UUID]struct{}{}
		for _, siteModel := range siteModels {
			if siteModel.Status == "unavailable" {
				continue
			}
			if _, ok := activeSites[siteModel.SiteID]; !ok {
				continue
			}
			if siteModel.CanonicalID.Valid && siteModel.CanonicalID.UUID == model.ID {
				item.SiteModelCount++
				sites[siteModel.SiteID] = struct{}{}
			}
		}
		item.SiteCount = len(sites)
		result = append(result, item)
	}
	return result, nil
}

func (r CanonicalModelRepository) ListAll(ctx context.Context) ([]CanonicalModel, error) {
	var models []CanonicalModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list canonical models: %w", err)
	}
	sort.SliceStable(models, func(i, j int) bool {
		return models[i].ModelKey < models[j].ModelKey
	})
	return models, nil
}

func (r CanonicalModelRepository) GetByID(ctx context.Context, id uuid.UUID) (CanonicalModel, error) {
	var item CanonicalModel
	if err := r.db.WithContext(ctx).Where(&CanonicalModel{ID: id}).First(&item).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("get canonical model: %w", err)
	}
	return item, nil
}

func (r CanonicalModelRepository) GetByKey(ctx context.Context, modelKey string) (CanonicalModel, error) {
	// Match case-insensitively: model_key may be stored with mixed case (legacy
	// rows), and the unique index is on LOWER(model_key). A value-equality query
	// would miss those rows and cause Upsert to insert a duplicate. Expressing
	// LOWER(model_key)=? would need a raw SQL fragment (disallowed by the ORM-only
	// policy), so scan the (small) table and compare in Go.
	var models []CanonicalModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("get canonical model by key: %w", err)
	}
	for _, item := range models {
		if strings.EqualFold(item.ModelKey, modelKey) {
			return item, nil
		}
	}
	return CanonicalModel{}, fmt.Errorf("get canonical model by key: %w", gorm.ErrRecordNotFound)
}

func (r CanonicalModelRepository) GetByNormalizedAlias(ctx context.Context, normalizedAlias string) (CanonicalModel, error) {
	var alias CanonicalModelAlias
	if err := r.db.WithContext(ctx).Where(&CanonicalModelAlias{NormalizedAlias: normalizedAlias}).First(&alias).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("get canonical model by alias: %w", err)
	}
	item, err := r.GetByID(ctx, alias.CanonicalModelID)
	if err != nil {
		return CanonicalModel{}, fmt.Errorf("get canonical model by alias: %w", err)
	}
	return item, nil
}

func (r CanonicalModelRepository) Upsert(ctx context.Context, params UpsertCanonicalModelParams) (CanonicalModel, error) {
	if existing, err := r.GetByKey(ctx, params.ModelKey); err == nil {
		if existing.Status == "archived" {
			existing.Status = stringDefault(params.Status, "active")
			if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
				return CanonicalModel{}, fmt.Errorf("restore canonical model: %w", err)
			}
		}
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return CanonicalModel{}, err
	}
	return r.Create(ctx, params)
}

func (r CanonicalModelRepository) SyncUpsert(ctx context.Context, params UpsertCanonicalModelParams) (CanonicalModel, error) {
	existing, err := r.GetByKey(ctx, params.ModelKey)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return CanonicalModel{}, err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.Create(ctx, params)
	}

	existing.ModelKey = params.ModelKey
	existing.DisplayName = stringDefault(params.DisplayName, existing.DisplayName)
	existing.Provider = params.Provider
	existing.Category = params.Category
	existing.Capabilities = mergeCanonicalModelCapabilities(existing, params.Capabilities)
	existing.Status = stringDefault(params.Status, existing.Status)
	if existing.PricingSource != CanonicalPricingSourceManual {
		existing.InputPrice = params.InputPrice
		existing.OutputPrice = params.OutputPrice
		existing.CacheReadRatio = params.CacheReadRatio
		existing.CacheWriteRatio = params.CacheWriteRatio
		existing.CacheWrite1hRatio = params.CacheWrite1hRatio
		existing.AudioRatio = params.AudioRatio
		existing.AudioCompletionRatio = params.AudioCompletionRatio
		existing.PricingSource = stringDefault(params.PricingSource, "none")
		existing.LastPricingSyncedAt = params.LastPricingSyncedAt
	}
	existing.SupportedEndpointTypes = jsonDefault(params.SupportedEndpointTypes, "[]")
	existing.Modalities = jsonKeepNonEmpty(params.Modalities, existing.Modalities)
	existing.ContextWindow = nullInt32OrKeep(params.ContextWindow, existing.ContextWindow)
	existing.MaxOutputTokens = nullInt32OrKeep(params.MaxOutputTokens, existing.MaxOutputTokens)

	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("sync upsert canonical model: %w", err)
	}
	return existing, nil
}

func (r CanonicalModelRepository) SyncPricingUpsert(ctx context.Context, params SyncCanonicalModelPricingParams) (CanonicalModel, error) {
	existing, err := r.GetByKey(ctx, params.ModelKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.Create(ctx, UpsertCanonicalModelParams{
			ModelKey:             params.ModelKey,
			Provider:             params.Provider,
			Category:             params.Category,
			Status:               params.Status,
			InputPrice:           params.InputPrice,
			OutputPrice:          params.OutputPrice,
			CacheReadRatio:       params.CacheReadRatio,
			CacheWriteRatio:      params.CacheWriteRatio,
			CacheWrite1hRatio:    params.CacheWrite1hRatio,
			AudioRatio:           params.AudioRatio,
			AudioCompletionRatio: params.AudioCompletionRatio,
			PricingSource:        params.PricingSource,
			LastPricingSyncedAt:  params.LastPricingSyncedAt,
		})
	}
	if err != nil {
		return CanonicalModel{}, err
	}

	if existing.PricingSource != CanonicalPricingSourceManual {
		existing.InputPrice = params.InputPrice
		existing.OutputPrice = params.OutputPrice
		existing.CacheReadRatio = params.CacheReadRatio
		existing.CacheWriteRatio = params.CacheWriteRatio
		existing.CacheWrite1hRatio = params.CacheWrite1hRatio
		existing.AudioRatio = params.AudioRatio
		existing.AudioCompletionRatio = params.AudioCompletionRatio
		existing.PricingSource = stringDefault(params.PricingSource, "none")
		existing.LastPricingSyncedAt = params.LastPricingSyncedAt
	}

	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("sync pricing upsert canonical model: %w", err)
	}
	return existing, nil
}

type FallbackPriceParams struct {
	ModelKey             string
	InputPrice           sql.NullFloat64
	OutputPrice          sql.NullFloat64
	AudioRatio           sql.NullFloat64
	AudioCompletionRatio sql.NullFloat64
	Override             bool
	Source               string
	SyncedAt             time.Time
}

func (r CanonicalModelRepository) FillMissingPrice(ctx context.Context, params FallbackPriceParams) (bool, error) {
	existing, err := r.GetByKey(ctx, params.ModelKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if existing.PricingSource == CanonicalPricingSourceManual {
		return false, nil
	}
	if !params.Override && existing.InputPrice.Valid {
		return false, nil
	}
	existing.InputPrice = params.InputPrice
	existing.OutputPrice = params.OutputPrice
	existing.AudioRatio = params.AudioRatio
	existing.AudioCompletionRatio = params.AudioCompletionRatio
	existing.PricingSource = stringDefault(params.Source, "curated")
	existing.LastPricingSyncedAt = sql.NullTime{Time: params.SyncedAt, Valid: true}
	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return false, fmt.Errorf("fill missing canonical model price: %w", err)
	}
	return true, nil
}

func (r CanonicalModelRepository) UpdatePricing(ctx context.Context, params UpdateCanonicalModelPricingParams) (CanonicalModel, error) {
	item, err := r.GetByID(ctx, params.ID)
	if err != nil {
		return CanonicalModel{}, err
	}
	if params.Manual {
		item.InputPrice = params.InputPrice
		item.OutputPrice = params.OutputPrice
		item.CacheReadRatio = params.CacheReadRatio
		item.CacheWriteRatio = params.CacheWriteRatio
		item.CacheWrite1hRatio = params.CacheWrite1hRatio
		item.PricingSource = CanonicalPricingSourceManual
	} else {
		item.PricingSource = "none"
	}
	item.LastPricingSyncedAt = sql.NullTime{Time: time.Now(), Valid: true}
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("update canonical model pricing: %w", err)
	}
	return item, nil
}

func (r CanonicalModelRepository) Create(ctx context.Context, params UpsertCanonicalModelParams) (CanonicalModel, error) {
	item := CanonicalModel{
		ModelKey:               params.ModelKey,
		DisplayName:            stringDefault(params.DisplayName, params.ModelKey),
		Provider:               params.Provider,
		Category:               params.Category,
		Capabilities:           jsonDefault(params.Capabilities, "{}"),
		Status:                 params.Status,
		InputPrice:             params.InputPrice,
		OutputPrice:            params.OutputPrice,
		CacheReadRatio:         params.CacheReadRatio,
		CacheWriteRatio:        params.CacheWriteRatio,
		CacheWrite1hRatio:      params.CacheWrite1hRatio,
		AudioRatio:             params.AudioRatio,
		AudioCompletionRatio:   params.AudioCompletionRatio,
		SupportedEndpointTypes: jsonDefault(params.SupportedEndpointTypes, "[]"),
		Modalities:             jsonDefault(params.Modalities, "[]"),
		ContextWindow:          params.ContextWindow,
		MaxOutputTokens:        params.MaxOutputTokens,
		PricingSource:          stringDefault(params.PricingSource, "none"),
		LastPricingSyncedAt:    params.LastPricingSyncedAt,
	}
	// Insert atomically against the LOWER(model_key) unique index. Concurrent
	// startup paths (models.dev sync + per-site model matching) create the same
	// model_key at once; without ON CONFLICT they each miss the existence check
	// and race to INSERT, producing repeated "duplicate key" errors. DO NOTHING
	// makes the loser a no-op; we then read back the winner's row.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "LOWER(model_key)", Raw: true}},
		DoNothing: true,
	}).Create(&item)
	if res.Error != nil {
		return CanonicalModel{}, fmt.Errorf("create canonical model: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		existing, err := r.GetByKey(ctx, params.ModelKey)
		if err != nil {
			return CanonicalModel{}, fmt.Errorf("create canonical model (load existing): %w", err)
		}
		return existing, nil
	}
	return item, nil
}

func (r CanonicalModelRepository) Update(ctx context.Context, params UpdateCanonicalModelParams) (CanonicalModel, error) {
	item, err := r.GetByID(ctx, params.ID)
	if err != nil {
		return CanonicalModel{}, err
	}
	item.ModelKey = params.ModelKey
	item.DisplayName = params.DisplayName
	item.Provider = params.Provider
	item.Category = params.Category
	item.Capabilities = jsonDefault(params.Capabilities, "{}")
	item.Status = params.Status
	item.InputPrice = params.InputPrice
	item.OutputPrice = params.OutputPrice
	item.CacheReadRatio = params.CacheReadRatio
	item.CacheWriteRatio = params.CacheWriteRatio
	item.CacheWrite1hRatio = params.CacheWrite1hRatio
	item.SupportedEndpointTypes = jsonDefault(params.SupportedEndpointTypes, "[]")
	item.Modalities = jsonDefault(params.Modalities, "[]")
	item.ContextWindow = params.ContextWindow
	item.MaxOutputTokens = params.MaxOutputTokens
	item.PricingSource = stringDefault(params.PricingSource, "none")
	item.LastPricingSyncedAt = params.LastPricingSyncedAt
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("update canonical model: %w", err)
	}
	return item, nil
}

func (r CanonicalModelRepository) UpdateCategory(ctx context.Context, id uuid.UUID, category string) (CanonicalModel, error) {
	item, err := r.GetByID(ctx, id)
	if err != nil {
		return CanonicalModel{}, err
	}
	if item.Category == category {
		return item, nil
	}
	item.Category = category
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("update canonical model category: %w", err)
	}
	return item, nil
}

func (r CanonicalModelRepository) UpdateProvider(ctx context.Context, id uuid.UUID, provider string) (CanonicalModel, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return CanonicalModel{}, fmt.Errorf("provider is required")
	}

	item, err := r.GetByID(ctx, id)
	if err != nil {
		return CanonicalModel{}, err
	}
	if item.Provider == provider {
		return item, nil
	}
	item.Provider = provider
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("update canonical model provider: %w", err)
	}
	return item, nil
}

func (r CanonicalModelRepository) ListAliases(ctx context.Context, canonicalID uuid.UUID) ([]CanonicalModelAlias, error) {
	var items []CanonicalModelAlias
	if err := r.db.WithContext(ctx).Where(&CanonicalModelAlias{CanonicalModelID: canonicalID}).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list canonical model aliases: %w", err)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Alias < items[j].Alias
	})
	return items, nil
}

func (r CanonicalModelRepository) CreateAlias(ctx context.Context, params CreateCanonicalModelAliasParams) (CanonicalModelAlias, error) {
	return upsertByKey(ctx, r.db, CanonicalModelAlias{NormalizedAlias: params.NormalizedAlias}, "create canonical model alias", func(item *CanonicalModelAlias) {
		item.CanonicalModelID = params.CanonicalModelID
		item.Alias = params.Alias
		item.NormalizedAlias = params.NormalizedAlias
		item.Source = params.Source
	})
}

func (r CanonicalModelRepository) DeleteAlias(ctx context.Context, canonicalID uuid.UUID, aliasID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&CanonicalModelAlias{CanonicalModelID: canonicalID, ID: aliasID}).Delete(&CanonicalModelAlias{}).Error; err != nil {
		return fmt.Errorf("delete canonical model alias: %w", err)
	}
	return nil
}

func (r CanonicalModelRepository) Archive(ctx context.Context, canonicalID uuid.UUID) (CanonicalModel, error) {
	model, err := r.GetByID(ctx, canonicalID)
	if err != nil {
		return CanonicalModel{}, err
	}
	model.Status = "archived"
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("archive canonical model: %w", err)
	}
	return model, nil
}

func (r CanonicalModelRepository) MarkManualCreated(ctx context.Context, canonicalID uuid.UUID, params UpsertCanonicalModelParams) (CanonicalModel, error) {
	model, err := r.GetByID(ctx, canonicalID)
	if err != nil {
		return CanonicalModel{}, err
	}
	capabilities := canonicalModelCapabilities(model)
	capabilities["source"] = "manual_create"
	capabilities["manual_created"] = true
	capabilities["auto_created"] = false
	delete(capabilities, "raw_name")
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return CanonicalModel{}, fmt.Errorf("mark canonical model manual created: %w", err)
	}
	model.DisplayName = params.DisplayName
	model.Capabilities = encoded
	model.Status = stringDefault(params.Status, "active")
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return CanonicalModel{}, fmt.Errorf("mark canonical model manual created: %w", err)
	}
	return model, nil
}

func (r CanonicalModelRepository) Restore(ctx context.Context, canonicalID uuid.UUID) (CanonicalModel, error) {
	model, err := r.GetByID(ctx, canonicalID)
	if err != nil {
		return CanonicalModel{}, err
	}
	if model.Status != "active" {
		model.Status = "active"
		if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
			return CanonicalModel{}, fmt.Errorf("restore canonical model: %w", err)
		}
	}
	return model, nil
}

func (r CanonicalModelRepository) ActiveSiteModelCount(ctx context.Context, canonicalID uuid.UUID) (int, error) {
	siteModels, err := NewSiteModelRepository(r.db).ListByCanonical(ctx, canonicalID)
	if err != nil {
		return 0, fmt.Errorf("count canonical model active site models: %w", err)
	}
	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return 0, fmt.Errorf("count canonical model active site models: %w", err)
	}
	activeSites := map[uuid.UUID]struct{}{}
	for _, site := range filterActiveSites(sites) {
		activeSites[site.ID] = struct{}{}
	}
	count := 0
	for _, siteModel := range siteModels {
		if siteModel.Status == "unavailable" {
			continue
		}
		if _, ok := activeSites[siteModel.SiteID]; !ok {
			continue
		}
		count++
	}
	return count, nil
}

func (r CanonicalModelRepository) ArchiveUnusedAutoCreated(ctx context.Context) (int64, error) {
	var models []CanonicalModel
	if err := r.db.WithContext(ctx).Find(&models).Error; err != nil {
		return 0, fmt.Errorf("archive unused auto-created canonical models: %w", err)
	}
	var siteModels []SiteModel
	if err := r.db.WithContext(ctx).Find(&siteModels).Error; err != nil {
		return 0, fmt.Errorf("archive unused auto-created canonical models: %w", err)
	}
	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return 0, fmt.Errorf("archive unused auto-created canonical models: %w", err)
	}
	activeSites := map[uuid.UUID]struct{}{}
	for _, site := range filterActiveSites(sites) {
		activeSites[site.ID] = struct{}{}
	}
	used := map[uuid.UUID]struct{}{}
	for _, siteModel := range siteModels {
		if siteModel.Status == "unavailable" {
			continue
		}
		if _, ok := activeSites[siteModel.SiteID]; !ok {
			continue
		}
		if siteModel.CanonicalID.Valid {
			used[siteModel.CanonicalID.UUID] = struct{}{}
		}
	}
	var archived int64
	for _, model := range models {
		if model.Status == "archived" {
			continue
		}
		if _, ok := used[model.ID]; ok {
			continue
		}
		if canonicalModelAutoCreated(model) {
			model.Status = "archived"
			result := r.db.WithContext(ctx).Save(&model)
			if result.Error != nil {
				return archived, fmt.Errorf("archive unused auto-created canonical models: %w", result.Error)
			}
			archived += result.RowsAffected
		}
	}
	return archived, nil
}

func canonicalModelCapabilities(model CanonicalModel) map[string]any {
	capabilities := map[string]any{}
	if len(model.Capabilities) == 0 {
		return capabilities
	}
	_ = json.Unmarshal(model.Capabilities, &capabilities)
	return capabilities
}

func mergeCanonicalModelCapabilities(existing CanonicalModel, next JSON) JSON {
	capabilities := map[string]any{}
	if len(next) > 0 {
		_ = json.Unmarshal(next, &capabilities)
	}
	existingCapabilities := canonicalModelCapabilities(existing)
	if CanonicalModelManualCreated(existing) {
		capabilities["source"] = "manual_create"
		capabilities["manual_created"] = true
		capabilities["auto_created"] = false
	} else if existingCapabilities["source"] != nil && capabilities["source"] == nil {
		capabilities["source"] = existingCapabilities["source"]
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil || len(encoded) == 0 {
		return jsonDefault(next, "{}")
	}
	return encoded
}

func canonicalModelAutoCreated(model CanonicalModel) bool {
	capabilities := canonicalModelCapabilities(model)
	autoCreated := capabilities["auto_created"] == true || capabilities["auto_created"] == "true"
	source, _ := capabilities["source"].(string)
	return autoCreated && source == "catalog_match"
}

func CanonicalModelManualCreated(model CanonicalModel) bool {
	capabilities := canonicalModelCapabilities(model)
	manualCreated := capabilities["manual_created"] == true || capabilities["manual_created"] == "true"
	source, _ := capabilities["source"].(string)
	return manualCreated || source == "manual_create"
}

func (r CanonicalModelRepository) Matrix(ctx context.Context, canonicalID uuid.UUID) ([]CanonicalModelMatrixRow, error) {
	siteModels, err := NewSiteModelRepository(r.db).ListByCanonical(ctx, canonicalID)
	if err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}
	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}
	sitesByID := map[uuid.UUID]Site{}
	for _, site := range filterActiveSites(sites) {
		sitesByID[site.ID] = site
	}
	var apiKeyModels []SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Find(&apiKeyModels).Error; err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}
	var credentials []SiteCredential
	if err := r.db.WithContext(ctx).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}
	var keyStates []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Find(&keyStates).Error; err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}
	cooldowns, err := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}
	credentialsByID := make(map[uuid.UUID]SiteCredential, len(credentials))
	credentialsBySiteID := map[uuid.UUID][]SiteCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
		credentialsBySiteID[credential.SiteID] = append(credentialsBySiteID[credential.SiteID], credential)
	}
	statesByCredentialID := make(map[uuid.UUID]SiteAPIKeyState, len(keyStates))
	for _, state := range keyStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	var pricings []SiteModelPricing
	if err := r.db.WithContext(ctx).Where(&SiteModelPricing{Available: true}).Find(&pricings).Error; err != nil {
		return nil, fmt.Errorf("load canonical model matrix: %w", err)
	}

	items := make([]CanonicalModelMatrixRow, 0)
	for _, model := range siteModels {
		if model.Status == "unavailable" {
			continue
		}
		site := sitesByID[model.SiteID]
		if site.ID == uuid.Nil {
			continue
		}
		totalCredentialIDs := map[uuid.UUID]struct{}{}
		availableCredentialIDs := map[uuid.UUID]struct{}{}
		for _, apiKeyModel := range apiKeyModels {
			if apiKeyModel.SiteModelID.Valid && apiKeyModel.SiteModelID.UUID == model.ID {
				totalCredentialIDs[apiKeyModel.SiteCredentialID] = struct{}{}
				credential, ok := credentialsByID[apiKeyModel.SiteCredentialID]
				if !ok || !apiKeyModel.Available || !apiKeyModel.Enabled || !isModelBoundCredentialType(credential.CredentialType) || !credentialUsable(credential) {
					continue
				}
				if !credentialStateUsableForCredentialAt(credential, statesByCredentialID[credential.ID], time.Now()) || credentialCooling(cooldowns, site.ID, model.ID, credential.ID) {
					continue
				}
				availableCredentialIDs[credential.ID] = struct{}{}
			}
		}
		total := len(totalCredentialIDs)
		available := len(availableCredentialIDs)
		if !isNewAPISite(site.SiteType) && total == 0 {
			available = siteFallbackCredentialCount(site.ID, credentialsBySiteID[site.ID], statesByCredentialID, cooldowns)
			total = available
		}
		matchedPricing := false
		for _, pricing := range pricings {
			if !pricing.SiteModelID.Valid || pricing.SiteModelID.UUID != model.ID {
				continue
			}
			matchedPricing = true
			items = append(items, canonicalMatrixRow(site, model, total, available, pricing))
		}
		if !matchedPricing {
			items = append(items, canonicalMatrixRow(site, model, total, available, SiteModelPricing{}))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SiteName != items[j].SiteName {
			return items[i].SiteName < items[j].SiteName
		}
		if items[i].UpstreamModelName != items[j].UpstreamModelName {
			return items[i].UpstreamModelName < items[j].UpstreamModelName
		}
		return items[i].GroupName.String < items[j].GroupName.String
	})
	return items, nil
}

func canonicalMatrixRow(site Site, model SiteModel, total int, available int, pricing SiteModelPricing) CanonicalModelMatrixRow {
	return CanonicalModelMatrixRow{
		SiteID:               site.ID,
		SiteName:             site.Name,
		SiteSlug:             site.Slug,
		SiteType:             site.SiteType,
		SiteEnabled:          site.Enabled,
		SiteModelID:          model.ID,
		UpstreamModelName:    model.UpstreamName,
		DisplayName:          model.DisplayName,
		ModelStatus:          model.Status,
		MatchSource:          model.MatchSource,
		MatchConfidence:      model.MatchConfidence,
		MatchedAt:            model.MatchedAt,
		APIKeyCount:          total,
		AvailableAPIKeyCount: available,
		GroupName:            sql.NullString{String: pricing.GroupName, Valid: pricing.ID != uuid.Nil},
		Currency:             sql.NullString{String: pricing.Currency, Valid: pricing.ID != uuid.Nil},
		InputValue:           pricing.InputValue,
		OutputValue:          pricing.OutputValue,
		PerRequestValue:      pricing.PerRequestValue,
		BillingType:          sql.NullString{String: pricing.BillingType, Valid: pricing.ID != uuid.Nil},
		PricingAvailable:     sql.NullBool{Bool: pricing.Available, Valid: pricing.ID != uuid.Nil},
		PricingLastSyncedAt:  pricing.LastSyncedAt,
		SiteModelCreatedAt:   model.CreatedAt,
		SiteModelUpdatedAt:   model.UpdatedAt,
	}
}
