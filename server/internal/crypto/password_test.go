package crypto

import "testing"

func TestHashPasswordAndComparePassword(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("CorrectHorseBatteryStaple123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" || hash == "CorrectHorseBatteryStaple123" {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}
	if !ComparePassword(hash, "CorrectHorseBatteryStaple123") {
		t.Fatal("expected password to match generated hash")
	}
	if ComparePassword(hash, "wrong-password") {
		t.Fatal("expected wrong password to be rejected")
	}
}
