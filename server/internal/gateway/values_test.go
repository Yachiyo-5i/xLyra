package gateway

import (
	"database/sql"
	"testing"
)

func TestStringValueFromNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    sql.NullString
		fallback string
		want     string
	}{
		{
			name:     "invalid uses fallback",
			value:    sql.NullString{String: "upstream", Valid: false},
			fallback: "fallback",
			want:     "fallback",
		},
		{
			name:     "blank valid uses fallback",
			value:    sql.NullString{String: "  \n\t ", Valid: true},
			fallback: "fallback",
			want:     "fallback",
		},
		{
			name:     "valid value is trimmed",
			value:    sql.NullString{String: "  upstream  ", Valid: true},
			fallback: "fallback",
			want:     "upstream",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := stringValueFromNull(tt.value, tt.fallback); got != tt.want {
				t.Fatalf("stringValueFromNull() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEndpointPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{
			name:   "absolute url keeps path only",
			rawURL: " https://api.example.com/v1/chat/completions?model=gpt ",
			want:   "/v1/chat/completions",
		},
		{
			name:   "relative url keeps path only",
			rawURL: "/v1/responses?stream=true",
			want:   "/v1/responses",
		},
		{
			name:   "missing path returns empty",
			rawURL: "https://api.example.com",
			want:   "",
		},
		{
			name:   "invalid url returns empty",
			rawURL: "://",
			want:   "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := endpointPath(tt.rawURL); got != tt.want {
				t.Fatalf("endpointPath(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestZeroIntToNil(t *testing.T) {
	t.Parallel()

	if got := zeroIntToNil(0); got != nil {
		t.Fatalf("zeroIntToNil(0) = %#v, want nil", got)
	}
	if got := zeroIntToNil(-1); got != nil {
		t.Fatalf("zeroIntToNil(-1) = %#v, want nil", got)
	}
	if got := zeroIntToNil(7); got != 7 {
		t.Fatalf("zeroIntToNil(7) = %#v, want 7", got)
	}
}

func TestInt64PtrValue(t *testing.T) {
	t.Parallel()

	if got := int64PtrValue(nil); got != nil {
		t.Fatalf("int64PtrValue(nil) = %#v, want nil", got)
	}

	value := int64(42)
	if got := int64PtrValue(&value); got != int64(42) {
		t.Fatalf("int64PtrValue(&42) = %#v, want int64(42)", got)
	}
}
