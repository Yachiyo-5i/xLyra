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

	registry.AddTokens("req-usage", TokenAmount{Total: 1200, Input: 900, Output: 300, Cached: 400})
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

	registry.AddTokens("req-usage", TokenAmount{Total: 300, Input: 0, Output: 300, Cached: 0})
	event = assertEvent(t, events, "usage", "req-usage", "")
	if event.Tokens != 300 || event.TotalTokens != 1500 {
		t.Fatalf("usage event tokens = %d, total_tokens = %d", event.Tokens, event.TotalTokens)
	}
	if snapshot := registry.Snapshot(); snapshot.TotalTokens != 1500 || len(snapshot.DownstreamUsage) != 1 || snapshot.DownstreamUsage[0].TotalTokens != 1500 || len(snapshot.UpstreamUsage) != 1 || snapshot.UpstreamUsage[0].TotalTokens != 1500 {
		t.Fatalf("snapshot usage = %#v", snapshot)
	}
}

func TestRegistryAccumulatesUsageCellsByKeySiteModel(t *testing.T) {
	registry := NewRegistry()
	events, unsubscribe := registry.Subscribe()
	defer unsubscribe()

	registry.Start(Request{RequestID: "req-a", APIKeyID: "key-1", APIKeyName: "Client A", ModelKey: "gpt-5.4", ModelProvider: "openai"})
	assertEvent(t, events, "upsert", "req-a", PhaseAccepted)
	registry.Route("req-a", Route{SiteID: "site-1", SiteName: "Primary", Attempt: 1})
	assertEvent(t, events, "upsert", "req-a", PhaseRouted)
	registry.AddTokens("req-a", TokenAmount{Total: 1000, Input: 800, Output: 200, Cached: 300})
	first := assertEvent(t, events, "usage", "req-a", "")
	if first.UsageCell == nil || first.UsageCell.APIKeyID != "key-1" || first.UsageCell.SiteID != "site-1" || first.UsageCell.ModelKey != "gpt-5.4" || first.UsageCell.TotalTokens != 1000 || first.UsageCell.InputTokens != 800 || first.UsageCell.OutputTokens != 200 || first.UsageCell.CachedTokens != 300 {
		t.Fatalf("first usage cell = %#v", first.UsageCell)
	}

	registry.AddTokens("req-a", TokenAmount{Total: 200, Input: 50, Output: 150, Cached: 10})
	second := assertEvent(t, events, "usage", "req-a", "")
	if second.UsageCell == nil || second.UsageCell.TotalTokens != 1200 || second.UsageCell.InputTokens != 850 || second.UsageCell.OutputTokens != 350 || second.UsageCell.CachedTokens != 310 {
		t.Fatalf("same-cell increment = %#v", second.UsageCell)
	}

	registry.Start(Request{RequestID: "req-b", APIKeyID: "key-2", APIKeyName: "Client B", ModelKey: "claude-sonnet-4.6", ModelProvider: "anthropic"})
	assertEvent(t, events, "upsert", "req-b", PhaseAccepted)
	registry.Route("req-b", Route{SiteID: "site-2", SiteName: "Secondary", Attempt: 1})
	assertEvent(t, events, "upsert", "req-b", PhaseRouted)
	registry.AddTokens("req-b", TokenAmount{Total: 400, Input: 280, Output: 120, Cached: 40})
	third := assertEvent(t, events, "usage", "req-b", "")
	if third.UsageCell == nil || third.UsageCell.APIKeyID != "key-2" || third.UsageCell.SiteID != "site-2" || third.UsageCell.ModelKey != "claude-sonnet-4.6" || third.UsageCell.TotalTokens != 400 {
		t.Fatalf("second usage cell = %#v", third.UsageCell)
	}

	snapshot := registry.Snapshot()
	if snapshot.TotalTokens != 1600 || len(snapshot.UsageCells) != 2 {
		t.Fatalf("snapshot cells = %#v", snapshot)
	}
	if snapshot.UsageCells[0].APIKeyID != "key-1" || snapshot.UsageCells[0].TotalTokens != 1200 {
		t.Fatalf("ranked first cell = %#v", snapshot.UsageCells[0])
	}
	if snapshot.UsageCells[1].APIKeyID != "key-2" || snapshot.UsageCells[1].SiteID != "site-2" || snapshot.UsageCells[1].ModelProvider != "anthropic" {
		t.Fatalf("ranked second cell = %#v", snapshot.UsageCells[1])
	}
}

func TestRegistryUsageCellAllowsEmptySiteAndModel(t *testing.T) {
	registry := NewRegistry()
	registry.Start(Request{RequestID: "req-empty", APIKeyID: "key-1", APIKeyName: "Client"})
	registry.AddTokens("req-empty", TokenAmount{Total: 50, Input: 50})
	snapshot := registry.Snapshot()
	if len(snapshot.UsageCells) != 1 {
		t.Fatalf("empty site/model cells = %#v", snapshot.UsageCells)
	}
	cell := snapshot.UsageCells[0]
	if cell.APIKeyID != "key-1" || cell.SiteID != "" || cell.ModelKey != "" || cell.TotalTokens != 50 {
		t.Fatalf("empty-dimension cell = %#v", cell)
	}
	if len(snapshot.UpstreamUsage) != 0 {
		t.Fatalf("upstream usage without site = %#v", snapshot.UpstreamUsage)
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
