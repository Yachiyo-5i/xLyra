package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/catalog"
	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/protocolspec"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

type RefreshResult struct {
	Site          store.Site
	State         store.SiteState
	Models        []store.SiteModel
	APIKeyStates  []store.SiteAPIKeyState
	APIKeyModels  []store.SiteAPIKeyModel
	PricingGroups []store.SitePricingGroup
	ModelPricings []store.SiteModelPricing
}

type RefreshBatchItem struct {
	Result     RefreshResult
	Err        error
	Skipped    bool
	SkipReason string
}

type refreshKeyModel struct {
	model adapter.Model
}

type refreshKey struct {
	credential   store.SiteCredential
	state        store.SiteAPIKeyState
	name         string
	models       []refreshKeyModel
	modelsSynced bool
}

type preparedRefreshKey struct {
	freshKey       adapter.APIKey
	credential     store.SiteCredential
	hasRawKey      bool
	resolvedAPIKey string
	enabled        bool
	usage          any
}

type refreshKeySummary struct {
	usage        any
	models       []refreshKeyModel
	modelsSynced bool
	syncStatus   string
	syncMessage  any
	keyError     bool
}

const refreshKeySummaryWorkers = 4

const siteMetaAutoDisabledByRefresh = "auto_disabled_by_refresh"
const credentialMetaAutoDisabledByRefresh = "auto_disabled_by_refresh"

func (s *Service) RefreshState(ctx context.Context, siteID uuid.UUID) (RefreshResult, error) {
	defer s.siteLocks.lock(siteID)()

	site, err := s.Get(ctx, siteID)
	if err != nil {
		return RefreshResult{}, err
	}

	stateRepo := store.NewSiteStateRepository(s.db.DB())
	_, _ = stateRepo.MarkSyncStarted(ctx, siteID)

	module, ok := s.adapters.ModuleForSiteType(site.SiteType)
	if !ok {
		return s.markRefreshFailed(ctx, site, fmt.Sprintf("unsupported site_type %q", site.SiteType))
	}

	var result RefreshResult
	var refreshErr error
	if _, ok := adapter.AsAPIKeyInventoryFetcher(module); ok {
		if _, ok := adapter.AsAPIKeySummaryFetcher(module); ok {
			result, refreshErr = s.refreshCapabilityState(ctx, site, module)
			if refreshErr != nil && site.SiteType == "xlyra" && xlyraRefreshCanFallbackToAPIKey(refreshErr) {
				result, refreshErr = s.refreshModelOnlyState(ctx, site)
				if refreshErr == nil {
					if current, err := s.Get(ctx, site.ID); err == nil {
						result.Site = s.restoreAutoDisabledSiteAfterRefreshSuccess(ctx, current)
					}
				}
			}
		} else {
			result, refreshErr = s.refreshModelOnlyState(ctx, site)
		}
	} else {
		result, refreshErr = s.refreshModelOnlyState(ctx, site)
	}

	if refreshErr == nil && result.Site.ID != uuid.Nil {
		if matchedModels, err := catalog.NewService(s.db, s.confFile).MatchSiteModels(ctx, site.ID); err == nil {
			result.Models = availableSiteModels(matchedModels)
		}
		if unpriced := s.syncCanonicalPricingFallback(ctx, site.ID, result.Models); len(unpriced) > 0 {
			message := fmt.Sprintf("%d models have no pricing and will record zero cost: %s", len(unpriced), strings.Join(unpriced, ", "))
			if state, err := stateRepo.MarkPricingGap(ctx, site.ID, message); err == nil {
				result.State = state
			}
		}
	}

	if fresh, err := s.Get(ctx, siteID); err == nil {
		probed := s.runQuotaProbes(ctx, fresh)
		if refreshErr == nil {
			result.Site = probed
		}
	}

	return result, refreshErr
}

func xlyraRefreshCanFallbackToAPIKey(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "xlyra access token is required") || strings.Contains(message, "get site credential")
}

func (s *Service) RefreshAllStates(ctx context.Context) ([]RefreshBatchItem, error) {
	sites, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]RefreshBatchItem, 0, len(sites))
	for _, item := range sites {
		if skip, reason := s.shouldSkipBulkRefresh(ctx, item); skip {
			results = append(results, RefreshBatchItem{
				Result: RefreshResult{
					Site:  item,
					State: s.refreshStateSnapshot(ctx, item.ID, "skipped"),
				},
				Skipped:    true,
				SkipReason: reason,
			})
			continue
		}
		result, refreshErr := s.RefreshState(ctx, item.ID)
		if result.Site.ID == uuid.Nil {
			result.Site = item
		}
		results = append(results, RefreshBatchItem{
			Result: result,
			Err:    refreshErr,
		})
	}

	return results, nil
}

func (s *Service) shouldSkipBulkRefresh(ctx context.Context, item store.Site) (bool, string) {
	if !item.Enabled && !siteAutoDisabledByRefresh(item) {
		return true, "site is disabled; skipped by bulk refresh"
	}
	return s.shouldSkipBulkOAuthRefresh(ctx, item)
}

func (s *Service) shouldSkipBulkOAuthRefresh(ctx context.Context, item store.Site) (bool, string) {
	if CredentialTypeForSiteType(item.SiteType) != "oauth" {
		return false, ""
	}
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, item.ID)
	if err == nil && oauthConnectionRequiresManualRefresh(connection) {
		return true, "oauth connection requires manual reconnect; skipped by bulk refresh"
	}
	state, err := store.NewSiteStateRepository(s.db.DB()).GetBySite(ctx, item.ID)
	if err == nil && OAuthPermanentAuthRefreshError(item.SiteType, state.SyncMessage.String) {
		return true, "oauth site has previous 4xx auth refresh failure; skipped by bulk refresh"
	}
	return false, ""
}

func (s *Service) refreshStateSnapshot(ctx context.Context, siteID uuid.UUID, fallbackStatus string) store.SiteState {
	state, err := store.NewSiteStateRepository(s.db.DB()).GetBySite(ctx, siteID)
	if err == nil {
		return state
	}
	return store.SiteState{SiteID: siteID, SyncStatus: fallbackStatus}
}

