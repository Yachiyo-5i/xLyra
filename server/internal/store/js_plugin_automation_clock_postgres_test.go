package store

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// An event is due on the database clock alone. Enqueue stamps next_attempt_at
// with NOW(); DueEvents compares to NOW() and takes no Go time, so a test can
// no longer inject a shifted process clock. RetryEvent adds its delay with
// make_interval on that same clock.
func TestDueEventsAndRetryEventUseDatabaseClock(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO js_plugins (id, source) VALUES ('acme', 'uploaded')`,
		`INSERT INTO js_plugin_versions (plugin_id, version, manifest, package, package_sha256, status, selftest)
		 VALUES ('acme', '1.0.0', '{}', '\x00', 'sha', 'enabled', '{}')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed plugin: %v", err)
		}
	}
	repo := NewJSPluginRepository(db)
	assertClockSignature(t, "DueEvents", repo.DueEvents, 2)
	retryType := assertClockSignature(t, "RetryEvent", repo.RetryEvent, 4)
	if retryType.In(3) != reflect.TypeOf(time.Duration(0)) {
		t.Fatalf("RetryEvent delay type = %s, want time.Duration", retryType.In(3))
	}

	binding, err := repo.CreateAutomationBinding(ctx, JSPluginBinding{
		PluginID: "acme", Version: "1.0.0", Kind: "automation", Events: JSON(`["oauth.quota_synced"]`),
	}, []JSPluginBindingInput{{
		InputName: "account", EntityType: "oauth_connection", EntityID: "conn-1", EventSubject: true,
	}})
	if err != nil {
		t.Fatalf("create automation binding: %v", err)
	}

	dueSQL, retrySQL := captureClockSQL(t, db)
	const rounds = 30
	var newest int64
	for round := 0; round < rounds; round++ {
		queued, err := repo.EnqueueEvent(ctx, "oauth.quota_synced", "oauth_connection", "conn-1", JSON(`{}`))
		if err != nil || queued != 1 {
			t.Fatalf("round %d: queued %d events, err %v", round, queued, err)
		}
		due, err := repo.DueEvents(ctx, 100)
		if err != nil {
			t.Fatalf("round %d: DueEvents: %v", round, err)
		}
		var maxID int64
		for _, event := range due {
			if event.BindingID != binding.ID {
				t.Fatalf("round %d: event %d belongs to %s", round, event.ID, event.BindingID)
			}
			if event.ID > maxID {
				maxID = event.ID
			}
		}
		if maxID <= newest {
			t.Fatalf("round %d: event just queued was not due (due %d, previous max %d)", round, len(due), newest)
		}
		newest = maxID
	}
	assertDueSQLUsesDatabaseClock(t, dueSQL)

	if err := repo.RetryEvent(ctx, newest, "later", time.Hour); err != nil {
		t.Fatalf("retry in an hour: %v", err)
	}
	assertRetrySQLUsesDatabaseClock(t, retrySQL, time.Hour.Seconds())
	if delta := eventLead(t, db, newest); delta < 59*60 || delta > 61*60 {
		t.Fatalf("retry of 1h is %s ahead of the database clock, want about 1h", time.Duration(delta*float64(time.Second)))
	}
	due, err := repo.DueEvents(ctx, 100)
	if err != nil {
		t.Fatalf("DueEvents after 1h retry: %v", err)
	}
	if len(due) != rounds-1 || containsEvent(due, newest) {
		t.Fatalf("after 1h retry, due = %d events, contains held event %v", len(due), containsEvent(due, newest))
	}

	if err := repo.RetryEvent(ctx, newest, "soon", 1500*time.Millisecond); err != nil {
		t.Fatalf("retry in 1.5s: %v", err)
	}
	assertRetrySQLUsesDatabaseClock(t, retrySQL, 1.5)
	if delta := eventLead(t, db, newest); delta <= 1 || delta >= 2 {
		t.Fatalf("retry of 1.5s is %s ahead of the database clock, want between 1s and 2s", time.Duration(delta*float64(time.Second)))
	}
	due, err = repo.DueEvents(ctx, 100)
	if err != nil {
		t.Fatalf("DueEvents after 1.5s retry: %v", err)
	}
	if containsEvent(due, newest) {
		t.Fatal("an event retried for 1.5s was due at once")
	}

	if err := repo.RetryEvent(ctx, newest, "now", 0); err != nil {
		t.Fatalf("retry with no delay: %v", err)
	}
	assertRetrySQLUsesDatabaseClock(t, retrySQL, 0)
	if delta := eventLead(t, db, newest); delta > 0 {
		t.Fatalf("retry with no delay is %s ahead of the database clock, want due", time.Duration(delta*float64(time.Second)))
	}
	due, err = repo.DueEvents(ctx, 100)
	if err != nil {
		t.Fatalf("DueEvents after zero delay: %v", err)
	}
	if len(due) != rounds || !containsEvent(due, newest) {
		t.Fatalf("after zero delay, due = %d events, contains retried event %v", len(due), containsEvent(due, newest))
	}
	attempts, status, detail := eventState(t, db, newest)
	if attempts != 3 || status != JSPluginEventPending || detail != "now" {
		t.Fatalf("retried event attempts=%d status=%s detail=%q, want 3 pending now", attempts, status, detail)
	}

	closed := []string{JSPluginEventDone, JSPluginEventSkipped, JSPluginEventFailed}
	for i, event := range due {
		if err := repo.FinishEvent(ctx, event.ID, closed[i%len(closed)], "closed"); err != nil {
			t.Fatalf("finish event %d: %v", event.ID, err)
		}
	}
	for _, status := range closed {
		var n int64
		if err := db.Raw(`SELECT COUNT(*) FROM js_plugin_events WHERE status = ?`, status).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			t.Fatalf("no event was finished as %s", status)
		}
	}
	due, err = repo.DueEvents(ctx, 100)
	if err != nil {
		t.Fatalf("DueEvents after finish: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("finished events still due: %+v", due)
	}

	queued, err := repo.EnqueueEvent(ctx, "oauth.quota_synced", "oauth_connection", "conn-1", JSON(`{}`))
	if err != nil || queued != 1 {
		t.Fatalf("queued after finish = %d, %v", queued, err)
	}
	due, err = repo.DueEvents(ctx, 100)
	if err != nil || len(due) != 1 || due[0].ID <= newest {
		t.Fatalf("event queued after finished rows = %+v, %v", due, err)
	}
}

