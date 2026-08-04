package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

const grokCredentialPrefix = "grok_sso:"

type GrokAccountUpdate struct {
	Enabled *bool
}

type GrokAccountModel struct {
	UpstreamName string `json:"upstream_name"`
	DisplayName  string `json:"display_name"`
	Enabled      bool   `json:"enabled"`
}

type GrokAccount struct {
	CredentialID   uuid.UUID          `json:"credential_id"`
	MaskedToken    string             `json:"masked_token"`
	Enabled        bool               `json:"enabled"`
	Tier           string             `json:"tier,omitempty"`
	SyncStatus     string             `json:"sync_status,omitempty"`
	SyncMessage    string             `json:"sync_message,omitempty"`
	RemainingQuota int64              `json:"remaining_quota"`
	UsedQuota      int64              `json:"used_quota"`
	ModelCount     int                `json:"model_count"`
	Models         []GrokAccountModel `json:"models"`
}

func (s *Service) ListGrokAccounts(ctx context.Context, siteID uuid.UUID) ([]GrokAccount, error) {
	if err := s.requireGrokSite(ctx, siteID); err != nil {
		return nil, err
	}
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	states, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	models, err := store.NewSiteAPIKeyModelRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	statesByCredential := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredential[state.SiteCredentialID] = state
	}
	modelsByCredential := map[uuid.UUID][]store.SiteAPIKeyModel{}
	for _, model := range models {
		modelsByCredential[model.SiteCredentialID] = append(modelsByCredential[model.SiteCredentialID], model)
	}
	accounts := make([]GrokAccount, 0, len(credentials))
	for _, credential := range credentials {
		account, ok := grokAccountFromStore(credential, statesByCredential[credential.ID], modelsByCredential[credential.ID])
		if ok {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (s *Service) UpdateGrokAccount(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, input GrokAccountUpdate) (GrokAccount, error) {
	if err := s.requireGrokSite(ctx, siteID); err != nil {
		return GrokAccount{}, err
	}
	var updated store.SiteCredential
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		credentialRepo := store.NewSiteCredentialRepository(tx)
		stateRepo := store.NewSiteAPIKeyStateRepository(tx)
		credential, err := credentialRepo.GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if credential.SiteID != siteID || !isGrokCredentialType(credential.CredentialType) {
			return fmt.Errorf("grok account was not found")
		}
		meta := grokMetaMap(credential.Meta)
		enable := input.Enabled != nil && *input.Enabled
		disable := input.Enabled != nil && !*input.Enabled
		if input.Enabled != nil {
			if enable && boolFromMeta(meta, "reauth_required", false) {
				return fmt.Errorf("grok account requires reauthorization")
			}
			meta["enabled"] = *input.Enabled
		}
		encoded, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal grok account meta: %w", err)
		}
		updated, err = credentialRepo.UpdateMeta(ctx, credential.ID, encoded)
		if err != nil {
			return err
		}
		if enable {
			if err := store.NewRouteCooldownRepository(tx).DeleteBySiteCredential(ctx, siteID, credentialID); err != nil {
				return err
			}
		}
		if enable || disable {
			states, err := stateRepo.ListBySite(ctx, siteID)
			if err != nil {
				return err
			}
			for _, state := range states {
				if state.SiteCredentialID == credentialID {
					if enable {
						_, err = stateRepo.RecoverPending(ctx, credentialID)
					} else {
						_, err = stateRepo.UpdateEnabled(ctx, credentialID, false)
					}
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return GrokAccount{}, err
	}
	state := grokAccountState(ctx, s.db.DB(), credentialID)
	models, _ := store.NewSiteAPIKeyModelRepository(s.db.DB()).ListByCredential(ctx, credentialID)
	account, _ := grokAccountFromStore(updated, state, models)
	return account, nil
}

func (s *Service) SetGrokAccountModelEnabled(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, modelName string, enabled bool) (GrokAccount, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return GrokAccount{}, fmt.Errorf("model name is required")
	}
	if err := s.requireGrokSite(ctx, siteID); err != nil {
		return GrokAccount{}, err
	}
	defer s.siteLocks.lock(siteID)()
	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	credential, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		return GrokAccount{}, err
	}
	if credential.SiteID != siteID || !isGrokCredentialType(credential.CredentialType) {
		return GrokAccount{}, fmt.Errorf("grok account was not found")
	}
	meta := grokMetaMap(credential.Meta)
	disabledModels := stringSetFromAny(meta["disabled_models"])
	if enabled {
		delete(disabledModels, modelName)
	} else {
		disabledModels[modelName] = struct{}{}
	}
	meta["disabled_models"] = sortedStringSet(disabledModels)
	updatedAPIKeyModel, _ := store.NewSiteAPIKeyModelRepository(s.db.DB()).UpdateEnabled(ctx, credential.ID, modelName, enabled)
	_ = s.syncSiteModelStatusFromAPIKeyModels(ctx, siteID, modelName, updatedAPIKeyModel)
	updated, err := updateCredentialMeta(ctx, credentialRepo, credential.ID, meta)
	if err != nil {
		return GrokAccount{}, err
	}
	state := grokAccountState(ctx, s.db.DB(), credentialID)
	models, _ := store.NewSiteAPIKeyModelRepository(s.db.DB()).ListByCredential(ctx, credentialID)
	account, _ := grokAccountFromStore(updated, state, models)
	return account, nil
}

func (s *Service) DeleteGrokAccount(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) error {
	if err := s.requireGrokSite(ctx, siteID); err != nil {
		return err
	}
	return s.db.WithinTx(ctx, func(tx store.Tx) error {
		credential, err := store.NewSiteCredentialRepository(tx).GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if credential.SiteID != siteID || !isGrokCredentialType(credential.CredentialType) {
			return fmt.Errorf("grok account was not found")
		}
		return deleteGrokAccountRelations(ctx, tx, siteID, credentialID)
	})
}

func (s *Service) CompleteGrokDeviceLogin(ctx context.Context, siteID uuid.UUID, tokens oauthsvc.GrokTokens, targetCredentialID *uuid.UUID) (GrokAccount, error) {
	if err := s.requireGrokSite(ctx, siteID); err != nil {
		return GrokAccount{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return GrokAccount{}, fmt.Errorf("grok tokens are missing an access token")
	}
	if strings.TrimSpace(tokens.PrincipalID) == "" {
		tokens.PrincipalID, tokens.TeamID = oauthsvc.GrokTokenIdentity(tokens.AccessToken)
	}
	defer s.siteLocks.lock(siteID)()
	secret, err := oauthsvc.EncodeGrokBundle(oauthsvc.GrokBundleFromTokens(tokens))
	if err != nil {
		return GrokAccount{}, err
	}
	encrypted, _, err := s.credentials.Encrypt(secret)
	if err != nil {
		return GrokAccount{}, err
	}
	var credential store.SiteCredential
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		credentialRepo := store.NewSiteCredentialRepository(tx)
		credentials, err := credentialRepo.ListBySite(ctx, siteID)
		if err != nil {
			return err
		}
		existing, found, err := s.resolveGrokCredentialForLogin(credentials, tokens, siteID, targetCredentialID)
		if err != nil {
			return err
		}
		meta := map[string]any{}
		credentialType := grokCredentialPrefix + uuid.NewString()
		if found {
			meta = grokMetaMap(existing.Meta)
			credentialType = existing.CredentialType
		}
		meta["enabled"] = true
		delete(meta, "reauth_required")
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
		encodedMeta, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal grok account meta: %w", err)
		}
		credential, err = credentialRepo.Upsert(ctx, store.UpsertSiteCredentialParams{
			SiteID:          siteID,
			CredentialType:  credentialType,
			EncryptedSecret: encrypted,
			MaskedSecret:    grokAccountMaskedLabel(tokens),
			Meta:            encodedMeta,
		})
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if err := store.NewRouteCooldownRepository(tx).DeleteBySiteCredential(ctx, siteID, credential.ID); err != nil {
			return err
		}
		states, err := store.NewSiteAPIKeyStateRepository(tx).ListBySite(ctx, siteID)
		if err != nil {
			return err
		}
		for _, state := range states {
			if state.SiteCredentialID == credential.ID {
				_, err = store.NewSiteAPIKeyStateRepository(tx).RecoverPending(ctx, credential.ID)
				return err
			}
		}
		return nil
	})
	if err != nil {
		return GrokAccount{}, err
	}
	state := grokAccountState(ctx, s.db.DB(), credential.ID)
	models, _ := store.NewSiteAPIKeyModelRepository(s.db.DB()).ListByCredential(ctx, credential.ID)
	account, _ := grokAccountFromStore(credential, state, models)
	return account, nil
}

