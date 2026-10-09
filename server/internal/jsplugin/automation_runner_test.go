package jsplugin

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type fakeHost struct {
	mu    sync.Mutex
	calls []string
	fail  bool
}

func (h *fakeHost) ResetAPIKeyQuota(_ context.Context, id uuid.UUID, scopes []string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fail {
		return errors.New("database is down")
	}
	h.calls = append(h.calls, id.String()+":"+scopes[0])
	return nil
}

const rolloverPlugin = `export const meta = { apiVersion: 1, id: "acme-reset", kind: "automation" };
export function handle(ctx, event) {
  const before = event.previous && event.previous.weekly;
  const after = event.current.weekly;
  if (!before || !after || after.reset_at <= before.reset_at) return { actions: [] };
  return {
    actions: event.targets.map((t) => ({ type: "apikey.reset_usage", target: t.id, scope: "total", idempotencyKey: String(after.reset_at) })),
    state: { lastReset: after.reset_at, level: ctx.config.level },
  };
}
`

func rolloverPackage(t *testing.T, version string) []byte {
	t.Helper()
	manifest := `{
  "id": "acme-reset", "name": "Reset", "version": "` + version + `", "apiVersion": 1, "hostApi": 1, "kind": "automation",
  "automation": {
    "subscribes": ["oauth.quota_synced"],
    "permissions": ["apikey.reset_usage"],
    "binding": {
      "subject": { "type": "oauth_connection", "providers": ["codex"] },
      "target": { "type": "api_key", "requires": "finite_total_quota" },
      "config": { "type": "object", "properties": { "level": { "type": "string", "default": "weekly" } } }
    }
  },
  "sha256": { "plugin.js": "` + hashSource(rolloverPlugin) + `" }
}`
	fixture := `{"name":"rollover","ctx":{"now":1,"config":{"level":"weekly"}},"input":{"type":"oauth.quota_synced","subject":{"type":"oauth_connection","id":"c"},"previous":{"weekly":{"reset_at":1}},"current":{"weekly":{"reset_at":2}},"targets":[{"type":"api_key","id":"k","name":"n","usage":{}}]},"expect":{"result":{"actions":[{"type":"apikey.reset_usage","target":"k","scope":"total","idempotencyKey":"2"}]}}}`
	raw, err := BuildPackage(map[string][]byte{
		"manifest.json":     []byte(manifest),
		"plugin.js":         []byte(rolloverPlugin),
		"fixtures/one.json": []byte(fixture),
	})
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	return raw
}

