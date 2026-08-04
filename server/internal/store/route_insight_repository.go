package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RouteOverviewRow struct {
	CanonicalModelID    uuid.UUID
	ModelKey            string
	DisplayName         string
	Status              string
	SiteModelCount      int
	SiteCount           int
	EligibleCount       int
	CooldownCount       int
	RequestCount24h     int
	SuccessCount24h     int
	SuccessRate24h      sql.NullFloat64
	AvgLatencyMS24h     sql.NullInt64
	PromptTokens24h     sql.NullInt64
	CompletionTokens24h sql.NullInt64
	EstimatedCost24h    sql.NullFloat64
	LastRoutedAt        sql.NullTime
	LastSiteName        sql.NullString
	LastStatusCode      sql.NullInt64
	LastSuccess         sql.NullBool
}

type RouteInsightRepository struct {
	db *gorm.DB
}

func NewRouteInsightRepository(db *gorm.DB) RouteInsightRepository {
	return RouteInsightRepository{db: db}
}

func (r RouteInsightRepository) ListOverview(ctx context.Context, since time.Time) ([]RouteOverviewRow, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	var canonicalModels []CanonicalModel
	var siteModels []SiteModel
	var sites []Site
	var requestLogs []RequestLog
	var usageRecords []UsageRecord
	var apiKeyModels []SiteAPIKeyModel
	var credentials []SiteCredential
	var keyStates []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&CanonicalModel{Status: "active"}).Find(&canonicalModels).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&siteModels).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Gte{Column: clause.Column{Name: "created_at"}, Value: since},
		}}).
		Find(&requestLogs).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	logsByModel := groupRouteInsightLogs(requestLogs)
	if len(logsByModel.IDs) > 0 {
		if err := r.db.WithContext(ctx).Where(map[string]any{"request_log_id": logsByModel.IDs}).Find(&usageRecords).Error; err != nil {
			return nil, fmt.Errorf("list route overview: %w", err)
		}
	}
	if err := r.db.WithContext(ctx).Find(&apiKeyModels).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&keyStates).Error; err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	cooldowns, err := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("list route overview: %w", err)
	}
	allSitesByID := map[uuid.UUID]Site{}
	activeSitesByID := map[uuid.UUID]Site{}
	for _, site := range sites {
		allSitesByID[site.ID] = site
		if !SiteDeleted(site) {
			activeSitesByID[site.ID] = site
		}
	}
	credentialsByID := map[uuid.UUID]SiteCredential{}
	credentialsBySiteID := map[uuid.UUID][]SiteCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
		credentialsBySiteID[credential.SiteID] = append(credentialsBySiteID[credential.SiteID], credential)
	}
	keyStatesByCredentialID := map[uuid.UUID]SiteAPIKeyState{}
	for _, state := range keyStates {
		keyStatesByCredentialID[state.SiteCredentialID] = state
	}
	apiKeyModelsBySiteModelID := map[uuid.UUID][]SiteAPIKeyModel{}
	for _, apiKeyModel := range apiKeyModels {
		if !apiKeyModel.SiteModelID.Valid {
			continue
		}
		apiKeyModelsBySiteModelID[apiKeyModel.SiteModelID.UUID] = append(apiKeyModelsBySiteModelID[apiKeyModel.SiteModelID.UUID], apiKeyModel)
	}
	usageByLogID := map[uuid.UUID]UsageRecord{}
	for _, usage := range usageRecords {
		usageByLogID[usage.RequestLogID] = usage
	}
	items := make([]RouteOverviewRow, 0, len(canonicalModels))
	for _, canonical := range canonicalModels {
		row := RouteOverviewRow{
			CanonicalModelID: canonical.ID,
			ModelKey:         canonical.ModelKey,
			DisplayName:      canonical.DisplayName,
			Status:           canonical.Status,
		}
		siteIDs := map[uuid.UUID]struct{}{}
		for _, model := range siteModels {
			if !model.CanonicalID.Valid || model.CanonicalID.UUID != canonical.ID {
				continue
			}
			if model.Status == "unavailable" {
				continue
			}
			site := activeSitesByID[model.SiteID]
			if site.ID == uuid.Nil {
				continue
			}
			configuredChannels := configuredRouteChannelCount(model, site, apiKeyModelsBySiteModelID[model.ID], credentialsByID)
			if configuredChannels == 0 {
				continue
			}
			row.SiteModelCount += configuredChannels
			siteIDs[model.SiteID] = struct{}{}
			if routeCooling(cooldowns, model.SiteID, model.ID) {
				row.CooldownCount += configuredChannels
			}
			if site.Enabled && site.Status == "active" && model.Status == "active" {
				row.EligibleCount += eligibleRouteChannelCount(model, site, apiKeyModelsBySiteModelID[model.ID], credentialsByID, credentialsBySiteID[site.ID], keyStatesByCredentialID, cooldowns)
			}
		}
		if row.SiteModelCount == 0 {
			continue
		}
		row.SiteCount = len(siteIDs)
		fillRouteOverviewStats(&row, logsByModel.ByModel[row.CanonicalModelID], usageByLogID, allSitesByID)
		items = append(items, row)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ModelKey < items[j].ModelKey
	})
	return items, nil
}