func (s *Service) resolveGrokCredentialForLogin(credentials []store.SiteCredential, tokens oauthsvc.GrokTokens, siteID uuid.UUID, targetCredentialID *uuid.UUID) (store.SiteCredential, bool, error) {
	if targetCredentialID == nil || *targetCredentialID == uuid.Nil {
		credential, found := s.matchGrokCredentialIdentity(credentials, tokens)
		return credential, found, nil
	}
	for _, credential := range credentials {
		if credential.ID == *targetCredentialID && credential.SiteID == siteID && isGrokCredentialType(credential.CredentialType) {
			return credential, true, nil
		}
	}
	return store.SiteCredential{}, false, fmt.Errorf("grok account was not found")
}

func (s *Service) matchGrokCredentialIdentity(credentials []store.SiteCredential, tokens oauthsvc.GrokTokens) (store.SiteCredential, bool) {
	wanted := grokIdentityKey(tokens.PrincipalID, tokens.TeamID)
	if wanted == "" {
		return store.SiteCredential{}, false
	}
	for _, credential := range credentials {
		if !isGrokCredentialType(credential.CredentialType) {
			continue
		}
		meta := grokMetaMap(credential.Meta)
		identity := grokIdentityKey(anyString(meta["principal_id"]), anyString(meta["team_id"]))
		if identity == "" {
			decrypted, err := s.credentials.Decrypt(credential.EncryptedSecret)
			if err == nil {
				bundle, decodeErr := oauthsvc.DecodeGrokBundle(decrypted)
				if decodeErr == nil {
					principalID, teamID := oauthsvc.GrokTokenIdentity(bundle.AccessToken)
					identity = grokIdentityKey(principalID, teamID)
				}
			}
		}
		if identity == wanted {
			return credential, true
		}
	}
	return store.SiteCredential{}, false
}

