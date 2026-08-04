package gateway

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestModelsCacheHandlerWrappersHandleNilAndInvalidate(t *testing.T) {
	apiKeyID := uuid.New()

	Handler{}.InvalidateModelsCache()
	Handler{}.InvalidateModelsCacheForAPIKey(apiKeyID)

	cache := newModelsCache()
	cache.items[apiKeyID] = modelsCacheEntry{
		payload: map[string]any{"object": "list"},
		cached:  cacheConcurrencyNow(),
	}
	handler := Handler{modelsCache: cache}

	handler.InvalidateModelsCacheForAPIKey(uuid.Nil)
	if _, ok := cache.get(apiKeyID); !ok {
		t.Fatal("nil api key invalidation removed cached payload")
	}

	handler.InvalidateModelsCacheForAPIKey(apiKeyID)
	if _, ok := cache.get(apiKeyID); ok {
		t.Fatal("api key invalidation left cached payload present")
	}

	cache.items[apiKeyID] = modelsCacheEntry{
		payload: map[string]any{"object": "list"},
		cached:  cacheConcurrencyNow(),
	}
	handler.InvalidateModelsCache()
	if _, ok := cache.get(apiKeyID); ok {
		t.Fatal("full invalidation left cached payload present")
	}
}

func TestPrewarmModelsCacheForAPIKeyGuardPaths(t *testing.T) {
	activeKey := store.APIKey{ID: uuid.New(), Status: "active", QuotaUnlimited: true}

	Handler{}.PrewarmModelsCacheForAPIKey(context.Background(), activeKey)
	Handler{modelsCache: newModelsCache()}.PrewarmModelsCacheForAPIKey(context.Background(), activeKey)
	Handler{modelsCache: newModelsCache(), logger: slog.Default()}.PrewarmModelsCache(context.Background())
}

func TestAcquireUpstreamConcurrencyReleasesAndClassifiesFailures(t *testing.T) {
	limit := 1
	candidate := cacheConcurrencyRouteCandidate()
	credentialID := uuid.New()
	cfg := &site.GatewayConfig{
		MaxConcurrency:           &limit,
		MaxModelConcurrency:      &limit,
		MaxCredentialConcurrency: &limit,
	}
	handler := Handler{limits: newUpstreamConcurrencyCache()}

	release, err := handler.acquireUpstreamConcurrency(context.Background(), candidate, credentialID, cfg)
	if err != nil {
		t.Fatalf("first acquire returned error: %v", err)
	}
	if _, err := handler.acquireUpstreamConcurrency(context.Background(), candidate, credentialID, cfg); !errors.Is(err, errSiteConcurrencyLimited) {
		t.Fatalf("second acquire error = %v, want site concurrency limit", err)
	}

	release()
	if release, err = handler.acquireUpstreamConcurrency(context.Background(), candidate, credentialID, cfg); err != nil {
		t.Fatalf("acquire after release returned error: %v", err)
	}
	release()

	if release, err = handler.acquireUpstreamConcurrency(context.Background(), candidate, credentialID, nil); err != nil {
		t.Fatalf("nil config acquire returned error: %v", err)
	}
	release()
}

func TestRateLimitNilGuards(t *testing.T) {
	handler := Handler{}

	reservation, limitErr, err := handler.acquireRateLimit(
		context.Background(),
		uuid.New(),
		chatCompletionsEndpointAdapter{},
		gatewayRequest{Payload: map[string]any{"model": "model-a"}},
		cacheConcurrencyNow(),
	)
	if reservation != nil || limitErr != nil || err != nil {
		t.Fatalf("nil service acquire = (%v, %v, %v), want all nil", reservation, limitErr, err)
	}

	handler.settleRateLimit(context.Background(), nil, 12)
}

func TestSiteModelDiagnosticTinyHelpers(t *testing.T) {
	if (&SiteModelTestError{Message: "bad route"}).Error() != "bad route" {
		t.Fatal("SiteModelTestError returned unexpected message")
	}
	var nilErr *SiteModelTestError
	if nilErr.Error() != "" {
		t.Fatal("nil SiteModelTestError should return empty string")
	}

	writer := &discardResponseWriter{}
	writer.WriteHeader(299)
	if got := writer.Header().Get("X-Diagnostic"); got != "" {
		t.Fatalf("unexpected header value after WriteHeader: %q", got)
	}
}

func cacheConcurrencyNow() time.Time {
	return time.Now()
}

func cacheConcurrencyRouteCandidate() routeengine.Candidate {
	return routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New()},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New()},
	}
}
