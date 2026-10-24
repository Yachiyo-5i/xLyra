package jsplugin

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

const accountImportFixture = `{"id":"u-1","uid":"u-1","nickname":"Nick","access_token":"at","refresh_token":"rt","expires_at":1794469656000,"domain":"www.example.cn","token_type":"Bearer","realm":"cn"}`

func importCredFields() []SiteProviderField {
	return []SiteProviderField{
		{Name: "access_token", Secret: true},
		{Name: "refresh_token", Secret: true},
		{Name: "expires_at", Type: "unix"},
		{Name: "domain"},
		{Name: "realm"},
	}
}

func importConfig(containers ...string) *SiteProviderImport {
	if len(containers) == 0 {
		containers = []string{"accounts", "items", "data"}
	}
	return &SiteProviderImport{
		Enabled:    true,
		Containers: containers,
		FieldMap: map[string][]string{
			"access_token":  {"access_token", "accessToken"},
			"refresh_token": {"refresh_token", "refreshToken"},
			"expires_at":    {"expires_at", "expiresAt"},
			"domain":        {"domain"},
			"realm":         {"realm"},
		},
	}
}

func importCredential(fields []SiteProviderField, expiresAtField string) SiteProviderCredential {
	return SiteProviderCredential{Fields: fields, ExpiresAtField: expiresAtField, AccessTokenField: "access_token"}
}

func parseImport(t *testing.T, raw string, cfg *SiteProviderImport) []ImportedAccount {
	t.Helper()
	return parseImportRaw(t, []byte(raw), cfg, importCredFields(), "expires_at")
}

func parseImportRaw(t *testing.T, raw []byte, cfg *SiteProviderImport, fields []SiteProviderField, expiresAtField string) []ImportedAccount {
	t.Helper()
	got, err := ParseAccountImport(raw, cfg, importCredential(fields, expiresAtField))
	if err != nil {
		t.Fatalf("ParseAccountImport: %v", err)
	}
	return got
}

func requireImportError(t *testing.T, err error, want string, hidden ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, want) {
		t.Fatalf("error %q does not contain %q", msg, want)
	}
	for _, value := range hidden {
		if value != "" && strings.Contains(msg, value) {
			t.Fatalf("error %q contains a field value", msg)
		}
	}
}

func requireAccount(t *testing.T, got ImportedAccount, fields map[string]string, expires time.Time) {
	t.Helper()
	if len(got.Fields) != len(fields) {
		t.Fatalf("fields = %#v, want %#v", got.Fields, fields)
	}
	for key, want := range fields {
		if got.Fields[key] != want {
			t.Fatalf("field %s = %q, want %q", key, got.Fields[key], want)
		}
	}
	if !expires.IsZero() || !got.ExpiresAt.IsZero() {
		if !got.ExpiresAt.Equal(expires) {
			t.Fatalf("expires = %s, want %s", got.ExpiresAt.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano))
		}
	}
}

func TestParseAccountImportShapes(t *testing.T) {
	t.Parallel()

	expires := time.UnixMilli(1794469656000).UTC()
	want := map[string]string{
		"access_token":  "at",
		"refresh_token": "rt",
		"expires_at":    "1794469656",
		"domain":        "www.example.cn",
		"realm":         "cn",
	}
	cases := []struct {
		name       string
		raw        string
		containers []string
	}{
		{name: "single object", raw: accountImportFixture},
		{name: "array", raw: "[" + accountImportFixture + "]"},
		{name: "accounts array", raw: `{"accounts":[` + accountImportFixture + `]}`},
		{name: "items array", raw: `{"items":[` + accountImportFixture + `]}`},
		{name: "data array", raw: `{"data":[` + accountImportFixture + `]}`},
		{name: "wrapped object", raw: `{"items":` + accountImportFixture + `}`},
		{name: "dot path array", raw: `{"data":{"accounts":[` + accountImportFixture + `]}}`, containers: []string{"data.accounts"}},
		{name: "dot path object", raw: `{"data":{"accounts":` + accountImportFixture + `}}`, containers: []string{"data.accounts"}},
		{name: "bom", raw: "\ufeff[" + accountImportFixture + "]"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseImport(t, tt.raw, importConfig(tt.containers...))
			if len(got) != 1 {
				t.Fatalf("got %d accounts, want 1", len(got))
			}
			requireAccount(t, got[0], want, expires)
			if _, ok := got[0].Fields["nickname"]; ok {
				t.Fatal("unmapped nickname was copied into the credential bag")
			}
		})
	}
}

