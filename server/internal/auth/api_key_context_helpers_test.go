package auth

import (
	"context"
	"strings"
	"testing"
)

func TestGeneratedAPIKeyAliasRequiresCompatiblePrefixAndValidSecret(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("A", apiKeySecretLength)
	alias, ok := generatedAPIKeyAlias(" \t" + apiKeyCompatiblePrefix + secret + "\n ")
	if !ok || alias != apiKeyPublicPrefix+secret {
		t.Fatalf("generatedAPIKeyAlias valid = %q, %v; want %q true", alias, ok, apiKeyPublicPrefix+secret)
	}

	for _, key := range []string{
		apiKeyPublicPrefix + secret,
		apiKeyCompatiblePrefix + strings.Repeat("A", apiKeySecretLength-1),
		apiKeyCompatiblePrefix + strings.Repeat("_", apiKeySecretLength),
		"custom-key",
	} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			if alias, ok := generatedAPIKeyAlias(key); ok || alias != "" {
				t.Fatalf("generatedAPIKeyAlias(%q) = %q, %v; want empty false", key, alias, ok)
			}
		})
	}
}

func TestValidateCustomGatewayAPIKeyAllowsAlnumHyphenOnly(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"abc", "ABC-123", "xlyra-custom-key"} {
		key := key
		t.Run("valid_"+key, func(t *testing.T) {
			t.Parallel()

			if err := validateCustomGatewayAPIKey(key); err != nil {
				t.Fatalf("validateCustomGatewayAPIKey(%q) returned error: %v", key, err)
			}
		})
	}

	for _, key := range []string{"", "has space", "under_score", "slash/key"} {
		key := key
		t.Run("invalid_"+key, func(t *testing.T) {
			t.Parallel()

			if err := validateCustomGatewayAPIKey(key); err == nil {
				t.Fatalf("validateCustomGatewayAPIKey(%q) expected error", key)
			}
		})
	}
}

func TestContextHelpersRejectWrongValueTypes(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), adminIDKey, "not-a-uuid")
	if got, ok := AdminIDFromContext(ctx); ok {
		t.Fatalf("AdminIDFromContext wrong type = %s, true; want false", got)
	}

	ctx = context.WithValue(context.Background(), adminSessionKey, "not-a-session")
	if got, ok := AdminSessionFromContext(ctx); ok {
		t.Fatalf("AdminSessionFromContext wrong type = %#v, true; want false", got)
	}

	ctx = context.WithValue(context.Background(), adminActorKey, "not-an-actor")
	if got, ok := AdminActorFromContext(ctx); ok {
		t.Fatalf("AdminActorFromContext wrong type = %#v, true; want false", got)
	}

	ctx = context.WithValue(context.Background(), apiKeyIDKey, "not-a-uuid")
	if got, ok := APIKeyIDFromContext(ctx); ok {
		t.Fatalf("APIKeyIDFromContext wrong type = %s, true; want false", got)
	}

	ctx = context.WithValue(context.Background(), apiKeyKey, "not-an-api-key")
	if got, ok := APIKeyFromContext(ctx); ok {
		t.Fatalf("APIKeyFromContext wrong type = %#v, true; want false", got)
	}
}
