package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestValidateModelRuleShapesRejectsDuplicateIdentities(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		rules []store.APIKeyModelRule
	}{
		{name: "exact_normalized", rules: []store.APIKeyModelRule{
			{Pattern: "gpt-4o", Target: "a"},
			{Pattern: " GPT_4O ", Target: "b"},
		}},
		{name: "wildcard_normalized", rules: []store.APIKeyModelRule{
			{Pattern: "gpt-4*", Target: "a"},
			{Pattern: "GPT_4*", Target: "b"},
		}},
		{name: "catch_all", rules: []store.APIKeyModelRule{
			{Pattern: "*", Target: "a"},
			{Pattern: "*", Target: "b"},
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := validateModelRuleShapes(testCase.rules)
			if err == nil || !strings.Contains(err.Error(), "duplicate model rule pattern") {
				t.Fatalf("error = %v, want duplicate pattern rejection", err)
			}
		})
	}
}

func TestValidateModelRuleShapesAllowsDistinctKinds(t *testing.T) {
	t.Parallel()

	err := validateModelRuleShapes([]store.APIKeyModelRule{
		{Pattern: "gpt-4o", Target: "a"},
		{Pattern: "gpt-4o*", Target: "b"},
		{Pattern: "*", Target: "c"},
	})
	if err != nil {
		t.Fatalf("distinct kinds error = %v, want nil", err)
	}
}

func TestValidateModelRuleShapesRejectsInvalidWildcardPrefix(t *testing.T) {
	t.Parallel()

	err := validateModelRuleShapes([]store.APIKeyModelRule{{Pattern: "-*", Target: "a"}})
	if err == nil || !strings.Contains(err.Error(), "is not valid") {
		t.Fatalf("error = %v, want invalid pattern rejection", err)
	}
}

func TestValidateModelRuleShapesRejectsInnerStarPatterns(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"gpt-*-turbo", "gpt-*-turbo*", "*gpt-4"} {
		err := validateModelRuleShapes([]store.APIKeyModelRule{{Pattern: pattern, Target: "a"}})
		if err == nil || !strings.Contains(err.Error(), "is not valid") {
			t.Fatalf("pattern %q error = %v, want invalid pattern rejection", pattern, err)
		}
	}
}

func TestValidateModelRuleShapesDistinguishesSeparatorBoundedWildcards(t *testing.T) {
	t.Parallel()

	err := validateModelRuleShapes([]store.APIKeyModelRule{
		{Pattern: "gpt-4*", Target: "a"},
		{Pattern: "gpt-4.*", Target: "b"},
		{Pattern: "gpt-4-*", Target: "c"},
	})
	if err != nil {
		t.Fatalf("separator-bounded wildcards error = %v, want treated as distinct patterns", err)
	}
}

func TestModelRulePatternIdentityKinds(t *testing.T) {
	t.Parallel()

	if got := modelRulePatternIdentity("*"); got != "*" {
		t.Fatalf("catch-all identity = %q", got)
	}
	if got := modelRulePatternIdentity("GPT-4*"); got != "wildcard:gpt-4" {
		t.Fatalf("wildcard identity = %q", got)
	}
	if got := modelRulePatternIdentity(" models/gpt-4o "); got != "exact:gpt-4o" {
		t.Fatalf("exact identity = %q", got)
	}
}

func modelRuleAllowListService(t *testing.T, allowedCanonical store.CanonicalModel, outsideCanonical store.CanonicalModel, siteModel store.SiteModel) *Service {
	t.Helper()

	return authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{allowedCanonical, outsideCanonical}
			tx.Statement.RowsAffected = 2
		case *store.CanonicalModel:
			*dest = allowedCanonical
			tx.Statement.RowsAffected = 1
		case *store.SiteModel:
			*dest = siteModel
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
}

func TestValidateModelRulesAllowListMembership(t *testing.T) {
	t.Parallel()

	allowedCanonical := store.CanonicalModel{ID: uuid.New(), ModelKey: "allowed-model", Status: "active"}
	outsideCanonical := store.CanonicalModel{ID: uuid.New(), ModelKey: "outside-model", Status: "active"}
	siteModelID := uuid.New()
	siteModel := store.SiteModel{ID: siteModelID, CanonicalID: uuid.NullUUID{UUID: allowedCanonical.ID, Valid: true}}
	service := modelRuleAllowListService(t, allowedCanonical, outsideCanonical, siteModel)

	validated, err := service.validateModelRules(context.Background(), []store.APIKeyModelRule{
		{Pattern: "alias", Target: " Allowed-Model ", Mode: "soft"},
	}, "allow_list", []uuid.UUID{siteModelID})
	if err != nil {
		t.Fatalf("allowed target error = %v, want pass", err)
	}
	if len(validated) != 1 || validated[0].Target != "allowed-model" || validated[0].Mode != store.APIKeyModelRuleModeSoft {
		t.Fatalf("validated = %#v, want canonicalized target with soft mode", validated)
	}

	_, err = service.validateModelRules(context.Background(), []store.APIKeyModelRule{
		{Pattern: "alias", Target: "outside-model"},
	}, "allow_list", []uuid.UUID{siteModelID})
	if err == nil || !strings.Contains(err.Error(), "not in the api key model allow list") {
		t.Fatalf("outside target error = %v, want allow list rejection", err)
	}

	_, err = service.validateModelRules(context.Background(), []store.APIKeyModelRule{
		{Pattern: "alias", Target: "allowed-model"},
	}, "allow_list", nil)
	if err == nil || !strings.Contains(err.Error(), "not in the api key model allow list") {
		t.Fatalf("empty allow list error = %v, want rejection", err)
	}

	validated, err = service.validateModelRules(context.Background(), []store.APIKeyModelRule{
		{Pattern: "alias", Target: "outside-model"},
	}, "allow_all", nil)
	if err != nil || len(validated) != 1 || validated[0].Target != "outside-model" {
		t.Fatalf("allow_all validation = (%#v, %v), want canonical-only check to pass", validated, err)
	}
}
