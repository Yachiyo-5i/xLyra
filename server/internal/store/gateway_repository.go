package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/upstream"
)

type GatewayRepository struct {
	db *gorm.DB
}

type GatewayCredential struct {
	Credential     SiteCredential
	State          SiteAPIKeyState
	GroupName      sql.NullString
	ModelUpdatedAt time.Time
}

type GatewayPricing struct {
	GroupName            sql.NullString
	Currency             sql.NullString
	InputValue           sql.NullFloat64
	OutputValue          sql.NullFloat64
	CacheRatio           sql.NullFloat64
	CreateCacheRatio     sql.NullFloat64
	CreateCache1hRatio   sql.NullFloat64
	GroupRatio           sql.NullFloat64
	ModelRatio           sql.NullFloat64
	CompletionRatio      sql.NullFloat64
	ImageRatio           sql.NullFloat64
	AudioRatio           sql.NullFloat64
	AudioCompletionRatio sql.NullFloat64
	ModelPrice           sql.NullFloat64
	PerRequestValue      sql.NullFloat64
	BillingType          sql.NullString
	QuotaType            sql.NullInt64
}

type GatewayModel struct {
	ID          uuid.UUID
	ModelKey    string
	DisplayName sql.NullString
	Provider    sql.NullString
	Category    sql.NullString
	CreatedAt   sql.NullTime
}

func NewGatewayRepository(db *gorm.DB) GatewayRepository {
	return GatewayRepository{db: db}
}

func (r GatewayRepository) GetCredentialForSiteModel(ctx context.Context, siteID uuid.UUID, siteModelID uuid.UUID) (GatewayCredential, error) {
	items, err := r.ListCredentialsForSiteModel(ctx, siteID, siteModelID)
	if err != nil {
		return GatewayCredential{}, err
	}
	if len(items) == 0 {
		return GatewayCredential{}, fmt.Errorf("get gateway credential for site model: %w", gorm.ErrRecordNotFound)
	}
	return items[0], nil
}

func (r GatewayRepository) ListCredentialsForSiteModel(ctx context.Context, siteID uuid.UUID, siteModelID uuid.UUID) ([]GatewayCredential, error) {
	var apiKeyModels []SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Where(map[string]any{"site_id": siteID, "site_model_id": siteModelID}).Find(&apiKeyModels).Error; err != nil {
		return nil, fmt.Errorf("list gateway credentials for site model: %w", err)
	}
	var credentials []SiteCredential
	if err := r.db.WithContext(ctx).Where(&SiteCredential{SiteID: siteID}).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("list gateway credentials for site model: %w", err)
	}
	var states []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteID: siteID}).Find(&states).Error; err != nil {
		return nil, fmt.Errorf("list gateway credentials for site model: %w", err)
	}
	cooldowns, err := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	credentialsByID := map[uuid.UUID]SiteCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
	}
	statesByCredentialID := map[uuid.UUID]SiteAPIKeyState{}
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	candidates := make([]GatewayCredential, 0)
	for _, apiModel := range apiKeyModels {
		if !apiModel.Available || !apiModel.Enabled {
			continue
		}
		credential, ok := credentialsByID[apiModel.SiteCredentialID]
		if !ok || !credentialUsable(credential) || credentialCooling(cooldowns, siteID, siteModelID, credential.ID) {
			continue
		}
		state := statesByCredentialID[credential.ID]
		if !credentialStateUsableForCredentialAt(credential, state, time.Now()) {
			continue
		}
		candidates = append(candidates, GatewayCredential{
			Credential:     credential,
			State:          state,
			GroupName:      state.GroupName,
			ModelUpdatedAt: apiModel.UpdatedAt,
		})
	}
	SortGatewayCredentials(candidates)
	return candidates, nil
}

func (r GatewayRepository) HasCredentialBindingsForSiteModel(ctx context.Context, siteID uuid.UUID, siteModelID uuid.UUID) (bool, error) {
	var apiKeyModels []SiteAPIKeyModel
	if err := r.db.WithContext(ctx).Where(map[string]any{"site_id": siteID, "site_model_id": siteModelID}).Find(&apiKeyModels).Error; err != nil {
		return false, fmt.Errorf("check gateway credential bindings for site model: %w", err)
	}
	return len(apiKeyModels) > 0, nil
}

