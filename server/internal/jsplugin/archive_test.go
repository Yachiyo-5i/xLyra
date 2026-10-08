package jsplugin

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
)

func TestReadPackageRejectsReservedPluginID(t *testing.T) {
	t.Parallel()
	manifest := validTestManifest("builtins", "quota_probe")
	source := `export const meta = { apiVersion: 1, id: "builtins", kind: "quota_probe" };
export function probe() { return { error: "empty" }; }`
	sum := HashSource(source)
	manifest = manifestWithHash(manifest, sum)
	raw := testPackageZip(manifest, source)
	_, err := ReadPackage(raw, true)
	if err == nil {
		t.Fatal("expected reserved id rejection")
	}
}

func TestReadPackageRejectsBuiltinPrefix(t *testing.T) {
	t.Parallel()
	raw := testPackageZip(manifestJSON(`{
		"id": "xlyra.quota.evil",
		"name": "evil",
		"version": "1.0.0",
		"apiVersion": 1,
		"hostApi": 1,
		"xlyra": ">=1.14.0",
		"kind": "quota_probe",
		"quotaProbe": { "baseURLMode": "as_is" },
		"sha256": { "plugin.js": "ignored" }
	}`), `export const meta = { apiVersion: 1, id: "xlyra.quota.evil", kind: "quota_probe" };
export function probe() { return { error: "nope" }; }`)
	_, err := ReadPackage(raw, true)
	if err == nil {
		t.Fatal("expected uploaded prefix rejection")
	}
}

func TestReadPackageRequiresFixturesForUploads(t *testing.T) {
	t.Parallel()
	manifest := validTestManifest("com.example.probe", "quota_probe")
	source := `export const meta = { apiVersion: 1, id: "com.example.probe", kind: "quota_probe" };
export function probe() { return { error: "empty" }; }`
	sum := HashSource(source)
	manifest = manifestWithHash(manifest, sum)
	raw := testPackageZipNoFixtures(manifest, source)
	_, err := ReadPackage(raw, true)
	if err == nil {
		t.Fatal("expected missing fixtures error")
	}
}

func manifestJSON(body string) string { return body }

func validTestManifest(id, kind string) string {
	return `{
		"id": "` + id + `",
		"name": "Test",
		"version": "1.0.0",
		"apiVersion": 1,
		"hostApi": 1,
		"xlyra": ">=1.14.0",
		"kind": "` + kind + `",
		"quotaProbe": { "baseURLMode": "as_is" },
		"sha256": { "plugin.js": "0000000000000000000000000000000000000000000000000000000000000000" }
	}`
}

func manifestWithHash(manifest, sum string) string {
	return stringsReplaceSHA(manifest, sum)
}

func stringsReplaceSHA(manifest, sum string) string {
	return stringsReplace(manifest, "0000000000000000000000000000000000000000000000000000000000000000", sum)
}

func stringsReplace(s, old, new string) string {
	return bytesReplace([]byte(s), old, new)
}

func bytesReplace(b []byte, old, new string) string {
	return string(bytes.ReplaceAll(b, []byte(old), []byte(new)))
}

func testPackageZip(manifest, source string) []byte {
	raw := testPackageZipNoFixtures(manifest, source)
	buf := &bytes.Buffer{}
	zr, _ := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	zw := zip.NewWriter(buf)
	for _, file := range zr.File {
		rc, _ := file.Open()
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		writeZip(zw, file.Name, string(data))
	}
	writeZip(zw, "fixtures/ok.json", `{"name":"ok","ctx":{"siteType":"newapi","baseURL":"https://example.com","credentialType":"api_key"},"responses":[],"expect":{"error":"empty"}}`)
	_ = zw.Close()
	return buf.Bytes()
}

func testPackageZipNoFixtures(manifest, source string) []byte {
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	writeZip(zw, "manifest.json", manifest)
	writeZip(zw, "plugin.js", source)
	_ = zw.Close()
	return buf.Bytes()
}

func writeZip(zw *zip.Writer, name, body string) {
	w, _ := zw.Create(name)
	_, _ = w.Write([]byte(body))
}
