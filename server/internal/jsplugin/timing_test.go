package jsplugin

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"
)

// Run: go test ./internal/jsplugin/ -run MeasureBuiltinPluginTiming -v -count=1
func TestMeasureBuiltinPluginTiming(t *testing.T) {
	if os.Getenv("JSPLUGIN_TIMING") != "1" {
		t.Skip("set JSPLUGIN_TIMING=1 to print builtin plugin timings")
	}
	registry, err := LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var ids []string
	for _, p := range registry.Plugins() {
		ids = append(ids, p.Manifest.ID)
	}
	sort.Strings(ids)

	t.Log("builtin selftest duration (all package fixtures, one run each)")
	for _, id := range ids {
		plugin, ok := registry.ByPluginID(id)
		if !ok {
			continue
		}
		name := plugin.Manifest.ID
		kind := plugin.Manifest.Kind
		fixtures := len(plugin.Fixtures())

		selfStart := time.Now()
		if err := plugin.SelfTest(ctx); err != nil {
			t.Fatalf("%s selftest: %v", name, err)
		}
		selfDur := time.Since(selfStart)
		perFixture := time.Duration(0)
		if fixtures > 0 {
			perFixture = selfDur / time.Duration(fixtures)
		}
		t.Logf("%-40s kind=%-12s fixtures=%2d  total=%s  ~per_fixture=%s",
			name, kind, fixtures, selfDur.Round(time.Microsecond), perFixture.Round(time.Microsecond))
	}
}

func BenchmarkBuiltinQuotaSelfTest(b *testing.B) {
	registry, err := LoadBuiltins()
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for _, probeType := range []string{"kimi", "glm", "moonshot", "deepseek", "sub2api", "newapi", "xlyra"} {
		plugin, ok := registry.ByProbeType(probeType)
		if !ok {
			b.Fatalf("missing %s", probeType)
		}
		b.Run(probeType, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := plugin.SelfTest(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkBuiltinSystemoneSelfTest(b *testing.B) {
	registry, err := LoadBuiltins()
	if err != nil {
		b.Fatal(err)
	}
	plugin, ok := registry.ByProtocolName("typesafe_systemone")
	if !ok {
		b.Fatal("missing systemone")
	}
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := plugin.SelfTest(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

