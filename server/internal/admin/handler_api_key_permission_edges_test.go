package admin

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func TestListAPIKeysOfflineDefaultAndSyncViews(t *testing.T) {
	t.Parallel()

	apiKey := offlineAPIKeyPermissionFixture()
	handler := offlineAPIKeyPermissionHandler(t, apiKey)
	for _, tc := range []struct {
		name     string
		target   string
		wantView any
	}{
		{name: "default", target: "/api/v1/api-keys"},
		{name: "sync", target: "/api/v1/api-keys?view=sync", wantView: "sync"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListAPIKeys, adminTestRequest(http.MethodGet, tc.target, ""))

			var body struct {
				Items []map[string]any `json:"items"`
				Meta  map[string]any   `json:"meta"`
			}
			adminAssertStatus(t, rec, http.StatusOK)
			body = adminDecodeJSON[struct {
				Items []map[string]any `json:"items"`
				Meta  map[string]any   `json:"meta"`
			}](t, rec)
			if len(body.Items) != 1 || body.Items[0]["id"] != apiKey.ID.String() {
				t.Fatalf("items = %#v, want fixture api key", body.Items)
			}
			if body.Meta["count"] != float64(1) || body.Meta["view"] != tc.wantView {
				t.Fatalf("meta = %#v, want count=1 view=%#v", body.Meta, tc.wantView)
			}
		})
	}
}

func TestAPIKeyPermissionHandlersReturnOfflineLists(t *testing.T) {
	t.Parallel()

	apiKey := offlineAPIKeyPermissionFixture()
	handler := offlineAPIKeyPermissionHandler(t, apiKey)
	for _, tc := range []struct {
		name   string
		method string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "get", method: http.MethodGet, target: "/api/v1/api-keys/" + apiKey.ID.String(), call: handler.GetAPIKey},
		{name: "site models", method: http.MethodGet, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/site-models", call: handler.ListAPIKeySiteModels},
		{name: "sites", method: http.MethodGet, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/sites", call: handler.ListAPIKeySites},
		{name: "site groups", method: http.MethodGet, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/site-groups", call: handler.ListAPIKeySiteGroups},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(tc.method, tc.target, "", "apiKeyID", apiKey.ID.String())
			rec := adminPerform(tc.call, req)

			adminAssertStatus(t, rec, http.StatusOK)
			if tc.name == "get" {
				body := adminDecodeJSON[struct {
					APIKey map[string]any `json:"api_key"`
				}](t, rec)
				if body.APIKey["id"] != apiKey.ID.String() {
					t.Fatalf("api_key = %#v, want fixture id", body.APIKey)
				}
				return
			}
			body := adminDecodeJSON[struct {
				Items []map[string]any `json:"items"`
				Meta  map[string]any   `json:"meta"`
			}](t, rec)
			if len(body.Items) != 0 || body.Meta["count"] != float64(0) {
				t.Fatalf("permission response = %#v meta=%#v, want empty list", body.Items, body.Meta)
			}
		})
	}
}

func TestAPIKeyMutationHandlersValidatePayloadsAfterLookup(t *testing.T) {
	t.Parallel()

	apiKey := offlineAPIKeyPermissionFixture()
	handler := offlineAPIKeyPermissionHandler(t, apiKey)
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		code   string
	}{
		{name: "update invalid json", method: http.MethodPatch, target: "/api/v1/api-keys/" + apiKey.ID.String(), body: `{"name":`, call: handler.UpdateAPIKey, code: "invalid_json"},
		{name: "site model id", method: http.MethodPut, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/site-models", body: `{"site_model_ids":["not-a-uuid"]}`, call: handler.UpdateAPIKeySiteModels, code: "invalid_site_model_id"},
		{name: "group id", method: http.MethodPut, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/site-groups", body: `{"group_ids":["not-a-uuid"]}`, call: handler.UpdateAPIKeySiteGroups, code: "invalid_group_id"},
		{name: "site id", method: http.MethodPut, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/sites", body: `{"site_ids":["not-a-uuid"]}`, call: handler.UpdateAPIKeySites, code: "invalid_site_id"},
		{name: "check model invalid json", method: http.MethodPost, target: "/api/v1/api-keys/" + apiKey.ID.String() + "/check-model", body: `{"model_key":`, call: handler.CheckAPIKeyModel, code: "invalid_json"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(tc.method, tc.target, tc.body, "apiKeyID", apiKey.ID.String())
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestDeleteAPIKeyReturnsOfflineServiceError(t *testing.T) {
	t.Parallel()

	apiKey := offlineAPIKeyPermissionFixture()
	handler := offlineAPIKeyPermissionHandler(t, apiKey)
	req := adminRequestWithRouteParam(http.MethodDelete, "/api/v1/api-keys/"+apiKey.ID.String(), "", "apiKeyID", apiKey.ID.String())
	rec := adminPerform(handler.DeleteAPIKey, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "api_key_delete_failed")
}

func offlineAPIKeyPermissionFixture() store.APIKey {
	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	return store.APIKey{
		ID:             uuid.New(),
		Name:           "offline key",
		KeyPrefix:      "xly",
		MaskedKey:      "xly_****",
		KeyKind:        "generated",
		Scope:          "gateway",
		Status:         "active",
		ModelPolicy:    "allow_all",
		SitePolicy:     "allow_all",
		QuotaUnlimited: true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func offlineAPIKeyPermissionHandler(t *testing.T, apiKey store.APIKey) Handler {
	t.Helper()

	db := adminGormWithCallbacks(t, adminGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.APIKey:
				*dest = apiKey
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *[]store.APIKey:
				*dest = []store.APIKey{apiKey}
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *[]store.APIKeySiteModelPermission:
				*dest = nil
			case *[]store.APIKeySitePermission:
				*dest = nil
			case *[]store.APIKeySiteGroupPermission:
				*dest = nil
			case *[]store.SiteGroup:
				*dest = nil
			case *[]store.SiteGroupSite:
				*dest = nil
			case *[]store.GatewayRateLimit:
				*dest = nil
			case *store.GatewayRateLimit:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(fmt.Errorf("unexpected api key permission query destination %T", tx.Statement.Dest))
			}
		},
		deleteCallback: func(tx *gorm.DB) {
			tx.RowsAffected = 0
			tx.Statement.RowsAffected = 0
		},
	})

	return Handler{auth: auth.NewService(db, adminTestMasterKey)}
}