func oauthConnectionRequiresManualRefresh(connection store.OAuthConnection) bool {
	if strings.EqualFold(strings.TrimSpace(connection.Status), "reconnect_required") {
		return true
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	return oauthPermanentAuthRefreshMessage(anyString(meta["last_error"])) ||
		oauthPermanentAuthRefreshMessage(oauthConnectionQuotaAuthMessage(connection.Provider, meta))
}

func OAuthPermanentAuthRefreshError(siteType string, message string) bool {
	if CredentialTypeForSiteType(siteType) != "oauth" {
		return false
	}
	return oauthPermanentAuthRefreshMessage(message)
}

func oauthPermanentAuthRefreshMessage(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if oauthTransientHTML403RefreshMessage(lower) {
		return false
	}
	if strings.Contains(lower, "refresh_token_reused") || strings.Contains(lower, "invalid_grant") {
		return true
	}
	return messageContainsAuthFailureCode(lower)
}

func oauthConnectionQuotaAuthMessage(provider string, meta map[string]any) string {
	quota, ok := meta["quota"].(map[string]any)
	if !ok || len(quota) == 0 {
		return ""
	}
	if available, ok := quota["available"].(bool); ok && available {
		return ""
	}
	message := firstNonEmptyString(
		anyString(quota["error"]),
		anyString(quota["message"]),
		anyString(quota["detail"]),
	)
	if !OAuthPermanentAuthRefreshError(provider, message) {
		return ""
	}
	return message
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func oauthTransientHTML403RefreshMessage(message string) bool {
	if !strings.Contains(message, "codex upstream returned 403") || !strings.Contains(message, "<html") {
		return false
	}
	return strings.Contains(message, `http-equiv="refresh"`) ||
		strings.Contains(message, `http-equiv='refresh'`) ||
		strings.Contains(message, "http-equiv=refresh") ||
		(strings.Contains(message, "<meta") && strings.Contains(message, "content=\"360\""))
}

// messageContainsAuthFailureCode reports whether an upstream error message
// carries an authentication-failure HTTP status (401/403). Only these justify
// auto-disabling a key/credential; other 4xx codes (400/404/408/429/…) are
// transient or non-auth and must not permanently disable a usable credential.
func messageContainsAuthFailureCode(message string) bool {
	prefixes := []string{
		"returned ", "status ", "status=", "status_code ", "http ", "responded with ",
	}
	for _, prefix := range prefixes {
		remaining := message
		for {
			idx := strings.Index(remaining, prefix)
			if idx < 0 {
				break
			}
			codeStart := idx + len(prefix)
			remaining = remaining[codeStart:]
			if len(remaining) < 3 {
				break
			}
			c0, c1, c2 := remaining[0], remaining[1], remaining[2]
			if c0 != '4' || c1 < '0' || c1 > '9' || c2 < '0' || c2 > '9' {
				continue
			}
			if len(remaining) > 3 && remaining[3] >= '0' && remaining[3] <= '9' {
				continue
			}
			if code := remaining[:3]; code == "401" || code == "403" {
				return true
			}
		}
	}
	return false
}

func (s *Service) refreshCapabilityState(ctx context.Context, item store.Site, module adapter.Module) (RefreshResult, error) {
	auth, err := s.SystemAuth(ctx, item.ID)
	if err != nil {
		return s.markRefreshFailed(ctx, item, err.Error())
	}

	keyInventoryFetcher, ok := adapter.AsAPIKeyInventoryFetcher(module)
	if !ok {
		return s.markRefreshFailed(ctx, item, fmt.Sprintf("site_type %q does not support api key inventory sync", item.SiteType))
	}
	keySummaryFetcher, ok := adapter.AsAPIKeySummaryFetcher(module)
	if !ok {
		return s.markRefreshFailed(ctx, item, fmt.Sprintf("site_type %q does not support api key summary sync", item.SiteType))
	}

	now := time.Now()
	freshKeys, err := keyInventoryFetcher.ListAPIKeys(ctx, s.toAdapterSite(ctx, item), auth)
	if err != nil {
		return s.markRefreshFailed(ctx, item, err.Error())
	}

	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	apiKeyStateRepo := store.NewSiteAPIKeyStateRepository(s.db.DB())
	apiKeyModelRepo := store.NewSiteAPIKeyModelRepository(s.db.DB())
	siteModelRepo := store.NewSiteModelRepository(s.db.DB())
	pricingGroupRepo := store.NewSitePricingGroupRepository(s.db.DB())
	modelPricingRepo := store.NewSiteModelPricingRepository(s.db.DB())

	syncStatus := "synced"
	siteMessages := make([]string, 0)
	keyMessages := make([]string, 0)
	var userSummary adapter.UserSummary
	if summaryFetcher, ok := adapter.AsUserSummaryFetcher(module); ok {
		if summary, err := summaryFetcher.FetchUserSummary(ctx, s.toAdapterSite(ctx, item), auth); err == nil {
			userSummary = summary
		} else {
			syncStatus = "partial"
			siteMessages = append(siteMessages, err.Error())
		}
	}

	preparedKeys := make([]preparedRefreshKey, 0, len(freshKeys))
	resolvedUsableKeyCount := 0
	for _, freshKey := range freshKeys {
		hasRawKey := strings.TrimSpace(freshKey.Key) != ""
		if !hasRawKey && strings.TrimSpace(freshKey.MaskedKey) == "" && freshKey.ID <= 0 && strings.TrimSpace(freshKey.ExternalID) == "" && strings.TrimSpace(freshKey.Name) == "" {
			continue
		}

		credentialType := apiKeyCredentialTypeForRefresh(freshKey)
		credentialInput, resolvedHasRawKey, err := s.apiKeyCredentialInputForRefresh(ctx, credentialRepo, item.ID, credentialType, freshKey)
		if err != nil {
			return s.markRefreshFailed(ctx, item, err.Error())
		}
		hasRawKey = resolvedHasRawKey
		resolvedAPIKey := strings.TrimSpace(credentialInput.Secret)
		if hasRawKey {
			resolvedUsableKeyCount++
		}

		credential, err := s.saveCredentialInput(ctx, credentialRepo, item.ID, credentialInput)
		if err != nil {
			return s.markRefreshFailed(ctx, item, err.Error())
		}

		enabled := credentialEnabledFromMeta(credential.Meta)
		var usage any = usageFromTokenRaw(freshKey.Raw)
		preparedKeys = append(preparedKeys, preparedRefreshKey{
			freshKey:       freshKey,
			credential:     credential,
			hasRawKey:      hasRawKey,
			resolvedAPIKey: resolvedAPIKey,
			enabled:        enabled,
			usage:          usage,
		})
	}
	if err := markMissingManagedAPIKeysStale(ctx, item.ID, credentialRepo, apiKeyStateRepo, apiKeyModelRepo, preparedKeys, now); err != nil {
		return s.markRefreshFailed(ctx, item, err.Error())
	}

	keyErrors := 0
	permanentKeyErrors := 0
	enabledSummaryCount := 0
	lastKeyErrorMessage := ""
	recoverableKeyErrorMessage := ""
	summaries := s.summarizePreparedRefreshKeys(ctx, item, keySummaryFetcher, preparedKeys)
	refreshedKeys := make([]refreshKey, 0, len(preparedKeys))
	codexSite := normalizeSiteType(item.SiteType) == "codex"
	var codexAdapterSite adapter.SiteConfig
	if codexSite {
		codexAdapterSite = s.toAdapterSite(ctx, item)
	}
	for index, preparedKey := range preparedKeys {
		freshKey := preparedKey.freshKey
		credential := preparedKey.credential
		summary := summaries[index]
		credentialMeta := map[string]any{}
		_ = json.Unmarshal(credential.Meta, &credentialMeta)
		autoDisabledCredential := boolFromMeta(credentialMeta, credentialMetaAutoDisabledByRefresh, false)
		recoveredCredential := false
		if summary.keyError {
			keyErrors++
			lastKeyErrorMessage = anyString(summary.syncMessage)
			failure := upstream.ClassifyMessage(lastKeyErrorMessage)
			if failure.CredentialInvalid() {
				permanentKeyErrors++
				preparedKey.enabled = false
				credentialMeta["enabled"] = false
				credentialMeta[credentialMetaAutoDisabledByRefresh] = true
				credential.Meta = store.JSON(jsonBytes(credentialMeta))
				if _, metaErr := updateCredentialMeta(ctx, credentialRepo, credential.ID, credentialMeta); metaErr != nil {
					return s.markRefreshFailed(ctx, item, fmt.Sprintf("mark api key disabled after auth failure: %v", metaErr))
				}
			} else {
				recoverableKeyErrorMessage = lastKeyErrorMessage
			}
		} else if refreshKeySummaryValidated(preparedKey, summary) && siteAutoDisabledByRefresh(item) && autoDisabledCredential {
			preparedKey.enabled = true
			recoveredCredential = true
			credentialMeta["enabled"] = true
			delete(credentialMeta, credentialMetaAutoDisabledByRefresh)
			credential.Meta = store.JSON(jsonBytes(credentialMeta))
			if _, metaErr := updateCredentialMeta(ctx, credentialRepo, credential.ID, credentialMeta); metaErr != nil {
				return s.markRefreshFailed(ctx, item, fmt.Sprintf("restore api key after auth recovery: %v", metaErr))
			}
		}
		if preparedKey.enabled && refreshKeySummaryValidated(preparedKey, summary) {
			enabledSummaryCount++
		}

		state, err := apiKeyStateRepo.Upsert(ctx, store.UpsertSiteAPIKeyStateParams{
			SiteCredentialID:  credential.ID,
			SiteID:            item.ID,
			UpstreamID:        nullableInt(freshKey.ID),
			Name:              defaultString(freshKey.Name, credential.MaskedSecret),
			UpstreamStatus:    jsonBytes(freshKey.Status),
			Enabled:           preparedKey.enabled,
			GroupName:         nullableString(anyString(freshKey.Raw["group"])),
			RemainQuota:       nullableInt64(int64FromAny(freshKey.Raw["remain_quota"])),
			UsedQuota:         nullableInt64(int64FromAny(freshKey.Raw["used_quota"])),
			UnlimitedQuota:    boolFromAny(freshKey.Raw["unlimited_quota"]),
			ExpiredTime:       nullableInt64(int64FromAny(freshKey.Raw["expired_time"])),
			ModelLimitsEnable: boolFromAny(freshKey.Raw["model_limits_enabled"]),
			ModelLimits:       modelLimitsJSON(freshKey.Raw["model_limits"]),
			Usage:             jsonBytes(summary.usage),
			Raw:               jsonBytes(freshKey.Raw),
			SyncStatus:        summary.syncStatus,
			SyncMessage:       summary.syncMessage,
			LastSyncedAt:      now,
		})
		if err != nil {
			return s.markRefreshFailed(ctx, item, err.Error())
		}
		if recoveredCredential {
			state, err = restoreAPIKeyStateAfterAuthRecovery(ctx, apiKeyStateRepo, credential.ID)
			if err != nil {
				return s.markRefreshFailed(ctx, item, err.Error())
			}
		}

		if codexSite {
			s.recoverCodexQuotaCooldown(ctx, item.ID, credential.ID, summary.usage)
			s.primeCodexWindow(ctx, codexAdapterSite, auth, summary.usage, summary.models)
		}
		if refreshKeySummaryValidated(preparedKey, summary) {
			s.recoverCredentialCooldownAfterRefresh(ctx, item.ID, credential.ID)
		}

		refreshed := refreshKey{
			credential:   credential,
			state:        state,
			name:         defaultString(state.Name, credential.MaskedSecret),
			models:       summary.models,
			modelsSynced: summary.modelsSynced,
		}
		refreshedKeys = append(refreshedKeys, refreshed)
	}
	if resolvedUsableKeyCount > 0 && enabledSummaryCount == 0 {
		message := lastKeyErrorMessage
		if recoverableKeyErrorMessage != "" || permanentKeyErrors < keyErrors {
			message = recoverableKeyErrorMessage
		}
		if strings.TrimSpace(message) == "" {
			message = "no enabled api keys returned a usable summary"
		} else if keyErrors > 0 {
			message = fmt.Sprintf("all %d enabled api keys failed: %s", keyErrors, message)
		}
		return s.markRefreshFailed(ctx, item, message)
	}

	if modelcapabilities.UsesModelNameEndpointInference(item.SiteType) {
		for keyIndex := range refreshedKeys {
			for modelIndex := range refreshedKeys[keyIndex].models {
				refreshedKeys[keyIndex].models[modelIndex].model = s.enrichModelCapabilities(ctx, item, refreshedKeys[keyIndex].models[modelIndex].model)
			}
		}
	}

	siteModels := []store.SiteModel{}
	apiKeyModels := []store.SiteAPIKeyModel{}
	if resolvedUsableKeyCount > 0 {
		siteModels, apiKeyModels, err = syncModelState(ctx, item.ID, refreshedKeys, siteModelRepo, apiKeyModelRepo)
		if err != nil {
			return s.markRefreshFailed(ctx, item, err.Error())
		}
		if len(siteModels) == 0 {
			return s.markRefreshFailed(ctx, item, "enabled api keys returned no usable models")
		}
	} else if userSummary.UserModels != nil {
		siteModels, err = s.syncUserModelsState(ctx, item, userSummary.UserModels, siteModelRepo)
		if err != nil {
			return s.markRefreshFailed(ctx, item, err.Error())
		}
	}

	if keyErrors > 0 {
		syncStatus = "partial"
		keyMessages = append(keyMessages, fmt.Sprintf("%d api key summaries failed", keyErrors))
	}
	allKeysAuthFailed := keyErrors > 0 && keyErrors == len(preparedKeys)
	if allKeysAuthFailed {
		siteMessages = append(siteMessages, fmt.Sprintf("all %d api keys returned auth errors", keyErrors))
	}
	if resolvedUsableKeyCount == 0 {
		syncStatus = "partial"
		keyMessages = append(keyMessages, "site did not expose raw api key values; api key summary sync skipped")
	}
	pricingSnapshot := adapter.PricingSnapshot{}
	if userSummary.Pricing != nil {
		if parser, ok := adapter.AsPricingParser(module); ok {
			pricingSnapshot = parser.ParsePricing(userSummary.Pricing)
		}
	}
	if len(pricingSnapshot.Items) == 0 {
		if pricingFetcher, ok := adapter.AsPricingFetcher(module); ok {
			if snapshot, err := pricingFetcher.FetchPricing(ctx, s.toAdapterSite(ctx, item), auth); err == nil {
				pricingSnapshot = snapshot
			} else {
				syncStatus = "partial"
				siteMessages = append(siteMessages, err.Error())
			}
		}
	}
	if len(pricingSnapshot.Items) > 0 {
		siteModels, err = s.mergePricingModelsState(ctx, item, pricingSnapshot, siteModels, siteModelRepo)
		if err != nil {
			return s.markRefreshFailed(ctx, item, err.Error())
		}
	}
	if len(siteModels) == 0 {
		if existingModels, err := siteModelRepo.ListBySite(ctx, item.ID); err == nil {
			siteModels = availableSiteModels(existingModels)
		}
	}

	pricingGroups := []store.SitePricingGroup{}
	modelPricings := []store.SiteModelPricing{}
	if len(pricingSnapshot.Items) > 0 || len(pricingSnapshot.Groups) > 0 {
		siteModelsByName := make(map[string]store.SiteModel, len(siteModels))
		for _, siteModel := range siteModels {
			siteModelsByName[siteModel.UpstreamName] = siteModel
			siteModelsByName[siteModel.DisplayName] = siteModel
		}
		groups, pricings, err := syncPricingState(ctx, item.ID, item.SiteType, pricingSnapshot, siteModelsByName, pricingGroupRepo, modelPricingRepo, now)
		if err != nil {
			syncStatus = "partial"
			siteMessages = append(siteMessages, err.Error())
		}
		pricingGroups = groups
		modelPricings = pricings
	}

	allMessages := make([]string, 0, len(siteMessages)+len(keyMessages))
	allMessages = append(allMessages, siteMessages...)
	allMessages = append(allMessages, keyMessages...)
	var syncMessage any
	if len(allMessages) > 0 {
		syncMessage = strings.Join(allMessages, "; ")
	}
	var siteValidationMessage any
	if len(siteMessages) > 0 {
		siteValidationMessage = strings.Join(siteMessages, "; ")
	}
	syncStatus, validationOK, validationMessage, authFailure := refreshValidationForSync(item.SiteType, syncStatus, siteValidationMessage)
	if authFailure != "" {
		if s.oauth != nil {
			if connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, item.ID); err == nil {
				_ = s.oauth.MarkConnectionUnavailable(ctx, connection.ID, authFailure)
			}
		}
		item = s.autoDisableSiteAfterRefreshFailure(ctx, item)
	} else {
		item = s.restoreAutoDisabledSiteAfterRefreshSuccess(ctx, item)
	}

	upsertParams := store.UpsertSiteStateParams{
		SiteID:            item.ID,
		ValidationOK:      validationOK,
		ValidationMessage: validationMessage,
		SyncStatus:        syncStatus,
		SyncMessage:       syncMessage,
		LastSyncStartedAt: now,
		LastSyncedAt:      now,
		APIKeyCount:       len(refreshedKeys),
		ModelCount:        len(siteModels),
	}
	if userSummary.User != nil {
		upsertParams.UserSummary = jsonBytes(userSummary)
		upsertParams.Pricing = jsonBytes(userSummary.Pricing)
		upsertParams.Checkin = jsonBytes(userSummary.Checkin)
	}
	state, err := store.NewSiteStateRepository(s.db.DB()).Upsert(ctx, upsertParams)
	if err != nil {
		return RefreshResult{}, err
	}
	if auth.ConnectionID != uuid.Nil {
		patch := map[string]any{
			"models": modelSnapshots(siteModels),
		}
		if newQuota := quotaFromUserSummary(userSummary); codexQuotaHasWindowData(newQuota) {
			patch["quota"] = newQuota
		}
		for key, value := range oauthMetadataFromUserSummary(userSummary) {
			patch[key] = value
		}
		_ = s.oauth.UpdateConnectionSync(ctx, auth.ConnectionID, patch)
	}

	apiKeyStates, _ := apiKeyStateRepo.ListBySite(ctx, item.ID)
	return RefreshResult{
		Site:          item,
		State:         state,
		Models:        siteModels,
		APIKeyStates:  apiKeyStates,
		APIKeyModels:  apiKeyModels,
		PricingGroups: pricingGroups,
		ModelPricings: modelPricings,
	}, authFailureError(authFailure)
}

func quotaFromUserSummary(summary adapter.UserSummary) map[string]any {
	user, ok := summary.User.(map[string]any)
	if !ok {
		return nil
	}
	quota, _ := user["quota"].(map[string]any)
	return quota
}

func oauthMetadataFromUserSummary(summary adapter.UserSummary) map[string]any {
	user, ok := summary.User.(map[string]any)
	if !ok {
		return nil
	}
	result := map[string]any{}
	for _, key := range []string{"project_id", "subscription_tier", "plan_type"} {
		if value := user[key]; value != nil && strings.TrimSpace(anyString(value)) != "" {
			result[key] = value
		}
	}
	return result
}

func modelSnapshots(models []store.SiteModel) []map[string]any {
	items := make([]map[string]any, 0, len(models))
	for _, model := range models {
		payload := map[string]any{
			"id":                  model.UpstreamName,
			"name":                model.UpstreamName,
			"display_name":        model.DisplayName,
			"display":             model.DisplayName,
			"site_model_id":       model.ID.String(),
			"upstream_model_name": model.UpstreamName,
			"status":              model.Status,
		}
		capabilities := map[string]any{}
		if len(model.Capabilities) > 0 {
			_ = json.Unmarshal(model.Capabilities, &capabilities)
		}
		if len(capabilities) > 0 {
			payload["capabilities"] = capabilities
			if quota, ok := capabilities["quota"].(map[string]any); ok {
				payload["quota"] = quota
			}
		}
		items = append(items, payload)
	}
	return items
}

func (s *Service) refreshModelOnlyState(ctx context.Context, item store.Site) (RefreshResult, error) {
	// Already runs under the per-site lock held by RefreshState; call the
	// unlocked core to avoid re-acquiring the non-reentrant mutex.
	result, err := s.syncModelsLocked(ctx, item.ID)
	now := time.Now()
	if err != nil {
		return s.markRefreshFailed(ctx, item, err.Error())
	}

	module, ok := s.adapters.ModuleForSiteType(item.SiteType)
	if !ok {
		return s.markRefreshFailed(ctx, item, fmt.Sprintf("unsupported site_type %q", item.SiteType))
	}

	syncStatus := "synced"
	var syncMessage any
	if result.KeyErrors > 0 {
		syncStatus = "partial"
		syncMessage = fmt.Sprintf("%d of %d api keys failed", result.KeyErrors, result.KeyErrorTotal)
	}
	apiKeyCount := 0
	if strings.EqualFold(strings.TrimSpace(item.SiteType), "grok") {
		apiKeyCount, err = s.grokSiteAPIKeyCount(ctx, item.ID)
		if err != nil {
			return RefreshResult{}, err
		}
	}
	pricingSnapshot := adapter.PricingSnapshot{}
	pricingGroups := []store.SitePricingGroup{}
	modelPricings := []store.SiteModelPricing{}

	if pricingFetcher, ok := adapter.AsPricingFetcher(module); ok {
		auth, authErr := s.pricingAuth(ctx, item)
		if authErr != nil {
			syncStatus = "partial"
			syncMessage = authErr.Error()
		} else {
			pricingSnapshot, err = pricingFetcher.FetchPricing(ctx, s.toAdapterSite(ctx, item), auth)
			if err != nil {
				syncStatus = "partial"
				syncMessage = err.Error()
			} else if len(pricingSnapshot.Items) > 0 || len(pricingSnapshot.Groups) > 0 {
				siteModelsByName := make(map[string]store.SiteModel, len(result.Models))
				for _, siteModel := range result.Models {
					siteModelsByName[siteModel.UpstreamName] = siteModel
					siteModelsByName[siteModel.DisplayName] = siteModel
				}
				groups, pricings, syncErr := syncPricingState(
					ctx,
					item.ID,
					item.SiteType,
					pricingSnapshot,
					siteModelsByName,
					store.NewSitePricingGroupRepository(s.db.DB()),
					store.NewSiteModelPricingRepository(s.db.DB()),
					now,
				)
				if syncErr != nil {
					syncStatus = "partial"
					syncMessage = syncErr.Error()
				} else {
					pricingGroups = groups
					modelPricings = pricings
				}
			}
		}
	}

	state, err := store.NewSiteStateRepository(s.db.DB()).Upsert(ctx, store.UpsertSiteStateParams{
		SiteID:            item.ID,
		ValidationOK:      true,
		ValidationMessage: nil,
		SyncStatus:        syncStatus,
		SyncMessage:       syncMessage,
		LastSyncStartedAt: now,
		LastSyncedAt:      now,
		APIKeyCount:       apiKeyCount,
		ModelCount:        result.ModelCount,
		Pricing:           jsonBytes(pricingSnapshot.Raw),
	})
	if err != nil {
		return RefreshResult{}, err
	}
	item = s.restoreAutoDisabledSiteAfterRefreshSuccess(ctx, item)

	return RefreshResult{
		Site:          item,
		State:         state,
		Models:        result.Models,
		APIKeyModels:  result.APIKeyModels,
		PricingGroups: pricingGroups,
		ModelPricings: modelPricings,
	}, nil
}

func (s *Service) summarizePreparedRefreshKeys(ctx context.Context, item store.Site, fetcher adapter.APIKeySummaryFetcher, keys []preparedRefreshKey) []refreshKeySummary {
	summaries := make([]refreshKeySummary, len(keys))
	if len(keys) == 0 {
		return summaries
	}

	workers := refreshKeySummaryWorkers
	if workers > len(keys) {
		workers = len(keys)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				summaries[index] = s.summarizePreparedRefreshKey(ctx, item, fetcher, keys[index])
			}
		}()
	}
	for index := range keys {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	return summaries
}

