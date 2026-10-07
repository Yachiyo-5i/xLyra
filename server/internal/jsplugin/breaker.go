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
	mu      sync.Mutex
	probe   map[breakerKey]int
	protocol map[breakerKey]*protocolBreakerState
	onTrip  func(pluginID, version string, reason string)
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
		onTrip:   onTrip,
	}
}

func (b *Breaker) RecordProbeResult(plugin *Plugin, success bool) {
	if b == nil || plugin == nil || plugin.Manifest.Kind != KindQuotaProbe {
		return
	}
	if stringsHasPrefixBuiltin(plugin.Manifest.ID) {
		return
	}
	key := breakerKey{pluginID: plugin.Manifest.ID, version: plugin.Manifest.Version}
	b.mu.Lock()
	defer b.mu.Unlock()
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
	if b == nil || plugin == nil || plugin.Manifest.Kind != KindProtocol {
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