func TestParseAccountImportContainerOrder(t *testing.T) {
	t.Parallel()

	raw := `{"items":[{"access_token":"from-items","refresh_token":"r"}],"accounts":[{"access_token":"from-accounts","refresh_token":"r"}]}`
	got := parseImport(t, raw, importConfig("accounts", "items"))
	if len(got) != 1 || got[0].Fields["access_token"] != "from-accounts" {
		t.Fatalf("accounts = %#v, want the accounts container", got)
	}

	// A container that resolves to an empty array wins over a later path.
	emptyFirst := `{"accounts":[],"items":[{"access_token":"later-secret","refresh_token":"r"}]}`
	_, err := ParseAccountImport([]byte(emptyFirst), importConfig("accounts", "items"), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, "contains no accounts", "later-secret")

	// A scalar at the container path is not a wrapper. The root object is the account.
	scalar := `{"data":"not-a-wrapper","access_token":"root-token","refresh_token":"r"}`
	got = parseImport(t, scalar, importConfig("data"))
	if len(got) != 1 || got[0].Fields["access_token"] != "root-token" {
		t.Fatalf("scalar container = %#v", got)
	}
	if strings.Contains(got[0].Fields["access_token"], "not-a-wrapper") {
		t.Fatal("scalar container value was used as a field")
	}
}

func TestParseAccountImportAliasPriority(t *testing.T) {
	t.Parallel()

	raw := `{
		"access_token": " ",
		"accessToken": "from-camel",
		"refresh_token": "",
		"refreshToken": 77,
		"expires_at": null,
		"expiresAt": " 1794469656 ",
		"domain": "  example.cn  ",
		"realm": false,
		"nickname": "ignored"
	}`
	got := parseImport(t, raw, importConfig())
	if len(got) != 1 {
		t.Fatalf("got %d accounts", len(got))
	}
	requireAccount(t, got[0], map[string]string{
		"access_token":  "from-camel",
		"refresh_token": "77",
		"expires_at":    "1794469656",
		"domain":        "example.cn",
	}, time.Unix(1794469656, 0).UTC())
	if _, ok := got[0].Fields["realm"]; ok {
		t.Fatal("boolean realm was stored")
	}

	firstWins := `{"access_token":"snake","accessToken":"camel","refresh_token":"r"}`
	got = parseImport(t, firstWins, importConfig())
	if got[0].Fields["access_token"] != "snake" {
		t.Fatalf("access_token = %q, want the first alias", got[0].Fields["access_token"])
	}

	numberFirst := `{"access_token":10001,"accessToken":"ignored","refresh_token":"r"}`
	got = parseImport(t, numberFirst, importConfig())
	if got[0].Fields["access_token"] != "10001" {
		t.Fatalf("access_token = %q, want the JSON number", got[0].Fields["access_token"])
	}

	// Aliases are literal top-level keys. "access.token" is not the path access → token.
	cfg := importConfig()
	cfg.FieldMap["access_token"] = []string{"access.token", "access_token"}
	dotted := `{"access":{"token":"nested-secret-value"},"access.token":"literal","access_token":"plain","refresh_token":"r"}`
	got = parseImport(t, dotted, cfg)
	if got[0].Fields["access_token"] != "literal" {
		t.Fatalf("access_token = %q, want the literal dotted key", got[0].Fields["access_token"])
	}
}

