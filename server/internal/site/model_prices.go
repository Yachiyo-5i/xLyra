package site

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

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
)

var (
	ErrModelPriceReadonly          = errors.New("model price is readonly")
	ErrModelPriceInvalid           = errors.New("model price is invalid")
	ErrModelPriceCanonicalMismatch = errors.New("model price canonical mismatch")
	ErrModelPriceNoCanonical       = errors.New("model has no canonical pricing to reset to")
)

type ModelPriceFilters struct {
	Q                string
	SiteID           uuid.UUID
	SiteType         string
	Provider         string
	ModelKey         string
	CanonicalModelID uuid.UUID
	BillingType      string
	PricingStatus    string
}

type ModelPriceItem struct {
	Site               store.Site
	Model              store.SiteModel
	Canonical          *store.CanonicalModel
	Pricing            *store.SiteModelPricing
	CredentialPricings []CredentialModelPricing
	Editable           bool
	EditReason         string
	PricingStatus      string
}

type ModelPriceList struct {
	Items        []ModelPriceItem
	Count        int
	PricedCount  int
	MissingCount int
	ManualCount  int
}

type ModelPriceInput struct {
	GroupName               string
	BillingType             string
	Currency                string
	InputValue              *float64
	OutputValue             *float64
	CacheInputValue         *float64
	CreateCacheInputValue   *float64
	CreateCache1hInputValue *float64
	AudioOutputValue        *float64
	PerRequestValue         *float64
	GroupRatio              *float64
	ManualNote              string
}

type BulkModelPriceInput struct {
	CanonicalModelID uuid.UUID
	SiteModelIDs     []uuid.UUID
	ModelPriceInput
}

type BulkModelPriceResult struct {
	Items        []ModelPriceItem
	Count        int
	CreatedCount int
	UpdatedCount int
}

func (s *Service) ListModelPrices(ctx context.Context, filters ModelPriceFilters) (ModelPriceList, error) {
	siteRepo := store.NewSiteRepository(s.db.DB())
	modelRepo := store.NewSiteModelRepository(s.db.DB())
	pricingRepo := store.NewSiteModelPricingRepository(s.db.DB())
	canonicalRepo := store.NewCanonicalModelRepository(s.db.DB())

	sites, err := siteRepo.List(ctx)
	if err != nil {
		return ModelPriceList{}, err
	}
	models, err := modelRepo.ListAll(ctx)
	if err != nil {
		return ModelPriceList{}, err
	}
	pricings, err := pricingRepo.ListAll(ctx)
	if err != nil {
		return ModelPriceList{}, err
	}
	canonicalItems, err := canonicalRepo.List(ctx)
	if err != nil {
		return ModelPriceList{}, err
	}
	credentialPricings, err := s.credentialModelPricings(ctx, sites, uuid.Nil)
	if err != nil {
		return ModelPriceList{}, err
	}
	credentialPricingsByModelID := make(map[uuid.UUID][]CredentialModelPricing)
	for _, item := range credentialPricings {
		credentialPricingsByModelID[item.SiteModelID] = append(credentialPricingsByModelID[item.SiteModelID], item)
	}

	sitesByID := make(map[uuid.UUID]store.Site, len(sites))
	for _, site := range sites {
		sitesByID[site.ID] = site
	}
	canonicalByID := make(map[uuid.UUID]store.CanonicalModel, len(canonicalItems))
	for _, item := range canonicalItems {
		canonicalByID[item.ID] = item.CanonicalModel
	}

	pricingBySiteModelID := map[uuid.UUID][]store.SiteModelPricing{}
	pricingBySiteModelName := map[string][]store.SiteModelPricing{}
	for _, pricing := range pricings {
		if !pricing.Available {
			continue
		}
		if pricing.SiteModelID.Valid {
			pricingBySiteModelID[pricing.SiteModelID.UUID] = append(pricingBySiteModelID[pricing.SiteModelID.UUID], pricing)
		}
		key := pricing.SiteID.String() + "\x1f" + strings.ToLower(strings.TrimSpace(pricing.ModelName))
		pricingBySiteModelName[key] = append(pricingBySiteModelName[key], pricing)
	}

	result := ModelPriceList{}
	for _, model := range models {
		if model.Status == "unavailable" {
			continue
		}
		site, ok := sitesByID[model.SiteID]
		if !ok {
			continue
		}
		var canonicalModel *store.CanonicalModel
		if model.CanonicalID.Valid {
			if canonical, ok := canonicalByID[model.CanonicalID.UUID]; ok {
				canonicalCopy := canonical
				canonicalModel = &canonicalCopy
			}
		}

		modelPricings := pricingBySiteModelID[model.ID]
		if len(modelPricings) == 0 {
			key := model.SiteID.String() + "\x1f" + strings.ToLower(strings.TrimSpace(model.UpstreamName))
			modelPricings = pricingBySiteModelName[key]
		}
		if len(modelPricings) == 0 {
			item := modelPriceItem(site, model, canonicalModel, nil)
			item.CredentialPricings = credentialPricingsByModelID[model.ID]
			result.add(item)
			continue
		}
		sort.SliceStable(modelPricings, func(i, j int) bool {
			return modelPricings[i].GroupName < modelPricings[j].GroupName
		})
		for _, pricing := range modelPricings {
			pricingCopy := pricing
			item := modelPriceItem(site, model, canonicalModel, &pricingCopy)
			item.CredentialPricings = credentialPricingsByModelID[model.ID]
			result.add(item)
		}
	}

	filtered := make([]ModelPriceItem, 0, len(result.Items))
	counts := ModelPriceList{}
	for _, item := range result.Items {
		if modelPriceMatchesFilters(item, filters) {
			filtered = append(filtered, item)
			counts.addCounts(item)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Site.Name != filtered[j].Site.Name {
			return filtered[i].Site.Name < filtered[j].Site.Name
		}
		leftModel := modelPriceDisplayKey(filtered[i])
		rightModel := modelPriceDisplayKey(filtered[j])
		if leftModel != rightModel {
			return leftModel < rightModel
		}
		return pricingGroupName(filtered[i].Pricing) < pricingGroupName(filtered[j].Pricing)
	})
	counts.Items = filtered
	counts.Count = len(filtered)
	return counts, nil
}