func (s *Service) summarizePreparedRefreshKey(ctx context.Context, item store.Site, fetcher adapter.APIKeySummaryFetcher, key preparedRefreshKey) refreshKeySummary {
	result := refreshKeySummary{
		usage:      key.usage,
		models:     []refreshKeyModel{},
		syncStatus: "synced",
	}
	if !key.hasRawKey {
		result.syncStatus = "partial"
		result.syncMessage = "raw api key is not available; complete it manually to sync per-key usage and models"
		return result
	}

	summary, err := fetcher.SummarizeAPIKey(ctx, s.toAdapterSite(ctx, item), key.resolvedAPIKey)
	if err != nil {
		result.syncStatus = "failed"
		result.syncMessage = err.Error()
		result.keyError = true
		return result
	}
	if usageHasQuotaData(summary.Usage) {
		result.usage = summary.Usage
	}
	for _, model := range summary.Models {
		result.models = append(result.models, refreshKeyModel{model: model})
	}
	result.modelsSynced = true
	return result
}

func refreshKeySummaryValidated(key preparedRefreshKey, summary refreshKeySummary) bool {
	return key.hasRawKey && summary.modelsSynced
}

func (s *Service) pricingAuth(ctx context.Context, item store.Site) (adapter.SystemAuth, error) {
	if CredentialTypeForSiteType(item.SiteType) == "api_key" {
		apiKey, err := s.APIKey(ctx, item.ID)
		if err != nil {
			return adapter.SystemAuth{}, err
		}
		return adapter.SystemAuth{AccessToken: apiKey}, nil
	}
	return s.SystemAuth(ctx, item.ID)
}

