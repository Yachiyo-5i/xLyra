package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/store"
)

type Service struct {
	db           *store.Store
	capabilities *modelcapabilities.Service
}

type CanonicalModelItem struct {
	Model   store.CanonicalModelWithStats
	Aliases []store.CanonicalModelAlias
}

type UpsertCanonicalModelInput struct {
	ModelKey     string
	DisplayName  string
	Provider     string
	Category     string
	Capabilities map[string]any
	Status       string
}

type Matrix struct {
	Model store.CanonicalModel
	Rows  []store.CanonicalModelMatrixRow
}

var modelKeySeparatorPattern = regexp.MustCompile(`[^a-z0-9.]+`)
var canonicalFamilyTokens = map[string]struct{}{
	"bge":           {},
	"claude":        {},
	"cohere":        {},
	"dall":          {},
	"deepseek":      {},
	"embedding":     {},
	"flux":          {},
	"gemma":         {},
	"gemini":        {},
	"glm":           {},
	"gpt":           {},
	"grok":          {},
	"hermes":        {},
	"jina":          {},
	"kimi":          {},
	"lyria":         {},
	"mimo":          {},
	"minimax":       {},
	"moonshot":      {},
	"moonshotai":    {},
	"nanobananapro": {},
	"o1":            {},
	"o3":            {},
	"o4":            {},
	"perplexity":    {},
	"qwen":          {},
	"qwen2":         {},
	"qwen3":         {},
	"rerank":        {},
	"sora":          {},
	"text":          {},
	"trinity":       {},
	"tts":           {},
	"veo":           {},
	"whisper":       {},
}
var canonicalSuffixNoiseTokens = map[string]struct{}{
	"business":  {},
	"inference": {},
	"search":    {},
}

type providerModelFamilyRule struct {
	provider string
	families []string
}

var providerModelFamilyRules = []providerModelFamilyRule{
	{provider: "openai", families: []string{"gpt", "o1", "o3", "o4", "dall", "sora"}},
	{provider: "anthropic", families: []string{"claude"}},
	{provider: "google", families: []string{"gemini", "gemma", "deep-research", "lyria", "nano-banana", "veo"}},
	{provider: "deepseek", families: []string{"deepseek"}},
	{provider: "xai", families: []string{"grok"}},
	{provider: "qwen", families: []string{"qwen", "qwen2", "qwen3"}},
	{provider: "zhipuai", families: []string{"glm"}},
	{provider: "moonshotai-cn", families: []string{"kimi", "moonshot", "moonshotai", "k3"}},
	{provider: "xiaomi", families: []string{"mimo"}},
	{provider: "minimax", families: []string{"minimax"}},
	{provider: "nvidia", families: []string{"nvidia", "nemotron"}},
	{provider: "bytedance", families: []string{"bytedance", "doubao", "seedance", "seedream"}},
	{provider: "vidu", families: []string{"vidu", "viduq"}},
	{provider: "stepfun", families: []string{"step", "stepaudio"}},
	{provider: "alibaba", families: []string{"funaudiollm", "tongyi", "wan", "happyhorse"}},
	{provider: "kuaishou", families: []string{"kling", "kwai", "kolors"}},
	{provider: "baai", families: []string{"bge"}},
	{provider: "flux", families: []string{"flux"}},
	{provider: "hunyuan", families: []string{"hy", "hunyuan", "tencent-hunyuan"}},
	{provider: "sensenova", families: []string{"sensenova"}},
	{provider: "perplexity", families: []string{"perplexity"}},
}

var existingBrandProviders = map[string]struct{}{
	"openai":        {},
	"anthropic":     {},
	"google":        {},
	"deepseek":      {},
	"xai":           {},
	"qwen":          {},
	"zhipuai":       {},
	"moonshotai-cn": {},
	"minimax":       {},
	"xiaomi":        {},
	"nvidia":        {},
	"bytedance":     {},
	"vidu":          {},
	"stepfun":       {},
	"alibaba":       {},
	"kuaishou":      {},
	"baai":          {},
	"flux":          {},
	"hunyuan":       {},
	"sensenova":     {},
}

