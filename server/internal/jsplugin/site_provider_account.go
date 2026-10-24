package jsplugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/textproto"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"weak"

	"xlyra/server/internal/accountauth"
)

const (
	hookStartLogin       = "startLogin"
	hookPollLogin        = "pollLogin"
	hookRefreshTokens    = "refreshTokens"
	hookSummarizeAccount = "summarizeAccount"

	accountRequestTimeout   = 20 * time.Second
	accountLoginSessionTTL  = 10 * time.Minute
	accountLoginDefaultWait = 5
	accountLoginMaxWait     = 60
	maxLoginSessions        = 256
)

// loginSessionReapInterval is how often an idle runtime drops expired login
// sessions. Zero means one minute. Tests shorten it; production leaves it at 0.
var loginSessionReapInterval atomic.Int64

var (
	// ErrLoginSessionNotFound means this process has no jar or device code
	// for the id. Login state is process-local: a poll that lands on another
	// instance, or a restart, loses the session.
	ErrLoginSessionNotFound = errors.New("login session is not in this process")

	// ErrLoginSessionExpired means the device code or the custom login timed out.
	ErrLoginSessionExpired = errors.New("login session expired")

	// ErrTooManyLoginSessions means this process is already holding the maximum
	// number of in-flight logins. Existing sessions are left in place.
	ErrTooManyLoginSessions = errors.New("too many login sessions; try again after one finishes")

	// ErrAccountTransient is a custom-login or custom-refresh transport failure
	// that should be retried. It does not mean the grant is dead.
	// oauth2_refresh uses accountauth.IsTransient instead.
	ErrAccountTransient = errors.New("account provider: transient failure")
)

// AccountProviderRuntime runs one enabled account-pool plugin.
//
// Secrets stay in a SecretBag. JS receives redacted responses and
// {{secret.<field>}} placeholders, never access tokens, refresh tokens, or
// other secret field values. Logs and errors are scrubbed with that bag.
//
// Login cookie jars live in this process, keyed by session id. They are not
// shared across instances and do not survive a restart.
type AccountProviderRuntime struct {
	plugin *Plugin
	spec   AccountPoolSpec
	hosts  []string
	client *http.Client

	// observe receives each hook's arguments exactly as they are passed to JS.
	// Tests use it to prove a token never crosses. Production leaves it nil.
	// The callback must be safe for concurrent calls and must not re-enter the
	// same login session.
	observe func(hook string, args []any)

	mu       sync.Mutex
	sessions map[string]*loginSession
}

// LoginSession is the public handle for an in-flight login.
// Credential is set only when Status is complete. It holds secrets; do not log it.
type LoginSession struct {
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

// String omits the user code and credential values.
func (s LoginSession) String() string {
	return fmt.Sprintf("LoginSession{id:%s kind:%s status:%s interval:%d}", s.ID, s.Kind, s.Status, s.Interval)
}

// GoString omits the user code and credential values.
func (s LoginSession) GoString() string { return s.String() }

// LoginPoll is one poll. Credential is set only when Status is complete.
// It holds secrets; do not log it.
type LoginPoll struct {
	Status     string
	Interval   int
	Identity   string
	Credential map[string]string
}

// String lists credential field names and omits their values.
func (p LoginPoll) String() string {
	return fmt.Sprintf("LoginPoll{status:%s interval:%d identity:%s credential:%s}", p.Status, p.Interval, p.Identity, credentialKeys(p.Credential))
}

// GoString lists credential field names and omits their values.
func (p LoginPoll) GoString() string { return p.String() }

// AccountSnapshot is one account as stored by the host.
// Fields may contain secrets. Summarize strips them before calling JS.
type AccountSnapshot struct {
	BaseURL string
	Fields  map[string]string
}

// AccountSummary is the host view of summarizeAccount.
type AccountSummary struct {
	Status  string
	Remark  string
	Label   string
	Entries []ProbeEntry
}

const maxSummaryLabelBytes = 256

func cleanSummaryLabel(bag *SecretBag, value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" || len(raw) > maxSummaryLabelBytes || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return ""
	}
	if bag != nil && bag.Scrub(raw) != raw {
		return ""
	}
	return raw
}

// loginSession is process-local login state. deviceCode and call.bag are
// secrets. The cookie jar is not visible to JS.
type loginSession struct {
	mu                      sync.Mutex
	id                      string
	kind                    string
	deviceCode              string
	userCode                string
	verificationURL         string
	verificationURLComplete string
	interval                int
	expiresAt               time.Time // set before publish; not written again
	closed                  bool
	params                  map[string]string
	call                    accountCall
	// baseline is the step list after startLogin. Each poll restores it so a
	// pending response does not stay in the list and block the next poll.
	baseline []ProbeStep
}

// accountCall is one stepped exchange. The bag is not safe for concurrent use;
// the caller holds the session lock, or the call is local to one goroutine.
type accountCall struct {
	bag   *SecretBag
	jar   http.CookieJar
	steps []ProbeStep
	base  string
}

// NewAccountProviderRuntime builds a runtime from an account-pool plugin.
// client is optional; nil uses the default transport with a 20s timeout.
// A non-nil client's dialer is kept, including an SSRF guard installed by
// the host. The plugin must already declare the hooks its manifest requires.
func NewAccountProviderRuntime(plugin *Plugin, client *http.Client) (*AccountProviderRuntime, error) {
	if plugin == nil {
		return nil, errors.New("plugin is required")
	}
	if plugin.Manifest.Kind != KindSiteProvider {
		return nil, fmt.Errorf("plugin %s is not a site_provider", plugin.Manifest.ID)
	}
	spec, err := NewAccountPoolSpec(plugin.Manifest.SiteProvider)
	if err != nil {
		return nil, err
	}
	for _, name := range accountPoolRequiredExports(plugin.Manifest.SiteProvider) {
		if !plugin.HasHook(name) {
			return nil, fmt.Errorf("%s hook is missing", name)
		}
	}
	rt := &AccountProviderRuntime{
		plugin:   plugin,
		spec:     spec,
		hosts:    append([]string(nil), plugin.Manifest.SiteProvider.Hosts...),
		client:   client,
		sessions: map[string]*loginSession{},
	}
	rt.armLoginReaper()
	return rt, nil
}

// StartLogin begins a device-code or custom_poll login.
// params carries login UI values such as region. Tokens are not accepted here.
func (r *AccountProviderRuntime) StartLogin(ctx context.Context, params map[string]string) (session LoginSession, err error) {
	defer func() { r.record(err) }()
	if err = ctx.Err(); err != nil {
		return LoginSession{}, err
	}
	params = cloneParams(params)
	if err = r.rejectIfLoginSessionsFull(); err != nil {
		return LoginSession{}, err
	}
	switch r.spec.Login.Kind {
	case "oauth2_device_code":
		return r.startOAuth(ctx)
	case "custom_poll":
		return r.startCustom(ctx, params)
	default:
		return LoginSession{}, fmt.Errorf("login kind %q is not supported", r.spec.Login.Kind)
	}
}

