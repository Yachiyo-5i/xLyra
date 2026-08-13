package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSiteHandlersRejectInvalidSiteIDBeforeService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name string
		req  *http.Request
		call func(http.ResponseWriter, *http.Request)
		code string
	}{
		{
			name: "list api keys",
			req:  adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/not-a-uuid/api-keys", "", "siteID", "not-a-uuid"),
			call: handler.ListSiteAPIKeys,
			code: "invalid_site_id",
		},
		{
			name: "list pricing",
			req:  adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/not-a-uuid/pricing", "", "siteID", "not-a-uuid"),
			call: handler.ListSitePricing,
			code: "invalid_site_id",
		},
		{
			name: "get",
			req:  adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/bad-id", "", "siteID", "bad-id"),
			call: handler.GetSite,
			code: "invalid_site_id",
		},
		{
			name: "validate",
			req:  adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/validate", "", "siteID", "bad-id"),
			call: handler.ValidateSite,
			code: "invalid_site_id",
		},
		{
			name: "check health",
			req:  adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/health/check", "", "siteID", "bad-id"),
			call: handler.CheckSiteHealth,
			code: "invalid_site_id",
		},
		{
			name: "get health",
			req:  adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/bad-id/health", "", "siteID", "bad-id"),
			call: handler.GetSiteHealth,
			code: "invalid_site_id",
		},
		{
			name: "refresh",
			req:  adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/refresh", "", "siteID", "bad-id"),
			call: handler.RefreshSite,
			code: "invalid_site_id",
		},
		{
			name: "sync models",
			req:  adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/models/sync", "", "siteID", "bad-id"),
			call: handler.SyncSiteModels,
			code: "invalid_site_id",
		},
		{
			name: "list models",
			req:  adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/bad-id/models", "", "siteID", "bad-id"),
			call: handler.ListSiteModels,
			code: "invalid_site_id",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, tc.req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestNestedSiteHandlersRejectInvalidRouteIDsBeforeServiceWork(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.NewString()
	cases := []struct {
		name        string
		method      string
		target      string
		routeKey    string
		routeValue  string
		call        func(http.ResponseWriter, *http.Request)
		wantCode    string
		wantStatus  int
		requestBody string
	}{
		{
			name:       "delete site model invalid model id",
			method:     http.MethodDelete,
			target:     "/api/v1/sites/" + siteID + "/models/bad-id",
			routeKey:   "modelID",
			routeValue: "bad-id",
			call:       handler.DeleteSiteModel,
			wantCode:   "invalid_model_id",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "delete api key invalid api key id",
			method:     http.MethodDelete,
			target:     "/api/v1/sites/" + siteID + "/api-keys/bad-id",
			routeKey:   "apiKeyID",
			routeValue: "bad-id",
			call:       handler.DeleteSiteAPIKey,
			wantCode:   "invalid_api_key_id",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:        "update api key invalid json",
			method:      http.MethodPatch,
			target:      "/api/v1/sites/" + siteID + "/api-keys/" + uuid.NewString(),
			routeKey:    "apiKeyID",
			routeValue:  uuid.NewString(),
			call:        handler.UpdateSiteAPIKey,
			wantCode:    "invalid_json",
			wantStatus:  http.StatusBadRequest,
			requestBody: `{`,
		},
		{
			name:        "update api key model invalid json",
			method:      http.MethodPatch,
			target:      "/api/v1/sites/" + siteID + "/api-keys/" + uuid.NewString() + "/models",
			routeKey:    "apiKeyID",
			routeValue:  uuid.NewString(),
			call:        handler.UpdateSiteAPIKeyModel,
			wantCode:    "invalid_json",
			wantStatus:  http.StatusBadRequest,
			requestBody: `{`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(tc.method, tc.target, tc.requestBody, "siteID", siteID)
			req = withRouteParam(req, tc.routeKey, tc.routeValue)
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, tc.wantStatus, tc.wantCode)
		})
	}
}