func NewService(db *store.Store, confFiles ...*config.ConfigFile) *Service {
	var confFile *config.ConfigFile
	if len(confFiles) > 0 {
		confFile = confFiles[0]
	}
	client, _ := httpclient.NewManager(confFile).Client(httpclient.DefaultProfile())
	return &Service{db: db, capabilities: modelcapabilities.NewWithConfig(modelcapabilities.Config{HTTPClient: client})}
}

func NormalizeModelKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "models/")
	value = strings.TrimPrefix(value, "model/")
	value = strings.TrimPrefix(value, "openai/")
	value = strings.TrimPrefix(value, "anthropic/")
	value = strings.TrimPrefix(value, "google/")
	value = strings.TrimPrefix(value, "gemini/")
	value = strings.TrimPrefix(value, "deepseek/")
	value = strings.TrimPrefix(value, "xai/")
	value = strings.TrimPrefix(value, "moonshotai/")
	value = strings.TrimPrefix(value, "nvidia/")
	value = strings.ReplaceAll(value, "+", " plus ")
	value = modelKeySeparatorPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")

	return value
}

func NormalizeModelKeyPrefix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "models/")
	value = strings.TrimPrefix(value, "model/")
	value = strings.TrimPrefix(value, "openai/")
	value = strings.TrimPrefix(value, "anthropic/")
	value = strings.TrimPrefix(value, "google/")
	value = strings.TrimPrefix(value, "gemini/")
	value = strings.TrimPrefix(value, "deepseek/")
	value = strings.TrimPrefix(value, "xai/")
	value = strings.TrimPrefix(value, "moonshotai/")
	value = strings.TrimPrefix(value, "nvidia/")
	value = strings.ReplaceAll(value, "+", " plus ")
	value = modelKeySeparatorPattern.ReplaceAllString(value, "-")
	value = strings.TrimLeft(value, "-.")

	return value
}

func CanonicalModelKeyFromUpstream(value string) string {
	normalized := NormalizeModelKey(value)
	if normalized == "" {
		return ""
	}

	tokens := strings.Split(normalized, "-")
	start := 0
	found := false
	for index, token := range tokens {
		if _, ok := canonicalFamilyTokens[token]; ok {
			start = index
			found = true
			break
		}
	}

	if !found {
		return normalized
	}

	tokens = tokens[start:]
	tokens = trimCanonicalSuffixNoise(tokens)
	return strings.Join(tokens, "-")
}

func InferProvider(modelKey string) string {
	modelKey = CanonicalModelKeyFromUpstream(modelKey)
	for _, rule := range providerModelFamilyRules {
		for _, family := range rule.families {
			if hasModelFamilyPrefix(modelKey, family) {
				return rule.provider
			}
		}
	}
	return "unknown"
}

func hasModelFamilyPrefix(modelKey string, family string) bool {
	modelKey = strings.ToLower(strings.TrimSpace(modelKey))
	family = strings.ToLower(strings.TrimSpace(family))
	if modelKey == "" || family == "" {
		return false
	}
	if modelKey == family {
		return true
	}
	if !strings.HasPrefix(modelKey, family) {
		return false
	}
	next := modelKey[len(family)]
	return next == '-' || next == '_' || next == '.' || (next >= '0' && next <= '9')
}

func InferCategory(modelKey string) string {
	modelKey = CanonicalModelKeyFromUpstream(modelKey)
	switch {
	case strings.Contains(modelKey, "embedding"), strings.Contains(modelKey, "embed"):
		return "embedding"
	case strings.Contains(modelKey, "rerank"):
		return "rerank"
	case strings.Contains(modelKey, "whisper"), strings.Contains(modelKey, "tts"), strings.Contains(modelKey, "audio"):
		return "audio"
	case modelcapabilities.IsImageGenerationModel(modelKey):
		return "image"
	case strings.HasPrefix(modelKey, "veo-"), strings.HasPrefix(modelKey, "sora-"), strings.Contains(modelKey, "video"), strings.Contains(modelKey, "vedio"):
		return "video"
	default:
		return "chat"
	}
}

