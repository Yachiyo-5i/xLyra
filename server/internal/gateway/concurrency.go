package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
)

const (
	// How often idle limiters are swept, and how long a limiter may sit unused
	// (with no in-flight holders) before it is evicted. Together these bound the
	// limiters map so a churn of credential/model IDs or limit changes cannot grow
	// it without limit.
	concurrencySweepInterval = time.Minute
	concurrencyIdleTTL       = 10 * time.Minute
)

var errSiteConcurrencyLimited = errors.New("site concurrency limit reached")
var errModelConcurrencyLimited = errors.New("model concurrency limit reached")
var errCredentialConcurrencyLimited = errors.New("credential concurrency limit reached")

type concurrencyScope string

const (
	concurrencyScopeSite       concurrencyScope = "site"
	concurrencyScopeModel      concurrencyScope = "site_model"
	concurrencyScopeCredential concurrencyScope = "credential"
)

type concurrencyLimiter struct {
	ch       chan struct{}
	limit    int
	lastUsed time.Time
}

type upstreamConcurrencyCache struct {
	mu        sync.Mutex
	limiters  map[string]*concurrencyLimiter
	lastSweep time.Time
	now       func() time.Time
}

func newUpstreamConcurrencyCache() *upstreamConcurrencyCache {
	return &upstreamConcurrencyCache{
		limiters: map[string]*concurrencyLimiter{},
		now:      time.Now,
	}
}

// limiterKey excludes the limit so a limit change replaces the existing entry
// (see Acquire) rather than leaking a stale channel under a new key.
func limiterKey(scope concurrencyScope, id uuid.UUID) string {
	return fmt.Sprintf("%s:%s", scope, id.String())
}

// evictIdleLocked drops limiters that have no in-flight holders (channel empty)
// and have been unused past the idle TTL. Caller holds c.mu.
func (c *upstreamConcurrencyCache) evictIdleLocked(now time.Time) {
	for key, entry := range c.limiters {
		if len(entry.ch) == 0 && now.Sub(entry.lastUsed) > concurrencyIdleTTL {
			delete(c.limiters, key)
		}
	}
}

func limiterScopeError(scope concurrencyScope) error {
	switch scope {
	case concurrencyScopeModel:
		return errModelConcurrencyLimited
	case concurrencyScopeCredential:
		return errCredentialConcurrencyLimited
	default:
		return errSiteConcurrencyLimited
	}
}

func (c *upstreamConcurrencyCache) Acquire(ctx context.Context, scope concurrencyScope, id uuid.UUID, limit int) (func(), error) {
	if c == nil || limit <= 0 {
		return func() {}, nil
	}

	key := limiterKey(scope, id)

	c.mu.Lock()
	now := c.now()
	if now.Sub(c.lastSweep) >= concurrencySweepInterval {
		c.evictIdleLocked(now)
		c.lastSweep = now
	}
	entry, ok := c.limiters[key]
	if !ok || entry.limit != limit {
		// New scope/id, or the configured limit changed: (re)build the channel.
		// In-flight holders keep releasing their captured channel, so replacing
		// the map entry is safe.
		entry = &concurrencyLimiter{ch: make(chan struct{}, limit), limit: limit}
		c.limiters[key] = entry
	}
	entry.lastUsed = now
	limiter := entry.ch
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case limiter <- struct{}{}:
		return func() {
			select {
			case <-limiter:
			default:
			}
		}, nil
	default:
		return nil, limiterScopeError(scope)
	}
}

func (h Handler) acquireUpstreamConcurrency(
	ctx context.Context,
	candidate routeengine.Candidate,
	credentialID uuid.UUID,
	cfg *site.GatewayConfig,
) (func(), error) {
	if cfg == nil {
		return func() {}, nil
	}

	releases := make([]func(), 0, 3)
	releaseAll := func() {
		for index := len(releases) - 1; index >= 0; index-- {
			releases[index]()
		}
	}

	acquire := func(scope concurrencyScope, id uuid.UUID, limit *int) error {
		if limit == nil || *limit <= 0 || id == uuid.Nil {
			return nil
		}
		release, err := h.limits.Acquire(ctx, scope, id, *limit)
		if err != nil {
			releaseAll()
			return err
		}
		releases = append(releases, release)
		return nil
	}

	if err := acquire(concurrencyScopeSite, candidate.Site.ID, cfg.MaxConcurrency); err != nil {
		return nil, err
	}
	if err := acquire(concurrencyScopeModel, candidate.Model.SiteModelID, cfg.MaxModelConcurrency); err != nil {
		return nil, err
	}
	if err := acquire(concurrencyScopeCredential, credentialID, cfg.MaxCredentialConcurrency); err != nil {
		return nil, err
	}

	return releaseAll, nil
}