func (s *Service) UpsertModelPrice(ctx context.Context, siteModelID uuid.UUID, input ModelPriceInput) (ModelPriceItem, bool, error) {
	var result ModelPriceItem
	var created bool
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		item, itemCreated, err := s.upsertModelPriceTx(ctx, tx, siteModelID, input, uuid.Nil)
		if err != nil {
			return err
		}
		result = item
		created = itemCreated
		return nil
	})
	return result, created, err
}

func (s *Service) BulkUpsertModelPrices(ctx context.Context, input BulkModelPriceInput) (BulkModelPriceResult, error) {
	if input.CanonicalModelID == uuid.Nil {
		return BulkModelPriceResult{}, fmt.Errorf("%w: canonical_model_id is required", ErrModelPriceInvalid)
	}
	if len(input.SiteModelIDs) == 0 {
		return BulkModelPriceResult{}, fmt.Errorf("%w: site_model_ids is required", ErrModelPriceInvalid)
	}

	result := BulkModelPriceResult{}
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		for _, siteModelID := range input.SiteModelIDs {
			item, created, err := s.upsertModelPriceTx(ctx, tx, siteModelID, input.ModelPriceInput, input.CanonicalModelID)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
			if created {
				result.CreatedCount++
			} else {
				result.UpdatedCount++
			}
		}
		return nil
	})
	if err != nil {
		return BulkModelPriceResult{}, err
	}
	result.Count = len(result.Items)
	return result, nil
}

