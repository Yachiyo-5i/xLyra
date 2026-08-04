package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestProfileAccessTokenEnabledRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	rec := adminPerform(Handler{}.UpdateProfileAccessTokenEnabled, adminTestRequest(http.MethodPatch, "/api/v1/profile/access-token", `{"enabled":`))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}

func TestCurrentAdminRejectsMissingActorNilAdminAndNilAuth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		handler Handler
		actor   *auth.AdminActor
	}{
		{name: "missing actor"},
		{name: "nil admin id", handler: Handler{auth: adminAuthService()}, actor: &auth.AdminActor{Type: "session", SessionID: uuid.New()}},
		{name: "nil auth", actor: &auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodGet, "/api/v1/profile", "")
			if tc.actor != nil {
				req = req.WithContext(auth.WithAdminActor(req.Context(), *tc.actor))
			}
			rec := adminRecorder()

			admin, ok := tc.handler.currentAdmin(rec, req)

			if ok || admin.ID != uuid.Nil {
				t.Fatalf("currentAdmin = %#v, %v; want unauthorized empty admin", admin, ok)
			}
			assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestCurrentAdminRejectsNilAuthEvenWithActor(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	req := adminTestRequest(http.MethodGet, "/api/v1/profile", "")
	req = req.WithContext(auth.WithAdminActor(req.Context(), auth.AdminActor{AdminID: adminID}))
	rec := adminRecorder()

	admin, ok := (Handler{}).currentAdmin(rec, req)

	if ok {
		t.Fatalf("currentAdmin ok = true with admin %#v, want false", admin)
	}
	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestAuditLogFiltersClampInvalidPaginationToDefaults(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/audit-logs?page=not-a-number&page_size=-99&action=%20profile.update%20&actor_type=%20session%20", "")
	rec := adminRecorder()

	filters, ok := (Handler{}).auditLogFiltersFromRequest(rec, req)

	adminRequireParserOK(t, rec, ok, "audit log filters")
	if filters.Page != 1 || filters.PageSize != 50 {
		t.Fatalf("pagination = page %d size %d, want defaults 1/50", filters.Page, filters.PageSize)
	}
	if filters.Action != "profile.update" || filters.ActorType != "session" {
		t.Fatalf("trimmed filters = %#v", filters)
	}
}

func TestSiteGroupByParamRejectsWithoutServiceOrBadID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		handler Handler
		groupID string
		status  int
		code    string
	}{
		{name: "missing service", groupID: uuid.NewString(), status: http.StatusServiceUnavailable, code: "site_service_unavailable"},
		{name: "bad id", handler: Handler{sites: adminSiteService()}, groupID: "not-a-uuid", status: http.StatusBadRequest, code: "invalid_site_group_id"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/site-groups/"+tc.groupID, "", "siteGroupID", tc.groupID)
			rec := adminRecorder()

			group, ok := tc.handler.siteGroupByParam(rec, req)

			if ok || group.ID != uuid.Nil {
				t.Fatalf("siteGroupByParam = %#v, %v; want failure", group, ok)
			}
			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestSiteGroupInputHonorsExplicitDisabledAndSkipsBlankIDs(t *testing.T) {
	t.Parallel()

	enabled := false
	siteID := uuid.New()
	req := adminTestRequest(http.MethodPost, "/api/v1/site-groups", "")
	rec := adminRecorder()

	input, ok := siteGroupInputFromRequest(rec, req, siteGroupRequest{
		Name:      "Archive",
		Slug:      "archive",
		Enabled:   &enabled,
		SortOrder: -3,
		SiteIDs:   []string{" ", siteID.String(), "\t"},
	})

	adminRequireParserOK(t, rec, ok, "site group input")
	if input.Enabled {
		t.Fatal("enabled = true, want explicit false")
	}
	if len(input.SiteIDs) != 1 || input.SiteIDs[0] != siteID || input.SortOrder != -3 {
		t.Fatalf("unexpected input: %#v", input)
	}
}

func TestSiteMutationHandlersRequireSiteService(t *testing.T) {
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
		{name: "create", method: http.MethodPost, target: "/api/v1/sites", body: `{}`, call: handler.CreateSite},
		{name: "update", method: http.MethodPatch, target: "/api/v1/sites/" + siteID, body: `{}`, call: handler.UpdateSite},
		{name: "delete", method: http.MethodDelete, target: "/api/v1/sites/" + siteID, call: handler.DeleteSite},
		{name: "enabled", method: http.MethodPatch, target: "/api/v1/sites/" + siteID + "/enabled", body: `{"enabled":true}`, call: handler.UpdateSiteEnabled},
		{name: "refresh all", method: http.MethodPost, target: "/api/v1/sites/refresh", call: handler.RefreshAllSites},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(tc.method, tc.target, tc.body)
			if tc.name == "update" || tc.name == "delete" || tc.name == "enabled" {
				req = withRouteParam(req, "siteID", siteID)
			}
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
		})
	}
}

