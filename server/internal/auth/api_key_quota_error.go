package auth

import (
	"errors"
	"time"

	"xlyra/server/internal/store"
)

type APIKeyQuotaFailure struct {
	Code    string
	Message string
	Scope   string
	ResetAt *time.Time
}

func APIKeyQuotaFailureFromError(err error) (APIKeyQuotaFailure, bool) {
	var quotaErr *store.APIKeyQuotaExceededError
	if !errors.As(err, &quotaErr) {
		return APIKeyQuotaFailure{}, false
	}
	failure := APIKeyQuotaFailure{
		Code:    "api_key_" + quotaErr.Scope + "_quota_exhausted",
		Message: "API key " + quotaErr.Scope + " quota has been exhausted.",
		Scope:   quotaErr.Scope,
		ResetAt: quotaErr.ResetAt,
	}
	return failure, true
}

func (f APIKeyQuotaFailure) Payload(requestID string) map[string]any {
	payload := map[string]any{
		"type":    "authentication_error",
		"code":    f.Code,
		"message": f.Message,
		"param":   nil,
		"scope":   f.Scope,
	}
	if f.ResetAt != nil {
		payload["reset_at"] = f.ResetAt.Format(time.RFC3339)
	}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}
