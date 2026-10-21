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
	return s.grokAccountPool().refresh(ctx, siteID, credentialID)
}

// syncGrokQuotaSummary writes the sites-table quota from the account billing
// snapshots. A zero monthly limit is an uncapped subscription. current may be
// the zero value to load the site.
func (s *Service) syncGrokQuotaSummary(ctx context.Context, siteID uuid.UUID, current store.Site) store.Site {
	siteRepo := store.NewSiteRepository(s.db.DB())
	item := current
	if item.ID == uuid.Nil {
		loaded, err := siteRepo.GetByID(ctx, siteID)
		if err != nil {
			return current
		}
		item = loaded
	}
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return item
	}
	states, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return item
	}
	statesByCredential := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredential[state.SiteCredentialID] = state
	}
	var accounts, known, capped int
	var used, remaining, limit float64
	uncapped := false
	for _, credential := range credentials {
		if !isGrokCredentialType(credential.CredentialType) {
			continue
		}
		accounts++
		state := statesByCredential[credential.ID]
		if !state.Enabled || len(state.Usage) == 0 {
			continue
		}
		var usage struct {
			Status    string   `json:"quota_status"`
			Unlimited bool     `json:"unlimited"`
			Limit     *float64 `json:"limit"`
			Used      *float64 `json:"used"`
			Remaining *float64 `json:"remaining"`
		}
		if json.Unmarshal(state.Usage, &usage) != nil || usage.Status != "ok" {
			continue
		}
		known++
		if usage.Used != nil {
			used += *usage.Used
		}
		if usage.Unlimited {
			uncapped = true
			continue
		}
		capped++
		if usage.Remaining != nil {
			remaining += *usage.Remaining
		}
		if usage.Limit != nil {
			limit += *usage.Limit
		}
	}
	meta := siteMetaMap(item)
	if accounts == 0 || known == 0 {
		if _, ok := meta[siteMetaQuotaProbeSummary]; !ok {
			return item
		}
		delete(meta, siteMetaQuotaProbeSummary)
	} else {
		summary := map[string]any{
			"probe_type":       "grok",
			"status":           "ok",
			"unit":             "percent",
			"credential_count": accounts,
			"ok_count":         known,
			"fetched_at":       time.Now().UTC().Format(time.RFC3339),
		}
		if uncapped {
			summary["unlimited"] = true
			summary["used_total"] = used
		} else if capped > 0 {
			summary["remaining_min"] = remaining
			summary["limit"] = limit
			summary["used"] = used
		}
		meta[siteMetaQuotaProbeSummary] = summary
	}
	updated, err := siteRepo.UpdateMeta(ctx, item.ID, store.JSON(jsonBytes(meta)))
	if err != nil {
		return item
	}
	return updated
}

func (s *Service) RefreshGrokAccounts(ctx context.Context, siteID uuid.UUID) ([]GrokRefreshBatchItem, error) {
	items, err := s.grokAccountPool().refreshAll(ctx, siteID)
	if err != nil {
		return nil, err
	}
	results := make([]GrokRefreshBatchItem, 0, len(items))
	for _, item := range items {
		results = append(results, GrokRefreshBatchItem{
			CredentialID: item.CredentialID,
			Account:      item.Account,
			Error:        item.Error,
		})
	}
	return results, nil
}

func persistAccountRefresh(ctx context.Context, db *store.Store, credential store.SiteCredential, summary adapter.APIKeySummary, now time.Time) (store.SiteAPIKeyState, []store.SiteAPIKeyModel, error) {
	tier, _, remaining, used, unlimited := accountRefreshQuotaValues(summary.Usage)
	meta := accountMetaMap(credential.Meta)
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

// applyAccountSummaryToState copies the account summary of a refresh (tier,
// quota, raw payload) into the key-state columns so a site refresh keeps the
// same data as an account refresh.
func applyAccountSummaryToState(params *store.UpsertSiteAPIKeyStateParams, summary adapter.APIKeySummary, name string) {
	tier, _, remaining, used, unlimited := accountRefreshQuotaValues(summary.Usage)
	params.Name = name
	params.GroupName = tier
	params.RemainQuota = remaining
	params.UsedQuota = used
	params.UnlimitedQuota = unlimited
	params.Usage = store.JSON(jsonBytes(summary.Usage))
	params.Raw = store.JSON(jsonBytes(summary.Raw))
}

// keepAccountStateQuota carries the last known quota over a failed refresh.
func keepAccountStateQuota(params *store.UpsertSiteAPIKeyStateParams, previous store.SiteAPIKeyState) {
	params.Name = previous.Name
	if previous.GroupName.Valid {
		params.GroupName = previous.GroupName.String
	}
	if previous.RemainQuota.Valid {
		params.RemainQuota = previous.RemainQuota.Int64
	}
	if previous.UsedQuota.Valid {
		params.UsedQuota = previous.UsedQuota.Int64
	}
	params.UnlimitedQuota = previous.UnlimitedQuota
	params.Usage = previous.Usage
	params.Raw = previous.Raw
}

func (s *Service) recordAccountRefreshFailure(ctx context.Context, credential store.SiteCredential, refreshErr error, needsReauth func(error) bool) {
	now := time.Now()
	message := refreshErr.Error()
	meta := accountMetaMap(credential.Meta)
	enabled := boolFromMeta(meta, "enabled", true)
	if needsReauth(refreshErr) {
		enabled = false
		meta["enabled"] = false
		meta["reauth_required"] = true
		_, _ = store.NewSiteCredentialRepository(s.db.DB()).UpdateMeta(ctx, credential.ID, store.JSON(jsonBytes(meta)))
	}
	params := store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: credential.ID,
		SiteID:           credential.SiteID,
		Enabled:          enabled,
		SyncStatus:       accountRefreshFailureStatus(refreshErr, needsReauth),
		SyncMessage:      message,
		LastSyncedAt:     now,
	}
	// Plugin account rows keep the last usage document. Built-in Grok callers of
	// this helper pass no Usage, and UpsertSyncResult still stores {} for that
	// empty value. The model-sync failure path keeps quota through
	// keepAccountStateQuota and Upsert, not here.
	if _, _, pluginAccount := store.ParseAccountCredentialType(credential.CredentialType); pluginAccount {
		previous := accountCredentialState(ctx, s.db.DB(), credential.ID)
		if previous.SiteCredentialID == credential.ID {
			params.Usage = previous.Usage
		}
	}
	state, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).UpsertSyncResult(ctx, params)
	if err == nil && !enabled && state.Enabled {
		_, _ = store.NewSiteAPIKeyStateRepository(s.db.DB()).UpdateEnabled(ctx, credential.ID, false)
	}
}

func accountRefreshFailureStatus(err error, needsReauth func(error) bool) string {
	if needsReauth(err) {
		return "reauth_required"
	}
	return "stale"
}

func shouldReauthorizeGrokCredential(err error) bool {
	return adapter.IsGrokUnauthorizedError(err) || errors.Is(err, oauthsvc.ErrGrokReauthorizationRequired)
}

func accountRefreshQuotaValues(usage any) (string, any, any, any, bool) {
	values, _ := usage.(map[string]any)
	tier := strings.TrimSpace(anyString(values["tier"]))
	if tier == "" {
		tier = "free"
	}
	unlimited, _ := values["unlimited"].(bool)
	return tier, accountRefreshOptionalInt64(values["limit"]), accountRefreshOptionalInt64(values["remaining"]), accountRefreshOptionalInt64(values["used"]), unlimited
}

func accountRefreshOptionalInt64(value any) any {
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
