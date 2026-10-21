package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

const grokCredentialPrefix = store.CredentialPrefixGrok

type GrokAccountUpdate struct {
	Enabled *bool
}

// GrokAccountModel is one model of an OAuth account. The endpoint fields mirror
// the API key model payload so the same protocol editor works for accounts.
type GrokAccountModel struct {
	UpstreamName           string                                `json:"upstream_name"`
	DisplayName            string                                `json:"display_name"`
	Enabled                bool                                  `json:"enabled"`
	SiteModelID            string                                `json:"site_model_id,omitempty"`
	SupportedEndpointTypes []string                              `json:"supported_endpoint_types"`
	EffectiveEndpointTypes []string                              `json:"effective_endpoint_types"`
	AvailableEndpointTypes []string                              `json:"available_endpoint_types"`
	EndpointOverride       store.SiteAPIKeyModelEndpointOverride `json:"endpoint_override"`

	// record is the stored row, used to resolve the effective protocols.
	record store.SiteAPIKeyModel
}

type GrokAccount struct {
	CredentialID     uuid.UUID          `json:"credential_id"`
	MaskedToken      string             `json:"masked_token"`
	Enabled          bool               `json:"enabled"`
	Tier             string             `json:"tier,omitempty"`
	SyncStatus       string             `json:"sync_status,omitempty"`
	SyncMessage      string             `json:"sync_message,omitempty"`
	RemainingQuota   int64              `json:"remaining_quota"`
	UsedQuota        int64              `json:"used_quota"`
	QuotaKnown       bool               `json:"quota_known"`
	UsedPercent      *float64           `json:"used_percent,omitempty"`
	RemainingPercent *float64           `json:"remaining_percent,omitempty"`
	PeriodType       string             `json:"period_type,omitempty"`
	PeriodEnd        string             `json:"billing_period_end,omitempty"`
	QuotaProduct     string             `json:"quota_product,omitempty"`
	ModelCount       int                `json:"model_count"`
	Models           []GrokAccountModel `json:"models"`
	Entries          []QuotaProbeEntry  `json:"entries,omitempty"`
}

func (s *Service) ListGrokAccounts(ctx context.Context, siteID uuid.UUID) ([]GrokAccount, error) {
	return s.grokAccountPool().list(ctx, siteID)
}

func (s *Service) UpdateGrokAccount(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, input GrokAccountUpdate) (GrokAccount, error) {
	return s.grokAccountPool().update(ctx, siteID, credentialID, input.Enabled)
}

func (s *Service) SetGrokAccountModelEnabled(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, modelName string, enabled bool) (GrokAccount, error) {
	return s.grokAccountPool().setModelEnabled(ctx, siteID, credentialID, modelName, enabled)
}

// SetGrokAccountModelEndpoint adjusts the protocols one account may use for a
// model (the same override the API key model dialog edits).
func (s *Service) SetGrokAccountModelEndpoint(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, siteModelID uuid.UUID, mode string, endpointTypes []string) (GrokAccount, error) {
	return s.grokAccountPool().setModelEndpoint(ctx, siteID, credentialID, siteModelID, mode, endpointTypes)
}

func (s *Service) DeleteGrokAccount(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) error {
	return s.grokAccountPool().delete(ctx, siteID, credentialID)
}

func (s *Service) CompleteGrokDeviceLogin(ctx context.Context, siteID uuid.UUID, tokens oauthsvc.GrokTokens, targetCredentialID *uuid.UUID) (GrokAccount, error) {
	return s.grokLogin().complete(ctx, siteID, tokens, targetCredentialID)
}

func (s *Service) grokLogin() accountLogin[oauthsvc.GrokTokens, GrokAccount] {
	return accountLogin[oauthsvc.GrokTokens, GrokAccount]{s: s, p: grokLoginProvider{s: s}}
}

func (s *Service) resolveGrokCredentialForLogin(credentials []store.SiteCredential, tokens oauthsvc.GrokTokens, siteID uuid.UUID, targetCredentialID *uuid.UUID) (store.SiteCredential, bool, error) {
	return s.grokLogin().matchExisting(credentials, siteID, targetCredentialID, tokens)
}

func (s *Service) matchGrokCredentialIdentity(credentials []store.SiteCredential, tokens oauthsvc.GrokTokens) (store.SiteCredential, bool) {
	credential, found, _ := s.grokLogin().matchExisting(credentials, uuid.Nil, nil, tokens)
	return credential, found
}

type grokLoginProvider struct {
	s *Service
}

func (grokLoginProvider) name() string { return "grok" }

func (grokLoginProvider) isCredential(credentialType string) bool {
	return isGrokCredentialType(credentialType)
}

func (grokLoginProvider) credentialPrefix() string { return grokCredentialPrefix }

