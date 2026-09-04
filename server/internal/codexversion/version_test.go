package codexversion

import (
	"context"
	"errors"
	"testing"
)

func TestVersionDefaultsBeforeRefresh(t *testing.T) {
	restore := WithFetcher(func(context.Context) (string, error) { return "0.999.0", nil })
	defer restore()

	if got := Version(); got != DefaultVersion {
		t.Fatalf("Version() = %q, want default %q", got, DefaultVersion)
	}
}

func TestRefreshStoresLatest(t *testing.T) {
	restore := WithFetcher(func(context.Context) (string, error) { return "0.154.0", nil })
	defer restore()

	if err := Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	if got := Version(); got != "0.154.0" {
		t.Fatalf("Version() = %q, want 0.154.0", got)
	}
}

func TestRefreshKeepsCurrentOnError(t *testing.T) {
	restore := WithFetcher(func(context.Context) (string, error) { return "", errors.New("registry down") })
	defer restore()

	if err := Refresh(context.Background()); err == nil {
		t.Fatal("Refresh returned nil error, want error")
	}
	if got := Version(); got != DefaultVersion {
		t.Fatalf("Version() = %q, want prior default preserved", got)
	}
}

func TestRefreshRejectsInvalidVersion(t *testing.T) {
	restore := WithFetcher(func(context.Context) (string, error) { return "not-a-version", nil })
	defer restore()

	if err := Refresh(context.Background()); err == nil {
		t.Fatal("Refresh returned nil error, want error for invalid version")
	}
	if got := Version(); got != DefaultVersion {
		t.Fatalf("Version() = %q, want prior default preserved", got)
	}
}

func TestNormalizeVersionStripsRustPrefixAndWhitespace(t *testing.T) {
	cases := map[string]string{
		" 0.153.3 ":   "0.153.3",
		"rust-0.153.3": "0.153.3",
		"v0.153.3":     "",
		"0.153":        "",
		"":             "",
		"beta":         "",
	}
	for input, want := range cases {
		if got := normalizeVersion(input); got != want {
			t.Fatalf("normalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWithFetcherRestoresPrevious(t *testing.T) {
	baseline := WithFetcher(func(context.Context) (string, error) { return "0.150.0", nil })
	if err := Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh failed: %v", err)
	}

	restore := WithFetcher(func(context.Context) (string, error) { return "0.99.99", nil })
	if err := Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh in override failed: %v", err)
	}
	restore()
	baseline()

	if got := Version(); got != DefaultVersion {
		t.Fatalf("Version() after restore = %q, want default %q", got, DefaultVersion)
	}
}
