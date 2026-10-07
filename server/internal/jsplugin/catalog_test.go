package jsplugin

import (
	"context"
	"testing"
)

func TestBuildGenerationIncrementsRegistry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	first, err := BuildGeneration(ctx, 1, nil, nil)
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	if first.Gen != 1 || first.Registry == nil {
		t.Fatalf("unexpected first snapshot: %#v", first)
	}
	if _, ok := first.Registry.ByProtocolName("typesafe_systemone"); !ok {
		t.Fatal("missing builtin systemone protocol")
	}
	catalog := &Catalog{}
	catalog.Replace(first)
	second, err := BuildGeneration(ctx, 2, nil, map[string]string{"demo": "missing"})
	if err != nil {
		t.Fatalf("second generation: %v", err)
	}
	catalog.Replace(second)
	if catalog.Generation() != 2 {
		t.Fatalf("generation = %d, want 2", catalog.Generation())
	}
}