func TestParseAccountImportExpiry(t *testing.T) {
	t.Parallel()

	sec := time.Unix(1794469656, 0).UTC()
	rfc := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
	offset := time.Date(2026, 1, 31, 15, 59, 59, 0, time.UTC)
	fraction := time.Date(2026, 1, 31, 23, 59, 59, 100_000_000, time.UTC)
	thresholdMS := time.UnixMilli(unixMillisThreshold).UTC()
	below := time.Unix(unixMillisThreshold-1, 0).UTC()
	cases := []struct {
		name    string
		expires string
		want    time.Time
	}{
		{name: "unix seconds", expires: "1794469656", want: sec},
		{name: "unix milliseconds", expires: "1794469656000", want: time.UnixMilli(1794469656000).UTC()},
		{name: "numeric string seconds", expires: `"1794469656"`, want: sec},
		{name: "numeric string milliseconds", expires: `"1794469656000"`, want: time.UnixMilli(1794469656000).UTC()},
		{name: "padded numeric string", expires: `" 1794469656 "`, want: sec},
		{name: "fractional seconds", expires: "1794469656.0", want: sec},
		{name: "rfc3339", expires: `"2026-01-31T23:59:59Z"`, want: rfc},
		{name: "rfc3339 offset", expires: `"2026-01-31T23:59:59+08:00"`, want: offset},
		{name: "rfc3339 fraction", expires: `"2026-01-31T23:59:59.1Z"`, want: fraction},
		{name: "threshold is milliseconds", expires: "1000000000000", want: thresholdMS},
		{name: "just below threshold is seconds", expires: "999999999999", want: below},
		{name: "scientific milliseconds", expires: `"1e12"`, want: thresholdMS},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := `{"access_token":"a","refresh_token":"r","expires_at":` + tt.expires + `}`
			got := parseImport(t, raw, importConfig())
			if len(got) != 1 {
				t.Fatalf("got %d accounts", len(got))
			}
			requireAccount(t, got[0], map[string]string{
				"access_token":  "a",
				"refresh_token": "r",
				"expires_at":    strconv.FormatInt(tt.want.Unix(), 10),
			}, tt.want)
			if tt.name == "just below threshold is seconds" && got[0].ExpiresAt.Equal(time.UnixMilli(unixMillisThreshold-1).UTC()) {
				t.Fatal("value below 1e12 was read as milliseconds")
			}
			if tt.name == "threshold is milliseconds" && got[0].ExpiresAt.Equal(time.Unix(unixMillisThreshold, 0).UTC()) {
				t.Fatal("1e12 was read as seconds")
			}
		})
	}

	t.Run("omitted", func(t *testing.T) {
		got := parseImport(t, `{"access_token":"a","refresh_token":"r","domain":"  "}`, importConfig())
		requireAccount(t, got[0], map[string]string{
			"access_token":  "a",
			"refresh_token": "r",
		}, time.Time{})
		if _, ok := got[0].Fields["expires_at"]; ok {
			t.Fatal("omitted expires_at was stored")
		}
		if _, ok := got[0].Fields["domain"]; ok {
			t.Fatal("blank domain was stored")
		}
	})

	t.Run("non positive does not fall through", func(t *testing.T) {
		raw := `{"access_token":"a","refresh_token":"r","expires_at":0,"expiresAt":1794469656}`
		_, err := ParseAccountImport([]byte(raw), importConfig(), importCredential(importCredFields(), "expires_at"))
		requireImportError(t, err, `account 1 has an invalid "expires_at"`)
	})
}

