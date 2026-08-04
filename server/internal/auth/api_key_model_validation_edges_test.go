package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestConsumeQuotaPropagatesUsageUpdateErrorForPositiveAmount(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("usage update stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	apiKey, err := service.ConsumeQuota(context.Background(), uuid.New(), 1)
	if apiKey.ID != uuid.Nil {
		t.Fatalf("ConsumeQuota returned api key = %#v, want zero value on repository error", apiKey)
	}
	assertAuthErrorIs(t, "ConsumeQuota", err, queryErr)
}

func TestValidateModelRulesReportsInvalidCanonicalModelOnLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("canonical lookup stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	_, err := service.validateModelRules(context.Background(), []store.APIKeyModelRule{
		{Pattern: " public ", Target: " gpt-4o "},
	}, "allow_all", nil)
	assertAuthErrorString(t, "validateModelRules", err, `mapped model "gpt-4o" is not a valid canonical model`)
}

func TestResolveCanonicalModelReportsAliasMissAfterKeyLookupMiss(t *testing.T) {
	t.Parallel()

	queryCount := 0
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		queryCount++
		tx.AddError(gorm.ErrRecordNotFound)
	})

	_, err := service.resolveCanonicalModel(context.Background(), " gpt-4o-mini ")
	assertAuthErrorString(t, "resolveCanonicalModel", err, `canonical model " gpt-4o-mini " was not found`)
	if queryCount != 2 {
		t.Fatalf("query callback count = %d, want key lookup plus alias lookup", queryCount)
	}
}

func TestValidateAPIKeyTreatsWhitespaceOnlyTokenAsInvalid(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("api key lookup stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	_, err := service.ValidateAPIKey(context.Background(), strings.Repeat(" ", 3))
	assertAuthErrorString(t, "ValidateAPIKey whitespace", err, "invalid api key")
}
