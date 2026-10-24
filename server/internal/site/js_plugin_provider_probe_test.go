package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

const providerProbeBody = `
export function probe(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/quota" } };
  if (steps.length === 1) return { request: { method: "GET", path: "/v1/quota/bonus" } };
  const week = steps[0].response.json;
  const bonus = steps[1].response.json;
  return { result: { kind: "balance", plan: "plugin-plan", entries: [
    { label: "pack", kind: "package", name: "Week", unit: "credits", remaining: week.remaining, limit: week.limit, used: week.used },
    { label: "bonus", kind: "package", name: "Bonus", unit: "credits", remaining: bonus.remaining, limit: bonus.limit, used: bonus.used }
  ] } };
}
`

func TestSiteProviderProbeRefreshWritesSummary(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/quota":
			_, _ = w.Write([]byte(`{"remaining":40,"limit":100,"used":60}`))
		case "/v1/quota/bonus":
			_, _ = w.Write([]byte(`{"remaining":15,"limit":20,"used":5}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	plugin := providerHookPlugin(t, "quota.example", providerProbeBody)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{probeServerHost(t, server.URL)})
	service := &Service{}
	if got := service.defaultQuotaProbeTypeForSite(store.Site{SiteType: "Quota.Example/Chat"}); got != "quota.example/chat" {
		t.Fatalf("default probe = %q", got)
	}

	item := store.Site{
		ID: uuid.New(), Name: "quota", SiteType: "Quota.Example/Chat", BaseURL: server.URL, Enabled: true,
	}
	credential := siteEncryptedCredential(t, uuid.New(), item.ID, "api_key", "sk-probe", nil)
	savedSite, stored, cooldown := refreshPluginQuota(t, item, credential)
	if cooldown {
		t.Fatal("plugin probe touched a cooldown")
	}
	if stored.Status != "ok" || stored.Plan != "plugin-plan" || stored.Kind != "balance" || len(stored.Entries) != 2 {
		t.Fatalf("stored = %+v", stored)
	}
	if stored.Entries[0].Name != "Week" || stored.Entries[0].Unit != "credits" || stored.Entries[1].Unit != "credits" || stored.Entries[1].Remaining == nil || *stored.Entries[1].Remaining != 15 {
		t.Fatalf("entries = %+v", stored.Entries)
	}
	mu.Lock()
	gotPaths := append([]string(nil), paths...)
	gotAuth := auth
	mu.Unlock()
	if len(gotPaths) != 2 || gotPaths[0] != "/v1/quota" || gotPaths[1] != "/v1/quota/bonus" || gotAuth != "Bearer sk-probe" {
		t.Fatalf("paths=%v auth=%q", gotPaths, gotAuth)
	}

	summary := quotaSummaryFromMeta(t, savedSite.Meta)
	if summary["probe_type"] != "quota.example/chat" || summary["status"] != "ok" || summary["plan"] != "plugin-plan" || summary["unit"] != "credits" || summary["remaining_min"] != 15.0 || summary["limit"] != 20.0 || summary["used"] != 5.0 {
		t.Fatalf("summary = %#v", summary)
	}
	if _, ok := summary["aggregate"]; ok {
		t.Fatalf("summary = %#v", summary)
	}
	entries := quotaSummaryEntries(t, summary)
	if len(entries) != 2 || entries[0].Label != "pack" || entries[0].Kind != "package" || entries[1].Name != "Bonus" {
		t.Fatalf("summary entries = %+v", entries)
	}
}

func TestSiteProviderProbeExplicitBuiltinWins(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"limit":"100","used":"10","remaining":"90","resetTime":"2026-07-27T02:19:14Z"},"limits":[]}`))
	}))
	t.Cleanup(server.Close)

	plugin := providerHookPlugin(t, "quota.example", `
export function probe() {
  return { result: { kind: "balance", plan: "from-plugin", entries: [{ label: "pack", unit: "credits", remaining: 1, limit: 2 }] } };
}
`)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{probeServerHost(t, server.URL)})
	item := store.Site{
		ID: uuid.New(), Name: "quota", SiteType: "quota.example/chat", BaseURL: server.URL, Enabled: true,
		Meta: siteJSONMeta(t, map[string]any{"gateway": map[string]any{"quota_probe": QuotaProbeTypeKimi}}),
	}
	credential := siteEncryptedCredential(t, uuid.New(), item.ID, "api_key", "sk-kimi", nil)
	savedSite, stored, _ := refreshPluginQuota(t, item, credential)
	if path != "/v1/usages" {
		t.Fatalf("path = %q, want builtin kimi", path)
	}
	if stored.Status != "ok" || stored.Plan == "from-plugin" || len(stored.Entries) != 1 || stored.Entries[0].Unit != "percent" || stored.Entries[0].Label != "weekly" {
		t.Fatalf("stored = %+v", stored)
	}
	summary := quotaSummaryFromMeta(t, savedSite.Meta)
	if summary["probe_type"] != QuotaProbeTypeKimi || summary["unit"] != "percent" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSiteProviderProbeSkipsWithoutHook(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(server.Close)

	if _, err := NormalizeQuotaProbeType("quota.example/chat"); err == nil {
		t.Fatal("site type was accepted as a quota_probe value")
	}
	bare := providerHookPlugin(t, "bare.example", "")
	useCatalog(t, bare)
	installProviderSite(t, bare, true, []string{probeServerHost(t, server.URL)})
	service := &Service{}
	if got := service.defaultQuotaProbeTypeForSite(store.Site{SiteType: "bare.example/chat", BaseURL: server.URL}); got != "" {
		t.Fatalf("missing probe default = %q", got)
	}
	if got := service.defaultQuotaProbeTypeForSite(store.Site{SiteType: "openai", BaseURL: server.URL}); got != "" {
		t.Fatalf("openai default = %q", got)
	}
	if updated := service.runQuotaProbes(context.Background(), store.Site{SiteType: "bare.example/chat", BaseURL: server.URL}); hits.Load() != 0 || updated.Meta != nil {
		t.Fatalf("hits=%d meta=%s", hits.Load(), updated.Meta)
	}

	withProbe := providerHookPlugin(t, "quota.example", providerProbeBody)
	useCatalog(t, withProbe)
	installProviderSite(t, withProbe, false, []string{probeServerHost(t, server.URL)})
	if got := service.defaultQuotaProbeTypeForSite(store.Site{SiteType: "quota.example/chat", BaseURL: server.URL}); got != "" {
		t.Fatalf("disabled probe default = %q", got)
	}
	if got := new(Service).defaultQuotaProbeTypeForSite(store.Site{SiteType: "kimi_code"}); got != QuotaProbeTypeKimi {
		t.Fatalf("kimi_code default = %q", got)
	}
}