func (r RouteInsightRepository) ListCandidateAvailability(ctx context.Context, since time.Time) ([]RouteOverviewRow, error) {
	if since.IsZero() {
		since = time.Now().Add(-24 * time.Hour)
	}
	var canonicalModels []CanonicalModel
	var siteModels []SiteModel
	var sites []Site
	var requestLogs []RequestLog
	var apiKeyModels []SiteAPIKeyModel
	var credentials []SiteCredential
	var keyStates []SiteAPIKeyState
	if err := r.db.WithContext(ctx).Where(&CanonicalModel{Status: "active"}).Find(&canonicalModels).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&siteModels).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Gte{Column: clause.Column{Name: "created_at"}, Value: since},
		}}).
		Find(&requestLogs).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&apiKeyModels).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&credentials).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	if err := r.db.WithContext(ctx).Find(&keyStates).Error; err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}
	cooldowns, err := NewRouteCooldownRepository(r.db).ListActive(ctx, time.Now())
	if err != nil {
		return nil, fmt.Errorf("list route candidate availability: %w", err)
	}

	activeSitesByID := map[uuid.UUID]Site{}
	for _, site := range sites {
		if !SiteDeleted(site) {
			activeSitesByID[site.ID] = site
		}
	}
	credentialsByID := map[uuid.UUID]SiteCredential{}
	credentialsBySiteID := map[uuid.UUID][]SiteCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
		credentialsBySiteID[credential.SiteID] = append(credentialsBySiteID[credential.SiteID], credential)
	}
	keyStatesByCredentialID := map[uuid.UUID]SiteAPIKeyState{}
	for _, state := range keyStates {
		keyStatesByCredentialID[state.SiteCredentialID] = state
	}
	apiKeyModelsBySiteModelID := map[uuid.UUID][]SiteAPIKeyModel{}
	for _, apiKeyModel := range apiKeyModels {
		if !apiKeyModel.SiteModelID.Valid {
			continue
		}
		apiKeyModelsBySiteModelID[apiKeyModel.SiteModelID.UUID] = append(apiKeyModelsBySiteModelID[apiKeyModel.SiteModelID.UUID], apiKeyModel)
	}
	requestCountByModelID := map[uuid.UUID]int{}
	for _, log := range requestLogs {
		if log.CanonicalModelID.Valid {
			requestCountByModelID[log.CanonicalModelID.UUID]++
		}
	}

	items := make([]RouteOverviewRow, 0, len(canonicalModels))
	for _, canonical := range canonicalModels {
		row := RouteOverviewRow{
			CanonicalModelID: canonical.ID,
			ModelKey:         canonical.ModelKey,
			DisplayName:      canonical.DisplayName,
			Status:           canonical.Status,
			RequestCount24h:  requestCountByModelID[canonical.ID],
		}
		siteIDs := map[uuid.UUID]struct{}{}
		for _, model := range siteModels {
			if !model.CanonicalID.Valid || model.CanonicalID.UUID != canonical.ID {
				continue
			}
			if model.Status == "unavailable" {
				continue
			}
			site := activeSitesByID[model.SiteID]
			if site.ID == uuid.Nil {
				continue
			}
			configuredChannels := configuredRouteChannelCount(model, site, apiKeyModelsBySiteModelID[model.ID], credentialsByID)
			if configuredChannels == 0 {
				continue
			}
			row.SiteModelCount += configuredChannels
			siteIDs[model.SiteID] = struct{}{}
			if routeCooling(cooldowns, model.SiteID, model.ID) {
				row.CooldownCount += configuredChannels
			}
			if site.Enabled && site.Status == "active" && model.Status == "active" {
				row.EligibleCount += eligibleRouteChannelCount(model, site, apiKeyModelsBySiteModelID[model.ID], credentialsByID, credentialsBySiteID[site.ID], keyStatesByCredentialID, cooldowns)
			}
		}
		if row.SiteModelCount == 0 {
			continue
		}
		row.SiteCount = len(siteIDs)
		items = append(items, row)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].ModelKey < items[j].ModelKey
	})
	return items, nil
}

