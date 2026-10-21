package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/credential"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

const pluginAccountSiteType = "acme.example/pool"

func TestPluginAccountResolveRefreshAndReauth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	spec := testPluginAccountSpec("ordered", 300)
	far := "4102444800"
	if accountCredentialNeedsRefresh(spec, map[string]string{"expires_at": far}, now) {
		t.Fatal("token outside the lead window was refreshed")
	}
	if accountCredentialNeedsRefresh(spec, map[string]string{"expires_at": strconvUnix(now.Add(300 * time.Second))}, now) {
		t.Fatal("token exactly leadSeconds away was refreshed")
	}
	if !accountCredentialNeedsRefresh(spec, map[string]string{"expires_at": strconvUnix(now.Add(299 * time.Second))}, now) {
		t.Fatal("token inside the lead window was not refreshed")
	}
	if !accountCredentialNeedsRefresh(spec, map[string]string{"expires_at": "soon"}, now) {
		t.Fatal("unparsable expiry was not refreshed")
	}
	if accountCredentialNeedsRefresh(spec, map[string]string{"expires_at": "4102444800000"}, now) {
		t.Fatal("millisecond expiry in the far future was refreshed")
	}
	none := spec
	none.Refresh.Kind = "none"
	if accountCredentialNeedsRefresh(none, map[string]string{"expires_at": "1"}, now) {
		t.Fatal("refresh kind none still refreshed")
	}
	noExpiry := spec
	noExpiry.Credential.ExpiresAtField = ""
	if accountCredentialNeedsRefresh(noExpiry, map[string]string{"expires_at": "1"}, now) {
		t.Fatal("pool without an expiry field still refreshed")
	}

	registerPluginAccountSite(t, pluginAccountSiteOptions{})
	handler := Handler{}
	fields := map[string]any{
		"access_token":  "token-live",
		"refresh_token": "refresh-live",
		"expires_at":    int64(4102444800),
		"region":        "cn",
		"domain":        "tenant.example",
	}
	resolved, failure := resolvePluginAccount(t, handler, fields)
	if failure != nil {
		t.Fatalf("resolve: %v", failure.Err)
	}
	if resolved.Key != "token-live" || resolved.BaseURL != "http://127.0.0.1/cn" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if resolved.HeaderVars["cred.access_token"] != "token-live" || resolved.HeaderVars["cred.domain"] != "tenant.example" || resolved.HeaderVars["cred.region"] != "cn" || resolved.HeaderVars["cred.expires_at"] != "4102444800" {
		t.Fatalf("header vars = %#v", resolved.HeaderVars)
	}
	if _, ok := resolved.HeaderVars["cred.refresh_token"]; ok {
		t.Fatalf("refresh token leaked into header vars: %#v", resolved.HeaderVars)
	}

	defaultRegion := fields
	delete(defaultRegion, "region")
	resolved, failure = resolvePluginAccount(t, handler, defaultRegion)
	if failure != nil || resolved.BaseURL != "http://127.0.0.1/intl" {
		t.Fatalf("default region base = %q failure=%v", resolved.BaseURL, failure)
	}

	resolved, failure = resolvePluginAccount(t, handler, map[string]any{
		"access_token": "token-live",
		"expires_at":   int64(4102444800),
		"realm":        "cn",
		"domain":       "tenant.example",
	})
	if failure != nil || resolved.BaseURL != "http://127.0.0.1/cn" {
		t.Fatalf("realm cn base = %q failure=%v", resolved.BaseURL, failure)
	}
	resolved, failure = resolvePluginAccount(t, handler, map[string]any{
		"access_token": "token-live",
		"expires_at":   int64(4102444800),
		"realm":        "mars",
	})
	if failure != nil || resolved.BaseURL != "http://127.0.0.1/intl" {
		t.Fatalf("unknown realm base = %q failure=%v", resolved.BaseURL, failure)
	}

	refresher := &fakePluginRefresher{fields: map[string]string{
		"access_token": "token-new",
		"region":       "intl",
		"domain":       "refreshed.example",
		"expires_at":   far,
	}}
	handler.pluginAccounts = refresher
	resolved, failure = resolvePluginAccount(t, handler, map[string]any{
		"access_token":  "token-old",
		"refresh_token": "refresh-old",
		"expires_at":    int64(1),
		"region":        "cn",
		"domain":        "old.example",
	})
	if failure != nil {
		t.Fatalf("refresh resolve: %v", failure.Err)
	}
	if refresher.calls != 1 || refresher.gotSite != pluginAccountSiteType || refresher.gotSiteID != pluginResolveSiteID || refresher.gotCredentialID != pluginResolveCredentialID || refresher.gotFields["access_token"] != "token-old" {
		t.Fatalf("refresh call site=%q siteID=%s credentialID=%s fields=%#v calls=%d", refresher.gotSite, refresher.gotSiteID, refresher.gotCredentialID, refresher.gotFields, refresher.calls)
	}
	if resolved.Key != "token-new" || resolved.BaseURL != "http://127.0.0.1/intl" || resolved.HeaderVars["cred.domain"] != "refreshed.example" {
		t.Fatalf("refreshed credential = %+v", resolved)
	}

	refresher.err = fmt.Errorf("refresh: %w", accountauth.ErrReauthorizationRequired)
	_, failure = resolvePluginAccount(t, handler, map[string]any{"access_token": "token-old", "expires_at": "nope"})
	if failure == nil || failure.ErrorType != "plugin_account_refresh_failed" || !failure.TryNextCredential || !failure.MarkReauth {
		t.Fatalf("reauth failure = %+v", failure)
	}
	refresher.err = errors.New("temporary")
	_, failure = resolvePluginAccount(t, handler, map[string]any{"access_token": "token-old", "expires_at": int64(1)})
	if failure == nil || !failure.TryNextCredential || failure.MarkReauth {
		t.Fatalf("temporary refresh failure = %+v", failure)
	}

	handler.pluginAccounts = nil
	_, failure = resolvePluginAccount(t, handler, map[string]any{"access_token": "token-old", "expires_at": int64(1)})
	if failure == nil || failure.ErrorType != "plugin_account_refresh_failed" || !strings.Contains(failure.Err.Error(), "plugin account refresher is not configured") {
		t.Fatalf("missing refresher = %+v", failure)
	}

	unknown := fields
	unknown["region"] = "eu"
	unknown["expires_at"] = int64(4102444800)
	_, failure = resolvePluginAccount(t, handler, unknown)
	if failure == nil || failure.ErrorType != "plugin_account_credential_invalid" || !failure.TryNextCredential || failure.MarkReauth {
		t.Fatalf("unknown region = %+v", failure)
	}

	registerPluginAccountSite(t, pluginAccountSiteOptions{baseFrom: "omit"})
	resolved, failure = resolvePluginAccount(t, handler, map[string]any{"access_token": "token-live", "expires_at": int64(4102444800)})
	if failure != nil || resolved.BaseURL != "" || resolved.Key != "token-live" {
		t.Fatalf("empty baseURLFrom resolved=%+v failure=%v", resolved, failure)
	}

	registerPluginAccountSite(t, pluginAccountSiteOptions{refreshKind: "none"})
	resolved, failure = resolvePluginAccount(t, handler, map[string]any{"access_token": "token-stale", "expires_at": int64(1)})
	if failure != nil || resolved.Key != "token-stale" {
		t.Fatalf("kind none resolved=%+v failure=%v", resolved, failure)
	}
}

