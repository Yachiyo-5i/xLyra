package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

const (
	upstreamNoResponseCooldownDuration     = 2 * time.Minute
	upstreamNoResponseCooldownThreshold    = 3
	upstreamNoResponseCooldownWindow       = 10 * time.Minute
	upstreamNoResponseCooldownAttemptLimit = 20

	transientCooldownBaseDuration     = 30 * time.Second
	transientCooldownMaxDuration      = 5 * time.Minute
	transientCooldownEscalationWindow = 30 * time.Minute

	streamFailureCooldownThreshold = 3

	credentialUnauthorizedCooldownDuration       = 5 * time.Minute
	credentialUnauthorizedRepeatCooldownDuration = 30 * time.Minute
)

func shouldTryNextCredential(result gatewayAttemptResult) bool {
	if result.success {
		return false
	}
	switch result.errorType {
	case "upstream_credential_decrypt_failed", "credential_concurrency_limited":
		return true
	}
	switch result.statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func (h Handler) cooldownAfterFailure(ctx context.Context, candidate routeengine.Candidate, result gatewayAttemptResult) {
	if result.success {
		return
	}
	input, ok := cooldownInputForFailure(candidate, result)
	if !ok {
		return
	}
	if upstreamNoResponseFailure(result) && !h.upstreamNoResponseCooldownThresholdReached(ctx, candidate) {
		return
	}
	if upstreamStreamFailure(result) && !h.streamFailureCooldownThresholdReached(ctx, candidate) {
		return
	}
	input.Duration = h.escalatedCooldownDuration(ctx, candidate, input, result)
	if _, err := h.router.ActivateCooldown(ctx, input); err != nil {
		h.logger.WarnContext(ctx, "failed to activate gateway cooldown", "scope", "gateway", "endpoint", result.downstreamPath, "error", err, "site_id", candidate.Site.ID, "site_model_id", candidate.Model.SiteModelID, "error_code", result.errorType)
		return
	}
	h.persistOpenCodeGoUsageLimit(ctx, candidate, result)
	h.InvalidateModelsCache()
}

func (h Handler) clearCooldownAfterRecovery(ctx context.Context, candidate routeengine.Candidate) {
	if !candidate.Cooling {
		return
	}
	siteModelID := candidate.Model.SiteModelID
	if err := h.router.ClearCooldown(ctx, candidate.Site.ID, &siteModelID, nil, "gateway"); err != nil {
		h.logger.WarnContext(ctx, "failed to clear gateway cooldown after recovery", "scope", "gateway", "site_id", candidate.Site.ID, "site_model_id", siteModelID, "error", err)
		return
	}
	h.InvalidateModelsCache()
}

func cooldownInputForFailure(candidate routeengine.Candidate, result gatewayAttemptResult) (routeengine.CooldownInput, bool) {
	siteModelID := candidate.Model.SiteModelID
	credentialID := result.credentialID
	metadata := map[string]any{
		"status_code":        result.statusCode,
		"error_type":         result.errorType,
		"error_message":      result.errorMessage,
		"upstream_model":     candidate.Model.UpstreamName,
		"credential_masked":  emptyToNil(result.credentialMasked),
		"credential_attempt": result.credentialAttempt,
		"credential_total":   result.credentialTotal,
	}
	if result.upstreamStatusCode > 0 {
		metadata["upstream_status_code"] = result.upstreamStatusCode
	}
	if result.retryAfterSeconds > 0 {
		metadata["retry_after_seconds"] = result.retryAfterSeconds
	}
	for key, value := range result.cooldownMetadata {
		metadata[key] = value
	}
	if upstreamNoResponseFailure(result) {
		metadata["no_upstream_response"] = true
	}
	base := routeengine.CooldownInput{
		SiteID:   candidate.Site.ID,
		Source:   "gateway",
		Metadata: metadata,
	}
	if result.cooldownReason != "" {
		base.Reason = result.cooldownReason
	}
	if result.cooldownDuration > 0 {
		base.Duration = result.cooldownDuration
	}

	switch {
	case result.errorType == "opencode_go_usage_limit" && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = store.CooldownReasonOpenCodeGoUsageLimitReached
		base.Duration = openCodeGoUsageLimitCooldownDuration(result)
		return base, true
	case result.errorType == "upstream_subscription_limit_exceeded" && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = store.CooldownReasonUpstreamSubscriptionLimitExceeded
		base.Duration = result.cooldownDuration
		return base, true
	case result.errorType == "upstream_credential_limited" && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = store.CooldownReasonUpstreamCredentialLimited
		base.Duration = rateLimitedCooldownDuration(result.retryAfterSeconds)
		return base, true
	case upstreamNoResponseFailure(result):
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		base.Reason = "upstream_no_response"
		base.Duration = upstreamNoResponseCooldownDuration
		return base, true
	case result.errorType == "upstream_response_transform_failed":
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		base.Reason = "upstream_response_transform_failed"
		if base.Duration <= 0 {
			base.Duration = 30 * time.Second
		}
		return base, true
	case upstreamStreamFailure(result):
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		base.Reason = store.CooldownReasonUpstreamStreamUnstable
		base.Duration = transientCooldownBaseDuration
		return base, true
	case result.errorType == "upstream_credential_unavailable":
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		base.Reason = store.CooldownReasonUpstreamCredentialsExhausted
		base.Duration = transientCooldownBaseDuration
		return base, true
	case (isCodexSite(candidate.Site.SiteType) || isAntigravitySite(candidate.Site.SiteType)) && result.cooldownScope == "model":
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		if base.Reason == "" {
			base.Reason = stringValue(&result.errorType, "upstream_model_unavailable")
		}
		if base.Duration <= 0 {
			base.Duration = 2 * time.Minute
		}
		return base, true
	case (isCodexSite(candidate.Site.SiteType) || isAntigravitySite(candidate.Site.SiteType)) && result.cooldownScope == "credential" && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		if base.Reason == "" {
			base.Reason = stringValue(&result.errorType, "upstream_credential_unavailable")
		}
		if base.Duration <= 0 {
			base.Duration = 5 * time.Minute
		}
		return base, true
	case result.statusCode == http.StatusUnauthorized && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = store.CooldownReasonUpstreamCredentialUnauthorized
		base.Duration = credentialUnauthorizedCooldownDuration
		return base, true
	case result.statusCode == http.StatusForbidden && credentialID != uuid.Nil:
		base.SiteModelID = &siteModelID
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = "upstream_credential_forbidden"
		base.Duration = 30 * time.Minute
		return base, true
	case result.statusCode == http.StatusTooManyRequests && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = "upstream_credential_rate_limited"
		base.Duration = rateLimitedCooldownDuration(result.retryAfterSeconds)
		return base, true
	case result.statusCode == http.StatusNotFound:
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		base.Reason = store.CooldownReasonUpstreamModelNotFound
		base.Duration = 30 * time.Minute
		return base, true
	case (result.statusCode >= 500 &&
		result.errorType != "site_concurrency_limited" &&
		result.errorType != "model_concurrency_limited" &&
		result.errorType != "credential_concurrency_limited" &&
		result.errorType != "upstream_concurrency_wait_timeout") ||
		result.errorType == "upstream_timeout" ||
		result.errorType == "upstream_transport_error":
		base.SiteModelID = &siteModelID
		base.Scope = "model"
		base.Reason = stringValue(&result.errorType, "upstream_failed")
		base.Duration = transientCooldownBaseDuration
		return base, true
	case result.errorType == "upstream_credential_decrypt_failed" && credentialID != uuid.Nil:
		base.SiteCredentialID = &credentialID
		base.Scope = "credential"
		base.Reason = "upstream_credential_decrypt_failed"
		base.Duration = 30 * time.Minute
		return base, true
	default:
		return routeengine.CooldownInput{}, false
	}
}

func openCodeGoUsageLimitCooldownDuration(result gatewayAttemptResult) time.Duration {
	if result.cooldownDuration > 0 {
		return result.cooldownDuration
	}
	if result.retryAfterSeconds > 0 {
		return time.Duration(result.retryAfterSeconds) * time.Second
	}
	return transientCooldownBaseDuration
}

func rateLimitedCooldownDuration(retryAfterSeconds int64) time.Duration {
	if retryAfterSeconds <= 0 {
		return transientCooldownBaseDuration
	}
	duration := time.Duration(retryAfterSeconds) * time.Second
	if duration < 5*time.Second {
		return 5 * time.Second
	}
	if duration > 2*time.Minute {
		return 2 * time.Minute
	}
	return duration
}

func (h Handler) upstreamNoResponseCooldownThresholdReached(ctx context.Context, candidate routeengine.Candidate) bool {
	return h.failureCooldownThresholdReached(ctx, candidate, "no-response", upstreamNoResponseCooldownThreshold, requestLogNoUpstreamResponseFailure)
}

func (h Handler) streamFailureCooldownThresholdReached(ctx context.Context, candidate routeengine.Candidate) bool {
	return h.failureCooldownThresholdReached(ctx, candidate, "stream", streamFailureCooldownThreshold, requestLogStreamFailure)
}

func (h Handler) failureCooldownThresholdReached(ctx context.Context, candidate routeengine.Candidate, kind string, threshold int, match func(store.RequestLog) bool) bool {
	if h.db == nil {
		return true
	}
	attempts, err := store.NewRequestLogRepository(h.db.DB()).ListRecentSiteModelAttempts(
		ctx,
		candidate.Site.ID,
		candidate.Model.SiteModelID,
		time.Now().Add(-upstreamNoResponseCooldownWindow),
		upstreamNoResponseCooldownAttemptLimit,
	)
	if err != nil {
		h.logger.WarnContext(ctx, "failed to inspect upstream failure streak", "scope", "gateway", "streak", kind, "site_id", candidate.Site.ID, "site_model_id", candidate.Model.SiteModelID, "error", err)
		return true
	}
	return failureStreakReached(attempts, threshold, match)
}

func upstreamNoResponseFailureStreakReached(attempts []store.RequestLog, threshold int) bool {
	return failureStreakReached(attempts, threshold, requestLogNoUpstreamResponseFailure)
}

func streamFailureStreakReached(attempts []store.RequestLog, threshold int) bool {
	return failureStreakReached(attempts, threshold, requestLogStreamFailure)
}

func failureStreakReached(attempts []store.RequestLog, threshold int, match func(store.RequestLog) bool) bool {
	if threshold <= 0 {
		return true
	}
	count := 0
	for _, item := range attempts {
		if !requestLogGatewayAttempt(item) {
			continue
		}
		if !match(item) {
			return false
		}
		count++
		if count >= threshold {
			return true
		}
	}
	return false
}

func (h Handler) escalatedCooldownDuration(ctx context.Context, candidate routeengine.Candidate, input routeengine.CooldownInput, result gatewayAttemptResult) time.Duration {
	if h.db == nil {
		return input.Duration
	}
	repo := store.NewRouteCooldownRepository(h.db.DB())
	since := time.Now().Add(-transientCooldownEscalationWindow)
	if input.Reason == store.CooldownReasonUpstreamCredentialUnauthorized && input.SiteCredentialID != nil {
		count, err := repo.CountRecentActivations(ctx, store.CountRouteCooldownActivationsParams{
			SiteID:           candidate.Site.ID,
			SiteCredentialID: uuid.NullUUID{UUID: *input.SiteCredentialID, Valid: true},
			Source:           "gateway",
			Reasons:          []string{store.CooldownReasonUpstreamCredentialUnauthorized},
			Since:            since,
		})
		if err == nil && count > 0 {
			return credentialUnauthorizedRepeatCooldownDuration
		}
		return input.Duration
	}
	if !escalatableModelCooldown(input, result) {
		return input.Duration
	}
	count, err := repo.CountRecentActivations(ctx, store.CountRouteCooldownActivationsParams{
		SiteID:      candidate.Site.ID,
		SiteModelID: uuid.NullUUID{UUID: candidate.Model.SiteModelID, Valid: true},
		Source:      "gateway",
		Since:       since,
	})
	if err != nil {
		return input.Duration
	}
	return escalateCooldownDuration(input.Duration, count)
}

func escalatableModelCooldown(input routeengine.CooldownInput, result gatewayAttemptResult) bool {
	if input.Scope != "model" || input.SiteCredentialID != nil {
		return false
	}
	if result.retryAfterSeconds > 0 {
		return false
	}
	switch input.Reason {
	case store.CooldownReasonUpstreamModelNotFound, "upstream_no_response", "codex_model_capacity", "antigravity_model_rate_limited":
		return false
	}
	return true
}

func escalateCooldownDuration(base time.Duration, recentActivations int64) time.Duration {
	if base <= 0 {
		base = transientCooldownBaseDuration
	}
	duration := base
	for i := int64(0); i < recentActivations && duration < transientCooldownMaxDuration; i++ {
		duration *= 2
	}
	if duration > transientCooldownMaxDuration {
		return transientCooldownMaxDuration
	}
	return duration
}

func upstreamStreamFailure(result gatewayAttemptResult) bool {
	if result.success {
		return false
	}
	switch result.errorType {
	case "upstream_stream_failed", "upstream_stream_incomplete", "upstream_stream_eof", "upstream_stream_error", "upstream_stream_read_failed", "upstream_stream_empty":
		return true
	default:
		return false
	}
}

func requestLogStreamFailure(item store.RequestLog) bool {
	if item.Success || !item.ErrorType.Valid {
		return false
	}
	switch item.ErrorType.String {
	case "upstream_stream_failed", "upstream_stream_incomplete", "upstream_stream_eof", "upstream_stream_error", "upstream_stream_read_failed", "upstream_stream_empty":
		return true
	default:
		return false
	}
}

func requestLogNoUpstreamResponseFailure(item store.RequestLog) bool {
	if item.Success || item.StatusCode < 500 {
		return false
	}
	if !item.ErrorType.Valid {
		return false
	}
	switch item.ErrorType.String {
	case "upstream_timeout", "upstream_transport_error":
	default:
		return false
	}

	metadata := map[string]any{}
	if len(item.Metadata) > 0 {
		_ = json.Unmarshal(item.Metadata, &metadata)
	}
	if value, ok := metadata["no_upstream_response"].(bool); ok {
		return value
	}
	if numericMetadataValue(metadata["upstream_status_code"]) > 0 {
		return false
	}
	if boolMetadataValue(metadata["stream_started"]) {
		return false
	}
	return true
}

func requestLogGatewayAttempt(item store.RequestLog) bool {
	metadata := map[string]any{}
	if len(item.Metadata) > 0 {
		_ = json.Unmarshal(item.Metadata, &metadata)
	}
	scope, ok := metadata["scope"].(string)
	return !ok || scope == "" || scope == "gateway"
}

func numericMetadataValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

func boolMetadataValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func upstreamNoResponseFailure(result gatewayAttemptResult) bool {
	if result.success || result.upstreamStatusCode > 0 || result.responseStarted {
		return false
	}
	switch result.errorType {
	case "upstream_timeout", "upstream_transport_error":
		return result.statusCode >= 500
	default:
		return false
	}
}
