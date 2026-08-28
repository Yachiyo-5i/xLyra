package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

const gatewayRecordingTimeout = 5 * time.Second

func (h Handler) recordAttempt(
	ctx context.Context,
	requestID string,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	result gatewayAttemptResult,
	upstreamResponse any,
) uuid.UUID {
	recordCtx, cancel := detachedRecordingContext(ctx)
	defer cancel()

	attemptRequestID := fmt.Sprintf("%s:%d:%s", requestID, result.attempt, uuid.NewString())
	metadata := attemptMetadata(ctx, attemptRequestID, requestID, apiKeyID, canonicalModelID, candidate, result)
	mergeInternalRequestMetadata(metadata, ctx)
	h.appendCacheObservationMetadata(ctx, metadata, candidate, result)
	internal, parentLogID := bridgeRecordingFromContext(ctx)
	requestLog, _, err := h.recorder.RecordGatewayRequest(recordCtx, GatewayRequestRecord{
		RequestID:                  attemptRequestID,
		ParentRequestID:            requestID,
		APIKeyID:                   apiKeyID,
		SiteID:                     candidate.Site.ID,
		CanonicalModelID:           canonicalModelID,
		SiteModelID:                candidate.Model.SiteModelID,
		Endpoint:                   result.downstreamPath,
		StatusCode:                 result.statusCode,
		Success:                    result.success,
		ErrorType:                  result.errorType,
		ErrorMessage:               result.errorMessage,
		LatencyMS:                  result.latencyMS,
		UpstreamLatencyMS:          result.upstreamLatencyMS,
		FirstByteLatencyMS:         result.firstByteLatencyMS,
		PromptTokens:               result.promptTokens,
		CompletionTokens:           result.completionTokens + result.audioOutputTokens,
		CachedTokens:               result.cachedPromptTokens,
		CacheWriteTokens:           result.cacheWriteTokens,
		CacheCreationInputTokens:   result.cacheCreationInputTokens,
		CacheCreation5mInputTokens: result.cacheCreation5mInputTokens,
		CacheCreation1hInputTokens: result.cacheCreation1hInputTokens,
		CacheWriteCost:             result.cacheWriteCost,
		EstimatedCost:              result.estimatedCost,
		Currency:                   result.currency,
		UpstreamStatusCode:         result.upstreamStatusCode,
		UpstreamResponse:           upstreamResponse,
		Metadata:                   metadata,
		SkipHealthSnapshot:         result.diagnostic,
		Internal:                   internal,
		ParentRequestLogID:         parentLogID,
	})
	if err != nil {
		h.logger.WarnContext(ctx, "failed to record gateway attempt", "scope", "gateway", "endpoint", result.downstreamPath, "error", err, "request_id", attemptRequestID, "status_code", result.statusCode, "error_code", result.errorType)
		return uuid.Nil
	}
	return requestLog.ID
}

func (h Handler) recordRequestFailure(
	ctx context.Context,
	requestID string,
	apiKeyID uuid.UUID,
	startedAt time.Time,
	statusCode int,
	errorType string,
	errorMessage string,
	requestedModel string,
	stream bool,
	stage string,
	endpoint string,
) {
	if h.db == nil {
		return
	}
	recorder := h.recorder
	if recorder.db == nil {
		recorder = NewRecorder(h.db, h.logger, h.recorder.timeZone)
	}
	recordCtx, cancel := detachedRecordingContext(ctx)
	defer cancel()

	metadata := requestFailureMetadata(ctx, requestID, apiKeyID, statusCode, errorType, errorMessage, requestedModel, stream, stage, endpoint)
	mergeInternalRequestMetadata(metadata, ctx)
	failureRequestID := fmt.Sprintf("%s:gateway:%s", requestID, uuid.NewString())
	if _, _, err := recorder.RecordGatewayRequest(recordCtx, GatewayRequestRecord{
		RequestID:       failureRequestID,
		ParentRequestID: requestID,
		APIKeyID:        apiKeyID,
		Endpoint:        stringValue(&endpoint, gatewayEndpointChatCompletions),
		StatusCode:      statusCode,
		Success:         false,
		ErrorType:       errorType,
		ErrorMessage:    errorMessage,
		LatencyMS:       time.Since(startedAt).Milliseconds(),
		Currency:        "USD",
		Metadata:        metadata,
	}); err != nil {
		h.logger.WarnContext(ctx, "failed to record gateway request failure", "scope", "gateway", "endpoint", endpoint, "error", err, "request_id", failureRequestID, "status_code", statusCode, "error_code", errorType)
	}
}

// recordingContextKey is a private context key type so the recording request ID
// cannot collide with unrelated context values keyed by a bare string.
type recordingContextKey struct{}

type bridgeRecordingContextKey struct{}

type internalRequestMetadataContextKey struct{}

func WithInternalRequestMetadata(ctx context.Context, metadata map[string]any) context.Context {
	if len(metadata) == 0 {
		return ctx
	}
	return context.WithValue(ctx, internalRequestMetadataContextKey{}, metadata)
}

func mergeInternalRequestMetadata(metadata map[string]any, ctx context.Context) {
	values, ok := ctx.Value(internalRequestMetadataContextKey{}).(map[string]any)
	if !ok {
		return
	}
	for key, value := range values {
		metadata[key] = value
	}
}

func withBridgeRecording(ctx context.Context, parentRequestLogID uuid.UUID) context.Context {
	return context.WithValue(ctx, bridgeRecordingContextKey{}, parentRequestLogID)
}

func bridgeRecordingFromContext(ctx context.Context) (bool, uuid.UUID) {
	if ctx == nil {
		return false, uuid.Nil
	}
	if parent, ok := ctx.Value(bridgeRecordingContextKey{}).(uuid.UUID); ok {
		return true, parent
	}
	return false, uuid.Nil
}

func detachedRecordingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx != nil {
		if requestID, ok := ctx.Value(recordingContextKey{}).(string); ok && requestID != "" {
			recordCtx, cancel := context.WithTimeout(context.WithValue(context.Background(), recordingContextKey{}, requestID), gatewayRecordingTimeout)
			return recordCtx, cancel
		}
	}
	return context.WithTimeout(context.Background(), gatewayRecordingTimeout)
}

func responseModeLabel(stream bool) string {
	if stream {
		return "stream"
	}
	return "non_stream"
}