func (s *Service) upsertModelPriceTx(ctx context.Context, tx store.Tx, siteModelID uuid.UUID, input ModelPriceInput, requiredCanonicalID uuid.UUID) (ModelPriceItem, bool, error) {
	modelRepo := store.NewSiteModelRepository(tx)
	siteRepo := store.NewSiteRepository(tx)
	pricingRepo := store.NewSiteModelPricingRepository(tx)
	canonicalRepo := store.NewCanonicalModelRepository(tx)

	model, err := modelRepo.GetByID(ctx, siteModelID)
	if err != nil {
		return ModelPriceItem{}, false, err
	}
	site, err := siteRepo.GetByID(ctx, model.SiteID)
	if err != nil {
		return ModelPriceItem{}, false, err
	}
	if site.SiteType == "newapi" {
		return ModelPriceItem{}, false, fmt.Errorf("%w: newapi pricing is synced from upstream", ErrModelPriceReadonly)
	}
	if requiredCanonicalID != uuid.Nil {
		if !model.CanonicalID.Valid || model.CanonicalID.UUID != requiredCanonicalID {
			return ModelPriceItem{}, false, fmt.Errorf("%w: site_model_id %s does not belong to canonical_model_id %s", ErrModelPriceCanonicalMismatch, siteModelID, requiredCanonicalID)
		}
	}
	normalized, err := normalizeModelPriceInput(input)
	if err != nil {
		return ModelPriceItem{}, false, err
	}
	if normalizeSiteType(site.SiteType) == "openai" {
		normalized.GroupName = "default"
		normalized.GroupRatio = nil
	}

	existing, _ := pricingRepo.ListBySiteModelID(ctx, model.ID)
	created := true
	for _, pricing := range existing {
		if pricing.ModelName == model.UpstreamName && pricing.GroupName == normalized.GroupName {
			created = false
			break
		}
	}

	cacheRatio := any(nil)
	if normalized.InputValue != nil && normalized.CacheInputValue != nil && *normalized.InputValue > 0 {
		cacheRatio = *normalized.CacheInputValue / *normalized.InputValue
	}

	createCacheRatio := any(nil)
	if normalized.InputValue != nil && normalized.CreateCacheInputValue != nil && *normalized.InputValue > 0 {
		createCacheRatio = *normalized.CreateCacheInputValue / *normalized.InputValue
	}

	createCache1hRatio := any(nil)
	if normalized.InputValue != nil && normalized.CreateCache1hInputValue != nil && *normalized.InputValue > 0 {
		createCache1hRatio = *normalized.CreateCache1hInputValue / *normalized.InputValue
	}

	audioRatio := any(nil)
	audioCompletionRatio := any(nil)
	if normalized.InputValue != nil && normalized.AudioOutputValue != nil && *normalized.InputValue > 0 {
		audioRatio = *normalized.AudioOutputValue / *normalized.InputValue
		audioCompletionRatio = 1.0
	}

	now := time.Now()
	raw := map[string]any{
		"source":        "manual",
		"billing_type":  normalized.BillingType,
		"currency":      normalized.Currency,
		"group_name":    normalized.GroupName,
		"manual_note":   normalized.ManualNote,
		"manual_update": true,
	}
	if normalized.InputValue != nil {
		raw["input_value"] = *normalized.InputValue
	}
	if normalized.OutputValue != nil {
		raw["output_value"] = *normalized.OutputValue
	}
	if normalized.CacheInputValue != nil {
		raw["cache_input_value"] = *normalized.CacheInputValue
	}
	if normalized.CreateCacheInputValue != nil {
		raw["create_cache_input_value"] = *normalized.CreateCacheInputValue
	}
	if normalized.CreateCache1hInputValue != nil {
		raw["create_cache_1h_input_value"] = *normalized.CreateCache1hInputValue
	}
	if normalized.AudioOutputValue != nil {
		raw["audio_output_value"] = *normalized.AudioOutputValue
	}
	if normalized.PerRequestValue != nil {
		raw["per_request_value"] = *normalized.PerRequestValue
	}
	if normalized.GroupRatio != nil {
		raw["group_ratio"] = *normalized.GroupRatio
	}

	groupRatio := 1.0
	if normalized.GroupRatio != nil && *normalized.GroupRatio > 0 {
		groupRatio = *normalized.GroupRatio
	}

	quotaType := 0
	if normalized.BillingType == "per_request" {
		quotaType = 1
	}
	pricing, err := pricingRepo.Upsert(ctx, store.UpsertSiteModelPricingParams{
		SiteID:               site.ID,
		SiteModelID:          uuid.NullUUID{UUID: model.ID, Valid: true},
		ModelName:            model.UpstreamName,
		GroupName:            normalized.GroupName,
		QuotaType:            quotaType,
		BillingType:          normalized.BillingType,
		Currency:             normalized.Currency,
		GroupRatio:           groupRatio,
		CacheRatio:           cacheRatio,
		CreateCacheRatio:     createCacheRatio,
		CreateCache1hRatio:   createCache1hRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		InputValue:           nullableFloatPtr(normalized.InputValue),
		OutputValue:          nullableFloatPtr(normalized.OutputValue),
		PerRequestValue:      nullableFloatPtr(normalized.PerRequestValue),
		PricingSource:        "manual",
		ManualOverride:       true,
		ManualUpdatedAt:      now,
		ManualNote:           normalized.ManualNote,
		Available:            true,
		Raw:                  jsonBytes(raw),
	})
	if err != nil {
		return ModelPriceItem{}, false, err
	}

	var canonicalModel *store.CanonicalModel
	if model.CanonicalID.Valid {
		if canonical, err := canonicalRepo.GetByID(ctx, model.CanonicalID.UUID); err == nil {
			canonicalModel = &canonical
		}
	}
	return modelPriceItem(site, model, canonicalModel, &pricing), created, nil
}

