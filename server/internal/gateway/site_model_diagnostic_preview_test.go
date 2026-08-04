package gateway

import (
	"net/http"
	"strings"
	"testing"
)

func TestSiteModelTestResponsePreviewExtractsCommonResponseShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "output text",
			body: `{"output_text":" ok "}`,
			want: "ok",
		},
		{
			name: "openai chat",
			body: `{"choices":[{"message":{"content":[{"type":"text","text":"hello chat"}]}}]}`,
			want: "hello chat",
		},
		{
			name: "responses",
			body: `{"output":[{"content":[{"output_text":"hello responses"}]}]}`,
			want: "hello responses",
		},
		{
			name: "anthropic messages",
			body: `{"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}`,
			want: "first\nsecond",
		},
		{
			name: "compact json fallback",
			body: `{"z":2,"a":1}`,
			want: `{"a":1,"z":2}`,
		},
		{
			name: "raw fallback",
			body: `not-json`,
			want: "not-json",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := siteModelTestResponsePreview([]byte(tt.body)); got != tt.want {
				t.Fatalf("preview = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSiteModelTestResponsePreviewTruncatesAndHandlesEmpty(t *testing.T) {
	t.Parallel()

	if got := siteModelTestResponsePreview(nil); got != "" {
		t.Fatalf("empty preview = %q, want empty", got)
	}

	long := strings.Repeat("x", 600)
	got := siteModelTestResponsePreview([]byte(long))
	if len(got) != 500 {
		t.Fatalf("truncated preview length = %d, want 500", len(got))
	}
	if got != strings.Repeat("x", 500) {
		t.Fatalf("unexpected truncated preview")
	}
}

func TestEmptyStringPointerTrimsBlankValues(t *testing.T) {
	t.Parallel()

	if got := emptyStringPointer(" \t "); got != nil {
		t.Fatalf("blank value pointer = %#v, want nil", got)
	}
	got := emptyStringPointer(" value ")
	if got == nil || *got != "value" {
		t.Fatalf("pointer = %#v, want trimmed value", got)
	}
}

func TestDiscardResponseWriterImplementsHeaderAndWrites(t *testing.T) {
	t.Parallel()

	writer := &discardResponseWriter{}
	writer.Header().Set("X-Test", "ok")
	n, err := writer.Write([]byte("discarded"))
	if err != nil {
		t.Fatalf("write returned error: %v", err)
	}
	if n != len("discarded") {
		t.Fatalf("write length = %d, want %d", n, len("discarded"))
	}
	writer.WriteHeader(http.StatusTeapot)
	if got := writer.Header().Get("X-Test"); got != "ok" {
		t.Fatalf("header = %q, want ok", got)
	}
}
