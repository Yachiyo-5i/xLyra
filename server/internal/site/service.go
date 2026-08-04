package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	"xlyra/server/internal/credential"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/modelcapabilities"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

const (
	defaultCredentialType       = "api_key"
	newAPIAccessTokenCredential = "newapi_access_token"
	xlyraAccessTokenCredential  = "xlyra_access_token"
)

type Service struct {
	db          *store.Store
	credentials credential.Service
	adapters    adapter.Registry
	modelCaps   *modelcapabilities.Service
	oauth       *oauthsvc.Service
	httpClients *httpclient.Manager
	confFile    *config.ConfigFile
	timeZone    config.TimeZone
	siteLocks   *siteRefreshLocks
}

type CreateSiteParams struct {
	Name            string
	Slug            string
	SiteType        string
	BaseURL         string
	Enabled         bool
	RoutingPriority *float64
	GatewayConfig   *GatewayConfig
	ProxyID         *string
	RequestHeaders  map[string]string
	Credential      *CredentialInput
	Credentials     []CredentialInput
}

type UpdateSiteParams struct {
	ID              uuid.UUID
	Name            string
	Slug            string
	SiteType        string
	BaseURL         string
	Enabled         bool
	RoutingPriority *float64
	GatewayConfig   *GatewayConfig
	ProxyID         *string
	RequestHeaders  map[string]string
	Credential      *CredentialInput
	Credentials     []CredentialInput
}

type CredentialInput struct {
	Type                   string
	Secret                 string
	Meta                   map[string]any
	DisplayName            *string
	RoutingPriority        *float64
	UpstreamCostMultiplier *float64
}

type SyncResult struct {
	SiteID        uuid.UUID
	ModelCount    int
	Models        []store.SiteModel
	APIKeyModels  []store.SiteAPIKeyModel
	KeyErrors     int
	KeyErrorTotal int
}

type gatewayAPIKeyModelSnapshot struct {
	SiteCredentialID uuid.UUID
	Model            adapter.Model
	Enabled          bool
}

type DetectResult = adapter.DetectResult

type SiteGroupInput struct {
	Name        string
	Slug        string
	Description string
	Enabled     bool
	SortOrder   int
	SiteIDs     []uuid.UUID
}

func NewService(db *store.Store, masterKey string, confFiles ...*config.ConfigFile) *Service {
	return NewServiceWithTimeZone(db, masterKey, config.ResolveTimeZone(), confFiles...)
}

func NewServiceWithTimeZone(db *store.Store, masterKey string, timeZone config.TimeZone, confFiles ...*config.ConfigFile) *Service {
	var confFile *config.ConfigFile
	if len(confFiles) > 0 {
		confFile = confFiles[0]
	}
	if timeZone.Location == nil {
		timeZone = config.ResolveTimeZone()
	}
	httpClients := httpclient.NewManager(confFile)
	modelCapsClient, _ := httpClients.Client(httpclient.DefaultProfile())
	return &Service{
		db:          db,
		credentials: credential.NewService(masterKey),
		adapters:    adapter.NewRegistry(),
		modelCaps:   modelcapabilities.NewWithConfig(modelcapabilities.Config{HTTPClient: modelCapsClient}),
		oauth:       oauthsvc.NewService(db, masterKey, confFile),
		httpClients: httpClients,
		confFile:    confFile,
		timeZone:    timeZone,
		siteLocks:   newSiteRefreshLocks(),
	}
}

func (s *Service) List(ctx context.Context) ([]store.Site, error) {
	return store.NewSiteRepository(s.db.DB()).List(ctx)
}

func (s *Service) ListWithDeletedRequestSites(ctx context.Context) ([]store.Site, error) {
	sites, err := store.NewSiteRepository(s.db.DB()).ListIncludingDeleted(ctx)
	if err != nil {
		return nil, err
	}

	deletedSiteIDs := make([]uuid.UUID, 0)
	for _, item := range sites {
		if store.SiteDeleted(item) {
			deletedSiteIDs = append(deletedSiteIDs, item.ID)
		}
	}
	if len(deletedSiteIDs) == 0 {
		return filterDeletedSitesWithUsage(sites, nil, nil), nil
	}

	loggedSiteIDs, err := store.NewRequestLogRepository(s.db.DB()).SiteIDsWithRequests(ctx, deletedSiteIDs)
	if err != nil {
		return nil, err
	}
	summarySiteIDs, err := store.NewRequestUsageSummaryRepository(s.db.DB()).SiteIDsWithUsage(ctx, deletedSiteIDs)
	if err != nil {
		return nil, err
	}
	return filterDeletedSitesWithUsage(sites, loggedSiteIDs, summarySiteIDs), nil
}

func filterDeletedSitesWithUsage(sites []store.Site, loggedSiteIDs map[uuid.UUID]bool, summarySiteIDs map[uuid.UUID]bool) []store.Site {
	filtered := make([]store.Site, 0, len(sites))
	for _, item := range sites {
		if !store.SiteDeleted(item) || loggedSiteIDs[item.ID] || summarySiteIDs[item.ID] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (s *Service) ListSiteGroups(ctx context.Context) ([]store.SiteGroup, error) {
	return store.NewSiteGroupRepository(s.db.DB()).List(ctx)
}

func (s *Service) GetSiteGroup(ctx context.Context, id uuid.UUID) (store.SiteGroup, error) {
	return store.NewSiteGroupRepository(s.db.DB()).GetByID(ctx, id)
}

func (s *Service) CreateSiteGroup(ctx context.Context, input SiteGroupInput) (store.SiteGroup, error) {
	params, err := siteGroupParams(input)
	if err != nil {
		return store.SiteGroup{}, err
	}
	var group store.SiteGroup
	err = s.db.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		created, err := store.NewSiteGroupRepository(tx).Create(ctx, params)
		if err != nil {
			return err
		}
		group = created
		if err := validateSiteIDs(ctx, tx, input.SiteIDs); err != nil {
			return err
		}
		return store.NewSiteGroupRepository(tx).SetGroupSites(ctx, group.ID, input.SiteIDs)
	})
	return group, err
}

func (s *Service) UpdateSiteGroup(ctx context.Context, id uuid.UUID, input SiteGroupInput) (store.SiteGroup, error) {
	params, err := siteGroupParams(input)
	if err != nil {
		return store.SiteGroup{}, err
	}
	params.ID = id
	var group store.SiteGroup
	err = s.db.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := store.NewSiteGroupRepository(tx)
		current, err := repo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		previousSites, err := repo.ListGroupSites(ctx, id)
		if err != nil {
			return err
		}
		updated, err := repo.Update(ctx, params)
		if err != nil {
			return err
		}
		group = updated
		if err := validateSiteIDs(ctx, tx, input.SiteIDs); err != nil {
			return err
		}
		if err := repo.SetGroupSites(ctx, group.ID, input.SiteIDs); err != nil {
			return err
		}
		if !group.Enabled {
			return nil
		}
		return syncAPIKeyModelsForAddedGroupSites(ctx, tx, group.ID, addedSiteIDsForGroupUpdate(current.Enabled, previousSites, input.SiteIDs))
	})
	return group, err
}

func (s *Service) DeleteSiteGroup(ctx context.Context, id uuid.UUID) error {
	return store.NewSiteGroupRepository(s.db.DB()).Delete(ctx, id)
}

func (s *Service) SiteGroupSites(ctx context.Context, id uuid.UUID) ([]store.SiteGroupSite, error) {
	return store.NewSiteGroupRepository(s.db.DB()).ListGroupSites(ctx, id)
}

func addedSiteIDsForGroupUpdate(wasEnabled bool, previousSites []store.SiteGroupSite, nextSiteIDs []uuid.UUID) []uuid.UUID {
	previous := map[uuid.UUID]struct{}{}
	if wasEnabled {
		for _, item := range previousSites {
			previous[item.SiteID] = struct{}{}
		}
	}
	seen := map[uuid.UUID]struct{}{}
	result := make([]uuid.UUID, 0, len(nextSiteIDs))
	for _, siteID := range nextSiteIDs {
		if siteID == uuid.Nil {
			continue
		}
		if _, ok := seen[siteID]; ok {
			continue
		}
		seen[siteID] = struct{}{}
		if _, ok := previous[siteID]; ok {
			continue
		}
		result = append(result, siteID)
	}
	return result
}

func syncAPIKeyModelsForAddedGroupSites(ctx context.Context, tx *gorm.DB, groupID uuid.UUID, siteIDs []uuid.UUID) error {
	if len(siteIDs) == 0 {
		return nil
	}

	var permissions []store.APIKeySiteGroupPermission
	if err := tx.WithContext(ctx).Where(&store.APIKeySiteGroupPermission{GroupID: groupID, Enabled: true}).Find(&permissions).Error; err != nil {
		return fmt.Errorf("list api keys for site group model sync: %w", err)
	}
	apiKeyIDs := make([]uuid.UUID, 0, len(permissions))
	for _, permission := range permissions {
		apiKeyIDs = append(apiKeyIDs, permission.APIKeyID)
	}
	var apiKeys []store.APIKey
	if len(apiKeyIDs) > 0 {
		if err := tx.WithContext(ctx).Where(map[string]any{"id": apiKeyIDs}).Find(&apiKeys).Error; err != nil {
			return fmt.Errorf("list api keys for site group model sync: %w", err)
		}
	}
	filteredAPIKeys := apiKeys[:0]
	for _, apiKey := range apiKeys {
		if apiKey.SitePolicy == "allow_list" && apiKey.ModelPolicy == "allow_list" {
			filteredAPIKeys = append(filteredAPIKeys, apiKey)
		}
	}
	apiKeys = filteredAPIKeys
	if len(apiKeys) == 0 {
		return nil
	}

	var siteModels []store.SiteModel
	if err := tx.WithContext(ctx).Where(map[string]any{"site_id": siteIDs}).Find(&siteModels).Error; err != nil {
		return fmt.Errorf("list added site models for api key sync: %w", err)
	}
	activeSiteModels := siteModels[:0]
	for _, siteModel := range siteModels {
		if siteModel.Status == "active" {
			activeSiteModels = append(activeSiteModels, siteModel)
		}
	}
	siteModels = activeSiteModels
	if len(siteModels) == 0 {
		return nil
	}

	modelPermissions := make([]store.APIKeySiteModelPermission, 0, len(apiKeys)*len(siteModels))
	for _, apiKey := range apiKeys {
		for _, siteModel := range siteModels {
			modelPermissions = append(modelPermissions, store.APIKeySiteModelPermission{
				APIKeyID:    apiKey.ID,
				SiteModelID: siteModel.ID,
				Enabled:     true,
			})
		}
	}
	if err := tx.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "api_key_id"}, {Name: "site_model_id"}}, DoUpdates: clause.AssignmentColumns([]string{"enabled"})}).
		Create(&modelPermissions).Error; err != nil {
		return fmt.Errorf("sync added site models to api keys: %w", err)
	}
	return nil
}

