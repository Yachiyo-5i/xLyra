package site

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

// AccountCounts reports how many login accounts a site holds and how many of
// them can still serve traffic.
type AccountCounts struct {
	Total int `json:"total"`
	Valid int `json:"valid"`
}

// IsAccountLoginSite reports whether the site type keeps one credential per
// logged-in account.
func IsAccountLoginSite(siteType string) bool {
	desc, ok := LookupSiteType(siteType)
	return ok && siteTypeHasAccounts(desc)
}

func siteTypeHasAccounts(desc SiteTypeDescriptor) bool {
	return desc.Accounts != nil
}

func accountCredentialForSite(siteType, credentialType string) bool {
	switch {
	case isGrokSiteType(siteType):
		return isGrokCredentialType(credentialType)
	default:
		return store.IsAccountCredentialTypeOf(credentialType, siteType)
	}
}

func isGrokSiteType(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "grok")
}

// AccountCounts counts the accounts of an account-login site. An account is
// valid when it is enabled and its session has not been rejected upstream
// (reauthorization required or a failed sync). The result does not depend on
// the stored site state, which a failed site refresh resets to zero.
func (s *Service) AccountCounts(ctx context.Context, siteID uuid.UUID, siteType string) (AccountCounts, error) {
	var counts AccountCounts
	if !IsAccountLoginSite(siteType) {
		return counts, nil
	}
	credentials, err := store.NewSiteCredentialRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return counts, err
	}
	states, err := store.NewSiteAPIKeyStateRepository(s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return counts, err
	}
	statesByCredential := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredential[state.SiteCredentialID] = state
	}
	for _, credential := range credentials {
		if !accountCredentialForSite(siteType, credential.CredentialType) {
			continue
		}
		counts.Total++
		state, hasState := statesByCredential[credential.ID]
		if accountUsable(credential, state, hasState) {
			counts.Valid++
		}
	}
	return counts, nil
}

func accountUsable(credential store.SiteCredential, state store.SiteAPIKeyState, hasState bool) bool {
	meta := accountMetaMap(credential.Meta)
	if boolFromMeta(meta, "reauth_required", false) {
		return false
	}
	enabled := boolFromMeta(meta, "enabled", true)
	if hasState {
		enabled = state.Enabled
		switch strings.ToLower(strings.TrimSpace(state.SyncStatus)) {
		case "reauth_required", "failed":
			return false
		}
	}
	return enabled
}

func (s *Service) accountPoolCredentialCount(ctx context.Context, siteID uuid.UUID, siteType string) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	return accountPoolCredentialCountDB(ctx, s.db.DB(), siteID, siteType)
}

func accountPoolCredentialCountDB(ctx context.Context, db *gorm.DB, siteID uuid.UUID, siteType string) (int, error) {
	credentials, err := store.NewSiteCredentialRepository(db).ListBySite(ctx, siteID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, credential := range credentials {
		if accountCredentialForSite(siteType, credential.CredentialType) {
			count++
		}
	}
	return count, nil
}
