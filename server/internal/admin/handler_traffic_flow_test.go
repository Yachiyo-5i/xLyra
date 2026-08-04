package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/inflight"
	"xlyra/server/internal/store"
)

func TestTrafficFlowStreamWritesSnapshotBeforeRequestEnds(t *testing.T) {
	requestID := "traffic-flow-test-request"
	inflight.Start(inflight.Request{RequestID: requestID, APIKeyName: "Client", ModelKey: "gpt-test"})
	defer inflight.Finish(requestID, inflight.PhaseCancelled)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/traffic-flow/stream", nil)
	rec := httptest.NewRecorder()
	Handler{}.TrafficFlowStream(rec, req)

	if rec.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "event: snapshot") || !strings.Contains(rec.Body.String(), requestID) {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
}

func TestTrafficFlowTopologyReturnsUnavailableWithoutStore(t *testing.T) {
	rec := adminPerform(Handler{}.TrafficFlowTopology, adminTestRequest(http.MethodGet, "/api/v1/traffic-flow/topology", ""))
	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "traffic_flow_topology_unavailable")
}

func TestBuildTrafficFlowTopologyMatchesManagementOrdering(t *testing.T) {
	base := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	activeOldID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	activeNewID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	disabledOldID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	disabledNewID := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	oauthID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	regularID := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	secondOAuthID := uuid.MustParse("10000000-0000-0000-0000-000000000003")
	disabledSiteID := uuid.MustParse("10000000-0000-0000-0000-000000000004")

	topology := buildTrafficFlowTopology(
		[]store.APIKey{
			{ID: disabledNewID, Name: "Disabled New", Status: "disabled", CreatedAt: base.Add(4 * time.Hour)},
			{ID: activeNewID, Name: "Active New", Status: "active", CreatedAt: base.Add(3 * time.Hour)},
			{ID: disabledOldID, Name: "Disabled Old", Status: "disabled", CreatedAt: base},
			{ID: activeOldID, Name: "Active Old", Status: "active", CreatedAt: base.Add(time.Hour)},
		},
		[]store.Site{
			{ID: disabledSiteID, Name: "Disabled High Weight", SiteType: "openai", Enabled: false, RoutingPriority: 9, CreatedAt: base},
			{ID: secondOAuthID, Name: "Antigravity OAuth", SiteType: "antigravity", Enabled: true, RoutingPriority: 1, CreatedAt: base.Add(3 * time.Hour)},
			{ID: regularID, Name: "Regular", SiteType: "openai", Enabled: true, RoutingPriority: 3, CreatedAt: base.Add(2 * time.Hour)},
			{ID: oauthID, Name: "Codex OAuth", SiteType: "codex", Enabled: true, RoutingPriority: 5, CreatedAt: base.Add(time.Hour)},
		},
	)

	wantDownstream := []uuid.UUID{activeOldID, activeNewID}
	if len(topology.Downstream) != len(wantDownstream) {
		t.Fatalf("downstream count = %d, want %d", len(topology.Downstream), len(wantDownstream))
	}
	for index, want := range wantDownstream {
		if topology.Downstream[index].ID != want.String() {
			t.Fatalf("downstream[%d] = %s, want %s", index, topology.Downstream[index].ID, want)
		}
	}

	wantUpstream := []uuid.UUID{oauthID, regularID, secondOAuthID}
	if len(topology.Upstream) != len(wantUpstream) {
		t.Fatalf("upstream count = %d, want %d", len(topology.Upstream), len(wantUpstream))
	}
	for index, want := range wantUpstream {
		if topology.Upstream[index].ID != want.String() {
			t.Fatalf("upstream[%d] = %s, want %s", index, topology.Upstream[index].ID, want)
		}
	}
	if topology.Upstream[0].SiteType != "codex" || topology.Upstream[2].SiteType != "antigravity" {
		t.Fatalf("oauth site types = %q, %q", topology.Upstream[0].SiteType, topology.Upstream[2].SiteType)
	}
}
