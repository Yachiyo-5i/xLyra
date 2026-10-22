package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/google/uuid"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

const (
	pluginAccountRefreshLeaseTTL  = 2 * time.Minute
	pluginAccountRefreshLeaseWait = 250 * time.Millisecond
	pluginAccountRefreshLead      = 300 * time.Second
	pluginLoginResultTTL          = 10 * time.Minute
	pluginPendingLoginMax         = 256
	pluginImportMaxFiles          = 50
	pluginImportMaxAccounts       = 200
	// pluginAccountWorkers matches the scheduler's default site-health worker
	// count. pluginAccountNetworkTimeout matches its per-site health timeout.
	// pluginSiteAccountRefreshBudget keeps one plugin site from using the
	// whole 10 minute batch refresh window.
	pluginAccountWorkers           = 4
	pluginAccountNetworkTimeout    = 10 * time.Second
	pluginSiteAccountRefreshBudget = 2 * time.Minute
	// pluginAccountPersistTimeout bounds a credential or lease write that
	// continues after the caller is canceled. It must stay well under
	// pluginAccountRefreshLeaseTTL so a hung write returns before another
	// refresh can take the lease and have this commit overwrite the newer token.
	pluginAccountPersistTimeout = 15 * time.Second
)

// PluginAccountRuntime is the process-local engine for one account-pool plugin.
// Login sessions live in the process that called StartLogin. A poll that lands
// on another instance, or a restart, loses the session. Replacing the runtime
// snapshot (catalog reload) drops in-flight sessions the same way.
//
// Credential maps and PluginLoginStart.Credential / PluginLoginPoll.Credential
// hold secrets. Do not log them or put them on an API response.
type PluginAccountRuntime interface {
	StartLogin(ctx context.Context, params map[string]string) (PluginLoginStart, error)
	PollLogin(ctx context.Context, sessionID string) (PluginLoginPoll, error)
	Refresh(ctx context.Context, fields map[string]string) (map[string]string, error)
	Summarize(ctx context.Context, baseURL string, fields map[string]string) (PluginAccountSummary, error)
	ListModels(ctx context.Context, fields map[string]string) ([]adapter.Model, error)
	ParseImport(raw []byte) ([]PluginImportedAccount, error)
}

// PluginLoginStart is one login attempt. Credential is set only when Status
// is complete, and only for the host. It is not an API field.
type PluginLoginStart struct {
	ID                      string
	Kind                    string
	Status                  string
	UserCode                string
	VerificationURL         string
	VerificationURLComplete string
	Interval                int
	ExpiresAt               time.Time
	Identity                string
	Credential              map[string]string
}

// PluginLoginPoll is one poll. Credential is set only when Status is complete.
type PluginLoginPoll struct {
	Status     string
	Interval   int
	Identity   string
	Credential map[string]string
}

// PluginAccountSummary is the host view of summarizeAccount.
// Entries, Remark, and Status are stored on the account state. They must not
// contain credential secrets.
type PluginAccountSummary struct {
	Status  string
	Remark  string
	Label   string
	Entries []QuotaProbeEntry
}

// PluginImportedAccount is one account from a declarative import file.
// Fields may contain secrets.
type PluginImportedAccount struct {
	Fields map[string]string
}

// PluginAccount is one plugin account as the admin API returns it.
// Fields are the manifest's non-secret credential fields. Tokens stay in the
// encrypted credential package.
type PluginAccount struct {
	CredentialID   uuid.UUID          `json:"credential_id"`
	MaskedToken    string             `json:"masked_token"`
	Enabled        bool               `json:"enabled"`
	Region         string             `json:"region,omitempty"`
	Identity       string             `json:"identity,omitempty"`
	Remark         string             `json:"remark,omitempty"`
	Status         string             `json:"status,omitempty"`
	ReauthRequired bool               `json:"reauth_required,omitempty"`
	SyncStatus     string             `json:"sync_status,omitempty"`
	SyncMessage    string             `json:"sync_message,omitempty"`
	Fields         map[string]string  `json:"fields,omitempty"`
	ModelCount     int                `json:"model_count"`
	Models         []GrokAccountModel `json:"models"`
	Entries        []QuotaProbeEntry  `json:"entries,omitempty"`
	RemainingQuota *int64             `json:"remaining_quota,omitempty"`
	UsedQuota      *int64             `json:"used_quota,omitempty"`
	QuotaLimit     *int64             `json:"quota_limit,omitempty"`
	QuotaUnit      string             `json:"quota_unit,omitempty"`
}

// PluginAccountImportItem is the outcome for one imported account.
type PluginAccountImportItem struct {
	Source       string `json:"source"`
	Label        string `json:"label,omitempty"`
	Region       string `json:"region,omitempty"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
	Warning      string `json:"warning,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
}

type pluginCredential struct {
	Fields   map[string]string
	Identity string
	Region   string
}

type pluginAccountRuntimeSnapshot struct {
	byType map[string]PluginAccountRuntime
}

type pluginAccountRuntimeSlot struct {
	current atomic.Pointer[pluginAccountRuntimeSnapshot]
}

type pluginCompletedLogin struct {
	account   PluginAccount
	expiresAt time.Time
}

type pluginPendingLogin struct {
	region    string
	expiresAt time.Time
}

type pluginLoginResults struct {
	mu      sync.Mutex
	items   map[string]pluginCompletedLogin
	pending map[string]pluginPendingLogin
}

func newPluginLoginResults() *pluginLoginResults {
	return &pluginLoginResults{
		items:   map[string]pluginCompletedLogin{},
		pending: map[string]pluginPendingLogin{},
	}
}

// SetPluginAccountRuntimes replaces the account-pool runtime snapshot.
// The next catalog reload passes the whole map again; nil clears it.
// Login sessions belong to the previous runtime and are dropped here.
func (s *Service) SetPluginAccountRuntimes(runtimes map[string]PluginAccountRuntime) error {
	if s == nil {
		return fmt.Errorf("site service is nil")
	}
	next := make(map[string]PluginAccountRuntime, len(runtimes))
	for key, runtime := range runtimes {
		id := strings.ToLower(strings.TrimSpace(key))
		if id == "" || runtime == nil {
			continue
		}
		next[id] = runtime
	}
	s.accountRuntimes.current.Store(&pluginAccountRuntimeSnapshot{byType: next})
	if s.pluginLogins != nil {
		s.pluginLogins.clear()
	}
	return nil
}

func (s *Service) pluginAccountRuntime(siteType string) (PluginAccountRuntime, bool) {
	if s == nil {
		return nil, false
	}
	snapshot := s.accountRuntimes.current.Load()
	if snapshot == nil {
		return nil, false
	}
	runtime, ok := snapshot.byType[strings.ToLower(strings.TrimSpace(siteType))]
	return runtime, ok && runtime != nil
}

func (s *Service) pluginModelsPerAccount(siteType string) bool {
	item, ok := LookupPluginSiteType(siteType)
	return ok && item.Enabled && item.Pool != nil && item.Pool.ModelsPerAccount
}

// syncPluginAccountModelsLocked lists models for each enabled account that does
// not need reauthorization. Accounts run one at a time under the site lock,
// the same way API-key model sync walks keys. Each listModels call keeps the
// stepped-hook limits: 6 calls, 200ms each, 20s per HTTP request. One account
// failing leaves the others. HTTP 401 marks that account for reauthorization.
func (s *Service) syncPluginAccountModelsLocked(ctx context.Context, site store.Site) (SyncResult, error) {
	provider, ok := s.pluginAccountProvider(site.SiteType)
	if !ok {
		return SyncResult{}, errors.New("plugin account pool is not configured")
	}
	api, ok := provider.(pluginAccountAPI)
	if !ok {
		return SyncResult{}, errors.New("plugin account pool is not configured")
	}
	return api.syncModels(ctx, site)
}

