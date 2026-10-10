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
    actions: ctx.inputs.keys.map((k) => ({ type: "apikey.reset_usage", target: k.id, scope: "total", idempotencyKey: String(after.reset_at) })),
    state: { lastReset: after.reset_at, level: ctx.inputs.level, account: ctx.inputs.account.name },
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
    "inputs": [
      { "name": "account", "type": "oauth_connection", "providers": ["codex", "claude_code"], "eventSubject": true },
      { "name": "keys", "type": "api_key", "multiple": true, "requires": "finite_total_quota" },
      { "name": "level", "type": "string", "default": "weekly" }
    ]
  },
  "sha256": { "plugin.js": "` + hashSource(rolloverPlugin) + `" }
}`
	fixture := `{"name":"rollover","ctx":{"now":1,"inputs":{"account":{"type":"oauth_connection","id":"c","name":"a@b","fields":{}},"keys":[{"type":"api_key","id":"k","name":"n","fields":{}}],"level":"weekly"}},"input":{"type":"oauth.quota_synced","subject":{"type":"oauth_connection","id":"c","name":"a@b","fields":{}},"previous":{"weekly":{"reset_at":1}},"current":{"weekly":{"reset_at":2}}},"expect":{"result":{"actions":[{"type":"apikey.reset_usage","target":"k","scope":"total","idempotencyKey":"2"}]}}}`
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
	other, _ := store.NewOAuthConnectionRepository(st.DB()).Save(ctx, store.OAuthConnection{
		Provider: "antigravity", Email: "other@example.com", Status: "connected", RawProfile: store.JSON("{}"), Metadata: store.JSON("{}"),
	})
	// The picker lists the accounts the admin pages list: those with a site.
	for slug, account := range map[string]store.OAuthConnection{"codex": conn, "other": other} {
		siteID := uuid.NewString()
		if err := st.DB().Exec(`INSERT INTO sites (id, name, slug, site_type, base_url) VALUES (?, ?, ?, 'codex', 'https://example.com')`, siteID, slug, slug).Error; err != nil {
			t.Fatalf("seed site: %v", err)
		}
		if err := st.DB().Exec(`UPDATE oauth_connections SET site_id = ? WHERE id = ?`, siteID, account.ID).Error; err != nil {
			t.Fatalf("attach an account to its site: %v", err)
		}
	}
	picks := func(account string, keys ...string) map[string]any {
		list := make([]any, len(keys))
		for i, key := range keys {
			list[i] = key
		}
		return map[string]any{"account": account, "keys": list}
	}
	with := func(inputs map[string]any, name string, value any) map[string]any {
		inputs[name] = value
		return inputs
	}
	for name, in := range map[string]AutomationBindingInput{
		"unlimited key":       {Inputs: picks(conn.ID.String(), unlimited.ID.String())},
		"no keys":             {Inputs: picks(conn.ID.String())},
		"no account":          {Inputs: map[string]any{"keys": []any{finite.ID.String()}}},
		"missing key":         {Inputs: picks(conn.ID.String(), uuid.NewString())},
		"unknown input":       {Inputs: with(picks(conn.ID.String(), finite.ID.String()), "nope", 1)},
		"wrong provider":      {Inputs: picks(other.ID.String(), finite.ID.String())},
		"unknown account":     {Inputs: picks(uuid.NewString(), finite.ID.String())},
		"two accounts":        {Inputs: with(picks(conn.ID.String(), finite.ID.String()), "account", []any{conn.ID.String(), other.ID.String()})},
		"parameter of a type": {Inputs: with(picks(conn.ID.String(), finite.ID.String()), "level", float64(3))},
		"the key twice":       {Inputs: picks(conn.ID.String(), finite.ID.String(), finite.ID.String())},
	} {
		var inputErr *AutomationInputError
		if _, err := manager.CreateAutomation(ctx, "acme-reset", in); !errors.As(err, &inputErr) {
			t.Errorf("%s: err = %v, want an input error", name, err)
		}
	}
	binding, err := manager.CreateAutomation(ctx, "acme-reset", AutomationBindingInput{Inputs: picks(conn.ID.String(), finite.ID.String())})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}

	emit := func(resetAt float64) {
		t.Helper()
		err := manager.EmitEvent(ctx, st.DB(), EmittedEvent{
			Type: "oauth.quota_synced", EntityType: EntityOAuthConnection, EntityID: conn.ID.String(),
			Previous: map[string]any{"weekly": map[string]any{"reset_at": resetAt - 1}},
			Current:  map[string]any{"weekly": map[string]any{"reset_at": resetAt}},
		})
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
	}

	// A different account's event reaches nobody.
	if err := manager.EmitEvent(ctx, st.DB(), EmittedEvent{Type: "oauth.quota_synced", EntityType: EntityOAuthConnection, EntityID: uuid.NewString()}); err != nil {
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
	inputsByName := map[string]AutomationInputView{}
	for _, input := range views[0].Inputs {
		inputsByName[input.Name] = input
	}
	if a := inputsByName["account"]; len(a.Entities) != 1 || a.Entities[0].Name != "owner@example.com" ||
		len(inputsByName["keys"].Entities) != 1 || inputsByName["keys"].Entities[0].Name != "finite" ||
		inputsByName["level"].Value != "weekly" ||
		views[0].State["lastReset"] != float64(200) || views[0].State["level"] != "weekly" || views[0].State["account"] != "owner@example.com" {
		t.Fatalf("view = %+v", views[0])
	}
	options, err := manager.AutomationOptions(ctx, "acme-reset", "keys")
	if err != nil || len(options) != 2 {
		t.Fatalf("options = %+v, %v", options, err)
	}
	for _, option := range options {
		if (option.Name == "unlimited") != option.Disabled || (option.Disabled && option.Reason == "") {
			t.Fatalf("option %+v: only the key without a finite quota is ruled out, with a reason", option)
		}
	}
	accountOptions, err := manager.AutomationOptions(ctx, "acme-reset", "account")
	if err != nil || len(accountOptions) != 2 {
		t.Fatalf("account options = %+v, %v", accountOptions, err)
	}
	for _, option := range accountOptions {
		if (option.Description == "antigravity") != option.Disabled {
			t.Fatalf("account option %+v: only the antigravity account is ruled out", option)
		}
	}
	if _, err := manager.AutomationOptions(ctx, "acme-reset", "level"); err == nil {
		t.Fatal("a parameter was offered as a list of objects")
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
	if n := countRows(t, st, `SELECT COUNT(*) FROM js_plugin_events`) + countRows(t, st, `SELECT COUNT(*) FROM js_plugin_binding_inputs`); n != 0 {
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
    actions: [{ type: "notify", level: "warn", message: "weekly remaining " + (weekly ? weekly.remaining_percent : "unknown") + "% for " + event.subject.name }],
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
    "inputs": [ { "name": "account", "type": "oauth_connection", "eventSubject": true } ]
  },
  "sha256": { "plugin.js": "` + hashSource(tickPlugin) + `" }
}`
	fixture := `{"name":"tick","ctx":{"now":1},"input":{"type":"schedule.tick","subject":{"type":"oauth_connection","id":"c","name":"a@b","fields":{}},"current":{"quota":{"weekly":{"remaining_percent":40}}}},"expect":{"result":{"actions":[{"type":"notify","level":"warn","message":"weekly remaining 40% for a@b"}],"state":{"ticks":1}}}}`
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
	binding, err := manager.CreateAutomation(ctx, "acme-tick", AutomationBindingInput{Inputs: map[string]any{"account": conn.ID.String()}})
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
