package admin

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/inflight"
	"xlyra/server/internal/store"
)

const trafficFlowSnapshotInterval = 10 * time.Second

type trafficFlowNode struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	SiteType           string   `json:"site_type,omitempty"`
	SitePolicy         string   `json:"site_policy,omitempty"`
	AllowedUpstreamIDs []string `json:"allowed_upstream_ids,omitempty"`
	HealthStatus       string   `json:"health_status,omitempty"`
	RecentSuccessRate  *float64 `json:"recent_success_rate,omitempty"`
	RecentAvgLatencyMS *int64   `json:"recent_avg_latency_ms,omitempty"`
}

type trafficFlowTopology struct {
	Gateway    trafficFlowNode   `json:"gateway"`
	Downstream []trafficFlowNode `json:"downstream"`
	Upstream   []trafficFlowNode `json:"upstream"`
}

func (h Handler) TrafficFlowTopology(w http.ResponseWriter, r *http.Request) {
	topology, err := h.trafficFlowTopology(r)
	if err != nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "traffic_flow_topology_unavailable", "traffic flow topology is not available")
		return
	}
	h.writePayload(w, http.StatusOK, topology)
}

func (h Handler) TrafficFlowStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeError(w, r, http.StatusInternalServerError, "stream_unsupported", "streaming is not supported")
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")

	events, unsubscribe := inflight.Subscribe()
	defer unsubscribe()
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	flusher.Flush()

	if err := writeTrafficFlowEvent(w, "snapshot", inflight.CurrentSnapshot()); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	snapshots := time.NewTicker(trafficFlowSnapshotInterval)
	defer snapshots.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeTrafficFlowEvent(w, event.Type, event); err != nil {
				return
			}
			flusher.Flush()
		case <-snapshots.C:
			if err := writeTrafficFlowEvent(w, "snapshot", inflight.CurrentSnapshot()); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h Handler) trafficFlowTopology(r *http.Request) (trafficFlowTopology, error) {
	if h.trafficDB == nil {
		return trafficFlowTopology{}, fmt.Errorf("traffic flow store is unavailable")
	}
	apiKeys, err := store.NewAPIKeyRepository(h.trafficDB.DB()).List(r.Context())
	if err != nil {
		return trafficFlowTopology{}, err
	}
	sites, err := store.NewSiteRepository(h.trafficDB.DB()).List(r.Context())
	if err != nil {
		return trafficFlowTopology{}, err
	}
	topology := buildTrafficFlowTopology(apiKeys, sites)
	h.enrichTrafficFlowTopology(r.Context(), topology, apiKeys)
	return topology, nil
}

func (h Handler) enrichTrafficFlowTopology(ctx context.Context, topology trafficFlowTopology, apiKeys []store.APIKey) {
	enabledSiteIDs := make([]uuid.UUID, 0, len(topology.Upstream))
	enabledSet := map[uuid.UUID]struct{}{}
	for _, node := range topology.Upstream {
		id, err := uuid.Parse(node.ID)
		if err != nil {
			continue
		}
		enabledSiteIDs = append(enabledSiteIDs, id)
		enabledSet[id] = struct{}{}
	}

	allowedByKey, err := h.trafficFlowAllowedUpstreamIDs(ctx, apiKeys, enabledSiteIDs, enabledSet)
	if err == nil {
		attachTrafficFlowAllowedUpstreams(topology.Downstream, allowedByKey)
	}

	if states, err := store.NewHealthRepository(h.trafficDB.DB()).ListSiteStates(ctx); err == nil {
		attachTrafficFlowHealth(topology.Upstream, states)
	}
}

