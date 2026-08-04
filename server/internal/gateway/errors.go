package gateway

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"xlyra/server/internal/httpx"
)

// decodeRequestFailure maps a request-body decode error to a chatFailure. An
// oversized body becomes 413, an invalid content encoding becomes 400, and
// anything else is treated as malformed JSON (400).
func decodeRequestFailure(err error) *chatFailure {
	if errors.Is(err, httpx.ErrRequestBodyTooLarge) {
		return &chatFailure{
			status:  http.StatusRequestEntityTooLarge,
			code:    "request_body_too_large",
			message: "request body exceeds the maximum allowed size",
			stage:   "decode",
		}
	}
	if errors.Is(err, httpx.ErrRequestBodyEncoding) {
		return &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_content_encoding",
			message: "request body content encoding is invalid",
			stage:   "decode",
		}
	}
	return &chatFailure{
		status:  http.StatusBadRequest,
		code:    "invalid_json",
		message: "request body must be valid JSON",
		stage:   "decode",
	}
}

type chatFailure struct {
	status         int
	code           string
	message        string
	requestedModel string
	stream         bool
	stage          string
	retryAfter     int64
	skipRecord     bool
}

func (h Handler) writeGatewayError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	if writer, ok := w.(downstreamSSEFailureWriter); ok && writer.WriteSSEFailure(downstreamSSEFailure{
		Code:      code,
		Message:   message,
		RequestID: middleware.GetReqID(r.Context()),
	}) {
		return
	}
	httpx.Error(w, r, status, code, message)
}

func (h Handler) writeChatFailure(
	w http.ResponseWriter,
	r *http.Request,
	endpoint string,
	requestID string,
	apiKeyID uuid.UUID,
	startedAt time.Time,
	failure chatFailure,
) {
	if !failure.skipRecord {
		ctx := r.Context()
		if failure.retryAfter > 0 {
			ctx = withRetryAfter(ctx, failure.retryAfter)
		}
		h.recordRequestFailure(
			ctx,
			requestID,
			apiKeyID,
			startedAt,
			failure.status,
			failure.code,
			failure.message,
			failure.requestedModel,
			failure.stream,
			failure.stage,
			endpoint,
		)
	}
	if failure.retryAfter > 0 {
		w.Header().Set("Retry-After", int64ToString(failure.retryAfter))
		if writer, ok := w.(downstreamSSEFailureWriter); ok && writer.WriteSSEFailure(downstreamSSEFailure{
			Code:              failure.code,
			Message:           failure.message,
			RequestID:         requestID,
			RetryAfterSeconds: failure.retryAfter,
		}) {
			return
		}
		httpx.JSON(w, failure.status, map[string]any{
			"error": map[string]any{
				"code":                failure.code,
				"message":             failure.message,
				"request_id":          requestID,
				"retry_after_seconds": failure.retryAfter,
			},
		})
		return
	}
	h.writeGatewayError(w, r, failure.status, failure.code, failure.message)
}

func (h Handler) writeNoRouteFailure(
	w http.ResponseWriter,
	r *http.Request,
	endpoint string,
	requestID string,
	apiKeyID uuid.UUID,
	startedAt time.Time,
	request gatewayRequest,
	access routeAccessForNoRoute,
) {
	key := noRouteSuppressionKey{
		APIKeyID:            apiKeyID,
		Endpoint:            endpoint,
		ModelKey:            access.ModelKey,
		AllowedSiteIDs:      access.AllowedSiteIDs,
		AllowedSiteModelIDs: access.AllowedSiteModelIDs,
		Code:                "no_route_candidates",
	}
	retryAfter, skipRecord := h.cachedNoRouteRetryAfter(key)
	if !skipRecord {
		retryAfter = h.noRouteRetryAfterSeconds(r.Context(), access)
		h.rememberNoRoute(key, retryAfter)
	}
	h.writeChatFailure(w, r, endpoint, requestID, apiKeyID, startedAt, chatFailure{
		status:         http.StatusServiceUnavailable,
		code:           "no_route_candidates",
		message:        "no available upstream route candidates",
		requestedModel: access.ModelKey,
		stream:         request.Stream,
		stage:          "route_plan",
		retryAfter:     retryAfter,
		skipRecord:     skipRecord,
	})
}