func siteGroupParams(input SiteGroupInput) (store.UpsertSiteGroupParams, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return store.UpsertSiteGroupParams{}, fmt.Errorf("name is required")
	}
	return store.UpsertSiteGroupParams{
		Name:        name,
		Slug:        strings.TrimSpace(input.Slug),
		Description: strings.TrimSpace(input.Description),
		Enabled:     input.Enabled,
		SortOrder:   input.SortOrder,
	}, nil
}

func validateSiteIDs(ctx context.Context, tx *gorm.DB, siteIDs []uuid.UUID) error {
	repo := store.NewSiteRepository(tx)
	seen := map[uuid.UUID]struct{}{}
	for _, siteID := range siteIDs {
		if siteID == uuid.Nil {
			continue
		}
		if _, ok := seen[siteID]; ok {
			continue
		}
		seen[siteID] = struct{}{}
		if _, err := repo.GetByID(ctx, siteID); err != nil {
			return fmt.Errorf("site_id %s was not found", siteID)
		}
	}
	return nil
}

func (s *Service) Capabilities(siteType string) []string {
	module, ok := s.adapters.ModuleForSiteType(siteType)
	if !ok {
		return []string{}
	}

	capabilities := adapter.ModuleCapabilities(module)
	result := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		result = append(result, string(capability))
	}

	return result
}

func (s *Service) DetectSiteType(ctx context.Context, baseURL string) (DetectResult, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return DetectResult{}, fmt.Errorf("base_url is required")
	}

	var best DetectResult
	var firstErr error
	for _, module := range s.adapters.Modules() {
		detector, ok := adapter.AsDetector(module)
		if !ok {
			continue
		}

		result, err := detector.Detect(ctx, baseURL)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if result.Matched && (!best.Matched || result.Confidence > best.Confidence) {
			best = result
		}
	}
	if best.Matched {
		return best, nil
	}
	if firstErr != nil {
		return DetectResult{}, firstErr
	}

	return DetectResult{
		SiteType:   "unknown",
		Matched:    false,
		Confidence: 0,
		Features:   map[string]any{},
	}, nil
}

func (s *Service) Get(ctx context.Context, siteID uuid.UUID) (store.Site, error) {
	return store.NewSiteRepository(s.db.DB()).GetByID(ctx, siteID)
}

// validateBaseURL restricts a site base_url to http/https with a host. It is a
// first line of defense against SSRF (rejecting file://, gopher://, etc.); the
// dial-layer guard in the httpclient blocks non-public IP targets.
func validateBaseURL(baseURL string) error {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base_url: %w", err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("base_url scheme must be http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("base_url must include a host")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, params CreateSiteParams) (store.Site, *store.SiteCredential, error) {
	params.SiteType = normalizeSiteType(params.SiteType)
	params.Slug = strings.TrimSpace(params.Slug)
	params.Name = strings.TrimSpace(params.Name)
	params.BaseURL = strings.TrimRight(strings.TrimSpace(params.BaseURL), "/")
	if params.SiteType == "grok" {
		params.BaseURL = adapter.GrokChatBaseURL
	}

	if params.BaseURL == "" {
		if module, ok := s.adapters.ModuleForSiteType(params.SiteType); ok {
			if provider, ok := adapter.AsDefaultBaseURLProvider(module); ok {
				params.BaseURL = strings.TrimRight(provider.DefaultBaseURL(), "/")
			}
		}
	}

	if params.Name == "" || params.Slug == "" || params.BaseURL == "" {
		return store.Site{}, nil, fmt.Errorf("name, slug, and base_url are required")
	}
	if err := validateBaseURL(params.BaseURL); err != nil {
		return store.Site{}, nil, err
	}

	if _, ok := s.adapters.ModuleForSiteType(params.SiteType); !ok {
		return store.Site{}, nil, fmt.Errorf("unsupported site_type %q", params.SiteType)
	}
	if err := s.validateProxyID(params.ProxyID); err != nil {
		return store.Site{}, nil, err
	}

	routingPriority, err := normalizeRoutingPriorityPointer(params.RoutingPriority)
	if err != nil {
		return store.Site{}, nil, err
	}

	credentials, err := prepareCreateCredentialInputs(params.SiteType, credentialInputs(params.Credential, params.Credentials))
	if err != nil {
		return store.Site{}, nil, err
	}

	var created store.Site
	var savedCredential *store.SiteCredential
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		siteRepo := store.NewSiteRepository(tx)
		credentialRepo := store.NewSiteCredentialRepository(tx)

		meta, err := MergeSiteGatewayConfig(nil, params.GatewayConfig)
		if err != nil {
			return err
		}
		meta = mergeStringMeta(meta, "proxy_id", params.ProxyID)
		meta = mergeRequestHeadersMeta(meta, params.RequestHeaders)

		site, err := siteRepo.Create(ctx, store.CreateSiteParams{
			Name:            params.Name,
			Slug:            params.Slug,
			SiteType:        params.SiteType,
			BaseURL:         params.BaseURL,
			Status:          "active",
			Enabled:         params.Enabled,
			RoutingPriority: routingPriority,
			Meta:            meta,
		})
		if err != nil {
			return err
		}

		created = site
		if len(credentials) == 0 {
			return nil
		}

		for index, input := range credentials {
			credential, err := s.saveCredentialInput(ctx, credentialRepo, site.ID, input)
			if err != nil {
				return err
			}
			if input.DisplayName != nil || input.RoutingPriority != nil || input.UpstreamCostMultiplier != nil {
				credential, err = credentialRepo.UpdateRoutingConfig(ctx, credential.ID, store.UpdateSiteCredentialRoutingConfigParams{
					DisplayName:            input.DisplayName,
					RoutingPriority:        input.RoutingPriority,
					UpstreamCostMultiplier: input.UpstreamCostMultiplier,
				})
				if err != nil {
					return err
				}
			}
			if index == 0 {
				savedCredential = &credential
			}
		}
		return nil
	})
	if err != nil {
		return store.Site{}, nil, err
	}

	return created, savedCredential, nil
}

func (s *Service) Update(ctx context.Context, params UpdateSiteParams) (store.Site, *store.SiteCredential, error) {
	params.SiteType = normalizeSiteType(params.SiteType)
	params.Slug = strings.TrimSpace(params.Slug)
	params.Name = strings.TrimSpace(params.Name)
	params.BaseURL = strings.TrimRight(strings.TrimSpace(params.BaseURL), "/")
	if params.SiteType == "grok" {
		params.BaseURL = adapter.GrokChatBaseURL
	}

	if params.ID == uuid.Nil {
		return store.Site{}, nil, fmt.Errorf("site id is required")
	}
	if params.Name == "" || params.Slug == "" || params.BaseURL == "" {
		return store.Site{}, nil, fmt.Errorf("name, slug, and base_url are required")
	}
	if err := validateBaseURL(params.BaseURL); err != nil {
		return store.Site{}, nil, err
	}
	if _, ok := s.adapters.ModuleForSiteType(params.SiteType); !ok {
		return store.Site{}, nil, fmt.Errorf("unsupported site_type %q", params.SiteType)
	}
	if err := s.validateProxyID(params.ProxyID); err != nil {
		return store.Site{}, nil, err
	}
	for _, input := range credentialInputs(params.Credential, params.Credentials) {
		if strings.TrimSpace(input.Secret) == "" {
			return store.Site{}, nil, fmt.Errorf("api key must not be empty when provided")
		}
	}

	var updated store.Site
	var savedCredential *store.SiteCredential
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		siteRepo := store.NewSiteRepository(tx)
		credentialRepo := store.NewSiteCredentialRepository(tx)
		existing, err := siteRepo.GetByID(ctx, params.ID)
		if err != nil {
			return err
		}

		routingPriority, err := normalizeRoutingPriorityWithFallback(params.RoutingPriority, existing.RoutingPriority)
		if err != nil {
			return err
		}

		meta, err := MergeSiteGatewayConfig(existing.Meta, params.GatewayConfig)
		if err != nil {
			return err
		}
		meta = mergeStringMeta(meta, "proxy_id", params.ProxyID)
		meta = mergeRequestHeadersMeta(meta, params.RequestHeaders)

		site, err := siteRepo.Update(ctx, store.UpdateSiteParams{
			ID:              params.ID,
			Name:            params.Name,
			Slug:            params.Slug,
			SiteType:        params.SiteType,
			BaseURL:         params.BaseURL,
			Status:          "active",
			Enabled:         params.Enabled,
			RoutingPriority: routingPriority,
			Meta:            meta,
		})
		if err != nil {
			return err
		}

		updated = site
		credentials := credentialInputs(params.Credential, params.Credentials)
		if len(credentials) == 0 {
			return nil
		}
		if normalizeSiteType(existing.SiteType) == normalizeSiteType(params.SiteType) && containsAPIKeyCredentialInput(credentials) {
			return fmt.Errorf("manage api keys through the site api key endpoints")
		}

		if shouldDeleteAPIKeyCredentials(existing.SiteType, params.SiteType, credentials) {
			if err := credentialRepo.DeleteAPIKeysBySite(ctx, site.ID); err != nil {
				return err
			}
		}
		if shouldDeleteXLyraAccessToken(existing.SiteType, params.SiteType, credentials) {
			if err := credentialRepo.DeleteBySiteAndType(ctx, site.ID, xlyraAccessTokenCredential); err != nil {
				return err
			}
		}
		for index, input := range credentials {
			credential, err := s.saveCredentialInput(ctx, credentialRepo, site.ID, input)
			if err != nil {
				return err
			}
			if index == 0 {
				savedCredential = &credential
			}
		}
		return nil
	})
	if err != nil {
		return store.Site{}, nil, err
	}

	return updated, savedCredential, nil
}

func shouldDeleteAPIKeyCredentials(existingSiteType string, nextSiteType string, credentials []CredentialInput) bool {
	if normalizeSiteType(existingSiteType) != normalizeSiteType(nextSiteType) {
		return true
	}
	return false
}

func containsAPIKeyCredentialInput(credentials []CredentialInput) bool {
	for _, input := range credentials {
		credentialType := normalizeCredentialType(input.Type)
		if credentialType == defaultCredentialType || strings.HasPrefix(credentialType, defaultCredentialType+":") {
			return true
		}
	}
	return false
}