func TestCredentialResolverForKeepsBuiltinAccounts(t *testing.T) {
	registerPluginAccountSite(t, pluginAccountSiteOptions{})
	stub := &stubGatewayOAuth{
		grokToken: "grok-access",
	}
	handler := Handler{oauth: stub}
	credentialID := uuid.New()
	grokType := "grok_sso:" + uuid.NewString()
	resolved, failure := credentialResolverFor("grok", grokType).Resolve(context.Background(), handler, routeengine.Candidate{}, store.GatewayCredential{Credential: store.SiteCredential{ID: credentialID}}, "not-json", "meta-account")
	if failure != nil || resolved.Key != "grok-access" || resolved.AccountID != "meta-account" || resolved.HeaderVars != nil {
		t.Fatalf("grok resolved=%+v failure=%v", resolved, failure)
	}
	if credentialResolverFor("openai", "api_key") != nil {
		t.Fatal("api_key resolver was installed")
	}
	if credentialResolverFor(pluginAccountSiteType, grokType) != nil {
		t.Fatal("builtin credential type was treated as this plugin pool")
	}
	if credentialResolverFor(pluginAccountSiteType, store.NewAccountCredentialType("other.example/pool", uuid.New())) != nil {
		t.Fatal("another site's account credential resolved against this pool")
	}
	if credentialResolverForSite(pluginAccountSiteType) != nil {
		t.Fatal("site-only resolver selected a plugin account pool")
	}
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/keys", DisplayName: "Keys"},
		PluginID:   "acme.example",
		Enabled:    true,
	}}); err != nil {
		t.Fatal(err)
	}
	if credentialResolverFor("acme.example/keys", store.NewAccountCredentialType("acme.example/keys", uuid.New())) != nil {
		t.Fatal("api_key plugin accepted an account credential")
	}

	exhausted := []store.GatewayCredential{{
		Credential: store.SiteCredential{ID: uuid.New(), CredentialType: grokType},
		State:      store.SiteAPIKeyState{Usage: pluginUsage(exhaustedEntry(time.Now().Add(time.Hour)))},
	}}
	if _, _, ok := arrangePluginAccountCredentials("grok", exhausted, time.Now()); ok {
		t.Fatal("grok credentials were rearranged by the plugin pool")
	}
	if !accountPoolCredentialFailover(grokType) || !accountPoolCredentialFailover(store.NewAccountCredentialType(pluginAccountSiteType, uuid.New())) || accountPoolCredentialFailover("api_key") {
		t.Fatal("account-pool transport failover did not follow account credential types")
	}
	if accountPoolSyncProvider("grok") != "grok" || accountPoolSyncProvider(pluginAccountSiteType) != pluginAccountSiteType {
		t.Fatal("account sync provider label changed")
	}
}

