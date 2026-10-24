package jsplugin

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetCatalogListenerNilManager(t *testing.T) {
	t.Parallel()
	var manager *Manager
	manager.SetCatalogListener(func(*Registry) {})
	manager.SetCatalogListener(nil)
}

func TestCallCatalogListener(t *testing.T) {
	called := false
	callCatalogListener(func(*Registry) { called = true }, nil)
	if called {
		t.Fatal("nil registry called the listener")
	}
	callCatalogListener(nil, NewRegistry())

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	callCatalogListener(func(*Registry) { panic("listener boom") }, NewRegistry())
	if !strings.Contains(logs.String(), "panicked") {
		t.Fatalf("log = %s", logs.String())
	}
}

func TestCatalogListenerReload(t *testing.T) {
	st := openTestStore(t)
	manager := NewManager(st, &Catalog{})
	ctx := context.Background()

	var calls atomic.Int32
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	manager.SetCatalogListener(func(*Registry) {
		calls.Add(1)
		panic("listener boom")
	})
	if err := manager.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	if !strings.Contains(logs.String(), "panicked") {
		t.Fatalf("log = %s", logs.String())
	}
	if manager.Catalog().Registry() == nil {
		t.Fatal("panic dropped the catalog")
	}

	manager.SetCatalogListener(nil)
	if err := manager.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("nil listener was called")
	}

	entered := make(chan struct{})
	manager.SetCatalogListener(func(*Registry) {
		close(entered)
		done := make(chan struct{})
		go func() {
			manager.SetCatalogListener(func(*Registry) { calls.Add(1) })
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("SetCatalogListener blocked inside the listener")
		}
	})
	if err := manager.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	default:
		t.Fatal("listener was not called")
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	before := calls.Load()
	if err := manager.Reload(canceled); err == nil {
		t.Fatal("canceled reload succeeded")
	}
	if calls.Load() != before {
		t.Fatal("listener ran after a failed reload")
	}
}

func TestCatalogListenerPeerSync(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	primary := NewManager(st, &Catalog{})
	peer := NewManager(st, &Catalog{})
	id := "acme.example"

	var mu sync.Mutex
	var primarySeen, peerSeen []bool
	remember := func(dst *[]bool) func(*Registry) {
		return func(registry *Registry) {
			_, ok := registry.ByPluginID(id)
			mu.Lock()
			*dst = append(*dst, ok)
			mu.Unlock()
		}
	}
	primary.SetCatalogListener(remember(&primarySeen))
	peer.SetCatalogListener(remember(&peerSeen))

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	source := meta(id, KindSiteProvider)
	manifest := siteProviderManifest(source)
	signed, err := AttachSignature(siteProviderZip(t, manifest, source, nil), priv)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := primary.Upload(ctx, "", signed); err != nil {
		t.Fatal(err)
	}
	if _, err := primary.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if err := primary.Enable(ctx, id, "1.0.0", EnableOptions{ConfirmedHosts: manifest.SiteProvider.Hosts}); err != nil {
		t.Fatal(err)
	}
	if len(primarySeen) != 1 || !primarySeen[0] {
		t.Fatalf("enable listener = %v", primarySeen)
	}
	if registry := peer.Catalog().Registry(); registry != nil {
		if _, ok := registry.ByPluginID(id); ok {
			t.Fatal("peer saw the plugin before sync")
		}
	}
	if err := peer.syncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(peerSeen) != 1 || !peerSeen[0] {
		t.Fatalf("sync listener = %v", peerSeen)
	}

	if err := primary.Disable(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(primarySeen) != 2 || primarySeen[1] {
		t.Fatalf("disable listener = %v", primarySeen)
	}
	if err := peer.syncOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(peerSeen) != 2 || peerSeen[1] {
		t.Fatalf("sync after disable = %v", peerSeen)
	}
}