func shouldDeleteXLyraAccessToken(existingSiteType string, nextSiteType string, credentials []CredentialInput) bool {
	if normalizeSiteType(existingSiteType) != "xlyra" || normalizeSiteType(nextSiteType) != "xlyra" {
		return false
	}
	for _, input := range credentials {
		if normalizeCredentialType(input.Type) == defaultCredentialType {
			return true
		}
	}
	return false
}

func (s *Service) Delete(ctx context.Context, siteID uuid.UUID) error {
	if siteID == uuid.Nil {
		return fmt.Errorf("site id is required")
	}

	if err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		if err := store.NewOAuthConnectionRepository(tx).DeleteBySiteID(ctx, siteID); err != nil {
			return err
		}
		if err := cleanupDeletedSiteRelations(ctx, tx, siteID); err != nil {
			return err
		}
		return store.NewSiteRepository(tx).Delete(ctx, siteID)
	}); err != nil {
		return err
	}

	_, err := store.NewCanonicalModelRepository(s.db.DB()).ArchiveUnusedAutoCreated(ctx)
	return err
}

func cleanupDeletedSiteRelations(ctx context.Context, tx store.Tx, siteID uuid.UUID) error {
	models, err := store.NewSiteModelRepository(tx).ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	modelIDs := make([]uuid.UUID, 0, len(models))
	for _, model := range models {
		modelIDs = append(modelIDs, model.ID)
	}
	if len(modelIDs) > 0 {
		if err := tx.WithContext(ctx).Where(map[string]any{"site_model_id": modelIDs}).Delete(&store.APIKeySiteModelPermission{}).Error; err != nil {
			return fmt.Errorf("clear deleted site model permissions: %w", err)
		}
	}
	if err := tx.WithContext(ctx).Where(&store.RouteCooldown{SiteID: siteID}).Delete(&store.RouteCooldown{}).Error; err != nil {
		return fmt.Errorf("clear deleted site cooldowns: %w", err)
	}
	if err := tx.WithContext(ctx).Where(&store.SiteGroupSite{SiteID: siteID}).Delete(&store.SiteGroupSite{}).Error; err != nil {
		return fmt.Errorf("clear deleted site groups: %w", err)
	}
	if err := tx.WithContext(ctx).Where(&store.APIKeySitePermission{SiteID: siteID}).Delete(&store.APIKeySitePermission{}).Error; err != nil {
		return fmt.Errorf("clear deleted site permissions: %w", err)
	}
	if err := tx.WithContext(ctx).Where(&store.SiteAPIKeyModel{SiteID: siteID}).Delete(&store.SiteAPIKeyModel{}).Error; err != nil {
		return fmt.Errorf("clear deleted site api key models: %w", err)
	}
	if err := tx.WithContext(ctx).Where(&store.SiteAPIKeyState{SiteID: siteID}).Delete(&store.SiteAPIKeyState{}).Error; err != nil {
		return fmt.Errorf("clear deleted site api key states: %w", err)
	}
	if err := store.NewSiteCredentialRepository(tx).DeleteBySite(ctx, siteID); err != nil {
		return err
	}
	return nil
}

func (s *Service) SetEnabled(ctx context.Context, siteID uuid.UUID, enabled bool) (store.Site, error) {
	if siteID == uuid.Nil {
		return store.Site{}, fmt.Errorf("site id is required")
	}

	repo := store.NewSiteRepository(s.db.DB())
	item, err := repo.GetByID(ctx, siteID)
	if err != nil {
		return store.Site{}, err
	}
	if enabled {
		reason, err := s.siteEnableBlockedReason(ctx, item)
		if err != nil {
			return store.Site{}, err
		}
		if reason != "" {
			return store.Site{}, fmt.Errorf("site is abnormal; refresh or reconnect before enabling: %s", reason)
		}
	}
	meta := siteMetaMap(item)
	delete(meta, siteMetaAutoDisabledByRefresh)
	return repo.SetEnabledAndMeta(ctx, siteID, enabled, store.JSON(jsonBytes(meta)))
}

func (s *Service) siteEnableBlockedReason(ctx context.Context, item store.Site) (string, error) {
	if reason := siteStatusEnableBlockedReason(item.Status); reason != "" {
		return reason, nil
	}

	state, err := store.NewSiteStateRepository(s.db.DB()).GetBySite(ctx, item.ID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return "", err
		}
	} else if reason := siteStateEnableBlockedReason(state); reason != "" {
		return reason, nil
	}

	if CredentialTypeForSiteType(item.SiteType) != "oauth" {
		return "", nil
	}
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, item.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return oauthConnectionEnableBlockedReason(connection), nil
}

func siteStatusEnableBlockedReason(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed", "unhealthy", "invalid", "unavailable":
		return "site status is " + strings.TrimSpace(status)
	default:
		return ""
	}
}

func siteStateEnableBlockedReason(state store.SiteState) string {
	if state.ValidationOK.Valid && !state.ValidationOK.Bool {
		return "site validation failed"
	}
	if strings.EqualFold(strings.TrimSpace(state.SyncStatus), "failed") {
		return "site sync failed"
	}
	return ""
}

func oauthConnectionEnableBlockedReason(connection store.OAuthConnection) string {
	switch strings.ToLower(strings.TrimSpace(connection.Status)) {
	case "reconnect_required":
		return "oauth connection requires reconnect"
	case "error", "expired", "failed":
		return "oauth connection status is " + strings.TrimSpace(connection.Status)
	}
	if oauthConnectionRequiresManualRefresh(connection) {
		return "oauth connection requires reconnect"
	}
	return ""
}

func (s *Service) AttachOAuthConnection(ctx context.Context, siteID uuid.UUID, provider string, connectionID uuid.UUID, attributes map[string]any) (store.Site, error) {
	repo := store.NewSiteRepository(s.db.DB())
	item, err := repo.GetByID(ctx, siteID)
	if err != nil {
		return store.Site{}, err
	}
	meta := map[string]any{}
	if len(item.Meta) > 0 {
		_ = json.Unmarshal(item.Meta, &meta)
	}
	for _, key := range []string{"oauth_provider", "oauth_connection_id", "oauth_account_id", "oauth_email", "oauth_plan_type", "oauth_subscription_tier", "oauth_project_id"} {
		delete(meta, key)
	}
	meta["oauth_provider"] = strings.TrimSpace(provider)
	meta["oauth_connection_id"] = connectionID.String()
	for key, value := range attributes {
		meta[key] = value
	}
	item.Meta = store.JSON(jsonBytes(meta))
	if err := s.db.DB().WithContext(ctx).Save(&item).Error; err != nil {
		return store.Site{}, fmt.Errorf("attach oauth connection: %w", err)
	}
	return item, nil
}

func (s *Service) SetCredential(ctx context.Context, siteID uuid.UUID, input CredentialInput) (store.SiteCredential, error) {
	return s.saveCredentialInput(ctx, store.NewSiteCredentialRepository(s.db.DB()), siteID, input)
}

type CreateAPIKeyInput struct {
	APIKey                 string
	DisplayName            string
	RoutingPriority        *float64
	UpstreamCostMultiplier *float64
}

type UpdateAPIKeyInput struct {
	Enabled                *bool
	DisplayName            *string
	RoutingPriority        *float64
	UpstreamCostMultiplier *float64
}

func (s *Service) CreateAPIKey(ctx context.Context, siteID uuid.UUID, input CreateAPIKeyInput) (APIKeyCredential, error) {
	if siteID == uuid.Nil {
		return APIKeyCredential{}, fmt.Errorf("site id is required")
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		return APIKeyCredential{}, fmt.Errorf("api key is required")
	}
	displayName, err := normalizeAPIKeyDisplayName(input.DisplayName)
	if err != nil {
		return APIKeyCredential{}, err
	}
	routingPriority, err := normalizeRoutingPriorityPointer(input.RoutingPriority)
	if err != nil {
		return APIKeyCredential{}, err
	}
	costMultiplier, err := normalizeUpstreamCostMultiplierPointer(input.UpstreamCostMultiplier, 1.0)
	if err != nil {
		return APIKeyCredential{}, err
	}

	site, err := s.Get(ctx, siteID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	if !SupportsMultipleAPIKeys(site.SiteType) {
		return APIKeyCredential{}, fmt.Errorf("site type %q does not support multiple api keys", site.SiteType)
	}
	if input.UpstreamCostMultiplier != nil && !SupportsAPIKeyCostMultiplier(site.SiteType) {
		return APIKeyCredential{}, fmt.Errorf("site type %q does not support api key cost multiplier", site.SiteType)
	}

	var created store.SiteCredential
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		credentialRepo := store.NewSiteCredentialRepository(tx)
		credentials, err := credentialRepo.ListBySite(ctx, siteID)
		if err != nil {
			return err
		}
		if err := migrateDefaultAPIKeyCredential(ctx, credentialRepo, credentials); err != nil {
			return err
		}
		for _, existing := range credentials {
			if existing.CredentialType != defaultCredentialType {
				continue
			}
			if err := syncNewAPIKeyWithExistingSiteModels(ctx, tx, siteID, existing, time.Now()); err != nil {
				return err
			}
		}
		credential, err := s.saveCredentialInput(ctx, credentialRepo, siteID, CredentialInput{
			Type:   scopedAPIKeyCredentialType(),
			Secret: apiKey,
			Meta:   map[string]any{"enabled": true},
		})
		if err != nil {
			return err
		}
		credential, err = credentialRepo.UpdateRoutingConfig(ctx, credential.ID, store.UpdateSiteCredentialRoutingConfigParams{
			DisplayName:            &displayName,
			RoutingPriority:        &routingPriority,
			UpstreamCostMultiplier: &costMultiplier,
		})
		if err != nil {
			return err
		}
		if err := syncNewAPIKeyWithExistingSiteModels(ctx, tx, siteID, credential, time.Now()); err != nil {
			return err
		}
		created = credential
		return nil
	})
	if err != nil {
		return APIKeyCredential{}, err
	}

	return s.apiKeyCredentialFromStore(created)
}