func (s *Service) ResetModelPricing(ctx context.Context, siteModelID uuid.UUID) (ModelPriceItem, error) {
	modelRepo := store.NewSiteModelRepository(s.db.DB())
	siteRepo := store.NewSiteRepository(s.db.DB())
	pricingRepo := store.NewSiteModelPricingRepository(s.db.DB())
	canonicalRepo := store.NewCanonicalModelRepository(s.db.DB())

	model, err := modelRepo.GetByID(ctx, siteModelID)
	if err != nil {
		return ModelPriceItem{}, err
	}
	site, err := siteRepo.GetByID(ctx, model.SiteID)
	if err != nil {
		return ModelPriceItem{}, err
	}
	if site.SiteType == "newapi" {
		return ModelPriceItem{}, fmt.Errorf("%w: newapi pricing is synced from upstream", ErrModelPriceReadonly)
	}
	if !model.CanonicalID.Valid {
		return ModelPriceItem{}, ErrModelPriceNoCanonical
	}
	canonical, err := canonicalRepo.GetByID(ctx, model.CanonicalID.UUID)
	if err != nil || !canonical.InputPrice.Valid {
		return ModelPriceItem{}, ErrModelPriceNoCanonical
	}

	if err := pricingRepo.DeleteBySiteModelID(ctx, siteModelID); err != nil {
		return ModelPriceItem{}, err
	}

	raw := map[string]any{"source": "canonical_fallback", "canonical_model_id": canonical.ID.String(), "reset": true}
	rawBytes, _ := json.Marshal(raw)

	pricing, err := pricingRepo.Upsert(ctx, store.UpsertSiteModelPricingParams{
		SiteID:               site.ID,
		SiteModelID:          uuid.NullUUID{UUID: model.ID, Valid: true},
		ModelName:            model.UpstreamName,
		GroupName:            "default",
		BillingType:          "tokens",
		Currency:             "USD",
		GroupRatio:           1,
		InputValue:           canonical.InputPrice,
		OutputValue:          canonical.OutputPrice,
		CacheRatio:           canonical.CacheReadRatio,
		CreateCacheRatio:     canonical.CacheWriteRatio,
		CreateCache1hRatio:   canonical.CacheWrite1hRatio,
		AudioRatio:           canonical.AudioRatio,
		AudioCompletionRatio: canonical.AudioCompletionRatio,
		PricingSource:        "canonical_fallback",
		ManualOverride:       false,
		Available:            true,
		Raw:                  store.JSON(rawBytes),
	})
	if err != nil {
		return ModelPriceItem{}, err
	}

	return modelPriceItem(site, model, &canonical, &pricing), nil
}

type ResetSitePricingResult struct {
	ResetCount int
	Items      []ModelPriceItem
}