func refreshValidationForSync(siteType string, syncStatus string, syncMessage any) (string, any, any, string) {
	message, _ := syncMessage.(string)
	if OAuthPermanentAuthRefreshError(siteType, message) {
		return "failed", false, message, message
	}
	return syncStatus, true, nil, ""
}

func authFailureError(message string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	return fmt.Errorf("%s", message)
}

func restoreAPIKeyStateAfterAuthRecovery(ctx context.Context, repo store.SiteAPIKeyStateRepository, credentialID uuid.UUID) (store.SiteAPIKeyState, error) {
	state, err := repo.UpdateEnabled(ctx, credentialID, true)
	if err != nil {
		return store.SiteAPIKeyState{}, fmt.Errorf("restore api key state after auth recovery: %w", err)
	}
	return state, nil
}

func (s *Service) markRefreshFailed(ctx context.Context, item store.Site, message string) (RefreshResult, error) {
	now := time.Now()
	failure := upstream.ClassifyMessage(message)
	recoverable := failure.Limited() || failure.Transient() ||
		(strings.Contains(strings.ToLower(message), "enabled api keys failed") && !failure.CredentialInvalid())
	validationOK := false
	syncStatus := "failed"
	if recoverable {
		validationOK = true
		syncStatus = "partial"
	} else {
		item = s.autoDisableSiteAfterRefreshFailure(ctx, item)
	}
	state, err := store.NewSiteStateRepository(s.db.DB()).Upsert(ctx, store.UpsertSiteStateParams{
		SiteID:       item.ID,
		ValidationOK: validationOK,
		ValidationMessage: func() any {
			if recoverable {
				return nil
			}
			return message
		}(),
		SyncStatus:        syncStatus,
		SyncMessage:       message,
		LastSyncStartedAt: now,
		LastSyncedAt:      nil,
	})
	if err != nil {
		return RefreshResult{}, err
	}

	return RefreshResult{
		Site:  item,
		State: state,
	}, fmt.Errorf("%s", message)
}

func (s *Service) autoDisableSiteAfterRefreshFailure(ctx context.Context, item store.Site) store.Site {
	if item.ID == uuid.Nil {
		return item
	}
	if !item.Enabled && !siteAutoDisabledByRefresh(item) {
		return item
	}
	meta := siteMetaMap(item)
	meta[siteMetaAutoDisabledByRefresh] = true
	updated, err := store.NewSiteRepository(s.db.DB()).SetEnabledAndMeta(ctx, item.ID, false, store.JSON(jsonBytes(meta)))
	if err != nil {
		return item
	}
	return updated
}

func (s *Service) restoreAutoDisabledSiteAfterRefreshSuccess(ctx context.Context, item store.Site) store.Site {
	if item.ID == uuid.Nil || !siteAutoDisabledByRefresh(item) {
		return item
	}
	meta := siteMetaMap(item)
	delete(meta, siteMetaAutoDisabledByRefresh)
	updated, err := store.NewSiteRepository(s.db.DB()).SetEnabledAndMeta(ctx, item.ID, true, store.JSON(jsonBytes(meta)))
	if err != nil {
		return item
	}
	return updated
}

func siteAutoDisabledByRefresh(item store.Site) bool {
	return boolFromMeta(siteMetaMap(item), siteMetaAutoDisabledByRefresh, false)
}

func siteMetaMap(item store.Site) map[string]any {
	meta := map[string]any{}
	if len(item.Meta) > 0 {
		_ = json.Unmarshal(item.Meta, &meta)
	}
	return meta
}

// upsertCredentialAPIKeyModels reconciles the api-key-model rows for a single
// credential: every non-empty model is upserted as available, and any model the
// credential no longer reports is marked unavailable. Returned rows carry the
// repository's persisted Enabled state (Upsert only assigns Enabled on insert),
// so callers can honour a model that was manually disabled.
func upsertCredentialAPIKeyModels(
	ctx context.Context,
	repo store.SiteAPIKeyModelRepository,
	siteID uuid.UUID,
	credentialID uuid.UUID,
	models []adapter.Model,
	now time.Time,
) ([]store.SiteAPIKeyModel, error) {
	seen := make([]string, 0, len(models))
	stored := make([]store.SiteAPIKeyModel, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model.UpstreamName) == "" {
			continue
		}
		seen = append(seen, model.UpstreamName)
		item, err := repo.Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
			SiteID:            siteID,
			SiteCredentialID:  credentialID,
			UpstreamModelName: model.UpstreamName,
			DisplayName:       defaultString(model.DisplayName, model.UpstreamName),
			Available:         true,
			Enabled:           true,
			Raw:               jsonBytes(model.Capabilities),
			LastSeenAt:        now,
			LastSyncedAt:      now,
		})
		if err != nil {
			return nil, err
		}
		stored = append(stored, item)
	}
	if err := repo.MarkUnavailableExcept(ctx, credentialID, seen); err != nil {
		return nil, err
	}
	return stored, nil
}

func syncModelState(ctx context.Context, siteID uuid.UUID, keys []refreshKey, siteModelRepo store.SiteModelRepository, apiKeyModelRepo store.SiteAPIKeyModelRepository) ([]store.SiteModel, []store.SiteAPIKeyModel, error) {
	aggregateModels := map[string]adapter.Model{}
	keyNamesByModel := map[string][]string{}
	apiKeyModels := make([]store.SiteAPIKeyModel, 0)
	existingSiteModels, err := siteModelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}
	existingSiteModelsByName := make(map[string]store.SiteModel, len(existingSiteModels))
	for _, model := range existingSiteModels {
		existingSiteModelsByName[model.UpstreamName] = model
	}

	for _, key := range keys {
		models := make([]adapter.Model, 0, len(key.models))
		modelsByName := make(map[string]adapter.Model, len(key.models))
		for _, item := range key.models {
			models = append(models, item.model)
			modelsByName[item.model.UpstreamName] = item.model
		}
		var stored []store.SiteAPIKeyModel
		if key.modelsSynced {
			stored, err = upsertCredentialAPIKeyModels(ctx, apiKeyModelRepo, siteID, key.credential.ID, models, time.Now())
			if err != nil {
				return nil, nil, err
			}
		} else {
			stored, err = apiKeyModelRepo.ListByCredential(ctx, key.credential.ID)
			if err != nil {
				return nil, nil, err
			}
		}
		apiKeyModels = append(apiKeyModels, stored...)

		for _, apiKeyModel := range stored {
			if !apiKeyModel.Available || !apiKeyModel.Enabled || !key.state.Enabled {
				continue
			}
			model, ok := modelsByName[apiKeyModel.UpstreamModelName]
			if !ok {
				existing, exists := existingSiteModelsByName[apiKeyModel.UpstreamModelName]
				if !exists {
					continue
				}
				model = adapter.Model{
					UpstreamName: existing.UpstreamName,
					DisplayName:  existing.DisplayName,
					Capabilities: map[string]any{},
				}
				_ = json.Unmarshal(existing.Capabilities, &model.Capabilities)
			}
			aggregateModels[model.UpstreamName] = coalesceModelCapabilities(aggregateModels[model.UpstreamName], model, key.name)
			keyNamesByModel[model.UpstreamName] = appendUnique(keyNamesByModel[model.UpstreamName], key.name)
		}
	}

	siteModels := make([]store.SiteModel, 0, len(aggregateModels))
	siteModelsByName := map[string]store.SiteModel{}
	seenSiteModelNames := make([]string, 0, len(aggregateModels))
	for name, model := range aggregateModels {
		if model.Capabilities == nil {
			model.Capabilities = map[string]any{}
		}
		model.Capabilities["api_keys"] = keyNamesByModel[name]

		capabilities, err := json.Marshal(model.Capabilities)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal model capabilities: %w", err)
		}
		siteModel, err := siteModelRepo.Upsert(ctx, store.UpsertSiteModelParams{
			SiteID:       siteID,
			UpstreamName: model.UpstreamName,
			DisplayName:  defaultString(model.DisplayName, model.UpstreamName),
			Capabilities: capabilities,
			Status:       "active",
		})
		if err != nil {
			return nil, nil, err
		}
		siteModels = append(siteModels, siteModel)
		siteModelsByName[name] = siteModel
		seenSiteModelNames = append(seenSiteModelNames, name)
	}
	if err := siteModelRepo.MarkUnavailableExcept(ctx, siteID, seenSiteModelNames); err != nil {
		return nil, nil, err
	}

	for _, key := range keys {
		for _, item := range key.models {
			siteModel, ok := siteModelsByName[item.model.UpstreamName]
			if !ok {
				continue
			}
			apiKeyModel, err := apiKeyModelRepo.Upsert(ctx, store.UpsertSiteAPIKeyModelParams{
				SiteID:            siteID,
				SiteCredentialID:  key.credential.ID,
				SiteModelID:       siteModel.ID,
				UpstreamModelName: item.model.UpstreamName,
				DisplayName:       defaultString(item.model.DisplayName, item.model.UpstreamName),
				Available:         true,
				Enabled:           true,
				Raw:               jsonBytes(item.model.Capabilities["raw"]),
				LastSeenAt:        time.Now(),
				LastSyncedAt:      time.Now(),
			})
			if err != nil {
				return nil, nil, err
			}
			apiKeyModels = append(apiKeyModels, apiKeyModel)
		}
	}

	return siteModels, apiKeyModels, nil
}