func (p pluginAccountAPI) syncModels(ctx context.Context, site store.Site) (SyncResult, error) {
	credentials, err := store.NewSiteCredentialRepository(p.s.db.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		return SyncResult{}, err
	}
	eligible := make([]store.SiteCredential, 0, len(credentials))
	for _, credential := range credentials {
		if !store.IsAccountCredentialTypeOf(credential.CredentialType, p.siteType) {
			continue
		}
		if !pluginAccountSyncEligible(accountMetaMap(credential.Meta)) {
			continue
		}
		eligible = append(eligible, credential)
	}
	if len(eligible) == 0 {
		return SyncResult{SiteID: site.ID}, nil
	}
	keyErrors := 0
	var lastErr error
	apiKeyModels := make([]store.SiteAPIKeyModel, 0)
	for _, credential := range eligible {
		fields, readErr := p.readCredentialFields(credential)
		if readErr != nil {
			if contextCanceled(ctx, readErr) {
				return SyncResult{}, readErr
			}
			keyErrors++
			lastErr = readErr
			p.s.recordAccountRefreshFailure(ctx, credential, readErr, func(error) bool { return false })
			continue
		}
		models, listErr := p.runtime.ListModels(ctx, cloneStringMap(fields))
		if listErr != nil {
			if contextCanceled(ctx, listErr) {
				return SyncResult{}, listErr
			}
			keyErrors++
			lastErr = listErr
			reauth := errors.Is(listErr, accountauth.ErrReauthorizationRequired)
			p.s.recordAccountRefreshFailure(ctx, credential, redactCredentialError(listErr, fields, p.secretNames()), func(error) bool { return reauth })
			continue
		}
		stored, writeErr := p.writeAccountModelsRows(ctx, credential, models)
		if writeErr != nil {
			return SyncResult{}, writeErr
		}
		apiKeyModels = append(apiKeyModels, stored...)
	}
	if keyErrors == len(eligible) {
		if lastErr != nil {
			return SyncResult{}, fmt.Errorf("all %d enabled accounts failed: %w", keyErrors, lastErr)
		}
		return SyncResult{}, fmt.Errorf("all %d enabled accounts failed", keyErrors)
	}
	siteModels, err := p.syncSiteModelUnion(ctx, site.ID)
	if err != nil {
		return SyncResult{}, err
	}
	if len(siteModels) == 0 {
		return SyncResult{}, fmt.Errorf("no usable models available for model sync")
	}
	return SyncResult{
		SiteID:        site.ID,
		ModelCount:    len(siteModels),
		Models:        siteModels,
		APIKeyModels:  apiKeyModels,
		KeyErrors:     keyErrors,
		KeyErrorTotal: len(eligible),
	}, nil
}

func pluginAccountSyncEligible(meta map[string]any) bool {
	if !boolFromMeta(meta, "enabled", true) {
		return false
	}
	return !boolFromMeta(meta, "reauth_required", false)
}

func (p pluginAccountAPI) writeAccountModelsRows(ctx context.Context, credential store.SiteCredential, models []adapter.Model) ([]store.SiteAPIKeyModel, error) {
	now := time.Now()
	var stored []store.SiteAPIKeyModel
	err := p.s.db.WithinTx(ctx, func(tx store.Tx) error {
		rows, err := upsertCredentialAPIKeyModels(ctx, store.NewSiteAPIKeyModelRepository(tx), credential.SiteID, credential.ID, models, now)
		if err != nil {
			return err
		}
		stored = rows
		return nil
	})
	return stored, err
}

func (s *Service) pluginAccountAPI(siteType string) (pluginAccountAPI, bool) {
	provider, ok := s.pluginAccountProvider(siteType)
	if !ok {
		return pluginAccountAPI{}, false
	}
	api, ok := provider.(pluginAccountAPI)
	return api, ok
}

func (s *Service) pluginAccountProvider(siteType string) (AccountProvider, bool) {
	item, ok := LookupPluginSiteType(siteType)
	if !ok || !item.Enabled || item.Pool == nil {
		return nil, false
	}
	runtime, ok := s.pluginAccountRuntime(item.Descriptor.SiteType)
	if !ok {
		return nil, false
	}
	return pluginAccountAPI{
		s:        s,
		siteType: item.Descriptor.SiteType,
		pool:     item.Pool,
		runtime:  runtime,
	}, true
}

func (r *pluginLoginResults) remember(session string, account PluginAccount) {
	session = strings.TrimSpace(session)
	if r == nil || session == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, item := range r.items {
		if now.After(item.expiresAt) {
			delete(r.items, id)
		}
	}
	r.items[session] = pluginCompletedLogin{account: account, expiresAt: now.Add(pluginLoginResultTTL)}
	delete(r.pending, session)
}

func (r *pluginLoginResults) rememberRegion(session, region string) error {
	session = strings.TrimSpace(session)
	region = strings.TrimSpace(region)
	if r == nil || session == "" || region == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, item := range r.pending {
		if now.After(item.expiresAt) {
			delete(r.pending, id)
		}
	}
	if _, exists := r.pending[session]; !exists && len(r.pending) >= pluginPendingLoginMax {
		return fmt.Errorf("too many pending logins; retry after an existing login expires")
	}
	r.pending[session] = pluginPendingLogin{region: region, expiresAt: now.Add(pluginLoginResultTTL)}
	return nil
}

// hasCapacity reports whether another pending region can be stored.
// Expired rows are dropped first. StartLogin checks this before asking the
// runtime for a device code; rememberRegion remains the final guard.
func (r *pluginLoginResults) hasCapacity() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, item := range r.pending {
		if now.After(item.expiresAt) {
			delete(r.pending, id)
		}
	}
	return len(r.pending) < pluginPendingLoginMax
}

func (r *pluginLoginResults) region(session string) string {
	session = strings.TrimSpace(session)
	if r == nil || session == "" {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.pending[session]
	if !ok || time.Now().After(item.expiresAt) {
		delete(r.pending, session)
		return ""
	}
	return item.region
}

func (r *pluginLoginResults) take(session string) (PluginAccount, bool) {
	session = strings.TrimSpace(session)
	if r == nil || session == "" {
		return PluginAccount{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[session]
	if !ok {
		return PluginAccount{}, false
	}
	delete(r.items, session)
	if time.Now().After(item.expiresAt) {
		return PluginAccount{}, false
	}
	return item.account, true
}

func (r *pluginLoginResults) clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = map[string]pluginCompletedLogin{}
	r.pending = map[string]pluginPendingLogin{}
}

type pluginAccountAPI struct {
	s        *Service
	siteType string
	pool     *jsplugin.AccountPoolSpec
	runtime  PluginAccountRuntime
}

func (p pluginAccountAPI) List(ctx context.Context, siteID uuid.UUID) (any, int, error) {
	accounts, err := p.poolAPI().list(ctx, siteID)
	if err != nil {
		return nil, 0, err
	}
	if accounts == nil {
		accounts = []PluginAccount{}
	}
	return accounts, len(accounts), nil
}

func (p pluginAccountAPI) Update(ctx context.Context, siteID, credentialID uuid.UUID, enabled *bool) (any, error) {
	account, err := p.poolAPI().update(ctx, siteID, credentialID, enabled)
	if err != nil {
		return nil, err
	}
	_ = p.s.upsertPluginAccountSiteState(ctx, siteID, p.siteType, "", nil, nil, time.Now(), true)
	return account, nil
}

func (p pluginAccountAPI) SetModelEnabled(ctx context.Context, siteID, credentialID uuid.UUID, model string, enabled bool) (any, error) {
	account, err := p.poolAPI().setModelEnabled(ctx, siteID, credentialID, model, enabled)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p pluginAccountAPI) SetModelEndpoint(ctx context.Context, siteID, credentialID, siteModelID uuid.UUID, mode string, endpointTypes []string) (any, error) {
	account, err := p.poolAPI().setModelEndpoint(ctx, siteID, credentialID, siteModelID, mode, endpointTypes)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p pluginAccountAPI) Delete(ctx context.Context, siteID, credentialID uuid.UUID) error {
	if err := p.poolAPI().delete(ctx, siteID, credentialID); err != nil {
		return err
	}
	_ = p.s.upsertPluginAccountSiteState(ctx, siteID, p.siteType, "", nil, nil, time.Now(), true)
	return nil
}

func (p pluginAccountAPI) Refresh(ctx context.Context, siteID, credentialID uuid.UUID) (any, error) {
	account, err := p.refreshAccount(ctx, siteID, credentialID)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p pluginAccountAPI) RefreshAll(ctx context.Context, siteID uuid.UUID) ([]AccountRefreshResult, error) {
	accounts, err := p.poolAPI().list(ctx, siteID)
	if err != nil {
		return nil, err
	}
	results := make([]AccountRefreshResult, len(accounts))
	done := make([]bool, len(accounts))
	workers := pluginAccountWorkers
	if workers > len(accounts) {
		workers = len(accounts)
	}
	if workers == 0 {
		return nil, nil
	}
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
				opCtx, cancel := context.WithTimeout(ctx, pluginAccountNetworkTimeout)
				refreshed, refreshErr := p.refreshAccount(opCtx, siteID, accounts[index].CredentialID)
				cancel()
				if refreshErr != nil && ctx.Err() != nil && (errors.Is(refreshErr, context.Canceled) || errors.Is(refreshErr, context.DeadlineExceeded)) {
					continue
				}
				item := AccountRefreshResult{CredentialID: accounts[index].CredentialID}
				if refreshErr != nil {
					item.Error = refreshErr.Error()
				} else {
					item.Account = refreshed
				}
				results[index] = item
				done[index] = true
			}
		}()
	}
	for index := range accounts {
		if ctx.Err() != nil {
			break
		}
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	finished := make([]AccountRefreshResult, 0, len(accounts))
	for index := range accounts {
		if done[index] {
			finished = append(finished, results[index])
		}
	}
	if ctx.Err() != nil && len(finished) < len(accounts) {
		return finished, fmt.Errorf("plugin account refresh timed out: %w", ctx.Err())
	}
	return finished, nil
}