func TestSiteProviderProbeHostRejectPreservesPrevious(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits.Add(1)
	}))
	t.Cleanup(server.Close)
	plugin := providerHookPlugin(t, "quota.example", `
export function probe() { return { request: { method: "GET", path: "/v1/quota" } }; }
`)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{"api.example.com"})
	savedSite, stored, cooldown := refreshPluginQuota(t, pluginProbeSite(t, server.URL, nil), pluginProbeCredential(t))
	if hits.Load() != 0 || cooldown {
		t.Fatalf("hits=%d cooldown=%v", hits.Load(), cooldown)
	}
	if stored.Status != "error" || !strings.Contains(stored.Error, "not allowed") || stored.Plan != "kept-plan" || len(stored.Entries) != 1 || stored.Entries[0].Unit != "credits" {
		t.Fatalf("stored = %+v", stored)
	}
	summary := quotaSummaryFromMeta(t, savedSite.Meta)
	if summary["status"] != "error" || summary["probe_type"] != "quota.example/chat" || summary["remaining_min"] != 42.0 || summary["unit"] != "credits" {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestSiteProviderProbeFailuresPreserveAndDoNotCoolDown(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"throw", `export function probe() { throw new Error("probe blew up"); }`, "probe blew up"},
		{"timeout", `export function probe() { for (;;) {} }`, jsplugin.KindTimeout},
		{"shape", `export function probe() { return { nope: true }; }`, jsplugin.KindShape},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := providerHookPlugin(t, "quota.example", tc.body)
			useCatalog(t, plugin)
			installProviderSite(t, plugin, true, []string{"api.example.com"})
			savedSite, stored, cooldown := refreshPluginQuota(t, pluginProbeSite(t, "https://api.example.com", nil), pluginProbeCredential(t))
			if cooldown {
				t.Fatal("failure started a cooldown")
			}
			if stored.Status != "error" || !strings.Contains(stored.Error, tc.want) || stored.Plan != "kept-plan" || len(stored.Entries) != 1 || !stored.FetchedAt.Equal(time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)) {
				t.Fatalf("stored = %+v", stored)
			}
			summary := quotaSummaryFromMeta(t, savedSite.Meta)
			if summary["status"] != "error" || summary["remaining_min"] != 42.0 || summary["unit"] != "credits" || summary["plan"] != "kept-plan" {
				t.Fatalf("summary = %#v", summary)
			}
		})
	}
}

