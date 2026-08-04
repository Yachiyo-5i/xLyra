package store

import (
	"errors"
	"testing"
)

func TestDatabaseInitRetryableOnlyRetriesPingFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		want bool
	}{
		"nil":                  {err: nil, want: false},
		"maintenance ping":     {err: errors.New("ping maintenance database: connect: connection refused"), want: true},
		"target ping":          {err: errors.New("ping target database: connect: connection refused"), want: true},
		"schema incompatible":  {err: errors.New("database has partial or incompatible schema"), want: false},
		"database create fail": {err: errors.New("create database \"xlyra\": permission denied"), want: false},
	}

	for name, tc := range cases {
		if got := databaseInitRetryable(tc.err); got != tc.want {
			t.Fatalf("%s: expected %v, got %v", name, tc.want, got)
		}
	}
}