func (s *Service) List(ctx context.Context) ([]CanonicalModelItem, error) {
	repo := store.NewCanonicalModelRepository(s.db.DB())
	models, err := repo.List(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]CanonicalModelItem, 0, len(models))
	for _, model := range models {
		aliases, _ := repo.ListAliases(ctx, model.ID)
		items = append(items, CanonicalModelItem{
			Model:   model,
			Aliases: aliases,
		})
	}

	return items, nil
}

func (s *Service) Create(ctx context.Context, input UpsertCanonicalModelInput) (store.CanonicalModel, error) {
	params, err := canonicalModelParams(input)
	if err != nil {
		return store.CanonicalModel{}, err
	}

	if input.DisplayName == "" && s.capabilities != nil {
		if name := s.capabilities.OfficialName(ctx, params.Provider, params.ModelKey); name != "" {
			params.DisplayName = name
		}
	}

	repo := store.NewCanonicalModelRepository(s.db.DB())
	model, err := repo.Upsert(ctx, params)
	if err != nil {
		return store.CanonicalModel{}, err
	}
	return repo.MarkManualCreated(ctx, model.ID, params)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, input UpsertCanonicalModelInput) (store.CanonicalModel, error) {
	params, err := canonicalModelParams(input)
	if err != nil {
		return store.CanonicalModel{}, err
	}

	return store.NewCanonicalModelRepository(s.db.DB()).Update(ctx, store.UpdateCanonicalModelParams{
		ID:           id,
		ModelKey:     params.ModelKey,
		DisplayName:  params.DisplayName,
		Provider:     params.Provider,
		Category:     params.Category,
		Capabilities: params.Capabilities,
		Status:       params.Status,
	})
}

type UpdateCanonicalPricingInput struct {
	Reset             bool
	InputPrice        *float64
	OutputPrice       *float64
	CacheReadRatio    *float64
	CacheWriteRatio   *float64
	CacheWrite1hRatio *float64
}

func (s *Service) UpdatePricing(ctx context.Context, id uuid.UUID, input UpdateCanonicalPricingInput) (store.CanonicalModel, error) {
	if id == uuid.Nil {
		return store.CanonicalModel{}, fmt.Errorf("canonical model id is required")
	}
	params := store.UpdateCanonicalModelPricingParams{ID: id, Manual: !input.Reset}
	if !input.Reset {
		if input.InputPrice == nil || input.OutputPrice == nil {
			return store.CanonicalModel{}, fmt.Errorf("input_price and output_price are required")
		}
		params.InputPrice = nullFloat64FromPtr(input.InputPrice)
		params.OutputPrice = nullFloat64FromPtr(input.OutputPrice)
		params.CacheReadRatio = nullFloat64FromPtr(input.CacheReadRatio)
		params.CacheWriteRatio = nullFloat64FromPtr(input.CacheWriteRatio)
		params.CacheWrite1hRatio = nullFloat64FromPtr(input.CacheWrite1hRatio)
	}
	return store.NewCanonicalModelRepository(s.db.DB()).UpdatePricing(ctx, params)
}

