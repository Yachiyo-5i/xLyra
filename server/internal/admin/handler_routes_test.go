package admin

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestListRouteCandidatesRequiresRouterService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListRouteCandidates, adminTestRequest(http.MethodGet, "/api/v1/routes/candidates?model_key=gpt-5", ""))

	adminAssertStatus(t, rec, http.StatusServiceUnavailable)
	body := adminDecodeJSON[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != "router_service_unavailable" {
		t.Fatalf("error code = %q, want router_service_unavailable", body.Error.Code)
	}
}

func TestRouteHandlersRequireRouterService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.NewString()
	cases := []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "list routes",
			method: http.MethodGet,
			target: "/api/v1/routes",
			call:   handler.ListRoutes,
		},
		{
			name:   "list cooldowns",
			method: http.MethodGet,
			target: "/api/v1/routes/cooldowns",
			call:   handler.ListRouteCooldowns,
		},
		{
			name:   "create cooldown",
			method: http.MethodPost,
			target: "/api/v1/routes/cooldowns",
			body:   `{"site_id":"` + siteID + `"}`,
			call:   handler.CreateRouteCooldown,
		},
		{
			name:   "select route",
			method: http.MethodPost,
			target: "/api/v1/routes/select",
			body:   `{"model_key":"gpt-5"}`,
			call:   handler.SelectRoute,
		},
		{
			name:   "failover route",
			method: http.MethodPost,
			target: "/api/v1/routes/failover",
			body:   `{"model_key":"gpt-5"}`,
			call:   handler.FailoverRoute,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(tc.method, tc.target, tc.body))

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "router_service_unavailable")
		})
	}
}

func TestListRoutesRejectsInvalidWindowHours(t *testing.T) {
	t.Parallel()

	handler := Handler{router: routeengine.NewService(nil)}
	for _, raw := range []string{"abc", "0", "-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListRoutes, adminTestRequest(http.MethodGet, "/api/v1/routes?window_hours="+raw, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_window_hours")
		})
	}
}