func assertClockSignature(t *testing.T, name string, fn any, args int) reflect.Type {
	t.Helper()
	typ := reflect.TypeOf(fn)
	if typ.Kind() != reflect.Func || typ.NumIn() != args {
		t.Fatalf("%s signature = %s, want %d arguments", name, typ, args)
	}
	for i := 0; i < typ.NumIn(); i++ {
		if typ.In(i) == reflect.TypeOf(time.Time{}) {
			t.Fatalf("%s argument %d is time.Time (%s)", name, i, typ)
		}
	}
	return typ
}

type capturedClockSQL struct {
	text string
	vars []any
}

func captureClockSQL(t *testing.T, db *gorm.DB) (due, retry *capturedClockSQL) {
	t.Helper()
	due = &capturedClockSQL{}
	retry = &capturedClockSQL{}
	const hook = "test:js_plugin_clock"
	record := func(dst *capturedClockSQL) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			text := tx.Statement.SQL.String()
			if !strings.Contains(text, "next_attempt_at") {
				return
			}
			dst.text = text
			dst.vars = append([]any(nil), tx.Statement.Vars...)
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(hook, record(due)); err != nil {
		t.Fatalf("capture DueEvents SQL: %v", err)
	}
	if err := db.Callback().Raw().After("gorm:raw").Register(hook, record(retry)); err != nil {
		t.Fatalf("capture RetryEvent SQL: %v", err)
	}
	t.Cleanup(func() {
		db.Callback().Query().Remove(hook)
		db.Callback().Raw().Remove(hook)
	})
	return due, retry
}

func assertDueSQLUsesDatabaseClock(t *testing.T, captured *capturedClockSQL) {
	t.Helper()
	if !strings.Contains(captured.text, "next_attempt_at <= NOW()") {
		t.Fatalf("DueEvents SQL = %q, want next_attempt_at <= NOW()", captured.text)
	}
	assertNoGoTime(t, "DueEvents", captured)
}

func assertRetrySQLUsesDatabaseClock(t *testing.T, captured *capturedClockSQL, seconds float64) {
	t.Helper()
	if !strings.Contains(captured.text, "NOW()") || !strings.Contains(captured.text, "make_interval(secs =>") {
		t.Fatalf("RetryEvent SQL = %q, want NOW() + make_interval", captured.text)
	}
	assertNoGoTime(t, "RetryEvent", captured)
	found := false
	for _, v := range captured.vars {
		f, ok := v.(float64)
		if ok && f == seconds {
			found = true
		}
	}
	if !found {
		t.Fatalf("RetryEvent vars = %#v, want %v seconds and no Go time", captured.vars, seconds)
	}
}

func assertNoGoTime(t *testing.T, name string, captured *capturedClockSQL) {
	t.Helper()
	if captured.text == "" {
		t.Fatalf("%s SQL was not captured", name)
	}
	for _, v := range captured.vars {
		switch v.(type) {
		case time.Time, *time.Time:
			t.Fatalf("%s SQL binds a Go time (%T) in %s vars=%#v", name, v, captured.text, captured.vars)
		}
	}
}

func eventLead(t *testing.T, db *gorm.DB, id int64) float64 {
	t.Helper()
	var delta float64
	err := db.Raw(`SELECT EXTRACT(EPOCH FROM (next_attempt_at - NOW()))::float8 FROM js_plugin_events WHERE id = ?`, id).Row().Scan(&delta)
	if err != nil {
		t.Fatalf("read next_attempt_at: %v", err)
	}
	return delta
}

func eventState(t *testing.T, db *gorm.DB, id int64) (attempts int, status, detail string) {
	t.Helper()
	err := db.Raw(`SELECT attempts, status, last_error FROM js_plugin_events WHERE id = ?`, id).Row().Scan(&attempts, &status, &detail)
	if err != nil {
		t.Fatalf("read event %d: %v", id, err)
	}
	return attempts, status, detail
}

func containsEvent(events []JSPluginEvent, id int64) bool {
	for _, event := range events {
		if event.ID == id {
			return true
		}
	}
	return false
}
