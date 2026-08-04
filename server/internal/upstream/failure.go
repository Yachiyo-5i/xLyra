package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type FailureClass string

const (
	FailureUnknown           FailureClass = "unknown"
	FailureLimited           FailureClass = "limited"
	FailureTransient         FailureClass = "transient"
	FailureCredentialInvalid FailureClass = "credential_invalid"
)

type Failure struct {
	Class             FailureClass
	StatusCode        int
	Code              string
	Type              string
	Message           string
	Scope             string
	RetryAfterSeconds int64
	ResetAt           time.Time
}

func (f Failure) Limited() bool {
	return f.Class == FailureLimited
}

func (f Failure) Transient() bool {
	return f.Class == FailureTransient
}

func (f Failure) CredentialInvalid() bool {
	return f.Class == FailureCredentialInvalid
}

type HTTPError struct {
	Prefix  string
	Body    []byte
	Failure Failure
}

func NewHTTPError(prefix string, statusCode int, headers http.Header, body []byte) *HTTPError {
	copied := append([]byte(nil), body...)
	return &HTTPError{
		Prefix:  strings.TrimSpace(prefix),
		Body:    copied,
		Failure: ClassifyResponse(statusCode, headers, copied),
	}
}

func (e *HTTPError) Error() string {
	prefix := strings.TrimSpace(e.Prefix)
	if prefix == "" {
		prefix = "upstream returned"
	}
	return fmt.Sprintf("%s %d: %s", prefix, e.Failure.StatusCode, strings.TrimSpace(string(e.Body)))
}

func ClassifyResponse(statusCode int, headers http.Header, body []byte) Failure {
	return classifyResponseAt(statusCode, headers, body, time.Now())
}

func ClassifyResponseAt(statusCode int, headers http.Header, body []byte, now time.Time) Failure {
	if now.IsZero() {
		now = time.Now()
	}
	return classifyResponseAt(statusCode, headers, body, now)
}

func ClassifyError(err error) Failure {
	if err == nil {
		return Failure{Class: FailureUnknown}
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Failure
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Failure{Class: FailureTransient, Message: err.Error()}
	}
	return ClassifyMessage(err.Error())
}

func ClassifyMessage(message string) Failure {
	statusCode := statusCodeFromMessage(message)
	body := jsonBodyFromMessage(message)
	if len(body) == 0 {
		body = []byte(message)
	}
	failure := classifyResponseAt(statusCode, nil, body, time.Now())
	if failure.Message == "" {
		failure.Message = strings.TrimSpace(message)
	}
	return failure
}

var statusCodePattern = regexp.MustCompile(`(?i)(?:returned|status(?:_code)?(?:\s+was)?|http|responded\s+with)\s*[=:]?\s*([1-5][0-9]{2})\b`)

func classifyResponseAt(statusCode int, headers http.Header, body []byte, now time.Time) Failure {
	failure := Failure{StatusCode: statusCode}
	root := decodePayload(body)
	errorPayload := nestedMap(root, "error")
	if errorPayload == nil {
		errorPayload = root
	}
	failure.Code = firstString(errorPayload["code"], root["code"])
	failure.Type = firstString(errorPayload["type"], root["type"])
	failure.Message = firstString(errorPayload["message"], root["message"], root["detail"])
	failure.Scope = firstString(errorPayload["scope"], root["scope"])
	failure.RetryAfterSeconds = retryAfterSeconds(headers, errorPayload, root, now)
	failure.ResetAt = resetAt(errorPayload, root)
	if failure.RetryAfterSeconds <= 0 && !failure.ResetAt.IsZero() && failure.ResetAt.After(now) {
		failure.RetryAfterSeconds = int64(failure.ResetAt.Sub(now).Seconds())
	}
	failure.Class = classifyFailure(failure, string(body))
	return failure
}

func classifyFailure(failure Failure, rawBody string) FailureClass {
	code := normalizeToken(failure.Code)
	typeName := normalizeToken(failure.Type)
	message := strings.ToLower(strings.TrimSpace(failure.Message))
	raw := strings.ToLower(strings.TrimSpace(rawBody))

	if limitedCode(code) || limitedCode(typeName) || limitedMessage(message) || limitedMessage(raw) {
		return FailureLimited
	}
	if credentialInvalidCode(code) || credentialInvalidCode(typeName) || credentialInvalidMessage(message) || credentialInvalidMessage(raw) {
		return FailureCredentialInvalid
	}
	if failure.StatusCode == http.StatusTooManyRequests {
		return FailureLimited
	}
	if failure.StatusCode == http.StatusRequestTimeout || failure.StatusCode == http.StatusTooEarly || failure.StatusCode >= 500 || transientMessage(message) || transientMessage(raw) {
		return FailureTransient
	}
	return FailureUnknown
}

