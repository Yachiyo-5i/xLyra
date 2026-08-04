package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/ratelimit"
	routeengine "xlyra/server/internal/router"
)

func (h Handler) Models(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeGatewayError(w, r, http.StatusServiceUnavailable, "gateway_unavailable", "gateway service is not available")
		return
	}

	apiKey, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
		return
	}

	requestID := middleware.GetReqID(r.Context())
	if requestID == "" {
		requestID = uuid.NewString()
	}
	startedAt := time.Now()
	if h.rateLimits != nil {
		reservation, err := h.rateLimits.Acquire(r.Context(), ratelimit.AcquireInput{
			APIKeyID:    apiKey.ID,
			Endpoint:    gatewayEndpointModels,
			EstimateTPM: false,
			RequestedAt: startedAt,
		})
		if err != nil {
			var limitErr ratelimit.LimitError
			if errors.As(err, &limitErr) {
				h.writeRateLimitFailure(w, r, gatewayEndpointModels, requestID, apiKey.ID, startedAt, gatewayRequest{
					DownstreamPath: gatewayEndpointModels,
					Stream:         false,
				}, limitErr)
				return
			}
			h.writeGatewayError(w, r, http.StatusInternalServerError, "rate_limit_unavailable", "failed to check rate limit")
			return
		}
		if reservation != nil {
			defer h.settleRateLimit(r.Context(), reservation, 0)
		}
	}

	if h.auth == nil || h.router == nil {
		h.writeGatewayError(w, r, http.StatusServiceUnavailable, "gateway_unavailable", "gateway service is not available")
		return
	}

	payload, err := h.modelsCache.getOrBuild(r.Context(), apiKey, h.buildModelsPayloadFast)
	if err != nil {
		h.writeGatewayError(w, r, http.StatusInternalServerError, "gateway_models_failed", "failed to list gateway models")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	if etag := modelsPayloadETag(payload); etag != "" {
		w.Header().Set("ETag", etag)
		if modelsETagMatches(r.Header.Values("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	httpx.JSON(w, http.StatusOK, payload)
}

func modelsPayloadETag(payload map[string]any) string {
	body, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(body)
	return `"models-sha256-` + hex.EncodeToString(sum[:]) + `"`
}

func modelsETagMatches(values []string, etag string) bool {
	if etag == "" {
		return false
	}
	for _, value := range values {
		for _, candidate := range strings.Split(value, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
				return true
			}
		}
	}
	return false
}

func routeCandidateQuery(modelKey string, access auth.APIKeyAccessSets) routeengine.CandidateQuery {
	return routeengine.CandidateQuery{
		ModelKey:            modelKey,
		AllowedSiteIDs:      access.AllowedSiteIDs,
		AllowedSiteModelIDs: access.AllowedSiteModelIDs,
	}
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
