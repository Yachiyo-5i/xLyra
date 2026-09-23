package gateway

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/ratelimit"
)

func (h Handler) GeminiModels(w http.ResponseWriter, r *http.Request) {
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
			Endpoint:    gatewayEndpointGeminiModels,
			EstimateTPM: false,
			RequestedAt: startedAt,
		})
		if err != nil {
			var limitErr ratelimit.LimitError
			if errors.As(err, &limitErr) {
				h.writeRateLimitFailure(w, r, gatewayEndpointGeminiModels, requestID, apiKey.ID, startedAt, gatewayRequest{
					DownstreamPath: gatewayEndpointGeminiModels,
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

	payload, err := h.modelsPayloadForAPIKey(r.Context(), apiKey)
	if err != nil {
		h.writeGatewayError(w, r, http.StatusInternalServerError, "gateway_models_failed", "failed to list gateway models")
		return
	}
	geminiPayload := geminiModelsPayload(payload)

	w.Header().Set("Cache-Control", "no-store")
	if etag := modelsPayloadETag(geminiPayload); etag != "" {
		w.Header().Set("ETag", etag)
		if modelsETagMatches(r.Header.Values("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	httpx.JSON(w, http.StatusOK, geminiPayload)
}

func geminiModelsPayload(payload map[string]any) map[string]any {
	models := make([]map[string]any, 0)
	rows, ok := payload["data"].([]map[string]any)
	if !ok {
		return map[string]any{"models": models}
	}

	for _, row := range rows {
		metadata, _ := row["metadata"].(map[string]any)
		if !hasGeminiEndpointType(metadata["supported_endpoint_types"]) {
			continue
		}
		id := strings.TrimSpace(anyString(row["id"]))
		if id == "" {
			continue
		}

		displayName := strings.TrimSpace(anyString(metadata["display_name"]))
		if displayName == "" {
			displayName = id
		}
		model := map[string]any{
			"name":                       geminiModelResourceName(id),
			"displayName":                displayName,
			"supportedGenerationMethods": []string{"generateContent"},
		}
		for key, field := range map[string]string{
			"version":     "version",
			"description": "description",
		} {
			if value := strings.TrimSpace(anyString(metadata[key])); value != "" {
				model[field] = value
			}
		}
		for key, field := range map[string]string{
			"input_token_limit":  "inputTokenLimit",
			"output_token_limit": "outputTokenLimit",
		} {
			if value := anyPositiveInt(metadata[key]); value > 0 {
				model[field] = value
			}
		}
		models = append(models, model)
	}

	return map[string]any{"models": models}
}

func hasGeminiEndpointType(value any) bool {
	switch items := value.(type) {
	case []string:
		for _, item := range items {
			if strings.EqualFold(strings.TrimSpace(item), upstreamEndpointTypeGoogleGemini) {
				return true
			}
		}
	case []any:
		for _, item := range items {
			if text, ok := item.(string); ok && strings.EqualFold(strings.TrimSpace(text), upstreamEndpointTypeGoogleGemini) {
				return true
			}
		}
	}
	return false
}

func geminiModelResourceName(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "models/") {
		return id
	}
	return "models/" + id
}

func anyPositiveInt(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case float32:
		return int(value)
	}
	return 0
}
