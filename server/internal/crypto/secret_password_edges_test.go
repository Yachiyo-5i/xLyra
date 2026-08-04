package crypto

import (
	"encoding/base64"
	"testing"
)

func TestComparePasswordRejectsMalformedHash(t *testing.T) {
	t.Parallel()

	if ComparePassword("not-a-bcrypt-hash", "anything") {
		t.Fatal("expected malformed bcrypt hash to be rejected")
	}
}

func TestDecryptSecretRejectsInvalidEncodingAndTamperedCiphertext(t *testing.T) {
	t.Parallel()

	if _, err := DecryptSecret("master-key", "$$$"); err == nil {
		t.Fatal("expected invalid base64 payload to fail decrypt")
	}

	encrypted, err := EncryptSecret("master-key", "sensitive-token")
	if err != nil {
		t.Fatalf("EncryptSecret: %v", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("decode encrypted payload: %v", err)
	}
	payload[len(payload)-1] ^= 0x01
	tampered := base64.RawURLEncoding.EncodeToString(payload)
	if _, err := DecryptSecret("master-key", tampered); err == nil {
		t.Fatal("expected tampered payload to fail decrypt")
	}
}

func TestMaskSecretBoundaryLengths(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "****"},
		{name: "eight characters", in: "12345678", want: "****"},
		{name: "nine characters", in: "123456789", want: "1234...6789"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := MaskSecret(tc.in); got != tc.want {
				t.Fatalf("MaskSecret(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
