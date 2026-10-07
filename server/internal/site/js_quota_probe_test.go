package site

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func fixtureResponseMatches(canonical string, response jsplugin.FixtureResponse, requestPath string) bool {
	if response.Match.Path != "" {
		resolved, err := jsplugin.ResolveProbeURL(canonical, response.Match.Path, nil)
		if err == nil {
			parsed, err := url.Parse(resolved)
			if err == nil && parsed.Path == requestPath {
				return true
			}
		}
	}
	if response.Match.PathPrefix != "" && strings.Contains(requestPath, response.Match.PathPrefix) {
		return true
	}
	return false
}

func TestJSProbeNewAPIBillingFallback(t *testing.T) {
	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatalf("load builtins: %v", err)
	}
	plugin := mustBuiltin(t, registry, QuotaProbeTypeNewAPI)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/billing/subscription":
			_, _ = w.Write([]byte(`{"hard_limit_usd": 10}`))
		case "/dashboard/billing/usage":
			_, _ = w.Write([]byte(`{"total_usage": 100}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeNewAPI, server.URL, "sk-test")
	if result.Status != "ok" {
		t.Fatalf("result = %+v", result)
	}
	if err := plugin.SelfTest(context.Background()); err != nil {
		t.Fatalf("selftest: %v", err)
	}
}

func TestJSProbeDropsPluginAuthorization(t *testing.T) {
	var sawAuth, sawTest string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawTest = r.Header.Get("X-Test")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.auth", kind: "quota_probe" };
export function probe(ctx, steps) {
  const blob = JSON.stringify(ctx) + JSON.stringify(steps);
  if (blob.includes("super-secret-value")) return { error: "secret leaked" };
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/usage", headers: { Authorization: "Bearer leaked", "X-Test": "1" } } };
  return { result: { kind: "balance", entries: [{ label: "balance", remaining: 1 }] } };
}`
	plugin := mustProbePlugin(t, "xlyra.test.auth", source)
	result := probeQuotaJS(context.Background(), server.Client(), plugin, "sub2api", "api_key", server.URL, "super-secret-value")
	if result.Status != "ok" {
		t.Fatalf("result = %+v", result)
	}
	if sawAuth != "Bearer super-secret-value" || sawTest != "1" {
		t.Fatalf("auth = %q, x-test = %q", sawAuth, sawTest)
	}
}

func TestJSProbeRejectsForeignURL(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.url", kind: "quota_probe" };
export function probe() { return { request: { path: "https://evil.example/v1/usage" } }; }`
	plugin := mustProbePlugin(t, "xlyra.test.url", source)
	result := probeQuotaJS(context.Background(), server.Client(), plugin, "sub2api", "api_key", server.URL, "secret")
	if result.Status == "ok" || !strings.Contains(result.Error, jsplugin.KindURLRejected) {
		t.Fatalf("result = %+v", result)
	}
	if called {
		t.Fatal("request was sent")
	}
}

func TestJSProbeStopsRedirectOffOrigin(t *testing.T) {
	evilHits := 0
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()
	later := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/next" {
			later++
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, evil.URL+"/stolen", http.StatusFound)
	}))
	defer server.Close()
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.redirect", kind: "quota_probe" };
export function probe(_ctx, steps) {
  if (steps.length === 0) return { request: { path: "/v1/usage" } };
  return { request: { path: "/v1/next" } };
}`
	plugin := mustProbePlugin(t, "xlyra.test.redirect", source)
	result := probeQuotaJS(context.Background(), server.Client(), plugin, "sub2api", "api_key", server.URL, "secret")
	if result.Status == "ok" || !strings.Contains(result.Error, jsplugin.KindURLRejected) {
		t.Fatalf("result = %+v", result)
	}
	if evilHits != 0 || later != 0 {
		t.Fatalf("evil hits %d, follow-up hits %d", evilHits, later)
	}
}

func TestJSProbeCancelStopsRequests(t *testing.T) {
	hits := 0
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			close(started)
			time.Sleep(200 * time.Millisecond)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.cancel", kind: "quota_probe" };
export function probe() { return { request: { path: "/v1/again" } }; }`
	plugin := mustProbePlugin(t, "xlyra.test.cancel", source)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan QuotaProbeResult, 1)
	go func() {
		done <- probeQuotaJS(ctx, server.Client(), plugin, "sub2api", "api_key", server.URL, "secret")
	}()
	<-started
	cancel()
	result := <-done
	if result.Status == "ok" || !strings.Contains(result.Error, "context canceled") {
		t.Fatalf("result = %+v", result)
	}
	if hits > 2 {
		t.Fatalf("requests after cancel = %d", hits)
	}
}

func TestJSProbeStepLimit(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.steps", kind: "quota_probe" };
export function probe() { return { request: { path: "/v1/again" } }; }`
	plugin := mustProbePlugin(t, "xlyra.test.steps", source)
	result := probeQuotaJS(context.Background(), server.Client(), plugin, "sub2api", "api_key", server.URL, "secret")
	if result.Status == "ok" || !strings.Contains(result.Error, "6 steps") {
		t.Fatalf("result = %+v", result)
	}
	if hits != 5 {
		t.Fatalf("requests = %d, want 5", hits)
	}
}

func TestJSPluginRuntimeValidation(t *testing.T) {
	if err := ValidateJSPluginRuntime(); err != nil {
		t.Fatalf("builtins: %v", err)
	}
}

func TestCanonicalQuotaBase(t *testing.T) {
	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatalf("load builtins: %v", err)
	}
	cases := []struct {
		probeType string
		path      string
		wantURL   func(base string) string
		bases     []string
	}{
		{QuotaProbeTypeKimi, "/v1/usages", func(base string) string {
			switch strings.TrimSpace(base) {
			case "":
				return "https://api.kimi.com/coding/v1/usages"
			case "https://api.kimi.com/coding/v1":
				return "https://api.kimi.com/coding/v1/usages"
			case "https://api.kimi.com/coding/V1":
				return "https://api.kimi.com/coding/V1/v1/usages"
			case "https://api.kimi.com/coding/extra":
				return "https://api.kimi.com/coding/extra/v1/usages"
			case "https://api.kimi.com:8443/coding":
				return "https://api.kimi.com:8443/coding/v1/usages"
			default:
				return ""
			}
		}, []string{"", "https://api.kimi.com/coding/v1", "https://api.kimi.com/coding/V1", "https://api.kimi.com/coding/extra", "https://api.kimi.com:8443/coding"}},
		{QuotaProbeTypeGLM, "/api/monitor/usage/quota/limit", func(base string) string {
			switch strings.TrimSpace(base) {
			case "", "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/V1":
				return "https://open.bigmodel.cn/api/monitor/usage/quota/limit"
			case "https://api.z.ai:8443/api/coding/paas/v4":
				return "https://api.z.ai:8443/api/monitor/usage/quota/limit"
			default:
				return ""
			}
		}, []string{"", "https://open.bigmodel.cn/api/coding/paas/v4", "https://open.bigmodel.cn/V1", "https://api.z.ai:8443/api/coding/paas/v4"}},
		{QuotaProbeTypeMoonshot, "/v1/users/me/balance", func(base string) string {
			switch strings.TrimSpace(base) {
			case "", "https://api.moonshot.cn/v1":
				return "https://api.moonshot.cn/v1/users/me/balance"
			case "https://api.moonshot.cn/V1":
				return "https://api.moonshot.cn/V1/v1/users/me/balance"
			case "https://api.moonshot.cn:8443":
				return "https://api.moonshot.cn:8443/v1/users/me/balance"
			default:
				return ""
			}
		}, []string{"", "https://api.moonshot.cn/v1", "https://api.moonshot.cn/V1", "https://api.moonshot.cn:8443"}},
		{QuotaProbeTypeDeepSeek, "/user/balance", func(base string) string {
			switch strings.TrimSpace(base) {
			case "", "https://api.deepseek.com/v1", "https://api.deepseek.com/V1":
				return "https://api.deepseek.com/user/balance"
			case "https://api.deepseek.com:8443/v1":
				return "https://api.deepseek.com:8443/user/balance"
			default:
				return ""
			}
		}, []string{"", "https://api.deepseek.com/v1", "https://api.deepseek.com/V1", "https://api.deepseek.com:8443/v1"}},
	}
	for _, tc := range cases {
		plugin := mustBuiltin(t, registry, tc.probeType)
		for _, base := range tc.bases {
			canonical, err := canonicalQuotaBase(plugin, base)
			if err != nil {
				t.Fatalf("%s base %q: %v", tc.probeType, base, err)
			}
			got, err := jsplugin.ResolveProbeURL(canonical, tc.path, nil)
			if err != nil {
				t.Fatalf("%s resolve %q: %v", tc.probeType, base, err)
			}
			want := tc.wantURL(base)
			if got != want {
				t.Fatalf("%s base %q: got %s, want %s", tc.probeType, base, got, want)
			}
		}
	}
	sub2 := mustBuiltin(t, registry, QuotaProbeTypeSub2API)
	if _, err := canonicalQuotaBase(sub2, "  "); err == nil {
		t.Fatal("empty sub2api base was accepted")
	}
	// Go concatenates an empty base into a relative path and fails when the request is built.
	if got := quotaProbeURL("  ", "/v1/usage"); strings.Contains(got, "://") {
		t.Fatalf("go empty base became absolute %s", got)
	}
}