type RefreshSingleAPIKeyResult struct {
	State      store.SiteAPIKeyState
	Models     []store.SiteAPIKeyModel
	SiteModels []store.SiteModel
}

func (s *Service) RefreshSingleAPIKey(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) (RefreshSingleAPIKeyResult, error) {
	defer s.siteLocks.lock(siteID)()

	site, err := s.Get(ctx, siteID)
	if err != nil {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("get site: %w", err)
	}

	module, ok := s.adapters.ModuleForSiteType(site.SiteType)
	if !ok {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("unsupported site_type %q", site.SiteType)
	}
	summaryFetcher, hasSummary := adapter.AsAPIKeySummaryFetcher(module)
	modelLister, hasModelLister := adapter.AsGatewayModelLister(module)
	if !hasSummary && !hasModelLister {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("site_type %q does not support api key refresh", site.SiteType)
	}

	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	credential, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("get credential: %w", err)
	}
	if credential.SiteID != siteID {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("credential does not belong to site")
	}

	secret, err := s.credentials.Decrypt(credential.EncryptedSecret)
	if err != nil {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("decrypt credential: %w", err)
	}
	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}
	apiKey := strings.TrimSpace(secretFromCredentialMeta(secret, meta))
	if strings.EqualFold(strings.TrimSpace(site.SiteType), "grok") {
		apiKey, err = s.oauth.EnsureGrokAccessToken(ctx, credential.ID)
		if err != nil {
			return RefreshSingleAPIKeyResult{}, fmt.Errorf("grok access token: %w", err)
		}
	}
	if apiKey == "" || strings.HasPrefix(apiKey, "missing-api-key") {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("raw api key is not available for this credential")
	}

	now := time.Now()
	adapterSite := s.toAdapterSite(ctx, site)
	apiKeyStateRepo := store.NewSiteAPIKeyStateRepository(s.db.DB())
	apiKeyModelRepo := store.NewSiteAPIKeyModelRepository(s.db.DB())

	syncStatus := "synced"
	var syncMessage any
	var usage any
	models := []refreshKeyModel{}
	modelsSynced := false
	keyEnabled := credentialEnabledFromMeta(credential.Meta)
	authFailed := false

	if hasSummary {
		summary, err := summaryFetcher.SummarizeAPIKey(ctx, adapterSite, apiKey)
		if err != nil {
			syncStatus = "failed"
			syncMessage = err.Error()
			if upstream.ClassifyError(err).CredentialInvalid() {
				keyEnabled = false
				authFailed = true
			}
		} else {
			if usageHasQuotaData(summary.Usage) {
				usage = summary.Usage
			}
			modelsSynced = true
			for _, model := range summary.Models {
				models = append(models, refreshKeyModel{model: model})
			}
		}
	} else {
		listed, err := modelLister.ListModels(ctx, adapterSite, apiKey)
		if err != nil {
			syncStatus = "failed"
			syncMessage = err.Error()
			if upstream.ClassifyError(err).CredentialInvalid() {
				keyEnabled = false
				authFailed = true
			}
		} else {
			modelsSynced = true
			for _, model := range listed {
				models = append(models, refreshKeyModel{model: model})
			}
		}
	}
	if syncStatus == "synced" {
		s.recoverCredentialCooldownAfterRefresh(ctx, siteID, credentialID)
	}

	if authFailed {
		credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
		meta["enabled"] = false
		_, _ = updateCredentialMeta(ctx, credentialRepo, credentialID, meta)
	}

	usageJSON := jsonBytes(usage)
	if usage == nil && strings.EqualFold(strings.TrimSpace(site.SiteType), "opencode_go") {
		if states, stateErr := apiKeyStateRepo.ListBySite(ctx, siteID); stateErr == nil {
			for _, existing := range states {
				if existing.SiteCredentialID == credentialID && len(existing.Usage) > 0 {
					usageJSON = existing.Usage
					break
				}
			}
		}
	}
	state, err := apiKeyStateRepo.UpsertSyncResult(ctx, store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: credentialID,
		SiteID:           siteID,
		Enabled:          keyEnabled,
		Usage:            usageJSON,
		SyncStatus:       syncStatus,
		SyncMessage:      syncMessage,
		LastSyncedAt:     now,
	})
	if err != nil {
		return RefreshSingleAPIKeyResult{}, fmt.Errorf("upsert api key state: %w", err)
	}

	if authFailed {
		state, _ = apiKeyStateRepo.UpdateEnabled(ctx, credentialID, false)
	}

	storedModels := []store.SiteAPIKeyModel{}
	if modelsSynced {
		adapterModels := make([]adapter.Model, 0, len(models))
		for _, item := range models {
			adapterModels = append(adapterModels, item.model)
		}
		storedModels, err = upsertCredentialAPIKeyModels(ctx, apiKeyModelRepo, siteID, credentialID, adapterModels, now)
		if err != nil {
			return RefreshSingleAPIKeyResult{}, fmt.Errorf("sync api key models: %w", err)
		}
	} else {
		storedModels, err = apiKeyModelRepo.ListByCredential(ctx, credentialID)
		if err != nil {
			return RefreshSingleAPIKeyResult{}, fmt.Errorf("list existing api key models: %w", err)
		}
	}

	siteModels, _ := s.syncSiteModelsFromAPIKeyState(ctx, siteID, site.SiteType, site.BaseURL)

	return RefreshSingleAPIKeyResult{
		State:      state,
		Models:     storedModels,
		SiteModels: siteModels,
	}, nil
}

func (s *Service) syncSiteModelsFromAPIKeyState(ctx context.Context, siteID uuid.UUID, siteType string, siteBaseURL string) ([]store.SiteModel, error) {
	apiKeyModelRepo := store.NewSiteAPIKeyModelRepository(s.db.DB())
	siteModelRepo := store.NewSiteModelRepository(s.db.DB())
	apiKeyStateRepo := store.NewSiteAPIKeyStateRepository(s.db.DB())

	allAPIKeyModels, err := apiKeyModelRepo.ListBySite(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("list api key models: %w", err)
	}
	allStates, err := apiKeyStateRepo.ListBySite(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("list api key states: %w", err)
	}
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("list site credentials: %w", err)
	}
	credentialsByID := make(map[uuid.UUID]store.SiteCredential, len(credentials))
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
	}
	statesByCredentialID := make(map[uuid.UUID]store.SiteAPIKeyState, len(allStates))
	for _, state := range allStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}

	activeModelNames := map[string]struct{}{}
	rawCapabilitiesByModel := map[string]store.JSON{}
	for _, akm := range allAPIKeyModels {
		if !akm.Available || !akm.Enabled {
			continue
		}
		credential, ok := credentialsByID[akm.SiteCredentialID]
		if !ok || !store.CredentialUsable(credential) || !store.CredentialStateUsableForCredential(credential, statesByCredentialID[akm.SiteCredentialID]) {
			continue
		}
		activeModelNames[akm.UpstreamModelName] = struct{}{}
		if _, exists := rawCapabilitiesByModel[akm.UpstreamModelName]; !exists {
			rawCapabilitiesByModel[akm.UpstreamModelName] = akm.Raw
		}
	}

	siteModels := make([]store.SiteModel, 0, len(activeModelNames))
	for name := range activeModelNames {
		capabilities := []byte(`{}`)
		if strings.EqualFold(strings.TrimSpace(siteType), "grok") {
			capabilities = grokSiteModelCapabilities(rawCapabilitiesByModel[name], name)
		} else if strings.EqualFold(strings.TrimSpace(siteType), "opencode_go") {
			capabilities = openCodeGoSiteModelCapabilities(rawCapabilitiesByModel[name], name)
		} else if strings.TrimSpace(siteType) != "" && modelcapabilities.UsesModelNameEndpointInference(siteType) {
			model := applyModelNameEndpointTypes(store.Site{SiteType: siteType, BaseURL: siteBaseURL}, adapter.Model{
				UpstreamName: name,
				DisplayName:  name,
			})
			var marshalErr error
			capabilities, marshalErr = json.Marshal(model.Capabilities)
			if marshalErr != nil {
				return nil, fmt.Errorf("marshal site model capabilities: %w", marshalErr)
			}
		}
		siteModel, err := siteModelRepo.Upsert(ctx, store.UpsertSiteModelParams{
			SiteID:       siteID,
			UpstreamName: name,
			DisplayName:  name,
			Capabilities: capabilities,
			Status:       "active",
		})
		if err != nil {
			return nil, fmt.Errorf("upsert site model: %w", err)
		}
		// Bind the contributing api-key models to this site model so credential
		// selection (ListCredentialsForSiteModel) can find the right key. The
		// full-site refresh does this in syncModelState; the single-key path must
		// too, otherwise a freshly refreshed key's models stay unbound and routing
		// falls back to an arbitrary credential.
		if err := apiKeyModelRepo.BindSiteModel(ctx, siteID, name, siteModel.ID); err != nil {
			return nil, fmt.Errorf("bind api key models to site model: %w", err)
		}
		siteModels = append(siteModels, siteModel)
	}

	seenNames := make([]string, 0, len(activeModelNames))
	for name := range activeModelNames {
		seenNames = append(seenNames, name)
	}
	if err := siteModelRepo.MarkUnavailableExcept(ctx, siteID, seenNames); err != nil {
		return nil, fmt.Errorf("mark unavailable site models: %w", err)
	}

	if matchedModels, err := catalog.NewService(s.db, s.confFile).MatchSiteModels(ctx, siteID); err == nil {
		siteModels = availableSiteModels(matchedModels)
	}

	return siteModels, nil
}

