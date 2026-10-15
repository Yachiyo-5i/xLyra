package jsplugin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

func base64Encode(value string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(value)), nil
}

func base64Decode(value string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", errors.New("base64 payload is not utf-8")
	}
	return string(decoded), nil
}

func base64URLEncode(value string) (string, error) {
	return base64.RawURLEncoding.EncodeToString([]byte(value)), nil
}

func base64URLDecode(value string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(decoded) {
		return "", errors.New("base64 payload is not utf-8")
	}
	return string(decoded), nil
}

func hmacSHA256(key, message string) (string, error) {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func sha256Hex(message string) (string, error) {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:]), nil
}

func signHS256(payload any, secret string) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if !jsonIsObject(body) {
		return "", errors.New("jwt payload must be an object")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	encoded := base64.RawURLEncoding.EncodeToString(body)
	unsigned := header + "." + encoded
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func decodeHS256(token, secret string) (any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("jwt must have three parts")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, err
	}
	alg, _ := header["alg"].(string)
	if alg != "HS256" {
		return nil, fmt.Errorf("jwt alg %q is not supported", alg)
	}
	unsigned := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(unsigned))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, errors.New("jwt signature mismatch")
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, err
	}
	if !jsonIsObject(payloadJSON) {
		return nil, errors.New("jwt payload must be an object")
	}
	return payload, nil
}

func jsonIsObject(body []byte) bool {
	for _, b := range body {
		if b == ' ' || b == '\n' || b == '\t' || b == '\r' {
			continue
		}
		return b == '{'
	}
	return false
}

func newUUID() (string, error) {
	return uuid.NewString(), nil
}