func (s *Service) DeleteAPIKey(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) error {
	if siteID == uuid.Nil {
		return fmt.Errorf("site id is required")
	}
	if credentialID == uuid.Nil {
		return fmt.Errorf("site credential id is required")
	}

	defer s.siteLocks.lock(siteID)()
	site, err := s.Get(ctx, siteID)
	if err != nil {
		return err
	}
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		credentialRepo := store.NewSiteCredentialRepository(tx)
		credential, err := credentialRepo.GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if credential.SiteID != siteID {
			return fmt.Errorf("site credential was not found")
		}
		if credential.CredentialType != defaultCredentialType && !strings.HasPrefix(credential.CredentialType, defaultCredentialType+":") {
			return fmt.Errorf("site credential is not an api key")
		}

		if err := store.NewRouteCooldownRepository(tx).DeleteBySiteCredential(ctx, siteID, credentialID); err != nil {
			return err
		}
		if err := store.NewSiteAPIKeyModelRepository(tx).DeleteByCredential(ctx, credentialID); err != nil {
			return err
		}
		if err := store.NewSiteAPIKeyStateRepository(tx).DeleteByCredential(ctx, credentialID); err != nil {
			return err
		}
		return credentialRepo.DeleteByID(ctx, credentialID)
	})
	if err != nil {
		return err
	}
	_, err = s.syncSiteModelsFromAPIKeyState(ctx, siteID, site.SiteType, site.BaseURL)
	return err
}

func syncNewAPIKeyWithExistingSiteModels(ctx context.Context, tx store.Tx, siteID uuid.UUID, credential store.SiteCredential, now time.Time) error {
	models, err := store.NewSiteModelRepository(tx).ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	modelRepo := store.NewSiteAPIKeyModelRepository(tx)
	for _, model := range models {
		if model.Status == "unavailable" {
			continue
		}
		if _, err := modelRepo.Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
			SiteID:            siteID,
			SiteCredentialID:  credential.ID,
			SiteModelID:       model.ID,
			UpstreamModelName: model.UpstreamName,
			DisplayName:       defaultString(model.DisplayName, model.UpstreamName),
			Available:         true,
			Enabled:           model.Status == "active",
			Raw:               jsonBytes(map[string]any{"source": "existing_site_model"}),
			LastSeenAt:        now,
			LastSyncedAt:      now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) saveCredentialInput(ctx context.Context, repo store.SiteCredentialRepository, siteID uuid.UUID, input CredentialInput) (store.SiteCredential, error) {
	credentialType := normalizeCredentialType(input.Type)
	encrypted, masked, err := s.credentials.Encrypt(input.Secret)
	if err != nil {
		return store.SiteCredential{}, err
	}

	meta := []byte(`{}`)
	if len(input.Meta) > 0 {
		encoded, err := json.Marshal(input.Meta)
		if err != nil {
			return store.SiteCredential{}, fmt.Errorf("marshal credential meta: %w", err)
		}
		meta = encoded
	}

	return repo.Upsert(ctx, store.UpsertSiteCredentialParams{
		SiteID:          siteID,
		CredentialType:  credentialType,
		EncryptedSecret: encrypted,
		MaskedSecret:    masked,
		Meta:            meta,
	})
}

func (s *Service) Validate(ctx context.Context, siteID uuid.UUID) error {
	site, err := s.Get(ctx, siteID)
	if err != nil {
		return err
	}

	module, ok := s.adapters.ModuleForSiteType(site.SiteType)
	if !ok {
		return fmt.Errorf("unsupported site_type %q", site.SiteType)
	}
	if validator, ok := adapter.AsSystemCredentialValidator(module); ok {
		if auth, err := s.SystemAuth(ctx, siteID); err == nil && strings.TrimSpace(auth.AccessToken) != "" {
			return validator.ValidateSystemCredentials(ctx, s.toAdapterSite(ctx, site), auth)
		}
	}

	_, apiKey, err := s.siteWithAPIKey(ctx, siteID)
	if err != nil {
		return err
	}
	validator, ok := adapter.AsCredentialValidator(module)
	if !ok {
		return fmt.Errorf("site_type %q does not support credential validation", site.SiteType)
	}

	return validator.ValidateCredentials(ctx, s.toAdapterSite(ctx, site), apiKey)
}

func (s *Service) SyncModels(ctx context.Context, siteID uuid.UUID) (SyncResult, error) {
	defer s.siteLocks.lock(siteID)()
	return s.syncModelsLocked(ctx, siteID)
}

// syncModelsLocked is the unlocked core of SyncModels. Callers that already hold
// the per-site lock (e.g. refreshModelOnlyState under RefreshState) call this
// directly to avoid re-acquiring the non-reentrant per-site mutex.
func (s *Service) syncModelsLocked(ctx context.Context, siteID uuid.UUID) (SyncResult, error) {
	site, err := s.Get(ctx, siteID)
	if err != nil {
		return SyncResult{}, err
	}

	module, ok := s.adapters.ModuleForSiteType(site.SiteType)
	if !ok {
		return SyncResult{}, fmt.Errorf("unsupported site_type %q", site.SiteType)
	}
	modelLister, ok := adapter.AsGatewayModelLister(module)
	if !ok {
		return SyncResult{}, fmt.Errorf("site_type %q does not support model sync", site.SiteType)
	}
	isGrok := strings.EqualFold(strings.TrimSpace(site.SiteType), "grok")
	summaryFetcher, hasSummaryFetcher := adapter.AsAPIKeySummaryFetcher(module)
	if isGrok && !hasSummaryFetcher {
		return SyncResult{}, fmt.Errorf("site_type %q does not support account summary", site.SiteType)
	}

	apiKeys, err := s.modelSyncCredentials(ctx, site)
	if err != nil {
		return SyncResult{}, err
	}
	if len(apiKeys) == 0 {
		if CredentialTypeForSiteType(site.SiteType) == "grok_sso" {
			return SyncResult{SiteID: site.ID}, nil
		}
		return SyncResult{}, fmt.Errorf("site api key is required")
	}

	modelsByName := map[string]adapter.Model{}
	keyNamesByModel := map[string][]string{}
	apiKeyModelSnapshots := make([]gatewayAPIKeyModelSnapshot, 0)
	syncedCredentialIDs := make([]uuid.UUID, 0, len(apiKeys))
	keyErrors := 0
	enabledKeyCount := 0
	permanentKeyErrors := 0
	lastKeyError := error(nil)
	recoverableKeyError := error(nil)
	apiKeyStateRepo := store.NewSiteAPIKeyStateRepository(s.db.DB())
	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	for _, apiKey := range apiKeys {
		autoDisabledCredential := boolFromMeta(apiKey.Meta, credentialMetaAutoDisabledByRefresh, false)
		if !apiKey.Enabled && !(siteAutoDisabledByRefresh(site) && autoDisabledCredential) {
			continue
		}
		enabledKeyCount++
		secret := apiKey.Secret
		var err error
		if isGrok {
			secret, err = s.oauth.EnsureGrokAccessToken(ctx, apiKey.Credential.ID)
		}
		var models []adapter.Model
		if err == nil {
			if isGrok {
				var summary adapter.APIKeySummary
				summary, err = summaryFetcher.SummarizeAPIKey(ctx, s.toAdapterSite(ctx, site), secret)
				models = summary.Models
			} else {
				models, err = modelLister.ListModels(ctx, s.toAdapterSite(ctx, site), secret)
			}
		}
		if err != nil {
			keyErrors++
			lastKeyError = err
			now := time.Now()
			keyEnabled := apiKey.Enabled
			keySyncStatus := "failed"
			failure := upstream.ClassifyError(err)
			if isGrok && shouldReauthorizeGrokCredential(err) {
				permanentKeyErrors++
				keyEnabled = false
				apiKey.Meta["enabled"] = false
				apiKey.Meta["reauth_required"] = true
				if _, metaErr := updateCredentialMeta(ctx, credentialRepo, apiKey.Credential.ID, apiKey.Meta); metaErr != nil {
					return SyncResult{}, fmt.Errorf("mark grok account for reauthorization: %w", metaErr)
				}
			} else if failure.CredentialInvalid() {
				permanentKeyErrors++
				keyEnabled = false
				apiKey.Meta["enabled"] = false
				apiKey.Meta[credentialMetaAutoDisabledByRefresh] = true
				if _, metaErr := updateCredentialMeta(ctx, credentialRepo, apiKey.Credential.ID, apiKey.Meta); metaErr != nil {
					return SyncResult{}, fmt.Errorf("mark api key disabled after auth failure: %w", metaErr)
				}
			} else {
				recoverableKeyError = err
			}
			if strings.EqualFold(strings.TrimSpace(site.SiteType), "grok") {
				keySyncStatus = grokRefreshFailureStatus(err)
			}
			if _, stateErr := apiKeyStateRepo.Upsert(ctx, store.UpsertSiteAPIKeyStateParams{
				SiteCredentialID: apiKey.Credential.ID,
				SiteID:           siteID,
				Enabled:          keyEnabled,
				SyncStatus:       keySyncStatus,
				SyncMessage:      err.Error(),
				LastSyncedAt:     now,
			}); stateErr != nil {
				return SyncResult{}, fmt.Errorf("save api key auth failure state: %w", stateErr)
			}
			if !keyEnabled {
				if _, stateErr := apiKeyStateRepo.UpdateEnabled(ctx, apiKey.Credential.ID, false); stateErr != nil {
					return SyncResult{}, fmt.Errorf("disable api key state after auth failure: %w", stateErr)
				}
			}
			continue
		}
		if autoDisabledCredential {
			apiKey.Enabled = true
			apiKey.Meta["enabled"] = true
			delete(apiKey.Meta, credentialMetaAutoDisabledByRefresh)
			if _, metaErr := updateCredentialMeta(ctx, credentialRepo, apiKey.Credential.ID, apiKey.Meta); metaErr != nil {
				return SyncResult{}, fmt.Errorf("restore api key after auth recovery: %w", metaErr)
			}
		}
		s.recoverCredentialCooldownAfterRefresh(ctx, site.ID, apiKey.Credential.ID)
		syncedCredentialIDs = append(syncedCredentialIDs, apiKey.Credential.ID)
		now := time.Now()
		if _, stateErr := apiKeyStateRepo.Upsert(ctx, store.UpsertSiteAPIKeyStateParams{
			SiteCredentialID: apiKey.Credential.ID,
			SiteID:           siteID,
			Enabled:          apiKey.Enabled,
			SyncStatus:       "synced",
			LastSyncedAt:     now,
		}); stateErr != nil {
			return SyncResult{}, fmt.Errorf("save api key synced state: %w", stateErr)
		}
		if autoDisabledCredential {
			if _, stateErr := restoreAPIKeyStateAfterAuthRecovery(ctx, apiKeyStateRepo, apiKey.Credential.ID); stateErr != nil {
				return SyncResult{}, stateErr
			}
		}
		keyName := apiKey.Name
		if keyName == "" {
			keyName = apiKey.MaskedSecret
		}
		disabledModels := stringSetFromAny(apiKey.Meta["disabled_models"])
		for _, model := range models {
			model = s.enrichModelCapabilities(ctx, site, model)
			modelEnabled := true
			if _, disabled := disabledModels[model.UpstreamName]; disabled {
				modelEnabled = false
			}
			apiKeyModelSnapshots = append(apiKeyModelSnapshots, gatewayAPIKeyModelSnapshot{
				SiteCredentialID: apiKey.Credential.ID,
				Model:            model,
				Enabled:          modelEnabled,
			})
			if !modelEnabled {
				continue
			}
			modelsByName[model.UpstreamName] = coalesceModelCapabilities(modelsByName[model.UpstreamName], model, keyName)
			keyNamesByModel[model.UpstreamName] = appendUnique(keyNamesByModel[model.UpstreamName], keyName)
		}
	}

	if keyErrors > 0 && keyErrors == enabledKeyCount {
		cause := lastKeyError
		if recoverableKeyError != nil || permanentKeyErrors < enabledKeyCount {
			cause = recoverableKeyError
		}
		if cause != nil {
			return SyncResult{}, fmt.Errorf("all %d enabled api keys failed: %w", keyErrors, cause)
		}
		return SyncResult{}, fmt.Errorf("all %d enabled api keys failed", keyErrors)
	}
	if enabledKeyCount == 0 {
		return SyncResult{}, fmt.Errorf("no enabled api keys available for model sync")
	}

	models := make([]adapter.Model, 0, len(modelsByName))
	for name, model := range modelsByName {
		if model.Capabilities == nil {
			model.Capabilities = map[string]any{}
		}
		model.Capabilities["api_keys"] = keyNamesByModel[name]
		models = append(models, model)
	}
	if len(models) == 0 {
		return SyncResult{}, fmt.Errorf("no usable models available for model sync")
	}

	saved := make([]store.SiteModel, 0, len(models))
	apiKeyModels := make([]store.SiteAPIKeyModel, 0, len(apiKeyModelSnapshots))
	if err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		repo := store.NewSiteModelRepository(tx)
		seenModelNames := make([]string, 0, len(models))
		for _, model := range models {
			capabilities, err := json.Marshal(model.Capabilities)
			if err != nil {
				return fmt.Errorf("marshal model capabilities: %w", err)
			}

			siteModel, err := repo.Upsert(ctx, store.UpsertSiteModelParams{
				SiteID:       site.ID,
				UpstreamName: model.UpstreamName,
				DisplayName:  model.DisplayName,
				Capabilities: capabilities,
				Status:       "active",
			})
			if err != nil {
				return err
			}

			if err := seedDefaultModelPricing(ctx, tx, site, siteModel, model); err != nil {
				return err
			}

			saved = append(saved, siteModel)
			seenModelNames = append(seenModelNames, model.UpstreamName)
		}
		if err := repo.MarkUnavailableExcept(ctx, site.ID, seenModelNames); err != nil {
			return err
		}

		currentModels, err := repo.ListBySite(ctx, site.ID)
		if err != nil {
			return err
		}

		siteModelsByName := make(map[string]store.SiteModel, len(currentModels)*2)
		for _, model := range currentModels {
			for _, name := range []string{model.UpstreamName, model.DisplayName} {
				name = strings.TrimSpace(name)
				if name != "" {
					siteModelsByName[name] = model
				}
			}
		}

		synced, err := syncGatewayAPIKeyModels(
			ctx,
			site.ID,
			syncedCredentialIDs,
			apiKeyModelSnapshots,
			siteModelsByName,
			store.NewSiteAPIKeyModelRepository(tx),
			time.Now(),
		)
		if err != nil {
			return err
		}
		apiKeyModels = synced
		return nil
	}); err != nil {
		return SyncResult{}, err
	}

	return SyncResult{
		SiteID:        site.ID,
		ModelCount:    len(saved),
		Models:        saved,
		APIKeyModels:  apiKeyModels,
		KeyErrors:     keyErrors,
		KeyErrorTotal: enabledKeyCount,
	}, nil
}

