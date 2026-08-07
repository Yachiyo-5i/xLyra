package auth

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestValidateModelRulesAllowsEmptyInputWithoutRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.validateModelRules(context.Background(), nil, "allow_all", nil); err != nil {
		t.Fatalf("validateModelRules nil error = %v, want nil", err)
	}
	if _, err := service.validateModelRules(context.Background(), []store.APIKeyModelRule{}, "allow_all", nil); err != nil {
		t.Fatalf("validateModelRules empty error = %v, want nil", err)
	}
}

func TestCreateAPIKeyReturnsZeroResultForBlankNameBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	result, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{Name: " \t\n "}, uuid.New())
	if result.Key != "" || result.KeyPrefix != "" || result.APIKey.ID != uuid.Nil {
		t.Fatalf("CreateAPIKey result = %#v, want zero on validation error", result)
	}
	assertAuthErrorString(t, "CreateAPIKey", err, "api key name is required")
}

func TestCreateAPIKeyRejectsInvalidLimitedQuotaBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	for _, limit := range []float64{-1, math.NaN(), math.Inf(1)} {
		limit := limit
		result, err := (&Service{}).CreateAPIKey(context.Background(), CreateAPIKeyInput{
			Name:       "limited key",
			QuotaLimit: &limit,
		}, uuid.New())
		if result.Key != "" || result.KeyPrefix != "" || result.APIKey.ID != uuid.Nil {
			t.Fatalf("CreateAPIKey result = %#v, want zero on invalid quota", result)
		}
		if err == nil || !strings.Contains(err.Error(), "quota_limit must be a finite number") {
			t.Fatalf("invalid quota %v error = %v", limit, err)
		}
	}
}

func TestUpdateAPIKeyRejectsQuotaBelowUsageBeforeWrite(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected api key quota query destination"))
			return
		}
		*item = store.APIKey{ID: apiKeyID, QuotaUsed: 10}
		tx.Statement.RowsAffected = 1
	})
	limit := 9.99

	updated, err := service.UpdateAPIKey(context.Background(), apiKeyID, UpdateAPIKeyInput{QuotaLimit: &limit})
	if updated.ID != uuid.Nil {
		t.Fatalf("UpdateAPIKey result = %#v, want zero on quota below usage", updated)
	}
	if err == nil || err.Error() != "quota_limit must be greater than or equal to quota_total_used" {
		t.Fatalf("quota below usage error = %v", err)
	}
}

func TestAPIKeyQuotaMutationInputsRejectInvalidOperationsBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.IncreaseAPIKeyQuota(context.Background(), uuid.New(), 0); err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("zero quota increase error = %v", err)
	}
	if _, err := service.ResetAPIKeyQuota(context.Background(), uuid.New(), nil); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty quota reset error = %v", err)
	}
	if _, err := service.ResetAPIKeyQuota(context.Background(), uuid.New(), []string{"monthly"}); err == nil || !strings.Contains(err.Error(), "total, daily, or weekly") {
		t.Fatalf("unknown quota reset error = %v", err)
	}
	dailyLimited := false
	if _, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		Name:                "missing daily limit",
		QuotaUnlimited:      true,
		QuotaDailyUnlimited: &dailyLimited,
	}, uuid.New()); err == nil || !strings.Contains(err.Error(), "quota_daily_limit is required") {
		t.Fatalf("missing daily quota limit error = %v", err)
	}
}