// PollLogin continues the session started by StartLogin.
// A session id from another process returns ErrLoginSessionNotFound.
func (r *AccountProviderRuntime) PollLogin(ctx context.Context, session LoginSession) (poll LoginPoll, err error) {
	defer func() { r.record(err) }()
	if err = ctx.Err(); err != nil {
		return LoginPoll{}, err
	}
	state := r.load(session.ID)
	if state == nil {
		return LoginPoll{}, ErrLoginSessionNotFound
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return LoginPoll{}, ErrLoginSessionNotFound
	}
	if !state.expiresAt.IsZero() && time.Now().After(state.expiresAt) {
		r.drop(state)
		return LoginPoll{}, ErrLoginSessionExpired
	}
	switch state.kind {
	case "oauth2_device_code":
		return r.pollOAuth(ctx, state)
	case "custom_poll":
		return r.pollCustom(ctx, state)
	default:
		return LoginPoll{}, fmt.Errorf("login kind %q is not supported", state.kind)
	}
}

// Refresh renews credentialFields.
// oauth2_refresh keeps the previous refresh token when the provider omits a
// new one. A dead grant matches accountauth.ErrReauthorizationRequired.
// Custom transport failures match ErrAccountTransient; oauth2 HTTP 5xx matches
// accountauth.IsTransient. Custom refresh uses the account region (an explicit
// region field, then realm, then domain) and otherwise the default region.
// leadSeconds is not applied here.
func (r *AccountProviderRuntime) Refresh(ctx context.Context, credentialFields map[string]string) (fields map[string]string, err error) {
	defer func() { r.record(err) }()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	switch r.spec.Refresh.Kind {
	case "none":
		return nil, errors.New("refresh is not supported")
	case "oauth2_refresh":
		return r.refreshOAuth(ctx, credentialFields)
	case "custom_steps":
		return r.refreshCustom(ctx, credentialFields)
	default:
		return nil, fmt.Errorf("refresh kind %q is not supported", r.spec.Refresh.Kind)
	}
}

// Summarize calls summarizeAccount. snapshot.Fields may contain secrets;
// the hook receives only non-secret fields and redacted responses.
func (r *AccountProviderRuntime) Summarize(ctx context.Context, snapshot AccountSnapshot) (summary AccountSummary, err error) {
	defer func() { r.record(err) }()
	if err = ctx.Err(); err != nil {
		return AccountSummary{}, err
	}
	if !r.plugin.HasHook(hookSummarizeAccount) {
		return AccountSummary{}, fmt.Errorf("%s hook is missing", hookSummarizeAccount)
	}
	base, err := r.resolveBase(snapshot.BaseURL, nil)
	if err != nil {
		return AccountSummary{}, err
	}
	bag := r.seedBag(snapshot.Fields)
	call := &accountCall{bag: bag, base: base}
	ctxValue := r.credentialContext(base, r.publicFields(snapshot.Fields, bag))
	result, err := r.drive(ctx, call, hookSummarizeAccount, KindAccountSummary, ctxValue, nil)
	if err != nil {
		return AccountSummary{}, bag.ScrubError(err)
	}
	parsed, ok := result.(*AccountSummaryResult)
	if !ok || parsed == nil {
		return AccountSummary{}, errors.New("unexpected summarize result")
	}
	return scrubSummary(bag, *parsed), nil
}

// ListModels calls listModels for one account.
// fields may contain secrets. The hook receives only non-secret fields, plus
// the resolved region id when the bag does not already store region. The base
// URL is the account region, using the same order as the gateway. Requests
// substitute {{secret.<field>}} from the bag. A secret never enters JS. An
// HTTP 401 is reauthorization and is not shown to the hook.
func (r *AccountProviderRuntime) ListModels(ctx context.Context, fields map[string]string) (entries []ModelListEntry, err error) {
	defer func() { r.record(err) }()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if !r.plugin.HasHook("listModels") {
		return nil, fmt.Errorf("%s hook is missing", "listModels")
	}
	base, err := r.accountBase(fields)
	if err != nil {
		return nil, err
	}
	bag := r.seedBag(fields)
	call := &accountCall{bag: bag, base: base}
	probe := ProbeContext{
		SiteType:       r.plugin.Manifest.SiteProvider.SiteType,
		BaseURL:        base,
		CredentialType: "account",
		Now:            time.Now().UnixMilli(),
		Account:        r.accountContextFields(fields, bag),
	}
	result, err := r.driveModelList(ctx, call, probe)
	if err != nil {
		return nil, bag.ScrubError(err)
	}
	list, ok := result.(*ModelListResult)
	if !ok || list == nil {
		return nil, errors.New("unexpected model list result")
	}
	return append([]ModelListEntry(nil), list.Models...), nil
}

func (r *AccountProviderRuntime) startOAuth(ctx context.Context) (LoginSession, error) {
	cfg, err := r.oauthConfig(r.spec.Login.TokenURL)
	if err != nil {
		return LoginSession{}, err
	}
	start, err := accountauth.StartDeviceCode(ctx, cfg)
	if err != nil {
		return LoginSession{}, scrubOutboundError(nil, err)
	}
	bag := NewSecretBag()
	bag.Set("device_code", start.DeviceCode)
	if start.UserCode != "" {
		bag.Set("user_code", start.UserCode)
	}
	state, err := r.newSession("oauth2_device_code", bag, nil, "")
	if err != nil {
		return LoginSession{}, bag.ScrubError(err)
	}
	state.deviceCode = start.DeviceCode
	state.userCode = start.UserCode
	state.verificationURL = start.VerificationURI
	state.verificationURLComplete = start.VerificationURIComplete
	state.interval = start.Interval
	state.expiresAt = time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)
	if err := r.store(state); err != nil {
		state.wipe()
		return LoginSession{}, err
	}
	return state.public("pending"), nil
}

func (r *AccountProviderRuntime) pollOAuth(ctx context.Context, state *loginSession) (LoginPoll, error) {
	cfg, err := r.oauthConfig(r.spec.Login.TokenURL)
	if err != nil {
		return LoginPoll{}, state.call.bag.ScrubError(err)
	}
	poll, err := accountauth.PollDeviceCode(ctx, cfg, state.deviceCode, state.interval)
	if err != nil {
		return LoginPoll{}, scrubOutboundError(state.call.bag, err)
	}
	state.interval = poll.Interval
	switch poll.Status {
	case accountauth.PollPending, accountauth.PollSlowDown:
		return LoginPoll{Status: poll.Status, Interval: poll.Interval}, nil
	case accountauth.PollComplete:
		return r.finishOAuth(state, poll.Tokens, nil)
	default:
		return LoginPoll{}, errors.New("oauth2: unexpected poll status")
	}
}