func countRows(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// An event travels from the queue through the plugin to a reset, once, and a
// failing host leaves it queued for another attempt.
func TestAutomationEndToEnd(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	manager := NewManager(st, &Catalog{})

	conn, err := store.NewOAuthConnectionRepository(st.DB()).Save(ctx, store.OAuthConnection{
		Provider: "codex", Email: "owner@example.com", Status: "connected", RawProfile: store.JSON("{}"), Metadata: store.JSON("{}"),
	})
	if err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	newKey := func(name string, limit float64, unlimited bool) store.APIKey {
		key := store.APIKey{
			Name: name, KeyPrefix: name, KeyHash: uuid.NewString(), MaskedKey: name, KeyKind: "generated",
			Scope: "gateway", Status: "active", ModelPolicy: "allow_all", SitePolicy: "allow_all", BillingMultiplier: 1,
			QuotaUnlimited: unlimited, QuotaDailyUnlimited: true, QuotaWeeklyUnlimited: true, QuotaTotalUsed: 12,
		}
		if !unlimited {
			key.QuotaLimit = sql.NullFloat64{Float64: limit, Valid: true}
		}
		if err := st.DB().Create(&key).Error; err != nil {
			t.Fatalf("seed key: %v", err)
		}
		return key
	}
	finite, unlimited := newKey("finite", 20, false), newKey("unlimited", 0, true)

	if _, err := manager.Upload(ctx, "", rolloverPackage(t, "1.0.0")); err != nil {
		t.Fatalf("upload: %v", err)
	}
	// Enabling needs the admin to grant what the manifest declares.
	err = manager.Enable(ctx, "acme-reset", "1.0.0", EnableOptions{ConfirmUntrusted: true})
	var needed *PermissionsRequiredError
	if !errors.As(err, &needed) {
		t.Fatalf("enable without grants: err = %v, want PermissionsRequiredError", err)
	}
	if err := manager.Enable(ctx, "acme-reset", "1.0.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionAPIKeyResetUsage}}); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// Bad bindings are refused with a reason the admin can act on.
	for name, in := range map[string]AutomationBindingInput{
		"unlimited key": {SubjectID: conn.ID.String(), TargetIDs: []string{unlimited.ID.String()}},
		"no targets":    {SubjectID: conn.ID.String()},
		"missing key":   {SubjectID: conn.ID.String(), TargetIDs: []string{uuid.NewString()}},
		"bad config":    {SubjectID: conn.ID.String(), TargetIDs: []string{finite.ID.String()}, Config: map[string]any{"nope": 1}},
		"unknown owner": {SubjectID: uuid.NewString(), TargetIDs: []string{finite.ID.String()}},
	} {
		var inputErr *AutomationInputError
		if _, err := manager.CreateAutomation(ctx, "acme-reset", in); !errors.As(err, &inputErr) {
			t.Errorf("%s: err = %v, want an input error", name, err)
		}
	}
	binding, err := manager.CreateAutomation(ctx, "acme-reset", AutomationBindingInput{
		SubjectID: conn.ID.String(), TargetIDs: []string{finite.ID.String()},
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}

	subject := AutomationSubject{Type: "oauth_connection", ID: conn.ID.String(), Provider: "codex", Label: "owner@example.com"}
	emit := func(resetAt float64) {
		t.Helper()
		err := manager.EmitEvent(ctx, st.DB(), EmittedEvent{
			Type: "oauth.quota_synced", Subject: subject,
			Previous: map[string]any{"weekly": map[string]any{"reset_at": resetAt - 1}},
			Current:  map[string]any{"weekly": map[string]any{"reset_at": resetAt}},
		})
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
	}

	// A different account's event reaches nobody.
	if err := manager.EmitEvent(ctx, st.DB(), EmittedEvent{Type: "oauth.quota_synced", Subject: AutomationSubject{Type: "oauth_connection", ID: uuid.NewString()}}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events`); n != 0 {
		t.Fatalf("queued %d events for an unbound account", n)
	}

	host := &fakeHost{fail: true}
	emit(100)
	if taken := manager.processDue(ctx, host); taken != 1 {
		t.Fatalf("processed %d events, want 1", taken)
	}
	if len(host.calls) != 0 {
		t.Fatalf("a failing host was credited with a reset: %v", host.calls)
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events WHERE status = 'pending' AND attempts = 1`); n != 1 {
		t.Fatalf("a failed attempt must leave the event pending, got %d", n)
	}
	// Not due yet, so a second pass leaves it alone.
	if taken := manager.processDue(ctx, host); taken != 0 {
		t.Fatalf("an event backing off was retried at once")
	}

	host.fail = false
	st.DB().Exec(`UPDATE js_plugin_events SET next_attempt_at = NOW() - INTERVAL '1 second'`)
	manager.processDue(ctx, host)
	if len(host.calls) != 1 || host.calls[0] != finite.ID.String()+":total" {
		t.Fatalf("host calls = %v, want one total reset of the finite key", host.calls)
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events WHERE status = 'done'`); n != 1 {
		t.Fatalf("done events = %d", n)
	}

	// The same upstream reset arriving again changes nothing.
	emit(100)
	manager.processDue(ctx, host)
	if len(host.calls) != 1 {
		t.Fatalf("the same reset was applied twice: %v", host.calls)
	}
	// A later reset applies again.
	emit(200)
	manager.processDue(ctx, host)
	if len(host.calls) != 2 {
		t.Fatalf("host calls = %v, want a second reset for the new window", host.calls)
	}

	views, err := manager.ListAutomations(ctx, "acme-reset")
	if err != nil || len(views) != 1 {
		t.Fatalf("views = %v, err = %v", views, err)
	}
	if views[0].SubjectLabel != "owner@example.com" || len(views[0].Targets) != 1 || views[0].Targets[0].Name != "finite" ||
		views[0].State["lastReset"] != float64(200) || views[0].State["level"] != "weekly" || views[0].Config["level"] != "weekly" {
		t.Fatalf("view = %+v", views[0])
	}
	log, err := manager.ActionLog(ctx, "acme-reset", 50)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range log {
		counts[row.Status]++
	}
	if counts[store.JSPluginActionApplied] != 2 || counts[store.JSPluginActionSkipped] != 1 || counts[store.JSPluginActionFailed] != 1 {
		t.Fatalf("action log = %v, want 2 applied, 1 skipped, 1 failed", counts)
	}

	// Revoking the grant stops the action without breaking the event.
	st.DB().Exec(`UPDATE js_plugin_versions SET granted_permissions = '[]'::jsonb`)
	emit(300)
	manager.processDue(ctx, host)
	if len(host.calls) != 2 {
		t.Fatalf("an ungranted action ran: %v", host.calls)
	}

	// A new version takes the bindings over, so the old one can be uninstalled
	// without cascading them away.
	if _, err := manager.Upload(ctx, "", rolloverPackage(t, "1.1.0")); err != nil {
		t.Fatalf("upload 1.1.0: %v", err)
	}
	if err := manager.Enable(ctx, "acme-reset", "1.1.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionAPIKeyResetUsage}}); err != nil {
		t.Fatalf("enable 1.1.0: %v", err)
	}
	if err := manager.DeleteVersion(ctx, "acme-reset", "1.0.0"); err != nil {
		t.Fatalf("delete 1.0.0: %v", err)
	}
	if got, err := manager.ListAutomations(ctx, "acme-reset"); err != nil || len(got) != 1 || got[0].Version != "1.1.0" {
		t.Fatalf("bindings after the upgrade = %+v, %v", got, err)
	}

	// Removing the binding takes its queue and targets with it.
	emit(400)
	if err := manager.DeleteAutomation(ctx, "acme-reset", mustParse(t, binding.ID.String())); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events`) + countRows(t, st, `SELECT COUNT(*) FROM js_plugin_binding_targets`); n != 0 {
		t.Fatalf("%d rows left after unbinding", n)
	}
}

func mustParse(t *testing.T, raw string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const tickPlugin = `export const meta = { apiVersion: 1, id: "acme-tick", kind: "automation" };
export function handle(ctx, event) {
  if (event.type !== "schedule.tick") return { actions: [] };
  const weekly = event.current.quota && event.current.quota.weekly;
  return {
    actions: [{ type: "notify", level: "warn", message: "weekly remaining " + (weekly ? weekly.remaining_percent : "unknown") + "% for " + event.subject.label }],
    state: { ticks: ((ctx.state && ctx.state.ticks) || 0) + 1 },
  };
}
`

func tickPackage(t *testing.T) []byte {
	t.Helper()
	manifest := `{
  "id": "acme-tick", "name": "Tick", "version": "1.0.0", "apiVersion": 1, "hostApi": 1, "kind": "automation",
  "automation": {
    "subscribes": ["schedule.tick"],
    "permissions": ["notify"],
    "schedule": { "everyMinutes": 60 },
    "binding": { "subject": { "type": "oauth_connection" } }
  },
  "sha256": { "plugin.js": "` + hashSource(tickPlugin) + `" }
}`
	fixture := `{"name":"tick","ctx":{"now":1},"input":{"type":"schedule.tick","subject":{"type":"oauth_connection","id":"c","label":"a@b"},"current":{"quota":{"weekly":{"remaining_percent":40}}},"targets":[]},"expect":{"result":{"actions":[{"type":"notify","level":"warn","message":"weekly remaining 40% for a@b"}],"state":{"ticks":1}}}}`
	raw, err := BuildPackage(map[string][]byte{
		"manifest.json":     []byte(manifest),
		"plugin.js":         []byte(tickPlugin),
		"fixtures/one.json": []byte(fixture),
	})
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	return raw
}

// A plugin that subscribes to schedule.tick is called on its own interval with
// the account as it is now, once per interval however long xLyra was down.
func TestAutomationScheduleTick(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	manager := NewManager(st, &Catalog{})
	conn, err := store.NewOAuthConnectionRepository(st.DB()).Save(ctx, store.OAuthConnection{
		Provider: "codex", Email: "owner@example.com", Status: "connected", RawProfile: store.JSON("{}"),
		Metadata: store.JSON(`{"quota":{"weekly":{"remaining_percent":40}}}`),
	})
	if err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	if _, err := manager.Upload(ctx, "", tickPackage(t)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := manager.Enable(ctx, "acme-tick", "1.0.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionNotify}}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	binding, err := manager.CreateAutomation(ctx, "acme-tick", AutomationBindingInput{SubjectID: conn.ID.String()})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	if binding.NextTickAt == nil {
		t.Fatal("a ticking binding must be due at once")
	}

	manager.queueTicks(ctx)
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events WHERE event_type = 'schedule.tick' AND status = 'pending'`); n != 1 {
		t.Fatalf("queued ticks = %d, want 1", n)
	}
	// Not due again for an hour, however often the loop looks.
	manager.queueTicks(ctx)
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events`); n != 1 {
		t.Fatalf("a tick was queued twice: %d", n)
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_bindings WHERE next_tick_at > NOW() + INTERVAL '59 minutes'`); n != 1 {
		t.Fatalf("the next tick was not pushed an interval ahead")
	}

	manager.processDue(ctx, &fakeHost{})
	log, err := manager.ActionLog(ctx, "acme-tick", 10)
	if err != nil || len(log) != 1 || log[0].Status != store.JSPluginActionApplied {
		t.Fatalf("action log = %+v, %v", log, err)
	}
	views, err := manager.ListAutomations(ctx, "acme-tick")
	if err != nil || len(views) != 1 || views[0].State["ticks"] != float64(1) {
		t.Fatalf("views = %+v, %v", views, err)
	}

	// After downtime the binding gets one tick, not one per missed interval.
	st.DB().Exec(`UPDATE js_plugin_bindings SET next_tick_at = NOW() - INTERVAL '3 days'`)
	manager.queueTicks(ctx)
	manager.queueTicks(ctx)
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events WHERE status = 'pending'`); n != 1 {
		t.Fatalf("after downtime queued %d ticks, want 1", n)
	}

	// A plugin that is switched off stops ticking: the binding is only postponed.
	if err := manager.Disable(ctx, "acme-tick"); err != nil {
		t.Fatal(err)
	}
	st.DB().Exec(`UPDATE js_plugin_bindings SET next_tick_at = NOW() - INTERVAL '1 minute'`)
	st.DB().Exec(`DELETE FROM js_plugin_events`)
	manager.queueTicks(ctx)
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events`); n != 0 {
		t.Fatalf("a disabled plugin was ticked")
	}
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_bindings WHERE next_tick_at > NOW()`); n != 1 {
		t.Fatal("a disabled plugin's binding was not postponed")
	}
}