func (p grokLoginProvider) requireSite(ctx context.Context, siteID uuid.UUID) error {
	return p.s.requireGrokSite(ctx, siteID)
}

func (grokLoginProvider) normalize(tokens oauthsvc.GrokTokens) (oauthsvc.GrokTokens, error) {
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return tokens, fmt.Errorf("grok tokens are missing an access token")
	}
	if strings.TrimSpace(tokens.PrincipalID) == "" {
		tokens.PrincipalID, tokens.TeamID = oauthsvc.GrokTokenIdentity(tokens.AccessToken)
	}
	return tokens, nil
}

func (grokLoginProvider) encodeSecret(tokens oauthsvc.GrokTokens) (string, error) {
	return oauthsvc.EncodeGrokBundle(oauthsvc.GrokBundleFromTokens(tokens))
}

func (grokLoginProvider) identity(tokens oauthsvc.GrokTokens) string {
	return grokIdentityKey(tokens.PrincipalID, tokens.TeamID)
}

func (grokLoginProvider) metaIdentity(meta map[string]any) string {
	return grokIdentityKey(anyString(meta["principal_id"]), anyString(meta["team_id"]))
}

func (grokLoginProvider) secretIdentity(secret string) string {
	bundle, err := oauthsvc.DecodeGrokBundle(secret)
	if err != nil {
		return ""
	}
	principalID, teamID := oauthsvc.GrokTokenIdentity(bundle.AccessToken)
	return grokIdentityKey(principalID, teamID)
}

func (grokLoginProvider) applyMeta(meta map[string]any, tokens oauthsvc.GrokTokens) {
	if value := strings.TrimSpace(tokens.Tier); value != "" {
		meta["tier"] = value
	}
	if value := strings.TrimSpace(tokens.Email); value != "" {
		meta["email"] = value
	}
	if value := strings.TrimSpace(tokens.PrincipalID); value != "" {
		meta["principal_id"] = value
	}
	if value := strings.TrimSpace(tokens.TeamID); value != "" {
		meta["team_id"] = value
	}
}

func (grokLoginProvider) maskedLabel(tokens oauthsvc.GrokTokens) string {
	return accountMaskedLabel(tokens.Email, tokens.AccessToken)
}

func (p grokLoginProvider) snapshot(ctx context.Context, credential store.SiteCredential) GrokAccount {
	state := accountCredentialState(ctx, p.s.db.DB(), credential.ID)
	models, _ := store.NewSiteAPIKeyModelRepository(p.s.db.DB()).ListByCredential(ctx, credential.ID)
	account, _ := grokAccountFromStore(credential, state, models)
	account.Models = p.s.resolveAccountModelEndpoints(ctx, account.Models)
	return account
}

func grokIdentityKey(principalID string, teamID string) string {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return ""
	}
	return strings.TrimSpace(teamID) + ":" + principalID
}

func grokAccountFromStore(credential store.SiteCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel) (GrokAccount, bool) {
	if !isGrokCredentialType(credential.CredentialType) {
		return GrokAccount{}, false
	}
	meta := accountMetaMap(credential.Meta)
	enabled := boolFromMeta(meta, "enabled", true)
	if state.SiteCredentialID != uuid.Nil && state.SiteCredentialID == credential.ID {
		enabled = state.Enabled
	}
	label := strings.TrimSpace(anyString(meta["email"]))
	if label == "" {
		label = credential.MaskedSecret
	}
	accountModels := make([]GrokAccountModel, 0, len(models))
	modelCount := 0
	for _, model := range models {
		if !model.Available {
			continue
		}
		modelCount++
		display := strings.TrimSpace(model.DisplayName)
		if display == "" || adapter.IsGrokImageModel(model.UpstreamModelName) {
			display = model.UpstreamModelName
		}
		accountModels = append(accountModels, newAccountModel(model, display))
	}
	account := GrokAccount{
		CredentialID: credential.ID,
		MaskedToken:  label,
		Enabled:      enabled,
		Tier:         anyString(meta["tier"]),
		SyncStatus:   state.SyncStatus,
		ModelCount:   modelCount,
		Models:       accountModels,
	}
	if state.SyncMessage.Valid {
		account.SyncMessage = state.SyncMessage.String
	}
	if state.RemainQuota.Valid {
		account.RemainingQuota = state.RemainQuota.Int64
	}
	if state.UsedQuota.Valid {
		account.UsedQuota = state.UsedQuota.Int64
	}
	if len(state.Usage) > 0 {
		var usage struct {
			Status    string   `json:"quota_status"`
			Unit      string   `json:"unit"`
			Used      *float64 `json:"used"`
			Remaining *float64 `json:"remaining"`
			PeriodEnd string   `json:"period_end"`
			Period    string   `json:"period_type"`
			Product   string   `json:"product"`
		}
		if json.Unmarshal(state.Usage, &usage) == nil && usage.Status == "ok" && usage.Unit == "percent" {
			account.QuotaKnown = true
			account.UsedPercent = usage.Used
			account.RemainingPercent = usage.Remaining
			account.PeriodEnd = usage.PeriodEnd
			account.PeriodType = usage.Period
			account.QuotaProduct = usage.Product
		}
		account.Entries = entriesFromUsage(state.Usage)
	}
	return account, true
}