func TestParseAccountImportRejectsMissingRequired(t *testing.T) {
	t.Parallel()

	raw := `{"data":{"accounts":[{"access_token":"kept-secret-token","refresh_token":"r"},{"access_token":"missing-refresh-secret","domain":"cn"}]}}`
	_, err := ParseAccountImport([]byte(raw), importConfig("data.accounts"), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, `account 2 is missing required field "refresh_token"`, "kept-secret-token", "missing-refresh-secret")

	nested := `{"access_token":{"token":"nested-secret-value"},"refresh_token":"r"}`
	_, err = ParseAccountImport([]byte(nested), importConfig(), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, `account 1 is missing required field "access_token"`, "nested-secret-value")

	both := `{"domain":"cn"}`
	_, err = ParseAccountImport([]byte(both), importConfig(), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, `account 1 is missing required field "access_token"`)
}

func TestParseAccountImportRejectsUnknownField(t *testing.T) {
	t.Parallel()

	cfg := importConfig()
	cfg.FieldMap["session_token"] = []string{"session_token"}
	raw := `{"access_token":"unknown-field-secret","refresh_token":"r","session_token":"session-value-secret"}`
	_, err := ParseAccountImport([]byte(raw), cfg, importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, `import fieldMap "session_token" is not a declared credential field`, "unknown-field-secret", "session-value-secret")

	_, err = ParseAccountImport([]byte(`{}`), nil, importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, "account import config is required")

	fields := importCredFields()
	_, err = ParseAccountImport([]byte(`{"access_token":"a","refresh_token":"r"}`), importConfig(), importCredential(fields, "missing_exp"))
	requireImportError(t, err, `import expires field "missing_exp" is not a declared credential field`)

	fields[2].Type = "string"
	_, err = ParseAccountImport([]byte(`{"access_token":"a","refresh_token":"r"}`), importConfig(), importCredential(fields, "expires_at"))
	requireImportError(t, err, `import expires field "expires_at" must have type "unix"`)
}

func TestParseAccountImportLimits(t *testing.T) {
	t.Parallel()

	object := `{"access_token":"limit-secret-token","refresh_token":"r"}`
	var many strings.Builder
	many.WriteByte('[')
	for i := 0; i < maxAccountImportRecords+1; i++ {
		if i > 0 {
			many.WriteByte(',')
		}
		many.WriteString(object)
	}
	many.WriteByte(']')
	_, err := ParseAccountImport([]byte(many.String()), importConfig(), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, "more than 500 accounts", "limit-secret-token")

	many.Reset()
	many.WriteByte('[')
	for i := 0; i < maxAccountImportRecords; i++ {
		if i > 0 {
			many.WriteByte(',')
		}
		many.WriteString(object)
	}
	many.WriteByte(']')
	got := parseImport(t, many.String(), importConfig())
	if len(got) != 1 {
		t.Fatalf("500 identical accounts = %d, want 1 after dedup", len(got))
	}

	over := make([]byte, maxAccountImportBytes+1)
	copy(over, []byte(`{"access_token":"oversize-secret-token","refresh_token":"r"}`))
	_, err = ParseAccountImport(over, importConfig(), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, "exceeds 4 MiB", "oversize-secret-token")

	body := []byte(`{"access_token":"a","refresh_token":"r"}`)
	exact := make([]byte, maxAccountImportBytes)
	copy(exact, body)
	for i := len(body); i < len(exact); i++ {
		exact[i] = ' '
	}
	got = parseImportRaw(t, exact, importConfig(), importCredFields(), "expires_at")
	if len(got) != 1 || got[0].Fields["access_token"] != "a" {
		t.Fatalf("exact 4 MiB = %#v", got)
	}
}