func limitedCode(value string) bool {
	switch value {
	case "api_key_total_quota_exhausted",
		"api_key_daily_quota_exhausted",
		"api_key_weekly_quota_exhausted",
		"api_key_quota_exhausted",
		"usage_limit_reached",
		"usage_limit_exceeded",
		"rate_limit_exceeded",
		"insufficient_quota",
		"insufficient_balance",
		"user_platform_daily_quota_exhausted",
		"user_platform_weekly_quota_exhausted",
		"user_platform_monthly_quota_exhausted":
		return true
	default:
		return false
	}
}

func limitedMessage(value string) bool {
	if value == "" {
		return false
	}
	for _, token := range []string{
		"api_key_total_quota_exhausted",
		"api_key_daily_quota_exhausted",
		"api_key_weekly_quota_exhausted",
		"api_key_quota_exhausted",
		"usage_limit_reached",
		"usage_limit_exceeded",
		"rate_limit_exceeded",
		"insufficient_quota",
		"insufficient_balance",
		"user_platform_daily_quota_exhausted",
		"user_platform_weekly_quota_exhausted",
		"user_platform_monthly_quota_exhausted",
		"api key 额度已用完",
		"api key total quota has been exhausted",
		"api key daily quota has been exhausted",
		"api key weekly quota has been exhausted",
		"insufficient account balance",
		"insufficient balance",
		"usage quota exhausted for this platform",
		"daily usage quota exhausted",
		"weekly usage quota exhausted",
		"monthly usage quota exhausted",
	} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func credentialInvalidCode(value string) bool {
	switch value {
	case "invalid_api_key",
		"api_key_required",
		"api_key_disabled",
		"api_key_expired",
		"user_not_found",
		"user_inactive",
		"invalid_grant",
		"refresh_token_reused",
		"token_invalidated",
		"unauthorized":
		return true
	default:
		return false
	}
}

func credentialInvalidMessage(value string) bool {
	if value == "" {
		return false
	}
	for _, token := range []string{
		"invalid_api_key",
		"api_key_required",
		"api_key_disabled",
		"api_key_expired",
		"user_not_found",
		"user_inactive",
		"valid api key is required",
		"invalid api key",
		"api key is disabled",
		"api key is required",
		"api key 已过期",
		"refresh_token_reused",
		"invalid_grant",
		"token_invalidated",
	} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func transientMessage(value string) bool {
	if value == "" {
		return false
	}
	for _, token := range []string{
		"context deadline exceeded",
		"i/o timeout",
		"connection refused",
		"connection reset",
		"no such host",
		"unexpected eof",
		"tls handshake timeout",
	} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func decodePayload(body []byte) map[string]any {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if decoder.Decode(&payload) != nil {
		return map[string]any{}
	}
	return payload
}

func nestedMap(values map[string]any, key string) map[string]any {
	value, _ := values[key].(map[string]any)
	return value
}

func firstString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				return text
			}
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		}
	}
	return ""
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func statusCodeFromMessage(message string) int {
	match := statusCodePattern.FindStringSubmatch(message)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(match[1])
	return value
}

func jsonBodyFromMessage(message string) []byte {
	start := strings.IndexByte(message, '{')
	end := strings.LastIndexByte(message, '}')
	if start < 0 || end < start {
		return nil
	}
	return []byte(message[start : end+1])
}

func retryAfterSeconds(headers http.Header, errorPayload map[string]any, root map[string]any, now time.Time) int64 {
	if headers != nil {
		value := strings.TrimSpace(headers.Get("Retry-After"))
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
			return seconds
		}
		if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
			return int64(retryAt.Sub(now).Seconds())
		}
	}
	for _, value := range []any{errorPayload["retry_after_seconds"], errorPayload["resets_in_seconds"], root["retry_after_seconds"], root["resets_in_seconds"]} {
		if seconds, ok := int64Value(value); ok && seconds > 0 {
			return seconds
		}
	}
	return 0
}

func resetAt(errorPayload map[string]any, root map[string]any) time.Time {
	for _, value := range []any{errorPayload["reset_at"], errorPayload["resets_at"], root["reset_at"], root["resets_at"]} {
		switch typed := value.(type) {
		case string:
			if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
				return parsed
			}
		case json.Number:
			if parsed, err := typed.Int64(); err == nil && parsed > 0 {
				return time.Unix(parsed, 0)
			}
		case float64:
			if typed > 0 {
				return time.Unix(int64(typed), 0)
			}
		}
	}
	return time.Time{}
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case float64:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