func TestRunQuotaProbesKeepsLastResultWhenHookTimesOut(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("timed-out hook sent %s", r.URL.Path)
	}))
	defer server.Close()

	source := `export const meta = { apiVersion: 1, id: "xlyra.test.slow", kind: "quota_probe" };
export function probe() { for (;;) {} }`
	manifest := jsplugin.Manifest{
		ID: "xlyra.test.slow", Name: "slow", Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		XLyra: ">=1.14.0", Kind: "quota_probe",
		QuotaProbe: jsplugin.QuotaProbeSection{BaseURLMode: "as_is", Replaces: QuotaProbeTypeKimi},
		SHA256:     map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, source, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	siteID := uuid.New()
	remaining := 42.0
	previous := QuotaProbeResult{
		Status:    "ok",
		Kind:      "token_plan",
		Plan:      "Allegretto",
		Entries:   []QuotaProbeEntry{{Label: "weekly", Unit: "percent", Remaining: &remaining}},
		FetchedAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}
	credential := siteEncryptedCredential(t, uuid.New(), siteID, "api_key", "sk-kimi",
		siteJSONMeta(t, map[string]any{QuotaProbeCredentialMetaKey: previous}))
	item := store.Site{
		ID: siteID, Name: "kimi", SiteType: "kimi_code", BaseURL: server.URL, Enabled: true,
		Meta: siteJSONMeta(t, map[string]any{"gateway": map[string]any{"quota_probe": QuotaProbeTypeKimi}}),
	}

	var savedCredential store.SiteCredential
	var savedSite store.Site
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{credential}
				tx.RowsAffected = 1
			case *store.SiteCredential:
				*dest = credential
				tx.RowsAffected = 1
			case *store.Site:
				*dest = item
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteCredential:
				savedCredential = *dest
			case *store.Site:
				savedSite = *dest
			}
			tx.RowsAffected = 1
		},
	})
	service.confFile = confFile
	prevCatalog := jsplugin.DefaultCatalog().Current()
	jsplugin.DefaultCatalog().Replace(&jsplugin.Snapshot{Gen: 999, Registry: jsplugin.NewRegistry(plugin)})
	defer func() {
		if prevCatalog != nil {
			jsplugin.DefaultCatalog().Replace(prevCatalog)
		}
	}()

	service.runQuotaProbes(context.Background(), item)

	var credentialMeta map[string]json.RawMessage
	if err := json.Unmarshal(savedCredential.Meta, &credentialMeta); err != nil {
		t.Fatalf("credential meta was not saved: %v", err)
	}
	var stored QuotaProbeResult
	if err := json.Unmarshal(credentialMeta[QuotaProbeCredentialMetaKey], &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status != "error" || !strings.Contains(stored.Error, jsplugin.KindTimeout) {
		t.Fatalf("stored probe = %+v, want a %s error", stored, jsplugin.KindTimeout)
	}
	if len(stored.Entries) != 1 || stored.Plan != "Allegretto" || !stored.FetchedAt.Equal(previous.FetchedAt) {
		t.Fatalf("stored probe = %+v, want the previous entries kept", stored)
	}

	siteMeta := siteMustJSONMap(t, savedSite.Meta)
	summary, _ := siteMeta[siteMetaQuotaProbeSummary].(map[string]any)
	if summary["status"] != "error" || summary["ok_count"] != float64(0) {
		t.Fatalf("summary = %#v, want an error summary with no ok credentials", summary)
	}
}

