package admin

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/newapi"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestDetectSiteTypeRejectsInvalidJSONWhenSiteServiceExists(t *testing.T) {
	t.Parallel()

	handler := Handler{sites: adminSiteService()}
	rec := adminPerform(
		handler.DetectSiteType,
		adminTestRequest(http.MethodPost, "/api/v1/site-types/detect", `{"base_url":`),
	)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}

func TestNewAPICheckinRejectsRequiredFieldValidationErrors(t *testing.T) {
	t.Parallel()

	handler := Handler{newAPI: newapi.NewService()}
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "missing base url", body: `{"access_token":"token","user_id":1}`, code: "invalid_base_url"},
		{name: "missing access token", body: `{"base_url":"https://newapi.example.com","access_token":" ","user_id":1}`, code: "invalid_access_token"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(
				handler.NewAPICheckin,
				adminTestRequest(http.MethodPost, "/api/v1/newapi/checkin", tc.body),
			)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestSiteNewAPIEndpointsRequireSiteServiceBeforeDecodingBody(t *testing.T) {
	t.Parallel()

	handler := Handler{newAPI: newapi.NewService()}

	for _, tc := range []struct {
		name   string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "user summary", target: "/api/v1/sites/site-id/newapi/user-summary", call: handler.SiteNewAPIUserSummary},
		{name: "api key summary", target: "/api/v1/sites/site-id/newapi/api-key-summary", call: handler.SiteNewAPIAPIKeySummary},
		{name: "checkin", target: "/api/v1/sites/site-id/newapi/checkin", call: handler.SiteNewAPICheckin},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(http.MethodPost, tc.target, `{`, "siteID", "site-id")
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
		})
	}
}

func TestRouteTracePayloadsIgnoreNonPositiveLimit(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Minute)
	payloads := routeTracePayloads([]store.RequestLogDetail{
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-a", CreatedAt: first}},
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-b", CreatedAt: second}},
	}, 0)

	if len(payloads) != 2 {
		t.Fatalf("payload count = %d, want 2 for non-positive limit: %#v", len(payloads), payloads)
	}
	if payloads[0]["parent_request_id"] != "req-b" || payloads[1]["parent_request_id"] != "req-a" {
		t.Fatalf("payload order = %#v, want newest trace first", payloads)
	}
}

func TestRouteCandidatePayloadHidesScoreBreakdownWithoutDebug(t *testing.T) {
	t.Parallel()

	payload := routeCandidatePayload(routeengine.Candidate{
		Rank: 1,
		Site: routeengine.CandidateSite{ID: uuid.New()},
		Model: routeengine.CandidateModel{
			SiteModelID: uuid.New(),
		},
		ScoreBreakdown: map[string]float64{"site_health": 1},
	}, false)

	if _, ok := payload["score_breakdown"]; ok {
		t.Fatalf("score_breakdown present with debug disabled: %#v", payload)
	}
}
