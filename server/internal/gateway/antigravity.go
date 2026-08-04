package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	antigravityGatewayVersion   = "4.1.32"
	antigravityGatewayUserAgent = "Antigravity/4.1.32 (Macintosh; Intel Mac OS X 10_15_7) Chrome/132.0.6834.160 Electron/39.2.3"
)

func isAntigravitySite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "antigravity")
}

func applyAntigravityGatewayHeaders(req *http.Request) {
	req.Header.Set("User-Agent", antigravityGatewayUserAgent)
	req.Header.Set("x-client-name", "antigravity")
	req.Header.Set("x-client-version", antigravityGatewayVersion)
	req.Header.Set("x-machine-id", "xlyra")
	req.Header.Set("x-vscode-sessionid", "xlyra")
}

func classifyAntigravityUpstreamError(statusCode int, body []byte, now time.Time) (codexUpstreamError, bool) {
	reason := antigravityErrorReason(body)
	if statusCode != http.StatusTooManyRequests || (reason != "QUOTA_EXHAUSTED" && reason != "RATE_LIMIT_EXCEEDED") {
		return codexUpstreamError{}, false
	}
	duration, seconds := parseAntigravityRetryAfter(body, now)
	if duration <= 0 {
		duration = 5 * time.Minute
		seconds = int64(duration.Seconds())
	}
	return codexUpstreamError{
		StatusCode:        http.StatusTooManyRequests,
		ErrorType:         "antigravity_model_rate_limited",
		ErrorMessage:      antigravityErrorMessage(body, "Antigravity model quota exhausted"),
		CooldownReason:    "antigravity_model_rate_limited",
		CooldownScope:     "model",
		CooldownDuration:  duration,
		RetryAfterSeconds: seconds,
	}, true
}

func antigravityErrorReason(body []byte) string {
	errorPayload := antigravityErrorPayload(body)
	for _, raw := range detailsFromAny(errorPayload["details"]) {
		metadata, _ := raw["metadata"].(map[string]any)
		reason := strings.TrimSpace(anyString(raw["reason"]))
		if reason == "" {
			reason = strings.TrimSpace(anyString(metadata["reason"]))
		}
		if reason != "" {
			return reason
		}
	}
	return strings.TrimSpace(anyString(errorPayload["status"]))
}

func antigravityErrorMessage(body []byte, fallback string) string {
	errorPayload := antigravityErrorPayload(body)
	for _, value := range []any{errorPayload["message"], errorPayload["status"]} {
		text := strings.TrimSpace(anyString(value))
		if text != "" {
			return text
		}
	}
	return fallback
}

func parseAntigravityRetryAfter(body []byte, now time.Time) (time.Duration, int64) {
	errorPayload := antigravityErrorPayload(body)
	for _, raw := range detailsFromAny(errorPayload["details"]) {
		metadata, _ := raw["metadata"].(map[string]any)
		if resetAt := strings.TrimSpace(anyString(metadata["quotaResetTimeStamp"])); resetAt != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, resetAt); err == nil && parsed.After(now) {
				duration := parsed.Sub(now)
				return duration, int64(duration.Seconds())
			}
		}
		for _, value := range []any{raw["retryDelay"], metadata["quotaResetDelay"]} {
			if duration, ok := parseAntigravityDuration(anyString(value)); ok {
				return duration, int64(duration.Seconds())
			}
		}
	}
	return 0, 0
}

func parseAntigravityDuration(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, false
	}
	return duration, true
}

func antigravityErrorPayload(body []byte) map[string]any {
	payload := map[string]any{}
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return map[string]any{}
	}
	errorPayload, _ := payload["error"].(map[string]any)
	return errorPayload
}

func detailsFromAny(value any) []map[string]any {
	values, _ := value.([]any)
	items := make([]map[string]any, 0, len(values))
	for _, item := range values {
		if typed, ok := item.(map[string]any); ok {
			items = append(items, typed)
		}
	}
	return items
}