func grokIdentityKey(principalID string, teamID string) string {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return ""
	}
	return strings.TrimSpace(teamID) + ":" + principalID
}

func grokAccountMaskedLabel(tokens oauthsvc.GrokTokens) string {
	if email := strings.TrimSpace(tokens.Email); email != "" {
		return email
	}
	token := strings.TrimSpace(tokens.AccessToken)
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "…" + token[len(token)-4:]
}

func grokAccountFromStore(credential store.SiteCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel) (GrokAccount, bool) {
	if !isGrokCredentialType(credential.CredentialType) {
		return GrokAccount{}, false
	}
	meta := grokMetaMap(credential.Meta)
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
		accountModels = append(accountModels, GrokAccountModel{
			UpstreamName: model.UpstreamModelName,
			DisplayName:  display,
			Enabled:      model.Enabled,
		})
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
	return account, true
}

func deleteGrokAccountRelations(ctx context.Context, tx store.Tx, siteID uuid.UUID, credentialID uuid.UUID) error {
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
	if !strings.HasPrefix(value, grokCredentialPrefix) {
		return false
	}
	_, err := uuid.Parse(strings.TrimPrefix(value, grokCredentialPrefix))
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

func grokMetaMap(raw []byte) map[string]any {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	return meta
}

func grokAccountState(ctx context.Context, db *gorm.DB, credentialID uuid.UUID) store.SiteAPIKeyState {
	var state store.SiteAPIKeyState
	_ = db.WithContext(ctx).Where(&store.SiteAPIKeyState{SiteCredentialID: credentialID}).First(&state).Error
	return state
}
