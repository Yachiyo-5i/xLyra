package ratelimit

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	gormcallbacks "gorm.io/gorm/callbacks"

	"xlyra/server/internal/store"
)

func memoryTestService(t *testing.T, global *store.GatewayRateLimit, perKey *store.GatewayRateLimit) *Service {
	t.Helper()
	return NewService(ratelimitStoreWithGorm(t, ratelimitGormWithQueryCallback(t, func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*store.GatewayRateLimit)
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		gormcallbacks.BuildQuerySQL(tx)
		isGlobal := false
		for _, value := range tx.Statement.Vars {
			if text, ok := value.(string); ok && text == store.RateLimitScopeGlobal {
				isGlobal = true
			}
		}
		row := perKey
		if isGlobal {
			row = global
		}
		if row == nil {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = *row
		tx.Statement.RowsAffected = 1
	})))
}

func enabledLimit(scope string, rpm *int64, tpm *int64) *store.GatewayRateLimit {
	item := &store.GatewayRateLimit{Scope: scope, Status: store.RateLimitStatusEnabled}
	if rpm != nil {
		item.RPMLimit = sql.NullInt64{Int64: *rpm, Valid: true}
	}
	if tpm != nil {
		item.TPMLimit = sql.NullInt64{Int64: *tpm, Valid: true}
	}
	return item
}

var memoryTestBase = time.Date(2026, 7, 17, 10, 0, 30, 0, time.UTC)

func TestMemoryRPMLimitsConcurrentHeldSlots(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(2), nil), nil)
	apiKeyID := uuid.New()

	first, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase})
	if err != nil || first == nil {
		t.Fatalf("first acquire = (%#v, %v)", first, err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase}); err != nil {
		t.Fatalf("second acquire failed: %v", err)
	}

	_, err = service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase})
	var limitErr LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("third acquire error = %v, want LimitError", err)
	}
	if limitErr.LimitType != "rpm" || limitErr.RetryAfterSeconds != rpmRetryAfterSeconds || limitErr.Limit != 2 {
		t.Fatalf("limit error = %#v, want rpm slot limit", limitErr)
	}

	if err := service.Settle(context.Background(), first, 0); err != nil {
		t.Fatalf("settle failed: %v", err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase.Add(5 * time.Minute)}); err != nil {
		t.Fatalf("acquire after release failed: %v", err)
	}
}

func TestMemoryRPMSlotsSurviveWindowRollover(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(1), nil), nil)
	apiKeyID := uuid.New()

	held, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase})
	if err != nil || held == nil {
		t.Fatalf("acquire = (%#v, %v)", held, err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase.Add(3 * time.Minute)}); err == nil {
		t.Fatal("held slot must still limit after minutes pass")
	}
	if err := service.Settle(context.Background(), held, 0); err != nil {
		t.Fatalf("settle failed: %v", err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("acquire after settle failed: %v", err)
	}
}

func TestMemoryTPMRetryAfterFloorIsOneSecond(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"max_tokens": 50}
	estimate := EstimateTokens(payload)
	limit := estimate
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, nil, &limit), nil)
	apiKeyID := uuid.New()
	lateInWindow := memoryTestBase.Truncate(time.Minute).Add(59*time.Second + 900*time.Millisecond)
	input := AcquireInput{APIKeyID: apiKeyID, Payload: payload, EstimateTPM: true, RequestedAt: lateInWindow}

	if _, err := service.Acquire(context.Background(), input); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	_, err := service.Acquire(context.Background(), input)
	var limitErr LimitError
	if !errors.As(err, &limitErr) || limitErr.RetryAfterSeconds != 1 {
		t.Fatalf("error = %v, want tpm LimitError with retry-after floor 1", err)
	}
}

func TestMemoryTPMReserveSettleAndRelease(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"model": "gpt-5.5", "messages": []any{map[string]any{"role": "user", "content": "hello"}}, "max_tokens": 100}
	estimate := EstimateTokens(payload)
	limit := estimate + estimate/2
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, nil, &limit), nil)
	apiKeyID := uuid.New()
	input := AcquireInput{APIKeyID: apiKeyID, Payload: payload, EstimateTPM: true, RequestedAt: memoryTestBase}

	first, err := service.Acquire(context.Background(), input)
	if err != nil || first == nil {
		t.Fatalf("first acquire = (%#v, %v)", first, err)
	}
	if first.Scopes[0].TPMReserved != estimate {
		t.Fatalf("reserved = %d, want %d", first.Scopes[0].TPMReserved, estimate)
	}

	_, err = service.Acquire(context.Background(), input)
	var limitErr LimitError
	if !errors.As(err, &limitErr) || limitErr.LimitType != "tpm" {
		t.Fatalf("second acquire error = %v, want tpm LimitError", err)
	}

	if err := service.Settle(context.Background(), first, estimate/2); err != nil {
		t.Fatalf("settle failed: %v", err)
	}
	third, err := service.Acquire(context.Background(), input)
	if err != nil || third == nil {
		t.Fatalf("post-settle acquire = (%#v, %v), want success", third, err)
	}
	if third.Scopes[0].TPMActual != estimate/2 {
		t.Fatalf("actual after settle = %d, want %d", third.Scopes[0].TPMActual, estimate/2)
	}
}

