package inflight

import (
	"testing"
	"time"
)

func TestRegistryPublishesLifecycleAndRemovesTerminalRequest(t *testing.T) {
	registry := NewRegistry()
	registry.terminalRetention = 5 * time.Millisecond
	events, unsubscribe := registry.Subscribe()
	defer unsubscribe()

	registry.Start(Request{RequestID: "req-1", APIKeyName: "Client", ModelKey: "gpt-test", ModelProvider: "openai", Stream: true})
	assertEvent(t, events, "upsert", "req-1", PhaseAccepted)

	registry.Route("req-1", Route{SiteID: "site-1", SiteName: "Primary", SiteType: "openai", Attempt: 1})
	event := assertEvent(t, events, "upsert", "req-1", PhaseRouted)
	if event.Request == nil || event.Request.SiteName != "Primary" || event.Request.Attempt != 1 {
		t.Fatalf("route event = %#v", event)
	}

	registry.Responding("req-1")
	assertEvent(t, events, "upsert", "req-1", PhaseResponding)
	registry.Finish("req-1", PhaseCompleted)
	assertEvent(t, events, "upsert", "req-1", PhaseCompleted)

	time.Sleep(registry.terminalRetention + 20*time.Millisecond)
	assertEvent(t, events, "remove", "req-1", "")
	if got := registry.Snapshot().Requests; len(got) != 0 {
		t.Fatalf("snapshot after terminal retention = %#v", got)
	}
}

func TestRegistryPreservesProvidedRequestStartTime(t *testing.T) {
	registry := NewRegistry()
	startedAt := time.Date(2026, 8, 17, 8, 0, 0, 123456789, time.UTC)

	registry.Start(Request{RequestID: "req-start", StartedAt: startedAt})

	requests := registry.Snapshot().Requests
	if len(requests) != 1 || !requests[0].StartedAt.Equal(startedAt) {
		t.Fatalf("request start time = %#v, want %s", requests, startedAt)
	}
}

func TestRegistryFinishIsIdempotentAndSnapshotIsSorted(t *testing.T) {
	registry := NewRegistry()
	registry.Start(Request{RequestID: "earlier"})
	time.Sleep(time.Millisecond)
	registry.Start(Request{RequestID: "later"})
	registry.Finish("earlier", PhaseFailed)
	registry.Finish("earlier", PhaseCompleted)

	requests := registry.Snapshot().Requests
	if len(requests) != 2 || requests[0].RequestID != "earlier" || requests[1].RequestID != "later" {
		t.Fatalf("sorted snapshot = %#v", requests)
	}
	if requests[0].Phase != PhaseFailed {
		t.Fatalf("terminal phase changed after duplicate finish = %q", requests[0].Phase)
	}
}

func TestRegistryAccumulatesAndPublishesTokens(t *testing.T) {
	registry := NewRegistry()
	events, unsubscribe := registry.Subscribe()
	defer unsubscribe()
	registry.Start(Request{RequestID: "req-usage", APIKeyID: "key-1", APIKeyName: "Client key"})
	assertEvent(t, events, "upsert", "req-usage", PhaseAccepted)
	registry.Route("req-usage", Route{SiteID: "site-1", SiteName: "Primary site", Attempt: 1})
	assertEvent(t, events, "upsert", "req-usage", PhaseRouted)

	registry.AddTokens("req-usage", 1200)
	event := assertEvent(t, events, "usage", "req-usage", "")
	if event.Tokens != 1200 || event.TotalTokens != 1200 {
		t.Fatalf("usage event tokens = %d, total_tokens = %d", event.Tokens, event.TotalTokens)
	}
	if event.DownstreamUsage == nil || event.DownstreamUsage.ID != "key-1" || event.DownstreamUsage.TotalTokens != 1200 {
		t.Fatalf("downstream usage event = %#v", event.DownstreamUsage)
	}
	if event.UpstreamUsage == nil || event.UpstreamUsage.ID != "site-1" || event.UpstreamUsage.TotalTokens != 1200 {
		t.Fatalf("upstream usage event = %#v", event.UpstreamUsage)
	}

	registry.AddTokens("req-usage", 300)
	event = assertEvent(t, events, "usage", "req-usage", "")
	if event.Tokens != 300 || event.TotalTokens != 1500 {
		t.Fatalf("usage event tokens = %d, total_tokens = %d", event.Tokens, event.TotalTokens)
	}
	if snapshot := registry.Snapshot(); snapshot.TotalTokens != 1500 || len(snapshot.DownstreamUsage) != 1 || snapshot.DownstreamUsage[0].TotalTokens != 1500 || len(snapshot.UpstreamUsage) != 1 || snapshot.UpstreamUsage[0].TotalTokens != 1500 {
		t.Fatalf("snapshot usage = %#v", snapshot)
	}
}

func assertEvent(t *testing.T, events <-chan Event, eventType string, requestID string, phase Phase) Event {
	t.Helper()
	select {
	case event := <-events:
		if event.Type != eventType || event.RequestID != requestID {
			t.Fatalf("event = %#v, want type=%q request_id=%q", event, eventType, requestID)
		}
		if phase != "" && (event.Request == nil || event.Request.Phase != phase) {
			t.Fatalf("event phase = %#v, want %q", event.Request, phase)
		}
		return event
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s event", eventType)
		return Event{}
	}
}