func TestRouteHandlersRejectInvalidPayloadBeforeRouterWork(t *testing.T) {
	t.Parallel()

	handler := Handler{router: routeengine.NewService(nil)}
	cases := []struct {
		name   string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		status int
		code   string
	}{
		{
			name:   "create cooldown invalid json",
			target: "/api/v1/routes/cooldowns",
			body:   `{`,
			call:   handler.CreateRouteCooldown,
			status: http.StatusBadRequest,
			code:   "invalid_json",
		},
		{
			name:   "create cooldown missing site id",
			target: "/api/v1/routes/cooldowns",
			body:   `{}`,
			call:   handler.CreateRouteCooldown,
			status: http.StatusBadRequest,
			code:   "invalid_site_id",
		},
		{
			name:   "select route invalid json",
			target: "/api/v1/routes/select",
			body:   `{`,
			call:   handler.SelectRoute,
			status: http.StatusBadRequest,
			code:   "invalid_json",
		},
		{
			name:   "select route missing model key",
			target: "/api/v1/routes/select",
			body:   `{"model_key":" "}`,
			call:   handler.SelectRoute,
			status: http.StatusBadRequest,
			code:   "invalid_route_query",
		},
		{
			name:   "failover route invalid json",
			target: "/api/v1/routes/failover",
			body:   `{`,
			call:   handler.FailoverRoute,
			status: http.StatusBadRequest,
			code:   "invalid_json",
		},
		{
			name:   "failover route negative failover limit",
			target: "/api/v1/routes/failover",
			body:   `{"model_key":"gpt-5","failover_limit":-1}`,
			call:   handler.FailoverRoute,
			status: http.StatusBadRequest,
			code:   "invalid_route_query",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(http.MethodPost, tc.target, tc.body))

			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestRouteTracePayloadsGroupsAttemptsByParentAndSorts(t *testing.T) {
	t.Parallel()

	parentID := "req-parent"
	firstID := uuid.New()
	secondID := uuid.New()
	laterTraceID := uuid.New()
	startedAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	secondAt := startedAt.Add(2 * time.Second)
	laterAt := startedAt.Add(time.Minute)

	payloads := routeTracePayloads([]store.RequestLogDetail{
		{
			RequestLog: store.RequestLog{
				ID:         secondID,
				RequestID:  "req-second",
				Success:    true,
				StatusCode: http.StatusOK,
				LatencyMS:  sql.NullInt64{Int64: 80, Valid: true},
				Metadata:   store.JSON(`{"parent_request_id":"` + parentID + `","attempt":2,"credential_attempt":1,"credential_total":2}`),
				CreatedAt:  secondAt,
			},
		},
		{
			RequestLog: store.RequestLog{
				ID:         firstID,
				RequestID:  "req-first",
				Success:    false,
				StatusCode: http.StatusBadGateway,
				ErrorType:  sql.NullString{String: "upstream_failed", Valid: true},
				LatencyMS:  sql.NullInt64{Int64: 120, Valid: true},
				Metadata:   store.JSON(`{"parent_request_id":"` + parentID + `","attempt":1,"upstream_response":{"error":"failed"}}`),
				CreatedAt:  startedAt,
			},
		},
		{
			RequestLog: store.RequestLog{
				ID:         laterTraceID,
				RequestID:  "req-later",
				Success:    true,
				StatusCode: http.StatusOK,
				CreatedAt:  laterAt,
			},
		},
	}, 10)

	if len(payloads) != 2 {
		t.Fatalf("expected two route traces, got %#v", payloads)
	}
	if payloads[0]["parent_request_id"] != "req-later" {
		t.Fatalf("latest trace should be first, got %#v", payloads[0]["parent_request_id"])
	}
	if payloads[1]["parent_request_id"] != parentID {
		t.Fatalf("second trace parent = %#v, want %s", payloads[1]["parent_request_id"], parentID)
	}
	if payloads[1]["success"] != true {
		t.Fatalf("trace success = %#v, want true when any attempt succeeds", payloads[1]["success"])
	}
	if payloads[1]["attempt_count"] != 2 || payloads[1]["failover_count"] != 1 {
		t.Fatalf("unexpected attempt counts: %#v", payloads[1])
	}
	if payloads[1]["total_latency_ms"] != int64(200) {
		t.Fatalf("total latency = %#v, want 200", payloads[1]["total_latency_ms"])
	}

	attempts, ok := payloads[1]["attempts"].([]map[string]any)
	if !ok || len(attempts) != 2 {
		t.Fatalf("expected two attempt payloads, got %#v", payloads[1]["attempts"])
	}
	if attempts[0]["request_id"] != "req-first" || attempts[1]["request_id"] != "req-second" {
		t.Fatalf("attempts should be sorted by created_at, got %#v", attempts)
	}
	if attempts[0]["failure_response"] == nil {
		t.Fatalf("failed attempt should include failure response, got %#v", attempts[0])
	}
}

func TestRouteCandidatePayloadIncludesDebugBreakdown(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	group := "default"
	currency := "USD"
	input := 0.25
	output := 1.25
	successRate := 0.95
	latency := int64(320)

	payload := routeCandidatePayload(routeengine.Candidate{
		Rank:  1,
		Score: 98.5,
		Site: routeengine.CandidateSite{
			ID:              siteID,
			Name:            "Primary",
			Slug:            "primary",
			SiteType:        "openai",
			BaseURL:         "https://api.example.com",
			RoutingPriority: 1.5,
		},
		Model: routeengine.CandidateModel{
			SiteModelID:     siteModelID,
			UpstreamName:    "gpt-5",
			DisplayName:     "GPT-5",
			MatchSource:     "alias",
			MatchConfidence: 95,
		},
		Health: routeengine.CandidateHealth{
			Status:             "healthy",
			RecentSuccessRate:  &successRate,
			RecentAvgLatencyMS: &latency,
		},
		Availability: routeengine.CandidateAvailability{
			AvailableAPIKeys: 2,
			TotalAPIKeys:     3,
		},
		Pricing: routeengine.CandidatePricing{
			GroupName:   &group,
			Currency:    &currency,
			InputValue:  &input,
			OutputValue: &output,
		},
		ScoreBreakdown: map[string]float64{"site_health": 40},
	}, true)

	if payload["rank"] != 1 || payload["score"] != 98.5 {
		t.Fatalf("unexpected rank/score: %#v", payload)
	}
	site, _ := payload["site"].(map[string]any)
	if site["id"] != siteID.String() || site["routing_priority"] != 1.5 {
		t.Fatalf("unexpected site payload: %#v", site)
	}
	model, _ := payload["model"].(map[string]any)
	if model["site_model_id"] != siteModelID.String() || model["canonical_match_confidence"] != 95 {
		t.Fatalf("unexpected model payload: %#v", model)
	}
	health, _ := payload["health"].(map[string]any)
	if health["recent_success_rate"] != successRate || health["recent_avg_latency_ms"] != latency {
		t.Fatalf("unexpected health payload: %#v", health)
	}
	if payload["score_breakdown"] == nil {
		t.Fatalf("expected debug score breakdown")
	}
}

func TestRouteSelectionRequestToCandidateQueryValidatesAndCopiesFields(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	excludeSiteIDs := []uuid.UUID{siteID}
	excludeSiteModelIDs := []uuid.UUID{siteModelID}
	query, err := (routeSelectionRequest{
		ModelKey:            " gpt-5 ",
		Debug:               true,
		ExcludeSiteIDs:      excludeSiteIDs,
		ExcludeSiteModelIDs: excludeSiteModelIDs,
		Limit:               5,
		FailoverLimit:       2,
	}).toCandidateQuery()
	if err != nil {
		t.Fatalf("toCandidateQuery returned error: %v", err)
	}
	if query.ModelKey != " gpt-5 " || !query.Debug || query.Limit != 5 || query.FailoverLimit != 2 {
		t.Fatalf("unexpected route query: %#v", query)
	}
	if len(query.ExcludeSiteIDs) != 1 || query.ExcludeSiteIDs[0] != siteID {
		t.Fatalf("exclude site IDs = %#v", query.ExcludeSiteIDs)
	}
	if len(query.ExcludeSiteModelIDs) != 1 || query.ExcludeSiteModelIDs[0] != siteModelID {
		t.Fatalf("exclude site model IDs = %#v", query.ExcludeSiteModelIDs)
	}
	excludeSiteIDs[0] = uuid.New()
	excludeSiteModelIDs[0] = uuid.New()
	if query.ExcludeSiteIDs[0] != siteID || query.ExcludeSiteModelIDs[0] != siteModelID {
		t.Fatalf("expected route query to own copies of exclude IDs, got sites=%#v models=%#v", query.ExcludeSiteIDs, query.ExcludeSiteModelIDs)
	}

	for _, payload := range []routeSelectionRequest{
		{ModelKey: " \t\n "},
		{ModelKey: "gpt-5", Limit: -1},
		{ModelKey: "gpt-5", FailoverLimit: -1},
	} {
		if _, err := payload.toCandidateQuery(); err == nil {
			t.Fatalf("expected invalid payload to fail: %#v", payload)
		}
	}

	if got := uuidStrings(nil); len(got) != 0 {
		t.Fatalf("uuidStrings(nil) = %#v, want empty slice", got)
	}
	if got := uuidStrings([]uuid.UUID{siteID}); len(got) != 1 || got[0] != siteID.String() {
		t.Fatalf("uuidStrings = %#v", got)
	}
}

func TestDecodeRouteSelectionRequestWritesBadRequestForInvalidPayload(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminTestRequest(http.MethodPost, "/api/v1/routes/select", `{"model_key":" ","limit":1}`)
	rec := adminRecorder()

	if _, ok := handler.decodeRouteSelectionRequest(rec, req); ok {
		t.Fatal("expected invalid route selection payload to fail")
	}
	adminAssertStatus(t, rec, http.StatusBadRequest)

	req = adminTestRequest(http.MethodPost, "/api/v1/routes/select", `{"model_key":"gpt-5","debug":true,"failover_limit":1}`)
	rec = adminRecorder()
	query, ok := handler.decodeRouteSelectionRequest(rec, req)
	adminRequireParserOK(t, rec, ok, "route selection payload")
	if query.ModelKey != "gpt-5" || !query.Debug || query.FailoverLimit != 1 {
		t.Fatalf("unexpected decoded query: %#v", query)
	}
}

func TestRouteSelectionOverviewAndCooldownPayloads(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	group := "default"
	currency := "USD"
	price := 0.25
	selected := routeengine.Candidate{
		Rank:  1,
		Score: 100,
		Site:  routeengine.CandidateSite{ID: siteID, Name: "Primary", Slug: "primary"},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-5"},
		Pricing: routeengine.CandidatePricing{
			GroupName:       &group,
			Currency:        &currency,
			PerRequestValue: &price,
		},
		ScoreBreakdown: map[string]float64{"site_health": 40},
	}
	planPayload := routeSelectionPlanPayload(routeengine.SelectionPlan{
		CanonicalModel: store.CanonicalModel{ID: canonicalID, ModelKey: "gpt-5", DisplayName: "GPT-5"},
		Selected:       selected,
		Failover:       []routeengine.Candidate{selected},
	}, true)
	meta := planPayload["meta"].(map[string]any)
	if meta["debug"] != true || meta["failover_count"] != 1 {
		t.Fatalf("unexpected route plan meta: %#v", meta)
	}
	selectedPayload := planPayload["selected"].(map[string]any)
	if selectedPayload["score_breakdown"] == nil {
		t.Fatalf("expected selected payload to include debug score breakdown: %#v", selectedPayload)
	}
	failover := planPayload["failover"].([]map[string]any)
	if len(failover) != 1 {
		t.Fatalf("failover payload = %#v", failover)
	}

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	overview := routeOverviewPayload(store.RouteOverviewRow{
		CanonicalModelID:    canonicalID,
		ModelKey:            "gpt-5",
		DisplayName:         "GPT-5",
		Status:              "active",
		SiteModelCount:      3,
		SiteCount:           2,
		EligibleCount:       1,
		CooldownCount:       1,
		RequestCount24h:     10,
		SuccessCount24h:     8,
		SuccessRate24h:      sql.NullFloat64{Float64: 0.8, Valid: true},
		AvgLatencyMS24h:     sql.NullInt64{Int64: 250, Valid: true},
		PromptTokens24h:     sql.NullInt64{Int64: 1000, Valid: true},
		CompletionTokens24h: sql.NullInt64{Int64: 2000, Valid: true},
		EstimatedCost24h:    sql.NullFloat64{Float64: 1.5, Valid: true},
		LastRoutedAt:        sql.NullTime{Time: now, Valid: true},
		LastSiteName:        sql.NullString{String: "Primary", Valid: true},
		LastStatusCode:      sql.NullInt64{Int64: 200, Valid: true},
		LastSuccess:         sql.NullBool{Bool: true, Valid: true},
	})
	candidateSummary := overview["candidate_summary"].(map[string]any)
	if candidateSummary["site_model_count"] != 3 || candidateSummary["eligible_count"] != 1 {
		t.Fatalf("unexpected candidate summary: %#v", candidateSummary)
	}
	traffic := overview["traffic_24h"].(map[string]any)
	if traffic["request_count"] != 10 || traffic["success_rate"] != 0.8 || traffic["estimated_cost"] != 1.5 {
		t.Fatalf("unexpected traffic payload: %#v", traffic)
	}
	lastRoute := overview["last_route"].(map[string]any)
	if lastRoute["site_name"] != "Primary" || lastRoute["success"] != true {
		t.Fatalf("unexpected last route payload: %#v", lastRoute)
	}

	cooldownID := uuid.New()
	credentialID := uuid.New()
	cooldown := routeCooldownPayload(store.RouteCooldown{
		ID:               cooldownID,
		SiteID:           siteID,
		SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Scope:            "credential",
		Source:           "manual",
		Reason:           "maintenance",
		ActiveUntil:      time.Now().Add(time.Minute),
		Metadata:         store.JSON(`{"trigger":"test"}`),
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if cooldown["id"] != cooldownID.String() || cooldown["site_id"] != siteID.String() || cooldown["site_model_id"] != siteModelID.String() {
		t.Fatalf("unexpected cooldown identity payload: %#v", cooldown)
	}
	if metadata, ok := cooldown["metadata"].(map[string]any); !ok || metadata["trigger"] != "test" {
		t.Fatalf("unexpected cooldown metadata: %#v", cooldown["metadata"])
	}
}
