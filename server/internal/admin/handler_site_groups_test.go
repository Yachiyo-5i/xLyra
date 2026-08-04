package admin

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"xlyra/server/internal/store"
)

func TestListSiteGroupsRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListSiteGroups, adminTestRequest(http.MethodGet, "/api/v1/site-groups", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}

func TestGetSiteGroupRejectsInvalidRouteID(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/site-groups/not-a-uuid", "", "siteGroupID", "not-a-uuid")
	rec := adminPerform(handler.GetSiteGroup, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_group_id")
}

func TestCreateSiteGroupRejectsInvalidJSONBeforeSiteGroupMutation(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	rec := adminPerform(handler.CreateSiteGroup, adminTestRequest(http.MethodPost, "/api/v1/site-groups", `{"name":`))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}

func TestSiteGroupMutationHandlersRejectInvalidRouteIDBeforeMutation(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "update", method: http.MethodPut, target: "/api/v1/site-groups/bad-id", body: `{}`, call: handler.UpdateSiteGroup},
		{name: "delete", method: http.MethodDelete, target: "/api/v1/site-groups/bad-id", call: handler.DeleteSiteGroup},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(tc.method, tc.target, tc.body, "siteGroupID", "bad-id")
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_group_id")
		})
	}
}

func TestSiteGroupInputDefaultsEnabledAndParsesSites(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	req := adminTestRequest(http.MethodPost, "/api/v1/site-groups", "")
	rec := adminRecorder()

	input, ok := siteGroupInputFromRequest(rec, req, siteGroupRequest{
		Name:        "Primary",
		Slug:        "primary",
		Description: "Main routes",
		SortOrder:   7,
		SiteIDs:     []string{" ", siteID.String()},
	})

	adminRequireParserOK(t, rec, ok, "site group input")
	if !input.Enabled {
		t.Fatal("enabled should default to true")
	}
	if input.Name != "Primary" || input.Slug != "primary" || input.Description != "Main routes" || input.SortOrder != 7 {
		t.Fatalf("unexpected input fields: %#v", input)
	}
	if len(input.SiteIDs) != 1 || input.SiteIDs[0] != siteID {
		t.Fatalf("unexpected site ids: %#v", input.SiteIDs)
	}
}

func TestSiteGroupInputRejectsInvalidSiteID(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/site-groups", "")
	rec := adminRecorder()

	_, ok := siteGroupInputFromRequest(rec, req, siteGroupRequest{SiteIDs: []string{"bad-id"}})

	adminAssertParserError(t, rec, ok, "invalid_site_id")
}

func TestSiteGroupPayloadIncludesLinkedSites(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	groupID := uuid.New()
	siteID := uuid.New()
	linkID := uuid.New()

	payload := siteGroupPayload(store.SiteGroup{
		ID:          groupID,
		Name:        "Primary",
		Slug:        "primary",
		Description: "Main routes",
		Enabled:     true,
		SortOrder:   3,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, []store.SiteGroupSite{
		{
			ID:        linkID,
			GroupID:   groupID,
			SiteID:    siteID,
			CreatedAt: now,
			UpdatedAt: now,
		},
	})

	if payload["id"] != groupID.String() || payload["name"] != "Primary" || payload["enabled"] != true {
		t.Fatalf("unexpected group payload: %#v", payload)
	}
	sites, ok := payload["sites"].([]map[string]any)
	if !ok || len(sites) != 1 {
		t.Fatalf("unexpected sites payload: %#v", payload["sites"])
	}
	if sites[0]["id"] != linkID.String() || sites[0]["group_id"] != groupID.String() || sites[0]["site_id"] != siteID.String() {
		t.Fatalf("unexpected site link payload: %#v", sites[0])
	}
}
