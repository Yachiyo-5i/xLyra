package jsplugin

import (
	"errors"
	"testing"
)

func TestDecodeEntryKindAndName(t *testing.T) {
	entry, err := decodeEntry(map[string]any{"label": "pack", "kind": "package", "name": "月度包"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != "package" || entry.Name != "月度包" || entry.Label != "pack" {
		t.Fatalf("entry = %+v", entry)
	}
	for _, kind := range []string{"balance", "window", "package"} {
		entry, err := decodeEntry(map[string]any{"label": "x", "kind": kind, "name": kind}, 0)
		if err != nil || entry.Kind != kind || entry.Name != kind {
			t.Fatalf("kind %s: entry=%+v err=%v", kind, entry, err)
		}
	}

	entry, err = decodeEntry(map[string]any{"label": "x"}, 0)
	if err != nil || entry.Kind != "" || entry.Name != "" {
		t.Fatalf("missing: %+v %v", entry, err)
	}
	entry, err = decodeEntry(map[string]any{"label": "x", "kind": "", "name": ""}, 0)
	if err != nil || entry.Kind != "" || entry.Name != "" {
		t.Fatalf("empty: %+v %v", entry, err)
	}

	_, err = decodeEntry(map[string]any{"label": "x", "kind": "foo"}, 0)
	var call *CallError
	if err == nil || !errors.As(err, &call) || call.Kind != KindShape {
		t.Fatalf("foo: %v", err)
	}
}

func TestDecodeEntryExpiresAt(t *testing.T) {
	entry, err := decodeEntry(map[string]any{
		"label":     "x",
		"resetAt":   "2026-02-01T00:00:00Z",
		"expiresAt": "2026-01-31T23:59:59+08:00",
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ExpiresAt != "2026-01-31T23:59:59+08:00" || entry.ResetAt != "2026-02-01T00:00:00Z" {
		t.Fatalf("entry = %+v", entry)
	}

	entry, err = decodeEntry(map[string]any{"label": "x"}, 0)
	if err != nil || entry.ExpiresAt != "" {
		t.Fatalf("missing: %+v %v", entry, err)
	}
	entry, err = decodeEntry(map[string]any{"label": "x", "expiresAt": ""}, 0)
	if err != nil || entry.ExpiresAt != "" {
		t.Fatalf("empty: %+v %v", entry, err)
	}

	_, err = decodeEntry(map[string]any{"label": "x", "expiresAt": 1}, 0)
	var call *CallError
	if err == nil || !errors.As(err, &call) || call.Kind != KindShape {
		t.Fatalf("non-string: %v", err)
	}
}

func TestResultMapWritesEntryExpiresAtWhenSet(t *testing.T) {
	got := resultMap(&ProbeResult{
		Kind: "balance",
		Entries: []ProbeEntry{
			{Label: "a", ExpiresAt: "2026-01-31T23:59:59Z"},
			{Label: "b"},
		},
	})
	entries, _ := got["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", got["entries"])
	}
	first, _ := entries[0].(map[string]any)
	if first["expiresAt"] != "2026-01-31T23:59:59Z" {
		t.Fatalf("first = %#v", first)
	}
	second, _ := entries[1].(map[string]any)
	if _, ok := second["expiresAt"]; ok {
		t.Fatalf("empty expiresAt was written: %#v", second)
	}
}