func (r GatewayRepository) GetFallbackCredentialForSite(ctx context.Context, siteID uuid.UUID) (GatewayCredential, error) {
	var credentials []SiteCredential
	if err := r.db.WithContext(ctx).Where(&SiteCredential{SiteID: siteID}).Find(&credentials).Error; err != nil {
		return GatewayCredential{}, fmt.Errorf("get fallback gateway credential for site: %w", err)
	}
	var states []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&SiteAPIKeyState{SiteID: siteID}).Find(&states).Error; err != nil {
		return GatewayCredential{}, fmt.Errorf("get fallback gateway credential for site: %w", err)
	}
	cooldowns, err := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	if err != nil {
		return GatewayCredential{}, err
	}
	statesByCredentialID := map[uuid.UUID]SiteAPIKeyState{}
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	candidates := make([]GatewayCredential, 0, len(credentials))
	for _, credential := range credentials {
		if !isAPIKeyCredentialType(credential.CredentialType) || !credentialUsable(credential) || credentialCooling(cooldowns, siteID, uuid.Nil, credential.ID) {
			continue
		}
		state := statesByCredentialID[credential.ID]
		if !credentialStateUsableForCredentialAt(credential, state, time.Now()) {
			continue
		}
		candidates = append(candidates, GatewayCredential{Credential: credential, State: state, GroupName: state.GroupName})
	}
	SortGatewayCredentials(candidates)
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	return GatewayCredential{}, fmt.Errorf("get fallback gateway credential for site: %w", gorm.ErrRecordNotFound)
}

func (r GatewayRepository) GetPricingForSiteModel(ctx context.Context, siteModelID uuid.UUID, groupName string) (GatewayPricing, error) {
	var pricings []SiteModelPricing
	if err := r.db.WithContext(ctx).Where(map[string]any{"site_model_id": siteModelID, "available": true}).Find(&pricings).Error; err != nil {
		return GatewayPricing{}, fmt.Errorf("get gateway pricing for site model: %w", err)
	}
	if len(pricings) == 0 {
		return GatewayPricing{}, fmt.Errorf("get gateway pricing for site model: %w", gorm.ErrRecordNotFound)
	}
	groupName = strings.TrimSpace(groupName)
	sort.SliceStable(pricings, func(i, j int) bool {
		return pricingRank(pricings[i], groupName) < pricingRank(pricings[j], groupName)
	})
	item := pricings[0]
	return GatewayPricing{
		GroupName:            sql.NullString{String: item.GroupName, Valid: item.GroupName != ""},
		Currency:             sql.NullString{String: item.Currency, Valid: item.Currency != ""},
		InputValue:           item.InputValue,
		OutputValue:          item.OutputValue,
		CacheRatio:           item.CacheRatio,
		CreateCacheRatio:     item.CreateCacheRatio,
		CreateCache1hRatio:   item.CreateCache1hRatio,
		GroupRatio:           sql.NullFloat64{Float64: item.GroupRatio, Valid: true},
		ModelRatio:           item.ModelRatio,
		CompletionRatio:      item.CompletionRatio,
		ImageRatio:           item.ImageRatio,
		AudioRatio:           item.AudioRatio,
		AudioCompletionRatio: item.AudioCompletionRatio,
		ModelPrice:           item.ModelPrice,
		PerRequestValue:      item.PerRequestValue,
		BillingType:          sql.NullString{String: item.BillingType, Valid: item.BillingType != ""},
		QuotaType:            sql.NullInt64{Int64: int64(item.QuotaType), Valid: true},
	}, nil
}

func (r GatewayRepository) ListModelsForAPIKey(ctx context.Context, apiKeyID uuid.UUID) ([]GatewayModel, error) {
	apiKey, err := NewAPIKeyRepository(r.db).GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("list gateway models for api key: %w", err)
	}
	var canonicalModels []CanonicalModel
	if err := r.db.WithContext(ctx).Where(&CanonicalModel{Status: "active"}).Find(&canonicalModels).Error; err != nil {
		return nil, fmt.Errorf("list gateway models for api key: %w", err)
	}
	allowedCanonicalIDs := map[uuid.UUID]struct{}{}
	if apiKey.ModelPolicy == "allow_list" {
		siteModelIDs, err := NewAPIKeyAccessRepository(r.db).EnabledSiteModelIDs(ctx, apiKeyID)
		if err != nil {
			return nil, err
		}
		siteModels := NewSiteModelRepository(r.db)
		for _, siteModelID := range siteModelIDs {
			siteModel, err := siteModels.GetByID(ctx, siteModelID)
			if err != nil {
				return nil, err
			}
			if siteModel.CanonicalID.Valid {
				allowedCanonicalIDs[siteModel.CanonicalID.UUID] = struct{}{}
			}
		}
	}
	items := make([]GatewayModel, 0)
	for _, canonical := range canonicalModels {
		if apiKey.ModelPolicy != "allow_all" {
			if _, ok := allowedCanonicalIDs[canonical.ID]; !ok {
				continue
			}
		}
		if !r.canonicalHasAvailableRoute(ctx, canonical.ID) {
			continue
		}
		items = append(items, GatewayModel{
			ID:          canonical.ID,
			ModelKey:    canonical.ModelKey,
			DisplayName: sql.NullString{String: canonical.DisplayName, Valid: canonical.DisplayName != ""},
			Provider:    sql.NullString{String: canonical.Provider, Valid: canonical.Provider != ""},
			Category:    sql.NullString{String: canonical.Category, Valid: canonical.Category != ""},
			CreatedAt:   sql.NullTime{Time: canonical.CreatedAt, Valid: true},
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ModelKey < items[j].ModelKey
	})
	return items, nil
}

