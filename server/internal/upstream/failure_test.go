package upstream

import (
	"net/http"
	"testing"
	"time"
)

func TestClassifyResponseRecognizesRecoverableLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		status int
		body   string
		code   string
		scope  string
	}{
		{name: "xlyra daily", status: http.StatusUnauthorized, body: `{"error":{"type":"authentication_error","code":"api_key_daily_quota_exhausted","message":"API key daily quota has been exhausted.","scope":"daily","reset_at":"2030-01-02T03:04:05Z"}}`, code: "api_key_daily_quota_exhausted", scope: "daily"},
		{name: "sub2api key", status: http.StatusTooManyRequests, body: `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`, code: "API_KEY_QUOTA_EXHAUSTED"},
		{name: "sub2api subscription", status: http.StatusTooManyRequests, body: `{"code":"USAGE_LIMIT_EXCEEDED","message":"daily limit exceeded"}`, code: "USAGE_LIMIT_EXCEEDED"},
		{name: "sub2api balance", status: http.StatusForbidden, body: `{"code":"INSUFFICIENT_BALANCE","message":"Insufficient account balance"}`, code: "INSUFFICIENT_BALANCE"},
		{name: "openai quota", status: http.StatusTooManyRequests, body: `{"error":{"type":"insufficient_quota","code":"insufficient_quota","message":"quota exhausted"}}`, code: "insufficient_quota"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := ClassifyResponse(test.status, nil, []byte(test.body))
			if !got.Limited() || got.Code != test.code || got.Scope != test.scope {
				t.Fatalf("ClassifyResponse() = %#v, want limited code=%q scope=%q", got, test.code, test.scope)
			}
		})
	}
}

func TestClassifyResponseSeparatesCredentialAndTransientFailures(t *testing.T) {
	t.Parallel()

	invalid := ClassifyResponse(http.StatusUnauthorized, nil, []byte(`{"code":"INVALID_API_KEY","message":"Invalid API key"}`))
	if !invalid.CredentialInvalid() {
		t.Fatalf("invalid key = %#v, want credential invalid", invalid)
	}
	unknown := ClassifyResponse(http.StatusUnauthorized, nil, []byte(`{"message":"authentication failed"}`))
	if unknown.Class != FailureUnknown {
		t.Fatalf("ambiguous 401 = %#v, want unknown", unknown)
	}
	transient := ClassifyResponse(http.StatusBadGateway, nil, []byte(`{"message":"upstream unavailable"}`))
	if !transient.Transient() {
		t.Fatalf("502 = %#v, want transient", transient)
	}
}

func TestHTTPErrorAndMessageClassificationPreserveQuotaDetails(t *testing.T) {
	t.Parallel()

	headers := http.Header{"Retry-After": []string{"17"}}
	err := NewHTTPError("upstream returned", http.StatusUnauthorized, headers, []byte(`{"error":{"code":"api_key_weekly_quota_exhausted","message":"API key weekly quota has been exhausted.","scope":"weekly"}}`))
	got := ClassifyError(err)
	if !got.Limited() || got.Scope != "weekly" || got.RetryAfterSeconds != 17 {
		t.Fatalf("ClassifyError() = %#v, want weekly limited retry=17", got)
	}
	fromMessage := ClassifyMessage(err.Error())
	if !fromMessage.Limited() || fromMessage.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ClassifyMessage() = %#v, want recoverable 401 quota", fromMessage)
	}
	if plain := ClassifyMessage("sync failed: API_KEY_DAILY_QUOTA_EXHAUSTED"); !plain.Limited() {
		t.Fatalf("plain quota code = %#v, want limited", plain)
	}
	if plain := ClassifyMessage("sync failed: INVALID_API_KEY"); !plain.CredentialInvalid() {
		t.Fatalf("plain invalid key code = %#v, want credential invalid", plain)
	}
}

func TestClassifyResponseReadsResetAt(t *testing.T) {
	t.Parallel()

	got := ClassifyResponse(http.StatusUnauthorized, nil, []byte(`{"error":{"code":"api_key_daily_quota_exhausted","reset_at":"2030-01-02T03:04:05Z"}}`))
	want, _ := time.Parse(time.RFC3339, "2030-01-02T03:04:05Z")
	if !got.ResetAt.Equal(want) {
		t.Fatalf("ResetAt = %s, want %s", got.ResetAt, want)
	}
}
