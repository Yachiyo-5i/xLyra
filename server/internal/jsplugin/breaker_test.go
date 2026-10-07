package jsplugin

import (
	"testing"
)

func TestBreakerTripsProbeFailures(t *testing.T) {
	t.Parallel()
	var trips int
	breaker := NewBreaker(func(pluginID, version, reason string) {
		trips++
		if pluginID != "com.example.probe" || reason != "probe_failures" {
			t.Fatalf("trip = %s %s %s", pluginID, version, reason)
		}
	})
	plugin := &Plugin{Manifest: Manifest{ID: "com.example.probe", Version: "1.0.0", Kind: KindQuotaProbe}}
	for i := 0; i < probeFailureTripCount; i++ {
		breaker.RecordProbeResult(plugin, false)
	}
	if trips != 1 {
		t.Fatalf("trips = %d, want 1", trips)
	}
	breaker.RecordProbeResult(plugin, true)
	for i := 0; i < probeFailureTripCount-1; i++ {
		breaker.RecordProbeResult(plugin, false)
	}
	if trips != 1 {
		t.Fatalf("success should reset counter, trips = %d", trips)
	}
}

func TestBreakerIgnoresBuiltinPlugins(t *testing.T) {
	t.Parallel()
	breaker := NewBreaker(func(pluginID, version, reason string) {
		t.Fatal("builtin plugins should not trip breaker")
	})
	plugin := &Plugin{Manifest: Manifest{ID: "xlyra.quota.kimi", Version: "1.0.0", Kind: KindQuotaProbe}}
	for i := 0; i < probeFailureTripCount+5; i++ {
		breaker.RecordProbeResult(plugin, false)
	}
}
