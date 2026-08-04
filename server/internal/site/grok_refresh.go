package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

type GrokRefreshBatchItem struct {
	CredentialID uuid.UUID    `json:"credential_id"`
	Account      *GrokAccount `json:"account,omitempty"`
	Error        string       `json:"error,omitempty"`
}

func (s *Service) RefreshGrokAccount(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) (GrokAccount, error) {
	defer s.siteLocks.lock(siteID)()
	if err := s.requireGrokSite(ctx, siteID); err != nil {
		return GrokAccount{}, err
	}
	site, err := s.Get(ctx, siteID)
	if err != nil {
		return GrokAccount{}, err
	}
	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	credential, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		return GrokAccount{}, err
	}
	if credential.SiteID != siteID || !isGrokCredentialType(credential.CredentialType) {
		return GrokAccount{}, fmt.Errorf("grok account was not found")
	}
	module, ok := s.adapters.ModuleForSiteType("grok")
	if !ok {
		return GrokAccount{}, fmt.Errorf("grok adapter is not registered")
	}
	fetcher, ok := adapter.AsAPIKeySummaryFetcher(module)
	if !ok {
		return GrokAccount{}, fmt.Errorf("grok adapter does not support account refresh")
	}
	token, err := s.oauth.EnsureGrokAccessToken(ctx, credential.ID)
	if err != nil {
		wrapped := fmt.Errorf("grok account token: %w", err)
		s.recordGrokRefreshFailure(ctx, credential, wrapped)
		return GrokAccount{}, wrapped
	}
	summary, err := fetcher.SummarizeAPIKey(ctx, s.toAdapterSite(ctx, site), token)
	if err != nil {
		s.recordGrokRefreshFailure(ctx, credential, err)
		return GrokAccount{}, err
	}
	now := time.Now()
	state, models, err := persistGrokRefresh(ctx, s.db, credential, summary, now)
	if err != nil {
		return GrokAccount{}, err
	}
	siteModels, err := s.syncSiteModelsFromAPIKeyState(ctx, siteID, site.SiteType, site.BaseURL)
	if err != nil {
		return GrokAccount{}, err
	}
	if _, err := s.updateGrokSiteStateAfterRefresh(ctx, siteID, siteModels, now); err != nil {
		return GrokAccount{}, err
	}
	updated, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		updated = credential
		meta := grokMetaMap(updated.Meta)
		tier, _, _, _, _ := grokRefreshQuotaValues(summary.Usage)
		meta["tier"] = tier
		updated.Meta = store.JSON(jsonBytes(meta))
	}
	account, _ := grokAccountFromStore(updated, state, models)
	return account, nil
}

func (s *Service) RefreshGrokAccounts(ctx context.Context, siteID uuid.UUID) ([]GrokRefreshBatchItem, error) {
	accounts, err := s.ListGrokAccounts(ctx, siteID)
	if err != nil {
		return nil, err
	}
	results := make([]GrokRefreshBatchItem, 0, len(accounts))
	for _, account := range accounts {
		refreshed, refreshErr := s.RefreshGrokAccount(ctx, siteID, account.CredentialID)
		item := GrokRefreshBatchItem{CredentialID: account.CredentialID}
		if refreshErr != nil {
			item.Error = refreshErr.Error()
		} else {
			item.Account = &refreshed
		}
		results = append(results, item)
	}
	return results, nil
}

func persistGrokRefresh(ctx context.Context, db *store.Store, credential store.SiteCredential, summary adapter.APIKeySummary, now time.Time) (store.SiteAPIKeyState, []store.SiteAPIKeyModel, error) {
	tier, _, remaining, used, unlimited := grokRefreshQuotaValues(summary.Usage)
	meta := grokMetaMap(credential.Meta)
	meta["tier"] = tier
	if values, ok := summary.Usage.(map[string]any); ok {
		if email := strings.TrimSpace(anyString(values["email"])); email != "" {
			meta["email"] = email
		}
	}
	encodedMeta, err := json.Marshal(meta)
	if err != nil {
		return store.SiteAPIKeyState{}, nil, fmt.Errorf("marshal grok account meta: %w", err)
	}
	usage, err := json.Marshal(summary.Usage)
	if err != nil {
		return store.SiteAPIKeyState{}, nil, fmt.Errorf("marshal grok usage: %w", err)
	}
	raw, err := json.Marshal(summary.Raw)
	if err != nil {
		return store.SiteAPIKeyState{}, nil, fmt.Errorf("marshal grok summary: %w", err)
	}
	var state store.SiteAPIKeyState
	var models []store.SiteAPIKeyModel
	err = db.WithinTx(ctx, func(tx store.Tx) error {
		updatedCredential, err := store.NewSiteCredentialRepository(tx).UpdateMeta(ctx, credential.ID, encodedMeta)
		if err != nil {
			return err
		}
		credential = updatedCredential
		state, err = store.NewSiteAPIKeyStateRepository(tx).Upsert(ctx, store.UpsertSiteAPIKeyStateParams{
			SiteCredentialID: credential.ID,
			SiteID:           credential.SiteID,
			Name:             credential.MaskedSecret,
			Enabled:          boolFromMeta(meta, "enabled", true),
			GroupName:        tier,
			RemainQuota:      remaining,
			UsedQuota:        used,
			UnlimitedQuota:   unlimited,
			Usage:            usage,
			Raw:              raw,
			SyncStatus:       "synced",
			LastSyncedAt:     now,
		})
		if err != nil {
			return err
		}
		stored, err := upsertCredentialAPIKeyModels(ctx, store.NewSiteAPIKeyModelRepository(tx), credential.SiteID, credential.ID, summary.Models, now)
		if err != nil {
			return err
		}
		models = stored
		return nil
	})
	if err != nil {
		return store.SiteAPIKeyState{}, nil, err
	}
	return state, models, nil
}