func syncGatewayAPIKeyModels(
	ctx context.Context,
	siteID uuid.UUID,
	syncedCredentialIDs []uuid.UUID,
	snapshots []gatewayAPIKeyModelSnapshot,
	siteModelsByName map[string]store.SiteModel,
	repo store.SiteAPIKeyModelRepository,
	now time.Time,
) ([]store.SiteAPIKeyModel, error) {
	seenByCredentialID := make(map[uuid.UUID][]string, len(syncedCredentialIDs))
	for _, credentialID := range syncedCredentialIDs {
		if credentialID != uuid.Nil {
			seenByCredentialID[credentialID] = nil
		}
	}

	items := make([]store.SiteAPIKeyModel, 0, len(snapshots))
	for _, snapshot := range snapshots {
		modelName := strings.TrimSpace(snapshot.Model.UpstreamName)
		if snapshot.SiteCredentialID == uuid.Nil || modelName == "" {
			continue
		}

		seenByCredentialID[snapshot.SiteCredentialID] = appendUnique(seenByCredentialID[snapshot.SiteCredentialID], modelName)

		var siteModelID any
		if siteModel, ok := siteModelsByName[modelName]; ok {
			siteModelID = siteModel.ID
		}

		var raw store.JSON
		if snapshot.Model.Capabilities != nil {
			raw = jsonBytes(snapshot.Model.Capabilities["raw"])
		}
		item, err := repo.Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
			SiteID:            siteID,
			SiteCredentialID:  snapshot.SiteCredentialID,
			SiteModelID:       siteModelID,
			UpstreamModelName: modelName,
			DisplayName:       defaultString(snapshot.Model.DisplayName, modelName),
			Available:         true,
			Enabled:           snapshot.Enabled,
			Raw:               raw,
			LastSeenAt:        now,
			LastSyncedAt:      now,
		})
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	for credentialID, modelNames := range seenByCredentialID {
		if err := repo.MarkUnavailableExcept(ctx, credentialID, modelNames); err != nil {
			return nil, err
		}
	}

	return items, nil
}

func (s *Service) ListModels(ctx context.Context, siteID uuid.UUID) ([]store.SiteModel, error) {
	models, err := store.NewSiteModelRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return visibleSiteModels(models), nil
}

func (s *Service) SitePricingGroups(ctx context.Context, siteID uuid.UUID) ([]store.SitePricingGroup, error) {
	return store.NewSitePricingGroupRepository(s.db.DB()).ListBySite(ctx, siteID)
}

func (s *Service) SiteModelPricings(ctx context.Context, siteID uuid.UUID) ([]store.SiteModelPricing, error) {
	items, err := store.NewSiteModelPricingRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return availableSiteModelPricings(items), nil
}

func (s *Service) AllSiteModelPricings(ctx context.Context) ([]store.SiteModelPricing, error) {
	items, err := store.NewSiteModelPricingRepository(s.db.DB()).ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return availableSiteModelPricings(items), nil
}

func (s *Service) SetModelEnabled(ctx context.Context, siteID uuid.UUID, modelID uuid.UUID, enabled bool) (store.SiteModel, error) {
	status := "disabled"
	if enabled {
		status = "active"
	}

	var model store.SiteModel
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		updated, err := store.NewSiteModelRepository(tx).UpdateStatus(ctx, siteID, modelID, status)
		if err != nil {
			return err
		}
		if err := syncSiteModelAPIKeySwitches(ctx, tx, siteID, updated, enabled); err != nil {
			return err
		}
		model = updated
		return nil
	})
	if err != nil {
		return store.SiteModel{}, err
	}
	return model, nil
}

func (s *Service) SetModelsEnabled(ctx context.Context, siteID uuid.UUID, modelIDs []uuid.UUID, enabled bool) ([]store.SiteModel, error) {
	if siteID == uuid.Nil {
		return nil, fmt.Errorf("site id is required")
	}
	modelIDs = uniqueUUIDs(modelIDs)
	if len(modelIDs) == 0 {
		return nil, fmt.Errorf("model_ids is required")
	}

	status := "disabled"
	if enabled {
		status = "active"
	}

	var models []store.SiteModel
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		updated, err := store.NewSiteModelRepository(tx).UpdateStatuses(ctx, siteID, modelIDs, status)
		if err != nil {
			return err
		}
		for _, model := range updated {
			if err := syncSiteModelAPIKeySwitches(ctx, tx, siteID, model, enabled); err != nil {
				return err
			}
		}
		models = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return models, nil
}

func syncSiteModelAPIKeySwitches(ctx context.Context, tx store.Tx, siteID uuid.UUID, model store.SiteModel, enabled bool) error {
	credentialRepo := store.NewSiteCredentialRepository(tx)
	credentials, err := credentialRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}

	modelName := strings.TrimSpace(model.UpstreamName)
	for _, credential := range credentials {
		if !isSiteAPIKeyCredentialType(credential.CredentialType) {
			continue
		}
		meta := map[string]any{}
		if len(credential.Meta) > 0 {
			_ = json.Unmarshal(credential.Meta, &meta)
		}
		disabledModels := stringSetFromAny(meta["disabled_models"])
		if enabled {
			delete(disabledModels, modelName)
		} else {
			disabledModels[modelName] = struct{}{}
		}
		meta["disabled_models"] = sortedStringSet(disabledModels)
		if _, err := updateCredentialMeta(ctx, credentialRepo, credential.ID, meta); err != nil {
			return err
		}
	}

	apiKeyModelRepo := store.NewSiteAPIKeyModelRepository(tx)
	apiKeyModels, err := apiKeyModelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	for _, item := range apiKeyModels {
		if !siteAPIKeyModelMatchesSiteModel(item, model) {
			continue
		}
		if _, err := apiKeyModelRepo.UpdateEnabled(ctx, item.SiteCredentialID, item.UpstreamModelName, enabled); err != nil {
			return err
		}
	}
	return nil
}

func siteAPIKeyModelMatchesSiteModel(item store.SiteAPIKeyModel, model store.SiteModel) bool {
	if item.SiteModelID.Valid && item.SiteModelID.UUID == model.ID {
		return true
	}
	return strings.TrimSpace(item.UpstreamModelName) == strings.TrimSpace(model.UpstreamName)
}

func visibleSiteModels(models []store.SiteModel) []store.SiteModel {
	visible := make([]store.SiteModel, 0, len(models))
	for _, model := range models {
		if model.Status == "unavailable" {
			continue
		}
		visible = append(visible, model)
	}
	return visible
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]struct{}{}
	result := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func availableSiteModelPricings(items []store.SiteModelPricing) []store.SiteModelPricing {
	available := make([]store.SiteModelPricing, 0, len(items))
	for _, item := range items {
		if !item.Available {
			continue
		}
		available = append(available, item)
	}
	return available
}

func (s *Service) Credential(ctx context.Context, siteID uuid.UUID) (store.Site, store.SiteCredential, string, error) {
	site, apiKey, err := s.siteWithAPIKeyCredential(ctx, siteID)
	if err != nil {
		return store.Site{}, store.SiteCredential{}, "", err
	}

	return site, apiKey.Credential, apiKey.Secret, nil
}

