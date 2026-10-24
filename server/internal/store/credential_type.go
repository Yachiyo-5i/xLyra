package store

import (
	"strings"

	"github.com/google/uuid"
)

const (
	CredentialTypeAPIKey    = "api_key"
	CredentialPrefixGrok    = "grok_sso:"
	CredentialPrefixAccount = "account:"
)

func IsAPIKeyCredentialType(v string) bool {
	return v == CredentialTypeAPIKey || strings.HasPrefix(v, CredentialTypeAPIKey+":")
}

func NewAccountCredentialType(siteType string, id uuid.UUID) string {
	siteType = strings.ToLower(strings.TrimSpace(siteType))
	return CredentialPrefixAccount + siteType + ":" + id.String()
}

func ParseAccountCredentialType(v string) (siteType string, id uuid.UUID, ok bool) {
	rest, found := strings.CutPrefix(v, CredentialPrefixAccount)
	if !found || rest == "" {
		return "", uuid.Nil, false
	}
	colon := strings.LastIndex(rest, ":")
	if colon <= 0 || colon == len(rest)-1 {
		return "", uuid.Nil, false
	}
	siteType = rest[:colon]
	if siteType == "" || !strings.Contains(siteType, "/") || strings.Contains(siteType, ":") {
		return "", uuid.Nil, false
	}
	id, ok = parseStandardUUID(rest[colon+1:])
	if !ok {
		return "", uuid.Nil, false
	}
	return siteType, id, true
}

func parseStandardUUID(v string) (uuid.UUID, bool) {
	if len(v) != 36 {
		return uuid.Nil, false
	}
	for i := 0; i < len(v); i++ {
		switch i {
		case 8, 13, 18, 23:
			if v[i] != '-' {
				return uuid.Nil, false
			}
		default:
			c := v[i]
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return uuid.Nil, false
			}
		}
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func IsAccountCredentialType(v string) bool {
	if strings.HasPrefix(v, CredentialPrefixGrok) {
		return true
	}
	_, _, ok := ParseAccountCredentialType(v)
	return ok
}

func IsAccountCredentialTypeOf(v, siteType string) bool {
	parsed, _, ok := ParseAccountCredentialType(v)
	return ok && strings.EqualFold(strings.TrimSpace(parsed), strings.TrimSpace(siteType))
}

func IsModelBoundCredentialType(v string) bool {
	return IsAPIKeyCredentialType(v) || IsAccountCredentialType(v)
}

func CredentialCategory(v string) string {
	switch {
	case IsAPIKeyCredentialType(v):
		return CredentialTypeAPIKey
	case IsAccountCredentialType(v):
		return "account"
	case v == "oauth", v == "system_token", v == "xlyra":
		return v
	default:
		return ""
	}
}