func (r *AccountProviderRuntime) finishOAuth(state *loginSession, tokens *accountauth.OAuth2Tokens, previous map[string]string) (LoginPoll, error) {
	if tokens == nil || strings.TrimSpace(tokens.AccessToken) == "" {
		return LoginPoll{}, errors.New("oauth2: token response missing access_token")
	}
	bag := state.call.bag
	if bag == nil {
		bag = NewSecretBag()
		state.call.bag = bag
	}
	bag.Set(r.accessField(), tokens.AccessToken)
	if name := r.spec.Credential.RefreshTokenField; name != "" && tokens.RefreshToken != "" {
		bag.Set(name, tokens.RefreshToken)
	}
	if tokens.IDToken != "" {
		bag.note(tokens.IDToken)
	}
	cred, err := oauthCredential(r.spec, *tokens, previous)
	if err != nil {
		return LoginPoll{}, bag.ScrubError(err)
	}
	identity, err := r.identity(cred, tokens.AccessToken, tokens.IDToken, bag)
	if err != nil {
		return LoginPoll{}, err
	}
	out := LoginPoll{Status: "complete", Interval: state.interval, Identity: identity, Credential: maps.Clone(cred)}
	r.drop(state)
	return out, nil
}

func (r *AccountProviderRuntime) refreshOAuth(ctx context.Context, credentialFields map[string]string) (map[string]string, error) {
	name := r.spec.Credential.RefreshTokenField
	refresh := ""
	if name != "" && credentialFields != nil {
		refresh = credentialFields[name]
	}
	bag := r.seedBag(credentialFields)
	if strings.TrimSpace(refresh) == "" {
		return nil, errors.New("refresh token is required")
	}
	tokenURL := strings.TrimSpace(r.spec.Refresh.TokenURL)
	if tokenURL == "" {
		tokenURL = r.spec.Login.TokenURL
	}
	cfg, err := r.oauthConfig(tokenURL)
	if err != nil {
		return nil, bag.ScrubError(err)
	}
	tokens, err := accountauth.RefreshOAuth2(ctx, cfg, refresh)
	if err != nil {
		return nil, scrubOutboundError(bag, err)
	}
	bag.Set(r.accessField(), tokens.AccessToken)
	if tokens.RefreshToken != "" && name != "" {
		bag.Set(name, tokens.RefreshToken)
	}
	if tokens.IDToken != "" {
		bag.note(tokens.IDToken)
	}
	cred, err := oauthCredential(r.spec, tokens, credentialFields)
	if err != nil {
		return nil, bag.ScrubError(err)
	}
	return cred, nil
}

func (r *AccountProviderRuntime) startCustom(ctx context.Context, params map[string]string) (LoginSession, error) {
	base, err := r.baseFor(params)
	if err != nil {
		return LoginSession{}, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return LoginSession{}, err
	}
	bag := NewSecretBag()
	state, err := r.newSession("custom_poll", bag, jar, base)
	if err != nil {
		return LoginSession{}, err
	}
	state.expiresAt = time.Now().Add(accountLoginSessionTTL)
	state.params = params
	state.mu.Lock()
	defer state.mu.Unlock()
	result, err := r.drive(ctx, &state.call, hookStartLogin, KindAccountLogin, r.loginContext(base, params), r.spec.Login.SecretPaths)
	if err != nil {
		state.wipe()
		return LoginSession{}, bag.ScrubError(err)
	}
	parsed, err := loginResult(result)
	if err != nil {
		state.wipe()
		return LoginSession{}, bag.ScrubError(err)
	}
	state.interval = clampInterval(parsed.Interval)
	if parsed.Status == "complete" {
		poll, err := r.finishCustom(state, parsed, nil)
		if err != nil {
			return LoginSession{}, err
		}
		return LoginSession{
			ID: state.id, Kind: state.kind, Status: "complete", Interval: poll.Interval,
			ExpiresAt: state.expiresAt, Identity: poll.Identity, Credential: poll.Credential,
		}, nil
	}
	if err := r.acceptPending(state, parsed); err != nil {
		state.wipe()
		return LoginSession{}, bag.ScrubError(err)
	}
	state.baseline = cloneSteps(state.call.steps)
	if err := r.store(state); err != nil {
		state.wipe()
		return LoginSession{}, err
	}
	return state.public("pending"), nil
}

func (r *AccountProviderRuntime) pollCustom(ctx context.Context, state *loginSession) (LoginPoll, error) {
	if len(state.baseline) > 0 {
		state.call.steps = cloneSteps(state.baseline)
	}
	result, err := r.drive(ctx, &state.call, hookPollLogin, KindAccountLogin, r.loginContext(state.call.base, state.params), r.spec.Login.SecretPaths)
	if errors.Is(err, errPollNonJSON) {
		return LoginPoll{Status: "pending", Interval: state.interval}, nil
	}
	if err != nil {
		return LoginPoll{}, state.call.bag.ScrubError(err)
	}
	parsed, err := loginResult(result)
	if err != nil {
		return LoginPoll{}, state.call.bag.ScrubError(err)
	}
	if parsed.Status == "complete" {
		return r.finishCustom(state, parsed, nil)
	}
	if err := r.acceptPending(state, parsed); err != nil {
		return LoginPoll{}, state.call.bag.ScrubError(err)
	}
	return LoginPoll{Status: "pending", Interval: state.interval}, nil
}

func (r *AccountProviderRuntime) finishCustom(state *loginSession, parsed *AccountLoginResult, previous map[string]string) (LoginPoll, error) {
	cred, err := r.materialize(state.call.bag, parsed.Credential)
	if err != nil {
		return LoginPoll{}, state.call.bag.ScrubError(err)
	}
	cred = mergeCredential(r.spec, previous, cred)
	FillExpiresFrom(r.spec, cred)
	identity, err := r.identity(cred, cred[r.accessField()], "", state.call.bag)
	if err != nil {
		return LoginPoll{}, err
	}
	out := LoginPoll{Status: "complete", Interval: state.interval, Identity: identity, Credential: maps.Clone(cred)}
	r.drop(state)
	return out, nil
}

