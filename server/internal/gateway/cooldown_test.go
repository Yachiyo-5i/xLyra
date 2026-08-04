package gateway

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestUpstreamNoResponseFailureStreakRequiresThreshold(t *testing.T) {
	t.Parallel()

	attempts := []store.RequestLog{
		noResponseRequestLog(),
		noResponseRequestLog(),
	}
	if upstreamNoResponseFailureStreakReached(attempts, 3) {
		t.Fatal("expected two no-response failures to stay below cooldown threshold")
	}

	attempts = append(attempts, noResponseRequestLog())
	if !upstreamNoResponseFailureStreakReached(attempts, 3) {
		t.Fatal("expected three no-response failures to reach cooldown threshold")
	}
}

func TestUpstreamNoResponseFailureStreakStopsOnSuccess(t *testing.T) {
	t.Parallel()

	attempts := []store.RequestLog{
		noResponseRequestLog(),
		successRequestLog(),
		noResponseRequestLog(),
	}
	if upstreamNoResponseFailureStreakReached(attempts, 3) {
		t.Fatal("expected successful attempt to break no-response failure streak")
	}
}

func TestUpstreamNoResponseFailureStreakSkipsDiagnosticAttempts(t *testing.T) {
	t.Parallel()

	diagnostic := noResponseRequestLog()
	diagnostic.Metadata = store.JSON([]byte(`{"scope":"site_model_test","no_upstream_response":true}`))

	attempts := []store.RequestLog{
		noResponseRequestLog(),
		diagnostic,
		noResponseRequestLog(),
		noResponseRequestLog(),
	}
	if !upstreamNoResponseFailureStreakReached(attempts, 3) {
		t.Fatal("expected diagnostic attempts to be skipped in gateway no-response failure streak")
	}
}

func TestRequestLogNoUpstreamResponseFailureRejectsExplicitUpstreamResponse(t *testing.T) {
	t.Parallel()

	item := noResponseRequestLog()
	item.Metadata = store.JSON([]byte(`{"upstream_status_code":500,"stream_started":false}`))
	if requestLogNoUpstreamResponseFailure(item) {
		t.Fatal("expected upstream status metadata to disqualify no-response failure")
	}
}

func TestRequestLogNoUpstreamResponseFailureRejectsStartedStream(t *testing.T) {
	t.Parallel()

	item := noResponseRequestLog()
	item.Metadata = store.JSON([]byte(`{"upstream_status_code":0,"stream_started":true}`))
	if requestLogNoUpstreamResponseFailure(item) {
		t.Fatal("expected started stream metadata to disqualify no-response failure")
	}
}

func TestShouldTryNextCredentialRetriesOnlyCredentialScopedFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		item gatewayAttemptResult
		want bool
	}{
		{name: "success", item: gatewayAttemptResult{success: true, statusCode: http.StatusUnauthorized}, want: false},
		{name: "unauthorized", item: gatewayAttemptResult{statusCode: http.StatusUnauthorized}, want: true},
		{name: "forbidden", item: gatewayAttemptResult{statusCode: http.StatusForbidden}, want: true},
		{name: "not_found", item: gatewayAttemptResult{statusCode: http.StatusNotFound}, want: true},
		{name: "rate_limited", item: gatewayAttemptResult{statusCode: http.StatusTooManyRequests}, want: true},
		{name: "decrypt_failed", item: gatewayAttemptResult{errorType: "upstream_credential_decrypt_failed"}, want: true},
		{name: "credential_concurrency", item: gatewayAttemptResult{errorType: "credential_concurrency_limited"}, want: true},
		{name: "server_error", item: gatewayAttemptResult{statusCode: http.StatusBadGateway}, want: false},
		{name: "bad_request", item: gatewayAttemptResult{statusCode: http.StatusBadRequest}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := shouldTryNextCredential(tc.item); got != tc.want {
				t.Fatalf("shouldTryNextCredential(%#v) = %v, want %v", tc.item, got, tc.want)
			}
		})
	}
}

func TestCooldownInputForFailureClassifiesStreamAndCredentialExhaustion(t *testing.T) {
	t.Parallel()

	candidate := resolverCooldownCandidate("openai")

	streamInput, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode:      499,
		errorType:       "upstream_stream_incomplete",
		responseStarted: true,
	})
	if !ok || streamInput.Scope != "model" || streamInput.Reason != store.CooldownReasonUpstreamStreamUnstable || streamInput.Duration != transientCooldownBaseDuration {
		t.Fatalf("stream failure input = %#v ok=%v, want transient model cooldown", streamInput, ok)
	}

	exhaustedInput, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode: http.StatusBadGateway,
		errorType:  "upstream_credential_unavailable",
	})
	if !ok || exhaustedInput.Scope != "model" || exhaustedInput.Reason != store.CooldownReasonUpstreamCredentialsExhausted || exhaustedInput.Duration != transientCooldownBaseDuration {
		t.Fatalf("credential exhaustion input = %#v ok=%v, want transient model cooldown", exhaustedInput, ok)
	}

	if _, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode: http.StatusServiceUnavailable,
		errorType:  "upstream_concurrency_wait_timeout",
	}); ok {
		t.Fatal("concurrency wait timeout must not trigger a cooldown")
	}
}