func (h Handler) trafficFlowAllowedUpstreamIDs(ctx context.Context, apiKeys []store.APIKey, enabledSiteIDs []uuid.UUID, enabledSet map[uuid.UUID]struct{}) (map[uuid.UUID][]string, error) {
	direct, err := store.NewAPIKeyRepository(h.trafficDB.DB()).ListAllSitePermissions(ctx)
	if err != nil {
		return nil, err
	}
	groupPermissions, err := store.NewAPIKeyAccessRepository(h.trafficDB.DB()).ListAllSiteGroupPermissions(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := store.NewSiteGroupRepository(h.trafficDB.DB()).List(ctx)
	if err != nil {
		return nil, err
	}
	groupSites, err := store.NewSiteGroupRepository(h.trafficDB.DB()).ListAllGroupSites(ctx)
	if err != nil {
		return nil, err
	}

	enabledGroups := map[uuid.UUID]struct{}{}
	for _, group := range groups {
		if group.Enabled {
			enabledGroups[group.ID] = struct{}{}
		}
	}
	sitesByGroup := map[uuid.UUID][]uuid.UUID{}
	for _, item := range groupSites {
		if _, ok := enabledGroups[item.GroupID]; !ok {
			continue
		}
		if _, ok := enabledSet[item.SiteID]; !ok {
			continue
		}
		sitesByGroup[item.GroupID] = append(sitesByGroup[item.GroupID], item.SiteID)
	}

	directByKey := map[uuid.UUID][]uuid.UUID{}
	for _, permission := range direct {
		if !permission.Enabled {
			continue
		}
		if _, ok := enabledSet[permission.SiteID]; !ok {
			continue
		}
		directByKey[permission.APIKeyID] = append(directByKey[permission.APIKeyID], permission.SiteID)
	}
	groupByKey := map[uuid.UUID][]uuid.UUID{}
	for _, permission := range groupPermissions {
		if !permission.Enabled {
			continue
		}
		groupByKey[permission.APIKeyID] = append(groupByKey[permission.APIKeyID], sitesByGroup[permission.GroupID]...)
	}

	allEnabled := make([]string, 0, len(enabledSiteIDs))
	for _, id := range enabledSiteIDs {
		allEnabled = append(allEnabled, id.String())
	}

	result := map[uuid.UUID][]string{}
	for _, apiKey := range apiKeys {
		if apiKey.Status != "active" {
			continue
		}
		result[apiKey.ID] = resolveAllowedUpstreamIDs(apiKey.SitePolicy, allEnabled, directByKey[apiKey.ID], groupByKey[apiKey.ID])
	}
	return result, nil
}

func buildTrafficFlowTopology(apiKeys []store.APIKey, sites []store.Site) trafficFlowTopology {
	store.SortAPIKeys(apiKeys)
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].RoutingPriority != sites[j].RoutingPriority {
			return sites[i].RoutingPriority > sites[j].RoutingPriority
		}
		if !sites[i].CreatedAt.Equal(sites[j].CreatedAt) {
			return sites[i].CreatedAt.Before(sites[j].CreatedAt)
		}
		return sites[i].ID.String() < sites[j].ID.String()
	})
	topology := trafficFlowTopology{
		Gateway:    trafficFlowNode{ID: "gateway", Name: "xLyra Gateway"},
		Downstream: make([]trafficFlowNode, 0, len(apiKeys)),
		Upstream:   make([]trafficFlowNode, 0, len(sites)),
	}
	for _, apiKey := range apiKeys {
		if apiKey.Status != "active" {
			continue
		}
		topology.Downstream = append(topology.Downstream, trafficFlowNode{
			ID:         apiKey.ID.String(),
			Name:       apiKey.Name,
			SitePolicy: trafficFlowSitePolicy(apiKey.SitePolicy),
		})
	}
	for _, site := range sites {
		if !site.Enabled || store.SiteDeleted(site) {
			continue
		}
		topology.Upstream = append(topology.Upstream, trafficFlowNode{ID: site.ID.String(), Name: site.Name, SiteType: site.SiteType})
	}
	return topology
}

func attachTrafficFlowAllowedUpstreams(nodes []trafficFlowNode, allowedByKey map[uuid.UUID][]string) {
	for index := range nodes {
		id, err := uuid.Parse(nodes[index].ID)
		if err != nil {
			continue
		}
		if ids, ok := allowedByKey[id]; ok {
			nodes[index].AllowedUpstreamIDs = ids
		}
	}
}

func attachTrafficFlowHealth(nodes []trafficFlowNode, states map[uuid.UUID]store.SiteHealthState) {
	for index := range nodes {
		id, err := uuid.Parse(nodes[index].ID)
		if err != nil {
			continue
		}
		state, ok := states[id]
		if !ok {
			continue
		}
		nodes[index].HealthStatus = state.Status
		if state.RecentSuccessRate.Valid {
			value := state.RecentSuccessRate.Float64
			nodes[index].RecentSuccessRate = &value
		}
		if state.RecentAvgLatencyMS.Valid {
			value := state.RecentAvgLatencyMS.Int64
			nodes[index].RecentAvgLatencyMS = &value
		}
	}
}

func resolveAllowedUpstreamIDs(sitePolicy string, allEnabled []string, direct []uuid.UUID, fromGroups []uuid.UUID) []string {
	if trafficFlowSitePolicy(sitePolicy) != "allow_list" {
		return append([]string(nil), allEnabled...)
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(direct)+len(fromGroups))
	for _, id := range append(append([]uuid.UUID{}, direct...), fromGroups...) {
		value := id.String()
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	sort.Strings(ids)
	return ids
}

func trafficFlowSitePolicy(value string) string {
	if value == "allow_list" {
		return "allow_list"
	}
	return "allow_all"
}

func writeTrafficFlowEvent(w http.ResponseWriter, event string, payload any) error {
	return writeServerSentEvent(w, event, payload)
}