func TestResetAPIKeyQuotaAcceptsTotalAndPreservesAccumulatedUsage(t *testing.T) {
	t.Parallel()

	service := NewService(authTransactionOnlyGorm(t), "test-master-key")
	apiKeyID := uuid.New()
	now := time.Now()
	dailyStart := service.timeZone.StartOfDay(now)
	weeklyStart := service.timeZone.StartOfWeek(now)
	authReplaceQueryCallback(t, service.db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected quota reset query destination"))
			return
		}
		*item = store.APIKey{
			ID: apiKeyID, QuotaUsed: 100, QuotaTotalUsed: 30,
			QuotaLimit:      sql.NullFloat64{Float64: 50, Valid: true},
			QuotaDailyLimit: sql.NullFloat64{Float64: 25, Valid: true}, QuotaDailyUsed: 10, QuotaDailyWindowStart: &dailyStart,
			QuotaWeeklyLimit: sql.NullFloat64{Float64: 40, Valid: true}, QuotaWeeklyUsed: 20, QuotaWeeklyWindowStart: &weeklyStart,
		}
		tx.Statement.RowsAffected = 1
	})
	authReplaceUpdateCallback(t, service.db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*store.APIKey); !ok {
			tx.AddError(errors.New("unexpected quota reset update destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	result, err := service.ResetAPIKeyQuota(context.Background(), apiKeyID, []string{"total", "weekly", "daily", "total"})
	if err != nil {
		t.Fatalf("ResetAPIKeyQuota returned error: %v", err)
	}
	if len(result.Scopes) != 3 || result.Scopes[0] != "total" || result.Scopes[1] != "weekly" || result.Scopes[2] != "daily" {
		t.Fatalf("reset scopes = %v", result.Scopes)
	}
	if result.TotalUsedBefore != 30 || result.DailyUsedBefore != 10 || result.WeeklyUsedBefore != 20 {
		t.Fatalf("reset usage before = total:%f daily:%f weekly:%f", result.TotalUsedBefore, result.DailyUsedBefore, result.WeeklyUsedBefore)
	}
	if result.APIKey.QuotaUsed != 100 || result.APIKey.QuotaTotalUsed != 0 || result.APIKey.QuotaDailyUsed != 0 || result.APIKey.QuotaWeeklyUsed != 0 || result.APIKey.QuotaTotalResetAt == nil {
		t.Fatalf("reset api key = %#v", result.APIKey)
	}
}

func TestResetAPIKeyQuotaRejectsScopesWithoutEnabledLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scope   string
		apiKey  store.APIKey
		message string
	}{
		{name: "unlimited total", scope: "total", apiKey: store.APIKey{QuotaUnlimited: true, QuotaLimit: sql.NullFloat64{Float64: 100, Valid: true}}, message: "total quota limit is not enabled"},
		{name: "unconfigured weekly", scope: "weekly", apiKey: store.APIKey{}, message: "weekly quota limit is not enabled"},
		{name: "unlimited daily", scope: "daily", apiKey: store.APIKey{QuotaDailyUnlimited: true, QuotaDailyLimit: sql.NullFloat64{Float64: 20, Valid: true}}, message: "daily quota limit is not enabled"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			apiKeyID := uuid.New()
			service := NewService(authTransactionOnlyGorm(t), "test-master-key")
			authReplaceQueryCallback(t, service.db, func(tx *gorm.DB) {
				item, ok := tx.Statement.Dest.(*store.APIKey)
				if !ok {
					tx.AddError(errors.New("unexpected quota reset query destination"))
					return
				}
				*item = test.apiKey
				item.ID = apiKeyID
				tx.Statement.RowsAffected = 1
			})

			if _, err := service.ResetAPIKeyQuota(context.Background(), apiKeyID, []string{test.scope}); err == nil || err.Error() != test.message {
				t.Fatalf("ResetAPIKeyQuota error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestZeroAPIKeyQuotaLimitsMeanUnlimited(t *testing.T) {
	t.Parallel()

	zero := float64(0)
	for _, field := range []string{"quota_limit", "quota_daily_limit", "quota_weekly_limit"} {
		if err := validateAPIKeyQuota(field, &zero, zeroQuotaLimit(&zero), 10); err != nil {
			t.Fatalf("%s zero limit validation error = %v, want unlimited", field, err)
		}
	}
}

func TestAPIKeyRateLimitReturnsDisabledForMissingAPIKey(t *testing.T) {
	t.Parallel()

	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})

	rateLimit, err := service.APIKeyRateLimit(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("APIKeyRateLimit returned error: %v", err)
	}
	if rateLimit.Status != store.RateLimitStatusDisabled {
		t.Fatalf("APIKeyRateLimit status = %q, want %q", rateLimit.Status, store.RateLimitStatusDisabled)
	}
	if rateLimit.RPMLimit != nil || rateLimit.TPMLimit != nil {
		t.Fatalf("APIKeyRateLimit limits = rpm %v tpm %v, want nil limits", rateLimit.RPMLimit, rateLimit.TPMLimit)
	}
}

func TestCheckModelAccessDeniesAccessWhenAPIKeyLookupFails(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("api key lookup stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	access, err := service.CheckModelAccess(context.Background(), uuid.New(), "gpt-4o")
	if access.Allowed {
		t.Fatalf("CheckModelAccess allowed = true, want false on repository error")
	}
	if access.APIKey.ID != uuid.Nil || access.ModelKey != "" {
		t.Fatalf("CheckModelAccess access = %#v, want zero api key and model key on api key lookup error", access)
	}
	assertAuthErrorIs(t, "CheckModelAccess", err, queryErr)
}

func TestEffectiveAllowedSiteIDsSkipsPermissionLookupForAllowAllPolicy(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("unexpected permission lookup")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		if tx.Statement != nil && strings.Contains(tx.Statement.Table, "api_key_site_permissions") {
			tx.AddError(queryErr)
		}
	})

	allowedSiteIDs, err := service.effectiveAllowedSiteIDs(context.Background(), store.APIKey{
		ID:         uuid.New(),
		SitePolicy: "allow_all",
	})
	if err != nil {
		t.Fatalf("effectiveAllowedSiteIDs returned error: %v", err)
	}
	if allowedSiteIDs != nil {
		t.Fatalf("effectiveAllowedSiteIDs = %#v, want nil for allow_all", allowedSiteIDs)
	}
}

func TestUpsertAPIKeyRateLimitAcceptsDisabledStatusWithNilLimits(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("rate limit upsert stopped")
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	_, err := upsertAPIKeyRateLimit(context.Background(), service.db, uuid.New(), RateLimitInput{
		Status: store.RateLimitStatusDisabled,
	})
	assertAuthErrorIs(t, "upsertAPIKeyRateLimit", err, queryErr)
}