func entriesFromUsage(raw []byte) []QuotaProbeEntry {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Entries []QuotaProbeEntry `json:"entries"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.Entries) == 0 {
		return nil
	}
	return payload.Entries
}

func deleteAccountRelations(ctx context.Context, tx store.Tx, siteID uuid.UUID, credentialID uuid.UUID) error {
	if err := store.NewRouteCooldownRepository(tx).DeleteBySiteCredential(ctx, siteID, credentialID); err != nil {
		return err
	}
	if err := store.NewSiteAPIKeyModelRepository(tx).DeleteByCredential(ctx, credentialID); err != nil {
		return err
	}
	if err := store.NewSiteAPIKeyStateRepository(tx).DeleteByCredential(ctx, credentialID); err != nil {
		return err
	}
	return store.NewSiteCredentialRepository(tx).DeleteByID(ctx, credentialID)
}

func isGrokCredentialType(value string) bool {
	if !strings.HasPrefix(value, store.CredentialPrefixGrok) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(value, store.CredentialPrefixGrok))
	return err == nil
}

func (s *Service) requireGrokSite(ctx context.Context, siteID uuid.UUID) error {
	if siteID == uuid.Nil {
		return fmt.Errorf("site id is required")
	}
	item, err := s.Get(ctx, siteID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(item.SiteType), "grok") {
		return fmt.Errorf("site type %q is not grok", item.SiteType)
	}
	return nil
}

func accountMetaMap(raw []byte) map[string]any {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	return meta
}

func accountCredentialState(ctx context.Context, db *gorm.DB, credentialID uuid.UUID) store.SiteAPIKeyState {
	var state store.SiteAPIKeyState
	_ = db.WithContext(ctx).Where(&store.SiteAPIKeyState{SiteCredentialID: credentialID}).First(&state).Error
	return state
}

func (s *Service) grokAccountPool() accountPool[GrokAccount] {
	return accountPool[GrokAccount]{s: s, p: grokAccountProvider{s: s}}
}

type grokAccountProvider struct {
	s *Service
}

func (grokAccountProvider) name() string { return "grok" }

func (grokAccountProvider) isCredential(credentialType string) bool {
	return isGrokCredentialType(credentialType)
}

func (p grokAccountProvider) requireSite(ctx context.Context, siteID uuid.UUID) error {
	return p.s.requireGrokSite(ctx, siteID)
}

func (p grokAccountProvider) ensureAccess(ctx context.Context, site store.Site, cred store.SiteCredential) (string, adapter.SiteConfig, error) {
	token, err := p.s.oauth.EnsureGrokAccessToken(ctx, cred.ID)
	if err != nil {
		return "", adapter.SiteConfig{}, err
	}
	return token, p.s.toAdapterSite(ctx, site), nil
}

func (grokAccountProvider) shouldReauth(err error) bool {
	return shouldReauthorizeGrokCredential(err)
}

func (grokAccountProvider) fromStore(cred store.SiteCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel) (GrokAccount, bool) {
	return grokAccountFromStore(cred, state, models)
}

func (p grokAccountProvider) decorate(ctx context.Context, account GrokAccount) GrokAccount {
	account.Models = p.s.resolveAccountModelEndpoints(ctx, account.Models)
	return account
}

func (grokAccountProvider) credentialID(account GrokAccount) uuid.UUID {
	return account.CredentialID
}

func (p grokAccountProvider) syncQuota(ctx context.Context, siteID uuid.UUID, current store.Site) store.Site {
	return p.s.syncGrokQuotaSummary(ctx, siteID, current)
}

func (grokAccountProvider) resyncQuotaOnUpdateDelete() bool { return false }

func (grokAccountProvider) syncQuotaBeforeRefreshReread() bool { return false }

func (grokAccountProvider) onRefreshRereadMiss(cred store.SiteCredential, summary adapter.APIKeySummary) store.SiteCredential {
	meta := accountMetaMap(cred.Meta)
	tier, _, _, _, _ := accountRefreshQuotaValues(summary.Usage)
	meta["tier"] = tier
	cred.Meta = store.JSON(jsonBytes(meta))
	return cred
}

func (p grokAccountProvider) afterRefresh(ctx context.Context, siteID uuid.UUID, siteModels []store.SiteModel, now time.Time) error {
	_, err := p.s.updateGrokSiteStateAfterRefresh(ctx, siteID, siteModels, now)
	return err
}
