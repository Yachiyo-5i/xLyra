package gateway

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAntigravityUpstreamPathNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		protocol antigravityProtocolAdapter
		baseURL  string
		want     string
	}{
		{
			name:    "empty buffered uses default base",
			baseURL: " \t ",
			want:    "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:generateContent",
		},
		{
			name:     "empty stream uses default base",
			protocol: antigravityProtocolAdapter{stream: true},
			baseURL:  "",
			want:     "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:streamGenerateContent?alt=sse",
		},
		{
			name:    "existing internal suffix avoids duplicate path",
			baseURL: " https://example.test/root/v1internal/ ",
			want:    "https://example.test/root/v1internal:generateContent",
		},
		{
			name:     "stream appends sse suffix",
			protocol: antigravityProtocolAdapter{stream: true},
			baseURL:  "https://example.test/root",
			want:     "https://example.test/root/v1internal:streamGenerateContent?alt=sse",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.protocol.UpstreamPath(tt.baseURL); got != tt.want {
				t.Fatalf("UpstreamPath(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestAntigravityRateLimitClassificationUsesFallbacks(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	errInfo, ok := classifyAntigravityUpstreamError(http.StatusTooManyRequests, []byte(`{
		"error": {
			"status": "RATE_LIMIT_EXCEEDED",
			"details": [{"metadata": {"quotaResetDelay": "bad-delay"}}]
		}
	}`), now)
	if !ok {
		t.Fatal("expected antigravity rate limit classification")
	}
	if errInfo.ErrorMessage != "RATE_LIMIT_EXCEEDED" {
		t.Fatalf("ErrorMessage = %q, want status fallback", errInfo.ErrorMessage)
	}
	if errInfo.CooldownDuration != 5*time.Minute || errInfo.RetryAfterSeconds != 300 {
		t.Fatalf("fallback cooldown = %s/%d, want 5m/300", errInfo.CooldownDuration, errInfo.RetryAfterSeconds)
	}

	if _, ok := classifyAntigravityUpstreamError(http.StatusForbidden, []byte(`{"error":{"status":"RATE_LIMIT_EXCEEDED"}}`), now); ok {
		t.Fatal("non-429 status should not classify as antigravity rate limit")
	}
	if _, ok := classifyAntigravityUpstreamError(http.StatusTooManyRequests, []byte(`{"error":{"status":"PERMISSION_DENIED"}}`), now); ok {
		t.Fatal("non-rate-limit reason should not classify as antigravity rate limit")
	}

	duration, seconds := parseAntigravityRetryAfter([]byte(`{"error":{"details":[{"metadata":{"quotaResetTimeStamp":"2026-06-23T12:00:45Z"}}]}}`), now)
	if duration != 45*time.Second || seconds != 45 {
		t.Fatalf("timestamp retry-after = %s/%d, want 45s/45", duration, seconds)
	}

	if got := antigravityErrorMessage([]byte(`{"error":{}}`), "fallback message"); got != "fallback message" {
		t.Fatalf("antigravityErrorMessage empty = %q, want fallback", got)
	}
}

func TestAntigravityImageOptionsAndFinishReasonBoundaries(t *testing.T) {
	t.Parallel()

	if got := antigravityImageSize(map[string]any{"imageSize": " 2k "}, "ignored"); got != "2K" {
		t.Fatalf("imageSize direct = %q, want 2K", got)
	}
	if got := antigravityImageSize(map[string]any{"quality": "standard"}, "ignored"); got != "1K" {
		t.Fatalf("quality standard size = %q, want 1K", got)
	}
	if got := antigravityImageSize(map[string]any{}, "imagen-4k"); got != "4K" {
		t.Fatalf("model inferred size = %q, want 4K", got)
	}
	if got := antigravityAspectRatioFromSize(" 1600 x 900 "); got != "16:9" {
		t.Fatalf("computed aspect ratio = %q, want 16:9", got)
	}
	if got := antigravityAspectRatioFromSize(" 1024 x 512 "); got != "1:1" {
		t.Fatalf("unsupported computed aspect ratio = %q, want 1:1", got)
	}

	for _, tt := range []struct {
		raw  string
		want string
	}{
		{raw: "MAX_TOKENS", want: "length"},
		{raw: "SAFETY", want: "content_filter"},
		{raw: "TOOL_CODE", want: "tool_calls"},
		{raw: "OTHER", want: "stop"},
	} {
		body := map[string]any{"candidates": []any{map[string]any{"finishReason": tt.raw}}}
		if got := antigravityFinishReason(body); got != tt.want {
			t.Fatalf("finish reason %q = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestAntigravityBufferedErrorPassThroughAndScalarHelpers(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"message":"upstream failed"}}`)
	transformed, err := (antigravityProtocolAdapter{}).TransformBufferedResponse(http.StatusBadGateway, http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse error pass-through returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusBadGateway || transformed.ContentType != "application/json; charset=utf-8" || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected pass-through response: %#v", transformed)
	}

	requestID := antigravityRequestID()
	if !strings.HasPrefix(requestID, "agent/") {
		t.Fatalf("antigravityRequestID = %q, want agent prefix", requestID)
	}

	if got := firstNonEmptyGatewayString(" ", "\tvalue\t", "later"); got != "value" {
		t.Fatalf("firstNonEmptyGatewayString = %q, want value", got)
	}
	if got := intFromAnyGateway(int64(9)); got != 9 {
		t.Fatalf("intFromAnyGateway(int64) = %d, want 9", got)
	}
	if got := intFromAnyGateway("9"); got != 0 {
		t.Fatalf("intFromAnyGateway(string) = %d, want 0", got)
	}
}
