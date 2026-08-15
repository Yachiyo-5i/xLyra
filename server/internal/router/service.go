package router

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

	"xlyra/server/internal/catalog"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

type Service struct {
	db *store.Store
}

type CandidateQuery struct {
	ModelKey            string
	EndpointType        string
	ImageGeneration     bool
	Debug               bool
	AllowedSiteIDs      []uuid.UUID
	AllowedSiteModelIDs []uuid.UUID
	ExcludeSiteIDs      []uuid.UUID
	ExcludeSiteModelIDs []uuid.UUID
	Limit               int
	FailoverLimit       int
}

type CandidateList struct {
	CanonicalModel store.CanonicalModel
	Items          []Candidate
}

type Candidate struct {
	Rank           int
	Score          float64
	Cooling        bool
	CoolingReason  string
	Site           CandidateSite
	Model          CandidateModel
	Health         CandidateHealth
	Availability   CandidateAvailability
	Credential     CandidateCredential
	Pricing        CandidatePricing
	ScoreBreakdown map[string]float64
}

type Selection struct {
	CanonicalModel store.CanonicalModel
	Candidate      Candidate
}

type SelectionPlan struct {
	CanonicalModel store.CanonicalModel
	Selected       Candidate
	Failover       []Candidate
}

type CandidateSite struct {
	ID                     uuid.UUID
	Name                   string
	Slug                   string
	SiteType               string
	BaseURL                string
	RoutingPriority        float64
	ResponsesToolPolicy    string
	DisabledResponsesTools []string
}

type CandidateModel struct {
	SiteModelID            uuid.UUID
	UpstreamName           string
	DisplayName            string
	MatchSource            string
	MatchConfidence        int
	SupportedEndpointTypes []string
}

type CandidateHealth struct {
	Status              string
	RecentSuccessRate   *float64
	RecentAvgLatencyMS  *int64
	ConsecutiveFailures int
	ModelSuccessRate    *float64
	ModelAvgLatencyMS   *int64
	ModelRequestCount   int
}

type CandidateAvailability struct {
	AvailableAPIKeys int
	TotalAPIKeys     int
}

type CandidateCredential struct {
	ID                     *uuid.UUID
	Name                   string
	RoutingPriority        float64
	GroupName              *string
	CacheDomain            string
	UpstreamCostMultiplier float64
}

type CandidatePricing struct {
	GroupName              *string
	Currency               *string
	BaseInputValue         *float64
	BaseOutputValue        *float64
	BasePerRequestValue    *float64
	InputValue             *float64
	OutputValue            *float64
	ImageRatio             *float64
	PerRequestValue        *float64
	BillingType            *string
	QuotaType              *int64
	UpstreamCostMultiplier float64
}

type CooldownInput struct {
	SiteID           uuid.UUID
	SiteModelID      *uuid.UUID
	SiteCredentialID *uuid.UUID
	Scope            string
	Source           string
	Reason           string
	Duration         time.Duration
	Metadata         map[string]any
}

var ErrNoRouteCandidates = errors.New("no route candidates available")

func NewService(db *store.Store) *Service {
	return &Service{db: db}
}

