package adapter

import (
	"testing"
	"time"
)

func TestCodexUsageAvailableRequiresEveryReturnedWindow(t *testing.T) {
	cases := []struct {
		name  string
		usage map[string]any
		want  bool
	}{
		{
			name: "both windows available",
			usage: map[string]any{
				"five_hour": map[string]any{"remaining_percent": 40},
				"weekly":    map[string]any{"remaining_percent": 55},
			},
			want: true,
		},
		{
			name: "both present but five_hour exhausted",
			usage: map[string]any{
				"five_hour": map[string]any{"remaining_percent": 0},
				"weekly":    map[string]any{"remaining_percent": 55},
			},
			want: false,
		},
		{
			name: "five_hour dropped by upstream, weekly available",
			usage: map[string]any{
				"weekly": map[string]any{"remaining_percent": 12},
			},
			want: true,
		},
		{
			name: "five_hour dropped by upstream, weekly exhausted",
			usage: map[string]any{
				"weekly": map[string]any{"remaining_percent": 0},
			},
			want: false,
		},
		{
			name:  "no window returned is treated as unavailable",
			usage: map[string]any{"plan_type": "plus"},
			want:  false,
		},
		{
			name:  "error snapshot is unavailable",
			usage: map[string]any{"success": false, "available": false},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CodexUsageAvailable(tc.usage); got != tc.want {
				t.Fatalf("CodexUsageAvailable(%#v) = %v, want %v", tc.usage, got, tc.want)
			}
		})
	}
}

func TestCodexShouldPrimeFiveHour(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cases := []struct {
		name  string
		usage map[string]any
		want  bool
	}{
		{
			name: "idle window with no reset should prime",
			usage: map[string]any{
				"five_hour": map[string]any{"remaining_percent": 100, "reset_at": nil},
			},
			want: true,
		},
		{
			name: "window already counting should not prime",
			usage: map[string]any{
				"five_hour": map[string]any{"remaining_percent": 100, "reset_at": now.Add(time.Hour).Unix()},
			},
			want: false,
		},
		{
			name: "elapsed reset counts as idle",
			usage: map[string]any{
				"five_hour": map[string]any{"remaining_percent": 100, "reset_at": now.Add(-time.Hour).Unix()},
			},
			want: true,
		},
		{
			name: "exhausted window should not prime",
			usage: map[string]any{
				"five_hour": map[string]any{"remaining_percent": 0, "reset_at": nil},
			},
			want: false,
		},
		{
			name: "no five_hour window means nothing to prime",
			usage: map[string]any{
				"weekly": map[string]any{"remaining_percent": 100, "reset_at": nil},
			},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CodexShouldPrimeFiveHour(tc.usage, now); got != tc.want {
				t.Fatalf("CodexShouldPrimeFiveHour(%#v) = %v, want %v", tc.usage, got, tc.want)
			}
		})
	}
}
