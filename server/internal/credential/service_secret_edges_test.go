package credential

import "testing"

func TestServiceEncryptMasksTrimmedShortSecret(t *testing.T) {
	t.Parallel()

	service := NewService("test-master-key")

	encrypted, masked, err := service.Encrypt("  short  ")

	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if masked != "****" {
		t.Fatalf("masked = %q, want ****", masked)
	}
	plain, err := service.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plain != "short" {
		t.Fatalf("plain = %q, want short", plain)
	}
}

func TestServiceDecryptRejectsInvalidPayloadAndWrongMasterKey(t *testing.T) {
	t.Parallel()

	service := NewService("test-master-key")
	if _, err := service.Decrypt("not-valid-secret-payload"); err == nil {
		t.Fatal("expected invalid payload to fail decrypt")
	}

	encrypted, _, err := service.Encrypt("sensitive-token")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := NewService("other-master-key").Decrypt(encrypted); err == nil {
		t.Fatal("expected wrong master key to fail decrypt")
	}
}