func grokSiteModelCapabilities(raw store.JSON, modelName string) []byte {
	capabilities := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &capabilities)
	}
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	if strings.TrimSpace(anyString(capabilities["source"])) == "" {
		capabilities["source"] = "grok"
	}
	if len(stringSliceFromPricingRaw(capabilities["supported_endpoint_types"])) == 0 {
		capabilities["supported_endpoint_types"] = modelcapabilities.InferEndpointTypesForModel(modelName)
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return []byte(`{"source":"grok"}`)
	}
	return encoded
}

func openCodeGoSiteModelCapabilities(raw store.JSON, modelName string) []byte {
	capabilities := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &capabilities)
	}
	if capabilities == nil {
		capabilities = map[string]any{}
	}
	capabilities["source"] = "opencode_go_spec"
	endpointTypes, matched, err := protocolspec.ResolveModelEndpointTypes("opencode_go", modelName)
	if err != nil {
		endpointTypes = nil
		matched = false
	}
	capabilities["supported_endpoint_types"] = endpointTypes
	if matched {
		capabilities["protocol_mapping_status"] = "mapped"
	} else {
		capabilities["protocol_mapping_status"] = "unsupported"
	}
	if version, versionErr := protocolspec.Version(); versionErr == nil {
		capabilities["protocol_spec_version"] = version
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return []byte(`{"source":"opencode_go_spec","supported_endpoint_types":[]}`)
	}
	return encoded
}

func (s *Service) SiteState(ctx context.Context, siteID uuid.UUID) (store.SiteState, error) {
	return store.NewSiteStateRepository(s.db.DB()).GetBySite(ctx, siteID)
}

func (s *Service) SiteStates(ctx context.Context) (map[uuid.UUID]store.SiteState, error) {
	return store.NewSiteStateRepository(s.db.DB()).List(ctx)
}

func (s *Service) APIKeyStates(ctx context.Context, siteID uuid.UUID) ([]store.SiteAPIKeyState, error) {
	return store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, siteID)
}

func (s *Service) APIKeyModels(ctx context.Context, siteCredentialID uuid.UUID) ([]store.SiteAPIKeyModel, error) {
	return store.NewSiteAPIKeyModelRepository(s.db.DB()).ListByCredential(ctx, siteCredentialID)
}

func apiKeyCredentialMeta(apiKey adapter.APIKey) map[string]any {
	meta := map[string]any{
		"name":            apiKey.Name,
		"upstream_id":     apiKey.ID,
		"upstream_key_id": strings.TrimSpace(apiKey.ExternalID),
		"status":          apiKey.Status,
		"raw_key_missing": strings.TrimSpace(apiKey.Key) == "",
	}
	if strings.TrimSpace(apiKey.ExternalID) == "" {
		delete(meta, "upstream_key_id")
	}
	if strings.TrimSpace(apiKey.MaskedKey) != "" {
		meta["upstream_masked_key"] = strings.TrimSpace(apiKey.MaskedKey)
	}
	for _, key := range []string{
		"remain_quota",
		"used_quota",
		"unlimited_quota",
		"model_limits_enabled",
		"model_limits",
		"expired_time",
		"group",
	} {
		if value, ok := apiKey.Raw[key]; ok {
			meta[key] = value
		}
	}

	return meta
}

func markMissingManagedAPIKeysStale(
	ctx context.Context,
	siteID uuid.UUID,
	credentialRepo store.SiteCredentialRepository,
	stateRepo store.SiteAPIKeyStateRepository,
	modelRepo store.SiteAPIKeyModelRepository,
	prepared []preparedRefreshKey,
	syncedAt time.Time,
) error {
	credentials, err := credentialRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	states, err := stateRepo.ListBySite(ctx, siteID)
	if err != nil {
		return err
	}
	statesByCredentialID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	seen := make(map[uuid.UUID]struct{}, len(prepared))
	for _, key := range prepared {
		seen[key.credential.ID] = struct{}{}
	}
	for _, credential := range credentials {
		if !isSiteAPIKeyCredentialType(credential.CredentialType) {
			continue
		}
		if _, ok := seen[credential.ID]; ok {
			continue
		}
		state, ok := statesByCredentialID[credential.ID]
		if !ok || !upstreamManagedAPIKeyCredential(credential, state) {
			continue
		}
		if _, err := stateRepo.MarkStale(ctx, credential.ID, syncedAt); err != nil {
			return err
		}
		if err := modelRepo.MarkUnavailableExcept(ctx, credential.ID, nil); err != nil {
			return err
		}
	}
	return nil
}

func upstreamManagedAPIKeyCredential(credential store.SiteCredential, state store.SiteAPIKeyState) bool {
	if state.UpstreamID.Valid {
		return true
	}
	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}
	if strings.TrimSpace(anyString(meta["upstream_key_id"])) != "" {
		return true
	}
	upstreamID, ok := int64FromAny(meta["upstream_id"])
	return ok && upstreamID > 0
}

func apiKeyCredentialTypeForRefresh(apiKey adapter.APIKey) string {
	externalID := strings.TrimSpace(apiKey.ExternalID)
	if externalID != "" {
		return defaultCredentialType + ":" + externalID
	}
	if apiKey.ID > 0 {
		return defaultCredentialType + ":" + strconv.Itoa(apiKey.ID)
	}
	return defaultCredentialType
}

func (s *Service) apiKeyCredentialInputForRefresh(
	ctx context.Context,
	repo store.SiteCredentialRepository,
	siteID uuid.UUID,
	credentialType string,
	apiKey adapter.APIKey,
) (CredentialInput, bool, error) {
	upstreamMeta := apiKeyCredentialMeta(apiKey)
	existingCredential, existingSecret, existingMeta, existingFound, err := s.existingAPIKeyCredential(ctx, repo, siteID, credentialType)
	if err != nil {
		return CredentialInput{}, false, err
	}

	secret := strings.TrimSpace(apiKey.Key)
	hasRawKey := secret != ""
	if !hasRawKey && existingFound && shouldPreserveExistingAPIKeySecret(existingMeta, existingSecret) {
		if maskedKeysCompatible(existingCredential.MaskedSecret, apiKey.MaskedKey) {
			secret = existingSecret
			hasRawKey = true
			upstreamMeta["raw_key_missing"] = false
			if boolFromMeta(existingMeta, "manually_completed", false) {
				upstreamMeta["manually_completed"] = true
			}
			upstreamMeta["upstream_masked_key"] = strings.TrimSpace(apiKey.MaskedKey)
		} else {
			secret = existingSecret
			upstreamMeta["manual_secret_invalidated"] = true
			upstreamMeta["manual_secret_invalidated_at"] = s.timeZone.Format(time.Now(), time.RFC3339)
		}
	}
	if strings.TrimSpace(secret) == "" {
		secret = apiKeySecretForStorage(apiKey)
	}

	meta := mergeAPIKeyRefreshMeta(existingMeta, upstreamMeta)
	if !hasRawKey {
		meta["raw_key_missing"] = true
		delete(meta, "manually_completed")
	}

	return CredentialInput{
		Type:   credentialType,
		Secret: secret,
		Meta:   meta,
	}, hasRawKey, nil
}

func shouldPreserveExistingAPIKeySecret(meta map[string]any, secret string) bool {
	secret = strings.TrimSpace(secret)
	if secret == "" || strings.HasPrefix(secret, "missing-api-key") {
		return false
	}
	return !boolFromMeta(meta, "raw_key_missing", false)
}

func (s *Service) existingAPIKeyCredential(
	ctx context.Context,
	repo store.SiteCredentialRepository,
	siteID uuid.UUID,
	credentialType string,
) (store.SiteCredential, string, map[string]any, bool, error) {
	credential, err := repo.GetBySiteAndType(ctx, siteID, credentialType)
	if err != nil {
		return store.SiteCredential{}, "", nil, false, nil
	}

	secret, err := s.credentials.Decrypt(credential.EncryptedSecret)
	if err != nil {
		return store.SiteCredential{}, "", nil, false, err
	}

	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}

	return credential, secretFromCredentialMeta(secret, meta), meta, true, nil
}

