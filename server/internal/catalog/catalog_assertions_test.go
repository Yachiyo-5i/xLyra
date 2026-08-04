package catalog

import (
	"errors"
	"strings"
	"testing"
)

func assertCatalogErrorContains(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want to contain %q", label, err, want)
	}
}

func assertCatalogErrorIs(t *testing.T, label string, err error, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Fatalf("%s error = %v, want %v", label, err, want)
	}
}

func assertCatalogFloat(t *testing.T, values map[string]any, key string, want float64) {
	t.Helper()

	if got := extractFloat(values, key); got != want {
		t.Fatalf("extractFloat(%q) = %v, want %v", key, got, want)
	}
}

func assertCatalogCost(t *testing.T, values map[string]any, key string, want float64, wantValid bool) {
	t.Helper()

	got := extractCost(values, key)
	if got.Valid != wantValid || (wantValid && got.Float64 != want) {
		t.Fatalf("extractCost(%q) = %#v, want valid=%v value=%v", key, got, wantValid, want)
	}
}

func assertCatalogInt(t *testing.T, values map[string]any, key string, want int32, wantValid bool) {
	t.Helper()

	got := extractInt(values, key)
	if got.Valid != wantValid || (wantValid && got.Int32 != want) {
		t.Fatalf("extractInt(%q) = %#v, want valid=%v value=%d", key, got, wantValid, want)
	}
}
