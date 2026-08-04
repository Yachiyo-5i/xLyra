package crypto

import "testing"

func TestEncryptSecretDecryptSecretRoundTrip(t *testing.T) {
	t.Parallel()

	encrypted, err := EncryptSecret("master-key", "sensitive-token")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if encrypted == "" || encrypted == "sensitive-token" {
		t.Fatalf("expected encrypted payload, got %q", encrypted)
	}

	plain, err := DecryptSecret("master-key", encrypted)
	if err != nil {
		t.Fatalf("DecryptSecret: %v", err)
	}
	if plain != "sensitive-token" {
		t.Fatalf("plain = %q, want sensitive-token", plain)
	}
}

func TestDecryptSecretRejectsWrongMasterKeyAndShortPayload(t *testing.T) {
	t.Parallel()

	encrypted, err := EncryptSecret("master-key", "sensitive-token")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	if _, err := DecryptSecret("other-master-key", encrypted); err == nil {
		t.Fatal("expected wrong master key to fail decrypt")
	}
	if _, err := DecryptSecret("master-key", "abc"); err == nil {
		t.Fatal("expected malformed encrypted payload to fail decrypt")
	}
}

func TestMaskSecret(t *testing.T) {
	t.Parallel()

	if got := MaskSecret("short"); got != "****" {
		t.Fatalf("MaskSecret(short) = %q, want ****", got)
	}
	if got := MaskSecret("1234567890abcdef"); got != "1234...cdef" {
		t.Fatalf("MaskSecret(long) = %q, want 1234...cdef", got)
	}
}