func configuredRouteChannelCount(_ SiteModel, site Site, apiKeyModels []SiteAPIKeyModel, credentials map[uuid.UUID]SiteCredential) int {
	if isNewAPISite(site.SiteType) {
		count := 0
		for _, apiKeyModel := range apiKeyModels {
			credential := credentials[apiKeyModel.SiteCredentialID]
			if apiKeyModel.Available && credential.ID != uuid.Nil && isModelBoundCredentialType(credential.CredentialType) {
				count++
			}
		}
		return count
	}
	return 1
}

func eligibleRouteChannelCount(
	model SiteModel,
	site Site,
	apiKeyModels []SiteAPIKeyModel,
	credentials map[uuid.UUID]SiteCredential,
	siteCredentials []SiteCredential,
	states map[uuid.UUID]SiteAPIKeyState,
	cooldowns []RouteCooldown,
) int {
	if routeCooling(cooldowns, site.ID, model.ID) {
		return 0
	}
	if isNewAPISite(site.SiteType) {
		return modelAvailableKeyCount(model, site, apiKeyModels, credentials, states, cooldowns)
	}
	if len(apiKeyModels) > 0 {
		if modelAvailableKeyCount(model, site, apiKeyModels, credentials, states, cooldowns) > 0 {
			return 1
		}
		return 0
	}
	if siteFallbackCredentialCount(site.ID, siteCredentials, states, cooldowns) > 0 {
		return 1
	}
	return 0
}

func modelAvailableKeyCount(model SiteModel, site Site, apiKeyModels []SiteAPIKeyModel, credentials map[uuid.UUID]SiteCredential, states map[uuid.UUID]SiteAPIKeyState, cooldowns []RouteCooldown) int {
	available := map[uuid.UUID]struct{}{}
	now := time.Now()
	for _, apiKeyModel := range apiKeyModels {
		credential, ok := credentials[apiKeyModel.SiteCredentialID]
		if !ok || credential.ID == uuid.Nil || credential.SiteID != site.ID || !isModelBoundCredentialType(credential.CredentialType) {
			continue
		}
		state := states[apiKeyModel.SiteCredentialID]
		if apiKeyModel.Available && apiKeyModel.Enabled && credentialUsable(credential) && credentialStateUsableForCredentialAt(credential, state, now) && !credentialCooling(cooldowns, site.ID, model.ID, credential.ID) {
			available[apiKeyModel.SiteCredentialID] = struct{}{}
		}
	}
	return len(available)
}

