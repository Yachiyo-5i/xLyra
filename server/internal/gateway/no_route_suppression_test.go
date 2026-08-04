package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestNoRouteSuppressionCacheReusesRetryAfterWithinTTL(t *testing.T) {
	t.Parallel()

	cache := newNoRouteSuppressionCache()
	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	key := noRouteSuppressionKey{
		APIKeyID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Endpoint: gatewayEndpointChatCompletions,
		ModelKey: "gpt-4o-mini",
		Code:     "no_route_candidates",
	}

	if retryAfter, ok := cache.retryAfter(key, now); ok || retryAfter != 0 {
		t.Fatalf("unexpected cache hit before remember: retryAfter=%d ok=%v", retryAfter, ok)
	}
	cache.remember(key, 30*time.Second, 30, now)
	if retryAfter, ok := cache.retryAfter(key, now.Add(time.Second)); !ok || retryAfter != 30 {
		t.Fatalf("expected cached retry-after inside ttl, retryAfter=%d ok=%v", retryAfter, ok)
	}
	if retryAfter, ok := cache.retryAfter(key, now.Add(31*time.Second)); ok || retryAfter != 0 {
		t.Fatalf("unexpected cache hit after ttl: retryAfter=%d ok=%v", retryAfter, ok)
	}
}

func TestNoRouteSuppressionKeySortsAllowedIDs(t *testing.T) {
	t.Parallel()

	siteA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	siteB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	modelA := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	modelB := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	base := noRouteSuppressionKey{
		APIKeyID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Endpoint: gatewayEndpointResponses,
		ModelKey: "gpt-5",
		Code:     "no_route_candidates",
	}
	left := base
	left.AllowedSiteIDs = []uuid.UUID{siteA, siteB}
	left.AllowedSiteModelIDs = []uuid.UUID{modelB, modelA}
	right := base
	right.AllowedSiteIDs = []uuid.UUID{siteB, siteA}
	right.AllowedSiteModelIDs = []uuid.UUID{modelA, modelB}

	if left.cacheKey() != right.cacheKey() {
		t.Fatalf("expected allowed id ordering not to affect suppression key\nleft:  %s\nright: %s", left.cacheKey(), right.cacheKey())
	}
}

func TestNoRouteRetryAfterFromCooldownsUsesMatchingActiveCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	siteID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	modelID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	otherModelID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	credentialID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

	got := noRouteRetryAfterFromCooldowns([]store.RouteCooldown{
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: otherModelID, Valid: true},
			ActiveUntil: now.Add(5 * time.Second),
		},
		{
			SiteID:           siteID,
			SiteModelID:      uuid.NullUUID{UUID: modelID, Valid: true},
			SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
			ActiveUntil:      now.Add(5 * time.Second),
		},
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			ActiveUntil: now.Add(90 * time.Second),
		},
	}, routeAccessForNoRoute{
		ModelKey:            "gpt-5",
		AllowedSiteIDs:      []uuid.UUID{siteID},
		AllowedSiteModelIDs: []uuid.UUID{modelID},
	}, now)

	if got != 90*time.Second {
		t.Fatalf("retry after = %v, want 90s", got)
	}
}

func TestNoRouteRetryAfterFromCooldownsFallsBackWhenCooldownIsUnrelated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	allowedModelID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	got := noRouteRetryAfterFromCooldowns([]store.RouteCooldown{
		{
			SiteID:      uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			SiteModelID: uuid.NullUUID{UUID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), Valid: true},
			ActiveUntil: now.Add(90 * time.Second),
		},
	}, routeAccessForNoRoute{
		ModelKey:            "gpt-5",
		AllowedSiteModelIDs: []uuid.UUID{allowedModelID},
	}, now)

	if got != noRouteSuppressionDefaultTTL {
		t.Fatalf("retry after = %v, want default %v", got, noRouteSuppressionDefaultTTL)
	}
}

func TestNoRouteRetryAfterFromCooldownsChoosesShortestMatchingActiveCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	siteID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	otherSiteID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	modelID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")

	got := noRouteRetryAfterFromCooldowns([]store.RouteCooldown{
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			ActiveUntil: now.Add(45 * time.Second),
		},
		{
			SiteID:      otherSiteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			ActiveUntil: now.Add(10 * time.Second),
		},
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			Source:      "site_health",
			ActiveUntil: now.Add(5 * time.Second),
		},
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			ActiveUntil: now.Add(30 * time.Second),
		},
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			ActiveUntil: now.Add(-time.Second),
		},
	}, routeAccessForNoRoute{
		AllowedSiteIDs:      []uuid.UUID{siteID},
		AllowedSiteModelIDs: []uuid.UUID{modelID},
	}, now)

	if got != 30*time.Second {
		t.Fatalf("retry after = %v, want shortest matching active cooldown 30s", got)
	}
}