func (s *Service) ResetSitePricing(ctx context.Context, siteID uuid.UUID) (ResetSitePricingResult, error) {
	siteRepo := store.NewSiteRepository(s.db.DB())
	modelRepo := store.NewSiteModelRepository(s.db.DB())
	pricingRepo := store.NewSiteModelPricingRepository(s.db.DB())
	canonicalRepo := store.NewCanonicalModelRepository(s.db.DB())

	siteItem, err := siteRepo.GetByID(ctx, siteID)
	if err != nil {
		return ResetSitePricingResult{}, err
	}
	if siteItem.SiteType == "newapi" {
		return ResetSitePricingResult{}, fmt.Errorf("%w: newapi pricing is synced from upstream", ErrModelPriceReadonly)
	}

	pricings, err := pricingRepo.ListBySite(ctx, siteID)
	if err != nil {
		return ResetSitePricingResult{}, err
	}

	var manualPricings []store.SiteModelPricing
	for _, p := range pricings {
		if p.ManualOverride && p.Available {
			manualPricings = append(manualPricings, p)
		}
	}
	if len(manualPricings) == 0 {
		return ResetSitePricingResult{}, nil
	}

	siteModelIDs := map[uuid.UUID]struct{}{}
	for _, p := range manualPricings {
		if p.SiteModelID.Valid {
			siteModelIDs[p.SiteModelID.UUID] = struct{}{}
		}
	}

	models, err := modelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return ResetSitePricingResult{}, err
	}
	modelByID := map[uuid.UUID]store.SiteModel{}
	for _, m := range models {
		modelByID[m.ID] = m
	}

	var items []ModelPriceItem
	for _, p := range manualPricings {
		if !p.SiteModelID.Valid {
			continue
		}
		model, ok := modelByID[p.SiteModelID.UUID]
		if !ok || !model.CanonicalID.Valid {
			continue
		}
		canonical, err := canonicalRepo.GetByID(ctx, model.CanonicalID.UUID)
		if err != nil || !canonical.InputPrice.Valid {
			continue
		}

		if err := pricingRepo.DeleteBySiteModelID(ctx, p.SiteModelID.UUID); err != nil {
			continue
		}

		raw := map[string]any{"source": "canonical_fallback", "canonical_model_id": canonical.ID.String(), "reset": true}
		rawBytes, _ := json.Marshal(raw)

		newPricing, err := pricingRepo.Upsert(ctx, store.UpsertSiteModelPricingParams{
			SiteID:               siteID,
			SiteModelID:          uuid.NullUUID{UUID: model.ID, Valid: true},
			ModelName:            model.UpstreamName,
			GroupName:            "default",
			BillingType:          "tokens",
			Currency:             "USD",
			GroupRatio:           1,
			InputValue:           canonical.InputPrice,
			OutputValue:          canonical.OutputPrice,
			CacheRatio:           canonical.CacheReadRatio,
			CreateCacheRatio:     canonical.CacheWriteRatio,
			CreateCache1hRatio:   canonical.CacheWrite1hRatio,
			AudioRatio:           canonical.AudioRatio,
			AudioCompletionRatio: canonical.AudioCompletionRatio,
			PricingSource:        "canonical_fallback",
			ManualOverride:       false,
			Available:            true,
			Raw:                  store.JSON(rawBytes),
		})
		if err != nil {
			continue
		}
		items = append(items, modelPriceItem(siteItem, model, &canonical, &newPricing))
	}

	return ResetSitePricingResult{ResetCount: len(items), Items: items}, nil
}

func modelPriceItem(site store.Site, model store.SiteModel, canonicalModel *store.CanonicalModel, pricing *store.SiteModelPricing) ModelPriceItem {
	editable := site.SiteType != "newapi"
	editReason := ""
	if !editable {
		editReason = "newapi pricing is synced from upstream"
	}
	status := modelPriceStatus(pricing)
	return ModelPriceItem{
		Site:          site,
		Model:         model,
		Canonical:     canonicalModel,
		Pricing:       pricing,
		Editable:      editable,
		EditReason:    editReason,
		PricingStatus: status,
	}
}

func (l *ModelPriceList) add(item ModelPriceItem) {
	l.Items = append(l.Items, item)
	l.addCounts(item)
}

func (l *ModelPriceList) addCounts(item ModelPriceItem) {
	switch item.PricingStatus {
	case "missing":
		l.MissingCount++
	case "manual":
		l.PricedCount++
		l.ManualCount++
	default:
		l.PricedCount++
	}
}

func modelPriceStatus(pricing *store.SiteModelPricing) string {
	if !modelPriceComplete(pricing) {
		return "missing"
	}
	if pricing.ManualOverride {
		return "manual"
	}
	return "synced"
}

func modelPriceComplete(pricing *store.SiteModelPricing) bool {
	if pricing == nil || !pricing.Available {
		return false
	}
	switch strings.TrimSpace(strings.ToLower(pricing.BillingType)) {
	case "per_request", "fixed":
		return pricing.PerRequestValue.Valid && pricing.PerRequestValue.Float64 >= 0
	default:
		return (pricing.InputValue.Valid && pricing.InputValue.Float64 >= 0) || (pricing.OutputValue.Valid && pricing.OutputValue.Float64 >= 0)
	}
}

