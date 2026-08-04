package admin

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestDetectSiteTypeRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(
		handler.DetectSiteType,
		adminTestRequest(http.MethodPost, "/api/v1/site-types/detect", `{"base_url":"https://example.com"}`),
	)

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}

func TestDetectSiteTypeRejectsMissingBaseURL(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	rec := adminPerform(
		handler.DetectSiteType,
		adminTestRequest(http.MethodPost, "/api/v1/site-types/detect", `{"base_url":" "}`),
	)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_base_url")
}

func TestNewAPIUserSummaryValidatesRequestBeforeCallingClient(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithNewAPIService()
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "missing base url", body: `{"access_token":"token","user_id":1}`, code: "invalid_base_url"},
		{name: "missing access token", body: `{"base_url":"https://newapi.example.com","user_id":1}`, code: "invalid_access_token"},
		{name: "invalid user id", body: `{"base_url":"https://newapi.example.com","access_token":"token","user_id":0}`, code: "invalid_user_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(
				handler.NewAPIUserSummary,
				adminTestRequest(http.MethodPost, "/api/v1/newapi/user-summary", tc.body),
			)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestNewAPIAPIKeySummaryValidatesRequestBeforeCallingClient(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithNewAPIService()
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "missing base url", body: `{"api_key":"sk-test"}`, code: "invalid_base_url"},
		{name: "missing api key", body: `{"base_url":"https://newapi.example.com","api_key":" "}`, code: "invalid_api_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(
				handler.NewAPIAPIKeySummary,
				adminTestRequest(http.MethodPost, "/api/v1/newapi/api-key-summary", tc.body),
			)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestNewAPICheckinValidatesRequestBeforeCallingClient(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithNewAPIService()
	rec := adminPerform(
		handler.NewAPICheckin,
		adminTestRequest(http.MethodPost, "/api/v1/newapi/checkin", `{"base_url":"https://newapi.example.com","access_token":"token","user_id":-1}`),
	)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_user_id")
}

func TestNewAPIEndpointsRequireService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		body string
	}{
		{name: "user-summary", call: handler.NewAPIUserSummary, body: `{"base_url":"https://newapi.example.com","access_token":"token","user_id":1}`},
		{name: "api-key-summary", call: handler.NewAPIAPIKeySummary, body: `{"base_url":"https://newapi.example.com","api_key":"sk-test"}`},
		{name: "checkin", call: handler.NewAPICheckin, body: `{"base_url":"https://newapi.example.com","access_token":"token","user_id":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(
				tc.call,
				adminTestRequest(http.MethodPost, "/api/v1/newapi/"+tc.name, tc.body),
			)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "newapi_service_unavailable")
		})
	}
}

func TestSiteNewAPIEndpointsValidateSiteRouteBeforeLookup(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndNewAPIService()
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "user-summary", call: handler.SiteNewAPIUserSummary},
		{name: "api-key-summary", call: handler.SiteNewAPIAPIKeySummary},
		{name: "checkin", call: handler.SiteNewAPICheckin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/newapi/"+tc.name, `{}`, "siteID", "bad-id")
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_id")
		})
	}
}

func TestSiteNewAPIEndpointRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithNewAPIService()
	req := adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/site-id/newapi/user-summary", `{}`, "siteID", uuid.NewString())
	rec := adminPerform(handler.SiteNewAPIUserSummary, req)

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}