func (s *Service) Candidates(ctx context.Context, query CandidateQuery) (CandidateList, error) {
	modelKey := strings.TrimSpace(query.ModelKey)
	if modelKey == "" {
		return CandidateList{}, fmt.Errorf("model_key is required")
	}

	canonicalModel, err := s.resolveCanonicalModel(ctx, modelKey)
	if err != nil {
		return CandidateList{}, err
	}

	rows, err := store.NewRouteCandidateRepository(s.db.DB()).ListByCanonicalModel(ctx, canonicalModel.ID)
	if err != nil {
		return CandidateList{}, err
	}

	cooldowns, err := store.NewRouteCooldownRepository(s.db.DB()).ListActive(ctx, time.Now())
	if err != nil {
		return CandidateList{}, err
	}
	cooldownState := indexCooldowns(cooldowns)
	allowedSites := uuidSet(query.AllowedSiteIDs)
	allowedModels := uuidSet(query.AllowedSiteModelIDs)
	excludedSites := uuidSet(query.ExcludeSiteIDs)
	excludedModels := uuidSet(query.ExcludeSiteModelIDs)

	candidates := make([]Candidate, 0, len(rows))
	for _, row := range rows {
		availableAPIKeys := row.ModelAvailableKeyCount
		totalAPIKeys := row.ModelAPIKeyCount
		if row.SiteType != "newapi" && totalAPIKeys == 0 {
			availableAPIKeys = row.SiteCredentialCount
			totalAPIKeys = row.SiteCredentialCount
		}

		if !row.SiteEnabled || row.SiteStatus != "active" {
			continue
		}
		if row.SiteModelStatus != "active" {
			continue
		}
		if !routeCandidateSupportsEndpoint(row, query.EndpointType) {
			continue
		}
		if query.ImageGeneration && sitepkg.ResponsesToolDisabled(siteGatewayConfig(row.SiteMeta), sitepkg.ResponsesHostedToolImageGeneration) {
			continue
		}
		if availableAPIKeys <= 0 {
			continue
		}
		if allowedSites != nil {
			if _, ok := allowedSites[row.SiteID]; !ok {
				continue
			}
		}
		if allowedModels != nil {
			if _, ok := allowedModels[row.SiteModelID]; !ok {
				continue
			}
		}
		if _, ok := excludedSites[row.SiteID]; ok {
			continue
		}
		if _, ok := excludedModels[row.SiteModelID]; ok {
			continue
		}
		if _, ok := cooldownState.sites[row.SiteID]; ok {
			continue
		}
		if _, ok := cooldownState.models[row.SiteModelID]; ok {
			continue
		}
		cooling, coolingReason := false, ""
		if item, ok := cooldownState.transientModels[row.SiteModelID]; ok {
			cooling = true
			coolingReason = item.Reason
		}

		costMultiplier := row.PreferredCredentialCostMultiplier
		if costMultiplier <= 0 {
			costMultiplier = 1
		}
		baseInputValue := nullableFloat(row.PricingInputValue)
		baseOutputValue := nullableFloat(row.PricingOutputValue)
		basePerRequestValue := nullableFloat(row.PricingPerRequestValue)
		candidates = append(candidates, Candidate{
			Cooling:       cooling,
			CoolingReason: coolingReason,
			Site: CandidateSite{
				ID:                     row.SiteID,
				Name:                   row.SiteName,
				Slug:                   row.SiteSlug,
				SiteType:               row.SiteType,
				BaseURL:                row.SiteBaseURL,
				RoutingPriority:        row.SiteRoutingPriority,
				ResponsesToolPolicy:    siteResponsesToolPolicy(row.SiteMeta),
				DisabledResponsesTools: siteDisabledResponsesTools(row.SiteMeta),
			},
			Model: CandidateModel{
				SiteModelID:            row.SiteModelID,
				UpstreamName:           row.UpstreamModelName,
				DisplayName:            row.SiteModelDisplayName,
				MatchSource:            row.MatchSource,
				MatchConfidence:        row.MatchConfidence,
				SupportedEndpointTypes: append([]string(nil), row.SupportedEndpointTypes...),
			},
			Health: CandidateHealth{
				Status:              row.SiteHealthStatus,
				RecentSuccessRate:   nullableFloat(row.SiteHealthSuccessRate),
				RecentAvgLatencyMS:  nullableInt64(row.SiteHealthAvgLatencyMS),
				ConsecutiveFailures: row.SiteHealthFailures,
				ModelSuccessRate:    nullableFloat(row.ModelSuccessRate),
				ModelAvgLatencyMS:   nullableInt64(row.ModelAvgLatencyMS),
				ModelRequestCount:   row.ModelRequestCount,
			},
			Availability: CandidateAvailability{
				AvailableAPIKeys: availableAPIKeys,
				TotalAPIKeys:     totalAPIKeys,
			},
			Credential: CandidateCredential{
				ID:                     nullableUUIDPointer(row.PreferredCredentialID),
				Name:                   nullStringText(row.PreferredCredentialName),
				RoutingPriority:        row.PreferredCredentialRoutingPriority,
				GroupName:              nullableString(row.PreferredCredentialGroupName),
				CacheDomain:            row.PreferredCredentialCacheDomain,
				UpstreamCostMultiplier: costMultiplier,
			},
			Pricing: CandidatePricing{
				GroupName:              nullableString(row.PricingGroupName),
				Currency:               nullableString(row.PricingCurrency),
				BaseInputValue:         baseInputValue,
				BaseOutputValue:        baseOutputValue,
				BasePerRequestValue:    basePerRequestValue,
				InputValue:             multipliedFloat(baseInputValue, costMultiplier),
				OutputValue:            multipliedFloat(baseOutputValue, costMultiplier),
				ImageRatio:             nullableFloat(row.PricingImageRatio),
				PerRequestValue:        multipliedFloat(basePerRequestValue, costMultiplier),
				BillingType:            nullableString(row.PricingBillingType),
				QuotaType:              nullableInt64(row.PricingQuotaType),
				UpstreamCostMultiplier: costMultiplier,
			},
		})
	}

	priceValues := make([]float64, 0)
	for _, item := range candidates {
		if value, ok := candidatePriceValue(item); ok {
			priceValues = append(priceValues, value)
		}
	}

	minPrice := 0.0
	maxPrice := 0.0
	if len(priceValues) > 0 {
		minPrice = priceValues[0]
		maxPrice = priceValues[0]
		for _, value := range priceValues[1:] {
			if value < minPrice {
				minPrice = value
			}
			if value > maxPrice {
				maxPrice = value
			}
		}
	}

	for index := range candidates {
		score, breakdown := scoreCandidate(candidates[index], minPrice, maxPrice)
		candidates[index].Score = score
		if query.Debug {
			candidates[index].ScoreBreakdown = breakdown
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Cooling != candidates[j].Cooling {
			return !candidates[i].Cooling
		}
		if candidates[i].Site.RoutingPriority != candidates[j].Site.RoutingPriority {
			return candidates[i].Site.RoutingPriority > candidates[j].Site.RoutingPriority
		}
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Site.Name < candidates[j].Site.Name
		}
		return candidates[i].Score > candidates[j].Score
	})

	for index := range candidates {
		candidates[index].Rank = index + 1
	}

	if query.Limit > 0 && len(candidates) > query.Limit {
		candidates = candidates[:query.Limit]
	}

	return CandidateList{
		CanonicalModel: canonicalModel,
		Items:          candidates,
	}, nil
}

