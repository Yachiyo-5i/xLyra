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

func TestBreakerSiteProviderRecordsWithoutTrip(t *testing.T) {
	t.Parallel()
	var trips []string
	breaker := NewBreaker(func(pluginID, version, reason string) {
		trips = append(trips, pluginID+" "+reason)
	})
	provider := &Plugin{Manifest: Manifest{ID: "com.example.provider", Version: "1.0.0", Kind: KindSiteProvider}}
	for i := 0; i < probeFailureTripCount+5; i++ {
		breaker.RecordProbeResult(provider, false)
	}
	if len(trips) != 0 {
		t.Fatalf("site_provider trips = %v", trips)
	}
	metrics := breaker.Metrics(provider.Manifest.ID, provider.Manifest.Version)
	if metrics.Calls != probeFailureTripCount+5 || metrics.Errors != probeFailureTripCount+5 {
		t.Fatalf("metrics = %+v", metrics)
	}
	breaker.RecordProbeResult(provider, true)
	metrics = breaker.Metrics(provider.Manifest.ID, provider.Manifest.Version)
	if metrics.Calls != probeFailureTripCount+6 || metrics.Errors != probeFailureTripCount+5 {
		t.Fatalf("metrics after success = %+v", metrics)
	}

	quota := &Plugin{Manifest: Manifest{ID: "com.example.probe", Version: "1.0.0", Kind: KindQuotaProbe}}
	for i := 0; i < probeFailureTripCount; i++ {
		breaker.RecordProbeResult(quota, false)
	}
	if len(trips) != 1 || trips[0] != "com.example.probe probe_failures" {
		t.Fatalf("quota trips = %v", trips)
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
