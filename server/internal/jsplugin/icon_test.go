package jsplugin

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestSiteProviderIconFromVersions(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	pngNext := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 2}
	webp := []byte("RIFF\x00\x00\x00\x00WEBP")
	base := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)

	enabled := iconVersion(t, store.JSPluginStatusEnabled, base, "icon.png", png, "")
	newer := iconVersion(t, store.JSPluginStatusVerified, base.Add(time.Hour), "icon.png", pngNext, "// newer\n")
	disabled := iconVersion(t, store.JSPluginStatusDisabled, base, "mark.webp", webp, "// disabled\n")
	older := iconVersion(t, store.JSPluginStatusDisabled, base, "icon.png", png, "// old\n")
	latest := iconVersion(t, store.JSPluginStatusVerified, base.Add(2*time.Hour), "icon.png", pngNext, "// latest\n")
	noIcon := iconVersion(t, store.JSPluginStatusEnabled, base, "", nil, "// plain\n")
	missing := iconVersion(t, store.JSPluginStatusEnabled, base, "icon.png", nil, "// missing\n")
	other := store.JSPluginVersion{
		PluginID:  "acme.probe",
		Version:   "1.0.0",
		Package:   quotaProbeIconPackage(t),
		Status:    store.JSPluginStatusEnabled,
		CreatedAt: base,
	}

	got, err := siteProviderIconFromVersions([]store.JSPluginVersion{newer, enabled})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "image/png" || string(got.Bytes) != string(png) || got.SHA256 != enabled.PackageSHA256 || len(got.SHA256) != 64 {
		t.Fatalf("enabled icon = %q %q %d", got.ContentType, got.SHA256, len(got.Bytes))
	}

	got, err = siteProviderIconFromVersions([]store.JSPluginVersion{disabled})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "image/webp" || string(got.Bytes) != string(webp) {
		t.Fatalf("disabled icon = %q %d", got.ContentType, len(got.Bytes))
	}

	got, err = siteProviderIconFromVersions([]store.JSPluginVersion{older, latest})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentType != "image/png" || string(got.Bytes) != string(pngNext) || got.SHA256 != latest.PackageSHA256 {
		t.Fatalf("latest upload was not chosen: %q %q", got.ContentType, got.SHA256)
	}

	for _, row := range []store.JSPluginVersion{noIcon, missing, other} {
		if _, err := siteProviderIconFromVersions([]store.JSPluginVersion{row}); !errors.Is(err, ErrPluginIconNotFound) {
			t.Fatalf("status %s icon %q: err = %v", row.Status, row.PluginID, err)
		}
	}
	if _, err := siteProviderIconFromVersions(nil); !errors.Is(err, ErrPluginIconNotFound) {
		t.Fatalf("missing plugin: %v", err)
	}

	pkg, err := ReadPackage(enabled.Package, true)
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := CompilePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if plugin.PackageSHA256() != pkg.SHA256 {
		t.Fatalf("PackageSHA256 = %q, want %q", plugin.PackageSHA256(), pkg.SHA256)
	}
}

func iconVersion(t *testing.T, status string, at time.Time, iconName string, body []byte, extra string) store.JSPluginVersion {
	t.Helper()
	source := meta("acme.example", KindSiteProvider) + extra
	manifest := siteProviderManifest(source)
	var files map[string][]byte
	if iconName != "" {
		manifest.SiteProvider.Display.Icon = iconName
		if body != nil {
			files = map[string][]byte{iconName: body}
		}
	}
	raw := siteProviderZip(t, manifest, source, files)
	row := store.JSPluginVersion{
		PluginID:  "acme.example",
		Version:   manifest.Version,
		Package:   raw,
		Status:    status,
		CreatedAt: at,
	}
	if pkg, err := ReadPackage(raw, true); err == nil {
		row.PackageSHA256 = pkg.SHA256
	}
	return row
}

func quotaProbeIconPackage(t *testing.T) []byte {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"acme.probe\", kind: \"quota_probe\" };\nexport function probe() { return { error: \"empty\" }; }\n"
	manifest := Manifest{
		ID: "acme.probe", Name: "Probe", Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		Kind:       KindQuotaProbe,
		QuotaProbe: QuotaProbeSection{BaseURLMode: "as_is"},
		SHA256:     map[string]string{"plugin.js": hashSource(source)},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := BuildPackage(map[string][]byte{
		"manifest.json":    encoded,
		"plugin.js":        []byte(source),
		"fixtures/ok.json": []byte(`{"name":"ok"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackage(raw, true); err != nil {
		t.Fatalf("ReadPackage quota probe: %v", err)
	}
	return raw
}