func (r GatewayRepository) canonicalHasAvailableRoute(ctx context.Context, canonicalID uuid.UUID) bool {
	siteModels, err := NewSiteModelRepository(r.db).ListByCanonical(ctx, canonicalID)
	if err != nil {
		return false
	}
	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return false
	}
	cooldowns, _ := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	sitesByID := map[uuid.UUID]Site{}
	for _, site := range filterActiveSites(sites) {
		sitesByID[site.ID] = site
	}
	for _, model := range siteModels {
		site := sitesByID[model.SiteID]
		if site.ID == uuid.Nil || !site.Enabled || site.Status != "active" || model.Status != "active" {
			continue
		}
		if routeCooling(cooldowns, site.ID, model.ID) {
			continue
		}
		if len(mustCredentialsForSiteModel(ctx, r, site.ID, model.ID)) > 0 {
			return true
		}
		if !isNewAPISite(site.SiteType) {
			hasBindings, err := r.HasCredentialBindingsForSiteModel(ctx, site.ID, model.ID)
			if err != nil || hasBindings {
				continue
			}
			if _, err := r.GetFallbackCredentialForSite(ctx, site.ID); err == nil {
				return true
			}
		}
	}
	return false
}

func mustCredentialsForSiteModel(ctx context.Context, r GatewayRepository, siteID uuid.UUID, siteModelID uuid.UUID) []GatewayCredential {
	items, err := r.ListCredentialsForSiteModel(ctx, siteID, siteModelID)
	if err != nil {
		return nil
	}
	return items
}

func pricingRank(item SiteModelPricing, groupName string) int64 {
	groupRank := int64(2)
	if groupName != "" && item.GroupName == groupName {
		groupRank = 0
	} else if item.GroupName == "default" {
		groupRank = 1
	}
	input := int64(999999999)
	if item.InputValue.Valid {
		input = int64(item.InputValue.Float64 * 1000000)
	} else if item.PerRequestValue.Valid {
		input = int64(item.PerRequestValue.Float64 * 1000000)
	}
	output := int64(999999999)
	if item.OutputValue.Valid {
		output = int64(item.OutputValue.Float64 * 1000000)
	}
	return groupRank*1000000000000000 + input*1000 + output
}

func credentialUsable(credential SiteCredential) bool {
	return jsonBoolDefault(credential.Meta, "enabled", true) && !jsonBoolDefault(credential.Meta, "raw_key_missing", false)
}

func CredentialUsable(credential SiteCredential) bool {
	return credentialUsable(credential)
}

func credentialStateUsable(state SiteAPIKeyState) bool {
	return credentialStateUsableAt(state, time.Now())
}

func credentialStateUsableAt(state SiteAPIKeyState, now time.Time) bool {
	if state.SiteCredentialID == uuid.Nil {
		return true
	}
	if !state.Enabled {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(state.SyncStatus), "stale") {
		return false
	}
	if state.ExpiredTime.Valid && state.ExpiredTime.Int64 > 0 {
		expiresAt := state.ExpiredTime.Int64
		if expiresAt > 10_000_000_000 {
			expiresAt /= 1000
		}
		if expiresAt <= now.Unix() {
			return false
		}
	}
	if !state.UnlimitedQuota && state.RemainQuota.Valid && state.RemainQuota.Int64 <= 0 {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(state.SyncStatus), "failed") {
		return true
	}
	return !credentialStatePermanentAuthFailure(state.SyncMessage.String)
}

func CredentialStateUsable(state SiteAPIKeyState) bool {
	return credentialStateUsable(state)
}

func CredentialStateUsableAt(state SiteAPIKeyState, now time.Time) bool {
	return credentialStateUsableAt(state, now)
}

func CredentialStateUsableForCredential(credential SiteCredential, state SiteAPIKeyState) bool {
	return credentialStateUsableForCredentialAt(credential, state, time.Now())
}

func CredentialStateUsableForCredentialAt(credential SiteCredential, state SiteAPIKeyState, now time.Time) bool {
	return credentialStateUsableForCredentialAt(credential, state, now)
}

func credentialStateUsableForCredentialAt(credential SiteCredential, state SiteAPIKeyState, now time.Time) bool {
	if state.SiteCredentialID == uuid.Nil && credentialManagedByUpstream(credential) {
		return false
	}
	return credentialStateUsableAt(state, now)
}

