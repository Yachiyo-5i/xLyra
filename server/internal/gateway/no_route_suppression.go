package gateway

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

const (
	noRouteSuppressionDefaultTTL = 30 * time.Second
	noRouteSuppressionMaxTTL     = 60 * time.Second
)

type routeAccessForNoRoute struct {
	ModelKey            string
	AllowedSiteIDs      []uuid.UUID
	AllowedSiteModelIDs []uuid.UUID
}

type noRouteSuppressionKey struct {
	APIKeyID            uuid.UUID
	Endpoint            string
	ModelKey            string
	AllowedSiteIDs      []uuid.UUID
	AllowedSiteModelIDs []uuid.UUID
	Code                string
}

type noRouteSuppressionCache struct {
	mu      sync.Mutex
	entries map[string]noRouteSuppressionEntry
}

type noRouteSuppressionEntry struct {
	ExpiresAt  time.Time
	RetryAfter int64
}

func newNoRouteSuppressionCache() *noRouteSuppressionCache {
	return &noRouteSuppressionCache{entries: map[string]noRouteSuppressionEntry{}}
}

func (h Handler) cachedNoRouteRetryAfter(key noRouteSuppressionKey) (int64, bool) {
	if h.noRoutes == nil {
		return 0, false
	}
	return h.noRoutes.retryAfter(key, time.Now())
}

func (h Handler) rememberNoRoute(key noRouteSuppressionKey, retryAfter int64) {
	if h.noRoutes == nil {
		return
	}
	h.noRoutes.remember(key, time.Duration(retryAfter)*time.Second, retryAfter, time.Now())
}

func (c *noRouteSuppressionCache) retryAfter(key noRouteSuppressionKey, now time.Time) (int64, bool) {
	if c == nil {
		return 0, false
	}
	cacheKey := key.cacheKey()

	c.mu.Lock()
	defer c.mu.Unlock()

	c.deleteExpired(now)
	if entry, ok := c.entries[cacheKey]; ok && entry.ExpiresAt.After(now) {
		return entry.RetryAfter, true
	}
	return 0, false
}

func (c *noRouteSuppressionCache) remember(key noRouteSuppressionKey, ttl time.Duration, retryAfter int64, now time.Time) {
	if c == nil {
		return
	}
	if ttl <= 0 {
		ttl = noRouteSuppressionDefaultTTL
	}
	if ttl > noRouteSuppressionMaxTTL {
		ttl = noRouteSuppressionMaxTTL
	}
	if retryAfter < 1 {
		retryAfter = 1
	}
	cacheKey := key.cacheKey()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.entries == nil {
		c.entries = map[string]noRouteSuppressionEntry{}
	}
	c.deleteExpired(now)
	c.entries[cacheKey] = noRouteSuppressionEntry{
		ExpiresAt:  now.Add(ttl),
		RetryAfter: retryAfter,
	}
}

func (c *noRouteSuppressionCache) deleteExpired(now time.Time) {
	for itemKey, entry := range c.entries {
		if !entry.ExpiresAt.After(now) {
			delete(c.entries, itemKey)
		}
	}
}

func (k noRouteSuppressionKey) cacheKey() string {
	return strings.Join([]string{
		k.APIKeyID.String(),
		strings.TrimSpace(k.Endpoint),
		strings.TrimSpace(k.ModelKey),
		joinSortedUUIDs(k.AllowedSiteIDs),
		joinSortedUUIDs(k.AllowedSiteModelIDs),
		strings.TrimSpace(k.Code),
	}, "|")
}

func (h Handler) noRouteRetryAfterSeconds(ctx context.Context, access routeAccessForNoRoute) int64 {
	ttl := h.noRouteRetryAfter(ctx, access, time.Now())
	if ttl <= 0 {
		ttl = noRouteSuppressionDefaultTTL
	}
	if ttl > noRouteSuppressionMaxTTL {
		ttl = noRouteSuppressionMaxTTL
	}
	seconds := int64(ttl.Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func (h Handler) noRouteRetryAfter(ctx context.Context, access routeAccessForNoRoute, now time.Time) time.Duration {
	if h.router == nil {
		return noRouteSuppressionDefaultTTL
	}
	cooldowns, err := h.router.ActiveCooldowns(ctx)
	if err != nil {
		return noRouteSuppressionDefaultTTL
	}
	return noRouteRetryAfterFromCooldowns(cooldowns, access, now)
}

func noRouteRetryAfterFromCooldowns(cooldowns []store.RouteCooldown, access routeAccessForNoRoute, now time.Time) time.Duration {
	allowedSites := uuidSetForNoRoute(access.AllowedSiteIDs)
	allowedModels := uuidSetForNoRoute(access.AllowedSiteModelIDs)

	var best time.Duration
	for _, item := range cooldowns {
		if item.Source == "site_health" || item.SiteCredentialID.Valid || !item.ActiveUntil.After(now) {
			continue
		}
		if allowedSites != nil {
			if _, ok := allowedSites[item.SiteID]; !ok {
				continue
			}
		}
		if item.SiteModelID.Valid {
			if allowedModels != nil {
				if _, ok := allowedModels[item.SiteModelID.UUID]; !ok {
					continue
				}
			}
		}
		remaining := item.ActiveUntil.Sub(now)
		if remaining <= 0 {
			continue
		}
		if best == 0 || remaining < best {
			best = remaining
		}
	}
	if best <= 0 {
		return noRouteSuppressionDefaultTTL
	}
	return best
}

func uuidSetForNoRoute(items []uuid.UUID) map[uuid.UUID]struct{} {
	if len(items) == 0 {
		return nil
	}
	result := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		result[item] = struct{}{}
	}
	return result
}

func joinSortedUUIDs(items []uuid.UUID) string {
	if len(items) == 0 {
		return ""
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.String())
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
