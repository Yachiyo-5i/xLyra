package jsplugin

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

func TestParseEd25519PublicKeyAcceptsCommonEncodings(t *testing.T) {
	t.Parallel()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	std := base64.StdEncoding.EncodeToString(pub)
	cases := map[string]string{
		"std":           std,
		"raw std":       base64.RawStdEncoding.EncodeToString(pub),
		"url":           base64.URLEncoding.EncodeToString(pub),
		"raw url":       base64.RawURLEncoding.EncodeToString(pub),
		"keygen prefix": "public_key=" + std,
		"whitespace":    "  " + std[:20] + "\n" + std[20:] + "  ",
	}
	for name, input := range cases {
		got, err := ParseEd25519PublicKey(input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, pub) {
			t.Fatalf("%s: decoded key mismatch", name)
		}
	}
}

func TestParseEd25519PublicKeyRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"empty":      "",
		"not base64": "!!!!",
		"too short":  base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		if _, err := ParseEd25519PublicKey(input); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}