func (p pluginAccountAPI) StartLogin(ctx context.Context, siteID uuid.UUID, params AccountLoginParams) (AccountLoginStart, error) {
	if err := p.requireSite(ctx, siteID); err != nil {
		return AccountLoginStart{}, err
	}
	region, err := p.resolveRegion(params.Region)
	if err != nil {
		return AccountLoginStart{}, err
	}
	loginParams := map[string]string{}
	if region != "" {
		loginParams["region"] = region
		if !p.s.pluginLogins.hasCapacity() {
			return AccountLoginStart{}, fmt.Errorf("too many pending logins; retry after an existing login expires")
		}
	}
	started, err := p.runtime.StartLogin(ctx, loginParams)
	if err != nil {
		return AccountLoginStart{}, err
	}
	if err := p.s.pluginLogins.rememberRegion(started.ID, region); err != nil {
		return AccountLoginStart{}, err
	}
	if started.Status == "complete" {
		account, finishErr := p.finishLogin(ctx, siteID, started.Identity, started.Credential, region, nil)
		if finishErr != nil {
			return AccountLoginStart{}, finishErr
		}
		p.s.pluginLogins.remember(started.ID, account)
	}
	return p.loginStart(started, region), nil
}

func (p pluginAccountAPI) PollLogin(ctx context.Context, siteID uuid.UUID, session string, targetCredentialID *uuid.UUID) (AccountLoginPoll, error) {
	if err := p.requireSite(ctx, siteID); err != nil {
		return AccountLoginPoll{}, err
	}
	session = strings.TrimSpace(session)
	if session == "" {
		return AccountLoginPoll{}, fmt.Errorf("session is required")
	}
	if account, ok := p.s.pluginLogins.take(session); ok {
		return AccountLoginPoll{Status: "complete", Account: account}, nil
	}
	poll, err := p.runtime.PollLogin(ctx, session)
	if err != nil {
		return AccountLoginPoll{}, err
	}
	switch poll.Status {
	case "pending", "slow_down":
		return AccountLoginPoll{Status: poll.Status, Interval: poll.Interval}, nil
	case "complete":
		region := p.s.pluginLogins.region(session)
		if region == "" {
			region, err = p.resolveRegion("")
			if err != nil {
				return AccountLoginPoll{}, err
			}
		}
		if fromFields := p.regionFromFields(poll.Credential); fromFields != "" {
			region = fromFields
		}
		account, finishErr := p.finishLogin(ctx, siteID, poll.Identity, poll.Credential, region, targetCredentialID)
		if finishErr != nil {
			return AccountLoginPoll{}, finishErr
		}
		return AccountLoginPoll{Status: "complete", Account: account}, nil
	default:
		return AccountLoginPoll{}, fmt.Errorf("login status %q is not supported", poll.Status)
	}
}

func (p pluginAccountAPI) Regions(ctx context.Context, siteID uuid.UUID) (any, string, error) {
	if err := p.requireSite(ctx, siteID); err != nil {
		return nil, "", err
	}
	items := make([]AccountRegion, 0, len(p.pool.Regions))
	for _, region := range p.pool.Regions {
		items = append(items, AccountRegion{
			ID:      region.ID,
			Label:   pluginRegionLabel(region.Label, "zh", "en"),
			LabelEN: pluginRegionLabel(region.Label, "en", "zh"),
		})
	}
	return items, p.pool.DefaultRegion, nil
}

func (p pluginAccountAPI) Import(ctx context.Context, siteID uuid.UUID, files []AccountImportFile) (any, error) {
	if p.pool == nil || !p.pool.Import.Enabled {
		return nil, ErrAccountImportUnsupported
	}
	if err := p.requireSite(ctx, siteID); err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no account files were provided")
	}
	if len(files) > pluginImportMaxFiles {
		return nil, fmt.Errorf("at most %d files can be imported at once", pluginImportMaxFiles)
	}
	items := make([]PluginAccountImportItem, 0, len(files))
	total := 0
	for index, file := range files {
		source := strings.TrimSpace(file.Name)
		if source == "" {
			source = fmt.Sprintf("file-%d", index+1)
		}
		imported, err := p.runtime.ParseImport([]byte(file.Content))
		if err != nil {
			items = append(items, PluginAccountImportItem{Source: source, Status: "failed", Error: redactText(err.Error(), map[string]string{"file": file.Content}, map[string]struct{}{"file": {}})})
			continue
		}
		for _, account := range imported {
			total++
			if total > pluginImportMaxAccounts {
				items = append(items, PluginAccountImportItem{Source: source, Status: "failed", Error: fmt.Sprintf("at most %d accounts can be imported at once", pluginImportMaxAccounts)})
				return items, nil
			}
			items = append(items, p.importOne(ctx, siteID, source, account))
		}
	}
	return items, nil
}

func (p pluginAccountAPI) importOne(ctx context.Context, siteID uuid.UUID, source string, imported PluginImportedAccount) PluginAccountImportItem {
	item := PluginAccountImportItem{Source: source}
	identity, err := p.identityOf(imported.Fields)
	if err != nil {
		item.Status = "failed"
		item.Error = redactCredentialError(err, imported.Fields, p.secretNames()).Error()
		return item
	}
	region := p.regionFromFields(imported.Fields)
	if region == "" {
		region = p.pool.DefaultRegion
	}
	account, err := p.login().complete(ctx, siteID, pluginCredential{
		Fields:   cloneStringMap(imported.Fields),
		Identity: identity,
		Region:   region,
	}, nil)
	if err != nil {
		item.Status = "failed"
		item.Error = redactCredentialError(err, imported.Fields, p.secretNames()).Error()
		return item
	}
	p.rememberAccountLabel(ctx, account.CredentialID, imported.Fields, "")
	item.Status = "imported"
	item.CredentialID = account.CredentialID.String()
	item.Label = account.MaskedToken
	item.Region = account.Region
	refreshed, refreshErr := p.refreshAccount(ctx, siteID, account.CredentialID)
	if refreshErr != nil {
		item.Warning = refreshErr.Error()
		return item
	}
	if refreshed.MaskedToken != "" {
		item.Label = refreshed.MaskedToken
	}
	item.Region = refreshed.Region
	return item
}

func (p pluginAccountAPI) finishLogin(ctx context.Context, siteID uuid.UUID, identity string, fields map[string]string, region string, target *uuid.UUID) (PluginAccount, error) {
	if strings.TrimSpace(identity) == "" {
		var err error
		identity, err = p.identityOf(fields)
		if err != nil {
			return PluginAccount{}, redactCredentialError(err, fields, p.secretNames())
		}
	}
	account, err := p.login().complete(ctx, siteID, pluginCredential{
		Fields:   cloneStringMap(fields),
		Identity: identity,
		Region:   region,
	}, target)
	if err != nil {
		return PluginAccount{}, redactCredentialError(err, fields, p.secretNames())
	}
	p.rememberAccountLabel(ctx, account.CredentialID, fields, "")
	refreshed, refreshErr := p.refreshAccount(ctx, siteID, account.CredentialID)
	if refreshErr != nil {
		return account, nil
	}
	return refreshed, nil
}

func (p pluginAccountAPI) loginStart(started PluginLoginStart, region string) AccountLoginStart {
	kind := started.Kind
	if kind == "" && p.pool != nil {
		kind = p.pool.Login.Kind
	}
	expires := started.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().Add(pluginLoginResultTTL)
	}
	out := AccountLoginStart{
		Kind:      kind,
		Session:   started.ID,
		Interval:  pluginLoginInterval(kind, started.Interval),
		ExpiresAt: expires,
		Region:    region,
	}
	if kind == accountLoginCustomPoll {
		out.AuthURL = started.VerificationURL
		out.UserCode = started.UserCode
		return out
	}
	out.UserCode = started.UserCode
	out.VerificationURI = started.VerificationURL
	out.VerificationURIComplete = started.VerificationURLComplete
	return out
}

func (p pluginAccountAPI) refreshAccount(ctx context.Context, siteID, credentialID uuid.UUID) (PluginAccount, error) {
	return p.refreshOne(ctx, siteID, credentialID, true)
}

type pluginRefreshPrepared struct {
	fields     map[string]string
	summary    PluginAccountSummary
	models     []adapter.Model
	syncModels bool
}

