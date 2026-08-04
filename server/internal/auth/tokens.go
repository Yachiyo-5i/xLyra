package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

const apiKeyPublicPrefix = "xlyra-"
const apiKeyCompatiblePrefix = "sk-"
const adminAccessTokenPrefix = "xlyra-admin-"
const apiKeyGeneratedKind = "generated"
const apiKeyCustomKind = "custom"
const apiKeySecretLength = 48

func newToken(prefix string, byteLen int) (string, error) {
	raw := make([]byte, byteLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func apiKeyPrefix(key string) string {
	const prefix = apiKeyPublicPrefix
	if strings.HasPrefix(key, prefix) {
		secret := strings.TrimPrefix(key, prefix)
		if len(secret) <= 8 {
			return prefix + secret
		}

		return prefix + secret[:8]
	}

	if len(key) <= 16 {
		return key
	}

	return key[:16]
}

func apiKeyStoragePrefix(key string, keyKind string) string {
	if keyKind == apiKeyCustomKind {
		return "custom-" + hashToken(key)[:12]
	}
	return apiKeyPrefix(key)
}

func generatedAPIKeyAlias(key string) (string, bool) {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, apiKeyCompatiblePrefix) {
		return "", false
	}
	secret := strings.TrimPrefix(key, apiKeyCompatiblePrefix)
	if !validGeneratedAPIKeySecret(secret) {
		return "", false
	}
	return apiKeyPublicPrefix + secret, true
}

func validGeneratedAPIKeySecret(secret string) bool {
	if len(secret) != apiKeySecretLength {
		return false
	}
	for _, ch := range secret {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		default:
			return false
		}
	}
	return true
}

func validateCustomGatewayAPIKey(key string) error {
	if key == "" {
		return fmt.Errorf("custom api key is required")
	}
	for _, ch := range key {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= 'A' && ch <= 'Z':
		case ch >= '0' && ch <= '9':
		case ch == '-':
		default:
			return fmt.Errorf("custom api key may only contain letters, numbers, and hyphen")
		}
	}
	return nil
}

func newGatewayAPIKey() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	result := make([]byte, apiKeySecretLength)
	max := big.NewInt(int64(len(alphabet)))
	for i := range result {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("read random index: %w", err)
		}
		result[i] = alphabet[index.Int64()]
	}

	return apiKeyPublicPrefix + string(result), nil
}

func newAdminAccessToken() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 16

	result := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range result {
		index, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("read random index: %w", err)
		}
		result[i] = alphabet[index.Int64()]
	}
	return adminAccessTokenPrefix + string(result), nil
}

func maskToken(token string) string {
	if len(token) <= 12 {
		return "****"
	}
	return token[:10] + "..." + token[len(token)-4:]
}
