package credential

import "testing"

func TestServiceEncryptDecryptRoundTripAndMasksSecret(t *testing.T) {
	t.Parallel()

	service := NewService("test-master-key")
	encrypted, masked, err := service.Encrypt("  sk-abcdefghijklmnopqrstuvwxyz  ")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if encrypted == "" || encrypted == "sk-abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("expected encrypted secret, got %q", encrypted)
	}
	if masked != "sk-a...wxyz" {
		t.Fatalf("masked = %q, want sk-a...wxyz", masked)
	}

	plain, err := service.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plain != "sk-abcdefghijklmnopqrstuvwxyz" {
		t.Fatalf("plain = %q", plain)
	}
}

func TestServiceEncryptRejectsEmptySecret(t *testing.T) {
	t.Parallel()

	if _, _, err := NewService("test-master-key").Encrypt(" \t "); err == nil {
		t.Fatal("expected empty secret to be rejected")
	}
}