type APIKeyCredential struct {
	Credential             store.SiteCredential
	Secret                 string
	Name                   string
	DisplayName            string
	UpstreamID             int
	Enabled                bool
	MaskedSecret           string
	SecretMissing          bool
	RoutingPriority        float64
	UpstreamCostMultiplier float64
	Meta                   map[string]any
}

func (s *Service) APIKeys(ctx context.Context, siteID uuid.UUID) ([]APIKeyCredential, error) {
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}

	result := make([]APIKeyCredential, 0, len(credentials))
	for _, credential := range credentials {
		if credential.CredentialType != defaultCredentialType && !strings.HasPrefix(credential.CredentialType, defaultCredentialType+":") {
			continue
		}
		if credential.CredentialType == defaultCredentialType && hasTokenScopedCredentials(credentials) {
			continue
		}

		secret, err := s.credentials.Decrypt(credential.EncryptedSecret)
		if err != nil {
			return nil, err
		}

		meta := map[string]any{}
		if len(credential.Meta) > 0 {
			_ = json.Unmarshal(credential.Meta, &meta)
		}
		name, _ := meta["name"].(string)
		upstreamID := 0
		if value, ok := meta["upstream_id"].(float64); ok {
			upstreamID = int(value)
		}

		result = append(result, APIKeyCredential{
			Credential:             credential,
			Secret:                 secretFromCredentialMeta(secret, meta),
			Name:                   strings.TrimSpace(name),
			DisplayName:            strings.TrimSpace(credential.DisplayName),
			UpstreamID:             upstreamID,
			Enabled:                boolFromMeta(meta, "enabled", true),
			MaskedSecret:           maskedSecretFromCredentialMeta(credential.MaskedSecret, meta),
			SecretMissing:          boolFromMeta(meta, "raw_key_missing", false),
			RoutingPriority:        credentialRoutingPriority(credential),
			UpstreamCostMultiplier: credentialUpstreamCostMultiplier(credential),
			Meta:                   meta,
		})
	}

	return result, nil
}

func (s *Service) modelSyncCredentials(ctx context.Context, site store.Site) ([]APIKeyCredential, error) {
	if CredentialTypeForSiteType(site.SiteType) != "grok_sso" {
		return s.APIKeys(ctx, site.ID)
	}

	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		return nil, err
	}
	result := make([]APIKeyCredential, 0, len(credentials))
	for _, credential := range credentials {
		if !isGrokCredentialType(credential.CredentialType) {
			continue
		}
		account, err := s.apiKeyCredentialFromStore(credential)
		if err != nil {
			return nil, err
		}
		result = append(result, account)
	}
	return result, nil
}

func (s *Service) APIKeyByCredentialID(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) (APIKeyCredential, error) {
	credential, err := store.NewSiteCredentialRepository(s.db.DB()).GetByID(ctx, credentialID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	if credential.SiteID != siteID {
		return APIKeyCredential{}, fmt.Errorf("credential does not belong to site")
	}
	return s.apiKeyCredentialFromStore(credential)
}

func (s *Service) SetAPIKeyEnabled(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, enabled bool) (APIKeyCredential, error) {
	defer s.siteLocks.lock(siteID)()

	site, err := s.Get(ctx, siteID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	credential, meta, err := s.apiKeyCredentialMeta(ctx, siteID, credentialID)
	if err != nil {
		return APIKeyCredential{}, err
	}

	meta["enabled"] = enabled
	updated, err := updateCredentialMeta(ctx, store.NewSiteCredentialRepository(s.db.DB()), credential.ID, meta)
	if err != nil {
		return APIKeyCredential{}, err
	}
	_, _ = store.NewSiteAPIKeyStateRepository(s.db.DB()).UpdateEnabled(ctx, credential.ID, enabled)
	// Toggling a key changes which upstream models are backed by an enabled
	// credential, so the site-level model availability must follow.
	_, _ = s.syncSiteModelsFromAPIKeyState(ctx, siteID, site.SiteType, site.BaseURL)

	return s.apiKeyCredentialFromStore(updated)
}

func (s *Service) UpdateAPIKey(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, input UpdateAPIKeyInput) (APIKeyCredential, error) {
	defer s.siteLocks.lock(siteID)()

	site, err := s.Get(ctx, siteID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	if !SupportsMultipleAPIKeys(site.SiteType) && (input.DisplayName != nil || input.RoutingPriority != nil || input.UpstreamCostMultiplier != nil) {
		return APIKeyCredential{}, fmt.Errorf("site type %s does not support api key routing configuration", site.SiteType)
	}
	if input.UpstreamCostMultiplier != nil && !SupportsAPIKeyCostMultiplier(site.SiteType) {
		return APIKeyCredential{}, fmt.Errorf("site type %s does not support api key cost multiplier", site.SiteType)
	}
	credential, meta, err := s.apiKeyCredentialMeta(ctx, siteID, credentialID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	params := store.UpdateSiteCredentialRoutingConfigParams{}
	if input.DisplayName != nil {
		value, err := normalizeAPIKeyDisplayName(*input.DisplayName)
		if err != nil {
			return APIKeyCredential{}, err
		}
		params.DisplayName = &value
	}
	if input.RoutingPriority != nil {
		value, err := normalizeRoutingPriority(*input.RoutingPriority)
		if err != nil {
			return APIKeyCredential{}, err
		}
		params.RoutingPriority = &value
	}
	if input.UpstreamCostMultiplier != nil {
		value, err := normalizeUpstreamCostMultiplier(*input.UpstreamCostMultiplier)
		if err != nil {
			return APIKeyCredential{}, err
		}
		params.UpstreamCostMultiplier = &value
	}

	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		repo := store.NewSiteCredentialRepository(tx)
		if params.DisplayName != nil || params.RoutingPriority != nil || params.UpstreamCostMultiplier != nil {
			credential, err = repo.UpdateRoutingConfig(ctx, credential.ID, params)
			if err != nil {
				return err
			}
		}
		if input.Enabled == nil {
			return nil
		}
		meta["enabled"] = *input.Enabled
		credential, err = repo.UpdateMeta(ctx, credential.ID, store.JSON(jsonBytes(meta)))
		if err != nil {
			return err
		}
		_, stateErr := store.NewSiteAPIKeyStateRepository(tx).UpdateEnabled(ctx, credential.ID, *input.Enabled)
		if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
			return stateErr
		}
		return nil
	})
	if err != nil {
		return APIKeyCredential{}, err
	}
	if input.Enabled != nil {
		if _, err := s.syncSiteModelsFromAPIKeyState(ctx, siteID, site.SiteType, site.BaseURL); err != nil {
			return APIKeyCredential{}, err
		}
	}
	return s.apiKeyCredentialFromStore(credential)
}

func (s *Service) PatchAPIKeyMeta(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, patch map[string]any) (APIKeyCredential, error) {
	credential, meta, err := s.apiKeyCredentialMeta(ctx, siteID, credentialID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	if len(patch) == 0 {
		return s.apiKeyCredentialFromStore(credential)
	}

	for key, value := range patch {
		meta[key] = value
	}
	updated, err := updateCredentialMeta(ctx, store.NewSiteCredentialRepository(s.db.DB()), credential.ID, meta)
	if err != nil {
		return APIKeyCredential{}, err
	}

	return s.apiKeyCredentialFromStore(updated)
}

func (s *Service) SetAPIKeyModelEnabled(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, modelName string, enabled bool) (APIKeyCredential, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return APIKeyCredential{}, fmt.Errorf("model name is required")
	}
	defer s.siteLocks.lock(siteID)()

	credential, meta, err := s.apiKeyCredentialMeta(ctx, siteID, credentialID)
	if err != nil {
		return APIKeyCredential{}, err
	}

	disabledModels := stringSetFromAny(meta["disabled_models"])
	if enabled {
		delete(disabledModels, modelName)
	} else {
		disabledModels[modelName] = struct{}{}
	}
	meta["disabled_models"] = sortedStringSet(disabledModels)
	updatedAPIKeyModel, _ := store.NewSiteAPIKeyModelRepository(s.db.DB()).UpdateEnabled(ctx, credential.ID, modelName, enabled)
	_ = s.syncSiteModelStatusFromAPIKeyModels(ctx, siteID, modelName, updatedAPIKeyModel)

	updated, err := updateCredentialMeta(ctx, store.NewSiteCredentialRepository(s.db.DB()), credential.ID, meta)
	if err != nil {
		return APIKeyCredential{}, err
	}

	return s.apiKeyCredentialFromStore(updated)
}

func (s *Service) syncSiteModelStatusFromAPIKeyModels(ctx context.Context, siteID uuid.UUID, modelName string, updatedAPIKeyModel store.SiteAPIKeyModel) error {
	apiKeyModelRepo := store.NewSiteAPIKeyModelRepository(s.db.DB())
	siteModelRepo := store.NewSiteModelRepository(s.db.DB())

	apiKeyModels, err := apiKeyModelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}

	matchingAPIKeyModels := make([]store.SiteAPIKeyModel, 0)
	for _, item := range apiKeyModels {
		if !item.Available {
			continue
		}
		if updatedAPIKeyModel.SiteModelID.Valid && item.SiteModelID.Valid && item.SiteModelID.UUID == updatedAPIKeyModel.SiteModelID.UUID {
			matchingAPIKeyModels = append(matchingAPIKeyModels, item)
			continue
		}
		if strings.TrimSpace(item.UpstreamModelName) == modelName {
			matchingAPIKeyModels = append(matchingAPIKeyModels, item)
		}
	}
	if len(matchingAPIKeyModels) == 0 {
		return nil
	}

	status := "disabled"
	for _, item := range matchingAPIKeyModels {
		if item.Enabled {
			status = "active"
			break
		}
	}

	siteModels, err := siteModelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	for _, model := range siteModels {
		if updatedAPIKeyModel.SiteModelID.Valid && model.ID == updatedAPIKeyModel.SiteModelID.UUID {
			_, err := siteModelRepo.UpdateStatus(ctx, siteID, model.ID, status)
			return err
		}
		if strings.TrimSpace(model.UpstreamName) == modelName {
			_, err := siteModelRepo.UpdateStatus(ctx, siteID, model.ID, status)
			return err
		}
	}

	return nil
}

