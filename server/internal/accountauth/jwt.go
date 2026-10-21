package accountauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// ClaimString reads one string claim from a JWT payload. The signature is
// not verified and must not be used to authorize a request. Use it only to
// read display or dedup identity (sub, email, team_id, principal_id).
// path is dot-separated. A leading "claims." refers to the payload object
// itself, so "claims.sub" reads the payload's sub claim rather than a
// nested "claims" object. Nested objects use a path such as "a.b".
func ClaimString(token, path string) (string, error) {
	claims, err := payloadClaims(token)
	if err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "claims.")
	if path == "" || strings.Contains(path, "..") || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") {
		return "", errors.New("jwt: claim path is empty")
	}
	var current any = claims
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("jwt: claim %q is not a string", path)
		}
		next, ok := object[part]
		if !ok {
			return "", fmt.Errorf("jwt: claim %q is missing", path)
		}
		current = next
	}
	value, ok := current.(string)
	if !ok {
		return "", fmt.Errorf("jwt: claim %q is not a string", path)
	}
	return value, nil
}

// ClaimExpiry reads the payload exp claim as a UTC time. The boolean is
// false when the token is not a JWT, exp is missing, or exp is not a
// number. The signature is not verified.
func ClaimExpiry(token string) (time.Time, bool) {
	claims, err := payloadClaims(token)
	if err != nil {
		return time.Time{}, false
	}
	value, ok := claims["exp"]
	if !ok {
		return time.Time{}, false
	}
	seconds, ok := jsonNumber(value)
	if !ok {
		return time.Time{}, false
	}
	whole, frac := math.Modf(seconds)
	return time.Unix(int64(whole), int64(frac*float64(time.Second))).UTC(), true
}

func payloadClaims(token string) (map[string]any, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("jwt: token is not a jwt")
	}
	raw, err := decodeBase64URL(parts[1])
	if err != nil {
		return nil, errors.New("jwt: payload is not valid base64url")
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil || claims == nil {
		return nil, errors.New("jwt: payload is not a json object")
	}
	return claims, nil
}

func decodeBase64URL(value string) ([]byte, error) {
	if decoded, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func jsonNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