func (r *AccountProviderRuntime) refreshCustom(ctx context.Context, credentialFields map[string]string) (map[string]string, error) {
	params := map[string]string{}
	if id := AccountRegionID(r.spec.Regions, credentialFields); id != "" {
		params["region"] = id
	}
	base, err := r.baseFor(params)
	if err != nil {
		return nil, err
	}
	bag := r.seedBag(credentialFields)
	call := &accountCall{bag: bag, base: base}
	result, err := r.drive(ctx, call, hookRefreshTokens, KindAccountRefresh, r.credentialContext(base, r.publicFields(credentialFields, bag)), r.spec.Refresh.SecretPaths)
	if err != nil {
		return nil, bag.ScrubError(err)
	}
	parsed, ok := result.(*AccountRefreshResult)
	if !ok || parsed == nil {
		return nil, errors.New("unexpected refresh result")
	}
	switch parsed.Status {
	case "reauth":
		return nil, bag.ScrubError(accountauth.ErrReauthorizationRequired)
	case "ok":
		cred, err := r.materialize(bag, parsed.Credential)
		if err != nil {
			return nil, bag.ScrubError(err)
		}
		merged := mergeCredential(r.spec, credentialFields, cred)
		FillExpiresFrom(r.spec, merged)
		return merged, nil
	default:
		return nil, fmt.Errorf("refresh status %q is not supported", parsed.Status)
	}
}

func (r *AccountProviderRuntime) acceptPending(state *loginSession, parsed *AccountLoginResult) error {
	if parsed.VerificationURL != "" {
		if err := r.checkURL(parsed.VerificationURL); err != nil {
			return err
		}
		state.verificationURL = parsed.VerificationURL
	}
	if parsed.UserCode != "" {
		state.userCode = parsed.UserCode
		if state.call.bag != nil {
			state.call.bag.Set("user_code", parsed.UserCode)
		}
	}
	state.interval = clampInterval(parsed.Interval)
	return nil
}

// drive runs one stepped hook. session.bag is updated with secretPaths hits.
// Steps shown back to JS use the pre-substitution request and the redacted response.
// Caller holds session.mu when call belongs to a login session.
func (r *AccountProviderRuntime) drive(ctx context.Context, call *accountCall, hook, resultKind string, ctxValue map[string]any, rules map[string]string) (any, error) {
	if call.bag == nil {
		call.bag = NewSecretBag()
	}
	allowed := r.allowedSecrets()
	client := r.hookedClient(call.jar, call.base)
	for step := 0; step < MaxProbeSteps; step++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.observe != nil {
			r.observe(hook, []any{ctxValue, stepsAsValue(call.steps)})
		}
		decision, err := r.plugin.CallAccountHook(ctx, hook, resultKind, ctxValue, call.steps)
		logs, dropped := decision.Logs, decision.DroppedLogs
		if callErr, ok := err.(*CallError); ok && len(logs) == 0 {
			logs, dropped = callErr.Logs, callErr.DroppedLogs
		}
		r.emit(ctx, hook, step, call.bag, logs, dropped, err)
		if err != nil {
			return nil, call.bag.ScrubError(err)
		}
		if decision.Error != "" {
			return nil, fmt.Errorf("plugin %s: %s", r.plugin.Manifest.ID, call.bag.Scrub(decision.Error))
		}
		if decision.Result != nil {
			return decision.Result, nil
		}
		if decision.Request == nil || step == MaxProbeSteps-1 {
			return nil, fmt.Errorf("plugin %s exceeded %d steps", r.plugin.Manifest.ID, MaxProbeSteps)
		}
		template := *decision.Request
		response, err := r.doAccountHTTP(ctx, client, call, allowed, rules, template, hook == hookPollLogin)
		if err != nil {
			return nil, err
		}
		call.steps = append(call.steps, ProbeStep{
			Request:  scrubProbeRequest(call.bag, template),
			Response: response,
		})
	}
	return nil, fmt.Errorf("plugin %s exceeded %d steps", r.plugin.Manifest.ID, MaxProbeSteps)
}

// driveModelList is drive for listModels. It uses the stepped model_list
// contract and the account secret bag. HTTP 401 stops the hook and asks the
// caller to mark the account for reauthorization.
func (r *AccountProviderRuntime) driveModelList(ctx context.Context, call *accountCall, probe ProbeContext) (any, error) {
	if call.bag == nil {
		call.bag = NewSecretBag()
	}
	allowed := r.allowedSecrets()
	client := r.hookedClient(call.jar, call.base)
	ctxValue := probe.asMap()
	for step := 0; step < MaxProbeSteps; step++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.observe != nil {
			r.observe("listModels", []any{ctxValue, stepsAsValue(call.steps)})
		}
		decision, err := r.plugin.CallSteppedHook(ctx, "listModels", KindModelList, probe, call.steps)
		logs, dropped := decision.Logs, decision.DroppedLogs
		if callErr, ok := err.(*CallError); ok && len(logs) == 0 {
			logs, dropped = callErr.Logs, callErr.DroppedLogs
		}
		r.emit(ctx, "listModels", step, call.bag, logs, dropped, err)
		if err != nil {
			return nil, call.bag.ScrubError(err)
		}
		if decision.Error != "" {
			return nil, fmt.Errorf("plugin %s: %s", r.plugin.Manifest.ID, call.bag.Scrub(decision.Error))
		}
		if decision.Result != nil {
			return decision.Result, nil
		}
		if decision.Request == nil || step == MaxProbeSteps-1 {
			return nil, fmt.Errorf("plugin %s exceeded %d steps", r.plugin.Manifest.ID, MaxProbeSteps)
		}
		template := *decision.Request
		response, err := r.doAccountHTTP(ctx, client, call, allowed, nil, template, false)
		if err != nil {
			return nil, err
		}
		if response.Status == http.StatusUnauthorized {
			return nil, accountauth.ErrReauthorizationRequired
		}
		call.steps = append(call.steps, ProbeStep{
			Request:  scrubProbeRequest(call.bag, template),
			Response: response,
		})
	}
	return nil, fmt.Errorf("plugin %s exceeded %d steps", r.plugin.Manifest.ID, MaxProbeSteps)
}

// doAccountHTTP substitutes placeholders, checks the host, then redacts the
// response into call.bag before anything is shown to JS.
// The template path and query are checked before substitution. After
// substitution the host must be unchanged, and the host and path must not
// contain a secret. Query values and header values may.
// errPollNonJSON is a static sentinel. It carries no response body, so a poll
// step that opts into onNonJSON pending can ignore a non-JSON body without
// showing that body to JS or writing it into an error.
var errPollNonJSON = errors.New("poll response is not JSON")