func TestSiteAPIKeyHandlersRejectBlankSecretBeforeServiceWork(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.NewString()
	apiKeyID := uuid.NewString()
	cases := []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		params map[string]string
	}{
		{
			name:   "create api key",
			method: http.MethodPost,
			target: "/api/v1/sites/" + siteID + "/api-keys",
			body:   `{"api_key":"  ","secret":"\t"}`,
			call:   handler.CreateSiteAPIKey,
			params: map[string]string{"siteID": siteID},
		},
		{
			name:   "update api key secret",
			method: http.MethodPatch,
			target: "/api/v1/sites/" + siteID + "/api-keys/" + apiKeyID + "/secret",
			body:   `{"api_key":"  ","secret":"\n"}`,
			call:   handler.UpdateSiteAPIKeySecret,
			params: map[string]string{"siteID": siteID, "apiKeyID": apiKeyID},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParams(tc.method, tc.target, tc.body, tc.params)
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_api_key")
		})
	}
}

func TestListSiteHealthUsesUnknownStateWhenMissing(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	handler := Handler{sites: sitepkg.NewService(adminSiteStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, Name: "No State", Slug: "no-state", SiteType: "openai", BaseURL: "https://api.example.com", Status: "active", Enabled: true}}
			tx.RowsAffected = 1
		case *[]store.SiteHealthState:
			*dest = []store.SiteHealthState{}
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}), adminTestMasterKey)}
	rec := adminPerform(handler.ListSiteHealth, adminTestRequest(http.MethodGet, "/api/v1/sites/health", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Items []struct {
			Health map[string]any `json:"health"`
		} `json:"items"`
		Meta map[string]any `json:"meta"`
	}](t, rec)
	if len(body.Items) != 1 || body.Meta["count"] != float64(1) {
		t.Fatalf("unexpected health list response: %#v", body)
	}
	health := body.Items[0].Health
	if health["site_id"] != siteID.String() || health["status"] != "unknown" {
		t.Fatalf("health fallback = %#v, want unknown state for site", health)
	}
	if meta, ok := health["metadata"].(map[string]any); !ok || len(meta) != 0 {
		t.Fatalf("metadata = %#v, want empty object", health["metadata"])
	}
}