func modelPriceMatchesFilters(item ModelPriceItem, filters ModelPriceFilters) bool {
	if filters.SiteID != uuid.Nil && item.Site.ID != filters.SiteID {
		return false
	}
	if filters.SiteType != "" && item.Site.SiteType != filters.SiteType {
		return false
	}
	if filters.Provider != "" {
		provider := ""
		if item.Canonical != nil {
			provider = item.Canonical.Provider
		}
		if provider != filters.Provider {
			return false
		}
	}
	if filters.CanonicalModelID != uuid.Nil {
		if item.Canonical == nil || item.Canonical.ID != filters.CanonicalModelID {
			return false
		}
	}
	if filters.ModelKey != "" {
		modelKey := strings.ToLower(strings.TrimSpace(filters.ModelKey))
		canonicalKey := ""
		if item.Canonical != nil {
			canonicalKey = strings.ToLower(item.Canonical.ModelKey)
		}
		if canonicalKey != modelKey && strings.ToLower(item.Model.UpstreamName) != modelKey {
			return false
		}
	}
	if filters.BillingType != "" {
		if item.Pricing == nil || strings.ToLower(item.Pricing.BillingType) != filters.BillingType {
			return false
		}
	}
	switch filters.PricingStatus {
	case "missing":
		if item.Site.SiteType == "newapi" || item.PricingStatus != "missing" {
			return false
		}
	case "priced":
		if item.PricingStatus == "missing" {
			return false
		}
	case "manual", "synced":
		if item.PricingStatus != filters.PricingStatus {
			return false
		}
	}
	if filters.Q != "" {
		q := strings.ToLower(filters.Q)
		haystack := strings.ToLower(strings.Join([]string{
			item.Site.Name,
			item.Site.SiteType,
			item.Model.UpstreamName,
			item.Model.DisplayName,
			modelPriceDisplayKey(item),
		}, " "))
		if !strings.Contains(haystack, q) {
			return false
		}
	}
	return true
}

func normalizeModelPriceInput(input ModelPriceInput) (ModelPriceInput, error) {
	input.GroupName = strings.TrimSpace(input.GroupName)
	if input.GroupName == "" {
		input.GroupName = "default"
	}
	input.BillingType = strings.TrimSpace(strings.ToLower(input.BillingType))
	if input.BillingType == "" {
		input.BillingType = "tokens"
	}
	if input.BillingType == "fixed" {
		input.BillingType = "per_request"
	}
	if input.BillingType != "tokens" && input.BillingType != "per_request" {
		return input, fmt.Errorf("%w: billing_type must be tokens or per_request", ErrModelPriceInvalid)
	}
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Currency == "" {
		input.Currency = "USD"
	}
	for label, value := range map[string]*float64{
		"input_value":                 input.InputValue,
		"output_value":                input.OutputValue,
		"cache_input_value":           input.CacheInputValue,
		"create_cache_input_value":    input.CreateCacheInputValue,
		"create_cache_1h_input_value": input.CreateCache1hInputValue,
		"audio_output_value":          input.AudioOutputValue,
		"per_request_value":           input.PerRequestValue,
		"group_ratio":                 input.GroupRatio,
	} {
		if value != nil && *value < 0 {
			return input, fmt.Errorf("%w: %s must not be negative", ErrModelPriceInvalid, label)
		}
	}
	if input.AudioOutputValue != nil && (input.InputValue == nil || *input.InputValue <= 0) {
		return input, fmt.Errorf("%w: audio_output_value requires a positive input_value", ErrModelPriceInvalid)
	}
	switch input.BillingType {
	case "tokens":
		if input.InputValue == nil && input.OutputValue == nil {
			return input, fmt.Errorf("%w: tokens pricing requires input_value or output_value", ErrModelPriceInvalid)
		}
		input.PerRequestValue = nil
	case "per_request":
		if input.PerRequestValue == nil {
			return input, fmt.Errorf("%w: per_request pricing requires per_request_value", ErrModelPriceInvalid)
		}
		input.InputValue = nil
		input.OutputValue = nil
		input.CacheInputValue = nil
		input.CreateCacheInputValue = nil
		input.CreateCache1hInputValue = nil
		input.AudioOutputValue = nil
	}
	return input, nil
}

func nullableFloatPtr(value *float64) any {
	if value == nil {
		return nil
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func modelPriceDisplayKey(item ModelPriceItem) string {
	if item.Canonical != nil {
		return item.Canonical.ModelKey
	}
	return catalog.CanonicalModelKeyFromUpstream(item.Model.UpstreamName)
}

func pricingGroupName(pricing *store.SiteModelPricing) string {
	if pricing == nil {
		return ""
	}
	return pricing.GroupName
}