func (r *AccountProviderRuntime) doAccountHTTP(ctx context.Context, client *http.Client, call *accountCall, allowed map[string]bool, rules map[string]string, template ProbeRequest, honorNonJSON bool) (ProbeResponse, error) {
	before, err := ResolveProbeURL(call.base, template.Path, template.Query)
	if err != nil {
		return ProbeResponse{}, &CallError{Kind: KindURLRejected, Detail: err.Error()}
	}
	if err := r.checkURL(before); err != nil {
		return ProbeResponse{}, err
	}
	request := template
	if HasSecretPlaceholder(template) {
		request, err = SubstituteSecrets(template, call.bag, allowed)
		if err != nil {
			return ProbeResponse{}, call.bag.ScrubError(err)
		}
	}
	after, err := ResolveProbeURL(call.base, request.Path, request.Query)
	if err != nil {
		return ProbeResponse{}, call.bag.ScrubError(&CallError{Kind: KindURLRejected, Detail: err.Error()})
	}
	if requestHost(before) != requestHost(after) {
		return ProbeResponse{}, errors.New(KindURLRejected + ": substitution changed the host")
	}
	if err := r.checkURL(after); err != nil {
		return ProbeResponse{}, err
	}
	if err := rejectSecretInURL(call.bag, after); err != nil {
		return ProbeResponse{}, err
	}
	headers, err := accountRequestHeaders(request.Headers)
	if err != nil {
		return ProbeResponse{}, err
	}
	var body io.Reader
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Body != nil {
		encoded, err := encodeAccountBody(request.Body)
		if err != nil {
			return ProbeResponse{}, err
		}
		body = strings.NewReader(string(encoded))
		if _, isString := request.Body.(string); !isString {
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, request.Method, after, body)
	if err != nil {
		return ProbeResponse{}, call.bag.ScrubError(err)
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ProbeResponse{}, ctx.Err()
		}
		var redirected *accountRedirectError
		if errors.As(err, &redirected) {
			// Do not include the redirect URL. It can carry a code that is not in the bag yet.
			return ProbeResponse{}, &CallError{Kind: KindURLRejected, Detail: "redirect left the allowed host"}
		}
		return ProbeResponse{}, accountTransportError(call.bag, err)
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeResponseBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return ProbeResponse{}, ctx.Err()
		}
		return ProbeResponse{}, accountTransportError(call.bag, err)
	}
	if len(buf) > maxProbeResponseBody {
		return ProbeResponse{}, &CallError{Kind: KindResponseTooLarge, Detail: "response is too large"}
	}
	contentType := resp.Header.Get("Content-Type")
	if honorNonJSON && template.OnNonJSON == "pending" && !accountJSONObject(buf) {
		return ProbeResponse{}, errPollNonJSON
	}
	redactedBody, payload, err := redactProbeBody(call.bag, contentType, buf, rules)
	if err != nil {
		return ProbeResponse{}, call.bag.ScrubError(err)
	}
	headersOut := FilterResponseHeaders(resp.Header)
	for key, value := range headersOut {
		headersOut[key] = call.bag.Scrub(value)
	}
	return ProbeResponse{Status: resp.StatusCode, Headers: headersOut, Body: redactedBody, JSON: payload}, nil
}

func (r *AccountProviderRuntime) oauthConfig(tokenURL string) (accountauth.OAuth2Config, error) {
	login := r.spec.Login
	return accountauth.NewOAuth2Config(accountauth.OAuth2Config{
		DeviceCodeURL: login.DeviceCodeURL,
		TokenURL:      tokenURL,
		ClientID:      login.ClientID,
		Scope:         login.Scope,
		GrantType:     login.GrantType,
		ExtraHeaders:  login.ExtraHeaders,
		CheckURL:      r.checkURL,
		Client:        r.client,
	})
}

// checkURL allows a URL whose hostname is an exact allow-list entry.
// http is accepted only for a loopback host that is itself on the list, so
// tests can use httptest. Every other host must be https. The raw URL is not
// included in the error.
func (r *AccountProviderRuntime) checkURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.Scheme == "" || parsed.User != nil {
		return errors.New(KindURLRejected + ": url rejected")
	}
	host := strings.ToLower(parsed.Hostname())
	if !r.hostAllowed(host) {
		return fmt.Errorf("%s: host %q is not allowed", KindURLRejected, host)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return nil
	case "http":
		if host == "127.0.0.1" || host == "::1" || host == "localhost" {
			return nil
		}
	}
	return fmt.Errorf("%s: host %q is not allowed", KindURLRejected, host)
}

func (r *AccountProviderRuntime) hostAllowed(host string) bool {
	host = strings.ToLower(host)
	for _, allowed := range r.hosts {
		if strings.ToLower(allowed) == host {
			return true
		}
	}
	return false
}

func cloneSteps(in []ProbeStep) []ProbeStep {
	if len(in) == 0 {
		return nil
	}
	out := make([]ProbeStep, len(in))
	copy(out, in)
	return out
}

func (r *AccountProviderRuntime) baseFor(params map[string]string) (string, error) {
	id := ""
	if params != nil {
		id = strings.TrimSpace(params["region"])
	}
	if id == "" {
		id = r.spec.DefaultRegion
	}
	for _, region := range r.spec.Regions {
		if region.ID != id {
			continue
		}
		base := strings.TrimRight(strings.TrimSpace(region.BaseURL), "/")
		if err := r.checkURL(base); err != nil {
			return "", err
		}
		return base, nil
	}
	return "", fmt.Errorf("region %q is not configured", id)
}

func (r *AccountProviderRuntime) resolveBase(explicit string, params map[string]string) (string, error) {
	explicit = strings.TrimRight(strings.TrimSpace(explicit), "/")
	if explicit == "" {
		return r.baseFor(params)
	}
	if err := r.checkURL(explicit); err != nil {
		return "", err
	}
	return explicit, nil
}

func (r *AccountProviderRuntime) loginContext(base string, params map[string]string) map[string]any {
	if params == nil {
		params = map[string]string{}
	}
	return contextMap(AccountLoginContext{
		SiteType: r.plugin.Manifest.SiteProvider.SiteType,
		BaseURL:  base,
		Params:   params,
		Now:      time.Now().UnixMilli(),
	})
}

func (r *AccountProviderRuntime) credentialContext(base string, fields map[string]string) map[string]any {
	if fields == nil {
		fields = map[string]string{}
	}
	return contextMap(AccountCredentialContext{
		SiteType: r.plugin.Manifest.SiteProvider.SiteType,
		BaseURL:  base,
		Fields:   fields,
		Now:      time.Now().UnixMilli(),
	})
}

func contextMap(value any) map[string]any {
	object, _ := encodeValue(value).(map[string]any)
	if object == nil {
		return map[string]any{}
	}
	return object
}

func (r *AccountProviderRuntime) allowedSecrets() map[string]bool {
	allowed := make(map[string]bool)
	for _, field := range r.spec.Credential.Fields {
		if field.Secret {
			allowed[field.Name] = true
		}
	}
	return allowed
}

func (r *AccountProviderRuntime) seedBag(fields map[string]string) *SecretBag {
	bag := NewSecretBag()
	if fields == nil {
		return bag
	}
	for _, field := range r.spec.Credential.Fields {
		if !field.Secret {
			continue
		}
		if value := fields[field.Name]; value != "" {
			bag.Set(field.Name, value)
		}
	}
	return bag
}

