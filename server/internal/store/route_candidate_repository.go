package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RouteCandidateRow struct {
	CanonicalModelID                   uuid.UUID
	CanonicalModelKey                  string
	CanonicalDisplayName               string
	SiteID                             uuid.UUID
	SiteName                           string
	SiteSlug                           string
	SiteType                           string
	SiteBaseURL                        string
	SiteStatus                         string
	SiteEnabled                        bool
	SiteRoutingPriority                float64
	SiteMeta                           JSON
	SiteModelID                        uuid.UUID
	UpstreamModelName                  string
	SiteModelDisplayName               string
	SiteModelStatus                    string
	MatchSource                        string
	MatchConfidence                    int
	SiteHealthStatus                   string
	SiteHealthSuccessRate              sql.NullFloat64
	SiteHealthAvgLatencyMS             sql.NullInt64
	SiteHealthFailures                 int
	SiteHealthCheckedAt                sql.NullTime
	ModelSuccessRate                   sql.NullFloat64
	ModelAvgLatencyMS                  sql.NullInt64
	ModelRequestCount                  int
	ModelAPIKeyCount                   int
	ModelAvailableKeyCount             int
	SiteCredentialCount                int
	PreferredCredentialID              uuid.NullUUID
	PreferredCredentialName            sql.NullString
	PreferredCredentialRoutingPriority float64
	PreferredCredentialCostMultiplier  float64
	PreferredCredentialGroupName       sql.NullString
	PreferredCredentialCacheDomain     string
	PricingGroupName                   sql.NullString
	PricingCurrency                    sql.NullString
	PricingInputValue                  sql.NullFloat64
	PricingOutputValue                 sql.NullFloat64
	PricingImageRatio                  sql.NullFloat64
	PricingPerRequestValue             sql.NullFloat64
	PricingBillingType                 sql.NullString
	PricingQuotaType                   sql.NullInt64
	SupportedEndpointTypes             []string
}

type RouteCandidateRepository struct {
	db *gorm.DB
}

func NewRouteCandidateRepository(db *gorm.DB) RouteCandidateRepository {
	return RouteCandidateRepository{db: db}
}

func (r RouteCandidateRepository) ListByCanonicalModel(ctx context.Context, canonicalModelID uuid.UUID) ([]RouteCandidateRow, error) {
	canonical, err := NewCanonicalModelRepository(r.db).GetByID(ctx, canonicalModelID)
	if err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if canonical.Status != "active" {
		return []RouteCandidateRow{}, nil
	}
	siteModels, err := NewSiteModelRepository(r.db).ListByCanonical(ctx, canonicalModelID)
	if err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	var sites []Site
	var healthStates []SiteHealthState
	var healthSnapshots []HealthSnapshot
	var apiKeyModels []SiteAPIKeyModel
	var credentials []SiteCredential
	var keyStates []SiteAPIKeyState
	var pricings []SiteModelPricing
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&healthStates).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&healthSnapshots).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&apiKeyModels).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&keyStates).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	if err := r.db.WithContext(ctx).Where(&SiteModelPricing{Available: true}).Find(&pricings).Error; err != nil {
		return nil, fmt.Errorf("list route candidates: %w", err)
	}
	cooldowns, err := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	sitesByID := map[uuid.UUID]Site{}
	for _, site := range filterActiveSites(sites) {
		sitesByID[site.ID] = site
	}
	healthBySiteID := map[uuid.UUID]SiteHealthState{}
	for _, state := range healthStates {
		healthBySiteID[state.SiteID] = state
	}
	credentialsByID := map[uuid.UUID]SiteCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
	}
	keyStatesByCredentialID := map[uuid.UUID]SiteAPIKeyState{}
	for _, state := range keyStates {
		keyStatesByCredentialID[state.SiteCredentialID] = state
	}

	items := make([]RouteCandidateRow, 0, len(siteModels))
	for _, model := range siteModels {
		site := sitesByID[model.SiteID]
		if site.ID == uuid.Nil {
			continue
		}
		state := healthBySiteID[site.ID]
		row := RouteCandidateRow{
			CanonicalModelID:       canonical.ID,
			CanonicalModelKey:      canonical.ModelKey,
			CanonicalDisplayName:   canonical.DisplayName,
			SiteID:                 site.ID,
			SiteName:               site.Name,
			SiteSlug:               site.Slug,
			SiteType:               site.SiteType,
			SiteBaseURL:            site.BaseURL,
			SiteStatus:             site.Status,
			SiteEnabled:            site.Enabled,
			SiteRoutingPriority:    site.RoutingPriority,
			SiteMeta:               site.Meta,
			SiteModelID:            model.ID,
			UpstreamModelName:      model.UpstreamName,
			SiteModelDisplayName:   model.DisplayName,
			SiteModelStatus:        model.Status,
			MatchSource:            model.MatchSource,
			MatchConfidence:        model.MatchConfidence,
			SiteHealthStatus:       defaultHealthStatus(state.Status),
			SiteHealthSuccessRate:  state.RecentSuccessRate,
			SiteHealthAvgLatencyMS: state.RecentAvgLatencyMS,
			SiteHealthFailures:     state.ConsecutiveFailures,
			SiteHealthCheckedAt:    state.CheckedAt,
		}
		fillModelHealth(&row, healthSnapshots)
		fillRouteKeyCounts(&row, apiKeyModels, credentialsByID, keyStatesByCredentialID, cooldowns)
		fillRouteSiteCredentialCount(&row, credentials, keyStatesByCredentialID, cooldowns)
		fillRoutePricing(&row, pricings)
		row.SupportedEndpointTypes = collectSupportedEndpointTypes(model)
		items = append(items, row)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SiteName != items[j].SiteName {
			return items[i].SiteName < items[j].SiteName
		}
		return items[i].UpstreamModelName < items[j].UpstreamModelName
	})
	return items, nil
}

