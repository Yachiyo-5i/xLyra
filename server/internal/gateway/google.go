package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const googleGatewayUserAgent = "xLyra/1.0 (Gemini-API)"

func isGoogleSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "google_gemini")
}

func applyGoogleGatewayHeaders(req *http.Request, apiKey string) {
	req.Header.Set("x-goog-api-key", strings.TrimSpace(apiKey))
	req.Header.Set("User-Agent", googleGatewayUserAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Del("Authorization")
}

func classifyGoogleUpstreamError(statusCode int, body []byte, _ time.Time) (codexUpstreamError, bool) {
	if statusCode != http.StatusTooManyRequests && statusCode < 500 {
		return codexUpstreamError{}, false
	}
	errorMsg := googleErrorMessage(body, "Google API error")
	if statusCode == http.StatusTooManyRequests {
		duration := googleRetryAfter(body)
		if duration <= 0 {
			duration = 60 * time.Second
		}
		return codexUpstreamError{
			StatusCode:        statusCode,
			ErrorType:         "google_rate_limited",
			ErrorMessage:      errorMsg,
			CooldownReason:    "google_rate_limited",
			CooldownScope:     "model",
			CooldownDuration:  duration,
			RetryAfterSeconds: int64(duration.Seconds()),
		}, true
	}
	return codexUpstreamError{
		StatusCode:   statusCode,
		ErrorType:    "google_upstream_error",
		ErrorMessage: errorMsg,
	}, true
}

func googleErrorMessage(body []byte, fallback string) string {
	if len(body) == 0 {
		return fallback
	}
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fallback
	}
	if strings.TrimSpace(payload.Error.Message) != "" {
		return payload.Error.Message
	}
	if strings.TrimSpace(payload.Error.Status) != "" {
		return payload.Error.Status
	}
	return fallback
}

func googleRetryAfter(body []byte) time.Duration {
	if len(body) == 0 {
		return 0
	}
	var payload struct {
		Error struct {
			Details []struct {
				RetryDelay string `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0
	}
	for _, detail := range payload.Error.Details {
		if strings.TrimSpace(detail.RetryDelay) != "" {
			if d, err := time.ParseDuration(detail.RetryDelay); err == nil && d > 0 {
				return d
			}
		}
	}
	return 0
}