func (r *AccountProviderRuntime) accountBase(fields map[string]string) (string, error) {
	id := AccountRegionID(r.spec.Regions, fields)
	if id == "" {
		id = r.spec.DefaultRegion
	}
	return r.baseFor(map[string]string{"region": id})
}

// accountContextFields is the non-secret bag plus the resolved region id.
// An explicit region field is left as stored.
func (r *AccountProviderRuntime) accountContextFields(fields map[string]string, bag *SecretBag) map[string]string {
	out := r.publicFields(fields, bag)
	if strings.TrimSpace(out["region"]) != "" {
		return out
	}
	id := AccountRegionID(r.spec.Regions, fields)
	if id == "" {
		id = r.spec.DefaultRegion
	}
	if id != "" {
		out["region"] = id
	}
	return out
}

// publicFields returns declared non-secret fields. A value that still contains
// a bag secret is dropped rather than shown to JS.
func (r *AccountProviderRuntime) publicFields(fields map[string]string, bag *SecretBag) map[string]string {
	out := map[string]string{}
	if fields == nil {
		return out
	}
	for _, field := range r.spec.Credential.Fields {
		if field.Secret {
			continue
		}
		value, ok := fields[field.Name]
		if !ok {
			continue
		}
		value = bag.Scrub(value)
		if bag.containsNeedle(value) {
			continue
		}
		out[field.Name] = value
	}
	return out
}

func (r *AccountProviderRuntime) accessField() string {
	if r.spec.Credential.AccessTokenField != "" {
		return r.spec.Credential.AccessTokenField
	}
	return defaultAccessTokenField
}

// materialize copies a hook credential claim into a secret-bearing map.
// A secret field must be exactly {{secret.<same field>}}. The raw token is
// taken from the bag. Any other value for a secret field is rejected.
func (r *AccountProviderRuntime) materialize(bag *SecretBag, claim map[string]string) (map[string]string, error) {
	if len(claim) == 0 {
		return nil, errors.New("credential claim is required")
	}
	declared := make(map[string]credentialFieldMeta, len(r.spec.Credential.Fields))
	for _, field := range r.spec.Credential.Fields {
		typ := field.Type
		if typ == "" {
			typ = "string"
		}
		declared[field.Name] = credentialFieldMeta{secret: field.Secret, typ: typ}
	}
	out := make(map[string]string, len(claim))
	for _, name := range mapKeys(claim) {
		meta, ok := declared[name]
		if !ok {
			return nil, fmt.Errorf("credential field %q is not declared", name)
		}
		value := claim[name]
		if meta.secret {
			if value != "{{secret."+name+"}}" {
				return nil, fmt.Errorf("credential field %q must reference the secret bag", name)
			}
			secret, ok := bag.Get(name)
			if !ok || secret == "" {
				return nil, fmt.Errorf("%w: %s", ErrSecretFieldUnavailable, name)
			}
			out[name] = secret
			continue
		}
		if strings.Contains(value, secretPrefix) || (bag != nil && bag.containsNeedle(value)) {
			return nil, fmt.Errorf("credential field %q contains a secret", name)
		}
		if strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("credential field %q contains a control character", name)
		}
		if meta.typ == "unix" && !validUnix(value) {
			return nil, fmt.Errorf("credential field %q must be a unix timestamp", name)
		}
		out[name] = value
	}
	if out[r.accessField()] == "" {
		return nil, fmt.Errorf("credential field %q is required", r.accessField())
	}
	return out, nil
}

func (r *AccountProviderRuntime) identity(cred map[string]string, access, idToken string, bag *SecretBag) (string, error) {
	value, err := resolveIdentity(r.spec.Credential.Identity.Items, cred, access, idToken)
	if err != nil {
		return "", bag.ScrubError(err)
	}
	if bag != nil && bag.containsNeedle(value) {
		return "", errors.New("identity contains a secret")
	}
	return value, nil
}

func accountJSONObject(raw []byte) bool {
	root, err := decodeJSONUseNumber(raw)
	if err != nil || root == nil {
		return false
	}
	_, ok := root.(map[string]any)
	return ok
}

func (r *AccountProviderRuntime) hookedClient(jar http.CookieJar, base string) *http.Client {
	client := &http.Client{Timeout: accountRequestTimeout}
	if r.client != nil {
		copied := *r.client
		client = &copied
		if client.Timeout == 0 {
			client.Timeout = accountRequestTimeout
		}
	}
	client.Jar = jar
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return &accountRedirectError{err: errors.New("stopped after 10 redirects")}
		}
		if err := SameOrigin(base, req.URL.String()); err != nil {
			return &accountRedirectError{err: err}
		}
		if err := r.checkURL(req.URL.String()); err != nil {
			return &accountRedirectError{err: err}
		}
		return nil
	}
	return client
}

func (r *AccountProviderRuntime) newSession(kind string, bag *SecretBag, jar http.CookieJar, base string) (*loginSession, error) {
	id, err := newLoginID()
	if err != nil {
		return nil, err
	}
	return &loginSession{
		id:   id,
		kind: kind,
		call: accountCall{bag: bag, jar: jar, base: base},
	}, nil
}

func (r *AccountProviderRuntime) store(session *loginSession) error {
	r.mu.Lock()
	expired := r.detachExpiredLocked(time.Now(), nil)
	if r.sessions == nil {
		r.sessions = map[string]*loginSession{}
	}
	if _, exists := r.sessions[session.id]; !exists && len(r.sessions) >= maxLoginSessions {
		r.mu.Unlock()
		wipeDetached(expired)
		return ErrTooManyLoginSessions
	}
	r.sessions[session.id] = session
	r.mu.Unlock()
	wipeDetached(expired)
	return nil
}

func (r *AccountProviderRuntime) load(id string) *loginSession {
	r.mu.Lock()
	session := r.sessions[id]
	expired := r.detachExpiredLocked(time.Now(), session)
	r.mu.Unlock()
	wipeDetached(expired)
	return session
}

func (r *AccountProviderRuntime) rejectIfLoginSessionsFull() error {
	r.mu.Lock()
	expired := r.detachExpiredLocked(time.Now(), nil)
	full := len(r.sessions) >= maxLoginSessions
	r.mu.Unlock()
	wipeDetached(expired)
	if full {
		return ErrTooManyLoginSessions
	}
	return nil
}

// reapLoginSessions drops expired sessions and wipes them after they leave the map.
// now is injectable so a poll can keep its lock while a later timestamp expires it.
func (r *AccountProviderRuntime) reapLoginSessions(now time.Time) {
	if r == nil {
		return
	}
	r.mu.Lock()
	expired := r.detachExpiredLocked(now, nil)
	r.mu.Unlock()
	wipeDetached(expired)
}

