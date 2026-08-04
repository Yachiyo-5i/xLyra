package gateway

import (
	"net/http"
	"testing"
	"time"
)

func TestApplyGoogleGatewayHeadersUsesAPIKeyAndRemovesBearerAuth(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://generativelanguage.googleapis.com/v1beta/models/gemini:generateContent", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer old")

	applyGoogleGatewayHeaders(req, " gemini-key ")

	if got := req.Header.Get("x-goog-api-key"); got != "gemini-key" {
		t.Fatalf("x-goog-api-key = %q, want gemini-key", got)
	}
	if got := req.Header.Get("User-Agent"); got != googleGatewayUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, googleGatewayUserAgent)
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want application/json", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should be removed, got %q", got)
	}
}

func TestClassifyGoogleUpstreamErrorHandlesRateLimitAndServerErrors(t *testing.T) {
	t.Parallel()

	if _, ok := classifyGoogleUpstreamError(http.StatusBadRequest, []byte(`{"error":{"message":"bad request"}}`), time.Now()); ok {
		t.Fatal("400 should not be classified as retryable Google upstream error")
	}

	rateLimited, ok := classifyGoogleUpstreamError(http.StatusTooManyRequests, []byte(`{
		"error": {
			"message": "quota exhausted",
			"details": [{"retryDelay": "45s"}]
		}
	}`), time.Now())
	if !ok {
		t.Fatal("expected Google 429 to be classified")
	}
	if rateLimited.ErrorType != "google_rate_limited" || rateLimited.CooldownReason != "google_rate_limited" || rateLimited.CooldownScope != "model" {
		t.Fatalf("unexpected rate limit classification: %#v", rateLimited)
	}
	if rateLimited.ErrorMessage != "quota exhausted" {
		t.Fatalf("ErrorMessage = %q, want quota exhausted", rateLimited.ErrorMessage)
	}
	if rateLimited.CooldownDuration != 45*time.Second || rateLimited.RetryAfterSeconds != 45 {
		t.Fatalf("unexpected retry-after: duration=%s seconds=%d", rateLimited.CooldownDuration, rateLimited.RetryAfterSeconds)
	}

	defaultRetry, ok := classifyGoogleUpstreamError(http.StatusTooManyRequests, []byte(`{}`), time.Now())
	if !ok {
		t.Fatal("expected Google 429 without retryDelay to be classified")
	}
	if defaultRetry.CooldownDuration != time.Minute || defaultRetry.RetryAfterSeconds != 60 {
		t.Fatalf("default retry-after = %s/%d, want 60s", defaultRetry.CooldownDuration, defaultRetry.RetryAfterSeconds)
	}

	serverError, ok := classifyGoogleUpstreamError(http.StatusServiceUnavailable, []byte(`{"error":{"status":"UNAVAILABLE"}}`), time.Now())
	if !ok {
		t.Fatal("expected Google 503 to be classified")
	}
	if serverError.ErrorType != "google_upstream_error" || serverError.ErrorMessage != "UNAVAILABLE" {
		t.Fatalf("unexpected server error classification: %#v", serverError)
	}
	if serverError.CooldownDuration != 0 || serverError.RetryAfterSeconds != 0 {
		t.Fatalf("server errors should not set retry-after cooldown: %#v", serverError)
	}
}

func TestApplyAntigravityGatewayHeadersAddsClientIdentity(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	applyAntigravityGatewayHeaders(req)

	if got := req.Header.Get("User-Agent"); got != antigravityGatewayUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, antigravityGatewayUserAgent)
	}
	if got := req.Header.Get("x-client-name"); got != "antigravity" {
		t.Fatalf("x-client-name = %q, want antigravity", got)
	}
	if got := req.Header.Get("x-client-version"); got != antigravityGatewayVersion {
		t.Fatalf("x-client-version = %q, want %q", got, antigravityGatewayVersion)
	}
	if got := req.Header.Get("x-machine-id"); got != "xlyra" {
		t.Fatalf("x-machine-id = %q, want xlyra", got)
	}
	if got := req.Header.Get("x-vscode-sessionid"); got != "xlyra" {
		t.Fatalf("x-vscode-sessionid = %q, want xlyra", got)
	}
}

