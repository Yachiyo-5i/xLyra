package store

import (
	"encoding/json"
	"testing"
)

func TestSiteModelManualRecognizesManualMarkers(t *testing.T) {
	t.Parallel()

	if !SiteModelManual(SiteModel{Capabilities: JSON([]byte(`{"manual":true}`))}) {
		t.Fatal("expected manual boolean marker to be recognized")
	}
	if !SiteModelManual(SiteModel{Capabilities: JSON([]byte(`{"source":"manual"}`))}) {
		t.Fatal("expected manual source marker to be recognized")
	}
	if SiteModelManual(SiteModel{Capabilities: JSON([]byte(`{"source":"newapi"}`))}) {
		t.Fatal("expected non-manual source to be ignored")
	}
	if SiteModelManual(SiteModel{Capabilities: JSON([]byte(`{`))}) {
		t.Fatal("expected invalid capabilities JSON to be ignored")
	}
	if SiteModelManual(SiteModel{Capabilities: JSON([]byte(`{"manual":false,"source":"newapi"}`))}) {
		t.Fatal("expected explicit false manual marker with non-manual source to be ignored")
	}
	if !SiteModelManual(SiteModel{Capabilities: JSON([]byte(`{"source":" Manual "}`))}) {
		t.Fatal("expected trimmed mixed-case manual source marker to be recognized")
	}
}

func TestCanonicalModelCreationMarkers(t *testing.T) {
	t.Parallel()

	manual := CanonicalModel{Capabilities: JSON([]byte(`{"manual_created":true,"source":"manual_create"}`))}
	if !CanonicalModelManualCreated(manual) {
		t.Fatal("expected manual canonical model marker to be recognized")
	}
	if canonicalModelAutoCreated(manual) {
		t.Fatal("manual canonical model should not be treated as auto-created")
	}

	auto := CanonicalModel{Capabilities: JSON([]byte(`{"auto_created":true,"source":"catalog_match"}`))}
	if CanonicalModelManualCreated(auto) {
		t.Fatal("auto-created canonical model should not be treated as manual")
	}
	if !canonicalModelAutoCreated(auto) {
		t.Fatal("expected auto-created canonical model marker to be recognized")
	}
}

func TestMergeCanonicalModelCapabilitiesPreservesManualCreatedMarker(t *testing.T) {
	t.Parallel()

	existing := CanonicalModel{Capabilities: JSON([]byte(`{"manual_created":true,"source":"manual_create"}`))}
	merged := mergeCanonicalModelCapabilities(existing, JSON([]byte(`{}`)))
	got := CanonicalModel{Capabilities: merged}

	if !CanonicalModelManualCreated(got) {
		t.Fatalf("expected merged capabilities to preserve manual marker, got %s", string(merged))
	}
	if canonicalModelAutoCreated(got) {
		t.Fatalf("expected merged capabilities to clear auto-created marker, got %s", string(merged))
	}

	preserved := mergeCanonicalModelCapabilities(
		CanonicalModel{Capabilities: JSON([]byte(`{"source":"catalog_match","note":"kept"}`))},
		JSON([]byte(`{"auto_created":true}`)),
	)
	var preservedValues map[string]any
	if err := json.Unmarshal(preserved, &preservedValues); err != nil {
		t.Fatalf("decode preserved capabilities: %v", err)
	}
	if preservedValues["source"] != "catalog_match" || preservedValues["auto_created"] != true {
		t.Fatalf("unexpected preserved capabilities merge: %s", preserved)
	}
}

func TestCanonicalModelCapabilitiesIgnoresInvalidJSON(t *testing.T) {
	t.Parallel()

	if got := canonicalModelCapabilities(CanonicalModel{}); len(got) != 0 {
		t.Fatalf("empty capabilities = %#v, want empty map", got)
	}
	if got := canonicalModelCapabilities(CanonicalModel{Capabilities: JSON([]byte(`{`))}); len(got) != 0 {
		t.Fatalf("invalid capabilities = %#v, want empty map", got)
	}
}
