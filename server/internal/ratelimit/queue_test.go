package ratelimit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type queueTestClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps int
}

func newQueueTestClock(start time.Time) *queueTestClock {
	return &queueTestClock{now: start}
}

func (c *queueTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *queueTestClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.sleeps++
	c.mu.Unlock()
	return nil
}

var queueTestBase = time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

func TestQueueWaitsForSlotRelease(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(1), nil), nil)
	apiKeyID := uuid.New()

	held, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
	if err != nil || held == nil {
		t.Fatalf("priming acquire = (%#v, %v)", held, err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := service.AcquireQueued(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		service.mu.Lock()
		depth := len(service.queues["global"])
		service.mu.Unlock()
		if depth == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter never enqueued")
		}
		time.Sleep(time.Millisecond)
	}

	if err := service.Settle(context.Background(), held, 0); err != nil {
		t.Fatalf("settle failed: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("queued acquire after release failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued acquire did not complete after slot release")
	}
}

func TestQueueTPMWaitsForNextWindow(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"max_tokens": 20}
	estimate := EstimateTokens(payload)
	limit := estimate
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, nil, &limit), nil)
	apiKeyID := uuid.New()
	clock := newQueueTestClock(queueTestBase)
	input := AcquireInput{APIKeyID: apiKeyID, Payload: payload, EstimateTPM: true, RequestedAt: clock.Now()}

	if _, err := service.Acquire(context.Background(), input); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}

	queued := input
	queued.RequestedAt = clock.Now()
	reservation, err := service.acquireQueued(context.Background(), queued, clock.Now, clock.Sleep)
	if err != nil || reservation == nil {
		t.Fatalf("queued acquire = (%#v, %v), want success after window rollover", reservation, err)
	}
	if got := clock.Now(); !got.Equal(queueTestBase.Add(time.Minute)) {
		t.Fatalf("clock after queue = %v, want next window boundary", got)
	}
}

func TestQueueFIFOOrder(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(3), nil), nil)
	apiKeyID := uuid.New()

	primed := make([]*Reservation, 0, 3)
	for i := 0; i < 3; i++ {
		reservation, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()})
		if err != nil || reservation == nil {
			t.Fatalf("priming acquire %d = (%#v, %v)", i, reservation, err)
		}
		primed = append(primed, reservation)
	}

	blocker := &queueWaiter{ready: make(chan struct{})}
	service.mu.Lock()
	service.queues["global"] = append(service.queues["global"], blocker)
	service.mu.Unlock()

	order := make(chan int, 3)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		index := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.AcquireQueued(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: time.Now()}); err != nil {
				t.Errorf("waiter %d failed: %v", index, err)
				return
			}
			order <- index
		}()

		deadline := time.Now().Add(2 * time.Second)
		for {
			service.mu.Lock()
			depth := len(service.queues["global"])
			service.mu.Unlock()
			if depth == index+2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("waiter %d never enqueued", index)
			}
			time.Sleep(time.Millisecond)
		}
	}

	service.finishWaiter("global", blocker)
	for _, reservation := range primed {
		if err := service.Settle(context.Background(), reservation, 0); err != nil {
			t.Fatalf("settle failed: %v", err)
		}
	}
	wg.Wait()
	close(order)
	position := 0
	for got := range order {
		if got != position {
			t.Fatalf("completion order position %d = waiter %d, want FIFO", position, got)
		}
		position++
	}
}

func TestQueueDepthLimitRejectsImmediately(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(1), nil), nil)
	apiKeyID := uuid.New()
	clock := newQueueTestClock(queueTestBase)

	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: clock.Now()}); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}

	service.mu.Lock()
	for i := 0; i < queueMaxDepth; i++ {
		service.queues["global"] = append(service.queues["global"], &queueWaiter{ready: make(chan struct{})})
	}
	service.mu.Unlock()

	_, err := service.acquireQueued(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: clock.Now()}, clock.Now, clock.Sleep)
	var limitErr LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("full queue error = %v, want LimitError", err)
	}
	if clock.sleeps != 0 {
		t.Fatal("full queue must not sleep")
	}
}

func TestQueueDeadlineExceededReturnsLimit(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(1), nil), nil)
	apiKeyID := uuid.New()
	clock := newQueueTestClock(queueTestBase)
	staleStart := clock.Now().Add(-2 * queueMaxWait)

	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: staleStart}); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}

	stale := AcquireInput{APIKeyID: apiKeyID, RequestedAt: staleStart}
	_, err := service.acquireQueued(context.Background(), stale, clock.Now, clock.Sleep)
	var limitErr LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("expired deadline error = %v, want LimitError", err)
	}
	if clock.sleeps != 0 {
		t.Fatal("expired deadline must not sleep")
	}
}

func TestQueueImpossibleTPMFailsFast(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"model": "gpt-5.5", "messages": []any{map[string]any{"role": "user", "content": "hello"}}, "max_tokens": 4000}
	estimate := EstimateTokens(payload)
	limit := estimate / 2
	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, nil, &limit), nil)
	clock := newQueueTestClock(queueTestBase)

	_, err := service.acquireQueued(context.Background(), AcquireInput{APIKeyID: uuid.New(), Payload: payload, EstimateTPM: true, RequestedAt: clock.Now()}, clock.Now, clock.Sleep)
	var limitErr LimitError
	if !errors.As(err, &limitErr) || limitErr.LimitType != "tpm" {
		t.Fatalf("impossible tpm error = %v, want tpm LimitError", err)
	}
	if clock.sleeps != 0 {
		t.Fatal("impossible tpm request must not wait")
	}
}

func TestQueueContextCancelCleansUp(t *testing.T) {
	t.Parallel()

	service := memoryTestService(t, enabledLimit(store.RateLimitScopeGlobal, ratelimitInt64(1), nil), nil)
	apiKeyID := uuid.New()
	clock := newQueueTestClock(queueTestBase)

	if _, err := service.Acquire(context.Background(), AcquireInput{APIKeyID: apiKeyID, RequestedAt: clock.Now()}); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}

	blocker := &queueWaiter{ready: make(chan struct{})}
	service.mu.Lock()
	service.queues["global"] = append(service.queues["global"], blocker)
	service.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := service.acquireQueued(ctx, AcquireInput{APIKeyID: apiKeyID, RequestedAt: clock.Now()}, clock.Now, clock.Sleep)
		done <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		service.mu.Lock()
		depth := len(service.queues["global"])
		service.mu.Unlock()
		if depth == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter never enqueued behind blocker")
		}
		time.Sleep(time.Millisecond)
	}

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v, want context.Canceled", err)
	}

	service.mu.Lock()
	blocker.abandoned = true
	service.mu.Unlock()
	service.finishWaiter("global", blocker)

	service.mu.Lock()
	depth := len(service.queues["global"])
	service.mu.Unlock()
	if depth != 0 {
		t.Fatalf("queue depth after cleanup = %d, want 0", depth)
	}
}