func (s *Service) SetAPIKeySecret(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, secret string) (APIKeyCredential, error) {
	credential, meta, err := s.apiKeyCredentialMeta(ctx, siteID, credentialID)
	if err != nil {
		return APIKeyCredential{}, err
	}
	if strings.TrimSpace(secret) == "" {
		return APIKeyCredential{}, fmt.Errorf("api key must not be empty")
	}

	meta["raw_key_missing"] = false
	meta["manually_completed"] = true
	delete(meta, "upstream_masked_key")

	updated, err := s.saveCredentialInput(ctx, store.NewSiteCredentialRepository(s.db.DB()), siteID, CredentialInput{
		Type:   credential.CredentialType,
		Secret: secret,
		Meta:   meta,
	})
	if err != nil {
		return APIKeyCredential{}, err
	}

	return s.apiKeyCredentialFromStore(updated)
}

func (s *Service) apiKeyCredentialMeta(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) (store.SiteCredential, map[string]any, error) {
	credential, err := store.NewSiteCredentialRepository(s.db.DB()).GetByID(ctx, credentialID)
	if err != nil {
		return store.SiteCredential{}, nil, err
	}
	if credential.SiteID != siteID {
		return store.SiteCredential{}, nil, fmt.Errorf("site credential was not found")
	}
	if credential.CredentialType != defaultCredentialType && !strings.HasPrefix(credential.CredentialType, defaultCredentialType+":") {
		return store.SiteCredential{}, nil, fmt.Errorf("site credential is not an api key")
	}

	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}

	return credential, meta, nil
}

func (s *Service) apiKeyCredentialFromStore(credential store.SiteCredential) (APIKeyCredential, error) {
	secret, err := s.credentials.Decrypt(credential.EncryptedSecret)
	if err != nil {
		return APIKeyCredential{}, err
	}

	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}
	name, _ := meta["name"].(string)
	upstreamID := 0
	if value, ok := meta["upstream_id"].(float64); ok {
		upstreamID = int(value)
	}

	return APIKeyCredential{
		Credential:             credential,
		Secret:                 secretFromCredentialMeta(secret, meta),
		Name:                   strings.TrimSpace(name),
		DisplayName:            strings.TrimSpace(credential.DisplayName),
		UpstreamID:             upstreamID,
		Enabled:                boolFromMeta(meta, "enabled", true),
		MaskedSecret:           maskedSecretFromCredentialMeta(credential.MaskedSecret, meta),
		SecretMissing:          boolFromMeta(meta, "raw_key_missing", false),
		RoutingPriority:        credentialRoutingPriority(credential),
		UpstreamCostMultiplier: credentialUpstreamCostMultiplier(credential),
		Meta:                   meta,
	}, nil
}

func (s *Service) SystemAuth(ctx context.Context, siteID uuid.UUID) (adapter.SystemAuth, error) {
	site, err := s.Get(ctx, siteID)
	if err != nil {
		return adapter.SystemAuth{}, err
	}
	if normalizeSiteType(site.SiteType) == "codex" {
		connection, err := s.oauth.EnsureCodexConnectionFresh(ctx, siteID)
		if err != nil {
			return adapter.SystemAuth{}, err
		}
		return adapter.SystemAuth{
			Provider:     "codex",
			ConnectionID: connection.Connection.ID,
			AccessToken:  connection.AccessToken,
			RefreshToken: connection.RefreshToken,
			IDToken:      connection.IDToken,
			AccountID:    connection.AccountID,
			Email:        connection.Email,
			ExpiresAt: func() time.Time {
				if connection.Connection.ExpiresAt.Valid {
					return connection.Connection.ExpiresAt.Time
				}
				return time.Time{}
			}(),
			Metadata: connection.Metadata,
		}, nil
	}
	if normalizeSiteType(site.SiteType) == "antigravity" {
		connection, err := s.oauth.EnsureAntigravityConnectionFresh(ctx, siteID)
		if err != nil {
			return adapter.SystemAuth{}, err
		}
		return adapter.SystemAuth{
			Provider:     "antigravity",
			ConnectionID: connection.Connection.ID,
			AccessToken:  connection.AccessToken,
			RefreshToken: connection.RefreshToken,
			AccountID:    connection.AccountID,
			Email:        connection.Email,
			ExpiresAt: func() time.Time {
				if connection.Connection.ExpiresAt.Valid {
					return connection.Connection.ExpiresAt.Time
				}
				return time.Time{}
			}(),
			Metadata: connection.Metadata,
		}, nil
	}
	if normalizeSiteType(site.SiteType) == "claude_code" {
		connection, err := s.oauth.EnsureClaudeCodeConnectionFresh(ctx, siteID)
		if err != nil {
			return adapter.SystemAuth{}, err
		}
		return adapter.SystemAuth{
			Provider:     "claude_code",
			ConnectionID: connection.Connection.ID,
			AccessToken:  connection.AccessToken,
			RefreshToken: connection.RefreshToken,
			AccountID:    connection.AccountID,
			Email:        connection.Email,
			ExpiresAt: func() time.Time {
				if connection.Connection.ExpiresAt.Valid {
					return connection.Connection.ExpiresAt.Time
				}
				return time.Time{}
			}(),
			Metadata: connection.Metadata,
		}, nil
	}

	systemCredentialType := newAPIAccessTokenCredential
	if normalizeSiteType(site.SiteType) == "xlyra" {
		systemCredentialType = xlyraAccessTokenCredential
	}
	credential, err := store.NewSiteCredentialRepository(s.db.DB()).GetBySiteAndType(ctx, siteID, systemCredentialType)
	if err != nil {
		return adapter.SystemAuth{}, err
	}

	accessToken, err := s.credentials.Decrypt(credential.EncryptedSecret)
	if err != nil {
		return adapter.SystemAuth{}, err
	}

	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}
	userID := 0
	if value, ok := meta["user_id"].(float64); ok {
		userID = int(value)
	}

	return adapter.SystemAuth{
		AccessToken: accessToken,
		UserID:      userID,
	}, nil
}

func (s *Service) APIKey(ctx context.Context, siteID uuid.UUID) (string, error) {
	_, apiKey, err := s.siteWithAPIKey(ctx, siteID)
	return apiKey, err
}

func (s *Service) siteWithAPIKey(ctx context.Context, siteID uuid.UUID) (store.Site, string, error) {
	site, apiKey, err := s.siteWithAPIKeyCredential(ctx, siteID)
	if err != nil {
		return store.Site{}, "", err
	}
	return site, apiKey.Secret, nil
}

func (s *Service) siteWithAPIKeyCredential(ctx context.Context, siteID uuid.UUID) (store.Site, APIKeyCredential, error) {
	site, err := s.Get(ctx, siteID)
	if err != nil {
		return store.Site{}, APIKeyCredential{}, err
	}

	apiKeys, err := s.APIKeys(ctx, siteID)
	if err != nil {
		return store.Site{}, APIKeyCredential{}, err
	}
	for _, apiKey := range apiKeys {
		if apiKey.Enabled && strings.TrimSpace(apiKey.Secret) != "" {
			return site, apiKey, nil
		}
	}
	if len(apiKeys) > 0 {
		return site, apiKeys[0], nil
	}

	return store.Site{}, APIKeyCredential{}, fmt.Errorf("get site credential: %w", gorm.ErrRecordNotFound)
}

func (s *Service) toAdapterSite(ctx context.Context, site store.Site) adapter.SiteConfig {
	meta := map[string]any{}
	if len(site.Meta) > 0 {
		_ = json.Unmarshal(site.Meta, &meta)
	}
	client, err := s.httpClientForSite(ctx, site, false)
	if err != nil {
		client = &http.Client{Transport: errorRoundTripper{err: err}}
	}
	return adapter.SiteConfig{
		ID:       site.ID.String(),
		Name:     site.Name,
		SiteType: site.SiteType,
		BaseURL:  site.BaseURL,
		Meta:     meta,
		Client:   client,
	}
}

type errorRoundTripper struct {
	err error
}

func (r errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, r.err
}

func (s *Service) httpClientForSite(_ context.Context, item store.Site, stream bool) (*http.Client, error) {
	proxyID := proxyIDFromMeta(item.Meta)
	profile := httpclientProfileFromGatewayConfig(GatewayConfigFromSiteMeta(item.Meta), proxyID)
	if stream {
		profile = httpclient.StreamingProfile(profile)
	}
	return s.httpClients.Client(profile)
}

func (s *Service) validateProxyID(proxyID *string) error {
	if proxyID == nil || strings.TrimSpace(*proxyID) == "" {
		return nil
	}
	id := strings.TrimSpace(*proxyID)
	for _, item := range httpclient.ReadNetworkConfig(s.confFile).Proxies {
		if item.ID == id {
			return nil
		}
	}
	return fmt.Errorf("proxy_id %q was not found", id)
}

func httpclientProfileFromGatewayConfig(cfg *GatewayConfig, proxyID string) httpclient.Profile {
	profile := httpclient.DefaultProfile()
	profile.ProxyID = proxyID
	if cfg == nil {
		return profile
	}
	if cfg.RequestTimeoutMS != nil {
		profile.RequestTimeout = time.Duration(*cfg.RequestTimeoutMS) * time.Millisecond
	}
	if cfg.ConnectTimeoutMS != nil {
		profile.ConnectTimeout = time.Duration(*cfg.ConnectTimeoutMS) * time.Millisecond
	}
	if cfg.ResponseHeaderTimeoutMS != nil {
		profile.ResponseHeaderTimeout = time.Duration(*cfg.ResponseHeaderTimeoutMS) * time.Millisecond
	}
	if cfg.MaxIdleConns != nil {
		profile.MaxIdleConns = *cfg.MaxIdleConns
	}
	if cfg.MaxIdleConnsPerHost != nil {
		profile.MaxIdleConnsPerHost = *cfg.MaxIdleConnsPerHost
	}
	if cfg.MaxConnsPerHost != nil {
		profile.MaxConnsPerHost = *cfg.MaxConnsPerHost
	}
	if cfg.IdleConnTimeoutMS != nil {
		profile.IdleConnTimeout = time.Duration(*cfg.IdleConnTimeoutMS) * time.Millisecond
	}
	return profile
}