func TestParseAccountImportDedup(t *testing.T) {
	t.Parallel()

	same := time.Unix(1794469656, 0).UTC()
	rfc := same.Format(time.RFC3339)
	raw := `[
		{"access_token":"a","refresh_token":"r","expires_at":1794469656000,"realm":"cn","nickname":"one"},
		{"realm":"cn","refresh_token":"r","access_token":"a","expires_at":1794469656,"nickname":"two"},
		{"access_token":"a","refresh_token":"r","expires_at":"` + rfc + `","realm":"cn"},
		{"access_token":"a","refresh_token":"r","expires_at":1794469656,"realm":"intl"},
		{"access_token":"a","refresh_token":"other","expires_at":1794469656,"realm":"cn"},
		{"access_token":"a","refresh_token":"r","expires_at":1794469656,"realm":"intl"}
	]`
	got := parseImport(t, raw, importConfig())
	if len(got) != 3 {
		t.Fatalf("got %d accounts, want 3", len(got))
	}
	requireAccount(t, got[0], map[string]string{
		"access_token": "a", "refresh_token": "r", "expires_at": "1794469656", "realm": "cn",
	}, same)
	requireAccount(t, got[1], map[string]string{
		"access_token": "a", "refresh_token": "r", "expires_at": "1794469656", "realm": "intl",
	}, same)
	requireAccount(t, got[2], map[string]string{
		"access_token": "a", "refresh_token": "other", "expires_at": "1794469656", "realm": "cn",
	}, same)

	// Same Unix second with different fractional seconds stays two accounts.
	fractional := `[
		{"access_token":"a","refresh_token":"r","expires_at":"2026-01-31T23:59:59.1Z"},
		{"access_token":"a","refresh_token":"r","expires_at":"2026-01-31T23:59:59.2Z"}
	]`
	got = parseImport(t, fractional, importConfig())
	if len(got) != 2 {
		t.Fatalf("fractional expiries = %d, want 2", len(got))
	}
	if got[0].ExpiresAt.Nanosecond() != 100_000_000 || got[1].ExpiresAt.Nanosecond() != 200_000_000 {
		t.Fatalf("fractional expiries = %s, %s", got[0].ExpiresAt, got[1].ExpiresAt)
	}
}

func TestParseAccountImportErrorsOmitValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		raw    string
		want   string
		hidden []string
	}{
		{
			name:   "invalid expiry",
			raw:    `{"access_token":"omit-access-secret","refresh_token":"omit-refresh-secret","expires_at":"not-a-time-SECRET"}`,
			want:   `account 1 has an invalid "expires_at"`,
			hidden: []string{"omit-access-secret", "omit-refresh-secret", "not-a-time-SECRET"},
		},
		{
			name:   "negative expiry",
			raw:    `{"access_token":"a","refresh_token":"r","expires_at":-5}`,
			want:   `account 1 has an invalid "expires_at"`,
			hidden: []string{"-5"},
		},
		{
			name:   "malformed",
			raw:    `{"access_token":"malformed-secret-token"`,
			want:   "account import file is not valid JSON",
			hidden: []string{"malformed-secret-token"},
		},
		{
			name:   "trailing value",
			raw:    `{"access_token":"a","refresh_token":"r"}{"access_token":"trailing-secret-token","refresh_token":"r"}`,
			want:   "account import file is not valid JSON",
			hidden: []string{"trailing-secret-token"},
		},
		{
			name:   "not an object",
			raw:    `[{"access_token":"a","refresh_token":"r"},"element-secret-token"]`,
			want:   "account 2 must be a JSON object",
			hidden: []string{"element-secret-token"},
		},
		{
			name:   "scalar string",
			raw:    `"scalar-secret-token"`,
			want:   "account import file must contain an account object or an array of accounts",
			hidden: []string{"scalar-secret-token"},
		},
		{
			name:   "scalar number",
			raw:    "1794469656000",
			want:   "account import file must contain an account object or an array of accounts",
			hidden: []string{"1794469656000"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseAccountImport([]byte(tt.raw), importConfig(), importCredential(importCredFields(), "expires_at"))
			if got != nil {
				t.Fatalf("got %#v, want no accounts", got)
			}
			requireImportError(t, err, tt.want, tt.hidden...)
		})
	}
}