func routeCandidateSupportsEndpoint(row store.RouteCandidateRow, endpointType string) bool {
	endpointType = strings.TrimSpace(strings.ToLower(endpointType))
	if endpointType == "" {
		return true
	}
	if isMiMoV25TTSRouteModel(row.UpstreamModelName) {
		return endpointType == "openai" || endpointType == "openai-audio-speech"
	}
	for _, item := range row.SupportedEndpointTypes {
		if endpointTypeSameFamily(endpointType, strings.TrimSpace(strings.ToLower(item))) {
			return true
		}
	}
	return false
}

func isMiMoV25TTSRouteModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone":
		return true
	default:
		return false
	}
}

func endpointTypeSameFamily(a, b string) bool {
	return endpointTypeFamily(a) != "" && endpointTypeFamily(a) == endpointTypeFamily(b)
}

func endpointTypeFamily(endpointType string) string {
	switch endpointType {
	case "openai", "openai-response", "anthropic-messages", "google-gemini":
		return "text"
	case "openai-image":
		return "image"
	case "openai-embedding":
		return "embedding"
	case "openai-audio-speech":
		return "audio-speech"
	default:
		return ""
	}
}

func siteGatewayConfig(meta store.JSON) *sitepkg.GatewayConfig {
	return sitepkg.GatewayConfigFromSiteMeta(meta)
}

func siteResponsesToolPolicy(meta store.JSON) string {
	cfg := sitepkg.GatewayConfigFromSiteMeta(meta)
	if cfg == nil {
		return sitepkg.ResponsesToolPolicyPassthrough
	}
	return sitepkg.NormalizeResponsesToolPolicy(cfg.ResponsesToolPolicy)
}

