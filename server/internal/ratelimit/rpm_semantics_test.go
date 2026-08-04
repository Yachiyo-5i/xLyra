package ratelimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestRPMSemanticsThroughputNotWindowLimited(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(2), nil), nil)
	apiKeyID := uuid.New()
	sameMinute := time.Date(2026, 7, 17, 15, 0, 10, 0, time.UTC)

	for i := 0; i < 20; i++ {
		reservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: sameMinute})
		if err != nil || reservation == nil {
			t.Fatalf("sequential acquire %d = (%#v, %v); concurrency semantics must not window-limit throughput", i, reservation, err)
		}
		if err := service.Settle(context.Background(), reservation, 0); err != nil {
			t.Fatalf("settle %d failed: %v", i, err)
		}
	}

	first, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: sameMinute})
	if err != nil {
		t.Fatalf("hold-1 failed: %v", err)
	}
	second, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: sameMinute})
	if err != nil {
		t.Fatalf("hold-2 failed: %v", err)
	}
	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: sameMinute}); err == nil {
		t.Fatal("third concurrent hold must be limited at RPM=2")
	}
	_ = service.Settle(context.Background(), first, 0)
	_ = service.Settle(context.Background(), second, 0)
}

func TestRPMSemanticsConcurrencyNeverExceedsLimit(t *testing.T) {
	t.Parallel()

	const limit = 10
	const workers = 60
	const iterations = 4
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(limit), nil), nil)
	apiKeyID := uuid.New()

	var active atomic.Int64
	var peak atomic.Int64
	var violations atomic.Int64
	var admitted atomic.Int64

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				reservation, err := service.AcquireQueued(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
				if err != nil || reservation == nil {
					violations.Add(1)
					return
				}
				admitted.Add(1)
				current := active.Add(1)
				if current > limit {
					violations.Add(1)
				}
				for {
					observed := peak.Load()
					if current <= observed || peak.CompareAndSwap(observed, current) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				_ = service.Settle(context.Background(), reservation, 0)
			}
		}()
	}
	wg.Wait()

	if violations.Load() != 0 {
		t.Fatalf("violations = %d: concurrency exceeded the limit or admissions failed", violations.Load())
	}
	if admitted.Load() != workers*iterations {
		t.Fatalf("admitted = %d, want %d (every request must eventually pass)", admitted.Load(), workers*iterations)
	}
	if peak.Load() > limit {
		t.Fatalf("peak concurrency = %d, want <= %d", peak.Load(), limit)
	}
	if peak.Load() < limit/2 {
		t.Fatalf("peak concurrency = %d suspiciously low for %d workers; limiter may be over-restricting", peak.Load(), workers)
	}

	service.mu.Lock()
	final := service.windows["global"].inflight
	service.mu.Unlock()
	if final != 0 {
		t.Fatalf("final inflight = %d, want 0 (all slots released)", final)
	}
}

func TestRPMSemanticsDualScopeIndependence(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t,
		enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(5), nil),
		enabledLimit(store.RateLimitScopeAPIKey, ratelimitInt64(1), nil))
	keyA := uuid.New()
	keyB := uuid.New()
	now := time.Now()

	heldA, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: keyA, RequestedAt: now})
	if err != nil || heldA == nil {
		t.Fatalf("keyA acquire = (%#v, %v)", heldA, err)
	}

	_, err = service.Acquire(context.Background(), AcquireInput{APIKeyID: keyA, RequestedAt: now})
	var limitErr LimitError
	if !errors.As(err, &limitErr) || limitErr.Scope != store.RateLimitScopeAPIKey {
		t.Fatalf("second keyA acquire error = %v, want api_key scope limit", err)
	}

	heldB, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: keyB, RequestedAt: now})
	if err != nil || heldB == nil {
		t.Fatalf("keyB acquire = (%#v, %v); other keys must pass while global has capacity", heldB, err)
	}

	service.mu.Lock()
	globalHeld := service.windows["global"].inflight
	service.mu.Unlock()
	if globalHeld != 2 {
		t.Fatalf("global held = %d, want 2 (one per key)", globalHeld)
	}

	_ = service.Settle(context.Background(), heldA, 0)
	_ = service.Settle(context.Background(), heldB, 0)
}

func TestRPMSemanticsQueueDrainsExactlyWithReleases(t *testing.T) {
	t.Parallel()

	const limit = 3
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(limit), nil), nil)
	apiKeyID := uuid.New()

	held := make([]*Reservation, 0, limit)
	for i := 0; i < limit; i++ {
		reservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
		if err != nil || reservation == nil {
			t.Fatalf("priming acquire %d = (%#v, %v)", i, reservation, err)
		}
		held = append(held, reservation)
	}

	const waiterCount = 5
	admitted := make(chan *Reservation, waiterCount)
	for i := 0; i < waiterCount; i++ {
		go func() {
			reservation, err := service.AcquireQueued(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
			if err == nil && reservation != nil {
				admitted <- reservation
			}
		}()
	}

	waitForQueueDepth := func(want int) {
		deadline := time.Now().Add(2 * time.Second)
		for {
			service.mu.Lock()
			depth := len(service.queues["global"])
			service.mu.Unlock()
			if depth == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("queue depth never reached %d", want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitForQueueDepth(waiterCount)

	collectAdmitted := func(want int) []*Reservation {
		result := make([]*Reservation, 0, want)
		timeout := time.After(2 * time.Second)
		for len(result) < want {
			select {
			case reservation := <-admitted:
				result = append(result, reservation)
			case <-timeout:
				t.Fatalf("only %d of %d waiters admitted", len(result), want)
			}
		}
		return result
	}

	for i := 0; i < limit; i++ {
		_ = service.Settle(context.Background(), held[i], 0)
	}
	firstBatch := collectAdmitted(limit)

	select {
	case <-admitted:
		t.Fatal("more waiters admitted than slots released")
	case <-time.After(100 * time.Millisecond):
	}

	service.mu.Lock()
	inflight := service.windows["global"].inflight
	service.mu.Unlock()
	if inflight != limit {
		t.Fatalf("inflight after first batch = %d, want %d", inflight, limit)
	}

	for _, reservation := range firstBatch {
		_ = service.Settle(context.Background(), reservation, 0)
	}
	secondBatch := collectAdmitted(waiterCount - limit)
	for _, reservation := range secondBatch {
		_ = service.Settle(context.Background(), reservation, 0)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		service.mu.Lock()
		inflight = service.windows["global"].inflight
		depth := len(service.queues["global"])
		service.mu.Unlock()
		if inflight == 0 && depth == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("final state inflight=%d depth=%d, want 0/0", inflight, depth)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRPMSemanticsSettleClampsAtZero(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(2), nil), nil)
	apiKeyID := uuid.New()

	reservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
	if err != nil || reservation == nil {
		t.Fatalf("acquire = (%#v, %v)", reservation, err)
	}
	_ = service.Settle(context.Background(), reservation, 0)
	_ = service.Settle(context.Background(), reservation, 0)

	service.mu.Lock()
	inflight := service.windows["global"].inflight
	service.mu.Unlock()
	if inflight != 0 {
		t.Fatalf("inflight after double settle = %d, want clamped 0", inflight)
	}
}
