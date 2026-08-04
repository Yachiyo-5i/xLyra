package gateway

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/httpx"
	"xlyra/server/internal/ratelimit"
)

type rateLimitContextKey struct{}

func withRateLimitMetadata(ctx context.Context, metadata map[string]any) context.Context {
	if metadata == nil {
		return ctx
	}
	return context.WithValue(ctx, rateLimitContextKey{}, metadata)
}

func rateLimitMetadataFromContext(ctx context.Context) (map[string]any, bool) {
	value, ok := ctx.Value(rateLimitContextKey{}).(map[string]any)
	return value, ok
}

func (h Handler) acquireRateLimit(ctx context.Context, apiKeyID uuid.UUID, endpoint gatewayEndpointAdapter, request gatewayRequest, startedAt time.Time) (*ratelimit.Reservation, *ratelimit.LimitError, error) {
	if h.rateLimits == nil {
		return nil, nil, nil
	}
	reservation, err := h.rateLimits.AcquireQueued(ctx, ratelimit.AcquireInput{
		APIKeyID:    apiKeyID,
		Endpoint:    endpoint.DownstreamPath(),
		Payload:     request.Payload,
		EstimateTPM: endpointUsesTPM(endpoint.DownstreamPath()),
		RequestedAt: startedAt,
	})
	if err == nil {
		return reservation, nil, nil
	}
	var limitErr ratelimit.LimitError
	if errors.As(err, &limitErr) {
		return nil, &limitErr, nil
	}
	return nil, nil, err
}

func (h Handler) settleRateLimit(ctx context.Context, reservation *ratelimit.Reservation, actualTokens int64) {
	if h.rateLimits == nil || reservation == nil {
		return
	}
	recordCtx, cancel := detachedRecordingContext(ctx)
	defer cancel()
	if err := h.rateLimits.Settle(recordCtx, reservation, actualTokens); err != nil {
		h.logger.WarnContext(ctx, "failed to settle gateway rate limit", "scope", "gateway", "error", err)
	}
}

func endpointUsesTPM(endpoint string) bool {
	switch endpoint {
	case gatewayEndpointChatCompletions, gatewayEndpointResponses, gatewayEndpointMessages, gatewayEndpointAudioSpeech:
		return true
	default:
		return false
	}
}

func (h Handler) writeRateLimitFailure(
	w http.ResponseWriter,
	r *http.Request,
	endpoint string,
	requestID string,
	apiKeyID uuid.UUID,
	startedAt time.Time,
	request gatewayRequest,
	limitErr ratelimit.LimitError,
) {
	retryAfter := limitErr.RetryAfterSeconds
	if retryAfter < 1 {
		retryAfter = 1
	}
	metadata := map[string]any{
		"scope":               limitErr.Scope,
		"scope_key":           limitErr.ScopeKey,
		"limit_type":          limitErr.LimitType,
		"retry_after_seconds": retryAfter,
	}
	ctx := withRateLimitMetadata(r.Context(), metadata)
	h.recordRequestFailure(
		ctx,
		requestID,
		apiKeyID,
		startedAt,
		http.StatusTooManyRequests,
		"rate_limit_exceeded",
		"rate limit exceeded",
		request.RequestedModel,
		request.Stream,
		"rate_limit",
		endpoint,
	)
	w.Header().Set("Retry-After", int64ToString(retryAfter))
	httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{
			"code":                "rate_limit_exceeded",
			"message":             "rate limit exceeded",
			"request_id":          requestID,
			"scope":               limitErr.Scope,
			"limit_type":          limitErr.LimitType,
			"retry_after_seconds": retryAfter,
		},
	})
}

func int64ToString(value int64) string {
	return strconv.FormatInt(value, 10)
}