func siteDisabledResponsesTools(meta store.JSON) []string {
	cfg := sitepkg.GatewayConfigFromSiteMeta(meta)
	if cfg == nil {
		return nil
	}
	return sitepkg.NormalizeDisabledResponsesTools(cfg.DisabledResponsesTools)
}

func (s *Service) Select(ctx context.Context, query CandidateQuery) (Selection, error) {
	result, err := s.Candidates(ctx, query)
	if err != nil {
		return Selection{}, err
	}
	if len(result.Items) == 0 {
		return Selection{}, ErrNoRouteCandidates
	}

	return Selection{
		CanonicalModel: result.CanonicalModel,
		Candidate:      result.Items[0],
	}, nil
}

func (s *Service) Plan(ctx context.Context, query CandidateQuery) (SelectionPlan, error) {
	result, err := s.Candidates(ctx, query)
	if err != nil {
		return SelectionPlan{}, err
	}
	if len(result.Items) == 0 {
		return SelectionPlan{}, ErrNoRouteCandidates
	}

	failoverLimit := query.FailoverLimit
	if failoverLimit <= 0 {
		failoverLimit = 3
	}

	failover := make([]Candidate, 0, failoverLimit)
	for _, item := range result.Items[1:] {
		if len(failover) >= failoverLimit {
			break
		}
		failover = append(failover, item)
	}

	return SelectionPlan{
		CanonicalModel: result.CanonicalModel,
		Selected:       result.Items[0],
		Failover:       failover,
	}, nil
}

func (s *Service) ActiveCooldowns(ctx context.Context) ([]store.RouteCooldown, error) {
	return store.NewRouteCooldownRepository(s.db.DB()).ListActive(ctx, time.Now())
}

func (s *Service) Overview(ctx context.Context, since time.Time) ([]store.RouteOverviewRow, error) {
	return store.NewRouteInsightRepository(s.db.DB()).ListOverview(ctx, since)
}

func (s *Service) ActivateCooldown(ctx context.Context, input CooldownInput) (store.RouteCooldown, error) {
	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		if input.SiteModelID != nil {
			scope = "model"
		} else {
			scope = "site"
		}
	}

	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "manual"
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "cooldown"
	}

	duration := input.Duration
	if duration <= 0 {
		duration = 5 * time.Minute
	}

	var siteModelID any
	if input.SiteModelID != nil {
		siteModelID = *input.SiteModelID
	}
	var siteCredentialID any
	if input.SiteCredentialID != nil {
		siteCredentialID = *input.SiteCredentialID
	}

	metadata, _ := json.Marshal(input.Metadata)
	return store.NewRouteCooldownRepository(s.db.DB()).Activate(ctx, store.ActivateRouteCooldownParams{
		SiteID:           input.SiteID,
		SiteModelID:      siteModelID,
		SiteCredentialID: siteCredentialID,
		Scope:            scope,
		Source:           source,
		Reason:           reason,
		ActiveUntil:      time.Now().Add(duration),
		Metadata:         metadata,
	})
}

func (s *Service) ClearCooldown(ctx context.Context, siteID uuid.UUID, siteModelID *uuid.UUID, siteCredentialID *uuid.UUID, source string) error {
	var modelValue any
	if siteModelID != nil {
		modelValue = *siteModelID
	}
	var credentialValue any
	if siteCredentialID != nil {
		credentialValue = *siteCredentialID
	}
	return store.NewRouteCooldownRepository(s.db.DB()).ClearActive(ctx, siteID, modelValue, credentialValue, source)
}

func (s *Service) resolveCanonicalModel(ctx context.Context, modelKey string) (store.CanonicalModel, error) {
	repo := store.NewCanonicalModelRepository(s.db.DB())
	normalized := catalog.CanonicalModelKeyFromUpstream(modelKey)
	if normalized == "" {
		normalized = catalog.NormalizeModelKey(modelKey)
	}

	canonical, err := repo.GetByKey(ctx, normalized)
	if err == nil {
		return canonical, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.CanonicalModel{}, err
	}

	canonical, err = repo.GetByNormalizedAlias(ctx, normalized)
	if err != nil {
		return store.CanonicalModel{}, fmt.Errorf("canonical model %q was not found", modelKey)
	}

	return canonical, nil
}

