package gateway

import (
	"context"
	"math/rand"
	"net/http"
	"time"

	routeengine "xlyra/server/internal/router"
)

const (
	upstreamRateLimitMaxWait     = 60 * time.Second
	upstreamRateLimitDefaultWait = 5 * time.Second
	upstreamRateLimitMaxStep     = 30 * time.Second
)

func upstreamRateLimitWaitable(candidate routeengine.Candidate, result gatewayAttemptResult) bool {
	if result.success || result.responseStarted {
		return false
	}
	if result.upstreamStatusCode != http.StatusTooManyRequests {
		return false
	}
	if isCodexSite(candidate.Site.SiteType) || isAntigravitySite(candidate.Site.SiteType) {
		return false
	}
	return true
}

func upstreamRateLimitWaitDuration(result gatewayAttemptResult) time.Duration {
	wait := upstreamRateLimitDefaultWait
	if result.retryAfterSeconds > 0 {
		wait = time.Duration(result.retryAfterSeconds) * time.Second
		if wait > upstreamRateLimitMaxStep {
			wait = upstreamRateLimitMaxStep
		}
	}
	return wait + time.Duration(rand.Int63n(int64(time.Second)))
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