func TestPluginAccountQuotaWeightAndExhaustion(t *testing.T) {
	registerPluginAccountSite(t, pluginAccountSiteOptions{selection: "quota_weighted"})
	now := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	soon := now.Add(48 * time.Hour)
	later := now.Add(10 * 24 * time.Hour)
	past := now.Add(-time.Hour)
	low := pluginAccountCredential(t, "low", 100, pluginUsage(entryRemaining(1, nil)))
	high := pluginAccountCredential(t, "high", 1, pluginUsage(entryRemaining(40, nil)))
	unlimited := pluginAccountCredential(t, "unlimited", 1, pluginUsage(site.QuotaProbeEntry{Label: "plan", Unlimited: true, Remaining: floatPtr(0), ResetAt: timePtr(soon.Format(time.RFC3339Nano))}))
	unknown := pluginAccountCredential(t, "unknown", 9, nil)
	ordered, cooled, ok := arrangePluginAccountCredentials(pluginAccountSiteType, []store.GatewayCredential{low, unknown, high, unlimited}, now)
	if !ok || len(cooled) != 0 || len(ordered) != 4 {
		t.Fatalf("ordered=%d cooled=%d ok=%v", len(ordered), len(cooled), ok)
	}
	if ordered[0].Credential.DisplayName != "unlimited" || ordered[1].Credential.DisplayName != "high" || ordered[2].Credential.DisplayName != "low" || ordered[3].Credential.DisplayName != "unknown" {
		t.Fatalf("quota order = %s %s %s %s", ordered[0].Credential.DisplayName, ordered[1].Credential.DisplayName, ordered[2].Credential.DisplayName, ordered[3].Credential.DisplayName)
	}

	registerPluginAccountSite(t, pluginAccountSiteOptions{selection: "ordered"})
	kept := pluginAccountCredential(t, "kept", 1, pluginUsage(entryRemaining(0, timePtr(past.Format(time.RFC3339Nano))), site.QuotaProbeEntry{Label: "open", Unlimited: true, Remaining: floatPtr(0)}))
	dropped := pluginAccountCredential(t, "dropped", 1000, pluginUsage(entryRemaining(3, nil), exhaustedEntry(soon), exhaustedEntry(later)))
	apiKey := pluginAccountCredential(t, "api", 1, pluginUsage(exhaustedEntry(soon)))
	apiKey.Credential.CredentialType = "api_key"
	ordered, cooled, ok = arrangePluginAccountCredentials(pluginAccountSiteType, []store.GatewayCredential{dropped, kept, apiKey}, now)
	if !ok || len(ordered) != 2 || ordered[0].Credential.DisplayName != "kept" || ordered[1].Credential.DisplayName != "api" {
		t.Fatalf("ordered selection = %#v", namesOf(ordered))
	}
	if len(cooled) != 1 || cooled[0].CredentialID != dropped.Credential.ID || !cooled[0].ResetAt.Equal(later) {
		t.Fatalf("cooled = %+v", cooled)
	}
	input, ok := quotaExhaustedCooldownInput(uuid.New(), cooled[0].CredentialID, cooled[0].ResetAt, now)
	if !ok || input.Scope != "credential" || input.Source != "gateway" || input.Reason != "quota_exhausted" || input.Duration != pluginQuotaExhaustedMax {
		t.Fatalf("cooldown = %+v ok=%v", input, ok)
	}
	if input.Metadata["reset_at"] != later.UTC().Format(time.RFC3339Nano) || input.SiteCredentialID == nil || *input.SiteCredentialID != dropped.Credential.ID {
		t.Fatalf("cooldown metadata = %#v", input.Metadata)
	}
	short, ok := quotaExhaustedCooldownInput(uuid.New(), dropped.Credential.ID, soon, now)
	if !ok || short.Duration != 48*time.Hour {
		t.Fatalf("short cooldown = %s", short.Duration)
	}
	if _, ok := quotaExhaustedCooldownInput(uuid.New(), uuid.Nil, later, now); ok {
		t.Fatal("nil credential id produced a cooldown")
	}
	if _, ok := quotaExhaustedCooldownInput(uuid.New(), dropped.Credential.ID, past, now); ok {
		t.Fatal("past reset produced a cooldown")
	}

	handler := Handler{}
	if _, err := handler.preparePluginAccountCredentials(context.Background(), routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "grok"}}, []store.GatewayCredential{dropped}); err != nil {
		t.Fatalf("grok prepare: %v", err)
	}
	_, err := handler.preparePluginAccountCredentials(context.Background(), routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: pluginAccountSiteType}}, []store.GatewayCredential{dropped})
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("exhausted pool err = %v", err)
	}
}

