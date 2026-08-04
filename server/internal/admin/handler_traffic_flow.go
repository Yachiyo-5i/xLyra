package admin

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"xlyra/server/internal/inflight"
	"xlyra/server/internal/store"
)

const trafficFlowSnapshotInterval = 10 * time.Second

type trafficFlowNode struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SiteType string `json:"site_type,omitempty"`
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
	return buildTrafficFlowTopology(apiKeys, sites), nil
}

func buildTrafficFlowTopology(apiKeys []store.APIKey, sites []store.Site) trafficFlowTopology {
	sort.SliceStable(apiKeys, func(i, j int) bool {
		leftActive := apiKeys[i].Status == "active"
		rightActive := apiKeys[j].Status == "active"
		if leftActive != rightActive {
			return leftActive
		}
		if !apiKeys[i].CreatedAt.Equal(apiKeys[j].CreatedAt) {
			return apiKeys[i].CreatedAt.Before(apiKeys[j].CreatedAt)
		}
		return apiKeys[i].ID.String() < apiKeys[j].ID.String()
	})
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
		topology.Downstream = append(topology.Downstream, trafficFlowNode{ID: apiKey.ID.String(), Name: apiKey.Name})
	}
	for _, site := range sites {
		if !site.Enabled || store.SiteDeleted(site) {
			continue
		}
		topology.Upstream = append(topology.Upstream, trafficFlowNode{ID: site.ID.String(), Name: site.Name, SiteType: site.SiteType})
	}
	return topology
}

func writeTrafficFlowEvent(w http.ResponseWriter, event string, payload any) error {
	return writeServerSentEvent(w, event, payload)
}