func credentialManagedByUpstream(credential SiteCredential) bool {
	meta := map[string]any{}
	if len(credential.Meta) == 0 || json.Unmarshal(credential.Meta, &meta) != nil {
		return false
	}
	if value, ok := meta["upstream_key_id"].(string); ok && strings.TrimSpace(value) != "" {
		return true
	}
	switch value := meta["upstream_id"].(type) {
	case float64:
		return value > 0
	case int:
		return value > 0
	case int64:
		return value > 0
	case json.Number:
		parsed, err := value.Int64()
		return err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return err == nil && parsed > 0
	default:
		return false
	}
}

func SiteCredentialRoutingPriority(credential SiteCredential) float64 {
	if math.IsNaN(credential.RoutingPriority) || math.IsInf(credential.RoutingPriority, 0) || credential.RoutingPriority < 1 || credential.RoutingPriority > 5 {
		return 1
	}
	return credential.RoutingPriority
}

func SiteCredentialUpstreamCostMultiplier(credential SiteCredential) float64 {
	if math.IsNaN(credential.UpstreamCostMultiplier) || math.IsInf(credential.UpstreamCostMultiplier, 0) || credential.UpstreamCostMultiplier < 0.01 || credential.UpstreamCostMultiplier > 100 {
		return 1
	}
	return credential.UpstreamCostMultiplier
}

func SiteCredentialDisplayName(credential SiteCredential, state SiteAPIKeyState) string {
	for _, value := range []string{credential.DisplayName, state.Name, credential.MaskedSecret} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return credential.CredentialType
}

func SortGatewayCredentials(items []GatewayCredential) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		leftPriority := SiteCredentialRoutingPriority(left.Credential)
		rightPriority := SiteCredentialRoutingPriority(right.Credential)
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		leftQuotaRank, leftRemaining := gatewayCredentialQuotaRank(left.State)
		rightQuotaRank, rightRemaining := gatewayCredentialQuotaRank(right.State)
		if leftQuotaRank != rightQuotaRank {
			return leftQuotaRank > rightQuotaRank
		}
		if leftRemaining != rightRemaining {
			return leftRemaining > rightRemaining
		}
		if !left.ModelUpdatedAt.Equal(right.ModelUpdatedAt) {
			return left.ModelUpdatedAt.After(right.ModelUpdatedAt)
		}
		if !left.Credential.CreatedAt.Equal(right.Credential.CreatedAt) {
			return left.Credential.CreatedAt.Before(right.Credential.CreatedAt)
		}
		return left.Credential.ID.String() < right.Credential.ID.String()
	})
}

func gatewayCredentialQuotaRank(state SiteAPIKeyState) (int, int64) {
	if state.SiteCredentialID == uuid.Nil {
		return 1, 0
	}
	if state.UnlimitedQuota {
		return 3, 0
	}
	if state.RemainQuota.Valid {
		return 2, state.RemainQuota.Int64
	}
	return 1, 0
}

func credentialStatePermanentAuthFailure(message string) bool {
	return upstream.ClassifyMessage(message).CredentialInvalid()
}

func jsonBoolDefault(data JSON, key string, fallback bool) bool {
	if len(data) == 0 {
		return fallback
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return fallback
	}
	value, ok := meta[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return fallback
	}
}

func isAPIKeyCredentialType(value string) bool {
	return value == "api_key" || strings.HasPrefix(value, "api_key:")
}

func isModelBoundCredentialType(value string) bool {
	return isAPIKeyCredentialType(value) || strings.HasPrefix(value, "grok_sso:")
}

func isNewAPISite(value string) bool {
	return value == "newapi"
}

func routeCooling(cooldowns []RouteCooldown, siteID uuid.UUID, siteModelID uuid.UUID) bool {
	for _, cooldown := range cooldowns {
		if cooldown.SiteID != siteID || cooldown.SiteCredentialID.Valid {
			continue
		}
		if TransientRouteCooldown(cooldown) {
			continue
		}
		if !cooldown.SiteModelID.Valid || cooldown.SiteModelID.UUID == siteModelID {
			return true
		}
	}
	return false
}

func credentialCooling(cooldowns []RouteCooldown, siteID uuid.UUID, siteModelID uuid.UUID, credentialID uuid.UUID) bool {
	for _, cooldown := range cooldowns {
		if cooldown.SiteID != siteID || !cooldown.SiteCredentialID.Valid || cooldown.SiteCredentialID.UUID != credentialID {
			continue
		}
		if !cooldown.SiteModelID.Valid || siteModelID == uuid.Nil || cooldown.SiteModelID.UUID == siteModelID {
			return true
		}
	}
	return false
}