func TestUpdateSiteClearsRequestHeadersAndPreservesOtherMeta(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	existing := store.Site{
		ID:              siteID,
		Name:            "Headers Site",
		Slug:            "headers-site",
		SiteType:        "openai",
		BaseURL:         "https://api.example.com",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
		Meta:            store.JSON(`{"keep":true,"request_headers":[{"key":"X-Trace","value":"enabled"}]}`),
	}
	var saved store.Site
	db := adminTransactionGormWithCallbacks(t, adminGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				if saved.ID != uuid.Nil {
					*dest = saved
				} else {
					*dest = existing
				}
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *store.SiteState:
				tx.AddError(gorm.ErrRecordNotFound)
			case *[]store.SiteModel:
				*dest = []store.SiteModel{}
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{}
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			item, ok := tx.Statement.Dest.(*store.Site)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			saved = *item
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	handler := Handler{sites: sitepkg.NewService(adminStoreWithGorm(db), adminTestMasterKey)}
	req := adminRequestWithRouteParam(
		http.MethodPut,
		"/api/v1/sites/"+siteID.String(),
		`{"name":"Headers Site","slug":"headers-site","site_type":"openai","base_url":"https://api.example.com","enabled":true,"routing_priority":1,"request_headers":[],"skip_refresh":true}`,
		"siteID",
		siteID.String(),
	)
	rec := adminPerform(handler.UpdateSite, req)

	adminAssertStatus(t, rec, http.StatusOK)
	meta := map[string]any{}
	if err := json.Unmarshal(saved.Meta, &meta); err != nil {
		t.Fatalf("decode saved meta: %v", err)
	}
	if _, ok := meta["request_headers"]; ok || meta["keep"] != true {
		t.Fatalf("saved meta = %#v, want request headers removed and other metadata preserved", meta)
	}
}

func TestListAllSitePricingsIncludesSiteFieldsAndSkipsMissingSite(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	missingSiteID := uuid.New()
	handler := Handler{sites: sitepkg.NewService(adminSiteStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteModelPricing:
			*dest = []store.SiteModelPricing{
				{
					ID:              uuid.New(),
					SiteID:          siteID,
					ModelName:       "gpt-priced",
					GroupName:       "default",
					BillingType:     "token",
					Currency:        "USD",
					GroupRatio:      1,
					InputValue:      sql.NullFloat64{Float64: 1.25, Valid: true},
					OutputValue:     sql.NullFloat64{Float64: 2.5, Valid: true},
					PricingSource:   "sync",
					Available:       true,
					Raw:             store.JSON(`{}`),
					LastSyncedAt:    sql.NullTime{Time: time.Unix(100, 0).UTC(), Valid: true},
					ManualUpdatedAt: sql.NullTime{},
				},
				{
					ID:            uuid.New(),
					SiteID:        missingSiteID,
					ModelName:     "orphan",
					GroupName:     "default",
					BillingType:   "token",
					Currency:      "USD",
					GroupRatio:    1,
					PricingSource: "sync",
					Available:     true,
					Raw:           store.JSON(`{}`),
				},
			}
			tx.RowsAffected = 2
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, Name: "Priced Site", Slug: "priced-site", SiteType: "openai", Enabled: true}}
			tx.RowsAffected = 1
		case *[]store.SiteCredential, *[]store.SiteAPIKeyState, *[]store.SiteAPIKeyModel:
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}), adminTestMasterKey)}
	rec := adminPerform(handler.ListAllSitePricings, adminTestRequest(http.MethodGet, "/api/v1/site-pricing", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Items []map[string]any `json:"items"`
		Meta  map[string]any   `json:"meta"`
	}](t, rec)
	if len(body.Items) != 2 || body.Meta["count"] != float64(2) {
		t.Fatalf("unexpected pricing list response: %#v", body)
	}
	var priced, orphan map[string]any
	for _, item := range body.Items {
		switch item["site_id"] {
		case siteID.String():
			priced = item
		case missingSiteID.String():
			orphan = item
		}
	}
	if priced == nil || priced["site_name"] != "Priced Site" || priced["site_slug"] != "priced-site" || priced["input_value"] != float64(1.25) {
		t.Fatalf("priced payload missing site or pricing fields: %#v", priced)
	}
	if orphan == nil {
		t.Fatalf("missing orphan pricing payload in %#v", body.Items)
	}
	if _, ok := orphan["site_name"]; ok {
		t.Fatalf("orphan pricing should not invent site fields: %#v", orphan)
	}
}

func TestListSiteAPIKeysEmptyWhenNoCredentials(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	handler := Handler{sites: sitepkg.NewService(adminSiteStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{ID: siteID, Name: "Keys Site", Slug: "keys-site", SiteType: "openai", BaseURL: "https://api.example.com", Status: "active", Enabled: true}
			tx.RowsAffected = 1
		case *[]store.SiteCredential:
			*dest = []store.SiteCredential{}
		case *[]store.SiteAPIKeyState:
			*dest = []store.SiteAPIKeyState{}
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}), adminTestMasterKey)}
	req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/"+siteID.String()+"/api-keys", "", "siteID", siteID.String())
	rec := adminPerform(handler.ListSiteAPIKeys, req)
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Items []map[string]any `json:"items"`
		Meta  map[string]any   `json:"meta"`
	}](t, rec)
	if len(body.Items) != 0 || body.Meta["count"] != float64(0) {
		t.Fatalf("api key empty response = %#v, want count 0", body)
	}
}

