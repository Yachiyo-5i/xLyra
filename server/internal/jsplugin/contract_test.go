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
