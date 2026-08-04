package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

// A limit change must replace the existing scope:id entry rather than leaking a
// new channel under a limit-suffixed key, and the new limit must take effect. (F16.)
func TestUpstreamConcurrencyCacheReplacesLimiterOnLimitChange(t *testing.T) {
	t.Parallel()

	cache := newUpstreamConcurrencyCache()
	id := uuid.New()

	r1, err := cache.Acquire(context.Background(), concurrencyScopeSite, id, 1)
	if err != nil {
		t.Fatalf("acquire limit 1: %v", err)
	}
	r1()

	r2, err := cache.Acquire(context.Background(), concurrencyScopeSite, id, 2)
	if err != nil {
		t.Fatalf("acquire after limit change: %v", err)
	}
	defer r2()
	r3, err := cache.Acquire(context.Background(), concurrencyScopeSite, id, 2)
	if err != nil {
		t.Fatalf("raised limit should allow a second holder: %v", err)
	}
	defer r3()

	if got := len(cache.limiters); got != 1 {
		t.Fatalf("limit change leaked entries: map size %d, want 1", got)
	}
}

// Idle limiters with no in-flight holders must be swept so a churn of ids cannot
// grow the map without bound; entries with active holders must survive. (F16.)
func TestUpstreamConcurrencyCacheEvictsIdleLimiters(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	cache := newUpstreamConcurrencyCache()
	cache.now = func() time.Time { return now }

	held, err := cache.Acquire(context.Background(), concurrencyScopeCredential, uuid.New(), 1)
	if err != nil {
		t.Fatalf("acquire held limiter: %v", err)
	}
	defer held() // never released before the sweep

	for i := 0; i < 5; i++ {
		release, err := cache.Acquire(context.Background(), concurrencyScopeCredential, uuid.New(), 1)
		if err != nil {
			t.Fatalf("acquire idle limiter %d: %v", i, err)
		}
		release() // channel now empty → eligible for eviction once idle
	}
	if got := len(cache.limiters); got != 6 {
		t.Fatalf("map size before sweep = %d, want 6", got)
	}

	now = now.Add(concurrencyIdleTTL + concurrencySweepInterval + time.Second)
	release, err := cache.Acquire(context.Background(), concurrencyScopeCredential, uuid.New(), 1)
	if err != nil {
		t.Fatalf("acquire after idle window: %v", err)
	}
	defer release()

	// 5 idle-empty entries evicted; the held entry and the new one remain.
	if got := len(cache.limiters); got != 2 {
		t.Fatalf("idle limiters not evicted: map size %d, want 2", got)
	}
}

func TestUpstreamConcurrencyCacheRejectsWhenSiteLimitIsReached(t *testing.T) {
	t.Parallel()

	cache := newUpstreamConcurrencyCache()
	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	release, err := cache.Acquire(context.Background(), concurrencyScopeSite, siteID, 1)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	_, err = cache.Acquire(context.Background(), concurrencyScopeSite, siteID, 1)
	if !errors.Is(err, errSiteConcurrencyLimited) {
		t.Fatalf("expected errSiteConcurrencyLimited, got %v", err)
	}
}

func TestUpstreamConcurrencyCacheSeparatesScopes(t *testing.T) {
	t.Parallel()

	cache := newUpstreamConcurrencyCache()
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	releaseSite, err := cache.Acquire(context.Background(), concurrencyScopeSite, id, 1)
	if err != nil {
		t.Fatalf("acquire site limiter: %v", err)
	}
	defer releaseSite()

	releaseModel, err := cache.Acquire(context.Background(), concurrencyScopeModel, id, 1)
	if err != nil {
		t.Fatalf("expected separate model scope limiter, got %v", err)
	}
	defer releaseModel()
}

func TestCooldownInputForFailureSkipsLocalConcurrencyLimit(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	_, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-4o-mini"},
	}, gatewayAttemptResult{
		statusCode: http.StatusServiceUnavailable,
		errorType:  "site_concurrency_limited",
	})
	if ok {
		t.Fatal("expected local concurrency limit to avoid cooldown activation")
	}
}

func TestCooldownInputForFailureSkipsModelAndCredentialConcurrencyLimit(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	for _, errorType := range []string{"model_concurrency_limited", "credential_concurrency_limited"} {
		_, ok := cooldownInputForFailure(routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: siteID},
			Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-4o-mini"},
		}, gatewayAttemptResult{
			statusCode: http.StatusServiceUnavailable,
			errorType:  errorType,
		})
		if ok {
			t.Fatalf("expected %s to avoid cooldown activation", errorType)
		}
	}
}