func (p pluginAccountAPI) refreshOne(ctx context.Context, siteID, credentialID uuid.UUID, publishSite bool) (PluginAccount, error) {
	var account PluginAccount
	if err := p.requireSite(ctx, siteID); err != nil {
		return account, err
	}
	repo := store.NewSiteCredentialRepository(p.s.db.DB())
	credential, err := repo.GetByID(ctx, credentialID)
	if err != nil {
		return account, err
	}
	if credential.SiteID != siteID || !store.IsAccountCredentialTypeOf(credential.CredentialType, p.siteType) {
		return account, fmt.Errorf("%s account was not found", p.siteType)
	}
	var prepared pluginRefreshPrepared
	err = p.s.withPluginRefreshLease(ctx, credential.ID, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := repo.GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		if current.SiteID != siteID || !store.IsAccountCredentialTypeOf(current.CredentialType, p.siteType) {
			return fmt.Errorf("%s account was not found", p.siteType)
		}
		fields, err := p.readCredentialFields(current)
		if err != nil {
			return err
		}
		rotated := false
		if p.refreshesTokens() {
			if err := ctx.Err(); err != nil {
				return err
			}
			refreshed, refreshErr := p.runtime.Refresh(ctx, cloneStringMap(fields))
			if refreshErr != nil {
				return p.failRefresh(ctx, current, refreshErr, fields, false)
			}
			if strings.TrimSpace(refreshed[p.accessField()]) == "" {
				return fmt.Errorf("refreshed account is missing %s", p.accessField())
			}
			fields = refreshed
			rotated = true
		}
		base := p.baseURL(anyString(accountMetaMap(current.Meta)["region"]))
		summary, sumErr := p.runtime.Summarize(ctx, base, cloneStringMap(fields))
		if sumErr != nil {
			return p.failRefresh(ctx, current, sumErr, fields, rotated)
		}
		summary = p.scrubSummary(summary, fields)
		if p.pool != nil && p.pool.ModelsPerAccount {
			models, listErr := p.runtime.ListModels(ctx, cloneStringMap(fields))
			if listErr != nil {
				return p.failRefresh(ctx, current, listErr, fields, rotated)
			}
			prepared.models = models
			prepared.syncModels = true
		}
		if rotated {
			if _, err := p.saveCredentialFields(ctx, current, fields, false); err != nil {
				return err
			}
		}
		prepared.fields = fields
		prepared.summary = summary
		return nil
	})
	if err != nil {
		return PluginAccount{}, err
	}
	if pluginRefreshLeaseReleased != nil {
		pluginRefreshLeaseReleased(siteID)
	}
	if err := ctx.Err(); err != nil {
		return PluginAccount{}, err
	}
	unlock := p.s.siteLocks.lock(siteID)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return PluginAccount{}, err
	}
	if err := p.requireSite(ctx, siteID); err != nil {
		return PluginAccount{}, err
	}
	current, err := repo.GetByID(ctx, credentialID)
	if err != nil {
		return PluginAccount{}, err
	}
	if current.SiteID != siteID || !store.IsAccountCredentialTypeOf(current.CredentialType, p.siteType) {
		return PluginAccount{}, fmt.Errorf("%s account was not found", p.siteType)
	}
	if prepared.syncModels {
		if err := p.writeAccountModels(ctx, current, prepared.models); err != nil {
			return PluginAccount{}, err
		}
		if _, err := p.syncSiteModelUnion(ctx, siteID); err != nil {
			return PluginAccount{}, err
		}
	}
	current, err = p.storeAccountLabel(ctx, current, prepared.fields, prepared.summary.Label)
	if err != nil {
		return PluginAccount{}, err
	}
	if err := p.persistSummary(ctx, current, prepared.summary); err != nil {
		return PluginAccount{}, err
	}
	if publishSite {
		p.syncQuota(ctx, siteID, store.Site{})
		_ = p.s.upsertPluginAccountSiteState(ctx, siteID, p.siteType, "synced", nil, nil, time.Now(), false)
	}
	updated, err := repo.GetByID(ctx, credentialID)
	if err != nil {
		updated = current
	}
	return p.snapshot(ctx, updated), nil
}