// drop wipes secrets and removes the session. Caller holds session.mu.
func (r *AccountProviderRuntime) drop(session *loginSession) {
	session.wipe()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[session.id] == session {
		delete(r.sessions, session.id)
	}
}

// detachExpiredLocked removes expired sessions from the map. The caller wipes
// them after releasing r.mu so a PollLogin holding session.mu can finish.
// keep stays in the map; PollLogin expires that session itself.
func (r *AccountProviderRuntime) detachExpiredLocked(now time.Time, keep *loginSession) []*loginSession {
	if len(r.sessions) == 0 {
		return nil
	}
	var expired []*loginSession
	for id, session := range r.sessions {
		if session == keep || session.expiresAt.IsZero() || !now.After(session.expiresAt) {
			continue
		}
		delete(r.sessions, id)
		expired = append(expired, session)
	}
	return expired
}

func wipeDetached(sessions []*loginSession) {
	for _, session := range sessions {
		if session == nil {
			continue
		}
		session.mu.Lock()
		session.wipe()
		session.mu.Unlock()
	}
}

// armLoginReaper reaps expired sessions on a timer. The timer holds a weak
// pointer, so a catalog reload that drops the runtime ends the chain on the
// next tick instead of leaving a goroutine behind.
func (r *AccountProviderRuntime) armLoginReaper() {
	scheduleLoginReap(weak.Make(r))
}

func scheduleLoginReap(slot weak.Pointer[AccountProviderRuntime]) {
	interval := time.Duration(loginSessionReapInterval.Load())
	if interval <= 0 {
		interval = time.Minute
	}
	time.AfterFunc(interval, func() {
		rt := slot.Value()
		if rt == nil {
			return
		}
		rt.reapLoginSessions(time.Now())
		scheduleLoginReap(slot)
	})
}

func accountTransportError(bag *SecretBag, err error) error {
	text := redactRequestURL(err.Error())
	if bag != nil {
		text = bag.Scrub(text)
	}
	return fmt.Errorf("%w: %s", ErrAccountTransient, text)
}

func scrubOutboundError(bag *SecretBag, err error) error {
	if err == nil {
		return nil
	}
	text := redactRequestURL(err.Error())
	if bag != nil {
		text = bag.Scrub(text)
	}
	if text == err.Error() {
		return err
	}
	return &scrubbedError{err: err, msg: text}
}

// userinfoPattern removes scheme-less user:password@host fragments.
// A single quote is not a boundary; stopping there would keep the query.
var userinfoPattern = regexp.MustCompile(`[^\s"'<>]+:[^\s"'<>]+@[^\s"'<>]+`)

func redactRequestURL(text string) string {
	for {
		start, length := urlSchemeIndex(text)
		if start < 0 {
			break
		}
		end := start + length
		for end < len(text) && !isURLTerminator(text[end]) {
			end++
		}
		// length is the scheme, so end is always past start. Stop anyway if a
		// future matcher ever reports an empty span.
		if end <= start {
			break
		}
		text = text[:start] + text[end:]
	}
	for {
		loc := userinfoPattern.FindStringIndex(text)
		if loc == nil || loc[1] <= loc[0] {
			return text
		}
		text = text[:loc[0]] + text[loc[1]:]
	}
}

func urlSchemeIndex(text string) (int, int) {
	lower := strings.ToLower(text)
	https := strings.Index(lower, "https://")
	http := strings.Index(lower, "http://")
	switch {
	case https >= 0 && (http < 0 || https <= http):
		return https, len("https://")
	case http >= 0:
		return http, len("http://")
	default:
		return -1, 0
	}
}

// isURLTerminator ends a URL at whitespace, a double quote, or an angle
// bracket. An apostrophe is not a terminator: a path may contain one, and
// stopping there would leave the rest of the query.
func isURLTerminator(b byte) bool {
	switch b {
	case ' ', '"', '<', '>', '\n', '\r', '\t':
		return true
	default:
		return false
	}
}

func (s *loginSession) wipe() {
	s.closed = true
	s.deviceCode = ""
	s.call.bag = nil
	s.call.jar = nil
	s.call.steps = nil
}

func (s *loginSession) public(status string) LoginSession {
	return LoginSession{
		ID:                      s.id,
		Kind:                    s.kind,
		Status:                  status,
		UserCode:                s.userCode,
		VerificationURL:         s.verificationURL,
		VerificationURLComplete: s.verificationURLComplete,
		Interval:                s.interval,
		ExpiresAt:               s.expiresAt,
	}
}

func (r *AccountProviderRuntime) record(err error) {
	if r == nil || r.plugin == nil {
		return
	}
	if breaker := DefaultCatalog().Breaker(); breaker != nil {
		breaker.RecordProbeResult(r.plugin, err == nil)
	}
}

// emit scrubs plugin logs before they reach the process logger.
// site.emitPluginLogs cannot be called from this package. This is the
// login, refresh, and summary path's equivalent: the bag runs first.
func (r *AccountProviderRuntime) emit(ctx context.Context, hook string, step int, bag *SecretBag, logs []LogEntry, dropped int, callErr error) {
	if callErr != nil {
		if annotated, ok := callErr.(*CallError); ok {
			annotated.Detail = bag.Scrub(annotated.Detail)
			if annotated.Err != nil {
				annotated.Err = bag.ScrubError(annotated.Err)
			}
			if len(logs) == 0 {
				logs, dropped = annotated.Logs, annotated.DroppedLogs
			}
		}
	}
	EmitLogs(ctx, r.plugin, hook, step, scrubLogEntries(bag, logs), dropped)
}

func oauthCredential(spec AccountPoolSpec, tokens accountauth.OAuth2Tokens, previous map[string]string) (map[string]string, error) {
	accessName := spec.Credential.AccessTokenField
	if accessName == "" {
		accessName = defaultAccessTokenField
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return nil, errors.New("oauth2: token response missing access_token")
	}
	out := map[string]string{accessName: tokens.AccessToken}
	if name := spec.Credential.RefreshTokenField; name != "" {
		refresh := tokens.RefreshToken
		if refresh == "" && previous != nil {
			refresh = previous[name]
		}
		if refresh != "" {
			out[name] = refresh
		}
	}
	if name := spec.Credential.ExpiresAtField; name != "" && !tokens.ExpiresAt.IsZero() {
		out[name] = strconv.FormatInt(tokens.ExpiresAt.Unix(), 10)
	}
	for _, field := range spec.Credential.Fields {
		if field.Secret || field.Name == spec.Credential.ExpiresAtField {
			continue
		}
		if out[field.Name] != "" || previous == nil || previous[field.Name] == "" {
			continue
		}
		out[field.Name] = previous[field.Name]
	}
	FillExpiresFrom(spec, out)
	return out, nil
}