func nullFloat64FromPtr(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func (s *Service) AddAlias(ctx context.Context, canonicalID uuid.UUID, alias string) (store.CanonicalModelAlias, error) {
	alias = strings.TrimSpace(alias)
	normalized := NormalizeModelKey(alias)
	if canonicalID == uuid.Nil {
		return store.CanonicalModelAlias{}, fmt.Errorf("canonical model id is required")
	}
	if alias == "" || normalized == "" {
		return store.CanonicalModelAlias{}, fmt.Errorf("alias is required")
	}

	return store.NewCanonicalModelRepository(s.db.DB()).CreateAlias(ctx, store.CreateCanonicalModelAliasParams{
		CanonicalModelID: canonicalID,
		Alias:            alias,
		NormalizedAlias:  normalized,
		Source:           "manual",
	})
}

func (s *Service) DeleteAlias(ctx context.Context, canonicalID uuid.UUID, aliasID uuid.UUID) error {
	if canonicalID == uuid.Nil || aliasID == uuid.Nil {
		return fmt.Errorf("canonical model id and alias id are required")
	}

	return store.NewCanonicalModelRepository(s.db.DB()).DeleteAlias(ctx, canonicalID, aliasID)
}

func (s *Service) BindSiteModel(ctx context.Context, siteModelID uuid.UUID, canonicalID *uuid.UUID) (store.SiteModel, error) {
	var updated store.SiteModel
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		repo := store.NewSiteModelRepository(tx)
		current, err := repo.GetByID(ctx, siteModelID)
		if err != nil {
			return err
		}
		canonicalIDs := []uuid.UUID{}
		if current.CanonicalID.Valid {
			canonicalIDs = append(canonicalIDs, current.CanonicalID.UUID)
		}

		if canonicalID == nil || *canonicalID == uuid.Nil {
			updated, err = repo.UpdateCanonical(ctx, siteModelID, nil, "unmatched", 0, nil)
			if err != nil {
				return err
			}
			return ReconcileCanonicalCategories(ctx, tx, canonicalIDs...)
		}

		canonicalRepo := store.NewCanonicalModelRepository(tx)
		if _, err := canonicalRepo.Restore(ctx, *canonicalID); err != nil {
			return err
		}
		canonicalIDs = append(canonicalIDs, *canonicalID)
		updated, err = repo.UpdateCanonical(ctx, siteModelID, *canonicalID, "manual", 100, time.Now())
		if err != nil {
			return err
		}
		return ReconcileCanonicalCategories(ctx, tx, canonicalIDs...)
	})
	if err != nil {
		return store.SiteModel{}, err
	}
	return updated, nil
}

func (s *Service) MatchSiteModels(ctx context.Context, siteID uuid.UUID) ([]store.SiteModel, error) {
	siteModelRepo := store.NewSiteModelRepository(s.db.DB())
	canonicalRepo := store.NewCanonicalModelRepository(s.db.DB())

	models, err := siteModelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}

	type modelMatch struct {
		model      store.SiteModel
		canonical  store.CanonicalModel
		confidence int
	}
	matches := make([]modelMatch, 0, len(models))
	for _, model := range models {
		if model.MatchSource == "manual" && model.CanonicalID.Valid {
			matches = append(matches, modelMatch{model: model, canonical: store.CanonicalModel{ID: model.CanonicalID.UUID}})
			continue
		}

		canonical, confidence, err := s.matchOrCreateCanonical(ctx, canonicalRepo, model)
		if err != nil {
			return nil, err
		}

		matches = append(matches, modelMatch{model: model, canonical: canonical, confidence: confidence})
	}

	matched := make([]store.SiteModel, 0, len(matches))
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		siteModelRepo := store.NewSiteModelRepository(tx)
		canonicalRepo := store.NewCanonicalModelRepository(tx)
		canonicalIDs := make([]uuid.UUID, 0, len(matches))
		for _, match := range matches {
			canonicalIDs = append(canonicalIDs, match.canonical.ID)
			if match.model.MatchSource == "manual" && match.model.CanonicalID.Valid {
				matched = append(matched, match.model)
				continue
			}
			updated, err := siteModelRepo.UpdateCanonical(ctx, match.model.ID, match.canonical.ID, "auto", match.confidence, time.Now())
			if err != nil {
				return err
			}
			matched = append(matched, updated)
		}
		if _, err := canonicalRepo.ArchiveUnusedAutoCreated(ctx); err != nil {
			return err
		}
		return ReconcileCanonicalCategories(ctx, tx, canonicalIDs...)
	})
	if err != nil {
		return nil, err
	}

	return matched, nil
}