func TestSiteHandlersRejectInvalidPayloadsIDsAndMissingServices(t *testing.T) {
	t.Parallel()

	siteID := uuid.NewString()
	handlerWithSites := Handler{sites: adminSiteService()}
	for _, tc := range []struct {
		name    string
		handler Handler
		req     *http.Request
		call    func(Handler, http.ResponseWriter, *http.Request)
		status  int
		code    string
	}{
		{
			name:    "list sites invalid deleted filter",
			handler: handlerWithSites,
			req:     adminTestRequest(http.MethodGet, "/api/v1/sites?deleted=true", ""),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.ListSites(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_deleted_filter",
		},
		{
			name:    "update site invalid json after id parse",
			handler: handlerWithSites,
			req:     adminRequestWithRouteParam(http.MethodPut, "/api/v1/sites/"+siteID, `{"name":`, "siteID", siteID),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.UpdateSite(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_json",
		},
		{
			name:    "delete site requires service before route id parse",
			handler: Handler{},
			req:     adminRequestWithRouteParam(http.MethodDelete, "/api/v1/sites/not-a-uuid", "", "siteID", "not-a-uuid"),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.DeleteSite(w, r) },
			status:  http.StatusServiceUnavailable,
			code:    "site_service_unavailable",
		},
		{
			name:    "update enabled invalid site id",
			handler: handlerWithSites,
			req:     adminRequestWithRouteParam(http.MethodPatch, "/api/v1/sites/not-a-uuid/enabled", `{"enabled":true}`, "siteID", "not-a-uuid"),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.UpdateSiteEnabled(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_site_id",
		},
		{
			name:    "create site invalid json",
			handler: handlerWithSites,
			req:     adminTestRequest(http.MethodPost, "/api/v1/sites", `{"name":`),
			call:    func(h Handler, w http.ResponseWriter, r *http.Request) { h.CreateSite(w, r) },
			status:  http.StatusBadRequest,
			code:    "invalid_json",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(func(w http.ResponseWriter, r *http.Request) {
				tc.call(tc.handler, w, r)
			}, tc.req)

			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestNestedSiteHandlersValidateIDsAndDecodeBeforeServiceWork(t *testing.T) {
	t.Parallel()

	siteID := uuid.NewString()
	apiKeyID := uuid.NewString()
	handler := Handler{}
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		params map[string]string
		call   func(http.ResponseWriter, *http.Request)
		code   string
	}{
		{
			name:   "update api key secret bad api key id",
			method: http.MethodPatch,
			target: "/api/v1/sites/" + siteID + "/api-keys/bad-id/secret",
			params: map[string]string{"siteID": siteID, "apiKeyID": "bad-id"},
			call:   handler.UpdateSiteAPIKeySecret,
			code:   "invalid_api_key_id",
		},
		{
			name:   "update site api key model bad api key id",
			method: http.MethodPatch,
			target: "/api/v1/sites/" + siteID + "/api-keys/bad-id/models",
			params: map[string]string{"siteID": siteID, "apiKeyID": "bad-id"},
			call:   handler.UpdateSiteAPIKeyModel,
			code:   "invalid_api_key_id",
		},
		{
			name:   "create site model bad credential id",
			method: http.MethodPost,
			target: "/api/v1/sites/" + siteID + "/models",
			body:   `{"canonical_model_id":"` + uuid.NewString() + `","site_credential_ids":["bad-id"]}`,
			params: map[string]string{"siteID": siteID},
			call:   handler.CreateSiteModel,
			code:   "invalid_site_credential_id",
		},
		{
			name:   "update api key invalid json",
			method: http.MethodPatch,
			target: "/api/v1/sites/" + siteID + "/api-keys/" + apiKeyID,
			body:   `{"enabled":`,
			params: map[string]string{"siteID": siteID, "apiKeyID": apiKeyID},
			call:   handler.UpdateSiteAPIKey,
			code:   "invalid_json",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParams(tc.method, tc.target, tc.body, tc.params)
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestSitePayloadHelpersFillStateUsageAndDefaults(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 24, 9, 30, 0, 0, time.UTC)
	siteID := uuid.New()
	statePayload := (Handler{}).sitePayloadWithState(store.Site{
		ID:       siteID,
		Name:     "NewAPI",
		Slug:     "newapi",
		SiteType: "newapi",
		Enabled:  true,
	}, store.SiteState{
		SiteID:      siteID,
		SyncStatus:  "synced",
		APIKeyCount: 2,
		ModelCount:  4,
		RawStatus:   store.JSON(`{"phase":"done"}`),
		UserSummary: store.JSON(`{"quota":10}`),
		Pricing:     store.JSON(`{"groups":1}`),
		Checkin:     store.JSON(`{"ok":true}`),
		UpdatedAt:   now,
	})

	if statePayload["model_count"] != 4 || statePayload["api_key_count"] != 2 {
		t.Fatalf("state counts missing: %#v", statePayload)
	}
	syncState := statePayload["sync_state"].(map[string]any)
	for key, want := range map[string]any{"raw_status": "phase", "user_summary": "quota", "pricing": "groups", "checkin": "ok"} {
		value, ok := syncState[key].(map[string]any)
		if !ok || value[want.(string)] == nil {
			t.Fatalf("sync_state[%s] = %#v", key, syncState[key])
		}
	}

	usage := siteUsagePayload(store.SiteUsageSummaryRow{
		RequestCount:     8,
		SuccessCount:     6,
		FailedCount:      2,
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		EstimatedCost:    0.25,
		Currency:         "USD",
		FirstRequestAt:   now,
		LastRequestAt:    now.Add(time.Hour),
	})
	if usage["request_count"] != int64(8) || usage["currency"] != "USD" || usage["first_request_at"] == nil {
		t.Fatalf("usage payload = %#v", usage)
	}
}

func TestSiteAPIKeyPayloadFromStateUsesFallbacksForLegacyNames(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	payload := (Handler{}).siteAPIKeyPayloadFromState(
		store.Site{ID: uuid.New(), SiteType: "openai"},
		sitepkg.APIKeyCredential{
			Credential:    store.SiteCredential{ID: credentialID},
			Name:          "默认 Key",
			UpstreamID:    7,
			Enabled:       true,
			MaskedSecret:  "sk-***",
			SecretMissing: true,
			Meta: map[string]any{
				"status":       "manual",
				"remain_quota": float64(9),
				"used_quota":   float64(1),
				"group":        "meta-group",
			},
		},
		store.SiteAPIKeyState{},
		[]store.SiteAPIKeyModel{
			{UpstreamModelName: "gpt-5", Enabled: true, Available: true},
			{UpstreamModelName: "disabled", Enabled: false, Available: true},
			{UpstreamModelName: "hidden", Enabled: true, Available: false},
		},
		"Apikey 3",
	)

	if payload["name"] != "Apikey 3" || payload["upstream_id"] != 7 || payload["status"] != "manual" {
		t.Fatalf("fallback fields = %#v", payload)
	}
	if payload["secret_missing"] != true || payload["can_complete"] != true || payload["group"] != "meta-group" {
		t.Fatalf("secret/group fields = %#v", payload)
	}
	models := payload["models"].([]string)
	if len(models) != 2 || models[0] != "gpt-5" || models[1] != "disabled" {
		t.Fatalf("models = %#v", models)
	}
	usage := payload["usage"].(map[string]any)
	data := usage["data"].(map[string]any)
	if data["total_granted"] != 10 || data["total_available"] != 9 {
		t.Fatalf("usage fallback = %#v", usage)
	}
}

func TestSiteMetaHelpersAndPricingPayloads(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	sitePayload := sitePayload(store.Site{
		ID:       siteID,
		Name:     "Codex Team",
		Slug:     "codex-team",
		SiteType: "codex",
		Enabled:  true,
		Meta: store.JSON(`{
			"proxy_id":" direct ",
			"request_headers":[{"key":"X-Team","value":"core"}],
			"oauth_provider":"codex",
			"oauth_connection_id":"conn-1",
			"oauth_account_id":"acct-1",
			"oauth_email":"team@example.test",
			"oauth_plan_type":"team"
		}`),
	})
	if sitePayload["proxy_id"] != "direct" || sitePayload["request_headers"] == nil {
		t.Fatalf("site metadata fields missing: %#v", sitePayload)
	}
	account := sitePayload["oauth_account"].(map[string]any)
	if account["provider"] != "codex" || account["email"] != "team@example.test" {
		t.Fatalf("oauth account = %#v", account)
	}

	pricing := siteModelPricingPayload(store.SiteModelPricing{
		ID:         uuid.New(),
		SiteID:     siteID,
		ModelName:  "gpt-5",
		GroupName:  "default",
		Currency:   "USD",
		GroupRatio: 2,
		InputValue: validNullFloat(3),
		CacheRatio: validNullFloat(0.5),
		ImageRatio: validNullFloat(4),
		Available:  true,
		Raw:        store.JSON(`{"source":"pricing-fixture"}`),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, store.Site{ID: siteID, Name: "Codex", Slug: "codex", SiteType: "codex", Enabled: true})
	if pricing["site_name"] != "Codex" || pricing["cache_input_value"] != float64(1.5) || pricing["image_input_value"] != float64(12) {
		t.Fatalf("pricing payload = %#v", pricing)
	}
	raw := pricing["raw"].(map[string]any)
	if raw["source"] != "pricing-fixture" {
		t.Fatalf("pricing raw = %#v", raw)
	}
	if _, err := json.Marshal(pricing["calculation"]); err != nil {
		t.Fatalf("calculation should marshal: %v", err)
	}
}

func TestRouteAndHealthPayloadHelpersPreserveMetadataAndStatus(t *testing.T) {
	t.Parallel()

	now := time.Now().Add(time.Minute)
	cooldown := routeCooldownPayload(store.RouteCooldown{
		ID:          uuid.New(),
		SiteID:      uuid.New(),
		Scope:       "site",
		Source:      "manual",
		Reason:      "manual test cooldown",
		ActiveUntil: now,
		Metadata:    store.JSON(`{"trigger":"test"}`),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})
	if cooldown["remaining_seconds"].(int) <= 0 {
		t.Fatalf("remaining_seconds = %#v, want positive", cooldown["remaining_seconds"])
	}
	metadata := cooldown["metadata"].(map[string]any)
	if metadata["trigger"] != "test" {
		t.Fatalf("cooldown metadata = %#v", metadata)
	}

	health := siteHealthStatePayload(store.SiteHealthState{
		SiteID:   uuid.New(),
		Status:   "unknown",
		Metadata: store.JSON(`{"source":"fallback"}`),
	})
	if health["status"] != "unknown" {
		t.Fatalf("health payload = %#v", health)
	}
}

func validNullFloat(value float64) sql.NullFloat64 {
	return sql.NullFloat64{Float64: value, Valid: true}
}
