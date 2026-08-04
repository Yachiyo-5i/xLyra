package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestCreateGatewayAPIKeyValueRejectsBlankCustomKey(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, _, err := service.createGatewayAPIKeyValue(context.Background(), " \t\n ")
	if err == nil {
		t.Fatal("expected blank custom key to return an error before generating an accepted key")
	}
}

func TestCreateGatewayAPIKeyValueChecksGeneratedAliasCollisions(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("api key exists lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	compatibleAlias := apiKeyCompatiblePrefix + strings.Repeat("a", apiKeySecretLength)
	_, _, err := service.createGatewayAPIKeyValue(context.Background(), compatibleAlias)
	assertAuthErrorIs(t, "createGatewayAPIKeyValue", err, queryErr)
}

func TestEffectiveAllowedSiteIDsPreservesAllowAllWhenRepositoryFails(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("allowed sites lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	allowedSiteIDs, err := service.effectiveAllowedSiteIDs(context.Background(), store.APIKey{})
	if allowedSiteIDs != nil {
		t.Fatalf("allowedSiteIDs = %#v, want nil on repository error", allowedSiteIDs)
	}
	assertAuthErrorIs(t, "effectiveAllowedSiteIDs", err, queryErr)
}

func TestFilterSiteModelIDsBySitesSkipsEmptyInputs(t *testing.T) {
	t.Parallel()

	service := &Service{}
	allowedSiteID := uuid.New()

	for _, tc := range []struct {
		name           string
		siteModelIDs   []uuid.UUID
		allowedSiteIDs []uuid.UUID
	}{
		{name: "nil site models", siteModelIDs: nil, allowedSiteIDs: []uuid.UUID{allowedSiteID}},
		{name: "empty site models", siteModelIDs: []uuid.UUID{}, allowedSiteIDs: []uuid.UUID{allowedSiteID}},
		{name: "nil allowed sites", siteModelIDs: []uuid.UUID{uuid.New()}, allowedSiteIDs: nil},
		{name: "empty allowed sites", siteModelIDs: []uuid.UUID{uuid.New()}, allowedSiteIDs: []uuid.UUID{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			filtered, err := service.filterSiteModelIDsBySites(context.Background(), tc.siteModelIDs, tc.allowedSiteIDs)
			if err != nil {
				t.Fatalf("filterSiteModelIDsBySites returned error: %v", err)
			}
			if filtered != nil {
				t.Fatalf("filterSiteModelIDsBySites = %#v, want nil", filtered)
			}
		})
	}
}

func TestAllowedSiteModelIDsForCanonicalReturnsRepositoryError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("canonical access lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	ids, err := service.allowedSiteModelIDsForCanonical(context.Background(), uuid.New(), uuid.New(), nil)
	if ids != nil {
		t.Fatalf("allowedSiteModelIDsForCanonical ids = %#v, want nil on repository error", ids)
	}
	assertAuthErrorIs(t, "allowedSiteModelIDsForCanonical", err, queryErr)
}

func TestFilterSiteModelIDsBySitesReturnsRepositoryErrorForNonEmptyInputs(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site model lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	filtered, err := service.filterSiteModelIDsBySites(context.Background(), []uuid.UUID{uuid.New()}, []uuid.UUID{uuid.New()})
	if filtered != nil {
		t.Fatalf("filterSiteModelIDsBySites result = %#v, want nil on repository error", filtered)
	}
	assertAuthErrorIs(t, "filterSiteModelIDsBySites", err, queryErr)
}

func TestNormalizeExistingSiteModelIDsWrapsMissingSiteModel(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site model lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	siteModelID := uuid.New()
	normalized, err := service.normalizeExistingSiteModelIDs(context.Background(), []uuid.UUID{siteModelID})
	if normalized != nil {
		t.Fatalf("normalizeExistingSiteModelIDs result = %#v, want nil on repository error", normalized)
	}
	assertAuthErrorContains(t, "normalizeExistingSiteModelIDs", err, "site_model_id "+siteModelID.String()+" was not found")
}

func TestResolveCanonicalModelNormalizesProviderPrefixesBeforeLookup(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("canonical model lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	_, err := service.resolveCanonicalModel(context.Background(), " openai/models/GPT_4.1 ")
	assertAuthErrorIs(t, "resolveCanonicalModel", err, queryErr)
}

func TestNormalizeExistingModelKeysRejectsBlankKeysBeforeLookup(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.normalizeExistingModelKeys(context.Background(), []string{" \t\n ", "gpt-4o"})
	assertAuthErrorString(t, "normalizeExistingModelKeys", err, "model_key is required")
}

func TestNormalizeExistingModelKeysWrapsMissingCanonicalModel(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("canonical model lookup stopped")
	service := authServiceWithRepositoryError(t, queryErr)
	modelKeys, err := service.normalizeExistingModelKeys(context.Background(), []string{" OpenAI/GPT_4o "})
	if modelKeys != nil {
		t.Fatalf("normalizeExistingModelKeys result = %#v, want nil on repository error", modelKeys)
	}
	assertAuthErrorString(t, "normalizeExistingModelKeys", err, `canonical model "gpt-4o" was not found`)
}