func mustBuiltin(t *testing.T, registry *jsplugin.Registry, probeType string) *jsplugin.Plugin {
	t.Helper()
	plugin, ok := registry.ByProbeType(probeType)
	if !ok {
		t.Fatalf("missing builtin %s", probeType)
	}
	return plugin
}

func mustProbePlugin(t *testing.T, id, source string) *jsplugin.Plugin {
	t.Helper()
	manifest := jsplugin.Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		XLyra: ">=1.14.0", Kind: "quota_probe",
		QuotaProbe: jsplugin.QuotaProbeSection{BaseURLMode: "as_is"},
		SHA256:     map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, source, nil, 0)
	if err != nil {
		t.Fatalf("plugin: %v", err)
	}
	return plugin
}

func quotaResultDiff(left, right QuotaProbeResult) string {
	left.FetchedAt = time.Time{}
	right.FetchedAt = time.Time{}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	var leftValue, rightValue any
	_ = json.Unmarshal(leftJSON, &leftValue)
	_ = json.Unmarshal(rightJSON, &rightValue)
	if err := diffQuotaJSON(leftValue, rightValue); err != nil {
		return err.Error()
	}
	return ""
}

func diffQuotaJSON(left, right any) error {
	switch l := left.(type) {
	case float64:
		r, ok := right.(float64)
		if !ok || math.Abs(l-r) > 1e-6 {
			return errQuota(left, right)
		}
		return nil
	case map[string]any:
		r, ok := right.(map[string]any)
		if !ok || len(l) != len(r) {
			return errQuota(left, right)
		}
		for key, value := range l {
			if key == "fetched_at" {
				continue
			}
			if err := diffQuotaJSON(value, r[key]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		r, ok := right.([]any)
		if !ok || len(l) != len(r) {
			return errQuota(left, right)
		}
		for i := range l {
			if err := diffQuotaJSON(l[i], r[i]); err != nil {
				return err
			}
		}
		return nil
	default:
		if left != right {
			return errQuota(left, right)
		}
		return nil
	}
}

type quotaDiffError struct{ left, right any }

func (e quotaDiffError) Error() string {
	left, _ := json.Marshal(e.left)
	right, _ := json.Marshal(e.right)
	return string(left) + " != " + string(right)
}

func errQuota(left, right any) error { return quotaDiffError{left, right} }
