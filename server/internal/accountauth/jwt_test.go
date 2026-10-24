package accountauth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestClaimStringAndExpiry(t *testing.T) {
	t.Parallel()
	token := jwtToken(t, `{"sub":"user-1","email":"a@b.c","profile":{"email":"nested@b.c"},"claims":{"sub":"nested-sub"}}`, false)
	sub, err := ClaimString(token, "sub")
	if err != nil || sub != "user-1" {
		t.Fatalf("sub = %q, %v", sub, err)
	}
	sub, err = ClaimString(token, "claims.sub")
	if err != nil || sub != "user-1" {
		t.Fatalf("claims.sub = %q, %v; want the payload sub", sub, err)
	}
	nested, err := ClaimString(token, "profile.email")
	if err != nil || nested != "nested@b.c" {
		t.Fatalf("profile.email = %q, %v", nested, err)
	}
	nested, err = ClaimString(token, "claims.profile.email")
	if err != nil || nested != "nested@b.c" {
		t.Fatalf("claims.profile.email = %q, %v", nested, err)
	}
	inner, err := ClaimString(token, "claims.claims.sub")
	if err != nil || inner != "nested-sub" {
		t.Fatalf("claims.claims.sub = %q, %v", inner, err)
	}
	if _, ok := ClaimExpiry(token); ok {
		t.Fatal("missing exp reported a time")
	}

	exp := int64(1700000000)
	withExp := jwtToken(t, `{"sub":"user-1","exp":1700000000}`, false)
	got, ok := ClaimExpiry(withExp)
	if !ok || !got.Equal(time.Unix(exp, 0)) {
		t.Fatalf("exp = %s ok=%v", got, ok)
	}
	// A garbage signature is ignored. Claim reads are not authentication.
	parts := strings.Split(withExp, ".")
	unsigned := parts[0] + "." + parts[1] + ".@@@"
	if _, err := ClaimString(unsigned, "sub"); err != nil {
		t.Fatalf("unsigned token: %v", err)
	}
}

func TestClaimStringBase64URLPadding(t *testing.T) {
	t.Parallel()
	payload := `{"sub":"a"}`
	padded := jwtToken(t, payload, true)
	raw := jwtToken(t, payload, false)
	paddedPayload := strings.Split(padded, ".")[1]
	rawPayload := strings.Split(raw, ".")[1]
	if !strings.Contains(paddedPayload, "=") {
		t.Fatalf("padded payload %q has no padding", paddedPayload)
	}
	if strings.Contains(rawPayload, "=") {
		t.Fatalf("raw payload %q has padding", rawPayload)
	}
	for _, token := range []string{padded, raw} {
		sub, err := ClaimString(token, "sub")
		if err != nil || sub != "a" {
			t.Fatalf("sub = %q, %v token=%s", sub, err, token)
		}
	}
}

func TestClaimStringRejectsBadTokens(t *testing.T) {
	t.Parallel()
	valid := jwtToken(t, `{"email":"a@b.c"}`, false)
	numeric := jwtToken(t, `{"sub":1}`, false)
	object := jwtToken(t, `{"profile":"x"}`, false)
	nullClaim := jwtToken(t, `{"sub":null}`, false)
	notObject := jwtToken(t, `["sub"]`, false)
	notJSON := partsToken(t, base64.RawURLEncoding.EncodeToString([]byte("not-json")))
	cases := []struct {
		name    string
		token   string
		path    string
		message string
	}{
		{name: "empty", token: "", path: "sub", message: "not a jwt"},
		{name: "two parts", token: "a.b", path: "sub", message: "not a jwt"},
		{name: "four parts", token: "a.b.c.d", path: "sub", message: "not a jwt"},
		{name: "bad base64", token: "aaa.!!!!.bbb", path: "sub", message: "base64url"},
		{name: "not json", token: notJSON, path: "sub", message: "json object"},
		{name: "not object", token: notObject, path: "sub", message: "json object"},
		{name: "missing", token: valid, path: "sub", message: "missing"},
		{name: "number", token: numeric, path: "sub", message: "not a string"},
		{name: "intermediate string", token: object, path: "profile.email", message: "not a string"},
		{name: "null", token: nullClaim, path: "sub", message: "not a string"},
		{name: "empty path", token: valid, path: "claims.", message: "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ClaimString(tc.token, tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("error = %v, want %q", err, tc.message)
			}
			if tc.token != "" && strings.Contains(err.Error(), tc.token) {
				t.Fatal("error leaked the token")
			}
		})
	}
	if _, ok := ClaimExpiry("not-a-jwt"); ok {
		t.Fatal("non-jwt expiry")
	}
	if _, ok := ClaimExpiry(jwtToken(t, `{"exp":"1700000000"}`, false)); ok {
		t.Fatal("string exp")
	}
	if _, ok := ClaimExpiry(jwtToken(t, `{"exp":true}`, false)); ok {
		t.Fatal("bool exp")
	}
	if _, ok := ClaimExpiry("aaa.!!!!.bbb"); ok {
		t.Fatal("bad base64 expiry")
	}
}

func jwtToken(t *testing.T, payload string, padded bool) string {
	t.Helper()
	enc := base64.RawURLEncoding.EncodeToString
	if padded {
		enc = base64.URLEncoding.EncodeToString
	}
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + "." + enc([]byte("sig"))
}

func partsToken(t *testing.T, payload string) string {
	t.Helper()
	return "e30." + payload + ".c2ln"
}