func TestParseAccountImportUnicode(t *testing.T) {
	t.Parallel()

	raw := `{"access_token":"tok","refresh_token":"rt","realm":"八千代","domain":"東京.example"}`
	got := parseImport(t, raw, importConfig())
	requireAccount(t, got[0], map[string]string{
		"access_token":  "tok",
		"refresh_token": "rt",
		"realm":         "八千代",
		"domain":        "東京.example",
	}, time.Time{})

	leaking := `{"access_token":"密钥-SECRET-八千代","domain":"東京"}`
	_, err := ParseAccountImport([]byte(leaking), importConfig(), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, `account 1 is missing required field "refresh_token"`, "密钥-SECRET-八千代", "東京")
}

func TestParseAccountImportRejectsMalformedAndEmpty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty", raw: "", want: "account import file is empty"},
		{name: "spaces", raw: " \n\t ", want: "account import file is empty"},
		{name: "bom", raw: "\ufeff \n", want: "account import file is empty"},
		{name: "not json", raw: "access_token=abc", want: "account import file is not valid JSON"},
		{name: "truncated", raw: "{", want: "account import file is not valid JSON"},
		{name: "null", raw: "null", want: "account import file must contain an account object or an array of accounts"},
		{name: "bool", raw: "true", want: "account import file must contain an account object or an array of accounts"},
		{name: "empty array", raw: "[]", want: "account import file contains no accounts"},
		{name: "empty wrapper", raw: `{"accounts":[]}`, want: "account import file contains no accounts"},
		{name: "nil raw via empty", raw: "", want: "account import file is empty"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseAccountImport([]byte(tt.raw), importConfig(), importCredential(importCredFields(), "expires_at"))
			requireImportError(t, err, tt.want)
		})
	}

	_, err := ParseAccountImport(nil, importConfig(), importCredential(importCredFields(), "expires_at"))
	requireImportError(t, err, "account import file is empty")
}

func TestParseAccountImportValueMapAndThreshold(t *testing.T) {
	t.Parallel()
	token := unsignedImportJWT(t, map[string]any{"sub": "user-1", "exp": 4102444800})
	cfg := importConfig()
	cfg.FieldMap["realm"] = []string{"realm"}
	cfg.FieldMap["email"] = []string{"nickname"}
	cfg.ValueMap = map[string]map[string]string{"realm": {"international": "intl"}}
	cut := int64(100000000000)
	cfg.MsThreshold = &cut
	cred := importCredential(append(importCredFields(), SiteProviderField{Name: "email"}, SiteProviderField{Name: "realm"}), "expires_at")
	cred.ExpiresFrom = "claims.exp"
	raw := `{"access_token":"` + token + `","refresh_token":"r","nickname":"nick","realm":"international","expires_at":200000000000}`
	parsed, err := ParseAccountImport([]byte(raw), cfg, cred)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 1 || parsed[0].Fields["realm"] != "intl" || parsed[0].Fields["email"] != "nick" || parsed[0].ExpiresAt.UnixMilli() != 200000000000 {
		t.Fatalf("mapped = %#v", parsed)
	}
	omitted := `{"access_token":"` + token + `","refresh_token":"r","realm":"cn"}`
	filled, err := ParseAccountImport([]byte(omitted), cfg, cred)
	if err != nil {
		t.Fatal(err)
	}
	if filled[0].Fields["expires_at"] != "4102444800" || !filled[0].ExpiresAt.Equal(time.Unix(4102444800, 0).UTC()) {
		t.Fatalf("expires from jwt = %#v", filled[0])
	}
}

func unsignedImportJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestApplyImportValueMapIsCaseInsensitive(t *testing.T) {
	entries := map[string]string{"international": "intl"}
	for _, in := range []string{"international", "International", "INTERNATIONAL"} {
		if got := applyImportValueMap(in, entries); got != "intl" {
			t.Fatalf("applyImportValueMap(%q) = %q, want intl", in, got)
		}
	}
	if got := applyImportValueMap("other", entries); got != "other" {
		t.Fatalf("unmapped value changed to %q", got)
	}
}
