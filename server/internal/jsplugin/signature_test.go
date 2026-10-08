package jsplugin

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"io"
	"testing"
)

func TestPackageSignatureRoundTrip(t *testing.T) {
	t.Parallel()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	manifest := validTestManifest("com.example.probe", "quota_probe")
	source := `export const meta = { apiVersion: 1, id: "com.example.probe", kind: "quota_probe" };
export function probe() { return { error: "empty" }; }`
	sum := HashSource(source)
	manifest = manifestWithHash(manifest, sum)
	raw := testPackageZip(manifest, source)
	signed, err := AttachSignature(raw, priv)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ReadPackage(signed, true)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Signer == "" {
		t.Fatal("expected signer fingerprint")
	}
	if pkg.SHA256 == "" {
		t.Fatal("expected package digest")
	}
}

func TestReadPackageRejectsInvalidSignature(t *testing.T) {
	t.Parallel()
	manifest := validTestManifest("com.example.probe", "quota_probe")
	source := `export const meta = { apiVersion: 1, id: "com.example.probe", kind: "quota_probe" };
export function probe() { return { error: "empty" }; }`
	sum := HashSource(source)
	manifest = manifestWithHash(manifest, sum)
	raw := testPackageZip(manifest, source)
	_, priv, _ := ed25519.GenerateKey(nil)
	signed, _ := AttachSignature(raw, priv)
	corrupt := replaceZipEntry(signed, signatureFileName, `{"alg":"ed25519","publicKey":"AAAA","sig":"AAAA"}`)
	_, err := ReadPackage(corrupt, true)
	if err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func replaceZipEntry(raw []byte, name, body string) []byte {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		panic(err)
	}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, file := range reader.File {
		rc, _ := file.Open()
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		entryName := file.Name
		if normalizeZipPath(file.Name) == name {
			data = []byte(body)
			entryName = name
		}
		w, _ := zw.Create(entryName)
		_, _ = w.Write(data)
	}
	_ = zw.Close()
	return buf.Bytes()
}