func TestMemoryRejectionConsumesNothing(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t,
		enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(5), nil),
		enabledLimit(store.RateLimitScopeAPIKey, ratelimitInt64(1), nil))
	apiKeyID := uuid.New()
	input := AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase}

	if _, err := service.Acquire(context.Background(), input); err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	if _, err := service.Acquire(context.Background(), input); err == nil {
		t.Fatal("second acquire should hit the api key limit")
	}

	service.mu.Lock()
	globalHeld := service.windows["global"].inflight
	service.mu.Unlock()
	if globalHeld != 1 {
		t.Fatalf("global held slots = %d, want 1 (rejected acquire must not consume)", globalHeld)
	}
}

func TestMemorySettleAfterRolloverIsIgnored(t *testing.T) {
	t.Parallel()

	tpm := int64(100000)
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, nil, &tpm), nil)
	apiKeyID := uuid.New()
	input := AcquireInput{APIKeyID: apiKeyID, Payload: map[string]any{"max_tokens": 10}, EstimateTPM: true, RequestedAt: memoryTestBase}

	reservation, err := service.Acquire(context.Background(), input)
	if err != nil || reservation == nil {
		t.Fatalf("acquire = (%#v, %v)", reservation, err)
	}

	later := input
	later.RequestedAt = memoryTestBase.Add(2 * time.Minute)
	if _, err := service.Acquire(context.Background(), later); err != nil {
		t.Fatalf("later acquire failed: %v", err)
	}
	if err := service.Settle(context.Background(), reservation, 999999); err != nil {
		t.Fatalf("stale settle failed: %v", err)
	}

	service.mu.Lock()
	actual := service.windows["global"].tpmActual
	service.mu.Unlock()
	if actual != 0 {
		t.Fatalf("stale settle mutated rolled window, actual = %d", actual)
	}
}

func TestMemorySweepEvictsStaleWindows(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, nil, enabledLimit(store.RateLimitScopeAPIKey, ratelimitInt64(10), nil))
	first := uuid.New()
	second := uuid.New()

	firstReservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: first, RequestedAt: memoryTestBase})
	if err != nil {
		t.Fatalf("first key acquire failed: %v", err)
	}
	if err := service.Settle(context.Background(), firstReservation, 0); err != nil {
		t.Fatalf("settle failed: %v", err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: second, RequestedAt: memoryTestBase.Add(4 * time.Minute)}); err != nil {
		t.Fatalf("second key acquire failed: %v", err)
	}

	service.mu.Lock()
	_, staleExists := service.windows["api_key:"+first.String()]
	service.mu.Unlock()
	if staleExists {
		t.Fatal("released stale window must be swept")
	}
}

func TestMemorySweepKeepsHeldSlots(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, nil, enabledLimit(store.RateLimitScopeAPIKey, ratelimitInt64(10), nil))
	first := uuid.New()
	second := uuid.New()

	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: first, RequestedAt: memoryTestBase}); err != nil {
		t.Fatalf("first key acquire failed: %v", err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: second, RequestedAt: memoryTestBase.Add(4 * time.Minute)}); err != nil {
		t.Fatalf("second key acquire failed: %v", err)
	}

	service.mu.Lock()
	window, exists := service.windows["api_key:"+first.String()]
	service.mu.Unlock()
	if !exists || window.inflight != 1 {
		t.Fatal("windows holding slots must never be swept")
	}
}

func TestMemoryConfigCacheInvalidation(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(10), nil), nil)
	apiKeyID := uuid.New()

	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase}); err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	service.mu.Lock()
	_, cached := service.configs["global"]
	service.mu.Unlock()
	if !cached {
		t.Fatal("global config must be cached after acquire")
	}

	service.invalidateConfig(store.RateLimitScopeGlobal, uuid.Nil)
	service.mu.Lock()
	_, cached = service.configs["global"]
	service.mu.Unlock()
	if cached {
		t.Fatal("global config cache must be invalidated after set")
	}
}

func TestMemoryConcurrentAcquires(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(100), nil), nil)
	apiKeyID := uuid.New()

	var wg sync.WaitGroup
	successes := make(chan struct{}, 200)
	for i := 0; i < 150; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: memoryTestBase})
			if err == nil && reservation != nil {
				successes <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(successes)

	count := 0
	for range successes {
		count++
	}
	if count != 100 {
		t.Fatalf("concurrent successes = %d, want exactly 100", count)
	}
}