func TestClassifyAntigravityUpstreamErrorHandlesQuotaDetails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

	classified, ok := classifyAntigravityUpstreamError(http.StatusTooManyRequests, []byte(`{
		"error": {
			"message": "model quota exhausted",
			"details": [{
				"reason": "QUOTA_EXHAUSTED",
				"metadata": {"quotaResetDelay": "90s"}
			}]
		}
	}`), now)
	if !ok {
		t.Fatal("expected quota exhausted response to be classified")
	}
	if classified.ErrorType != "antigravity_model_rate_limited" || classified.CooldownScope != "model" {
		t.Fatalf("unexpected Antigravity classification: %#v", classified)
	}
	if classified.ErrorMessage != "model quota exhausted" {
		t.Fatalf("ErrorMessage = %q, want model quota exhausted", classified.ErrorMessage)
	}
	if classified.CooldownDuration != 90*time.Second || classified.RetryAfterSeconds != 90 {
		t.Fatalf("unexpected quota reset delay: %s/%d", classified.CooldownDuration, classified.RetryAfterSeconds)
	}

	resetAtBody := []byte(`{
		"error": {
			"status": "RESOURCE_EXHAUSTED",
			"details": [{
				"metadata": {
					"reason": "RATE_LIMIT_EXCEEDED",
					"quotaResetTimeStamp": "2026-06-22T10:02:00Z"
				}
			}]
		}
	}`)
	classified, ok = classifyAntigravityUpstreamError(http.StatusTooManyRequests, resetAtBody, now)
	if !ok {
		t.Fatal("expected rate limit response to be classified")
	}
	if classified.CooldownDuration != 2*time.Minute || classified.RetryAfterSeconds != 120 {
		t.Fatalf("timestamp retry-after = %s/%d, want 2m/120", classified.CooldownDuration, classified.RetryAfterSeconds)
	}

	defaultRetry, ok := classifyAntigravityUpstreamError(http.StatusTooManyRequests, []byte(`{"error":{"status":"QUOTA_EXHAUSTED"}}`), now)
	if !ok {
		t.Fatal("expected status fallback quota exhaustion to be classified")
	}
	if defaultRetry.CooldownDuration != 5*time.Minute || defaultRetry.RetryAfterSeconds != 300 {
		t.Fatalf("default retry-after = %s/%d, want 5m/300", defaultRetry.CooldownDuration, defaultRetry.RetryAfterSeconds)
	}

	if _, ok := classifyAntigravityUpstreamError(http.StatusTooManyRequests, []byte(`{"error":{"status":"INVALID_ARGUMENT"}}`), now); ok {
		t.Fatal("unrelated Antigravity 429 reason should not be classified")
	}
	if _, ok := classifyAntigravityUpstreamError(http.StatusServiceUnavailable, []byte(`{"error":{"status":"QUOTA_EXHAUSTED"}}`), now); ok {
		t.Fatal("non-429 Antigravity quota response should not be classified")
	}
}

func TestAntigravityDurationAndPayloadHelpers(t *testing.T) {
	t.Parallel()

	if duration, ok := parseAntigravityDuration(" 3.5s "); !ok || duration != 3500*time.Millisecond {
		t.Fatalf("parseAntigravityDuration = %s/%v, want 3.5s true", duration, ok)
	}
	for _, value := range []string{"", "0s", "-1s", "soon"} {
		if duration, ok := parseAntigravityDuration(value); ok || duration != 0 {
			t.Fatalf("parseAntigravityDuration(%q) = %s/%v, want 0 false", value, duration, ok)
		}
	}

	payload := antigravityErrorPayload([]byte(`{"error":{"message":"quota","details":[{"reason":"QUOTA_EXHAUSTED"}]}}`))
	if payload["message"] != "quota" {
		t.Fatalf("antigravityErrorPayload message = %#v, want quota", payload["message"])
	}
	if len(detailsFromAny(payload["details"])) != 1 {
		t.Fatalf("detailsFromAny = %#v, want one detail", payload["details"])
	}
	if payload := antigravityErrorPayload([]byte(`not json`)); len(payload) != 0 {
		t.Fatalf("invalid payload = %#v, want empty", payload)
	}
}
