package ratelimit

import (
	"context"
	"errors"
	"time"
)

const (
	queueMaxWait  = 60 * time.Second
	queueMaxDepth = 100
)

type queueWaiter struct {
	ready     chan struct{}
	abandoned bool
}

func (s *Service) AcquireQueued(ctx context.Context, input AcquireInput) (*Reservation, error) {
	return s.acquireQueued(ctx, input, time.Now, realSleep)
}

func realSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) acquireQueued(ctx context.Context, input AcquireInput, nowFn func() time.Time, sleepFn func(context.Context, time.Duration) error) (*Reservation, error) {
	start := input.RequestedAt
	if start.IsZero() {
		start = nowFn()
	}
	deadline := start.Add(queueMaxWait)

	attempt := input
	attempt.RequestedAt = start
	reservation, err := s.Acquire(ctx, attempt)
	if err == nil {
		return reservation, nil
	}
	var limitErr LimitError
	if !errors.As(err, &limitErr) {
		return nil, err
	}

	for {
		if queueWaitImpossible(limitErr, input) {
			return nil, limitErr
		}
		if nowFn().After(deadline) {
			return nil, limitErr
		}

		queueScope := limitErr.ScopeKey
		waiter, ok := s.enqueueWaiter(queueScope)
		if !ok {
			return nil, limitErr
		}

		select {
		case <-waiter.ready:
		case <-ctx.Done():
			s.finishWaiter(queueScope, waiter)
			return nil, ctx.Err()
		}

		var terminalErr error
		scopeChanged := false
		for {
			attempt.RequestedAt = nowFn()
			reservation, err = s.acquire(ctx, attempt, queueScope)
			if err == nil {
				break
			}
			nextErr := LimitError{}
			if !errors.As(err, &nextErr) {
				terminalErr = err
				break
			}
			limitErr = nextErr
			if nextErr.ScopeKey != queueScope {
				scopeChanged = true
				break
			}
			if nextErr.LimitType == "rpm" {
				remaining := deadline.Sub(nowFn())
				if remaining <= 0 {
					terminalErr = nextErr
					break
				}
				if waitErr := s.waitForRelease(ctx, queueScope, remaining); waitErr != nil {
					terminalErr = waitErr
					if errors.Is(waitErr, errReleaseWaitTimeout) {
						terminalErr = nextErr
					}
					break
				}
				continue
			}
			wait := time.Duration(nextErr.RetryAfterSeconds) * time.Second
			if nowFn().Add(wait).After(deadline) {
				terminalErr = nextErr
				break
			}
			if sleepErr := sleepFn(ctx, wait); sleepErr != nil {
				terminalErr = sleepErr
				break
			}
		}

		s.finishWaiter(queueScope, waiter)

		if reservation != nil {
			return reservation, nil
		}
		if terminalErr != nil {
			return nil, terminalErr
		}
		if !scopeChanged {
			return nil, limitErr
		}
	}
}

var errReleaseWaitTimeout = errors.New("rate limit release wait timed out")

func (s *Service) waitForRelease(ctx context.Context, scopeKey string, maxWait time.Duration) error {
	release := s.releaseSignal(scopeKey)
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errReleaseWaitTimeout
	}
}

func queueWaitImpossible(limitErr LimitError, input AcquireInput) bool {
	if limitErr.LimitType != "tpm" || limitErr.Limit <= 0 || !input.EstimateTPM {
		return false
	}
	return EstimateTokens(input.Payload) > limitErr.Limit
}

func (s *Service) enqueueWaiter(scopeKey string) (*queueWaiter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	waiters := s.queues[scopeKey]
	if len(waiters) >= queueMaxDepth {
		return nil, false
	}
	waiter := &queueWaiter{ready: make(chan struct{})}
	s.queues[scopeKey] = append(waiters, waiter)
	if len(s.queues[scopeKey]) == 1 {
		close(waiter.ready)
	}
	return waiter, true
}

func (s *Service) finishWaiter(scopeKey string, waiter *queueWaiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	waiter.abandoned = true
	waiters := s.queues[scopeKey]
	for len(waiters) > 0 && waiters[0].abandoned {
		waiters = waiters[1:]
	}
	s.queues[scopeKey] = waiters
	if len(waiters) == 0 {
		delete(s.queues, scopeKey)
		return
	}
	select {
	case <-waiters[0].ready:
	default:
		close(waiters[0].ready)
	}
}
