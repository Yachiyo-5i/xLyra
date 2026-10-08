package jsplugin

import "testing"

func TestCanonicalPackageDigestIgnoresSignatureFile(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"manifest.json":   []byte("{}"),
		"plugin.js":       []byte("x"),
		"signature.json":  []byte(`{"alg":"ed25519"}`),
		"fixtures/a.json": []byte("{}"),
	}
	d1, err := CanonicalPackageDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	delete(files, "signature.json")
	d2, err := CanonicalPackageDigest(files)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest changed when only signature.json differed: %s vs %s", d1, d2)
	}
}
