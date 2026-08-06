package gateway

import (
	"net/http"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func waitTestCandidate(siteType string) routeengine.Candidate {
	candidate := routeengine.Candidate{}
	candidate.Site.SiteType = siteType
	return candidate
}

func TestUpstreamRateLimitWaitable(t *testing.T) {
	limited := gatewayAttemptResult{statusCode: http.StatusTooManyRequests, upstreamStatusCode: http.StatusTooManyRequests}
	if !upstreamRateLimitWaitable(waitTestCandidate("openai"), limited) {
		t.Fatal("openai-compatible 429 must be waitable")
	}
	subscriptionLimited := limited
	subscriptionLimited.errorType = "upstream_subscription_limit_exceeded"
	if upstreamRateLimitWaitable(waitTestCandidate("openai"), subscriptionLimited) {
		t.Fatal("subscription limit 429 must not be waitable")
	}
	if upstreamRateLimitWaitable(waitTestCandidate("codex"), limited) {
		t.Fatal("codex 429 signals quota exhaustion and must not be waitable")
	}
	if upstreamRateLimitWaitable(waitTestCandidate("antigravity"), limited) {
		t.Fatal("antigravity 429 must not be waitable")
	}

	serverError := gatewayAttemptResult{statusCode: http.StatusBadGateway, upstreamStatusCode: http.StatusBadGateway}
	if upstreamRateLimitWaitable(waitTestCandidate("openai"), serverError) {
		t.Fatal("non-429 failures must not be waitable")
	}

	started := limited
	started.responseStarted = true
	if upstreamRateLimitWaitable(waitTestCandidate("openai"), started) {
		t.Fatal("started responses must never wait")
	}
}

func TestUpstreamRateLimitWaitDuration(t *testing.T) {
	base := upstreamRateLimitWaitDuration(gatewayAttemptResult{})
	if base < upstreamRateLimitDefaultWait || base >= upstreamRateLimitDefaultWait+time.Second {
		t.Fatalf("default wait = %v, want default plus jitter under 1s", base)
	}

	fromHeader := upstreamRateLimitWaitDuration(gatewayAttemptResult{retryAfterSeconds: 12})
	if fromHeader < 12*time.Second || fromHeader >= 13*time.Second {
		t.Fatalf("retry-after wait = %v, want 12s plus jitter", fromHeader)
	}

	capped := upstreamRateLimitWaitDuration(gatewayAttemptResult{retryAfterSeconds: 300})
	if capped >= upstreamRateLimitMaxStep+time.Second {
		t.Fatalf("capped wait = %v, want at most max step plus jitter", capped)
	}
}

func TestApplyRetryAfterHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "42")
	result := applyRetryAfterHeader(gatewayAttemptResult{upstreamStatusCode: http.StatusTooManyRequests}, headers)
	if result.retryAfterSeconds != 42 {
		t.Fatalf("retry-after = %d, want 42", result.retryAfterSeconds)
	}

	preset := applyRetryAfterHeader(gatewayAttemptResult{upstreamStatusCode: http.StatusTooManyRequests, retryAfterSeconds: 7}, headers)
	if preset.retryAfterSeconds != 7 {
		t.Fatal("existing retry-after must not be overwritten")
	}

	non429 := applyRetryAfterHeader(gatewayAttemptResult{upstreamStatusCode: http.StatusBadGateway}, headers)
	if non429.retryAfterSeconds != 0 {
		t.Fatal("retry-after must only apply to 429 responses")
	}

	httpDate := http.Header{}
	httpDate.Set("Retry-After", time.Now().Add(30*time.Second).UTC().Format(http.TimeFormat))
	dated := applyRetryAfterHeader(gatewayAttemptResult{upstreamStatusCode: http.StatusTooManyRequests}, httpDate)
	if dated.retryAfterSeconds < 25 || dated.retryAfterSeconds > 30 {
		t.Fatalf("http-date retry-after = %d, want ~30", dated.retryAfterSeconds)
	}
}

func TestRateLimitedCooldownDuration(t *testing.T) {
	if got := rateLimitedCooldownDuration(0); got != transientCooldownBaseDuration {
		t.Fatalf("no retry-after cooldown = %v, want %v", got, transientCooldownBaseDuration)
	}
	if got := rateLimitedCooldownDuration(30); got != 30*time.Second {
		t.Fatalf("30s retry-after cooldown = %v, want 30s", got)
	}
	if got := rateLimitedCooldownDuration(2); got != 5*time.Second {
		t.Fatalf("tiny retry-after cooldown = %v, want 5s floor", got)
	}
	if got := rateLimitedCooldownDuration(600); got != 2*time.Minute {
		t.Fatalf("huge retry-after cooldown = %v, want 2m cap", got)
	}
}

func TestPreferredFinalGatewayFailurePreservesUpstream429(t *testing.T) {
	t.Parallel()

	original := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		body:               []byte(`{"code":"USAGE_LIMIT_EXCEEDED"}`),
	}
	retained := retainUpstreamRateLimitFailure(nil, original)
	unavailable := &gatewayAttemptResult{statusCode: http.StatusBadGateway, errorType: "upstream_credential_unavailable"}
	got := preferredFinalGatewayFailure(unavailable, retained)
	if got == nil || got.statusCode != http.StatusTooManyRequests || string(got.body) != string(original.body) {
		t.Fatalf("preferred failure = %#v, want original 429", got)
	}

	serverError := &gatewayAttemptResult{statusCode: http.StatusBadGateway, errorType: "upstream_transport_error"}
	if got := preferredFinalGatewayFailure(serverError, retained); got != serverError {
		t.Fatalf("preferred failure = %#v, want later concrete upstream failure", got)
	}
}
