package gateway

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestProtocolNumericHelpersCoverTypedBranches(t *testing.T) {
	t.Parallel()

	if got := zeroInt64ToNil(-1); got != nil {
		t.Fatalf("zeroInt64ToNil(-1) = %#v, want nil", got)
	}
	if got := zeroInt64ToNil(0); got != nil {
		t.Fatalf("zeroInt64ToNil(0) = %#v, want nil", got)
	}
	if got := zeroInt64ToNil(9); got != int64(9) {
		t.Fatalf("zeroInt64ToNil(9) = %#v, want int64(9)", got)
	}

	int64Cases := []struct {
		name  string
		value any
		want  int64
		ok    bool
	}{
		{name: "int32", value: int32(7), want: 7, ok: true},
		{name: "float32", value: float32(8), want: 8, ok: true},
		{name: "json number", value: json.Number("10"), want: 10, ok: true},
		{name: "trimmed string", value: " 11 ", want: 11, ok: true},
		{name: "bad string", value: "soon", ok: false},
		{name: "unsupported", value: true, ok: false},
	}
	for _, tt := range int64Cases {
		tt := tt
		t.Run("int64FromAny "+tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := int64FromAny(tt.value)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("int64FromAny(%#v) = %d/%v, want %d/%v", tt.value, got, ok, tt.want, tt.ok)
			}
		})
	}

	numericCases := []struct {
		name  string
		value any
		want  float64
	}{
		{name: "float32", value: float32(1.5), want: 1.5},
		{name: "int64", value: int64(7), want: 7},
		{name: "json number", value: json.Number("8.25"), want: 8.25},
		{name: "bad json number", value: json.Number("bad"), want: 0},
		{name: "unsupported", value: "9", want: 0},
	}
	for _, tt := range numericCases {
		tt := tt
		t.Run("numericMetadataValue "+tt.name, func(t *testing.T) {
			t.Parallel()

			if got := numericMetadataValue(tt.value); got != tt.want {
				t.Fatalf("numericMetadataValue(%#v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseCodexRetryAfterHandlesResetFormats(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0)

	duration, seconds := parseCodexRetryAfter([]byte(`{"error":{"resets_at":1060}}`), now)
	if duration != time.Minute || seconds != 60 {
		t.Fatalf("resets_at retry-after = %s/%d, want 1m/60", duration, seconds)
	}

	duration, seconds = parseCodexRetryAfter([]byte(`{"error":{"resets_at":900,"resets_in_seconds":"45"}}`), now)
	if duration != 45*time.Second || seconds != 45 {
		t.Fatalf("resets_in_seconds retry-after = %s/%d, want 45s/45", duration, seconds)
	}

	for _, body := range [][]byte{
		nil,
		[]byte(`not json`),
		[]byte(`{"error":{"resets_at":900}}`),
		[]byte(`{"error":{"resets_in_seconds":"soon"}}`),
	} {
		duration, seconds = parseCodexRetryAfter(body, now)
		if duration != 0 || seconds != 0 {
			t.Fatalf("parseCodexRetryAfter(%q) = %s/%d, want 0/0", body, duration, seconds)
		}
	}
}

func TestAntigravityImagePartFromCanonicalDataURLBranches(t *testing.T) {
	t.Parallel()

	got := antigravityImagePartFromCanonical(canonicalContentPart{
		ImageURL: "data:image/png;base64,abc123",
	})
	want := map[string]any{
		"inlineData": map[string]any{
			"mimeType": "image/png",
			"data":     "abc123",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("antigravityImagePartFromCanonical() = %#v, want %#v", got, want)
	}

	got = antigravityImagePartFromCanonical(canonicalContentPart{
		Raw: map[string]any{"image_url": map[string]any{"url": " data:image/jpeg;base64,xyz "}},
	})
	want = map[string]any{
		"inlineData": map[string]any{
			"mimeType": "image/jpeg",
			"data":     "xyz",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("raw fallback image part = %#v, want %#v", got, want)
	}

	for _, part := range []canonicalContentPart{
		{ImageURL: "https://example.com/image.png"},
		{ImageURL: "data:image/png"},
		{ImageURL: "data:,abc"},
	} {
		if got := antigravityImagePartFromCanonical(part); got != nil {
			t.Fatalf("antigravityImagePartFromCanonical(%#v) = %#v, want nil", part, got)
		}
	}
}

func TestAntigravityContentTextAndSchemaHelpers(t *testing.T) {
	t.Parallel()

	parts := []canonicalContentPart{
		{Type: "input_text", Text: "hello"},
		{Type: "input_image", ImageURL: "data:image/png;base64,abc"},
		{Type: "output_text", Text: "  "},
		{Type: "output_text", Text: "world"},
	}
	if got := antigravityCanonicalContentText(parts, "ignored"); got != "hello\nworld" {
		t.Fatalf("antigravityCanonicalContentText(parts) = %q, want hello\\nworld", got)
	}
	if got := antigravityCanonicalContentText(nil, " raw text "); got != " raw text " {
		t.Fatalf("antigravityCanonicalContentText(raw string) = %q, want raw string", got)
	}
	if got := antigravityCanonicalContentText(nil, map[string]any{"ok": true}); got != `{"ok":true}` {
		t.Fatalf("antigravityCanonicalContentText(raw map) = %q, want JSON object", got)
	}
	if got := antigravityCanonicalOutputText(nil); got != "" {
		t.Fatalf("antigravityCanonicalOutputText(nil) = %q, want empty", got)
	}

	schemaCases := []struct {
		name  string
		value any
		want  string
	}{
		{name: "string", value: " object ", want: "object"},
		{name: "union skips null", value: []any{"", "null", "integer"}, want: "integer"},
		{name: "union without concrete type", value: []any{"null", ""}, want: ""},
		{name: "unsupported", value: 123, want: ""},
	}
	for _, tt := range schemaCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := antigravitySchemaType(tt.value); got != tt.want {
				t.Fatalf("antigravitySchemaType(%#v) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestStreamErrorMessageFallbackHelpers(t *testing.T) {
	t.Parallel()

	responseError := errorMessageFromResponsesStreamEvent(responsesStreamEvent{
		Response: &responsesResponse{Error: map[string]any{"message": "boom"}},
	})
	if responseError != `{"message":"boom"}` {
		t.Fatalf("response error message = %q, want encoded response error", responseError)
	}

	tests := []struct {
		name  string
		event responsesStreamEvent
		want  string
	}{
		{
			name:  "api error message",
			event: responsesStreamEvent{Type: "response.failed", Error: &responsesAPIError{Message: "failed"}},
			want:  "failed",
		},
		{
			name:  "api error code fallback",
			event: responsesStreamEvent{Type: "response.failed", Error: &responsesAPIError{Code: "rate_limit"}},
			want:  "rate_limit",
		},
		{
			name:  "event type fallback",
			event: responsesStreamEvent{Type: "response.failed"},
			want:  "response.failed",
		},
		{
			name:  "default fallback",
			event: responsesStreamEvent{},
			want:  "upstream stream failed",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := errorMessageFromResponsesStreamEvent(tt.event); got != tt.want {
				t.Fatalf("errorMessageFromResponsesStreamEvent() = %q, want %q", got, tt.want)
			}
		})
	}

	anthropicCases := []struct {
		name  string
		value map[string]any
		want  string
	}{
		{name: "nil", value: nil, want: "anthropic stream returned an error"},
		{name: "message", value: map[string]any{"message": " failed "}, want: "failed"},
		{name: "type fallback", value: map[string]any{"type": " overloaded_error "}, want: "overloaded_error"},
		{name: "default", value: map[string]any{}, want: "anthropic stream returned an error"},
	}
	for _, tt := range anthropicCases {
		tt := tt
		t.Run("anthropic "+tt.name, func(t *testing.T) {
			t.Parallel()

			if got := anthropicStreamErrorMessage(tt.value); got != tt.want {
				t.Fatalf("anthropicStreamErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