func TestPluginUpstreamStreamOnlyForcesChatAggregation(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)
	grok := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "grok-4"}}
	grok.Site.SiteType = "grok"
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, grok)
	before := map[string]any{}
	for key, value := range spec.RequestParams.Forced {
		before[key] = value
	}
	applyPluginUpstreamStreamOnly(&spec, "grok")
	if len(spec.RequestParams.Forced) != len(before) {
		t.Fatalf("grok forced params = %#v, before %#v", spec.RequestParams.Forced, before)
	}
	for key, value := range before {
		if spec.RequestParams.Forced[key] != value {
			t.Fatalf("grok forced %s = %#v, want %#v", key, spec.RequestParams.Forced[key], value)
		}
	}

	registerPluginAccountSite(t, pluginAccountSiteOptions{})
	plain := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: pluginAccountSiteType}, Model: routeengine.CandidateModel{UpstreamName: "plugin-model"}}
	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		pluginAccountSiteType: runtimeSpecJSON("openai_chat", "https://api.example.com", "/v1/chat/completions", ""),
	}); err != nil {
		t.Fatal(err)
	}
	spec = effectiveProtocolSpec(canonicalProtocolOpenAIChat, plain)
	if _, ok := spec.RequestParams.Forced["stream"]; ok {
		t.Fatalf("plugin without the flag forced stream: %#v", spec.RequestParams.Forced)
	}

	registerPluginAccountSite(t, pluginAccountSiteOptions{streamOnly: true})
	spec = effectiveProtocolSpec(canonicalProtocolOpenAIChat, plain)
	if spec.RequestParams.Forced["stream"] != true {
		t.Fatalf("plugin forced = %#v", spec.RequestParams.Forced)
	}
	request := gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Payload: map[string]any{"model": "plugin-model", "messages": []any{}}}
	protocol := newOpenAIChatProtocolAdapter(request, plain)
	if !protocolUsesRawBufferedResponse(protocol) {
		t.Fatal("non-streaming plugin chat did not keep the raw SSE body")
	}
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	sse := []byte("data: {\"id\":\"c1\",\"model\":\"plugin-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")
	buffered, err := protocol.TransformBufferedResponse(http.StatusOK, headers, sse)
	if err != nil || !strings.Contains(string(buffered.Body), `"content":"ok"`) || strings.Contains(string(buffered.Body), "data:") {
		t.Fatalf("aggregated = %s err=%v", buffered.Body, err)
	}
	streaming := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true}, plain)
	if protocolUsesRawBufferedResponse(streaming) {
		t.Fatal("downstream stream was aggregated")
	}
	passed, err := streaming.TransformBufferedResponse(http.StatusOK, headers, sse)
	if err != nil || string(passed.Body) != string(sse) {
		t.Fatalf("streaming body = %s err=%v", passed.Body, err)
	}
}