func TestEscalateCooldownDurationDoublesPerRecentActivationWithCap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		base  time.Duration
		count int64
		want  time.Duration
	}{
		{base: transientCooldownBaseDuration, count: 0, want: 30 * time.Second},
		{base: transientCooldownBaseDuration, count: 1, want: time.Minute},
		{base: transientCooldownBaseDuration, count: 2, want: 2 * time.Minute},
		{base: transientCooldownBaseDuration, count: 3, want: 4 * time.Minute},
		{base: transientCooldownBaseDuration, count: 4, want: transientCooldownMaxDuration},
		{base: transientCooldownBaseDuration, count: 20, want: transientCooldownMaxDuration},
		{base: 0, count: 0, want: transientCooldownBaseDuration},
		{base: 2 * time.Minute, count: 2, want: transientCooldownMaxDuration},
	} {
		if got := escalateCooldownDuration(tc.base, tc.count); got != tc.want {
			t.Fatalf("escalateCooldownDuration(%v, %d) = %v, want %v", tc.base, tc.count, got, tc.want)
		}
	}
}

func TestEscalatableModelCooldownExclusions(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	credentialID := uuid.New()
	modelInput := func(reason string) routeengine.CooldownInput {
		return routeengine.CooldownInput{Scope: "model", Reason: reason, SiteModelID: &modelID}
	}

	if !escalatableModelCooldown(modelInput("upstream_http_error"), gatewayAttemptResult{}) {
		t.Fatal("generic upstream failure should escalate")
	}
	if !escalatableModelCooldown(modelInput(store.CooldownReasonUpstreamStreamUnstable), gatewayAttemptResult{}) {
		t.Fatal("stream instability should escalate")
	}
	for _, reason := range []string{store.CooldownReasonUpstreamModelNotFound, "upstream_no_response", "codex_model_capacity", "antigravity_model_rate_limited"} {
		if escalatableModelCooldown(modelInput(reason), gatewayAttemptResult{}) {
			t.Fatalf("reason %q must not escalate", reason)
		}
	}
	if escalatableModelCooldown(modelInput("upstream_http_error"), gatewayAttemptResult{retryAfterSeconds: 30}) {
		t.Fatal("retry-after driven cooldowns must not escalate")
	}
	credentialInput := routeengine.CooldownInput{Scope: "credential", Reason: "upstream_credential_rate_limited", SiteCredentialID: &credentialID}
	if escalatableModelCooldown(credentialInput, gatewayAttemptResult{}) {
		t.Fatal("credential scope must not use the model ladder")
	}
}

func TestStreamFailureStreakRequiresThresholdAndBreaksOnSuccess(t *testing.T) {
	t.Parallel()

	attempts := []store.RequestLog{
		streamFailureRequestLog(),
		streamFailureRequestLog(),
	}
	if streamFailureStreakReached(attempts, 3) {
		t.Fatal("expected two stream failures to stay below cooldown threshold")
	}

	attempts = append(attempts, streamFailureRequestLog())
	if !streamFailureStreakReached(attempts, 3) {
		t.Fatal("expected three stream failures to reach cooldown threshold")
	}

	broken := []store.RequestLog{
		streamFailureRequestLog(),
		successRequestLog(),
		streamFailureRequestLog(),
		streamFailureRequestLog(),
	}
	if streamFailureStreakReached(broken, 3) {
		t.Fatal("expected successful attempt to break stream failure streak")
	}
}

func TestClearCooldownAfterRecoveryOnlyForCoolingCandidates(t *testing.T) {
	t.Parallel()

	updates := 0
	db := gatewayOfflineGorm(t)
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		updates++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
	handler := Handler{
		router: routeengine.NewService(gatewayStoreWithGorm(t, db)),
		logger: gatewayDiscardLogger(),
	}
	candidate := resolverCooldownCandidate("openai")

	handler.clearCooldownAfterRecovery(context.Background(), candidate)
	if updates != 0 {
		t.Fatalf("updates = %d, want no cooldown clear for healthy candidate", updates)
	}

	candidate.Cooling = true
	handler.clearCooldownAfterRecovery(context.Background(), candidate)
	if updates != 1 {
		t.Fatalf("updates = %d, want one cooldown clear for recovered cooling candidate", updates)
	}
}

func streamFailureRequestLog() store.RequestLog {
	return store.RequestLog{
		StatusCode: 499,
		ErrorType:  sql.NullString{String: "upstream_stream_incomplete", Valid: true},
		Metadata:   store.JSON([]byte(`{"stream_started":true}`)),
	}
}

func noResponseRequestLog() store.RequestLog {
	return store.RequestLog{
		StatusCode: http.StatusBadGateway,
		ErrorType:  sql.NullString{String: "upstream_timeout", Valid: true},
		Metadata:   store.JSON([]byte(`{"no_upstream_response":true}`)),
	}
}

func successRequestLog() store.RequestLog {
	return store.RequestLog{
		StatusCode: http.StatusOK,
		Success:    true,
		Metadata:   store.JSON([]byte(`{}`)),
	}
}