func ReconcileCategories(ctx context.Context, db store.Tx) error {
	canonicalModels, err := store.NewCanonicalModelRepository(db).ListAll(ctx)
	if err != nil {
		return err
	}
	siteModels, err := store.NewSiteModelRepository(db).ListAll(ctx)
	if err != nil {
		return err
	}
	modelsByCanonicalID := map[uuid.UUID][]store.SiteModel{}
	for _, siteModel := range siteModels {
		if siteModel.CanonicalID.Valid {
			modelsByCanonicalID[siteModel.CanonicalID.UUID] = append(modelsByCanonicalID[siteModel.CanonicalID.UUID], siteModel)
		}
	}
	canonicalRepo := store.NewCanonicalModelRepository(db)
	for _, canonical := range canonicalModels {
		siteModels, ok := modelsByCanonicalID[canonical.ID]
		if !ok || canonical.Status != "active" {
			continue
		}
		category := categoryFromSiteModelProtocols(canonical.ModelKey, siteModels)
		if _, err := canonicalRepo.UpdateCategory(ctx, canonical.ID, category); err != nil {
			return err
		}
	}
	return nil
}

func ReconcileProviders(ctx context.Context, db store.Tx) (int, error) {
	canonicalModels, err := store.NewCanonicalModelRepository(db).ListAll(ctx)
	if err != nil {
		return 0, err
	}

	canonicalRepo := store.NewCanonicalModelRepository(db)
	updated := 0
	for _, canonical := range canonicalModels {
		if canonical.Status != "active" || strings.TrimSpace(canonical.Provider) != "unknown" {
			continue
		}
		provider := InferProvider(canonical.ModelKey)
		if _, ok := existingBrandProviders[provider]; !ok {
			continue
		}
		if _, err := canonicalRepo.UpdateProvider(ctx, canonical.ID, provider); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func ReconcileCanonicalCategories(ctx context.Context, db store.Tx, canonicalIDs ...uuid.UUID) error {
	canonicalRepo := store.NewCanonicalModelRepository(db)
	siteModelRepo := store.NewSiteModelRepository(db)
	seen := map[uuid.UUID]struct{}{}
	for _, canonicalID := range canonicalIDs {
		if canonicalID == uuid.Nil {
			continue
		}
		if _, ok := seen[canonicalID]; ok {
			continue
		}
		seen[canonicalID] = struct{}{}

		canonical, err := canonicalRepo.GetByID(ctx, canonicalID)
		if err != nil {
			return err
		}
		if canonical.Status != "active" {
			continue
		}
		siteModels, err := siteModelRepo.ListByCanonical(ctx, canonicalID)
		if err != nil {
			return err
		}
		category := categoryFromSiteModelProtocols(canonical.ModelKey, siteModels)
		if _, err := canonicalRepo.UpdateCategory(ctx, canonicalID, category); err != nil {
			return err
		}
	}
	return nil
}

func categoryFromSiteModelProtocols(modelKey string, models []store.SiteModel) string {
	endpointTypes := map[string]struct{}{}
	for _, model := range models {
		capabilities := map[string]any{}
		if json.Unmarshal(model.Capabilities, &capabilities) != nil {
			continue
		}
		values, ok := capabilities["supported_endpoint_types"]
		if !ok {
			continue
		}
		for _, endpointType := range endpointTypeValues(values) {
			endpointTypes[endpointType] = struct{}{}
		}
	}
	if _, ok := endpointTypes["openai-image"]; ok {
		return "image"
	}
	if _, ok := endpointTypes["openai-embedding"]; ok {
		return "embedding"
	}
	if _, ok := endpointTypes["openai-audio-speech"]; ok {
		return "audio"
	}
	return InferCategory(modelKey)
}

func endpointTypeValues(value any) []string {
	items := []string{}
	appendValue := func(item string) {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			items = append(items, item)
		}
	}
	switch values := value.(type) {
	case []string:
		for _, item := range values {
			appendValue(item)
		}
	case []any:
		for _, item := range values {
			if text, ok := item.(string); ok {
				appendValue(text)
			}
		}
	}
	return items
}

func (s *Service) Archive(ctx context.Context, canonicalID uuid.UUID) (store.CanonicalModel, error) {
	if canonicalID == uuid.Nil {
		return store.CanonicalModel{}, fmt.Errorf("canonical model id is required")
	}
	repo := store.NewCanonicalModelRepository(s.db.DB())
	model, err := repo.GetByID(ctx, canonicalID)
	if err != nil {
		return store.CanonicalModel{}, err
	}
	if !store.CanonicalModelManualCreated(model) {
		return store.CanonicalModel{}, fmt.Errorf("only manually created canonical models can be archived manually")
	}
	count, err := repo.ActiveSiteModelCount(ctx, canonicalID)
	if err != nil {
		return store.CanonicalModel{}, err
	}
	if count > 0 {
		return store.CanonicalModel{}, fmt.Errorf("canonical model still has linked site models")
	}
	return repo.Archive(ctx, canonicalID)
}

func (s *Service) Matrix(ctx context.Context, canonicalID uuid.UUID) (Matrix, error) {
	repo := store.NewCanonicalModelRepository(s.db.DB())
	model, err := repo.GetByID(ctx, canonicalID)
	if err != nil {
		return Matrix{}, err
	}

	rows, err := repo.Matrix(ctx, canonicalID)
	if err != nil {
		return Matrix{}, err
	}

	return Matrix{
		Model: model,
		Rows:  rows,
	}, nil
}

func (s *Service) matchOrCreateCanonical(ctx context.Context, repo store.CanonicalModelRepository, model store.SiteModel) (store.CanonicalModel, int, error) {
	normalized := CanonicalModelKeyFromUpstream(model.UpstreamName)
	rawNormalized := NormalizeModelKey(model.UpstreamName)
	if normalized == "" {
		normalized = CanonicalModelKeyFromUpstream(model.DisplayName)
		rawNormalized = NormalizeModelKey(model.DisplayName)
	}
	if normalized == "" {
		return store.CanonicalModel{}, 0, fmt.Errorf("site model %s has no usable model name", model.ID)
	}

	if canonical, err := repo.GetByKey(ctx, normalized); err == nil {
		if canonical.Status == "archived" {
			restored, err := repo.Restore(ctx, canonical.ID)
			if err != nil {
				return store.CanonicalModel{}, 0, err
			}
			canonical = restored
		}
		s.syncOfficialDisplayName(ctx, repo, &canonical)
		return canonical, 100, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.CanonicalModel{}, 0, err
	}

	if canonical, err := repo.GetByNormalizedAlias(ctx, normalized); err == nil {
		if canonical.Status == "archived" {
			restored, err := repo.Restore(ctx, canonical.ID)
			if err != nil {
				return store.CanonicalModel{}, 0, err
			}
			canonical = restored
		}
		s.syncOfficialDisplayName(ctx, repo, &canonical)
		return canonical, 95, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.CanonicalModel{}, 0, err
	}

	if rawNormalized != "" && rawNormalized != normalized {
		if canonical, err := repo.GetByNormalizedAlias(ctx, rawNormalized); err == nil {
			if canonical.Status == "archived" {
				restored, err := repo.Restore(ctx, canonical.ID)
				if err != nil {
					return store.CanonicalModel{}, 0, err
				}
				canonical = restored
			}
			s.syncOfficialDisplayName(ctx, repo, &canonical)
			return canonical, 95, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return store.CanonicalModel{}, 0, err
		}
	}

	capabilities := map[string]any{
		"auto_created": true,
		"source":       "catalog_match",
		"raw_name":     model.UpstreamName,
	}
	encoded, _ := json.Marshal(capabilities)

	provider := InferProvider(normalized)
	displayName := normalized
	if s.capabilities != nil {
		if name := s.capabilities.OfficialName(ctx, provider, normalized); name != "" {
			displayName = name
		}
	}

	canonical, err := repo.Upsert(ctx, store.UpsertCanonicalModelParams{
		ModelKey:     normalized,
		DisplayName:  displayName,
		Provider:     provider,
		Category:     InferCategory(normalized),
		Capabilities: encoded,
		Status:       "active",
	})
	if err != nil {
		return store.CanonicalModel{}, 0, err
	}

	return canonical, 80, nil
}

func (s *Service) syncOfficialDisplayName(ctx context.Context, repo store.CanonicalModelRepository, canonical *store.CanonicalModel) {
	if s.capabilities == nil {
		return
	}
	if canonical.DisplayName != canonical.ModelKey {
		return
	}
	name := s.capabilities.OfficialName(ctx, canonical.Provider, canonical.ModelKey)
	if name == "" || name == canonical.DisplayName {
		return
	}
	updated, err := repo.Update(ctx, store.UpdateCanonicalModelParams{
		ID:                     canonical.ID,
		ModelKey:               canonical.ModelKey,
		DisplayName:            name,
		Provider:               canonical.Provider,
		Category:               canonical.Category,
		Capabilities:           canonical.Capabilities,
		Status:                 canonical.Status,
		InputPrice:             canonical.InputPrice,
		OutputPrice:            canonical.OutputPrice,
		CacheReadRatio:         canonical.CacheReadRatio,
		CacheWriteRatio:        canonical.CacheWriteRatio,
		CacheWrite1hRatio:      canonical.CacheWrite1hRatio,
		SupportedEndpointTypes: canonical.SupportedEndpointTypes,
		Modalities:             canonical.Modalities,
		ContextWindow:          canonical.ContextWindow,
		MaxOutputTokens:        canonical.MaxOutputTokens,
		PricingSource:          canonical.PricingSource,
		LastPricingSyncedAt:    canonical.LastPricingSyncedAt,
	})
	if err == nil {
		*canonical = store.CanonicalModel{
			ID:                     updated.ID,
			ModelKey:               updated.ModelKey,
			DisplayName:            updated.DisplayName,
			Provider:               updated.Provider,
			Category:               updated.Category,
			Capabilities:           updated.Capabilities,
			Status:                 updated.Status,
			InputPrice:             updated.InputPrice,
			OutputPrice:            updated.OutputPrice,
			CacheReadRatio:         updated.CacheReadRatio,
			CacheWriteRatio:        updated.CacheWriteRatio,
			CacheWrite1hRatio:      updated.CacheWrite1hRatio,
			SupportedEndpointTypes: updated.SupportedEndpointTypes,
			Modalities:             updated.Modalities,
			ContextWindow:          updated.ContextWindow,
			MaxOutputTokens:        updated.MaxOutputTokens,
			PricingSource:          updated.PricingSource,
			LastPricingSyncedAt:    updated.LastPricingSyncedAt,
			CreatedAt:              updated.CreatedAt,
			UpdatedAt:              updated.UpdatedAt,
		}
	}
}

func canonicalModelParams(input UpsertCanonicalModelInput) (store.UpsertCanonicalModelParams, error) {
	modelKey := CanonicalModelKeyFromUpstream(input.ModelKey)
	if modelKey == "" {
		return store.UpsertCanonicalModelParams{}, fmt.Errorf("model_key is required")
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		displayName = modelKey
	}

	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = InferProvider(modelKey)
	}

	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = InferCategory(modelKey)
	}

	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}

	capabilities := []byte(`{}`)
	if len(input.Capabilities) > 0 {
		encoded, err := json.Marshal(input.Capabilities)
		if err != nil {
			return store.UpsertCanonicalModelParams{}, fmt.Errorf("marshal canonical model capabilities: %w", err)
		}
		capabilities = encoded
	}

	return store.UpsertCanonicalModelParams{
		ModelKey:     modelKey,
		DisplayName:  displayName,
		Provider:     provider,
		Category:     category,
		Capabilities: capabilities,
		Status:       status,
	}, nil
}

func displayNameFromModel(model store.SiteModel) string {
	if text := strings.TrimSpace(model.DisplayName); text != "" {
		return text
	}
	return strings.TrimSpace(model.UpstreamName)
}

func trimCanonicalSuffixNoise(tokens []string) []string {
	end := len(tokens)
	for end > 1 {
		if _, ok := canonicalSuffixNoiseTokens[tokens[end-1]]; !ok {
			break
		}
		end--
	}
	return tokens[:end]
}