func TestPluginAccountPoolFailoverRefreshAndDiagnostic(t *testing.T) {
	transport := &pluginAccountTransport{}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true})
	low := newPluginForwardAccount("token-low", "intl", 100, pluginUsage(entryRemaining(1, nil)))
	high := newPluginForwardAccount("token-high", "intl", 1, pluginUsage(entryRemaining(40, nil)))
	handler, candidate, _ := pluginAccountForwardHandler(t, client, nil, low, high)
	transport.fail(errors.New("dial failed"))
	transport.respond(http.StatusOK, `{"ok":true}`)
	result := forwardPluginAccount(handler, candidate, false)
	if !result.success || !sameStrings(transport.authorizations(), []string{"Bearer token-low", "Bearer token-high"}) {
		t.Fatalf("ordered failover success=%v auth=%v err=%s", result.success, transport.authorizations(), result.errorMessage)
	}

	transport.reset()
	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true, selection: "quota_weighted"})
	handler, candidate, _ = pluginAccountForwardHandler(t, client, nil, low, high)
	transport.fail(errors.New("dial failed"))
	transport.respond(http.StatusOK, `{"ok":true}`)
	result = forwardPluginAccount(handler, candidate, false)
	if !result.success || !sameStrings(transport.authorizations(), []string{"Bearer token-high", "Bearer token-low"}) {
		t.Fatalf("weighted failover success=%v auth=%v err=%s", result.success, transport.authorizations(), result.errorMessage)
	}

	transport.reset()
	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true})
	exhausted := newPluginForwardAccount("token-empty", "intl", 1000, pluginUsage(exhaustedEntry(time.Now().Add(24*time.Hour))))
	healthy := newPluginForwardAccount("token-healthy", "cn", 1, pluginUsage(entryRemaining(5, nil)))
	handler, candidate, _ = pluginAccountForwardHandler(t, client, nil, exhausted, healthy)
	transport.respond(http.StatusOK, `{"ok":true}`)
	result = forwardPluginAccount(handler, candidate, false)
	if !result.success || !sameStrings(transport.authorizations(), []string{"Bearer token-healthy"}) || transport.domains()[0] != "tenant.example" {
		t.Fatalf("exhausted skip auth=%v domain=%v err=%s", transport.authorizations(), transport.domains(), result.errorMessage)
	}

	transport.reset()
	handler, candidate, _ = pluginAccountForwardHandler(t, client, nil, exhausted, newPluginForwardAccount("token-also-empty", "intl", 50, pluginUsage(exhaustedEntry(time.Now().Add(time.Hour)))))
	result = forwardPluginAccount(handler, candidate, false)
	if result.success || result.errorType != "upstream_credential_unavailable" || len(transport.authorizations()) != 0 {
		t.Fatalf("all exhausted success=%v type=%s auth=%v", result.success, result.errorType, transport.authorizations())
	}

	transport.reset()
	registerPluginAccountSite(t, pluginAccountSiteOptions{})
	firstKey := pluginForwardSecret("sk-first")
	secondKey := pluginForwardSecret("sk-second")
	firstKey.credentialType = "api_key"
	secondKey.credentialType = "api_key"
	handler, candidate, _ = pluginAccountForwardHandler(t, client, nil, firstKey, secondKey)
	transport.fail(errors.New("dial failed"))
	transport.fail(errors.New("dial failed"))
	result = forwardPluginAccount(handler, candidate, false)
	if result.success || result.errorType != "upstream_transport_error" || !sameStrings(transport.authorizations(), []string{"Bearer sk-first"}) {
		t.Fatalf("api key failover success=%v type=%s auth=%v", result.success, result.errorType, transport.authorizations())
	}

	transport.reset()
	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true})
	stale := newPluginForwardAccount("token-stale", "cn", 10, nil)
	stale.expires = 1
	fresh := newPluginForwardAccount("token-fresh", "intl", 10, nil)
	refresher := &fakePluginRefresher{fields: map[string]string{"access_token": "token-renewed", "region": "intl", "domain": "renewed.example", "expires_at": "4102444800"}}
	handler, candidate, _ = pluginAccountForwardHandler(t, client, refresher, stale)
	transport.respond(http.StatusOK, `{"ok":true}`)
	result = forwardPluginAccount(handler, candidate, false)
	if !result.success || refresher.calls != 1 || !sameStrings(transport.authorizations(), []string{"Bearer token-renewed"}) {
		t.Fatalf("refresh success=%v calls=%d auth=%v err=%s", result.success, refresher.calls, transport.authorizations(), result.errorMessage)
	}

	transport.reset()
	refresher.err = fmt.Errorf("grant: %w", accountauth.ErrReauthorizationRequired)
	handler, candidate, _ = pluginAccountForwardHandler(t, client, refresher, stale, fresh)
	transport.respond(http.StatusOK, `{"ok":true}`)
	result = forwardPluginAccount(handler, candidate, false)
	if !result.success || !sameStrings(transport.authorizations(), []string{"Bearer token-fresh"}) {
		t.Fatalf("reauth failover success=%v auth=%v err=%s", result.success, transport.authorizations(), result.errorMessage)
	}

	transport.reset()
	denied := newPluginForwardAccount("token-denied", "intl", 10, nil)
	allowed := newPluginForwardAccount("token-allowed", "intl", 9, nil)
	handler, candidate, _ = pluginAccountForwardHandler(t, client, nil, denied, allowed)
	transport.respond(http.StatusUnauthorized, `{"error":"no"}`)
	transport.respond(http.StatusOK, `{"ok":true}`)
	result = forwardPluginAccount(handler, candidate, false)
	if !result.success || !sameStrings(transport.authorizations(), []string{"Bearer token-denied", "Bearer token-allowed"}) {
		t.Fatalf("401 failover success=%v auth=%v err=%s", result.success, transport.authorizations(), result.errorMessage)
	}

	transport.reset()
	transport.respond(http.StatusOK, `{"ok":true}`)
	live := newPluginForwardAccount("token-diag", "cn", 10, nil)
	handler, candidate, creds := pluginAccountForwardHandler(t, client, nil, live)
	selected := &creds[0]
	diagnostic := handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/plugin-account", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &statusGatewayProtocol{}, selected)
	if !diagnostic.success || !sameStrings(transport.authorizations(), []string{"Bearer token-diag"}) || transport.requestedURLs()[0] == "" || !strings.Contains(transport.requestedURLs()[0], "/cn/") {
		t.Fatalf("diagnostic success=%v auth=%v url=%v err=%s", diagnostic.success, transport.authorizations(), transport.requestedURLs(), diagnostic.errorMessage)
	}
}

