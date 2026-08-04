package gateway

import (
	"reflect"
	"testing"
)

func TestNormalizeToolOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content any
		want    any
	}{
		{
			name:    "nil becomes empty string",
			content: nil,
			want:    "",
		},
		{
			name:    "string passes through",
			content: " raw output ",
			want:    " raw output ",
		},
		{
			name:    "structured output becomes JSON string",
			content: map[string]any{"count": float64(2), "ok": true},
			want:    `{"count":2,"ok":true}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeToolOutput(tt.content); got != tt.want {
				t.Fatalf("normalizeToolOutput() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeImageURL(t *testing.T) {
	t.Parallel()

	original := map[string]any{"detail": "high"}
	tests := []struct {
		name  string
		value any
		want  any
	}{
		{
			name:  "string passes through",
			value: "https://example.com/image.png",
			want:  "https://example.com/image.png",
		},
		{
			name:  "map url is extracted and trimmed",
			value: map[string]any{"url": " https://example.com/image.png ", "detail": "low"},
			want:  "https://example.com/image.png",
		},
		{
			name:  "map without url passes through",
			value: original,
			want:  original,
		},
		{
			name:  "other value passes through",
			value: 123,
			want:  123,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeImageURL(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeImageURL() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