func (s *Service) updateGrokSiteStateAfterRefresh(ctx context.Context, siteID uuid.UUID, siteModels []store.SiteModel, now time.Time) (store.SiteState, error) {
	apiKeyCount, err := s.grokSiteAPIKeyCount(ctx, siteID)
	if err != nil {
		return store.SiteState{}, err
	}
	repo := store.NewSiteStateRepository(s.db.DB())
	existing, err := repo.GetBySite(ctx, siteID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.SiteState{}, err
	}
	return repo.Upsert(ctx, store.UpsertSiteStateParams{
		SiteID:            siteID,
		ValidationOK:      true,
		ValidationMessage: nil,
		SyncStatus:        "synced",
		SyncMessage:       nil,
		LastSyncStartedAt: existing.LastSyncStartedAt,
		LastSyncedAt:      now,
		APIKeyCount:       apiKeyCount,
		ModelCount:        len(availableSiteModels(siteModels)),
		RawStatus:         existing.RawStatus,
		UserSummary:       existing.UserSummary,
		Pricing:           existing.Pricing,
		Checkin:           existing.Checkin,
	})
}

func (s *Service) grokSiteAPIKeyCount(ctx context.Context, siteID uuid.UUID) (int, error) {
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, credential := range credentials {
		if isGrokCredentialType(credential.CredentialType) {
			count++
		}
	}
	return count, nil
}

func (s *Service) recordGrokRefreshFailure(ctx context.Context, credential store.SiteCredential, refreshErr error) {
	now := time.Now()
	message := refreshErr.Error()
	meta := grokMetaMap(credential.Meta)
	enabled := boolFromMeta(meta, "enabled", true)
	if shouldReauthorizeGrokCredential(refreshErr) {
		enabled = false
		meta["enabled"] = false
		meta["reauth_required"] = true
		_, _ = store.NewSiteCredentialRepository(s.db.DB()).UpdateMeta(ctx, credential.ID, store.JSON(jsonBytes(meta)))
	}
	state, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).UpsertSyncResult(ctx, store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: credential.ID,
		SiteID:           credential.SiteID,
		Enabled:          enabled,
		SyncStatus:       grokRefreshFailureStatus(refreshErr),
		SyncMessage:      message,
		LastSyncedAt:     now,
	})
	if err == nil && !enabled && state.Enabled {
		_, _ = store.NewSiteAPIKeyStateRepository(s.db.DB()).UpdateEnabled(ctx, credential.ID, false)
	}
}

func grokRefreshFailureStatus(err error) string {
	if shouldReauthorizeGrokCredential(err) {
		return "reauth_required"
	}
	return "stale"
}

func shouldReauthorizeGrokCredential(err error) bool {
	return adapter.IsGrokUnauthorizedError(err) || errors.Is(err, oauthsvc.ErrGrokReauthorizationRequired)
}

func grokRefreshQuotaValues(usage any) (string, any, any, any, bool) {
	values, _ := usage.(map[string]any)
	tier := strings.TrimSpace(anyString(values["tier"]))
	if tier == "" {
		tier = "free"
	}
	unlimited, _ := values["unlimited"].(bool)
	return tier, grokRefreshOptionalInt64(values["limit"]), grokRefreshOptionalInt64(values["remaining"]), grokRefreshOptionalInt64(values["used"]), unlimited
}

func grokRefreshOptionalInt64(value any) any {
	switch typed := value.(type) {
	case json.Number:
		result, err := typed.Int64()
		if err == nil {
			return result
		}
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case sql.NullInt64:
		if typed.Valid {
			return typed.Int64
		}
	}
	return nil
}
