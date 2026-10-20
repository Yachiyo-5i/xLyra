package jsplugin

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestSiteProviderLifecycleGuards(t *testing.T) {
	st := openTestStore(t)
	mgr := NewManager(st, nil)
	ctx := context.Background()
	const id = "acme.example"
	const siteType = "acme.example/example"
	const otherType = "acme.example/other"
	hosts := []string{"api.example.com", "files.example.com"}

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Upload(ctx, "", signedSiteProviderVersion(t, priv, "1.0.0", siteType, "// v1\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Upload(ctx, "", signedSiteProviderVersion(t, priv, "1.1.0", siteType, "// v1.1\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.AddTrustedKey(ctx, "", "acme", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}

	repo := store.NewSiteRepository(st.DB())
	emptyCount, err := repo.CountBySiteType(ctx, " ")
	if err != nil || emptyCount != 0 {
		t.Fatalf("empty count = %d, %v", emptyCount, err)
	}
	active := createLifecycleSite(t, st, " Acme.Example/Example ", true)
	disabled := createLifecycleSite(t, st, "ACME.EXAMPLE/example", false)
	deleted := createLifecycleSite(t, st, siteType, true)
	if err := repo.Delete(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}
	_ = createLifecycleSite(t, st, "openai", true)
	count, err := repo.CountBySiteType(ctx, " acme.example/example ")
	if err != nil || count != 2 {
		t.Fatalf("count = %d, %v", count, err)
	}

	if err := mgr.DeleteVersion(ctx, id, "1.0.0"); err != nil {
		t.Fatalf("delete non-last version: %v", err)
	}
	err = mgr.DeleteVersion(ctx, id, "1.1.0")
	var inUse *PluginInUseError
	if !errors.As(err, &inUse) || inUse.PluginID != id || inUse.SiteType != siteType || inUse.Count != 2 {
		t.Fatalf("delete last version: %#v %v", inUse, err)
	}
	if err := mgr.Enable(ctx, id, "1.1.0", EnableOptions{ConfirmedHosts: hosts}); err != nil {
		t.Fatalf("enable same site type: %v", err)
	}
	if err := mgr.DeleteVersion(ctx, id, "1.1.0"); !errors.Is(err, ErrVersionEnabled) {
		t.Fatalf("delete enabled version: %v", err)
	}

	if _, err := mgr.Upload(ctx, "", signedSiteProviderVersion(t, priv, "1.2.0", otherType, "// v1.2\n")); err != nil {
		t.Fatal(err)
	}
	err = mgr.Enable(ctx, id, "1.2.0", EnableOptions{ConfirmedHosts: hosts})
	var locked *SiteTypeChangeError
	if !errors.As(err, &locked) || locked.PluginID != id || locked.From != siteType || locked.To != otherType || locked.Count != 2 {
		t.Fatalf("change site type: %#v %v", locked, err)
	}
	if err := repo.Delete(ctx, active.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, disabled.ID); err != nil {
		t.Fatal(err)
	}
	cleared, err := repo.CountBySiteType(ctx, siteType)
	if err != nil || cleared != 0 {
		t.Fatalf("count after delete = %d, %v", cleared, err)
	}
	if err := mgr.Enable(ctx, id, "1.2.0", EnableOptions{ConfirmedHosts: hosts}); err != nil {
		t.Fatalf("enable changed site type with no sites: %v", err)
	}

	replacement := createLifecycleSite(t, st, otherType, true)
	if err := mgr.Disable(ctx, id); err != nil {
		t.Fatalf("disable while sites exist: %v", err)
	}
	err = mgr.DeletePlugin(ctx, id)
	inUse = nil
	if !errors.As(err, &inUse) || inUse.SiteType != otherType || inUse.Count != 1 {
		t.Fatalf("delete plugin: %#v %v", inUse, err)
	}
	if err := repo.Delete(ctx, replacement.ID); err != nil {
		t.Fatal(err)
	}
	if err := mgr.DeletePlugin(ctx, id); err != nil {
		t.Fatalf("delete plugin after sites are gone: %v", err)
	}
	if err := mgr.DeletePlugin(ctx, id); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("delete missing plugin: %v", err)
	}
}

func signedSiteProviderVersion(t *testing.T, priv ed25519.PrivateKey, version, siteType, extra string) []byte {
	t.Helper()
	source := meta("acme.example", KindSiteProvider) + extra
	manifest := siteProviderManifest(source)
	manifest.Version = version
	manifest.SiteProvider.SiteType = siteType
	raw, err := AttachSignature(siteProviderZip(t, manifest, source, nil), priv)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func createLifecycleSite(t *testing.T, st *store.Store, siteType string, enabled bool) store.Site {
	t.Helper()
	site, err := store.NewSiteRepository(st.DB()).Create(context.Background(), store.CreateSiteParams{
		Name:            "site-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Slug:            "site-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		SiteType:        siteType,
		BaseURL:         "https://api.example.com/v1",
		Status:          "active",
		Enabled:         enabled,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return site
}