func TestPluginAccountPoolAggregatesForcedChatSSE(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"model\":\"plugin-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n")
	}))
	defer server.Close()

	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)
	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true, streamOnly: true, regions: []jsplugin.SiteProviderRegion{{
		ID: "intl", BaseURL: server.URL,
	}}})
	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		pluginAccountSiteType: runtimeSpecJSON("openai_chat", "https://api.example.com", "/v1/chat/completions", ""),
	}); err != nil {
		t.Fatal(err)
	}
	account := newPluginForwardAccount("token-stream", "intl", 10, nil)
	handler, candidate, _ := pluginAccountForwardHandler(t, &http.Client{Timeout: 5 * time.Second}, nil, account)
	handler.clients = httpclient.NewManager(nil)
	candidate.Site.BaseURL = "http://127.0.0.1/dead"
	request := gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload:        map[string]any{"model": "plugin-model", "messages": []any{}},
	}
	protocol := newOpenAIChatProtocolAdapter(request, candidate)
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-plugin-sse", 1, uuid.New(), uuid.New(), candidate, request, nil, protocol)
	if !result.success || !strings.Contains(string(result.body), `"object":"chat.completion"`) || !strings.Contains(string(result.body), `"content":"ok"`) {
		t.Fatalf("aggregated success=%v type=%s body=%s err=%s", result.success, result.errorType, result.body, result.errorMessage)
	}
	if !strings.Contains(string(body), `"stream":true`) || !strings.Contains(result.upstreamURL, server.URL) {
		t.Fatalf("upstream body=%s url=%s", body, result.upstreamURL)
	}
}

type pluginAccountSiteOptions struct {
	selection   string
	refreshKind string
	baseFrom    string
	streamOnly  bool
	headers     bool
	regions     []jsplugin.SiteProviderRegion
}

func registerPluginAccountSite(t *testing.T, opts pluginAccountSiteOptions) {
	t.Helper()
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	if opts.selection == "" {
		opts.selection = "ordered"
	}
	if opts.refreshKind == "" {
		opts.refreshKind = "custom_steps"
	}
	baseFrom := "region"
	if opts.baseFrom == "omit" {
		baseFrom = ""
	}
	regions := opts.regions
	if regions == nil {
		regions = []jsplugin.SiteProviderRegion{
			{ID: "cn", BaseURL: "http://127.0.0.1/cn"},
			{ID: "intl", BaseURL: "http://127.0.0.1/intl"},
		}
	}
	lead := 300
	spec := jsplugin.AccountPoolSpec{
		Credential: jsplugin.SiteProviderCredential{
			Model:            "account_pool",
			AccessTokenField: "access_token",
			ExpiresAtField:   "expires_at",
			Fields: []jsplugin.SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "refresh_token", Secret: true},
				{Name: "expires_at", Type: "unix"},
				{Name: "region"},
				{Name: "domain"},
			},
		},
		Regions:       regions,
		DefaultRegion: "intl",
		Refresh:       jsplugin.SiteProviderRefresh{Kind: opts.refreshKind, LeadSeconds: &lead},
		Selection:     opts.selection,
		Request:       jsplugin.SiteProviderRequest{BaseURLFrom: baseFrom, UpstreamStreamOnly: opts.streamOnly},
	}
	if opts.refreshKind == "none" {
		spec.Refresh.LeadSeconds = nil
	}
	var headers map[string]jsplugin.HeaderSpec
	if opts.headers {
		headers = jsplugin.HeaderMap(map[string]string{
			"Authorization": "Bearer {{cred.access_token}}",
			"X-Domain":      "{{cred.domain}}",
		})
	}
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: pluginAccountSiteType, DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"127.0.0.1"},
		Request: site.PluginSiteRequest{
			Headers:            headers,
			UpstreamStreamOnly: opts.streamOnly,
			BaseURLFrom:        baseFrom,
		},
		Pool: &spec,
	}}); err != nil {
		t.Fatal(err)
	}
}