func TestNoRouteSuppressionHandlerWrappers(t *testing.T) {
	t.Parallel()

	key := noRouteSuppressionKey{
		APIKeyID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Endpoint: gatewayEndpointChatCompletions,
		ModelKey: "gpt-5",
		Code:     "no_route_candidates",
	}
	handler := Handler{noRoutes: newNoRouteSuppressionCache()}

	if retryAfter, ok := handler.cachedNoRouteRetryAfter(key); ok || retryAfter != 0 {
		t.Fatalf("unexpected cached retry after before remember: retryAfter=%d ok=%v", retryAfter, ok)
	}
	handler.rememberNoRoute(key, 0)
	if retryAfter, ok := handler.cachedNoRouteRetryAfter(key); !ok || retryAfter != 1 {
		t.Fatalf("rememberNoRoute should clamp retry-after to at least one second, retryAfter=%d ok=%v", retryAfter, ok)
	}

	handler = Handler{}
	if retryAfter, ok := handler.cachedNoRouteRetryAfter(key); ok || retryAfter != 0 {
		t.Fatalf("nil cache should miss, retryAfter=%d ok=%v", retryAfter, ok)
	}
	handler.rememberNoRoute(key, 30)
	if retryAfter, ok := handler.cachedNoRouteRetryAfter(key); ok || retryAfter != 0 {
		t.Fatalf("nil cache remember should be a no-op, retryAfter=%d ok=%v", retryAfter, ok)
	}
}

func TestNoRouteRetryAfterSecondsDefaultsAndClamps(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	if got := handler.noRouteRetryAfterSeconds(context.Background(), routeAccessForNoRoute{}); got != 30 {
		t.Fatalf("nil router retry-after seconds = %d, want 30", got)
	}

	now := time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)
	siteID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	modelID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	if got := noRouteRetryAfterFromCooldowns([]store.RouteCooldown{{
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		ActiveUntil: now.Add(90 * time.Second),
	}}, routeAccessForNoRoute{
		AllowedSiteIDs:      []uuid.UUID{siteID},
		AllowedSiteModelIDs: []uuid.UUID{modelID},
	}, now); got != 90*time.Second {
		t.Fatalf("raw retry-after = %v, want 90s before handler clamp", got)
	}
}

func TestWriteNoRouteFailureUsesCachedRetryAfterEnvelope(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	modelID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	access := routeAccessForNoRoute{
		ModelKey:            "gpt-5",
		AllowedSiteIDs:      []uuid.UUID{siteID},
		AllowedSiteModelIDs: []uuid.UUID{modelID},
	}
	key := noRouteSuppressionKey{
		APIKeyID:            apiKeyID,
		Endpoint:            gatewayEndpointResponses,
		ModelKey:            access.ModelKey,
		AllowedSiteIDs:      access.AllowedSiteIDs,
		AllowedSiteModelIDs: access.AllowedSiteModelIDs,
		Code:                "no_route_candidates",
	}
	handler := Handler{noRoutes: newNoRouteSuppressionCache()}
	handler.rememberNoRoute(key, 17)

	req := httptest.NewRequest(http.MethodPost, gatewayEndpointResponses, nil)
	rec := httptest.NewRecorder()

	handler.writeNoRouteFailure(rec, req, gatewayEndpointResponses, "req-no-route", apiKeyID, time.Now(), gatewayRequest{
		Stream: true,
	}, access)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Retry-After"); got != "17" {
		t.Fatalf("Retry-After = %q, want 17", got)
	}

	var body struct {
		Error struct {
			Code              string `json:"code"`
			Message           string `json:"message"`
			RequestID         string `json:"request_id"`
			RetryAfterSeconds int64  `json:"retry_after_seconds"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "no_route_candidates" {
		t.Fatalf("error code = %q, want no_route_candidates", body.Error.Code)
	}
	if body.Error.Message != "no available upstream route candidates" {
		t.Fatalf("error message = %q", body.Error.Message)
	}
	if body.Error.RequestID != "req-no-route" {
		t.Fatalf("request_id = %q, want req-no-route", body.Error.RequestID)
	}
	if body.Error.RetryAfterSeconds != 17 {
		t.Fatalf("retry_after_seconds = %d, want 17", body.Error.RetryAfterSeconds)
	}
}
