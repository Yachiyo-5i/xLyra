package catalog

import (
	"net/http"
	"testing"
)

func assertStringSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

type catalogSyncRoundTripFunc func(*http.Request) (*http.Response, error)

func (f catalogSyncRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
