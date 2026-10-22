package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

type accountPoolProvider[A any] interface {
	name() string
	isCredential(credentialType string) bool
	requireSite(ctx context.Context, siteID uuid.UUID) error
	ensureAccess(ctx context.Context, site store.Site, cred store.SiteCredential) (token string, upstream adapter.SiteConfig, err error)
	shouldReauth(err error) bool
	fromStore(cred store.SiteCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel) (A, bool)
	decorate(ctx context.Context, account A) A
	credentialID(account A) uuid.UUID
	syncQuota(ctx context.Context, siteID uuid.UUID, current store.Site) store.Site
	resyncQuotaOnUpdateDelete() bool
	syncQuotaBeforeRefreshReread() bool
	onRefreshRereadMiss(cred store.SiteCredential, summary adapter.APIKeySummary) store.SiteCredential
	afterRefresh(ctx context.Context, siteID uuid.UUID, siteModels []store.SiteModel, now time.Time) error
}

type accountPool[A any] struct {
	s *Service
	p accountPoolProvider[A]
}

type accountRefreshItem[A any] struct {
	CredentialID uuid.UUID
	Account      *A
	Error        string
}

func (p accountPool[A]) list(ctx context.Context, siteID uuid.UUID) ([]A, error) {
	if err := p.p.requireSite(ctx, siteID); err != nil {
		return nil, err
	}
	credentials, err := store.NewSiteCredentialRepository(p.s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	states, err := store.NewSiteAPIKeyStateRepository(p.s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	models, err := store.NewSiteAPIKeyModelRepository(p.s.db.DB()).ListBySite(ctx, siteID)
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
	accounts := make([]A, 0, len(credentials))
	for _, credential := range credentials {
		account, ok := p.p.fromStore(credential, statesByCredential[credential.ID], modelsByCredential[credential.ID])
		if ok {
			accounts = append(accounts, p.p.decorate(ctx, account))
		}
	}
	return accounts, nil
}

func (p accountPool[A]) update(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, enabled *bool) (A, error) {
	var zero A
	if err := p.p.requireSite(ctx, siteID); err != nil {
		return zero, err
	}
	var updated store.SiteCredential
	err := p.s.db.WithinTx(ctx, func(tx store.Tx) error {
		credentialRepo := store.NewSiteCredentialRepository(tx)
		stateRepo := store.NewSiteAPIKeyStateRepository(tx)
		credential, err := credentialRepo.GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if credential.SiteID != siteID || !p.p.isCredential(credential.CredentialType) {
			return p.notFound()
		}
		meta := accountMetaMap(credential.Meta)
		enable := enabled != nil && *enabled
		disable := enabled != nil && !*enabled
		if enabled != nil {
			if enable && boolFromMeta(meta, "reauth_required", false) {
				return p.reauthRequired()
			}
			meta["enabled"] = *enabled
		}
		encoded, err := json.Marshal(meta)
		if err != nil {
			return p.marshalMetaError(err)
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
		return zero, err
	}
	if p.p.resyncQuotaOnUpdateDelete() {
		p.p.syncQuota(ctx, siteID, store.Site{})
	}
	return p.snapshot(ctx, updated), nil
}

func (p accountPool[A]) setModelEnabled(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, modelName string, enabled bool) (A, error) {
	var zero A
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return zero, fmt.Errorf("model name is required")
	}
	if err := p.p.requireSite(ctx, siteID); err != nil {
		return zero, err
	}
	defer p.s.siteLocks.lock(siteID)()
	credentialRepo := store.NewSiteCredentialRepository(p.s.db.DB())
	credential, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		return zero, err
	}
	if credential.SiteID != siteID || !p.p.isCredential(credential.CredentialType) {
		return zero, p.notFound()
	}
	meta := accountMetaMap(credential.Meta)
	disabledModels := stringSetFromAny(meta["disabled_models"])
	if enabled {
		delete(disabledModels, modelName)
	} else {
		disabledModels[modelName] = struct{}{}
	}
	meta["disabled_models"] = sortedStringSet(disabledModels)
	var updated store.SiteCredential
	err = p.s.db.WithinTx(ctx, func(tx store.Tx) error {
		updatedModel, err := store.NewSiteAPIKeyModelRepository(tx).UpdateEnabled(ctx, credential.ID, modelName, enabled)
		if err != nil {
			if !errorsIsNotFound(err) {
				return err
			}
		} else if err := syncSiteModelStatusOn(ctx, tx, siteID, modelName, updatedModel); err != nil {
			return err
		}
		updated, err = updateCredentialMeta(ctx, store.NewSiteCredentialRepository(tx), credential.ID, meta)
		return err
	})
	if err != nil {
		return zero, err
	}
	return p.snapshot(ctx, updated), nil
}

func (p accountPool[A]) setModelEndpoint(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, siteModelID uuid.UUID, mode string, endpointTypes []string) (A, error) {
	var zero A
	if err := p.p.requireSite(ctx, siteID); err != nil {
		return zero, err
	}
	credential, err := p.s.setAccountModelEndpointOverride(ctx, siteID, credentialID, siteModelID, mode, endpointTypes, p.p.isCredential)
	if err != nil {
		return zero, err
	}
	return p.snapshot(ctx, credential), nil
}

func (p accountPool[A]) delete(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) error {
	if err := p.p.requireSite(ctx, siteID); err != nil {
		return err
	}
	err := p.s.db.WithinTx(ctx, func(tx store.Tx) error {
		credential, err := store.NewSiteCredentialRepository(tx).GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if credential.SiteID != siteID || !p.p.isCredential(credential.CredentialType) {
			return p.notFound()
		}
		return deleteAccountRelations(ctx, tx, siteID, credentialID)
	})
	if err == nil && p.p.resyncQuotaOnUpdateDelete() {
		p.p.syncQuota(ctx, siteID, store.Site{})
	}
	return err
}

func (p accountPool[A]) refresh(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) (A, error) {
	var zero A
	defer p.s.siteLocks.lock(siteID)()
	if err := p.p.requireSite(ctx, siteID); err != nil {
		return zero, err
	}
	site, err := p.s.Get(ctx, siteID)
	if err != nil {
		return zero, err
	}
	credentialRepo := store.NewSiteCredentialRepository(p.s.db.DB())
	credential, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		return zero, err
	}
	if credential.SiteID != siteID || !p.p.isCredential(credential.CredentialType) {
		return zero, p.notFound()
	}
	module, ok := p.s.adapters.ModuleForSiteType(p.p.name())
	if !ok {
		return zero, p.adapterMissing()
	}
	fetcher, ok := adapter.AsAPIKeySummaryFetcher(module)
	if !ok {
		return zero, p.adapterUnsupported()
	}
	token, upstream, err := p.p.ensureAccess(ctx, site, credential)
	if err != nil {
		wrapped := p.tokenError(err)
		p.s.recordAccountRefreshFailure(ctx, credential, wrapped, p.p.shouldReauth)
		return zero, wrapped
	}
	summary, err := fetcher.SummarizeAPIKey(ctx, upstream, token)
	if err != nil {
		p.s.recordAccountRefreshFailure(ctx, credential, err, p.p.shouldReauth)
		return zero, err
	}
	now := time.Now()
	state, models, err := persistAccountRefresh(ctx, p.s.db, credential, summary, now)
	if err != nil {
		return zero, err
	}
	siteModels, err := p.s.syncSiteModelsFromAPIKeyState(ctx, siteID, site.SiteType, site.BaseURL)
	if err != nil {
		return zero, err
	}
	if err := p.p.afterRefresh(ctx, siteID, siteModels, now); err != nil {
		return zero, err
	}
	if p.p.syncQuotaBeforeRefreshReread() {
		p.p.syncQuota(ctx, siteID, store.Site{})
	}
	updated, err := credentialRepo.GetByID(ctx, credentialID)
	if err != nil {
		updated = p.p.onRefreshRereadMiss(credential, summary)
	}
	account, _ := p.p.fromStore(updated, state, models)
	account = p.p.decorate(ctx, account)
	if !p.p.syncQuotaBeforeRefreshReread() {
		p.p.syncQuota(ctx, siteID, store.Site{})
	}
	return account, nil
}

func (p accountPool[A]) refreshAll(ctx context.Context, siteID uuid.UUID) ([]accountRefreshItem[A], error) {
	accounts, err := p.list(ctx, siteID)
	if err != nil {
		return nil, err
	}
	results := make([]accountRefreshItem[A], 0, len(accounts))
	for _, account := range accounts {
		credentialID := p.p.credentialID(account)
		refreshed, refreshErr := p.refresh(ctx, siteID, credentialID)
		item := accountRefreshItem[A]{CredentialID: credentialID}
		if refreshErr != nil {
			item.Error = refreshErr.Error()
		} else {
			item.Account = &refreshed
		}
		results = append(results, item)
	}
	return results, nil
}

func (p accountPool[A]) snapshot(ctx context.Context, credential store.SiteCredential) A {
	state := accountCredentialState(ctx, p.s.db.DB(), credential.ID)
	models, _ := store.NewSiteAPIKeyModelRepository(p.s.db.DB()).ListByCredential(ctx, credential.ID)
	account, _ := p.p.fromStore(credential, state, models)
	return p.p.decorate(ctx, account)
}

func (p accountPool[A]) notFound() error {
	return fmt.Errorf("%s account was not found", p.p.name())
}

func (p accountPool[A]) reauthRequired() error {
	return fmt.Errorf("%s account requires reauthorization", p.p.name())
}

func (p accountPool[A]) marshalMetaError(err error) error {
	return fmt.Errorf("marshal %s account meta: %w", p.p.name(), err)
}

func (p accountPool[A]) tokenError(err error) error {
	return fmt.Errorf("%s account token: %w", p.p.name(), err)
}

func (p accountPool[A]) adapterMissing() error {
	return fmt.Errorf("%s adapter is not registered", p.p.name())
}

func (p accountPool[A]) adapterUnsupported() error {
	return fmt.Errorf("%s adapter does not support account refresh", p.p.name())
}
