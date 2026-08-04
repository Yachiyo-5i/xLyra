package admin

import (
	"database/sql"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
)

func TestListModelsRequiresCatalogService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListModels, adminTestRequest(http.MethodGet, "/api/v1/models", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "catalog_service_unavailable")
}

func TestCanonicalModelHandlersRequireCatalogService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	modelID := uuid.New().String()
	siteModelID := uuid.New().String()
	aliasID := uuid.New().String()
	cases := []struct {
		name   string
		method string
		target string
		body   string
		req    *http.Request
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "create model", method: http.MethodPost, target: "/api/v1/models", body: `{"model_key":"gpt-5"}`, call: handler.CreateModel},
		{name: "update model", req: adminRequestWithRouteParam(http.MethodPatch, "/api/v1/models/"+modelID, "", "modelID", modelID), body: `{"model_key":"gpt-5"}`, call: handler.UpdateModel},
		{name: "delete model", req: adminRequestWithRouteParam(http.MethodDelete, "/api/v1/models/"+modelID, "", "modelID", modelID), call: handler.DeleteModel},
		{name: "create alias", req: adminRequestWithRouteParam(http.MethodPost, "/api/v1/models/"+modelID+"/aliases", "", "modelID", modelID), body: `{"alias":"gpt-latest"}`, call: handler.CreateModelAlias},
		{name: "delete alias", req: adminRequestWithRouteParam(http.MethodDelete, "/api/v1/models/"+modelID+"/aliases/"+aliasID, "", "modelID", modelID), call: handler.DeleteModelAlias},
		{name: "bind site model", req: adminRequestWithRouteParam(http.MethodPatch, "/api/v1/site-models/"+siteModelID+"/canonical", "", "siteModelID", siteModelID), body: `{"canonical_model_id":"` + modelID + `"}`, call: handler.BindSiteModelCanonical},
		{name: "matrix", req: adminRequestWithRouteParam(http.MethodGet, "/api/v1/models/"+modelID+"/matrix", "", "modelID", modelID), call: handler.GetModelMatrix},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := tc.req
			if req == nil {
				req = adminTestRequest(tc.method, tc.target, tc.body)
			} else if tc.body != "" {
				req = req.Clone(req.Context())
				req.Body = ioNopCloserString(tc.body)
			}
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "catalog_service_unavailable")
		})
	}
}

func TestCanonicalModelHandlersRejectBadInputBeforeCatalogMutation(t *testing.T) {
	t.Parallel()

	handler := Handler{catalog: catalog.NewService(nil)}
	modelID := uuid.New().String()
	siteModelID := uuid.New().String()

	rec := adminPerform(handler.CreateModel, adminTestRequest(http.MethodPost, "/api/v1/models", `{"model_key":`))
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")

	req := adminRequestWithRouteParam(http.MethodPatch, "/api/v1/models/not-a-uuid", "", "modelID", "not-a-uuid")
	rec = adminPerform(handler.UpdateModel, req)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_model_id")

	req = adminRequestWithRouteParam(http.MethodPost, "/api/v1/models/"+modelID+"/aliases", "", "modelID", modelID)
	req.Body = ioNopCloserString(`{"alias":`)
	rec = adminPerform(handler.CreateModelAlias, req)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")

	req = adminRequestWithRouteParam(http.MethodPatch, "/api/v1/site-models/"+siteModelID+"/canonical", "", "siteModelID", siteModelID)
	req.Body = ioNopCloserString(`{"canonical_model_id":"not-a-uuid"}`)
	rec = adminPerform(handler.BindSiteModelCanonical, req)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_canonical_model_id")
}