func mergeAPIKeyRefreshMeta(existing map[string]any, upstream map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range upstream {
		merged[key] = value
	}

	for _, key := range []string{
		"enabled",
		credentialMetaAutoDisabledByRefresh,
		"disabled_models",
		"manually_completed",
		"manual_secret_invalidated",
		"manual_secret_invalidated_at",
	} {
		if value, ok := existing[key]; ok {
			merged[key] = value
		}
	}
	if upstream["manual_secret_invalidated"] == true {
		merged["manual_secret_invalidated"] = true
		if value, ok := upstream["manual_secret_invalidated_at"]; ok {
			merged["manual_secret_invalidated_at"] = value
		}
		delete(merged, "manually_completed")
	}
	if upstream["manually_completed"] == true {
		merged["manually_completed"] = true
		delete(merged, "manual_secret_invalidated")
		delete(merged, "manual_secret_invalidated_at")
	}

	return merged
}

func maskedKeysCompatible(existingMasked string, upstreamMasked string) bool {
	upstreamMasked = strings.TrimSpace(upstreamMasked)
	if upstreamMasked == "" {
		return true
	}

	visibleParts := strings.FieldsFunc(upstreamMasked, func(r rune) bool {
		return r == '*' || r == '.'
	})
	existingMasked = strings.TrimSpace(existingMasked)
	hasComparablePart := false
	for _, part := range visibleParts {
		part = strings.TrimSpace(part)
		if len(part) < 3 {
			continue
		}
		hasComparablePart = true
		if strings.HasSuffix(existingMasked, part) || strings.Contains(existingMasked, part) {
			return true
		}
	}

	return !hasComparablePart
}

func apiKeySecretForStorage(apiKey adapter.APIKey) string {
	if key := strings.TrimSpace(apiKey.Key); key != "" {
		return key
	}
	if key := strings.TrimSpace(apiKey.MaskedKey); key != "" {
		return key
	}
	if apiKey.ID > 0 {
		return fmt.Sprintf("missing-api-key:%d", apiKey.ID)
	}
	if name := strings.TrimSpace(apiKey.Name); name != "" {
		return "missing-api-key:" + name
	}

	return "missing-api-key"
}

func (s *Service) syncUserModelsState(ctx context.Context, site store.Site, payload any, siteModelRepo store.SiteModelRepository) ([]store.SiteModel, error) {
	models := modelsFromUserSummaryPayload(payload)
	return s.syncSiteModelsFromAdapterModels(ctx, site, models, siteModelRepo, true)
}

func (s *Service) syncPricingModelsState(ctx context.Context, site store.Site, pricingSnapshot adapter.PricingSnapshot, siteModelRepo store.SiteModelRepository) ([]store.SiteModel, error) {
	return s.syncPricingModelsStateWithMode(ctx, site, pricingSnapshot, siteModelRepo, true)
}

func (s *Service) syncPricingModelsStateWithMode(ctx context.Context, site store.Site, pricingSnapshot adapter.PricingSnapshot, siteModelRepo store.SiteModelRepository, markUnavailable bool) ([]store.SiteModel, error) {
	modelsByName := map[string]adapter.Model{}
	for _, pricing := range pricingSnapshot.Items {
		modelName := strings.TrimSpace(pricing.ModelName)
		if modelName == "" {
			continue
		}
		modelsByName[modelName] = adapter.Model{
			UpstreamName: modelName,
			DisplayName:  pricingModelDisplayName(pricing),
			Capabilities: pricingModelCapabilities(pricing),
		}
	}

	models := make([]adapter.Model, 0, len(modelsByName))
	for _, model := range modelsByName {
		models = append(models, model)
	}
	return s.syncSiteModelsFromAdapterModels(ctx, site, models, siteModelRepo, markUnavailable)
}

func pricingModelDisplayName(pricing adapter.ModelPricing) string {
	if value := strings.TrimSpace(pricing.DisplayName); value != "" {
		return value
	}
	if pricing.Raw != nil {
		if value := strings.TrimSpace(anyString(pricing.Raw["display_name"])); value != "" {
			return value
		}
		if value := strings.TrimSpace(anyString(pricing.Raw["name"])); value != "" {
			return value
		}
	}
	return pricing.ModelName
}

func pricingModelCapabilities(pricing adapter.ModelPricing) map[string]any {
	capabilities := map[string]any{"source": "newapi_pricing"}
	if pricing.Raw == nil {
		return capabilities
	}
	if endpointTypes := stringSliceFromPricingRaw(pricing.Raw["supported_endpoint_types"]); len(endpointTypes) > 0 {
		capabilities["supported_endpoint_types"] = endpointTypes
	}
	return capabilities
}

func stringSliceFromPricingRaw(value any) []string {
	switch v := value.(type) {
	case []string:
		result := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				result = append(result, item)
			}
		}
		return result
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
	}
}

func (s *Service) mergePricingModelsState(ctx context.Context, site store.Site, pricingSnapshot adapter.PricingSnapshot, existing []store.SiteModel, siteModelRepo store.SiteModelRepository) ([]store.SiteModel, error) {
	if len(pricingSnapshot.Items) == 0 {
		return existing, nil
	}

	pricingModels, err := s.syncPricingModelsStateWithMode(ctx, site, pricingSnapshot, siteModelRepo, len(existing) == 0)
	if err != nil {
		return nil, err
	}

	merged := make([]store.SiteModel, 0, len(existing)+len(pricingModels))
	seen := map[uuid.UUID]struct{}{}
	for _, item := range existing {
		if item.Status == "unavailable" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		merged = append(merged, item)
	}
	for _, item := range pricingModels {
		if item.Status == "unavailable" {
			continue
		}
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		merged = append(merged, item)
	}
	return merged, nil
}

func (s *Service) syncSiteModelsFromAdapterModels(ctx context.Context, site store.Site, models []adapter.Model, siteModelRepo store.SiteModelRepository, markUnavailable bool) ([]store.SiteModel, error) {
	if len(models) == 0 {
		return nil, nil
	}

	siteModels := make([]store.SiteModel, 0, len(models))
	seenNames := make([]string, 0, len(models))

	for _, model := range models {
		model = s.enrichModelCapabilities(ctx, site, model)

		capabilities, err := json.Marshal(model.Capabilities)
		if err != nil {
			return nil, fmt.Errorf("marshal user model capabilities: %w", err)
		}

		siteModel, err := siteModelRepo.Upsert(ctx, store.UpsertSiteModelParams{
			SiteID:       site.ID,
			UpstreamName: model.UpstreamName,
			DisplayName:  defaultString(model.DisplayName, model.UpstreamName),
			Capabilities: capabilities,
			Status:       "active",
		})
		if err != nil {
			return nil, err
		}

		siteModels = append(siteModels, siteModel)
		seenNames = append(seenNames, model.UpstreamName)
	}

	if markUnavailable {
		if err := siteModelRepo.MarkUnavailableExcept(ctx, site.ID, seenNames); err != nil {
			return nil, err
		}
	}

	return siteModels, nil
}

func availableSiteModels(models []store.SiteModel) []store.SiteModel {
	result := make([]store.SiteModel, 0, len(models))
	for _, model := range models {
		if model.Status == "unavailable" {
			continue
		}
		result = append(result, model)
	}

	return result
}

func modelsFromUserSummaryPayload(payload any) []adapter.Model {
	switch value := payload.(type) {
	case []string:
		models := make([]adapter.Model, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			models = append(models, adapter.Model{
				UpstreamName: item,
				DisplayName:  item,
				Capabilities: map[string]any{"source": "newapi_user_models"},
			})
		}
		return models
	case []any:
		models := make([]adapter.Model, 0, len(value))
		for _, item := range value {
			switch raw := item.(type) {
			case string:
				raw = strings.TrimSpace(raw)
				if raw == "" {
					continue
				}
				models = append(models, adapter.Model{
					UpstreamName: raw,
					DisplayName:  raw,
					Capabilities: map[string]any{"source": "newapi_user_models"},
				})
			case map[string]any:
				id := defaultString(anyString(raw["id"]), anyString(raw["model_name"]))
				if id == "" {
					continue
				}
				displayName := defaultString(anyString(raw["display_name"]), id)
				models = append(models, adapter.Model{
					UpstreamName: id,
					DisplayName:  displayName,
					Capabilities: map[string]any{
						"source": "newapi_user_models",
						"raw":    raw,
					},
				})
			}
		}
		return models
	case map[string]any:
		if dataModels := modelsFromUserSummaryPayload(value["data"]); len(dataModels) > 0 {
			return dataModels
		}
		if nestedModels := modelsFromUserSummaryPayload(value["models"]); len(nestedModels) > 0 {
			return nestedModels
		}
		models := make([]adapter.Model, 0, len(value))
		for key, raw := range value {
			modelName := strings.TrimSpace(key)
			if modelName == "" || modelName == "success" || modelName == "message" {
				continue
			}
			capabilities := map[string]any{"source": "newapi_user_models"}
			if rawMap, ok := raw.(map[string]any); ok {
				capabilities["raw"] = rawMap
			}
			models = append(models, adapter.Model{
				UpstreamName: modelName,
				DisplayName:  modelName,
				Capabilities: capabilities,
			})
		}
		return models
	default:
		return nil
	}
}

func usageHasQuotaData(usage any) bool {
	usageMap, ok := usage.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := usageMap["five_hour"]; ok {
		return true
	}
	if _, ok := usageMap["weekly"]; ok {
		return true
	}
	data, ok := usageMap["data"].(map[string]any)
	if !ok {
		return false
	}
	_, hasGranted := data["total_granted"]
	_, hasUsed := data["total_used"]
	_, hasAvailable := data["total_available"]
	return hasGranted || hasUsed || hasAvailable
}