func testPluginAccountSpec(selection string, lead int) jsplugin.AccountPoolSpec {
	return jsplugin.AccountPoolSpec{
		Credential: jsplugin.SiteProviderCredential{ExpiresAtField: "expires_at", AccessTokenField: "access_token"},
		Refresh:    jsplugin.SiteProviderRefresh{Kind: "custom_steps", LeadSeconds: &lead},
		Selection:  selection,
	}
}

var (
	pluginResolveSiteID       = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	pluginResolveCredentialID = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

func resolvePluginAccount(t *testing.T, handler Handler, fields map[string]any) (resolvedUpstreamCredential, *credentialResolveFailure) {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{ID: pluginResolveSiteID, SiteType: pluginAccountSiteType}}
	credential := store.GatewayCredential{Credential: store.SiteCredential{ID: pluginResolveCredentialID}}
	return (pluginAccountCredentialResolver{}).Resolve(context.Background(), handler, candidate, credential, string(raw), "acct")
}

type fakePluginRefresher struct {
	calls           int
	gotSite         string
	gotSiteID       uuid.UUID
	gotCredentialID uuid.UUID
	gotFields       map[string]string
	fields          map[string]string
	err             error
}

func (f *fakePluginRefresher) Refresh(_ context.Context, siteID, credentialID uuid.UUID, siteType string, fields map[string]string) (map[string]string, error) {
	f.calls++
	f.gotSite = siteType
	f.gotSiteID = siteID
	f.gotCredentialID = credentialID
	f.gotFields = fields
	if f.err != nil {
		return nil, f.err
	}
	return f.fields, nil
}

func pluginAccountCredential(t *testing.T, name string, remain int64, usage store.JSON) store.GatewayCredential {
	t.Helper()
	id := uuid.New()
	return store.GatewayCredential{
		Credential: store.SiteCredential{
			ID:              id,
			DisplayName:     name,
			CredentialType:  store.NewAccountCredentialType(pluginAccountSiteType, id),
			RoutingPriority: 1,
		},
		State: store.SiteAPIKeyState{
			SiteCredentialID: id,
			Enabled:          true,
			RemainQuota:      sql.NullInt64{Int64: remain, Valid: true},
			Usage:            usage,
		},
	}
}

func pluginUsage(entries ...site.QuotaProbeEntry) store.JSON {
	raw, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		panic(err)
	}
	return raw
}

func entryRemaining(remaining float64, reset *string) site.QuotaProbeEntry {
	return site.QuotaProbeEntry{Label: "plan", Remaining: floatPtr(remaining), ResetAt: reset}
}

func exhaustedEntry(reset time.Time) site.QuotaProbeEntry {
	return entryRemaining(0, timePtr(reset.UTC().Format(time.RFC3339Nano)))
}

func floatPtr(value float64) *float64 { return &value }

func timePtr(value string) *string { return &value }

func namesOf(items []store.GatewayCredential) []string {
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.Credential.DisplayName
	}
	return names
}

func strconvUnix(value time.Time) string {
	return fmt.Sprintf("%d", value.Unix())
}

type pluginForwardAccount struct {
	token          string
	region         string
	domain         string
	expires        int64
	remain         int64
	usage          store.JSON
	secret         string
	credentialType string
}

func newPluginForwardAccount(token, region string, remain int64, usage store.JSON) pluginForwardAccount {
	return pluginForwardAccount{token: token, region: region, domain: "tenant.example", expires: 4102444800, remain: remain, usage: usage}
}

func pluginForwardSecret(secret string) pluginForwardAccount {
	return pluginForwardAccount{secret: secret, remain: 10}
}

