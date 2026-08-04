package gateway

import (
	"database/sql"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

func stringFromMapAny(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	text, _ := values[key].(string)
	return strings.TrimSpace(text)
}

func int64PtrValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func stringValue(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func stringValueFromNull(value sql.NullString, fallback string) string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return fallback
	}
	return strings.TrimSpace(value.String)
}

func nullableCredentialID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value.String()
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func zeroInt64ToNil(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func zeroIntToNil(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func endpointPath(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Path == "" {
		return ""
	}
	return parsed.Path
}