func mergeCredential(spec AccountPoolSpec, previous, next map[string]string) map[string]string {
	out := map[string]string{}
	for _, field := range spec.Credential.Fields {
		if previous != nil && previous[field.Name] != "" {
			out[field.Name] = previous[field.Name]
		}
	}
	for _, field := range spec.Credential.Fields {
		if next != nil && next[field.Name] != "" {
			out[field.Name] = next[field.Name]
		}
	}
	return out
}

func loginResult(result any) (*AccountLoginResult, error) {
	parsed, ok := result.(*AccountLoginResult)
	if !ok || parsed == nil {
		return nil, errors.New("unexpected login result")
	}
	switch parsed.Status {
	case "pending", "complete":
		return parsed, nil
	default:
		return nil, fmt.Errorf("login status %q is not supported", parsed.Status)
	}
}

func redactProbeBody(bag *SecretBag, contentType string, raw []byte, rules map[string]string) (string, any, error) {
	extracted := NewSecretBag()
	body, payload, err := RedactResponse(contentType, raw, rules, extracted)
	if err != nil {
		return "", nil, err
	}
	if payload != nil {
		payload = extracted.ScrubJSON(bag.ScrubJSON(payload))
		encoded, err := json.Marshal(payload)
		if err != nil {
			return "", nil, ErrSecretRedactLeak
		}
		text := extracted.Scrub(bag.Scrub(string(encoded)))
		if bag.containsNeedle(text) || extracted.containsNeedle(text) {
			return "", nil, ErrSecretRedactLeak
		}
		parsed, err := decodeJSONUseNumber([]byte(text))
		if err != nil || bag.treeHasNeedle(parsed) || extracted.treeHasNeedle(parsed) {
			return "", nil, ErrSecretRedactLeak
		}
		mergeExtracted(bag, extracted)
		if bag.containsNeedle(text) {
			return "", nil, ErrSecretRedactLeak
		}
		return text, parsed, nil
	}
	text := extracted.Scrub(bag.Scrub(string(body)))
	if bag.containsNeedle(text) || extracted.containsNeedle(text) {
		return "", nil, ErrSecretRedactLeak
	}
	mergeExtracted(bag, extracted)
	return text, nil, nil
}

func mergeExtracted(dst, src *SecretBag) {
	if dst == nil || src == nil {
		return
	}
	for _, field := range src.Fields() {
		value, ok := src.Get(field)
		if ok && value != "" {
			dst.Set(field, value)
		}
	}
}

func scrubProbeRequest(bag *SecretBag, req ProbeRequest) ProbeRequest {
	if bag == nil {
		return req
	}
	req.Method = bag.Scrub(req.Method)
	req.Path = bag.Scrub(req.Path)
	req.Headers = scrubStringMapValues(bag, req.Headers)
	req.Query = scrubStringMapValues(bag, req.Query)
	req.Body = bag.ScrubJSON(req.Body)
	return req
}

func scrubStringMapValues(bag *SecretBag, in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[bag.Scrub(key)] = bag.Scrub(value)
	}
	return out
}

func scrubLogEntries(bag *SecretBag, logs []LogEntry) []LogEntry {
	if len(logs) == 0 || bag == nil {
		return logs
	}
	out := make([]LogEntry, len(logs))
	for i, entry := range logs {
		entry.Message = bag.Scrub(entry.Message)
		entry.Level = bag.Scrub(entry.Level)
		if len(entry.Fields) > 0 {
			if scrubbed, ok := bag.ScrubJSON(entry.Fields).(map[string]any); ok {
				entry.Fields = scrubbed
			}
		}
		out[i] = entry
	}
	return out
}

func scrubSummary(bag *SecretBag, parsed AccountSummaryResult) AccountSummary {
	summary := AccountSummary{Status: parsed.Status, Remark: bag.Scrub(parsed.Remark), Label: cleanSummaryLabel(bag, parsed.Label)}
	summary.Entries = make([]ProbeEntry, len(parsed.Entries))
	for i, entry := range parsed.Entries {
		entry.Label = bag.Scrub(entry.Label)
		entry.Kind = bag.Scrub(entry.Kind)
		entry.Name = bag.Scrub(entry.Name)
		entry.Unit = bag.Scrub(entry.Unit)
		entry.ResetAt = bag.Scrub(entry.ResetAt)
		entry.ExpiresAt = bag.Scrub(entry.ExpiresAt)
		summary.Entries[i] = entry
	}
	return summary
}

func rejectSecretInURL(bag *SecretBag, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || bag == nil {
		return errors.New(KindURLRejected + ": url rejected")
	}
	if bag.containsNeedle(parsed.Hostname()) || bag.containsNeedle(parsed.EscapedPath()) || bag.containsNeedle(parsed.Path) {
		return ErrSecretRedactLeak
	}
	return nil
}

func accountRequestHeaders(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	total := 0
	for key, value := range in {
		name := strings.ToLower(strings.TrimSpace(key))
		if name == "" {
			continue
		}
		switch name {
		case "cookie", "host", "content-length", "transfer-encoding", "connection", "proxy-authorization":
			continue
		}
		if strings.HasPrefix(name, "x-forwarded-") {
			continue
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, ErrSecretHeaderInjection
		}
		if len(out) >= maxRequestHeaders {
			return nil, fmt.Errorf("too many request headers")
		}
		total += len(key) + len(value)
		if total > maxRequestHeaderBytes {
			return nil, fmt.Errorf("request headers are too large")
		}
		out[textproto.CanonicalMIMEHeaderKey(key)] = value
	}
	return out, nil
}

func encodeAccountBody(body any) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	var encoded []byte
	switch typed := body.(type) {
	case string:
		encoded = []byte(typed)
	default:
		var err error
		encoded, err = json.Marshal(typed)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) > MaxProbeRequestBody {
		return nil, &CallError{Kind: KindShape, Detail: "request body is too large"}
	}
	return encoded, nil
}

func requestHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

func clampInterval(value *int) int {
	if value == nil || *value < 1 {
		return accountLoginDefaultWait
	}
	if *value > accountLoginMaxWait {
		return accountLoginMaxWait
	}
	return *value
}

func validUnix(value string) bool {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return false
	}
	return strconv.FormatInt(parsed, 10) == value
}

func cloneParams(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	return maps.Clone(in)
}

func credentialKeys(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func newLoginID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func scrubBag(bag *SecretBag, err error) error {
	if bag == nil {
		return err
	}
	return bag.ScrubError(err)
}

type accountRedirectError struct {
	err error
}

func (e *accountRedirectError) Error() string {
	if e == nil || e.err == nil {
		return "redirect left the site origin"
	}
	return e.err.Error()
}

func (e *accountRedirectError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}