func codexQuotaHasWindowData(quota map[string]any) bool {
	if len(quota) == 0 {
		return false
	}
	for _, key := range []string{"five_hour", "weekly", "credits", "reset_credits"} {
		if _, ok := quota[key]; ok {
			return true
		}
	}
	if models, ok := quota["models"].([]any); ok && len(models) > 0 {
		return true
	}
	return false
}

func usageFromTokenRaw(raw map[string]any) map[string]any {
	remain, _ := int64FromAny(raw["remain_quota"])
	used, _ := int64FromAny(raw["used_quota"])
	return map[string]any{
		"success": true,
		"source":  "token_list",
		"data": map[string]any{
			"object":               "token_usage",
			"name":                 raw["name"],
			"total_granted":        remain + used,
			"total_used":           used,
			"total_available":      remain,
			"unlimited_quota":      raw["unlimited_quota"],
			"model_limits":         raw["model_limits"],
			"model_limits_enabled": raw["model_limits_enabled"],
			"expires_at":           raw["expired_time"],
		},
	}
}

func modelLimitsJSON(value any) []byte {
	switch v := value.(type) {
	case string:
		items := []string{}
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
		return jsonBytes(items)
	case []string, []any:
		return jsonBytes(v)
	default:
		return []byte(`[]`)
	}
}

func syncPricingState(
	ctx context.Context,
	siteID uuid.UUID,
	siteType string,
	pricingSnapshot adapter.PricingSnapshot,
	siteModelsByName map[string]store.SiteModel,
	groupRepo store.SitePricingGroupRepository,
	modelPricingRepo store.SiteModelPricingRepository,
	now time.Time,
) ([]store.SitePricingGroup, []store.SiteModelPricing, error) {
	groupNames := make([]string, 0, len(pricingSnapshot.Groups))
	storedGroups := make([]store.SitePricingGroup, 0, len(pricingSnapshot.Groups))
	for _, group := range pricingSnapshot.Groups {
		groupNames = append(groupNames, group.GroupName)
		item, err := groupRepo.Upsert(ctx, store.UpsertSitePricingGroupParams{
			SiteID:       siteID,
			GroupName:    group.GroupName,
			DisplayName:  nullableString(group.DisplayName),
			Ratio:        group.Ratio,
			IsAuto:       group.IsAuto,
			Available:    true,
			Raw:          jsonBytes(group),
			LastSyncedAt: now,
		})
		if err != nil {
			return nil, nil, err
		}
		storedGroups = append(storedGroups, item)
	}
	if err := groupRepo.MarkUnavailableExcept(ctx, siteID, groupNames); err != nil {
		return nil, nil, err
	}

	compositeKeys := make([]string, 0, len(pricingSnapshot.Items))
	storedPricings := make([]store.SiteModelPricing, 0, len(pricingSnapshot.Items))
	for _, pricing := range pricingSnapshot.Items {
		compositeKeys = append(compositeKeys, pricingKey(pricing.ModelName, pricing.GroupName))

		siteModelID := any(nil)
		if matched, ok := siteModelsByName[pricing.ModelName]; ok {
			siteModelID = matched.ID
		}

		item, err := modelPricingRepo.Upsert(ctx, store.UpsertSiteModelPricingParams{
			SiteID:               siteID,
			SiteModelID:          siteModelID,
			ModelName:            pricing.ModelName,
			GroupName:            pricing.GroupName,
			QuotaType:            pricing.QuotaType,
			BillingType:          pricing.BillingType,
			Currency:             defaultString(pricing.Currency, "USD"),
			GroupRatio:           pricing.GroupRatio,
			ModelRatio:           nullableFloat64(pricing.ModelRatio, pricing.HasModelRatio),
			CompletionRatio:      nullableFloat64(pricing.CompletionRatio, pricing.HasCompletionRatio),
			CacheRatio:           nullableFloat64(pricing.CacheRatio, pricing.HasCacheRatio),
			CreateCacheRatio:     nullableFloat64(pricing.CreateCacheRatio, pricing.HasCreateCacheRatio),
			CreateCache1hRatio:   nullableFloat64(pricing.CreateCache1hRatio, pricing.HasCreateCache1hRatio),
			ImageRatio:           nullableFloat64(pricing.ImageRatio, pricing.HasImageRatio),
			AudioRatio:           nullableFloat64(pricing.AudioRatio, pricing.HasAudioRatio),
			AudioCompletionRatio: nullableFloat64(pricing.AudioCompletionRatio, pricing.HasAudioCompletionRatio),
			ModelPrice:           nullableFloat64(pricing.ModelPrice, pricing.HasModelPrice),
			PerRequestValue:      nullableFloat64(pricing.PerRequestValue, pricing.HasPerRequestValue),
			InputValue:           nullableFloat64(pricing.InputValue, pricing.HasInputValue),
			OutputValue:          nullableFloat64(pricing.OutputValue, pricing.HasOutputValue),
			VendorID:             nullableInt64(pricing.VendorID, pricing.HasVendorID),
			VendorName:           nullableString(pricing.VendorName),
			VendorIcon:           nullableString(pricing.VendorIcon),
			Description:          nullableString(pricing.Description),
			OwnerBy:              nullableString(pricing.OwnerBy),
			PricingSource:        syncedPricingSource(siteType, pricing.Raw),
			PreserveManual:       siteType != "newapi",
			Available:            true,
			Raw:                  jsonBytes(pricing.Raw),
			LastSyncedAt:         now,
		})
		if err != nil {
			return nil, nil, err
		}
		storedPricings = append(storedPricings, item)
	}
	if err := modelPricingRepo.MarkUnavailableExcept(ctx, siteID, compositeKeys); err != nil {
		return nil, nil, err
	}

	return storedGroups, storedPricings, nil
}

func pricingKey(modelName string, groupName string) string {
	return modelName + string(rune(31)) + groupName
}

func (s *Service) syncCanonicalPricingFallback(ctx context.Context, siteID uuid.UUID, models []store.SiteModel) []string {
	if len(models) == 0 {
		return nil
	}

	pricingRepo := store.NewSiteModelPricingRepository(s.db.DB())
	canonicalRepo := store.NewCanonicalModelRepository(s.db.DB())

	unpriced := []string{}
	for _, model := range models {
		existing, _ := pricingRepo.ListBySiteModelID(ctx, model.ID)
		hasUpstream := false
		for _, p := range existing {
			if p.Available && p.PricingSource != "canonical_fallback" {
				hasUpstream = true
				break
			}
		}
		if hasUpstream {
			if model.CanonicalID.Valid {
				s.backfillCanonicalPricingRatios(ctx, pricingRepo, canonicalRepo, model, existing)
			}
			continue
		}

		if !model.CanonicalID.Valid {
			unpriced = append(unpriced, model.UpstreamName)
			continue
		}

		canonical, err := canonicalRepo.GetByID(ctx, model.CanonicalID.UUID)
		if err != nil || !canonical.InputPrice.Valid {
			unpriced = append(unpriced, model.UpstreamName)
			continue
		}

		raw := []byte(fmt.Sprintf(`{"source":"canonical_fallback","canonical_model_id":"%s"}`, canonical.ID))
		if _, err := pricingRepo.Upsert(ctx, store.UpsertSiteModelPricingParams{
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
			PreserveManual:       true,
			Available:            true,
			Raw:                  raw,
		}); err != nil {
			unpriced = append(unpriced, model.UpstreamName)
			continue
		}
	}
	return unpriced
}

func syncedPricingSource(siteType string, raw map[string]any) string {
	if siteType == "newapi" {
		return "newapi"
	}
	if raw != nil {
		if source, ok := raw["source"].(string); ok && strings.TrimSpace(source) != "" {
			return strings.TrimSpace(source)
		}
	}
	return "unknown"
}

func (s *Service) backfillCanonicalPricingRatios(ctx context.Context, pricingRepo store.SiteModelPricingRepository, canonicalRepo store.CanonicalModelRepository, model store.SiteModel, existing []store.SiteModelPricing) {
	if !model.CanonicalID.Valid {
		return
	}
	needsBackfill := false
	for _, p := range existing {
		if !p.Available || p.ManualOverride {
			continue
		}
		if !p.CreateCache1hRatio.Valid || p.PricingSource == "canonical_fallback" && (!p.AudioRatio.Valid || !p.AudioCompletionRatio.Valid) {
			needsBackfill = true
			break
		}
	}
	if !needsBackfill {
		return
	}
	canonical, err := canonicalRepo.GetByID(ctx, model.CanonicalID.UUID)
	if err != nil {
		return
	}
	for _, p := range existing {
		if !p.Available || p.ManualOverride {
			continue
		}
		changed := false
		if !p.CreateCache1hRatio.Valid && canonical.CacheWrite1hRatio.Valid {
			p.CreateCache1hRatio = canonical.CacheWrite1hRatio
			changed = true
		}
		if p.PricingSource == "canonical_fallback" && !p.AudioRatio.Valid && canonical.AudioRatio.Valid {
			p.AudioRatio = canonical.AudioRatio
			changed = true
		}
		if p.PricingSource == "canonical_fallback" && !p.AudioCompletionRatio.Valid && canonical.AudioCompletionRatio.Valid {
			p.AudioCompletionRatio = canonical.AudioCompletionRatio
			changed = true
		}
		if changed {
			_ = pricingRepo.Save(ctx, p)
		}
	}
}

func nullableFloat64(value float64, ok bool) any {
	if !ok {
		return nil
	}
	return value
}

func credentialEnabledFromMeta(raw []byte) bool {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	return boolFromMeta(meta, "enabled", true)
}

func jsonBytes(value any) []byte {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableInt64(value int64, ok bool) any {
	if !ok {
		return nil
	}
	return value
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func anyString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolFromAny(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}

func int64FromAny(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case json.Number:
		parsed, err := strconv.ParseInt(v.String(), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