func pluginAccountForwardHandler(t *testing.T, client *http.Client, refresher PluginAccountRefresher, accounts ...pluginForwardAccount) (Handler, routeengine.Candidate, []store.GatewayCredential) {
	t.Helper()
	secrets := credential.NewService("test-master")
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentials := make([]store.SiteCredential, 0, len(accounts))
	states := make([]store.SiteAPIKeyState, 0, len(accounts))
	bindings := make([]store.SiteAPIKeyModel, 0, len(accounts))
	for i, account := range accounts {
		id := uuid.New()
		credentialType := account.credentialType
		if credentialType == "" {
			credentialType = store.NewAccountCredentialType(pluginAccountSiteType, id)
		}
		plain := account.secret
		if plain == "" {
			raw, err := json.Marshal(map[string]any{
				"access_token":  account.token,
				"refresh_token": "refresh-" + account.token,
				"expires_at":    account.expires,
				"region":        account.region,
				"domain":        account.domain,
			})
			if err != nil {
				t.Fatal(err)
			}
			plain = string(raw)
		}
		encrypted, masked, err := secrets.Encrypt(plain)
		if err != nil {
			t.Fatal(err)
		}
		item := store.SiteCredential{
			ID:              id,
			SiteID:          siteID,
			CredentialType:  credentialType,
			EncryptedSecret: encrypted,
			MaskedSecret:    masked,
			Meta:            store.JSON(`{"enabled":true}`),
			RoutingPriority: 1,
			CreatedAt:       time.Unix(int64(10+i), 0),
		}
		state := store.SiteAPIKeyState{
			SiteCredentialID: id,
			SiteID:           siteID,
			Enabled:          true,
			SyncStatus:       "synced",
			RemainQuota:      sql.NullInt64{Int64: account.remain, Valid: true},
			Usage:            account.usage,
		}
		credentials = append(credentials, item)
		states = append(states, state)
		bindings = append(bindings, store.SiteAPIKeyModel{
			SiteID:            siteID,
			SiteCredentialID:  id,
			SiteModelID:       uuid.NullUUID{UUID: siteModelID, Valid: true},
			UpstreamModelName: "plugin-model",
			Available:         true,
			Enabled:           true,
			UpdatedAt:         time.Unix(20, 0),
		})
	}
	selected := make([]store.GatewayCredential, len(credentials))
	for i := range credentials {
		selected[i] = store.GatewayCredential{Credential: credentials[i], State: states[i]}
	}
	db := pluginAccountGorm(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{ID: siteID, SiteType: pluginAccountSiteType, BaseURL: "http://127.0.0.1/site"}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteAPIKeyModel:
			*dest = bindings
			tx.Statement.RowsAffected = int64(len(bindings))
		case *[]store.SiteCredential:
			*dest = credentials
			tx.Statement.RowsAffected = int64(len(credentials))
		case *[]store.SiteAPIKeyState:
			*dest = states
			tx.Statement.RowsAffected = int64(len(states))
		case *[]store.RouteCooldown, *[]store.SiteModelPricing:
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
	st := gatewayStoreWithGorm(t, db)
	handler := Handler{
		logger:         gatewayDiscardLogger(),
		db:             st,
		router:         routeengine.NewService(st),
		credentials:    secrets,
		recorder:       Recorder{},
		httpClient:     client,
		clients:        httpclient.NewManager(nil),
		pluginAccounts: refresher,
	}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID, Name: "Acme", SiteType: pluginAccountSiteType, BaseURL: "http://127.0.0.1/site"},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "plugin-model"},
	}
	return handler, candidate, selected
}

func forwardPluginAccount(handler Handler, candidate routeengine.Candidate, stream bool) gatewayAttemptResult {
	return handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-plugin-account", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: stream}, nil, &statusGatewayProtocol{})
}

func pluginAccountGorm(t *testing.T, callback func(*gorm.DB)) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=127.0.0.1 port=1 user=xlyra dbname=xlyra sslmode=disable connect_timeout=1",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open plugin account gorm: %v", err)
	}
	gatewayReplaceQueryCallback(t, db, callback)
	return db
}

type statusGatewayProtocol struct {
	testGatewayProtocol
}

func (p *statusGatewayProtocol) TransformBufferedResponse(status int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	return gatewayBufferedResponse{StatusCode: status, ContentType: headers.Get("Content-Type"), Body: append([]byte(nil), body...)}, nil
}

type pluginAccountResponse struct {
	status int
	body   string
	err    error
}

type pluginAccountTransport struct {
	mu       sync.Mutex
	script   []pluginAccountResponse
	auth     []string
	domain   []string
	seenURLs []string
	next     int
}

func (p *pluginAccountTransport) fail(err error) {
	p.script = append(p.script, pluginAccountResponse{err: err})
}

func (p *pluginAccountTransport) respond(status int, body string) {
	p.script = append(p.script, pluginAccountResponse{status: status, body: body})
}

func (p *pluginAccountTransport) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.script = nil
	p.auth = nil
	p.domain = nil
	p.seenURLs = nil
	p.next = 0
}

func (p *pluginAccountTransport) authorizations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.auth...)
}

func (p *pluginAccountTransport) domains() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.domain...)
}

func (p *pluginAccountTransport) requestedURLs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seenURLs...)
}

func (p *pluginAccountTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.auth = append(p.auth, r.Header.Get("Authorization"))
	p.domain = append(p.domain, r.Header.Get("X-Domain"))
	if r.URL != nil {
		p.seenURLs = append(p.seenURLs, r.URL.String())
	}
	if p.next >= len(p.script) {
		return nil, errors.New("unexpected upstream call")
	}
	step := p.script[p.next]
	p.next++
	if step.err != nil {
		return nil, step.err
	}
	return &http.Response{
		StatusCode:    step.status,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(step.body)),
		ContentLength: int64(len(step.body)),
		Request:       r,
	}, nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
