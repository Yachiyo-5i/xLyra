package oauth

import (
	"testing"
	"time"
)

func TestOAuthWeeklyResetTransition(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	oldReset := now.Add(-time.Hour)

	tests := []struct {
		name                  string
		previous, current     time.Time
		previousOK, currentOK bool
		want                  bool
	}{
		{name: "first sync", current: now.Add(time.Hour), currentOK: true},
		{name: "upstream reset moved forward", previous: oldReset, current: now.Add(time.Hour), previousOK: true, currentOK: true, want: true},
		{name: "same reset is idempotent", previous: oldReset, current: oldReset, previousOK: true, currentOK: true},
		{name: "invalid previous reset", current: now.Add(time.Hour), currentOK: true},
		{name: "invalid current reset", previous: oldReset, previousOK: true},
		{name: "future previous reset", previous: now.Add(time.Hour), current: now.Add(2 * time.Hour), previousOK: true, currentOK: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := weeklyResetOccurred(test.previous, test.previousOK, test.current, test.currentOK, now); got != test.want {
				t.Fatalf("weeklyResetOccurred() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOAuthQuotaResetAtParsesUnixSecondsAndRFC3339(t *testing.T) {
	want := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		value any
		valid bool
	}{
		{name: "unix seconds", value: want.Unix(), valid: true},
		{name: "rfc3339 string", value: want.Format(time.RFC3339), valid: true},
		{name: "invalid string", value: "not-a-time"},
		{name: "zero", value: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := oauthQuotaResetAt(test.value)
			if ok != test.valid || (ok && !got.Equal(want)) {
				t.Fatalf("oauthQuotaResetAt(%#v) = %s, %v; want %s, %v", test.value, got, ok, want, test.valid)
			}
		})
	}
}
