package store

import "strings"

const (
	CredentialTypeAPIKey = "api_key"
	CredentialPrefixGrok = "grok_sso:"
)

func IsAPIKeyCredentialType(v string) bool {
	return v == CredentialTypeAPIKey || strings.HasPrefix(v, CredentialTypeAPIKey+":")
}

func IsAccountCredentialType(v string) bool {
	return strings.HasPrefix(v, CredentialPrefixGrok)
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