func defaultHealthStatus(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func fillModelHealth(row *RouteCandidateRow, snapshots []HealthSnapshot) {
	since := time.Now().Add(-24 * time.Hour)
	total := 0
	success := 0
	latencySum := int64(0)
	latencyCount := int64(0)
	for _, snapshot := range snapshots {
		if !snapshot.SiteModelID.Valid || snapshot.SiteModelID.UUID != row.SiteModelID || snapshot.Scope != "model" || snapshot.Source != "gateway" || snapshot.CheckedAt.Before(since) {
			continue
		}
		total++
		if snapshot.Success {
			success++
		}
		if snapshot.LatencyMS.Valid {
			latencySum += snapshot.LatencyMS.Int64
			latencyCount++
		}
	}
	row.ModelRequestCount = total
	if total > 0 {
		row.ModelSuccessRate = sql.NullFloat64{Float64: float64(success) / float64(total), Valid: true}
	}
	if latencyCount > 0 {
		row.ModelAvgLatencyMS = sql.NullInt64{Int64: latencySum / latencyCount, Valid: true}
	}
}

func fillRouteKeyCounts(row *RouteCandidateRow, apiKeyModels []SiteAPIKeyModel, credentials map[uuid.UUID]SiteCredential, states map[uuid.UUID]SiteAPIKeyState, cooldowns []RouteCooldown) {
	total := map[uuid.UUID]struct{}{}
	available := map[uuid.UUID]GatewayCredential{}
	now := time.Now()
	for _, apiKeyModel := range apiKeyModels {
		if !apiKeyModel.SiteModelID.Valid || apiKeyModel.SiteModelID.UUID != row.SiteModelID {
			continue
		}
		total[apiKeyModel.SiteCredentialID] = struct{}{}
		credential, ok := credentials[apiKeyModel.SiteCredentialID]
		if !ok || credential.ID == uuid.Nil || credential.SiteID != row.SiteID || !isModelBoundCredentialType(credential.CredentialType) {
			continue
		}
		state := states[apiKeyModel.SiteCredentialID]
		if apiKeyModel.Available && apiKeyModel.Enabled && credentialUsable(credential) && credentialStateUsableForCredentialAt(credential, state, now) && !credentialCooling(cooldowns, row.SiteID, row.SiteModelID, credential.ID) {
			available[apiKeyModel.SiteCredentialID] = GatewayCredential{Credential: credential, State: state, GroupName: state.GroupName, ModelUpdatedAt: apiKeyModel.UpdatedAt}
		}
	}
	row.ModelAPIKeyCount = len(total)
	row.ModelAvailableKeyCount = len(available)
	items := make([]GatewayCredential, 0, len(available))
	for _, item := range available {
		items = append(items, item)
	}
	SortGatewayCredentials(items)
	if len(items) > 0 {
		setPreferredRouteCredential(row, items[0])
	}
}

func fillRouteSiteCredentialCount(row *RouteCandidateRow, credentials []SiteCredential, states map[uuid.UUID]SiteAPIKeyState, cooldowns []RouteCooldown) {
	items := make([]GatewayCredential, 0)
	now := time.Now()
	for _, credential := range credentials {
		if credential.SiteID != row.SiteID || !isAPIKeyCredentialType(credential.CredentialType) || !credentialUsable(credential) || credentialCooling(cooldowns, row.SiteID, uuid.Nil, credential.ID) {
			continue
		}
		state := states[credential.ID]
		if !credentialStateUsableForCredentialAt(credential, state, now) {
			continue
		}
		items = append(items, GatewayCredential{Credential: credential, State: state, GroupName: state.GroupName})
	}
	SortGatewayCredentials(items)
	row.SiteCredentialCount = len(items)
	if !row.PreferredCredentialID.Valid && len(items) > 0 {
		setPreferredRouteCredential(row, items[0])
	}
}

func setPreferredRouteCredential(row *RouteCandidateRow, item GatewayCredential) {
	row.PreferredCredentialID = uuid.NullUUID{UUID: item.Credential.ID, Valid: item.Credential.ID != uuid.Nil}
	name := SiteCredentialDisplayName(item.Credential, item.State)
	row.PreferredCredentialName = sql.NullString{String: name, Valid: name != ""}
	row.PreferredCredentialRoutingPriority = SiteCredentialRoutingPriority(item.Credential)
	row.PreferredCredentialCostMultiplier = SiteCredentialUpstreamCostMultiplier(item.Credential)
	row.PreferredCredentialGroupName = item.GroupName
	row.PreferredCredentialCacheDomain = SiteCredentialCacheDomain(item.Credential)
}

func fillRoutePricing(row *RouteCandidateRow, pricings []SiteModelPricing) {
	matches := make([]SiteModelPricing, 0)
	for _, pricing := range pricings {
		if pricing.SiteModelID.Valid && pricing.SiteModelID.UUID == row.SiteModelID {
			matches = append(matches, pricing)
		}
	}
	if len(matches) == 0 {
		return
	}
	groupName := ""
	if row.PreferredCredentialGroupName.Valid {
		groupName = row.PreferredCredentialGroupName.String
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return pricingRank(matches[i], groupName) < pricingRank(matches[j], groupName)
	})
	pricing := matches[0]
	row.PricingGroupName = sql.NullString{String: pricing.GroupName, Valid: pricing.GroupName != ""}
	row.PricingCurrency = sql.NullString{String: pricing.Currency, Valid: pricing.Currency != ""}
	row.PricingInputValue = pricing.InputValue
	row.PricingOutputValue = pricing.OutputValue
	row.PricingImageRatio = pricing.ImageRatio
	row.PricingPerRequestValue = pricing.PerRequestValue
	row.PricingBillingType = sql.NullString{String: pricing.BillingType, Valid: pricing.BillingType != ""}
	row.PricingQuotaType = sql.NullInt64{Int64: int64(pricing.QuotaType), Valid: true}
}

func collectSupportedEndpointTypes(model SiteModel) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0)

	if len(model.Capabilities) > 0 {
		var values map[string]any
		if err := json.Unmarshal(model.Capabilities, &values); err == nil {
			switch raw := values["supported_endpoint_types"].(type) {
			case []any:
				for _, item := range raw {
					text, _ := item.(string)
					text = strings.TrimSpace(text)
					if text == "" {
						continue
					}
					if _, ok := seen[text]; ok {
						continue
					}
					seen[text] = struct{}{}
					items = append(items, text)
				}
			case []string:
				for _, item := range raw {
					text := strings.TrimSpace(item)
					if text == "" {
						continue
					}
					if _, ok := seen[text]; ok {
						continue
					}
					seen[text] = struct{}{}
					items = append(items, text)
				}
			}
		}
	}

	return items
}
