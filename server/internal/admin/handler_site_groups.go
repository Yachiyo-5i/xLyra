package admin

import (
	"net/http"

	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

type siteGroupRequest struct {
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	Description string   `json:"description"`
	Enabled     *bool    `json:"enabled"`
	SortOrder   int      `json:"sort_order"`
	SiteIDs     []string `json:"site_ids"`
}

func (h Handler) ListSiteGroups(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	groups, err := h.sites.ListSiteGroups(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_group_list_failed", "failed to list site groups")
		return
	}
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		sites, _ := h.sites.SiteGroupSites(r.Context(), group.ID)
		items = append(items, siteGroupPayload(group, sites))
	}
	h.writeItems(w, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (h Handler) GetSiteGroup(w http.ResponseWriter, r *http.Request) {
	group, ok := h.siteGroupByParam(w, r)
	if !ok {
		return
	}
	sites, _ := h.sites.SiteGroupSites(r.Context(), group.ID)
	h.writeResource(w, http.StatusOK, "site_group", siteGroupPayload(group, sites))
}

func (h Handler) CreateSiteGroup(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	var payload siteGroupRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	input, ok := siteGroupInputFromRequest(w, r, payload)
	if !ok {
		return
	}
	group, err := h.sites.CreateSiteGroup(r.Context(), input)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_group_create_failed", err.Error())
		return
	}
	sites, _ := h.sites.SiteGroupSites(r.Context(), group.ID)
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusCreated, "site_group", siteGroupPayload(group, sites))
}

func (h Handler) UpdateSiteGroup(w http.ResponseWriter, r *http.Request) {
	group, ok := h.siteGroupByParam(w, r)
	if !ok {
		return
	}
	var payload siteGroupRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	input, ok := siteGroupInputFromRequest(w, r, payload)
	if !ok {
		return
	}
	updated, err := h.sites.UpdateSiteGroup(r.Context(), group.ID, input)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_group_update_failed", err.Error())
		return
	}
	sites, _ := h.sites.SiteGroupSites(r.Context(), updated.ID)
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "site_group", siteGroupPayload(updated, sites))
}

func (h Handler) DeleteSiteGroup(w http.ResponseWriter, r *http.Request) {
	group, ok := h.siteGroupByParam(w, r)
	if !ok {
		return
	}
	if err := h.sites.DeleteSiteGroup(r.Context(), group.ID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_group_delete_failed", err.Error())
		return
	}
	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) siteGroupByParam(w http.ResponseWriter, r *http.Request) (store.SiteGroup, bool) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return store.SiteGroup{}, false
	}
	id, ok := siteGroupIDParam(w, r)
	if !ok {
		return store.SiteGroup{}, false
	}
	group, err := h.sites.GetSiteGroup(r.Context(), id)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_group_not_found", "site group was not found")
		return store.SiteGroup{}, false
	}
	return group, true
}

func siteGroupInputFromRequest(w http.ResponseWriter, r *http.Request, payload siteGroupRequest) (site.SiteGroupInput, bool) {
	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	siteIDs, ok := parseSiteIDs(w, r, payload.SiteIDs)
	if !ok {
		return site.SiteGroupInput{}, false
	}
	return site.SiteGroupInput{
		Name:        payload.Name,
		Slug:        payload.Slug,
		Description: payload.Description,
		Enabled:     enabled,
		SortOrder:   payload.SortOrder,
		SiteIDs:     siteIDs,
	}, true
}

func siteGroupPayload(group store.SiteGroup, sites []store.SiteGroupSite) map[string]any {
	return map[string]any{
		"id":          group.ID.String(),
		"name":        group.Name,
		"slug":        group.Slug,
		"description": group.Description,
		"enabled":     group.Enabled,
		"sort_order":  group.SortOrder,
		"sites":       siteGroupSitePayloads(sites),
		"created_at":  timeString(group.CreatedAt),
		"updated_at":  timeString(group.UpdatedAt),
	}
}

func siteGroupSitePayloads(items []store.SiteGroupSite) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id":         item.ID.String(),
			"group_id":   item.GroupID.String(),
			"site_id":    item.SiteID.String(),
			"created_at": timeString(item.CreatedAt),
			"updated_at": timeString(item.UpdatedAt),
		})
	}
	return result
}