type routeInsightLogGroups struct {
	ByModel map[uuid.UUID][]RequestLog
	IDs     []uuid.UUID
}

func groupRouteInsightLogs(logs []RequestLog) routeInsightLogGroups {
	result := routeInsightLogGroups{
		ByModel: map[uuid.UUID][]RequestLog{},
	}
	for _, log := range logs {
		if !log.CanonicalModelID.Valid {
			continue
		}
		modelID := log.CanonicalModelID.UUID
		result.ByModel[modelID] = append(result.ByModel[modelID], log)
		result.IDs = append(result.IDs, log.ID)
	}
	return result
}

func siteFallbackCredentialCount(siteID uuid.UUID, credentials []SiteCredential, states map[uuid.UUID]SiteAPIKeyState, cooldowns []RouteCooldown) int {
	count := 0
	now := time.Now()
	for _, credential := range credentials {
		if !isAPIKeyCredentialType(credential.CredentialType) || !credentialUsable(credential) || credentialCooling(cooldowns, siteID, uuid.Nil, credential.ID) {
			continue
		}
		state := states[credential.ID]
		if !credentialStateUsableForCredentialAt(credential, state, now) {
			continue
		}
		count++
	}
	return count
}

func fillRouteOverviewStats(row *RouteOverviewRow, logs []RequestLog, usage map[uuid.UUID]UsageRecord, sites map[uuid.UUID]Site) {
	success := 0
	latencySum := int64(0)
	latencyCount := int64(0)
	promptTokens := int64(0)
	completionTokens := int64(0)
	cost := float64(0)
	costValid := false
	var last *RequestLog
	for _, log := range logs {
		if last == nil || log.CreatedAt.After(last.CreatedAt) || (log.CreatedAt.Equal(last.CreatedAt) && log.ID.String() > last.ID.String()) {
			copyLog := log
			last = &copyLog
		}
	}
	for _, log := range logs {
		row.RequestCount24h++
		if log.Success {
			success++
		}
		if log.LatencyMS.Valid {
			latencySum += log.LatencyMS.Int64
			latencyCount++
		}
		if usageRecord, ok := usage[log.ID]; ok {
			promptTokens += int64(usageRecord.PromptTokens)
			completionTokens += int64(usageRecord.CompletionTokens)
			if usageRecord.EstimatedCost.Valid {
				cost += usageRecord.EstimatedCost.Float64
				costValid = true
			}
		}
	}
	row.SuccessCount24h = success
	if row.RequestCount24h > 0 {
		row.SuccessRate24h = sql.NullFloat64{Float64: float64(success) / float64(row.RequestCount24h), Valid: true}
	}
	if latencyCount > 0 {
		row.AvgLatencyMS24h = sql.NullInt64{Int64: latencySum / latencyCount, Valid: true}
	}
	if promptTokens > 0 {
		row.PromptTokens24h = sql.NullInt64{Int64: promptTokens, Valid: true}
	}
	if completionTokens > 0 {
		row.CompletionTokens24h = sql.NullInt64{Int64: completionTokens, Valid: true}
	}
	if costValid {
		row.EstimatedCost24h = sql.NullFloat64{Float64: cost, Valid: true}
	}
	if last != nil {
		row.LastRoutedAt = sql.NullTime{Time: last.CreatedAt, Valid: true}
		row.LastStatusCode = sql.NullInt64{Int64: int64(last.StatusCode), Valid: true}
		row.LastSuccess = sql.NullBool{Bool: last.Success, Valid: true}
		if last.SiteID.Valid {
			if site, ok := sites[last.SiteID.UUID]; ok {
				row.LastSiteName = sql.NullString{String: site.Name, Valid: true}
			}
		}
	}
}