func TestSiteProviderProbeBreakerDoesNotDisable(t *testing.T) {
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if n.Add(1) <= 21 {
			_, _ = w.Write([]byte(`{"ok":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	plugin := providerHookPlugin(t, "breaker.example", `
export function probe(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/quota" } };
  if (!steps[0].response.json || !steps[0].response.json.ok) return { error: "upstream failed" };
  return { result: { kind: "balance", entries: [{ label: "pack", unit: "credits", remaining: 7, limit: 10 }] } };
}
`)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{probeServerHost(t, server.URL)})
	service := &Service{}
	siteType := "breaker.example/chat"
	breaker := jsplugin.DefaultCatalog().Breaker()
	before := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	gen := jsplugin.DefaultCatalog().Generation()
	for i := 0; i < 21; i++ {
		result := service.probeQuota(context.Background(), server.Client(), siteType, siteType, "api_key", server.URL, "sk-probe")
		if result.Status == "ok" || !strings.Contains(result.Error, "upstream failed") {
			t.Fatalf("call %d = %+v", i, result)
		}
	}
	mid := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	if mid.Calls < before.Calls+21 || mid.Errors < before.Errors+21 {
		t.Fatalf("metrics after failures = %+v, before %+v", mid, before)
	}
	result := service.probeQuota(context.Background(), server.Client(), siteType, siteType, "api_key", server.URL, "sk-probe")
	if result.Status != "ok" || len(result.Entries) != 1 || result.Entries[0].Unit != "credits" || result.Entries[0].Remaining == nil || *result.Entries[0].Remaining != 7 {
		t.Fatalf("recovered probe = %+v", result)
	}
	after := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	if after.Calls < before.Calls+22 || after.Errors != mid.Errors {
		t.Fatalf("metrics after recovery = %+v, mid %+v", after, mid)
	}
	if jsplugin.DefaultCatalog().Generation() != gen {
		t.Fatalf("generation = %d, want %d", jsplugin.DefaultCatalog().Generation(), gen)
	}
	if got, ok := service.siteTypeHookPlugin(siteType, "probe"); !ok || got.Manifest.ID != plugin.Manifest.ID || !got.HasHook("probe") {
		t.Fatalf("probe hook after failures = %v", ok)
	}
}

func TestQuotaProbeSummaryEntryKeepsUnitForUnknownProbeType(t *testing.T) {
	t.Parallel()
	probeType := "quota.example/chat"
	credits := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "pack", Unit: "credits", Remaining: f64ptr(9)},
		{Label: "bonus", Unit: "credits", Remaining: f64ptr(4)},
	}}
	entry, ok := quotaProbeSummaryEntry(probeType, credits)
	if !ok || entry.Unit != "credits" || entry.Remaining == nil || *entry.Remaining != 4 {
		t.Fatalf("credits = %+v ok=%v", entry, ok)
	}
	percent := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "five_hour", Unit: "percent", Remaining: f64ptr(80)},
		{Label: "weekly", Unit: "percent", Remaining: f64ptr(20)},
	}}
	entry, ok = quotaProbeSummaryEntry(probeType, percent)
	if !ok || entry.Unit != "percent" || entry.Label != "weekly" || entry.Remaining == nil || *entry.Remaining != 20 {
		t.Fatalf("percent = %+v ok=%v", entry, ok)
	}
	usd := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "cash", Unit: "usd", Remaining: f64ptr(3)},
		{Label: "balance", Unit: "usd", Remaining: f64ptr(12.5)},
	}}
	entry, ok = quotaProbeSummaryEntry(probeType, usd)
	if !ok || entry.Unit != "usd" || entry.Label != "balance" || entry.Remaining == nil || *entry.Remaining != 12.5 {
		t.Fatalf("usd = %+v ok=%v", entry, ok)
	}
	// balance wins even when another unit is smaller, and a credits balance stays credits.
	mixed := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "pack", Unit: "usd", Remaining: f64ptr(1)},
		{Label: "balance", Unit: "credits", Remaining: f64ptr(40)},
	}}
	entry, ok = quotaProbeSummaryEntry(probeType, mixed)
	if !ok || entry.Unit != "credits" || entry.Label != "balance" || entry.Remaining == nil || *entry.Remaining != 40 {
		t.Fatalf("credits balance = %+v ok=%v", entry, ok)
	}
	smallestCredits := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "cash", Unit: "usd", Remaining: f64ptr(3)},
		{Label: "pack", Unit: "credits", Remaining: f64ptr(1)},
	}}
	entry, ok = quotaProbeSummaryEntry(probeType, smallestCredits)
	if !ok || entry.Unit != "credits" || entry.Remaining == nil || *entry.Remaining != 1 {
		t.Fatalf("smallest credits = %+v ok=%v", entry, ok)
	}
}

func pluginProbeSite(t *testing.T, baseURL string, extra map[string]any) store.Site {
	t.Helper()
	meta := map[string]any{
		"quota_probe_summary": map[string]any{
			"status":        "ok",
			"probe_type":    "quota.example/chat",
			"remaining_min": 42.0,
			"unit":          "credits",
			"plan":          "kept-plan",
		},
	}
	for key, value := range extra {
		meta[key] = value
	}
	return store.Site{
		ID: uuid.New(), Name: "quota", SiteType: "quota.example/chat", BaseURL: baseURL, Enabled: true,
		Meta: siteJSONMeta(t, meta),
	}
}

func pluginProbeCredential(t *testing.T) store.SiteCredential {
	t.Helper()
	remaining := 42.0
	previous := QuotaProbeResult{
		Status:    "ok",
		Kind:      "balance",
		Plan:      "kept-plan",
		Entries:   []QuotaProbeEntry{{Label: "pack", Unit: "credits", Remaining: &remaining}},
		FetchedAt: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC),
	}
	return siteEncryptedCredential(t, uuid.New(), uuid.New(), "api_key", "sk-probe",
		siteJSONMeta(t, map[string]any{QuotaProbeCredentialMetaKey: previous}))
}

func TestSiteProviderProbeQuotaAggregate(t *testing.T) {
	const aggregateProbeBody = `
export function probe(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/quota" } };
  const body = steps[0].response.json;
  return { result: { kind: "balance", entries: [{
    label: "balance", unit: "credits",
    remaining: body.remaining, limit: body.limit, used: body.used, resetAt: body.resetAt
  }] } };
}
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Authorization") {
		case "Bearer sk-tight":
			_, _ = w.Write([]byte(`{"remaining":10,"limit":40,"used":30,"resetAt":"2099-06-01T00:00:00Z"}`))
		case "Bearer sk-wide":
			_, _ = w.Write([]byte(`{"remaining":25,"limit":80,"used":55,"resetAt":"2099-01-15T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	for _, tc := range []struct {
		mode      string
		remaining float64
		limit     float64
		used      float64
		resetAt   string
	}{
		{mode: "min", remaining: 10, limit: 40, used: 30, resetAt: "2099-06-01T00:00:00Z"},
		{mode: "sum", remaining: 35, limit: 120, used: 85, resetAt: "2099-01-15T00:00:00Z"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			plugin := providerHookPlugin(t, "quota.example", aggregateProbeBody)
			useCatalog(t, plugin)
			installProviderSiteMode(t, plugin, true, []string{probeServerHost(t, server.URL)}, tc.mode)
			item := store.Site{
				ID: uuid.New(), Name: "quota", SiteType: "quota.example/chat", BaseURL: server.URL, Enabled: true,
			}
			credentials := []store.SiteCredential{
				siteEncryptedCredential(t, uuid.New(), item.ID, "api_key", "sk-tight", nil),
				siteEncryptedCredential(t, uuid.New(), item.ID, "api_key", "sk-wide", nil),
			}
			savedSite, _, _ := refreshPluginQuotas(t, item, credentials)
			summary := quotaSummaryFromMeta(t, savedSite.Meta)
			if summary["aggregate"] != tc.mode || summary["status"] != "ok" || summary["unit"] != "credits" || summary["remaining_min"] != tc.remaining || summary["limit"] != tc.limit || summary["used"] != tc.used || summary["reset_at"] != tc.resetAt {
				t.Fatalf("summary = %#v", summary)
			}
			if summary["credential_count"] != 2.0 || summary["ok_count"] != 2.0 {
				t.Fatalf("counts = %#v", summary)
			}
		})
	}

	t.Run("none", func(t *testing.T) {
		plugin := providerHookPlugin(t, "quota.example", `
export function probe() {
  return { result: { kind: "balance", entries: [{ label: "balance", unit: "credits", remaining: 3, limit: 4, used: 1 }] } };
}
`)
		useCatalog(t, plugin)
		installProviderSiteMode(t, plugin, true, []string{"api.example.com"}, "none")
		savedSite, stored, cooldown := refreshPluginQuota(t, pluginProbeSite(t, "https://api.example.com", nil), pluginProbeCredential(t))
		if cooldown {
			t.Fatal("none started a cooldown")
		}
		if stored.Status != "ok" || len(stored.Entries) != 1 || stored.Entries[0].Remaining == nil || *stored.Entries[0].Remaining != 3 {
			t.Fatalf("stored = %+v", stored)
		}
		meta := siteMustJSONMap(t, savedSite.Meta)
		if _, ok := meta[siteMetaQuotaProbeSummary]; ok {
			t.Fatalf("summary = %#v", meta[siteMetaQuotaProbeSummary])
		}
	})
}

func refreshPluginQuota(t *testing.T, item store.Site, credential store.SiteCredential) (store.Site, QuotaProbeResult, bool) {
	t.Helper()
	return refreshPluginQuotas(t, item, []store.SiteCredential{credential})
}

func refreshPluginQuotas(t *testing.T, item store.Site, credentials []store.SiteCredential) (store.Site, QuotaProbeResult, bool) {
	t.Helper()
	var savedSite store.Site
	var savedCredential store.SiteCredential
	var cooldown bool
	db := siteTransactionPostgresGorm(t)
	siteReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		if routeCooldownStatement(tx) {
			cooldown = true
		}
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteCredential:
			*dest = append([]store.SiteCredential(nil), credentials...)
			tx.RowsAffected = int64(len(credentials))
		case *store.SiteCredential:
			if len(credentials) > 0 {
				*dest = credentials[0]
			}
			tx.RowsAffected = 1
		case *store.Site:
			*dest = item
			tx.RowsAffected = 1
		case *[]store.RouteCooldown:
			cooldown = true
			tx.RowsAffected = 0
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
		tx.Statement.RowsAffected = tx.RowsAffected
	})
	siteReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if routeCooldownStatement(tx) {
			cooldown = true
		}
		switch dest := tx.Statement.Dest.(type) {
		case *store.SiteCredential:
			savedCredential = *dest
		case map[string]any:
			if meta, ok := dest["meta"].(store.JSON); ok {
				savedCredential.Meta = append(store.JSON(nil), meta...)
			}
		case *store.Site:
			savedSite = *dest
		}
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	})
	siteReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*store.RouteCooldown); ok {
			cooldown = true
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(gorm.ErrInvalidData)
	})
	service := NewService(siteStoreWithGorm(t, db), siteTestMasterKey)
	service.runQuotaProbes(context.Background(), item)
	var stored QuotaProbeResult
	if len(savedCredential.Meta) > 0 {
		var credentialMeta map[string]json.RawMessage
		if err := json.Unmarshal(savedCredential.Meta, &credentialMeta); err != nil {
			t.Fatalf("credential meta: %v", err)
		}
		if raw, ok := credentialMeta[QuotaProbeCredentialMetaKey]; ok {
			if err := json.Unmarshal(raw, &stored); err != nil {
				t.Fatal(err)
			}
		}
	}
	return savedSite, stored, cooldown
}

func routeCooldownStatement(tx *gorm.DB) bool {
	switch tx.Statement.Model.(type) {
	case *store.RouteCooldown, store.RouteCooldown:
		return true
	}
	switch tx.Statement.Dest.(type) {
	case *store.RouteCooldown, *[]store.RouteCooldown:
		return true
	}
	return false
}

func quotaSummaryFromMeta(t *testing.T, meta store.JSON) map[string]any {
	t.Helper()
	summary, ok := siteMustJSONMap(t, meta)[siteMetaQuotaProbeSummary].(map[string]any)
	if !ok {
		t.Fatalf("summary missing in %s", meta)
	}
	return summary
}

func quotaSummaryEntries(t *testing.T, summary map[string]any) []QuotaProbeEntry {
	t.Helper()
	raw, err := json.Marshal(summary["entries"])
	if err != nil {
		t.Fatal(err)
	}
	var entries []QuotaProbeEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func probeServerHost(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Hostname()
}

func f64ptr(v float64) *float64 {
	return &v
}