func TestModelHandlersRejectInvalidRouteIDsBeforeCatalogWork(t *testing.T) {
	t.Parallel()

	handler := Handler{catalog: catalog.NewService(nil)}
	modelID := uuid.NewString()

	cases := []struct {
		name   string
		req    *http.Request
		call   func(http.ResponseWriter, *http.Request)
		status int
		code   string
	}{
		{
			name:   "delete alias invalid alias id",
			req:    adminRequestWithRouteParam(http.MethodDelete, "/api/v1/models/"+modelID+"/aliases/bad-id", "", "modelID", modelID),
			call:   handler.DeleteModelAlias,
			status: http.StatusBadRequest,
			code:   "invalid_alias_id",
		},
		{
			name:   "matrix invalid model id",
			req:    adminRequestWithRouteParam(http.MethodGet, "/api/v1/models/bad-id/matrix", "", "modelID", "bad-id"),
			call:   handler.GetModelMatrix,
			status: http.StatusBadRequest,
			code:   "invalid_model_id",
		},
		{
			name:   "bind site model invalid site model id",
			req:    adminRequestWithRouteParam(http.MethodPatch, "/api/v1/site-models/bad-id/canonical", "", "siteModelID", "bad-id"),
			call:   handler.BindSiteModelCanonical,
			status: http.StatusBadRequest,
			code:   "invalid_site_model_id",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, tc.req)

			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestCanonicalModelRequestToInputPreservesFields(t *testing.T) {
	t.Parallel()

	input := canonicalModelRequest{
		ModelKey:    "gpt-5",
		DisplayName: "GPT-5",
		Provider:    "openai",
		Category:    "chat",
		Capabilities: map[string]any{
			"reasoning": true,
		},
		Status: "active",
	}.toInput()

	if input.ModelKey != "gpt-5" || input.DisplayName != "GPT-5" || input.Provider != "openai" || input.Category != "chat" || input.Status != "active" {
		t.Fatalf("unexpected canonical input: %#v", input)
	}
	if input.Capabilities["reasoning"] != true {
		t.Fatalf("capabilities were not preserved: %#v", input.Capabilities)
	}
}

func TestCanonicalModelItemPayloadIncludesAliasesStatsAndProviderIcon(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	aliasID := uuid.New()
	createdAt := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	payload := canonicalModelItemPayload(catalog.CanonicalModelItem{
		Model: store.CanonicalModelWithStats{
			CanonicalModel: store.CanonicalModel{
				ID:           modelID,
				ModelKey:     "claude-sonnet-4",
				DisplayName:  "Claude Sonnet 4",
				Provider:     "anthropic",
				Category:     "chat",
				Capabilities: store.JSON(`{"supported_endpoint_types":["anthropic-messages"]}`),
				Status:       "active",
				CreatedAt:    createdAt,
				UpdatedAt:    createdAt,
			},
			SiteModelCount: 3,
			SiteCount:      2,
		},
		Aliases: []store.CanonicalModelAlias{{
			ID:               aliasID,
			CanonicalModelID: modelID,
			Alias:            "claude-4-sonnet",
			NormalizedAlias:  "claude-4-sonnet",
			Source:           "manual",
			CreatedAt:        createdAt,
			UpdatedAt:        createdAt,
		}},
	})

	if payload["id"] != modelID.String() || payload["model_key"] != "claude-sonnet-4" {
		t.Fatalf("unexpected canonical payload identity: %#v", payload)
	}
	if payload["icon_url"] != "/brand-icons/anthropic.png" {
		t.Fatalf("icon_url = %#v, want anthropic icon", payload["icon_url"])
	}
	if payload["site_model_count"] != 3 || payload["site_count"] != 2 {
		t.Fatalf("unexpected stats: %#v", payload)
	}

	capabilities, ok := payload["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected capabilities: %#v", payload["capabilities"])
	}
	endpoints, ok := capabilities["supported_endpoint_types"].([]any)
	if !ok || len(endpoints) != 1 || endpoints[0] != "anthropic-messages" {
		t.Fatalf("unexpected capability endpoints: %#v", capabilities)
	}

	aliases, ok := payload["aliases"].([]map[string]any)
	if !ok || len(aliases) != 1 {
		t.Fatalf("expected one alias payload, got %#v", payload["aliases"])
	}
	if aliases[0]["id"] != aliasID.String() || aliases[0]["source"] != "manual" {
		t.Fatalf("unexpected alias payload: %#v", aliases[0])
	}
}

func TestCanonicalModelMatrixPayloadGroupsPricingBySiteModel(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	createdAt := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	syncedAt := createdAt.Add(time.Minute)
	payload := canonicalModelMatrixPayload(catalog.Matrix{
		Model: store.CanonicalModel{
			ID:          modelID,
			ModelKey:    "gpt-5",
			DisplayName: "GPT-5",
			Provider:    "openai",
			Status:      "active",
		},
		Rows: []store.CanonicalModelMatrixRow{
			{
				SiteID:               siteID,
				SiteName:             "Primary",
				SiteSlug:             "primary",
				SiteType:             "openai",
				SiteEnabled:          true,
				SiteModelID:          siteModelID,
				UpstreamModelName:    "gpt-5",
				DisplayName:          "GPT-5",
				ModelStatus:          "active",
				MatchSource:          "alias",
				MatchConfidence:      95,
				MatchedAt:            sql.NullTime{Time: syncedAt, Valid: true},
				APIKeyCount:          3,
				AvailableAPIKeyCount: 2,
				GroupName:            sql.NullString{String: "default", Valid: true},
				Currency:             sql.NullString{String: "USD", Valid: true},
				InputValue:           sql.NullFloat64{Float64: 1.25, Valid: true},
				OutputValue:          sql.NullFloat64{Float64: 2.5, Valid: true},
				BillingType:          sql.NullString{String: "tokens", Valid: true},
				PricingAvailable:     sql.NullBool{Bool: true, Valid: true},
				PricingLastSyncedAt:  sql.NullTime{Time: syncedAt, Valid: true},
				SiteModelCreatedAt:   createdAt,
				SiteModelUpdatedAt:   syncedAt,
			},
			{
				SiteID:              siteID,
				SiteModelID:         siteModelID,
				GroupName:           sql.NullString{String: "fast", Valid: true},
				Currency:            sql.NullString{String: "USD", Valid: true},
				PerRequestValue:     sql.NullFloat64{Float64: 0.01, Valid: true},
				BillingType:         sql.NullString{String: "per_request", Valid: true},
				PricingAvailable:    sql.NullBool{Bool: true, Valid: true},
				PricingLastSyncedAt: sql.NullTime{Time: syncedAt, Valid: true},
			},
		},
	})

	items, ok := payload["items"].([]map[string]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected one grouped matrix item, got %#v", payload["items"])
	}
	if items[0]["site_id"] != siteID.String() || items[0]["site_model_id"] != siteModelID.String() {
		t.Fatalf("unexpected matrix item identity: %#v", items[0])
	}
	pricing, ok := items[0]["pricing"].([]map[string]any)
	if !ok || len(pricing) != 2 {
		t.Fatalf("expected two pricing rows grouped under one site model, got %#v", items[0]["pricing"])
	}
	if pricing[0]["group_name"] != "default" || pricing[1]["group_name"] != "fast" {
		t.Fatalf("unexpected pricing groups: %#v", pricing)
	}
	meta, _ := payload["meta"].(map[string]any)
	if meta["count"] != 1 {
		t.Fatalf("matrix count = %#v, want 1", meta["count"])
	}
}

func TestProviderIconURL(t *testing.T) {
	t.Parallel()

	if got := providerIconURL("moonshotai-cn"); got != "/brand-icons/moonshot.png" {
		t.Fatalf("moonshot icon = %q", got)
	}
	if got := providerIconURL("flux"); got != "/brand-icons/flux-dark.png" {
		t.Fatalf("flux icon = %q", got)
	}
	if got := providerIconURL("hunyuan"); got != "/brand-icons/hunyuan-dark.png" {
		t.Fatalf("hunyuan icon = %q", got)
	}
	if got := providerIconURL("unknown-provider"); got != "" {
		t.Fatalf("unknown provider icon = %q, want empty", got)
	}
}

func ioNopCloserString(value string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(value))
}