func TestListSitePricingEmptyPayload(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	handler := Handler{sites: sitepkg.NewService(adminSiteStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{ID: siteID, Name: "Pricing Site", Slug: "pricing-site", SiteType: "openai", BaseURL: "https://api.example.com", Status: "active", Enabled: true}
			tx.RowsAffected = 1
		case *store.SiteState:
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]store.SitePricingGroup:
			*dest = []store.SitePricingGroup{}
		case *[]store.SiteModelPricing:
			*dest = []store.SiteModelPricing{}
		case *[]store.SiteCredential, *[]store.SiteAPIKeyState, *[]store.SiteAPIKeyModel:
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}), adminTestMasterKey)}
	req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/"+siteID.String()+"/pricing", "", "siteID", siteID.String())
	rec := adminPerform(handler.ListSitePricing, req)
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Site   map[string]any   `json:"site"`
		Groups []map[string]any `json:"groups"`
		Items  []map[string]any `json:"items"`
		Meta   map[string]any   `json:"meta"`
	}](t, rec)
	if body.Site["id"] != siteID.String() || body.Site["name"] != "Pricing Site" {
		t.Fatalf("site payload = %#v", body.Site)
	}
	if len(body.Groups) != 0 || len(body.Items) != 0 || body.Meta["group_count"] != float64(0) || body.Meta["count"] != float64(0) {
		t.Fatalf("pricing empty payload = %#v", body)
	}
}

func TestSiteSetupResponseRefreshErrorIncludesNewAPISummaries(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	refreshErr := errors.New("refresh refused")
	handler := Handler{sites: sitepkg.NewService(adminSiteStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{ID: siteID, Name: "Refreshed", Slug: "refreshed", SiteType: "newapi", BaseURL: "https://newapi.example.com", Status: "active", Enabled: true}
			tx.RowsAffected = 1
		case *store.SiteState:
			*dest = store.SiteState{SiteID: siteID, SyncStatus: "offline", UserSummary: store.JSON(`{"quota":7}`)}
			tx.RowsAffected = 1
		default:
			tx.AddError(refreshErr)
		}
	}), adminTestMasterKey)}
	req := adminTestRequest(http.MethodPost, "/api/v1/sites", "")

	response := handler.siteSetupResponse(req, store.Site{ID: siteID, Name: "Created", Slug: "created", SiteType: "newapi", BaseURL: "https://newapi.example.com", Status: "active", Enabled: true})

	message, _ := response["message"].(string)
	if response["ok"] != false || !strings.Contains(message, refreshErr.Error()) {
		t.Fatalf("refresh error response = %#v", response)
	}
	validation := response["validation"].(map[string]any)
	modelSync := response["model_sync"].(map[string]any)
	if validation["ok"] != false || modelSync["ok"] != false {
		t.Fatalf("validation/model_sync should be failed: validation=%#v model_sync=%#v", validation, modelSync)
	}
	newAPI := response["newapi"].(map[string]any)
	apiKeysSummary := newAPI["api_keys_summary"].(map[string]any)
	userSummary := newAPI["user_summary"].(map[string]any)
	if apiKeysSummary["ok"] != false || apiKeysSummary["count"] != 0 {
		t.Fatalf("api key summary = %#v", apiKeysSummary)
	}
	if userSummary["ok"] != false {
		t.Fatalf("user summary = %#v", userSummary)
	}
	if _, ok := userSummary["data"].(map[string]any); !ok {
		t.Fatalf("user summary data = %#v", userSummary["data"])
	}
}

func TestSiteAPIKeyDefaultNameFallsBackWhenCredentialMissing(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	knownCredentialID := uuid.New()
	handler := Handler{sites: sitepkg.NewService(adminSiteStoreWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteCredential:
			*dest = []store.SiteCredential{{ID: knownCredentialID, SiteID: siteID, CredentialType: "api_key"}}
			tx.RowsAffected = 1
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	}), adminTestMasterKey)}

	if got := handler.siteAPIKeyDefaultName(context.Background(), siteID, uuid.New()); got != "Apikey 1" {
		t.Fatalf("missing credential default name = %q, want Apikey 1", got)
	}
}

func adminSiteStoreWithQueryCallback(t *testing.T, query func(*gorm.DB)) *store.Store {
	t.Helper()

	return adminStoreWithCallbacks(t, adminGormCallbacks{
		query: query,
		create: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
		deleteCallback: func(tx *gorm.DB) {
			tx.RowsAffected = 1
		},
	})
}
