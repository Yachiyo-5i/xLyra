package jsplugin

import (
	"context"
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestStartSyncIgnoresNonPositiveInterval(t *testing.T) {
	var none *Manager
	none.StartSync(context.Background(), time.Second)

	m := &Manager{}
	m.StartSync(nil, time.Second)
	m.StartSync(context.Background(), 0)
	m.StartSync(context.Background(), -time.Second)
	if m.syncStopped != nil {
		t.Fatal("non-positive interval started catalog sync")
	}
}

func TestCatalogSyncPropagatesEnableAndDisable(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	a := NewManager(st, &Catalog{})
	b := NewManager(st, &Catalog{})
	repo := store.NewJSPluginRepository(st.DB())

	if _, err := a.Upload(ctx, "", tickPackage(t)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if gen, err := repo.CatalogGeneration(ctx); err != nil || gen != 0 {
		t.Fatalf("upload generation = %d, %v, want 0", gen, err)
	}
	if err := a.Enable(ctx, "acme-tick", "1.0.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionNotify}}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, ok := b.Catalog().Registry().ByPluginID("acme-tick"); ok {
		t.Fatal("peer saw the plugin before a sync")
	}
	if err := b.syncOnce(ctx); err != nil {
		t.Fatalf("sync after enable: %v", err)
	}
	if _, ok := b.Catalog().Registry().ByPluginID("acme-tick"); !ok {
		t.Fatal("peer did not load the enabled plugin")
	}

	if err := a.Disable(ctx, "acme-tick"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := b.syncOnce(ctx); err != nil {
		t.Fatalf("sync after disable: %v", err)
	}
	if _, ok := b.Catalog().Registry().ByPluginID("acme-tick"); ok {
		t.Fatal("peer still has the disabled plugin")
	}

	before := b.catalog.Generation()
	if err := b.Enable(ctx, "acme-tick", "1.0.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionNotify}}); err != nil {
		t.Fatalf("peer enable: %v", err)
	}
	if b.catalog.Generation() == before {
		t.Fatal("enable did not reload the peer")
	}
	after := b.catalog.Generation()
	if err := b.syncOnce(ctx); err != nil {
		t.Fatalf("sync after own enable: %v", err)
	}
	if b.catalog.Generation() != after {
		t.Fatalf("own enable caused another reload, generation %d -> %d", after, b.catalog.Generation())
	}
}

func TestCatalogSyncLoopSurvivesReadFailure(t *testing.T) {
	st := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := NewManager(st, &Catalog{})
	b := NewManager(st, &Catalog{})
	ticks := make(chan time.Time)
	ack := make(chan struct{})
	b.launchSync(ctx, time.Hour, ticks, ack)
	waitCatalogGen(t, b, 1)

	if err := st.DB().Exec(`DROP TABLE js_plugin_catalog_state`).Error; err != nil {
		t.Fatalf("drop catalog state: %v", err)
	}
	ticks <- time.Now()
	waitAck(t, ack)

	if err := st.DB().Exec(`
		CREATE TABLE js_plugin_catalog_state (
			id SMALLINT PRIMARY KEY CHECK (id = 1),
			generation BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`).Error; err != nil {
		t.Fatalf("restore catalog state: %v", err)
	}
	if err := st.DB().Exec(`INSERT INTO js_plugin_catalog_state (id, generation) VALUES (1, 0)`).Error; err != nil {
		t.Fatalf("restore catalog row: %v", err)
	}
	if _, err := a.Upload(ctx, "", tickPackage(t)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := a.Enable(ctx, "acme-tick", "1.0.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionNotify}}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	ticks <- time.Now()
	waitAck(t, ack)
	if _, ok := b.Catalog().Registry().ByPluginID("acme-tick"); !ok {
		t.Fatal("next sync round did not load the plugin")
	}

	cancel()
	select {
	case <-b.syncStopped:
	case <-time.After(2 * time.Second):
		t.Fatal("catalog sync did not exit after cancel")
	}
}

func TestPrimeCatalogSyncRetriesAfterFailedReload(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	a := NewManager(st, &Catalog{})
	b := NewManager(st, &Catalog{})
	if _, err := a.Upload(ctx, "", tickPackage(t)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	if err := a.Enable(ctx, "acme-tick", "1.0.0", EnableOptions{ConfirmUntrusted: true, GrantPermissions: []string{ActionNotify}}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	gen, err := store.NewJSPluginRepository(st.DB()).CatalogGeneration(ctx)
	if err != nil || gen == 0 {
		t.Fatalf("generation = %d, %v", gen, err)
	}
	if err := st.DB().Exec(`ALTER TABLE js_plugin_versions RENAME TO js_plugin_versions_hidden`).Error; err != nil {
		t.Fatalf("hide versions: %v", err)
	}
	b.primeCatalogSync(ctx)
	if _, ok := b.Catalog().Registry().ByPluginID("acme-tick"); ok {
		t.Fatal("reload succeeded while versions were hidden")
	}
	if err := st.DB().Exec(`ALTER TABLE js_plugin_versions_hidden RENAME TO js_plugin_versions`).Error; err != nil {
		t.Fatalf("restore versions: %v", err)
	}
	if err := b.syncOnce(ctx); err != nil {
		t.Fatalf("sync after failed prime: %v", err)
	}
	if _, ok := b.Catalog().Registry().ByPluginID("acme-tick"); !ok {
		t.Fatal("next sync did not reload after a failed prime")
	}
}

func waitCatalogGen(t *testing.T, m *Manager, min int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m.catalog.Generation() >= min {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("catalog generation = %d, want >= %d", m.catalog.Generation(), min)
}

func waitAck(t *testing.T, ack <-chan struct{}) {
	t.Helper()
	select {
	case <-ack:
	case <-time.After(10 * time.Second):
		t.Fatal("catalog sync did not finish a round")
	}
}