func proxyIDFromMeta(raw []byte) string {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	if value, ok := meta["proxy_id"].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func normalizeSiteType(siteType string) string {
	if strings.TrimSpace(siteType) == "" {
		return "openai"
	}

	return strings.TrimSpace(siteType)
}

// CredentialTypeForSiteType returns the credential behavior category for a site type:
// "api_key" for OpenAI-compatible providers, "system_token" for newapi, "xlyra" for xLyra, "oauth" for OAuth providers.
func CredentialTypeForSiteType(siteType string) string {
	switch strings.TrimSpace(strings.ToLower(siteType)) {
	case "newapi":
		return "system_token"
	case "xlyra":
		return "xlyra"
	case "codex", "antigravity", "claude_code":
		return "oauth"
	case "grok":
		return "grok_sso"
	default:
		return "api_key"
	}
}

func SupportsMultipleAPIKeys(siteType string) bool {
	return CredentialTypeForSiteType(siteType) == "api_key"
}

func SupportsAPIKeyCostMultiplier(siteType string) bool {
	return strings.EqualFold(normalizeSiteType(siteType), "openai")
}

func normalizeCredentialType(credentialType string) string {
	if strings.TrimSpace(credentialType) == "" {
		return defaultCredentialType
	}

	return strings.TrimSpace(credentialType)
}

func normalizeRoutingPriorityPointer(value *float64) (float64, error) {
	if value == nil {
		return 1.0, nil
	}
	return normalizeRoutingPriority(*value)
}

func normalizeRoutingPriorityWithFallback(value *float64, fallback float64) (float64, error) {
	if value == nil {
		return fallback, nil
	}
	return normalizeRoutingPriority(*value)
}

func normalizeRoutingPriority(value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 1.0 || value > 5.0 {
		return 0, fmt.Errorf("routing_priority must be between 1.0 and 5.0")
	}

	scaled := value * 10
	if math.Abs(scaled-math.Round(scaled)) > 1e-9 {
		return 0, fmt.Errorf("routing_priority must have at most 1 decimal place")
	}

	return math.Round(scaled) / 10, nil
}

func normalizeAPIKeyDisplayName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len([]rune(value)) > 100 {
		return "", fmt.Errorf("name must be at most 100 characters")
	}
	return value, nil
}

func normalizeUpstreamCostMultiplierPointer(value *float64, fallback float64) (float64, error) {
	if value == nil {
		return fallback, nil
	}
	return normalizeUpstreamCostMultiplier(*value)
}

func normalizeUpstreamCostMultiplier(value float64) (float64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0.01 || value > 100 {
		return 0, fmt.Errorf("upstream_cost_multiplier must be between 0.01 and 100.00")
	}
	scaled := value * 10000
	if math.Abs(scaled-math.Round(scaled)) > 1e-9 {
		return 0, fmt.Errorf("upstream_cost_multiplier must have at most 4 decimal places")
	}
	return math.Round(scaled) / 10000, nil
}

func credentialRoutingPriority(credential store.SiteCredential) float64 {
	return store.SiteCredentialRoutingPriority(credential)
}

func credentialUpstreamCostMultiplier(credential store.SiteCredential) float64 {
	return store.SiteCredentialUpstreamCostMultiplier(credential)
}

func credentialInputs(primary *CredentialInput, credentials []CredentialInput) []CredentialInput {
	result := make([]CredentialInput, 0, len(credentials)+1)
	if primary != nil {
		result = append(result, *primary)
	}
	result = append(result, credentials...)
	return result
}

func prepareCreateCredentialInputs(siteType string, inputs []CredentialInput) ([]CredentialInput, error) {
	result := append([]CredentialInput(nil), inputs...)
	apiKeyCount := 0
	for _, input := range result {
		credentialType := normalizeCredentialType(input.Type)
		if credentialType == defaultCredentialType || strings.HasPrefix(credentialType, defaultCredentialType+":") {
			apiKeyCount++
		}
	}
	if apiKeyCount > 1 && !SupportsMultipleAPIKeys(siteType) {
		return nil, fmt.Errorf("site type %q does not support multiple api keys", siteType)
	}
	for index := range result {
		credentialType := normalizeCredentialType(result[index].Type)
		isAPIKey := credentialType == defaultCredentialType || strings.HasPrefix(credentialType, defaultCredentialType+":")
		if apiKeyCount > 1 && credentialType == defaultCredentialType {
			result[index].Type = scopedAPIKeyCredentialType()
		}
		if !isAPIKey && (result[index].DisplayName != nil || result[index].RoutingPriority != nil || result[index].UpstreamCostMultiplier != nil) {
			return nil, fmt.Errorf("credential type %q does not support api key routing configuration", credentialType)
		}
		if result[index].DisplayName != nil {
			value, err := normalizeAPIKeyDisplayName(*result[index].DisplayName)
			if err != nil {
				return nil, err
			}
			result[index].DisplayName = &value
		}
		if result[index].RoutingPriority != nil {
			value, err := normalizeRoutingPriority(*result[index].RoutingPriority)
			if err != nil {
				return nil, err
			}
			result[index].RoutingPriority = &value
		}
		if result[index].UpstreamCostMultiplier != nil {
			if !SupportsAPIKeyCostMultiplier(siteType) {
				return nil, fmt.Errorf("site type %q does not support api key cost multiplier", siteType)
			}
			value, err := normalizeUpstreamCostMultiplier(*result[index].UpstreamCostMultiplier)
			if err != nil {
				return nil, err
			}
			result[index].UpstreamCostMultiplier = &value
		}
	}
	return result, nil
}

func hasTokenScopedCredentials(credentials []store.SiteCredential) bool {
	for _, credential := range credentials {
		if strings.HasPrefix(credential.CredentialType, defaultCredentialType+":") {
			return true
		}
	}

	return false
}

func migrateDefaultAPIKeyCredential(ctx context.Context, repo store.SiteCredentialRepository, credentials []store.SiteCredential) error {
	for _, credential := range credentials {
		if credential.CredentialType != defaultCredentialType {
			continue
		}
		_, err := repo.UpdateCredentialType(ctx, credential.ID, scopedAPIKeyCredentialType())
		return err
	}
	return nil
}

func scopedAPIKeyCredentialType() string {
	return defaultCredentialType + ":" + uuid.NewString()
}

func coalesceModelCapabilities(existing adapter.Model, next adapter.Model, keyName string) adapter.Model {
	if existing.UpstreamName == "" {
		if next.Capabilities == nil {
			next.Capabilities = map[string]any{}
		}
		if strings.TrimSpace(stringFromMap(next.Capabilities, "source")) == "" {
			next.Capabilities["source"] = "upstream"
		}
		if keyName != "" {
			next.Capabilities["first_api_key"] = keyName
		}
		return next
	}

	if existing.Capabilities == nil {
		existing.Capabilities = map[string]any{}
	}
	return existing
}

func (s *Service) enrichModelCapabilities(ctx context.Context, site store.Site, model adapter.Model) adapter.Model {
	model = applyModelNameEndpointTypes(site, model)
	if s == nil || s.modelCaps == nil {
		return model
	}
	if strings.TrimSpace(model.UpstreamName) == "" {
		return model
	}

	provider := capabilityProviderForSite(site.SiteType)
	if provider == "" {
		return model
	}

	result := s.modelCaps.Enrich(ctx, modelcapabilities.Input{
		Provider:         provider,
		SiteType:         site.SiteType,
		ModelID:          model.UpstreamName,
		BaseCapabilities: model.Capabilities,
	})
	if len(result.Capabilities) == 0 {
		return model
	}
	model.Capabilities = result.Capabilities
	return applyModelNameEndpointTypes(site, model)
}

func applyModelNameEndpointTypes(site store.Site, model adapter.Model) adapter.Model {
	if !modelcapabilities.UsesModelNameEndpointInference(site.SiteType) || strings.TrimSpace(model.UpstreamName) == "" {
		return model
	}
	if strings.EqualFold(stringFromMap(model.Capabilities, "source"), "grok") && len(stringSliceFromPricingRaw(model.Capabilities["supported_endpoint_types"])) > 0 {
		return model
	}
	if model.Capabilities == nil {
		model.Capabilities = map[string]any{}
	}
	model.Capabilities["supported_endpoint_types"] = modelcapabilities.InferEndpointTypesForSiteModel(model.UpstreamName, site.SiteType, site.BaseURL)
	return model
}

func capabilityProviderForSite(siteType string) string {
	switch strings.TrimSpace(siteType) {
	case "openai":
		return "openai"
	case "deepseek":
		return "openai"
	case "minimax":
		return "openai"
	case "newapi":
		return "openai"
	case "xiaomi_mimo":
		return "xiaomi_mimo"
	case "moonshot":
		return "openai"
	case "kimi_code":
		return "openai"
	case "zhipu", "glm_code":
		return "zhipuai"
	case "codex":
		return "openai"
	case "antigravity":
		return "google"
	case "claude_code":
		return "anthropic"
	case "anthropic":
		return "anthropic"
	default:
		return ""
	}
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}

	return append(values, value)
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	text, _ := values[key].(string)
	return strings.TrimSpace(text)
}

func updateCredentialMeta(ctx context.Context, repo store.SiteCredentialRepository, id uuid.UUID, meta map[string]any) (store.SiteCredential, error) {
	encoded, err := json.Marshal(meta)
	if err != nil {
		return store.SiteCredential{}, fmt.Errorf("marshal credential meta: %w", err)
	}

	return repo.UpdateMeta(ctx, id, encoded)
}

func boolFromMeta(meta map[string]any, key string, fallback bool) bool {
	value, ok := meta[key]
	if !ok {
		return fallback
	}
	boolValue, ok := value.(bool)
	if !ok {
		return fallback
	}

	return boolValue
}

func secretFromCredentialMeta(secret string, meta map[string]any) string {
	if boolFromMeta(meta, "raw_key_missing", false) {
		return ""
	}

	return secret
}

func maskedSecretFromCredentialMeta(maskedSecret string, meta map[string]any) string {
	if boolFromMeta(meta, "raw_key_missing", false) {
		if upstreamMasked, _ := meta["upstream_masked_key"].(string); strings.TrimSpace(upstreamMasked) != "" {
			return strings.TrimSpace(upstreamMasked)
		}
	}

	return maskedSecret
}

func stringSetFromAny(value any) map[string]struct{} {
	result := map[string]struct{}{}
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				result[text] = struct{}{}
			}
		}
	case []string:
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item != "" {
				result[item] = struct{}{}
			}
		}
	}

	return result
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func mergeStringMeta(existing []byte, key string, value *string) []byte {
	if value == nil {
		return existing
	}

	root := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		delete(root, key)
	} else {
		root[key] = trimmed
	}

	encoded, err := json.Marshal(root)
	if err != nil {
		return existing
	}
	return encoded
}

func mergeRequestHeadersMeta(existing []byte, headers map[string]string) []byte {
	if len(headers) == 0 {
		return existing
	}

	root := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &root)
	}
	if root == nil {
		root = map[string]any{}
	}

	items := make([]map[string]string, 0, len(headers))
	for k, v := range headers {
		items = append(items, map[string]string{"key": k, "value": v})
	}
	root["request_headers"] = items

	encoded, err := json.Marshal(root)
	if err != nil {
		return existing
	}
	return encoded
}
