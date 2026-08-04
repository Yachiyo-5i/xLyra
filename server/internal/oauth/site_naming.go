package oauth

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const oauthSiteRandomSuffixLength = 4

func ImportedSiteName(provider string, email string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case antigravityProvider:
		return SiteNameFromEmail("Antigravity", email)
	default:
		return SiteNameFromEmail("Codex", email)
	}
}

func CodexSiteNameFromEmail(email string) (string, error) {
	return SiteNameFromEmail("Codex", email)
}

func SiteNameFromEmail(prefix string, email string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	email = strings.TrimSpace(strings.ToLower(email))
	if prefix == "" {
		prefix = "OAuth"
	}
	if email == "" {
		return prefix, nil
	}
	at := strings.Index(email, "@")
	if at <= 0 {
		return prefix, nil
	}

	prefixPart := email[:min(len(email), 4)]
	prefixPart = normalizeSiteNameToken(prefixPart)
	if prefixPart == "" {
		return prefix, nil
	}
	suffix, err := randomLowercaseToken(oauthSiteRandomSuffixLength)
	if err != nil {
		return "", err
	}
	return prefix + "_" + prefixPart + "_" + suffix, nil
}

func SiteSlugFromName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	builder := strings.Builder{}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		} else if r == '_' || r == '-' || r == ' ' {
			builder.WriteRune('-')
		}
	}
	slug := builder.String()
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	return strings.Trim(slug, "-")
}

func normalizeSiteNameToken(value string) string {
	builder := strings.Builder{}
	for _, r := range strings.TrimSpace(strings.ToLower(value)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func randomLowercaseToken(length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyz"
	max := big.NewInt(int64(len(alphabet)))
	out := make([]byte, length)
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate oauth site suffix: %w", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}
