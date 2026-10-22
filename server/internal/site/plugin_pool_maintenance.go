package site

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

// upsertPluginAccountSiteState writes the site card's account and model counts.
// keepStatus leaves the previous sync status and message in place (enable,
// disable, delete). A refresh passes keepStatus false and marks the site synced.
func (s *Service) upsertPluginAccountSiteState(ctx context.Context, siteID uuid.UUID, siteType string, syncStatus string, syncMessage any, siteModels []store.SiteModel, now time.Time, keepStatus bool) error {
	if s == nil || s.db == nil || siteID == uuid.Nil {
		return nil
	}
	return s.upsertPluginAccountSiteStateOn(ctx, s.db.DB(), siteID, siteType, syncStatus, syncMessage, siteModels, now, keepStatus)
}

func (s *Service) upsertPluginAccountSiteStateOn(ctx context.Context, db *gorm.DB, siteID uuid.UUID, siteType string, syncStatus string, syncMessage any, siteModels []store.SiteModel, now time.Time, keepStatus bool) error {
	if s == nil || db == nil || siteID == uuid.Nil {
		return nil
	}
	count, err := accountPoolCredentialCountDB(ctx, db, siteID, siteType)
	if err != nil {
		return err
	}
	models := siteModels
	if models == nil {
		loaded, loadErr := store.NewSiteModelRepository(db).ListBySite(ctx, siteID)
		if loadErr != nil {
			return loadErr
		}
		models = loaded
	}
	repo := store.NewSiteStateRepository(db)
	existing, err := repo.GetBySite(ctx, siteID)
	if err != nil && !errorsIsNotFound(err) {
		return err
	}
	status := strings.TrimSpace(syncStatus)
	message := syncMessage
	validationOK := true
	var validationMessage any
	var lastStarted any
	var lastSynced any = now
	if keepStatus {
		validationOK = existing.ValidationOK.Valid && existing.ValidationOK.Bool
		if existing.ValidationMessage.Valid {
			validationMessage = existing.ValidationMessage.String
		}
		status = existing.SyncStatus
		message = nil
		if existing.SyncMessage.Valid {
			message = existing.SyncMessage.String
		}
		if existing.LastSyncedAt.Valid {
			lastSynced = existing.LastSyncedAt.Time
		}
	}
	if status == "" {
		status = "synced"
	}
	if existing.LastSyncStartedAt.Valid {
		lastStarted = existing.LastSyncStartedAt.Time
	}
	_, err = repo.Upsert(ctx, store.UpsertSiteStateParams{
		SiteID:            siteID,
		ValidationOK:      validationOK,
		ValidationMessage: validationMessage,
		SyncStatus:        status,
		SyncMessage:       message,
		LastSyncStartedAt: lastStarted,
		LastSyncedAt:      lastSynced,
		APIKeyCount:       count,
		ModelCount:        len(availableSiteModels(models)),
		RawStatus:         existing.RawStatus,
		UserSummary:       existing.UserSummary,
		Pricing:           existing.Pricing,
		Checkin:           existing.Checkin,
	})
	return err
}

func errorsIsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// finishPluginAccountPoolRefresh records the site state after RefreshAll.
// Account refresh already ran (lease, reauth, summary). This writes quota and
// the account/model counts. Model listing stays on syncModelsLocked; a missing
// lister does not fail the account refresh.
func (s *Service) finishPluginAccountPoolRefresh(ctx context.Context, site store.Site, api pluginAccountAPI, results []AccountRefreshResult, accountErr error) (RefreshResult, error) {
	if accountErr != nil {
		result, err := s.markRefreshFailed(ctx, site, accountErr.Error())
		result.Site = api.syncQuota(ctx, site.ID, result.Site)
		return result, err
	}
	enabled, failed, sample := pluginAccountRefreshStats(ctx, s, site.ID, api.siteType, results)
	if enabled > 0 && failed == enabled {
		message := fmt.Sprintf("all %d enabled accounts failed", enabled)
		if sample != "" {
			message = message + ": " + sample
		}
		result, err := s.markRefreshFailed(ctx, site, message)
		result.Site = api.syncQuota(ctx, site.ID, result.Site)
		return result, err
	}

	now := time.Now()
	syncStatus := "synced"
	var syncMessage any
	if failed > 0 {
		syncStatus = "partial"
		syncMessage = fmt.Sprintf("%d of %d accounts failed", failed, enabled)
	}

	modelCount := 0
	var models []store.SiteModel
	syncResult, syncErr := s.syncModelsLocked(ctx, site.ID)
	switch {
	case syncErr == nil && syncResult.ModelCount > 0:
		models = syncResult.Models
		modelCount = syncResult.ModelCount
	default:
		if syncErr != nil && !pluginModelSyncSkippable(syncErr) {
			syncStatus = "partial"
			syncMessage = appendSyncMessage(syncMessage, syncErr.Error())
		}
		if syncErr != nil && pluginModelSyncSkippable(syncErr) && (api.pool == nil || !api.pool.ModelsPerAccount) {
			syncStatus = "partial"
			syncMessage = appendSyncMessage(syncMessage, "model sync skipped: models.perAccount is not enabled")
		}
		stored, loadErr := store.NewSiteModelRepository(s.db.DB()).ListBySite(ctx, site.ID)
		if loadErr != nil {
			return RefreshResult{}, loadErr
		}
		models = availableSiteModels(stored)
		modelCount = len(models)
	}

	pricing := store.JSON(nil)
	var pricingGroups []store.SitePricingGroup
	var modelPricings []store.SiteModelPricing
	if snapshot, groups, pricings, bound, pricingErr := s.syncPluginPricing(ctx, site, models, now); bound {
		if pricingErr != nil {
			syncStatus = "partial"
			syncMessage = appendSyncMessage(syncMessage, pricingErr.Error())
		} else {
			pricingGroups, modelPricings = groups, pricings
			if encoded := jsonBytes(snapshot.Raw); len(encoded) > 0 && string(encoded) != "null" {
				pricing = encoded
			}
		}
	}

	accountCount, err := s.accountPoolCredentialCount(ctx, site.ID, api.siteType)
	if err != nil {
		return RefreshResult{}, err
	}
	// No accounts yet: stay synced, the same as a built-in account pool before
	// the first login. Model sync has nothing to list and must not mark the site down.
	if accountCount == 0 {
		syncStatus = "synced"
		syncMessage = nil
	}
	repo := store.NewSiteStateRepository(s.db.DB())
	existing, err := repo.GetBySite(ctx, site.ID)
	if err != nil && !errorsIsNotFound(err) {
		return RefreshResult{}, err
	}
	statePricing := existing.Pricing
	if len(pricing) > 0 {
		statePricing = pricing
	}
	state, err := repo.Upsert(ctx, store.UpsertSiteStateParams{
		SiteID:            site.ID,
		ValidationOK:      true,
		SyncStatus:        syncStatus,
		SyncMessage:       syncMessage,
		LastSyncStartedAt: now,
		LastSyncedAt:      now,
		APIKeyCount:       accountCount,
		ModelCount:        modelCount,
		RawStatus:         existing.RawStatus,
		UserSummary:       existing.UserSummary,
		Pricing:           statePricing,
		Checkin:           existing.Checkin,
	})
	if err != nil {
		return RefreshResult{}, err
	}
	site = s.restoreAutoDisabledSiteAfterRefreshSuccess(ctx, site)
	site = api.syncQuota(ctx, site.ID, site)
	models, state = s.matchAndPriceSiteModels(ctx, site, models, state)
	return RefreshResult{
		Site:          site,
		State:         state,
		Models:        models,
		PricingGroups: pricingGroups,
		ModelPricings: modelPricings,
	}, nil
}

func pluginModelSyncSkippable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "does not support model sync") || strings.Contains(message, "unsupported site_type")
}

func pluginAccountRefreshStats(ctx context.Context, s *Service, siteID uuid.UUID, siteType string, results []AccountRefreshResult) (enabled, failed int, sample string) {
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return 0, len(results), ""
	}
	states, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return 0, len(results), ""
	}
	byID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		byID[state.SiteCredentialID] = state
	}
	enabledByID := map[uuid.UUID]bool{}
	for _, credential := range credentials {
		if !store.IsAccountCredentialTypeOf(credential.CredentialType, siteType) {
			continue
		}
		meta := accountMetaMap(credential.Meta)
		on := boolFromMeta(meta, "enabled", true)
		if state, ok := byID[credential.ID]; ok && state.SiteCredentialID == credential.ID {
			on = state.Enabled
		}
		enabledByID[credential.ID] = on
	}
	for _, result := range results {
		if !enabledByID[result.CredentialID] {
			continue
		}
		enabled++
		if result.Error == "" {
			continue
		}
		failed++
		if sample == "" {
			sample = result.Error
		}
	}
	return enabled, failed, sample
}

