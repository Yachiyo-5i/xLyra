package jsplugin

import (
	"sync"
	"time"
)

const (
	probeFailureTripCount = 20
	protocolErrorWindow   = 5 * time.Minute
	protocolMinCalls      = 50
	protocolErrorRate     = 0.5
	protocolInterruptWindow = time.Minute
	protocolInterruptTrip = 10
)

type breakerKey struct {
	pluginID string
	version  string
}

// Breaker tracks failures per plugin version for automatic disable.
type Breaker struct {
	mu       sync.Mutex
	probe    map[breakerKey]int
	protocol map[breakerKey]*protocolBreakerState
	metrics  map[breakerKey]*pluginMetricsWindow
	onTrip   func(pluginID, version string, reason string)
}

// PluginMetricsWindow summarizes recent plugin activity for admin list APIs.
type PluginMetricsWindow struct {
	Calls      int
	Errors     int
	ErrorRate  float64
	WindowEnds time.Time
}

type pluginMetricsWindow struct {
	windowStart time.Time
	calls       int
	errors      int
}

type protocolBreakerState struct {
	windowStart time.Time
	calls       int
	errors      int
	interrupts  []time.Time
}

func NewBreaker(onTrip func(pluginID, version string, reason string)) *Breaker {
	return &Breaker{
		probe:    map[breakerKey]int{},
		protocol: map[breakerKey]*protocolBreakerState{},
		metrics:  map[breakerKey]*pluginMetricsWindow{},
		onTrip:   onTrip,
	}
}

const pluginMetricsWindowDuration = 24 * time.Hour

func (b *Breaker) Metrics(pluginID, version string) PluginMetricsWindow {
	if b == nil {
		return PluginMetricsWindow{}
	}
	key := breakerKey{pluginID: pluginID, version: version}
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.metrics[key]
	if state == nil {
		return PluginMetricsWindow{WindowEnds: time.Now().UTC().Add(pluginMetricsWindowDuration)}
	}
	if time.Since(state.windowStart) > pluginMetricsWindowDuration {
		return PluginMetricsWindow{WindowEnds: time.Now().UTC().Add(pluginMetricsWindowDuration)}
	}
	rate := 0.0
	if state.calls > 0 {
		rate = float64(state.errors) / float64(state.calls)
	}
	return PluginMetricsWindow{
		Calls:      state.calls,
		Errors:     state.errors,
		ErrorRate:  rate,
		WindowEnds: state.windowStart.Add(pluginMetricsWindowDuration).UTC(),
	}
}

func (b *Breaker) recordMetricsLocked(key breakerKey, failed bool, now time.Time) {
	if b == nil {
		return
	}
	state := b.metrics[key]
	if state == nil || now.Sub(state.windowStart) > pluginMetricsWindowDuration {
		state = &pluginMetricsWindow{windowStart: now}
		b.metrics[key] = state
	}
	state.calls++
	if failed {
		state.errors++
	}
}

func (b *Breaker) RecordProbeResult(plugin *Plugin, success bool) {
	if b == nil || plugin == nil || !coldPathKind(plugin.Manifest.Kind) {
		return
	}
	if stringsHasPrefixBuiltin(plugin.Manifest.ID) {
		return
	}
	key := breakerKey{pluginID: plugin.Manifest.ID, version: plugin.Manifest.Version}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recordMetricsLocked(key, !success, now)
	if success {
		b.probe[key] = 0
		return
	}
	b.probe[key]++
	if b.probe[key] >= probeFailureTripCount {
		b.probe[key] = 0
		b.trip(key, "probe_failures")
	}
}

func (b *Breaker) RecordProtocolCall(plugin *Plugin, failed bool, interrupted bool) {
	if b == nil || plugin == nil || !hotPathKind(plugin.Manifest.Kind) {
		return
	}
	if stringsHasPrefixBuiltin(plugin.Manifest.ID) {
		return
	}
	key := breakerKey{pluginID: plugin.Manifest.ID, version: plugin.Manifest.Version}
	now := time.Now()
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.protocol[key]
	if state == nil || now.Sub(state.windowStart) > protocolErrorWindow {
		state = &protocolBreakerState{windowStart: now}
		b.protocol[key] = state
	}
	state.calls++
	if failed {
		state.errors++
	}
	b.recordMetricsLocked(key, failed, now)
	if interrupted {
		state.interrupts = append(state.interrupts, now)
	}
	cutoff := now.Add(-protocolInterruptWindow)
	kept := make([]time.Time, 0, len(state.interrupts))
	for _, at := range state.interrupts {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	state.interrupts = kept
	if len(state.interrupts) >= protocolInterruptTrip {
		b.protocol[key] = nil
		b.trip(key, "protocol_interrupts")
		return
	}
	if state.calls >= protocolMinCalls {
		rate := float64(state.errors) / float64(state.calls)
		if rate > protocolErrorRate {
			b.protocol[key] = nil
			b.trip(key, "protocol_error_rate")
		}
	}
}

func (b *Breaker) trip(key breakerKey, reason string) {
	if b.onTrip != nil {
		b.onTrip(key.pluginID, key.version, reason)
	}
}

func stringsHasPrefixBuiltin(id string) bool {
	return len(id) >= 6 && id[:6] == "xlyra."
}

// coldPathKind reports whether a kind runs in the background, where a plugin
// is stopped only after many failures in a row.
func coldPathKind(kind string) bool {
	spec, ok := lookupKind(kind)
	return ok && !spec.HotPath
}

// hotPathKind reports whether a kind runs while a client request waits, where
// the error rate in a short window decides.
func hotPathKind(kind string) bool {
	spec, ok := lookupKind(kind)
	return ok && spec.HotPath
}