func scoreCandidate(item Candidate, minPrice float64, maxPrice float64) (float64, map[string]float64) {
	breakdown := map[string]float64{
		"site_health":        healthScore(item.Health.Status),
		"site_success_rate":  successRateScore(item.Health.RecentSuccessRate, 10),
		"site_latency":       latencyScore(item.Health.RecentAvgLatencyMS, 5),
		"model_success_rate": successRateScore(item.Health.ModelSuccessRate, 30),
		"model_latency":      latencyScore(item.Health.ModelAvgLatencyMS, 15),
		"api_key_capacity":   float64(min(item.Availability.AvailableAPIKeys, 5)) * 3,
		"price":              priceScore(item, minPrice, maxPrice),
	}

	total := 0.0
	for _, value := range breakdown {
		total += value
	}

	return total, breakdown
}

type cooldownIndex struct {
	sites                 map[uuid.UUID]store.RouteCooldown
	models                map[uuid.UUID]store.RouteCooldown
	transientModels       map[uuid.UUID]store.RouteCooldown
	siteCredentialCounts  map[uuid.UUID]int
	modelCredentialCounts map[uuid.UUID]int
}

func indexCooldowns(items []store.RouteCooldown) cooldownIndex {
	index := cooldownIndex{
		sites:                 make(map[uuid.UUID]store.RouteCooldown),
		models:                make(map[uuid.UUID]store.RouteCooldown),
		transientModels:       make(map[uuid.UUID]store.RouteCooldown),
		siteCredentialCounts:  make(map[uuid.UUID]int),
		modelCredentialCounts: make(map[uuid.UUID]int),
	}
	for _, item := range items {
		if item.SiteCredentialID.Valid {
			if item.SiteModelID.Valid {
				index.modelCredentialCounts[item.SiteModelID.UUID]++
			} else {
				index.siteCredentialCounts[item.SiteID]++
			}
			continue
		}
		if item.SiteModelID.Valid {
			if store.TransientRouteCooldown(item) {
				index.transientModels[item.SiteModelID.UUID] = item
			} else {
				index.models[item.SiteModelID.UUID] = item
			}
			continue
		}
		index.sites[item.SiteID] = item
	}

	return index
}

func uuidSet(items []uuid.UUID) map[uuid.UUID]struct{} {
	if len(items) == 0 {
		return nil
	}

	result := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		result[item] = struct{}{}
	}
	return result
}

func healthScore(status string) float64 {
	switch status {
	case "healthy":
		return 40
	case "degraded":
		return 22
	case "unknown":
		return 12
	default:
		return 0
	}
}

func successRateScore(rate *float64, maxScore float64) float64 {
	if rate == nil {
		return 0
	}
	return *rate * maxScore
}

func latencyScore(latency *int64, maxScore float64) float64 {
	if latency == nil {
		return 0
	}
	switch {
	case *latency <= 500:
		return maxScore
	case *latency <= 1500:
		return maxScore * 0.6
	case *latency <= 3000:
		return maxScore * 0.25
	default:
		return 0
	}
}

func priceScore(item Candidate, minPrice float64, maxPrice float64) float64 {
	value, ok := candidatePriceValue(item)
	if !ok {
		return 2
	}
	if maxPrice <= minPrice {
		return 10
	}
	return 10 * ((maxPrice - value) / (maxPrice - minPrice))
}

func candidatePriceValue(item Candidate) (float64, bool) {
	value := 0.0
	found := false
	if item.Pricing.PerRequestValue != nil {
		return *item.Pricing.PerRequestValue, true
	}
	if item.Pricing.InputValue != nil {
		value += *item.Pricing.InputValue
		found = true
	}
	if item.Pricing.OutputValue != nil {
		value += *item.Pricing.OutputValue
		found = true
	}
	return value, found
}

func nullableFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

func nullStringText(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nullableUUIDPointer(value uuid.NullUUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}
	result := value.UUID
	return &result
}

func multipliedFloat(value *float64, multiplier float64) *float64 {
	if value == nil {
		return nil
	}
	result := *value * multiplier
	return &result
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