// runPluginAccountHealth probes each enabled plugin account. Reauthorization
// disables the account.
// Any other failure records sync status stale and leaves the account enabled.
// The site stays healthy while at least one enabled account answers.
func (s *Service) runPluginAccountHealth(ctx context.Context, item store.Site, api pluginAccountAPI, start time.Time, check siteHealthCheck) siteHealthCheck {
	check.endpoint = "summarizeAccount"
	check.metadata["probe"] = "accounts"
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, item.ID)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = err.Error()
		return check
	}
	states, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, item.ID)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = err.Error()
		return check
	}
	byID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		byID[state.SiteCredentialID] = state
	}

	type healthAccount struct {
		credential store.SiteCredential
		enabled    bool
	}
	accountCreds := make([]healthAccount, 0, len(credentials))
	for _, credential := range credentials {
		if !store.IsAccountCredentialTypeOf(credential.CredentialType, item.SiteType) {
			continue
		}
		meta := accountMetaMap(credential.Meta)
		enabled := boolFromMeta(meta, "enabled", true)
		if state, ok := byID[credential.ID]; ok && state.SiteCredentialID == credential.ID {
			enabled = state.Enabled
		}
		accountCreds = append(accountCreds, healthAccount{credential: credential, enabled: enabled})
	}
	results := make([]map[string]any, len(accountCreds))
	probed := make([]bool, len(accountCreds))
	enabledIndexes := make([]int, 0, len(accountCreds))
	for index, account := range accountCreds {
		wrapped := APIKeyCredential{Credential: account.credential, Name: pluginHealthAccountName(account.credential)}
		if !account.enabled {
			results[index] = healthProbeCredentialResult(wrapped, "disabled", "")
			probed[index] = true
			continue
		}
		enabledIndexes = append(enabledIndexes, index)
	}
	workers := pluginAccountWorkers
	if workers > len(enabledIndexes) {
		workers = len(enabledIndexes)
	}
	if workers > 0 {
		jobs := make(chan int)
		var wg sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for index := range jobs {
					if ctx.Err() != nil {
						continue
					}
					credential := accountCreds[index].credential
					wrapped := APIKeyCredential{Credential: credential, Name: pluginHealthAccountName(credential)}
					status, message, probeErr := api.probeAccountHealth(ctx, credential)
					if probeErr != nil || contextCanceled(ctx, probeErr) {
						continue
					}
					results[index] = healthProbeCredentialResult(wrapped, status, message)
					probed[index] = true
				}
			}()
		}
		for _, index := range enabledIndexes {
			if ctx.Err() != nil {
				break
			}
			jobs <- index
		}
		close(jobs)
		wg.Wait()
	}
	total := 0
	passed := 0
	enabledCount := len(enabledIndexes)
	finished := 0
	visible := make([]map[string]any, 0, len(accountCreds))
	for index, account := range accountCreds {
		if !account.enabled {
			visible = append(visible, results[index])
			continue
		}
		if !probed[index] {
			continue
		}
		finished++
		total++
		status, _ := results[index]["status"].(string)
		if status == "ok" {
			passed++
		}
		visible = append(visible, results[index])
	}
	check.metadata["accounts"] = visible
	check.metadata["accounts_total"] = total
	check.metadata["accounts_passed"] = passed
	if ctx.Err() != nil && finished < enabledCount {
		check.success = false
		check.errorType = "timeout"
		check.message = ctx.Err().Error()
		check.latency = time.Since(start)
		return check
	}
	if len(accountCreds) == 0 {
		// Waiting for the first account. grokHealthOutcome treats zero
		// credentials as a missing credential; that failure snapshot is what
		// made an empty plugin pool look abnormal.
		check.success = true
		check.errorType = ""
		check.message = "ok"
		check.latency = time.Since(start)
		return check
	}
	check.success, check.errorType, check.message = grokHealthOutcome(len(accountCreds), total, passed)
	check.message = accountPoolHealthMessage(item.SiteType, check.message)
	check.latency = time.Since(start)
	return check
}

func pluginHealthAccountName(credential store.SiteCredential) string {
	if name := strings.TrimSpace(credential.DisplayName); name != "" {
		return name
	}
	meta := accountMetaMap(credential.Meta)
	if name := strings.TrimSpace(anyString(meta["identity"])); name != "" {
		return name
	}
	return strings.TrimSpace(credential.MaskedSecret)
}