// probeAccountHealth ensures a token when it is due, then calls summarizeAccount.
// It does not write usage or the site quota. A token the upstream already
// issued is stored even when the context is cancelled afterwards. Cancellation
// does not mark the account failed. Other probe failures still update sync status.
func (p pluginAccountAPI) probeAccountHealth(ctx context.Context, credential store.SiteCredential) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	repo := store.NewSiteCredentialRepository(p.s.db.DB())
	var status, message string
	err := p.s.withPluginRefreshLease(ctx, credential.ID, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := repo.GetByID(ctx, credential.ID)
		if err != nil {
			return err
		}
		fields, err := p.readCredentialFields(current)
		if err != nil {
			if contextCanceled(ctx, err) {
				return err
			}
			status, message = "stale", err.Error()
			return p.failRefresh(ctx, current, err, fields, false)
		}
		rotated := false
		if p.refreshesTokens() && p.pool != nil && PluginAccountNeedsRefresh(*p.pool, fields, time.Now()) {
			if err := ctx.Err(); err != nil {
				return err
			}
			refreshed, refreshErr := p.runtime.Refresh(ctx, cloneStringMap(fields))
			if refreshErr != nil {
				if accountFailureUnproven(ctx, refreshErr) {
					return refreshErr
				}
				status = accountRefreshFailureStatus(refreshErr, func(error) bool {
					return errors.Is(refreshErr, accountauth.ErrReauthorizationRequired)
				})
				message = redactCredentialError(refreshErr, fields, p.secretNames()).Error()
				return p.failRefresh(ctx, current, refreshErr, fields, false)
			}
			if strings.TrimSpace(refreshed[p.accessField()]) == "" {
				cause := fmt.Errorf("refreshed account is missing %s", p.accessField())
				status, message = "stale", cause.Error()
				return p.failRefresh(ctx, current, cause, fields, false)
			}
			current, err = p.saveCredentialFields(ctx, current, refreshed, true)
			if err != nil {
				return err
			}
			fields = refreshed
			rotated = true
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		base := p.baseURL(anyString(accountMetaMap(current.Meta)["region"]))
		summary, sumErr := p.runtime.Summarize(ctx, base, cloneStringMap(fields))
		if sumErr != nil {
			if accountFailureUnproven(ctx, sumErr) {
				return sumErr
			}
			status = accountRefreshFailureStatus(sumErr, func(error) bool {
				return errors.Is(sumErr, accountauth.ErrReauthorizationRequired)
			})
			message = redactCredentialError(sumErr, fields, p.secretNames()).Error()
			return p.failRefresh(ctx, current, sumErr, fields, false)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		summary = p.scrubSummary(summary, fields)
		switch summary.Status {
		case "reauth":
			remark := summary.Remark
			if remark == "" {
				remark = "reauthorization required"
			}
			status, message = "reauth_required", remark
			p.noteRefreshFailure(ctx, current, errors.New(remark), true)
		case "unavailable":
			remark := summary.Remark
			if remark == "" {
				remark = "account is unavailable"
			}
			status, message = "stale", remark
			p.noteRefreshFailure(ctx, current, errors.New(remark), false)
		default:
			status = "ok"
			if rotated && boolFromMeta(accountMetaMap(current.Meta), "reauth_required", false) {
				if _, err := p.saveCredentialFields(ctx, current, fields, false); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		if accountFailureUnproven(ctx, err) {
			return "", "", err
		}
		if status == "" {
			status = "stale"
		}
		if message == "" {
			message = err.Error()
		}
		return status, message, nil
	}
	return status, message, nil
}

func (p pluginAccountAPI) refreshesTokens() bool {
	if p.pool == nil {
		return false
	}
	switch p.pool.Refresh.Kind {
	case "", "none":
		return false
	default:
		return true
	}
}

func (p pluginAccountAPI) failRefresh(ctx context.Context, credential store.SiteCredential, cause error, fields map[string]string, rotated bool) error {
	scrubbed := redactCredentialError(cause, fields, p.secretNames())
	if rotated {
		saved, err := p.saveCredentialFields(ctx, credential, fields, true)
		if err != nil {
			if !errors.Is(err, errPluginRefreshLeaseLost) {
				p.noteRefreshFailure(ctx, credential, scrubbed, errors.Is(cause, accountauth.ErrReauthorizationRequired))
			}
			return joinRefreshFailure(err, cause, scrubbed)
		}
		credential = saved
	}
	if accountFailureUnproven(ctx, cause) {
		return scrubbed
	}
	p.noteRefreshFailure(ctx, credential, scrubbed, errors.Is(cause, accountauth.ErrReauthorizationRequired))
	return scrubbed
}

// accountFailureUnproven reports that this attempt must not be stored as an
// account failure. A parent that is already canceled hides ordinary upstream
// errors, because a plugin may return those only because its context ended.
// ErrReauthorizationRequired is a definite upstream result and is recorded
// anyway. contextCanceled stays as-is for callers outside this refresh path.
func accountFailureUnproven(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, accountauth.ErrReauthorizationRequired) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return ctx != nil && ctx.Err() != nil
}

func (p pluginAccountAPI) noteRefreshFailure(ctx context.Context, credential store.SiteCredential, reported error, reauth bool) {
	recordCtx, cancel := persistContext(ctx)
	defer cancel()
	p.s.recordAccountRefreshFailure(recordCtx, credential, reported, func(error) bool { return reauth })
}

func joinRefreshFailure(saveErr, cause, scrubbed error) error {
	reported := cause
	if scrubbed != nil && cause != nil && scrubbed.Error() != cause.Error() {
		if errors.Is(cause, accountauth.ErrReauthorizationRequired) {
			reported = fmt.Errorf("%s: %w", scrubbed.Error(), accountauth.ErrReauthorizationRequired)
		} else {
			reported = scrubbed
		}
	}
	return fmt.Errorf("%w: %w", saveErr, reported)
}

// contextCanceled reports that this attempt must not be recorded as an account
// failure. A cancelled parent counts even when the upstream error is not
// wrapped as context.Canceled, so a timed-out probe cannot disable an account.
func contextCanceled(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// RefreshPluginAccountForGateway renews one stored account for a gateway
// request. The same credential's refreshes share the site refresh lease.
// Inside the lease the stored bag is read again, so a refresh that already
// landed is not repeated. A successful refresh is written back. A dead grant
// is marked for reauthorization and still matches
// accountauth.ErrReauthorizationRequired. fields is the bag the gateway
// already decrypted; the stored bag wins.
func (s *Service) RefreshPluginAccountForGateway(ctx context.Context, siteID, credentialID uuid.UUID, siteType string, fields map[string]string) (map[string]string, error) {
	provider, ok := s.pluginAccountProvider(siteType)
	if !ok {
		return nil, errors.New("plugin account pool is not configured")
	}
	api, ok := provider.(pluginAccountAPI)
	if !ok {
		return nil, errors.New("plugin account pool is not configured")
	}
	return api.refreshForGateway(ctx, siteID, credentialID, fields)
}

func (p pluginAccountAPI) refreshForGateway(ctx context.Context, siteID, credentialID uuid.UUID, _ map[string]string) (map[string]string, error) {
	if err := p.requireSite(ctx, siteID); err != nil {
		return nil, err
	}
	unlock := p.s.siteLocks.lock(siteID)
	defer unlock()
	if pluginGatewayHoldsSiteLock != nil {
		pluginGatewayHoldsSiteLock(siteID)
	}
	repo := store.NewSiteCredentialRepository(p.s.db.DB())
	credential, err := repo.GetByID(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	if credential.SiteID != siteID || !store.IsAccountCredentialTypeOf(credential.CredentialType, p.siteType) {
		return nil, fmt.Errorf("%s account was not found", p.siteType)
	}
	var fields map[string]string
	err = p.s.withPluginRefreshLease(ctx, credential.ID, func(ctx context.Context) error {
		current, err := repo.GetByID(ctx, credentialID)
		if err != nil {
			return err
		}
		stored, err := p.readCredentialFields(current)
		if err != nil {
			return err
		}
		if p.pool == nil || !PluginAccountNeedsRefresh(*p.pool, stored, time.Now()) {
			fields = stored
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		refreshed, refreshErr := p.runtime.Refresh(ctx, cloneStringMap(stored))
		if refreshErr != nil {
			return p.failGatewayRefresh(ctx, current, refreshErr, stored)
		}
		if strings.TrimSpace(refreshed[p.accessField()]) == "" {
			return fmt.Errorf("refreshed account is missing %s", p.accessField())
		}
		if _, err := p.saveCredentialFields(ctx, current, refreshed, true); err != nil {
			return err
		}
		fields = refreshed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return fields, nil
}

func (p pluginAccountAPI) failGatewayRefresh(ctx context.Context, credential store.SiteCredential, cause error, fields map[string]string) error {
	reauth := errors.Is(cause, accountauth.ErrReauthorizationRequired)
	scrubbed := redactCredentialError(cause, fields, p.secretNames())
	if accountFailureUnproven(ctx, cause) {
		return scrubbed
	}
	p.noteRefreshFailure(ctx, credential, scrubbed, reauth)
	if !reauth {
		return scrubbed
	}
	return fmt.Errorf("%s: %w", scrubbed.Error(), accountauth.ErrReauthorizationRequired)
}

func (p pluginAccountAPI) readCredentialFields(credential store.SiteCredential) (map[string]string, error) {
	plain, err := p.s.credentials.Decrypt(credential.EncryptedSecret)
	if err != nil {
		return nil, errors.New("account credential package cannot be read")
	}
	return decodePluginCredential(plain)
}

func (p pluginAccountAPI) saveCredentialFields(ctx context.Context, credential store.SiteCredential, fields map[string]string, keepState bool) (store.SiteCredential, error) {
	encoded, err := encodePluginCredential(fields)
	if err != nil {
		return store.SiteCredential{}, err
	}
	encrypted, _, err := p.s.credentials.Encrypt(encoded)
	if err != nil {
		return store.SiteCredential{}, err
	}
	writeCtx, cancel := persistContext(ctx)
	defer cancel()
	meta := accountMetaMap(credential.Meta)
	if !keepState && boolFromMeta(meta, "reauth_required", false) {
		delete(meta, "reauth_required")
		meta["enabled"] = true
	}
	p.applyPublicMeta(meta, fields)
	if identity, idErr := p.identityOf(fields); idErr == nil {
		meta["identity"] = identity
	}
	prefix := store.CredentialPrefixAccount + strings.ToLower(strings.TrimSpace(p.siteType)) + ":"
	var updated store.SiteCredential
	err = p.s.db.WithinTx(writeCtx, func(tx store.Tx) error {
		if pluginCredentialWriteHook != nil {
			if err := pluginCredentialWriteHook(writeCtx); err != nil {
				return err
			}
		}
		if err := abandonIfRefreshLeaseLost(writeCtx, tx, credential.ID); err != nil {
			return err
		}
		repo := store.NewSiteCredentialRepository(tx)
		saved, err := repo.UpdateSecretIfTypePrefix(writeCtx, credential.ID, encrypted, prefix)
		if err != nil {
			return err
		}
		saved, err = updateCredentialMeta(writeCtx, repo, saved.ID, meta)
		if err != nil {
			return err
		}
		updated = saved
		return nil
	})
	if err != nil {
		return store.SiteCredential{}, err
	}
	return updated, nil
}

func (p pluginAccountAPI) applyPublicMeta(meta map[string]any, fields map[string]string) {
	for _, field := range p.pool.Credential.Fields {
		if field.Secret {
			delete(meta, field.Name)
			continue
		}
		value := strings.TrimSpace(fields[field.Name])
		if value == "" {
			continue
		}
		meta[field.Name] = value
	}
}

func (p pluginAccountAPI) writeAccountModels(ctx context.Context, credential store.SiteCredential, models []adapter.Model) error {
	_, err := p.writeAccountModelsRows(ctx, credential, models)
	return err
}

func (p pluginAccountAPI) syncSiteModelUnion(ctx context.Context, siteID uuid.UUID) ([]store.SiteModel, error) {
	item, err := p.s.Get(ctx, siteID)
	if err != nil {
		return nil, err
	}
	return p.s.syncSiteModelsFromAPIKeyState(ctx, siteID, item.SiteType, item.BaseURL)
}

func (p pluginAccountAPI) persistSummary(ctx context.Context, credential store.SiteCredential, summary PluginAccountSummary) error {
	meta := accountMetaMap(credential.Meta)
	enabled := boolFromMeta(meta, "enabled", true)
	metaDirty := false
	if p.pool != nil && p.pool.StoresSummaryLabel() && summary.Label != "" {
		meta["display"] = summary.Label
		metaDirty = true
	}
	if summary.Status == "reauth" {
		enabled = false
		meta["enabled"] = false
		meta["reauth_required"] = true
		metaDirty = true
	}
	if metaDirty {
		if _, err := store.NewSiteCredentialRepository(p.s.db.DB()).UpdateMeta(ctx, credential.ID, store.JSON(jsonBytes(meta))); err != nil {
			return err
		}
	}
	payload := map[string]any{
		"status":  summary.Status,
		"remark":  summary.Remark,
		"entries": summary.Entries,
	}
	remain, used, limit, unit := pluginQuotaTotals(summary.Entries)
	if limit != nil {
		payload["limit"] = limit
	}
	if unit != "" {
		payload["unit"] = unit
	}
	usage, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal account summary: %w", err)
	}
	syncStatus := "synced"
	switch summary.Status {
	case "unavailable":
		syncStatus = "stale"
	case "reauth":
		syncStatus = "reauth_required"
	}
	var message any
	if summary.Remark != "" {
		message = summary.Remark
	}
	name := strings.TrimSpace(credential.DisplayName)
	if name == "" {
		name = credential.MaskedSecret
	}
	repo := store.NewSiteAPIKeyStateRepository(p.s.db.DB())
	if _, err := repo.Upsert(ctx, store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: credential.ID,
		SiteID:           credential.SiteID,
		Name:             name,
		Enabled:          enabled,
		RemainQuota:      remain,
		UsedQuota:        used,
		Usage:            usage,
		Raw:              usage,
		SyncStatus:       syncStatus,
		SyncMessage:      message,
		LastSyncedAt:     time.Now(),
	}); err != nil {
		return err
	}
	_, err = repo.UpdateEnabled(ctx, credential.ID, enabled)
	return err
}

func (p pluginAccountAPI) snapshot(ctx context.Context, credential store.SiteCredential) PluginAccount {
	return p.poolAPI().snapshot(ctx, credential)
}

func (p pluginAccountAPI) poolAPI() accountPool[PluginAccount] {
	return accountPool[PluginAccount]{s: p.s, p: pluginAccountProvider{api: p}}
}

func (p pluginAccountAPI) login() accountLogin[pluginCredential, PluginAccount] {
	return accountLogin[pluginCredential, PluginAccount]{s: p.s, p: pluginLoginProvider{api: p}}
}

func (p pluginAccountAPI) requireSite(ctx context.Context, siteID uuid.UUID) error {
	if siteID == uuid.Nil {
		return fmt.Errorf("site id is required")
	}
	item, err := p.s.Get(ctx, siteID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(item.SiteType), p.siteType) {
		return fmt.Errorf("site type %q is not %s", item.SiteType, p.siteType)
	}
	return nil
}

func (p pluginAccountAPI) resolveRegion(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if p.pool == nil {
			return "", nil
		}
		return p.pool.DefaultRegion, nil
	}
	for _, region := range p.pool.Regions {
		if region.ID == raw {
			return region.ID, nil
		}
	}
	return "", fmt.Errorf("region %q is not configured", raw)
}

func (p pluginAccountAPI) regionFromFields(fields map[string]string) string {
	if p.pool == nil {
		return ""
	}
	for _, region := range p.pool.Regions {
		if fields["region"] == region.ID || fields["realm"] == region.ID {
			return region.ID
		}
		for _, realm := range region.Realms {
			if fields["realm"] == realm {
				return region.ID
			}
		}
		for _, domain := range region.Domains {
			if fields["domain"] == domain {
				return region.ID
			}
		}
	}
	return ""
}

func (p pluginAccountAPI) baseURL(region string) string {
	if p.pool == nil {
		return ""
	}
	for _, item := range p.pool.Regions {
		if item.ID == region {
			return strings.TrimRight(strings.TrimSpace(item.BaseURL), "/")
		}
	}
	return p.pool.DefaultBaseURL()
}

func (p pluginAccountAPI) accessField() string {
	if p.pool != nil && p.pool.Credential.AccessTokenField != "" {
		return p.pool.Credential.AccessTokenField
	}
	return "access_token"
}

func (p pluginAccountAPI) secretNames() map[string]struct{} {
	names := map[string]struct{}{}
	if p.pool == nil {
		return names
	}
	for _, field := range p.pool.Credential.Fields {
		if field.Secret {
			names[field.Name] = struct{}{}
		}
	}
	return names
}

func (p pluginAccountAPI) identityOf(fields map[string]string) (string, error) {
	if p.pool == nil {
		return "", errors.New("account identity is not configured")
	}
	return p.pool.ResolveIdentity(fields)
}

func (p pluginAccountAPI) scrubSummary(summary PluginAccountSummary, fields map[string]string) PluginAccountSummary {
	secrets := p.secretNames()
	summary.Status = redactText(summary.Status, fields, secrets)
	summary.Remark = redactText(summary.Remark, fields, secrets)
	summary.Label = cleanPluginDisplayLabel(redactText(summary.Label, fields, secrets), fields, secrets)
	for i := range summary.Entries {
		entry := summary.Entries[i]
		entry.Label = redactText(entry.Label, fields, secrets)
		entry.Kind = redactText(entry.Kind, fields, secrets)
		entry.Name = redactText(entry.Name, fields, secrets)
		entry.Unit = redactText(entry.Unit, fields, secrets)
		if entry.ResetAt != nil {
			raw := redactText(*entry.ResetAt, fields, secrets)
			entry.ResetAt = &raw
		}
		if entry.ExpiresAt != nil {
			raw := redactText(*entry.ExpiresAt, fields, secrets)
			entry.ExpiresAt = &raw
		}
		summary.Entries[i] = entry
	}
	return summary
}

func (p pluginAccountAPI) syncQuota(ctx context.Context, siteID uuid.UUID, current store.Site) store.Site {
	mode := "sum"
	if p.pool != nil && p.pool.QuotaAggregate != "" {
		mode = p.pool.QuotaAggregate
	}
	if mode == "none" {
		return current
	}
	siteRepo := store.NewSiteRepository(p.s.db.DB())
	item := current
	if item.ID == uuid.Nil {
		loaded, err := siteRepo.GetByID(ctx, siteID)
		if err != nil {
			return current
		}
		item = loaded
	}
	credentials, err := store.NewSiteCredentialRepository(p.s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return item
	}
	states, err := store.NewSiteAPIKeyStateRepository(p.s.db.DB()).ListBySite(ctx, siteID)
	if err != nil {
		return item
	}
	statesByCredential := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredential[state.SiteCredentialID] = state
	}
	var entries []QuotaProbeEntry
	accounts := 0
	for _, credential := range credentials {
		if !store.IsAccountCredentialTypeOf(credential.CredentialType, p.siteType) {
			continue
		}
		accounts++
		state := statesByCredential[credential.ID]
		if !state.Enabled {
			continue
		}
		parsed := pluginUsageView(state.Usage)
		entries = append(entries, parsed.Entries...)
	}
	meta := siteMetaMap(item)
	summary, ok := AggregateQuota(mode, entries)
	if accounts == 0 || !ok {
		existing, _ := meta[siteMetaQuotaProbeSummary].(map[string]any)
		if anyString(existing["probe_type"]) != p.siteType {
			return item
		}
		delete(meta, siteMetaQuotaProbeSummary)
	} else {
		payload := map[string]any{
			"probe_type":       p.siteType,
			"status":           "ok",
			"aggregate":        mode,
			"credential_count": accounts,
			"fetched_at":       time.Now().UTC().Format(time.RFC3339),
		}
		if summary.Unit != "" {
			payload["unit"] = summary.Unit
		}
		if summary.Remaining != nil {
			payload["remaining_min"] = *summary.Remaining
		}
		if summary.Limit != nil {
			payload["limit"] = *summary.Limit
		}
		if summary.Used != nil {
			payload["used"] = *summary.Used
		}
		meta[siteMetaQuotaProbeSummary] = payload
	}
	updated, err := siteRepo.UpdateMeta(ctx, item.ID, store.JSON(jsonBytes(meta)))
	if err != nil {
		return item
	}
	return updated
}

type pluginAccountProvider struct {
	api pluginAccountAPI
}

func (p pluginAccountProvider) name() string { return p.api.siteType }

func (p pluginAccountProvider) isCredential(credentialType string) bool {
	return store.IsAccountCredentialTypeOf(credentialType, p.api.siteType)
}

func (p pluginAccountProvider) requireSite(ctx context.Context, siteID uuid.UUID) error {
	return p.api.requireSite(ctx, siteID)
}

func (pluginAccountProvider) ensureAccess(context.Context, store.Site, store.SiteCredential) (string, adapter.SiteConfig, error) {
	return "", adapter.SiteConfig{}, errors.New("plugin account refresh does not use the adapter")
}

func (pluginAccountProvider) shouldReauth(err error) bool {
	return errors.Is(err, accountauth.ErrReauthorizationRequired)
}

func (p pluginAccountProvider) fromStore(credential store.SiteCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel) (PluginAccount, bool) {
	return p.api.accountFromStore(credential, state, models)
}

func (p pluginAccountProvider) decorate(ctx context.Context, account PluginAccount) PluginAccount {
	account.Models = p.api.s.resolveAccountModelEndpoints(ctx, account.Models)
	if account.Models == nil {
		account.Models = []GrokAccountModel{}
	}
	return account
}

func (pluginAccountProvider) credentialID(account PluginAccount) uuid.UUID {
	return account.CredentialID
}

func (p pluginAccountProvider) syncQuota(ctx context.Context, siteID uuid.UUID, current store.Site) store.Site {
	return p.api.syncQuota(ctx, siteID, current)
}

func (pluginAccountProvider) resyncQuotaOnUpdateDelete() bool { return true }

func (pluginAccountProvider) syncQuotaBeforeRefreshReread() bool { return false }

func (pluginAccountProvider) onRefreshRereadMiss(credential store.SiteCredential, _ adapter.APIKeySummary) store.SiteCredential {
	return credential
}

func (p pluginAccountProvider) afterRefresh(ctx context.Context, siteID uuid.UUID, siteModels []store.SiteModel, now time.Time) error {
	return p.api.s.upsertPluginAccountSiteState(ctx, siteID, p.api.siteType, "synced", nil, siteModels, now, false)
}

type pluginLoginProvider struct {
	api pluginAccountAPI
}

func (p pluginLoginProvider) name() string { return p.api.siteType }

func (p pluginLoginProvider) requireSite(ctx context.Context, siteID uuid.UUID) error {
	return p.api.requireSite(ctx, siteID)
}

func (p pluginLoginProvider) isCredential(credentialType string) bool {
	return store.IsAccountCredentialTypeOf(credentialType, p.api.siteType)
}

func (p pluginLoginProvider) credentialPrefix() string {
	return store.CredentialPrefixAccount + p.api.siteType + ":"
}

func (p pluginLoginProvider) normalize(tokens pluginCredential) (pluginCredential, error) {
	tokens.Fields = cloneStringMap(tokens.Fields)
	if strings.TrimSpace(tokens.Fields[p.api.accessField()]) == "" {
		return tokens, fmt.Errorf("account is missing %s", p.api.accessField())
	}
	if strings.TrimSpace(tokens.Identity) == "" {
		identity, err := p.api.identityOf(tokens.Fields)
		if err != nil {
			return tokens, err
		}
		tokens.Identity = identity
	}
	if strings.TrimSpace(tokens.Region) == "" && p.api.pool != nil {
		tokens.Region = p.api.pool.DefaultRegion
	}
	return tokens, nil
}

func (pluginLoginProvider) encodeSecret(tokens pluginCredential) (string, error) {
	return encodePluginCredential(tokens.Fields)
}

func (pluginLoginProvider) identity(tokens pluginCredential) string {
	return strings.TrimSpace(tokens.Identity)
}

func (pluginLoginProvider) metaIdentity(meta map[string]any) string {
	return strings.TrimSpace(anyString(meta["identity"]))
}

func (p pluginLoginProvider) secretIdentity(secret string) string {
	fields, err := decodePluginCredential(secret)
	if err != nil {
		return ""
	}
	identity, err := p.api.identityOf(fields)
	if err != nil {
		return ""
	}
	return identity
}

func (p pluginLoginProvider) applyMeta(meta map[string]any, tokens pluginCredential) {
	if value := strings.TrimSpace(tokens.Identity); value != "" {
		meta["identity"] = value
	}
	if value := strings.TrimSpace(tokens.Region); value != "" {
		meta["region"] = value
	}
	p.api.applyPublicMeta(meta, tokens.Fields)
}

func (p pluginLoginProvider) maskedLabel(tokens pluginCredential) string {
	return p.api.visibleLabel(tokens.Fields, "")
}

func (p pluginLoginProvider) snapshot(ctx context.Context, credential store.SiteCredential) PluginAccount {
	return p.api.snapshot(ctx, credential)
}

func (p pluginAccountAPI) accountFromStore(credential store.SiteCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel) (PluginAccount, bool) {
	if !store.IsAccountCredentialTypeOf(credential.CredentialType, p.siteType) {
		return PluginAccount{}, false
	}
	meta := accountMetaMap(credential.Meta)
	enabled := boolFromMeta(meta, "enabled", true)
	if state.SiteCredentialID != uuid.Nil && state.SiteCredentialID == credential.ID {
		enabled = state.Enabled
	}
	accountModels := make([]GrokAccountModel, 0, len(models))
	modelCount := 0
	for _, model := range models {
		if !model.Available {
			continue
		}
		modelCount++
		display := strings.TrimSpace(model.DisplayName)
		if display == "" {
			display = model.UpstreamModelName
		}
		accountModels = append(accountModels, newAccountModel(model, display))
	}
	usage := pluginUsageView(state.Usage)
	fields := map[string]string{}
	if p.pool != nil {
		for _, field := range p.pool.Credential.Fields {
			if field.Secret {
				continue
			}
			value := strings.TrimSpace(anyString(meta[field.Name]))
			if value == "" {
				continue
			}
			fields[field.Name] = value
		}
	}
	label := ""
	if p.pool != nil {
		label = p.pool.ResolveDisplay(fields, anyString(meta["display"]))
	}
	if label == "" {
		label = strings.TrimSpace(anyString(meta["identity"]))
	}
	if label == "" {
		label = credential.MaskedSecret
	}
	account := PluginAccount{
		CredentialID:   credential.ID,
		MaskedToken:    label,
		Enabled:        enabled,
		Region:         anyString(meta["region"]),
		Identity:       anyString(meta["identity"]),
		Remark:         usage.Remark,
		Status:         usage.Status,
		ReauthRequired: boolFromMeta(meta, "reauth_required", false),
		SyncStatus:     state.SyncStatus,
		Fields:         fields,
		ModelCount:     modelCount,
		Models:         accountModels,
		Entries:        usage.Entries,
		QuotaUnit:      usage.Unit,
	}
	if state.SyncMessage.Valid && state.SyncMessage.String != usage.Remark {
		account.SyncMessage = state.SyncMessage.String
	}
	if state.RemainQuota.Valid {
		value := state.RemainQuota.Int64
		account.RemainingQuota = &value
	}
	if state.UsedQuota.Valid {
		value := state.UsedQuota.Int64
		account.UsedQuota = &value
	}
	if usage.Limit != nil {
		limit := int64(math.Round(*usage.Limit))
		account.QuotaLimit = &limit
	}
	if account.Models == nil {
		account.Models = []GrokAccountModel{}
	}
	if len(account.Fields) == 0 {
		account.Fields = nil
	}
	return account, true
}

type pluginUsage struct {
	Status  string
	Remark  string
	Entries []QuotaProbeEntry
	Limit   *float64
	Unit    string
}

func pluginUsageView(raw []byte) pluginUsage {
	if len(raw) == 0 {
		return pluginUsage{}
	}
	var payload struct {
		Status  string            `json:"status"`
		Remark  string            `json:"remark"`
		Entries []QuotaProbeEntry `json:"entries"`
		Limit   *float64          `json:"limit"`
		Unit    string            `json:"unit"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return pluginUsage{}
	}
	return pluginUsage{
		Status:  payload.Status,
		Remark:  payload.Remark,
		Entries: payload.Entries,
		Limit:   payload.Limit,
		Unit:    payload.Unit,
	}
}

func pluginQuotaTotals(entries []QuotaProbeEntry) (remain, used any, limit *float64, unit string) {
	summary, ok := AggregateQuota("sum", entries)
	if !ok {
		return nil, nil, nil, ""
	}
	return floatToInt(summary.Remaining), floatToInt(summary.Used), summary.Limit, summary.Unit
}

func floatToInt(value *float64) any {
	if value == nil {
		return nil
	}
	return int64(math.Round(*value))
}

func encodePluginCredential(fields map[string]string) (string, error) {
	if len(fields) == 0 {
		return "", errors.New("account credential package is empty")
	}
	encoded, err := json.Marshal(cloneStringMap(fields))
	if err != nil {
		return "", errors.New("account credential package cannot be stored")
	}
	return string(encoded), nil
}

func decodePluginCredential(secret string) (map[string]string, error) {
	var fields map[string]string
	if err := json.Unmarshal([]byte(secret), &fields); err != nil || fields == nil {
		return nil, errors.New("account credential package is invalid")
	}
	return fields, nil
}

// pluginAccountAliasFields are readable, non-secret credential fields used when
// the manifest does not declare a display source or an identity.
var pluginAccountAliasFields = []string{
	"email", "nickname", "nick_name", "preferred_username", "phone", "phone_number", "phoneNumber",
}

// visibleLabel is the account name written onto the credential: manifest display,
// then identity, then a nickname or email field, then a truncated mask.
// The result never contains a secret and is never the full credential id.
func (p pluginAccountAPI) visibleLabel(fields map[string]string, summaryLabel string) string {
	candidates := make([]string, 0, 3)
	if p.pool != nil {
		if label := strings.TrimSpace(p.pool.ResolveDisplay(fields, summaryLabel)); label != "" {
			candidates = append(candidates, label)
		}
		if identity, err := p.pool.ResolveIdentity(fields); err == nil {
			if identity = strings.TrimSpace(identity); identity != "" {
				candidates = append(candidates, identity)
			}
		}
	}
	if alias := pluginAccountPublicAlias(p.pool, fields); alias != "" {
		candidates = append(candidates, alias)
	}
	secrets := p.secretNames()
	access := ""
	if fields != nil {
		access = fields[p.accessField()]
	}
	for _, candidate := range candidates {
		cleaned := cleanPluginDisplayLabel(candidate, fields, secrets)
		if cleaned == "" || pluginLabelInsideSecret(cleaned, access) {
			continue
		}
		return cleaned
	}
	return accountMaskedLabel("", access)
}

func pluginAccountPublicAlias(pool *jsplugin.AccountPoolSpec, fields map[string]string) string {
	if pool == nil || fields == nil {
		return ""
	}
	secret := map[string]struct{}{}
	for _, field := range pool.Credential.Fields {
		if field.Secret {
			secret[field.Name] = struct{}{}
		}
	}
	for _, name := range pluginAccountAliasFields {
		if _, hidden := secret[name]; hidden {
			continue
		}
		if value := strings.TrimSpace(fields[name]); value != "" {
			return value
		}
	}
	return ""
}

func pluginLabelInsideSecret(label, secret string) bool {
	label = strings.TrimSpace(label)
	secret = strings.TrimSpace(secret)
	if label == "" || secret == "" {
		return false
	}
	return strings.Contains(secret, label)
}

func pluginAccountLabelIsUUID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

// pluginAccountLabelReplaceable reports whether a stored name was generated
// (empty, a uuid, a truncation mask, or the sibling auto label) and can be
// replaced. A custom name that differs from its sibling is kept.
func pluginAccountLabelReplaceable(value, sibling string, id uuid.UUID) bool {
	value = strings.TrimSpace(value)
	sibling = strings.TrimSpace(sibling)
	if value == "" || pluginAccountLabelIsUUID(value) {
		return true
	}
	if id != uuid.Nil && strings.EqualFold(value, id.String()) {
		return true
	}
	if strings.Contains(value, "...") || strings.Contains(value, "…") {
		return true
	}
	return sibling != "" && value == sibling
}

func (p pluginAccountAPI) storeAccountLabel(ctx context.Context, credential store.SiteCredential, fields map[string]string, summaryLabel string) (store.SiteCredential, error) {
	label := p.visibleLabel(fields, summaryLabel)
	if strings.TrimSpace(label) == "" {
		return credential, nil
	}
	display := strings.TrimSpace(credential.DisplayName)
	masked := strings.TrimSpace(credential.MaskedSecret)
	nextDisplay := display
	nextMask := masked
	if pluginAccountLabelReplaceable(display, masked, credential.ID) {
		nextDisplay = label
		nextMask = label
	} else if pluginAccountLabelReplaceable(masked, display, credential.ID) {
		nextMask = label
	}
	if nextDisplay == display && nextMask == masked {
		return credential, nil
	}
	return store.NewSiteCredentialRepository(p.s.db.DB()).UpdateAccountLabel(ctx, credential.ID, nextDisplay, nextMask)
}

func (p pluginAccountAPI) rememberAccountLabel(ctx context.Context, credentialID uuid.UUID, fields map[string]string, summaryLabel string) {
	if credentialID == uuid.Nil || p.s == nil || p.s.db == nil {
		return
	}
	credential, err := store.NewSiteCredentialRepository(p.s.db.DB()).GetByID(ctx, credentialID)
	if err != nil {
		return
	}
	_, _ = p.storeAccountLabel(ctx, credential, fields, summaryLabel)
}

func pluginLoginInterval(kind string, interval int) int {
	if interval > 0 {
		return interval
	}
	if kind == accountLoginCustomPoll {
		return 3
	}
	return 5
}

func pluginRegionLabel(text jsplugin.LocalizedText, primary, fallback string) string {
	if value := strings.TrimSpace(text[primary]); value != "" {
		return value
	}
	if value := strings.TrimSpace(text[fallback]); value != "" {
		return value
	}
	return ""
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func redactCredentialError(err error, fields map[string]string, secrets map[string]struct{}) error {
	if err == nil {
		return nil
	}
	redacted := redactText(err.Error(), fields, secrets)
	if redacted == err.Error() {
		return err
	}
	return errors.New(redacted)
}

func cleanPluginDisplayLabel(value string, fields map[string]string, secrets map[string]struct{}) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	for name := range secrets {
		secret := strings.TrimSpace(fields[name])
		if secret != "" && strings.Contains(value, secret) {
			return ""
		}
	}
	return value
}

func redactText(text string, fields map[string]string, secrets map[string]struct{}) string {
	for name := range secrets {
		value := fields[name]
		if len(value) < 4 {
			continue
		}
		text = strings.ReplaceAll(text, value, "[redacted]")
	}
	return text
}

// PluginAccountNeedsRefresh reports whether a decrypted account bag should be
// renewed before this request. A missing or unreadable expiry is renewed.
// An expiry exactly leadSeconds away is not.
func PluginAccountNeedsRefresh(spec jsplugin.AccountPoolSpec, fields map[string]string, now time.Time) bool {
	if spec.Refresh.Kind == "" || spec.Refresh.Kind == "none" {
		return false
	}
	name := spec.Credential.ExpiresAtField
	if name == "" {
		return false
	}
	lead := pluginAccountRefreshLead
	if spec.Refresh.LeadSeconds != nil {
		if *spec.Refresh.LeadSeconds < 0 {
			lead = 0
		} else {
			lead = time.Duration(*spec.Refresh.LeadSeconds) * time.Second
		}
	}
	raw := ""
	if fields != nil {
		raw = fields[name]
	}
	seconds, ok := parsePluginUnixSeconds(raw)
	if !ok {
		return true
	}
	return time.Unix(seconds, 0).Before(now.Add(lead))
}

func parsePluginUnixSeconds(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return normalizePluginUnixSeconds(n), true
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return normalizePluginUnixSeconds(int64(value)), true
}

func normalizePluginUnixSeconds(n int64) int64 {
	if n > 10_000_000_000 {
		return n / 1000
	}
	return n
}

// pluginCredentialWriteHook runs inside the credential write transaction before
// the secret is stored. Tests block on ctx or replace the refresh lease.
// Production leaves it nil.
var pluginCredentialWriteHook func(ctx context.Context) error

// pluginAccountPersistTimeoutForTest shortens persistContext in tests.
// Production leaves it at zero so pluginAccountPersistTimeout applies.
var pluginAccountPersistTimeoutForTest time.Duration

// pluginRefreshLeaseReleased runs after refreshOne leaves the credential lease
// and before it takes the site lock. Tests interleave another writer here.
// Production leaves it nil.
var pluginRefreshLeaseReleased func(siteID uuid.UUID)

// pluginGatewayHoldsSiteLock runs after a gateway refresh takes the site lock
// and before it takes the credential lease. Tests use it to overlap the refresh
// paths. Production leaves it nil.
var pluginGatewayHoldsSiteLock func(siteID uuid.UUID)

type pluginRefreshLeaseContextKey struct{}

type pluginRefreshLeaseID struct {
	credentialID uuid.UUID
	leaseID      string
}

var errPluginRefreshLeaseLost = errors.New("plugin account refresh lease is no longer held")

func persistContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := pluginAccountPersistTimeout
	if pluginAccountPersistTimeoutForTest > 0 {
		timeout = pluginAccountPersistTimeoutForTest
	}
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func abandonIfRefreshLeaseLost(ctx context.Context, tx store.Tx, credentialID uuid.UUID) error {
	held, ok := ctx.Value(pluginRefreshLeaseContextKey{}).(pluginRefreshLeaseID)
	if !ok || held.leaseID == "" {
		return nil
	}
	if held.credentialID != credentialID {
		return errPluginRefreshLeaseLost
	}
	var leaseID string
	err := tx.WithContext(ctx).Raw(`
		SELECT lease_id FROM site_credential_refresh_leases
		WHERE site_credential_id = ? AND lease_id = ? AND lease_until > ?
		FOR UPDATE`,
		credentialID, held.leaseID, time.Now()).Row().Scan(&leaseID)
	if errors.Is(err, sql.ErrNoRows) {
		return errPluginRefreshLeaseLost
	}
	if err != nil {
		return fmt.Errorf("check plugin refresh lease: %w", err)
	}
	if leaseID != held.leaseID {
		return errPluginRefreshLeaseLost
	}
	return nil
}

func (s *Service) withPluginRefreshLease(ctx context.Context, credentialID uuid.UUID, fn func(context.Context) error) error {
	if s == nil || s.db == nil {
		return errors.New("site store is not available")
	}
	leases := store.NewSiteCredentialRefreshLeaseRepository(s.db.DB())
	var held string
	defer func() {
		if held == "" {
			return
		}
		releaseCtx, cancel := persistContext(ctx)
		defer cancel()
		_ = leases.Release(releaseCtx, credentialID, held)
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		leaseID := uuid.NewString()
		acquired, err := leases.TryAcquire(ctx, credentialID, leaseID, time.Now(), pluginAccountRefreshLeaseTTL)
		if err != nil {
			return err
		}
		if !acquired {
			timer := time.NewTimer(pluginAccountRefreshLeaseWait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		held = leaseID
		return fn(context.WithValue(ctx, pluginRefreshLeaseContextKey{}, pluginRefreshLeaseID{
			credentialID: credentialID,
			leaseID:      leaseID,
		}))
	}
}
