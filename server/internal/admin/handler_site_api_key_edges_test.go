package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/newapi"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestFreshAPIKeyMetaPatchKeepsChangedWhitelistedFreshFields(t *testing.T) {
	t.Parallel()

	if patch := freshAPIKeyMetaPatch(map[string]any{"name": "kept"}, newapi.UserAPIKey{}); patch != nil {
		t.Fatalf("empty fresh key patch = %#v, want nil", patch)
	}

	patch := freshAPIKeyMetaPatch(map[string]any{
		"upstream_id":          json.Number("42"),
		"name":                 "Primary",
		"status":               float64(1),
		"remain_quota":         int64(20),
		"model_limits_enabled": false,
	}, newapi.UserAPIKey{
		ID:     42,
		Name:   "  Primary  ",
		Status: 1,
		Raw: map[string]any{
			"remain_quota":         float64(20),
			"model_limits_enabled": true,
			"expired_time":         json.Number("0"),
			"ignored":              "not persisted",
		},
	})

	if _, ok := patch["upstream_id"]; ok {
		t.Fatalf("numeric-equivalent upstream_id should be skipped: %#v", patch)
	}
	if _, ok := patch["name"]; ok {
		t.Fatalf("trimmed equivalent name should be skipped: %#v", patch)
	}
	if _, ok := patch["status"]; ok {
		t.Fatalf("numeric-equivalent status should be skipped: %#v", patch)
	}
	if _, ok := patch["remain_quota"]; ok {
		t.Fatalf("numeric-equivalent remain_quota should be skipped: %#v", patch)
	}
	if patch["model_limits_enabled"] != true || patch["expired_time"] != json.Number("0") {
		t.Fatalf("changed whitelisted fields missing from patch: %#v", patch)
	}
	if _, ok := patch["ignored"]; ok {
		t.Fatalf("unlisted raw field should not be patched: %#v", patch)
	}
}

func TestFreshNewAPIKeyByIDReturnsEmptyKeyWhenLookupCannotRun(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminTestRequest(http.MethodGet, "/api/v1/sites/site-id/api-keys", "")
	siteID := uuid.New()

	for _, tc := range []struct {
		name       string
		siteType   string
		upstreamID int
	}{
		{name: "missing upstream id", siteType: "newapi", upstreamID: 0},
		{name: "not a newapi site", siteType: "openai", upstreamID: 7},
		{name: "newapi client unavailable", siteType: "newapi", upstreamID: 7},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := handler.freshNewAPIKeyByID(req, store.Site{ID: siteID, SiteType: tc.siteType}, siteID, tc.upstreamID)
			if got.ID != 0 || got.Name != "" || len(got.Raw) != 0 {
				t.Fatalf("freshNewAPIKeyByID = %#v, want empty key", got)
			}
		})
	}
}

func TestSiteAPIKeyPayloadPrefersFreshStatusAndLegacyDefaultName(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	status := "disabled"
	payload := (Handler{newAPI: newapi.NewService()}).siteAPIKeyPayload(
		adminTestRequest(http.MethodGet, "/api/v1/sites/site-id/api-keys", ""),
		store.Site{ID: uuid.New(), SiteType: "newapi"},
		sitepkg.APIKeyCredential{
			Credential:   store.SiteCredential{ID: credentialID},
			Name:         "默认 Key",
			UpstreamID:   3,
			MaskedSecret: "sk-***",
			Secret:       "sk-copy",
			Enabled:      true,
			Meta:         map[string]any{"status": "active"},
		},
		newapi.UserAPIKey{Status: status},
	)

	if payload["id"] != credentialID.String() || payload["upstream_id"] != 3 {
		t.Fatalf("identity fields = %#v", payload)
	}
	if payload["name"] != "Apikey 1" {
		t.Fatalf("legacy default name = %#v, want Apikey 1", payload["name"])
	}
	if payload["status"] != status {
		t.Fatalf("fresh status = %#v, want %q", payload["status"], status)
	}
	if _, hasCopyKey := payload["copy_key"]; hasCopyKey || payload["key"] != "sk-***" || payload["enabled"] != true {
		t.Fatalf("secret/enabled fields = %#v", payload)
	}
	if models := payload["models"].([]string); len(models) != 0 {
		t.Fatalf("models = %#v, want empty without newapi client", models)
	}
	if modelItems := payload["model_items"].([]map[string]any); len(modelItems) != 0 {
		t.Fatalf("model_items = %#v, want empty without newapi client", modelItems)
	}
}

func TestSiteAPIKeyHandlersRejectMalformedRouteIDs(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.New().String()
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "list api keys invalid site id",
			method: http.MethodGet,
			target: "/api/v1/sites/bad-id/api-keys",
			call:   handler.ListSiteAPIKeys,
		},
		{
			name:   "delete api key invalid api key id",
			method: http.MethodDelete,
			target: "/api/v1/sites/" + siteID + "/api-keys/bad-id",
			call:   handler.DeleteSiteAPIKey,
		},
		{
			name:   "update api key invalid api key id",
			method: http.MethodPatch,
			target: "/api/v1/sites/" + siteID + "/api-keys/bad-id",
			body:   `{"enabled":true}`,
			call:   handler.UpdateSiteAPIKey,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wantCode := "invalid_api_key_id"
			routeSiteID := siteID
			routeAPIKeyID := "bad-id"
			if strings.Contains(tc.target, "bad-id/api-keys") {
				wantCode = "invalid_site_id"
				routeSiteID = "bad-id"
				routeAPIKeyID = ""
			}
			assertSiteAPIKeyAdminError(t, tc.call, tc.method, tc.target, tc.body, routeSiteID, routeAPIKeyID, wantCode)
		})
	}
}

func TestCreateSiteAPIKeyRejectsInvalidPayloadBeforeServiceWork(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.New().String()
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "invalid json", body: `{`, code: "invalid_json"},
		{name: "missing secret", body: `{"api_key":"  ","secret":"\t"}`, code: "invalid_api_key"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertSiteAPIKeyAdminError(t, handler.CreateSiteAPIKey, http.MethodPost, "/api/v1/sites/"+siteID+"/api-keys", tc.body, siteID, "", tc.code)
		})
	}
}

func TestSiteHealthEndpointsRejectInvalidWindowQueries(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.New().String()
	for _, tc := range []struct {
		name   string
		target string
		call   func(http.ResponseWriter, *http.Request)
		code   string
	}{
		{
			name:   "history invalid limit",
			target: "/api/v1/sites/" + siteID + "/health/history?limit=0",
			call:   handler.GetSiteHealthHistory,
			code:   "invalid_limit",
		},
		{
			name:   "hourly invalid hours",
			target: "/api/v1/sites/" + siteID + "/health/hourly?hours=bad",
			call:   handler.GetSiteHealthHourly,
			code:   "invalid_hours",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertSiteAPIKeyAdminError(t, tc.call, http.MethodGet, tc.target, "", siteID, "", tc.code)
		})
	}
}

func assertSiteAPIKeyAdminError(
	t *testing.T,
	call func(http.ResponseWriter, *http.Request),
	method string,
	target string,
	body string,
	siteID string,
	apiKeyID string,
	code string,
) {
	t.Helper()

	params := map[string]string{"siteID": siteID}
	if apiKeyID != "" {
		params["apiKeyID"] = apiKeyID
	}
	req := adminRequestWithRouteParams(method, target, body, params)
	rec := adminPerform(call, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, code)
}
