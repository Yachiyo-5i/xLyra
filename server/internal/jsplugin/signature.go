package jsplugin

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

var ErrUnsignedPackage = errors.New("unsigned plugin package")

// PackageSignature is the optional detached signature for an uploaded .xlp.
type PackageSignature struct {
	Algorithm string
	Signer    string
	Raw       []byte
}

func parsePackageSignature(raw string) (PackageSignature, error) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return PackageSignature{}, fmt.Errorf("signature is empty")
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return PackageSignature{}, fmt.Errorf("signature must be algorithm:payload")
	}
	algorithm := strings.TrimSpace(parts[0])
	payload := strings.TrimSpace(parts[1])
	if algorithm == "" || payload == "" {
		return PackageSignature{}, fmt.Errorf("signature must be algorithm:payload")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return PackageSignature{}, fmt.Errorf("signature payload is not base64: %w", err)
	}
	return PackageSignature{Algorithm: algorithm, Signer: algorithm, Raw: decoded}, nil
}

func verifyPackageSignature(digestHex string, sig PackageSignature) error {
	if sig.Algorithm != "ed25519" {
		return fmt.Errorf("unsupported signature algorithm %q", sig.Algorithm)
	}
	digest, err := hex.DecodeString(strings.TrimSpace(digestHex))
	if err != nil {
		return fmt.Errorf("invalid package digest: %w", err)
	}
	for _, key := range trustedPackagePublicKeys() {
		if ed25519.Verify(ed25519.PublicKey(key), digest, sig.Raw) {
			return nil
		}
	}
	return fmt.Errorf("package signature verification failed")
}

func trustedPackagePublicKeys() [][]byte {
	raw := strings.TrimSpace(os.Getenv("JSPLUGIN_PACKAGE_PUBLIC_KEYS"))
	if raw == "" {
		return nil
	}
	var keys [][]byte
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(part)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			continue
		}
		keys = append(keys, decoded)
	}
	return keys
}

func packageRequiresUnsignedConfirmation(signer string) bool {
	if strings.TrimSpace(signer) != "" {
		return false
	}
	return len(trustedPackagePublicKeys()) > 0
}
