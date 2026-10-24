package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	oauthsvc "xlyra/server/internal/oauth"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

type accountCall struct {
	name       string
	method     string
	path       string
	body       string
	credential bool
	fn         func(http.ResponseWriter, *http.Request)
}

func accountCalls(h Handler) []accountCall {
	return []accountCall{
		{name: "list", method: http.MethodGet, path: "/api/v1/sites/x/accounts", fn: h.ListAccounts},
		{name: "regions", method: http.MethodGet, path: "/api/v1/sites/x/accounts/regions", fn: h.ListAccountRegions},
		{name: "import", method: http.MethodPost, path: "/api/v1/sites/x/accounts/import", body: `{"files":[{"name":"a.json","content":"{}"}]}`, fn: h.ImportAccounts},
		{name: "login start", method: http.MethodPost, path: "/api/v1/sites/x/accounts/login/start", body: `{}`, fn: h.StartAccountLogin},
		{name: "login poll", method: http.MethodPost, path: "/api/v1/sites/x/accounts/login/poll", body: `{"session":"session-1"}`, fn: h.PollAccountLogin},
		{name: "refresh all", method: http.MethodPost, path: "/api/v1/sites/x/accounts/refresh", fn: h.RefreshAccounts},
		{name: "update", method: http.MethodPut, path: "/api/v1/sites/x/accounts/x", body: `{"enabled":false}`, credential: true, fn: h.UpdateAccount},
		{name: "model", method: http.MethodPut, path: "/api/v1/sites/x/accounts/x/models", body: `{"model":"m","enabled":true}`, credential: true, fn: h.UpdateAccountModel},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/sites/x/accounts/x", credential: true, fn: h.DeleteAccount},
		{name: "refresh", method: http.MethodPost, path: "/api/v1/sites/x/accounts/x/refresh", credential: true, fn: h.RefreshAccount},
	}
}

func TestAccountRoutesRejectMissingService(t *testing.T) {
	t.Parallel()

	for _, call := range accountCalls(Handler{}) {
		t.Run(call.name, func(t *testing.T) {
			t.Parallel()
			rec := adminPerform(call.fn, adminTestRequest(call.method, call.path, call.body))
			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
		})
	}
}

func TestAccountRoutesRejectInvalidSiteID(t *testing.T) {
	t.Parallel()

	handler := Handler{sites: sitepkg.NewService(nil, adminTestMasterKey)}
	for _, call := range accountCalls(handler) {
		t.Run(call.name, func(t *testing.T) {
			t.Parallel()
			vars := map[string]string{"siteID": "not-a-uuid"}
			if call.credential {
				vars["credentialID"] = uuid.NewString()
			}
			rec := adminPerform(call.fn, adminRequestWithRouteParams(call.method, call.path, call.body, vars))
			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_id")
		})
	}
}

func TestAccountRoutesRejectUnsupportedSiteType(t *testing.T) {
	t.Parallel()

	fixture := newAccountRouteFixture(t, "openai")
	for _, call := range accountCalls(fixture.handler) {
		t.Run(call.name, func(t *testing.T) {
			rec := fixture.perform(call.fn, call.method, call.path, call.body, call.credential)
			assertAdminErrorCode(t, rec, http.StatusNotFound, "accounts_not_supported")
		})
	}
}

func TestAccountRoutesRejectUnavailablePluginProvider(t *testing.T) {
	t.Parallel()

	fixture := newAccountRouteFixture(t, "acme.example/missing")
	for _, call := range accountCalls(fixture.handler) {
		t.Run(call.name, func(t *testing.T) {
			t.Parallel()
			rec := fixture.perform(call.fn, call.method, call.path, call.body, call.credential)
			adminAssertStatus(t, rec, http.StatusConflict)
			body := adminDecodeJSON[struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}](t, rec)
			if body.Error.Code != "provider_unavailable" || body.Error.Message != "provider plugin is not enabled" {
				t.Fatalf("%s error = %#v", call.name, body.Error)
			}
		})
	}
}

func TestAccountRoutesRejectMissingSite(t *testing.T) {
	t.Parallel()

	fixture := newAccountRouteFixture(t, "grok")
	for _, call := range accountCalls(fixture.handler) {
		t.Run(call.name, func(t *testing.T) {
			vars := map[string]string{"siteID": uuid.NewString()}
			if call.credential {
				vars["credentialID"] = uuid.NewString()
			}
			rec := adminPerform(call.fn, adminRequestWithRouteParams(call.method, call.path, call.body, vars))
			assertAdminErrorCode(t, rec, http.StatusNotFound, "site_not_found")
		})
	}
}

func TestAccountRoutesRejectInvalidCredentialID(t *testing.T) {
	t.Parallel()

	for _, siteType := range []string{"grok"} {
		t.Run(siteType, func(t *testing.T) {
			fixture := newAccountRouteFixture(t, siteType)
			for _, call := range accountCalls(fixture.handler) {
				if !call.credential {
					continue
				}
				vars := map[string]string{"siteID": fixture.site.ID.String(), "credentialID": "not-a-uuid"}
				rec := adminPerform(call.fn, adminRequestWithRouteParams(call.method, call.path, call.body, vars))
				adminAssertStatus(t, rec, http.StatusBadRequest)
				if !strings.Contains(rec.Body.String(), "invalid credential id") {
					t.Fatalf("%s body = %s", call.name, rec.Body.String())
				}
			}
		})
	}
}

func TestAccountRouteShapes(t *testing.T) {
	t.Parallel()

	for _, siteType := range []string{"grok"} {
		t.Run(siteType, func(t *testing.T) {
			prefix := store.CredentialPrefixGrok
			fixture := newAccountRouteFixture(t, siteType)
			credentialID := fixture.addAccount(uuid.Nil, prefix+uuid.NewString(), "user@example.com", nil)

			listed := fixture.perform(fixture.handler.ListAccounts, http.MethodGet, "/api/v1/sites/x/accounts", "", false)
			adminAssertStatus(t, listed, http.StatusOK)
			listBody := decodeObject(t, listed.Body.Bytes())
			assertExactJSONKeys(t, listBody, "list", "items", "meta")
			assertExactJSONKeys(t, mapFromRawObject(t, listBody["meta"]), "list meta", "count")
			if string(listBody["meta"]) != `{"count":1}` && string(listBody["meta"]) != `{"count": 1}` {
				compact := compactJSON(t, listBody["meta"])
				if compact != `{"count":1}` {
					t.Fatalf("list meta = %s", listBody["meta"])
				}
			}
			items := decodeArray(t, listBody["items"])
			if len(items) != 1 {
				t.Fatalf("list items = %d", len(items))
			}
			assertAccountShape(t, siteType, items[0], credentialID, true)

			updated := fixture.perform(fixture.handler.UpdateAccount, http.MethodPut, "/api/v1/sites/x/accounts/x", `{"enabled":false}`, true)
			adminAssertStatus(t, updated, http.StatusOK)
			updateBody := decodeObject(t, updated.Body.Bytes())
			assertExactJSONKeys(t, updateBody, "update", "account")
			assertAccountShape(t, siteType, updateBody["account"], credentialID, false)

			modelBody := `{"model":"demo-model","enabled":false}`
			modeled := fixture.perform(fixture.handler.UpdateAccountModel, http.MethodPut, "/api/v1/sites/x/accounts/x/models", modelBody, true)
			adminAssertStatus(t, modeled, http.StatusOK)
			assertExactJSONKeys(t, decodeObject(t, modeled.Body.Bytes()), "model", "account")

			refreshed := fixture.performTimeout(fixture.handler.RefreshAccount, http.MethodPost, "/api/v1/sites/x/accounts/"+uuid.NewString()+"/refresh", "", uuid.New())
			assertAdminErrorCode(t, refreshed, http.StatusBadRequest, "account_refresh_failed")

			empty := newAccountRouteFixture(t, siteType)
			refreshAll := empty.performTimeout(empty.handler.RefreshAccounts, http.MethodPost, "/api/v1/sites/x/accounts/refresh", "", uuid.Nil)
			adminAssertStatus(t, refreshAll, http.StatusOK)
			refreshBody := decodeObject(t, refreshAll.Body.Bytes())
			assertExactJSONKeys(t, refreshBody, "refresh all", "items", "meta")
			if compactJSON(t, refreshBody["items"]) != "[]" {
				t.Fatalf("empty refresh items = %s", refreshBody["items"])
			}
			if compactJSON(t, refreshBody["meta"]) != `{"count":0,"failed":0}` {
				t.Fatalf("empty refresh meta = %s", refreshBody["meta"])
			}

			failedFixture := newAccountRouteFixture(t, siteType)
			failedID := failedFixture.addAccount(uuid.Nil, prefix+uuid.NewString(), "user@example.com", nil)
			failed := failedFixture.performTimeout(failedFixture.handler.RefreshAccounts, http.MethodPost, "/api/v1/sites/x/accounts/refresh", "", uuid.Nil)
			adminAssertStatus(t, failed, http.StatusOK)
			failedBody := decodeObject(t, failed.Body.Bytes())
			if compactJSON(t, failedBody["meta"]) != `{"count":1,"failed":1}` {
				t.Fatalf("failed refresh meta = %s body=%s", failedBody["meta"], failed.Body.String())
			}
			failedItems := decodeArray(t, failedBody["items"])
			if len(failedItems) != 1 {
				t.Fatalf("failed refresh items = %s", failedBody["items"])
			}
			failedItem := mapFromRawObject(t, failedItems[0])
			assertExactJSONKeys(t, failedItem, "refresh item", "credential_id", "error")
			if string(failedItem["credential_id"]) != `"`+failedID.String()+`"` || string(failedItem["error"]) == `""` {
				t.Fatalf("refresh item = %s", failedItems[0])
			}

			deleted := fixture.perform(fixture.handler.DeleteAccount, http.MethodDelete, "/api/v1/sites/x/accounts/x", "", true)
			adminAssertStatus(t, deleted, http.StatusNoContent)
			if deleted.Body.Len() != 0 {
				t.Fatalf("delete body = %s", deleted.Body.String())
			}
		})
	}
}

func TestAccountListAndUpdateJSON(t *testing.T) {
	t.Parallel()

	const credentialID = "22222222-2222-2222-2222-222222222222"
	id := uuid.MustParse(credentialID)
	cases := []struct {
		siteType string
		prefix   string
		fields   map[string]string
	}{
		{
			siteType: "grok",
			prefix:   store.CredentialPrefixGrok,
			fields: map[string]string{
				"credential_id":   `"` + credentialID + `"`,
				"masked_token":    `"same@example.com"`,
				"enabled":         "true",
				"remaining_quota": "0",
				"used_quota":      "0",
				"quota_known":     "false",
				"model_count":     "0",
				"models":          "[]",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.siteType+" list", func(t *testing.T) {
			t.Parallel()
			fixture := newAccountRouteFixture(t, tc.siteType)
			fixture.addAccount(id, tc.prefix+uuid.NewString(), "same@example.com", nil)
			got := fixture.perform(fixture.handler.ListAccounts, http.MethodGet, "/api/v1/sites/x/accounts", "", false)
			adminAssertStatus(t, got, http.StatusOK)
			items := decodeArray(t, decodeObject(t, got.Body.Bytes())["items"])
			if len(items) != 1 {
				t.Fatalf("list items = %s", got.Body.String())
			}
			assertJSONFields(t, items[0], tc.siteType+" list", tc.fields)
		})
		t.Run(tc.siteType+" update", func(t *testing.T) {
			t.Parallel()
			fixture := newAccountRouteFixture(t, tc.siteType)
			fixture.addAccount(id, tc.prefix+uuid.NewString(), "same@example.com", nil)
			got := fixture.perform(fixture.handler.UpdateAccount, http.MethodPut, "/api/v1/sites/x/accounts/x", `{"enabled":false}`, true)
			adminAssertStatus(t, got, http.StatusOK)
			body := decodeObject(t, got.Body.Bytes())
			assertExactJSONKeys(t, body, tc.siteType+" update", "account")
			fields := map[string]string{}
			for key, value := range tc.fields {
				fields[key] = value
			}
			fields["enabled"] = "false"
			assertJSONFields(t, body["account"], tc.siteType+" update", fields)
		})
	}
}

func TestAccountLoginStartShape(t *testing.T) {
	t.Parallel()

	grok := newAccountRouteFixture(t, "grok")
	grok.setTransport(scriptTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/oauth2/device/code") {
			return jsonHTTP(http.StatusNotFound, `{}`), nil
		}
		return jsonHTTP(http.StatusOK, `{"device_code":"dev-code","user_code":"USER-CODE","verification_uri":"https://auth.example/device","verification_uri_complete":"https://auth.example/device?user_code=USER-CODE","expires_in":600,"interval":5}`), nil
	}))
	started := grok.perform(grok.handler.StartAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/start", "", false)
	adminAssertStatus(t, started, http.StatusOK)
	login := mapFromRawObject(t, decodeObject(t, started.Body.Bytes())["login"])
	assertExactJSONKeys(t, login, "grok login", "kind", "session", "user_code", "verification_uri", "verification_uri_complete", "interval", "expires_at")
	if string(login["kind"]) != `"oauth2_device_code"` || string(login["session"]) != `"dev-code"` || string(login["user_code"]) != `"USER-CODE"` || string(login["interval"]) != "5" {
		t.Fatalf("grok login = %s", started.Body.String())
	}
	assertExpiresIn(t, login["expires_at"], 600)
}

func TestAccountLoginPollShape(t *testing.T) {
	t.Parallel()

	pending := newAccountRouteFixture(t, "grok")
	pending.setTransport(grokTokenTransport(`{"error":"authorization_pending"}`, http.StatusBadRequest))
	rec := pending.perform(pending.handler.PollAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/poll", `{"session":"dev-code"}`, false)
	adminAssertStatus(t, rec, http.StatusOK)
	login := mapFromRawObject(t, decodeObject(t, rec.Body.Bytes())["login"])
	assertExactJSONKeys(t, login, "pending", "status")
	if string(login["status"]) != `"pending"` {
		t.Fatalf("pending = %s", rec.Body.String())
	}

	slow := newAccountRouteFixture(t, "grok")
	slow.setTransport(grokTokenTransport(`{"error":"slow_down"}`, http.StatusBadRequest))
	rec = slow.perform(slow.handler.PollAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/poll", `{"session":"dev-code"}`, false)
	adminAssertStatus(t, rec, http.StatusOK)
	login = mapFromRawObject(t, decodeObject(t, rec.Body.Bytes())["login"])
	assertExactJSONKeys(t, login, "slow_down", "status")
	if string(login["status"]) != `"slow_down"` {
		t.Fatalf("slow_down = %s", rec.Body.String())
	}

	complete := newAccountRouteFixture(t, "grok")
	complete.setTransport(grokTokenTransport(`{"access_token":"access-token-value","token_type":"Bearer","expires_in":0}`, http.StatusOK))
	rec = complete.performTimeout(complete.handler.PollAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/poll", `{"session":"dev-code"}`, uuid.Nil)
	adminAssertStatus(t, rec, http.StatusOK)
	login = mapFromRawObject(t, decodeObject(t, rec.Body.Bytes())["login"])
	assertExactJSONKeys(t, login, "complete", "status", "account")
	if string(login["status"]) != `"complete"` {
		t.Fatalf("complete = %s", rec.Body.String())
	}
	account := mapFromRawObject(t, login["account"])
	if account["credential_id"] == nil || account["region"] != nil {
		t.Fatalf("complete account = %s", login["account"])
	}
}

func TestAccountImportAndRegions(t *testing.T) {
	t.Parallel()

	grok := newAccountRouteFixture(t, "grok")
	rec := grok.perform(grok.handler.ImportAccounts, http.MethodPost, "/api/v1/sites/x/accounts/import", `{"files":[{"name":"a.json","content":"{}"}]}`, false)
	assertAdminErrorCode(t, rec, http.StatusNotFound, "account_import_unsupported")
	rec = grok.perform(grok.handler.ListAccountRegions, http.MethodGet, "/api/v1/sites/x/accounts/regions", "", false)
	adminAssertStatus(t, rec, http.StatusOK)
	body := decodeObject(t, rec.Body.Bytes())
	if compactJSON(t, body["items"]) != "[]" || compactJSON(t, body["meta"]) != `{"count":0,"default":""}` {
		t.Fatalf("grok regions = %s", rec.Body.String())
	}
}

func TestAccountRouteValidation(t *testing.T) {
	t.Parallel()

	fixture := newAccountRouteFixture(t, "grok")
	fixture.addAccount(uuid.Nil, store.CredentialPrefixGrok+uuid.NewString(), "user@example.com", map[string]any{"enabled": false, "reauth_required": true})
	rec := fixture.perform(fixture.handler.UpdateAccount, http.MethodPut, "/api/v1/sites/x/accounts/x", `{"enabled":true}`, true)
	reauthBody := rec.Body.String()
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "account_update_failed")
	if !strings.Contains(reauthBody, "requires reauthorization") {
		t.Fatalf("reauth body = %s", reauthBody)
	}

	vars := map[string]string{"siteID": fixture.site.ID.String(), "credentialID": uuid.NewString()}
	rec = adminPerform(fixture.handler.UpdateAccount, adminRequestWithRouteParams(http.MethodPut, "/api/v1/sites/x/accounts/x", `{"enabled":false}`, vars))
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "account_update_failed")

	rec = fixture.perform(fixture.handler.UpdateAccount, http.MethodPut, "/api/v1/sites/x/accounts/x", `{`, true)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
	rec = fixture.perform(fixture.handler.UpdateAccountModel, http.MethodPut, "/api/v1/sites/x/accounts/x/models", `{}`, true)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_model_update")
	rec = fixture.perform(fixture.handler.UpdateAccountModel, http.MethodPut, "/api/v1/sites/x/accounts/x/models", `{"site_model_id":"nope","endpoint_mode":"inherit"}`, true)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_model_id")
	rec = fixture.perform(fixture.handler.UpdateAccountModel, http.MethodPut, "/api/v1/sites/x/accounts/x/models", `{"site_model_id":"`+uuid.NewString()+`","endpoint_mode":"nope"}`, true)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "account_model_endpoint_update_failed")

	rec = fixture.perform(fixture.handler.StartAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/start", `{`, false)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
	rec = fixture.perform(fixture.handler.PollAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/poll", `{"session":" "}`, false)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_login_session")
	rec = fixture.perform(fixture.handler.ImportAccounts, http.MethodPost, "/api/v1/sites/x/accounts/import", `{`, false)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")

	fixture.setOAuth(nil)
	rec = fixture.perform(fixture.handler.StartAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/start", `{}`, false)
	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "oauth_service_unavailable")

	upstream := newAccountRouteFixture(t, "grok")
	upstream.setTransport(scriptTransport(func(*http.Request) (*http.Response, error) {
		return jsonHTTP(http.StatusBadGateway, `{"error":"down"}`), nil
	}))
	rec = upstream.perform(upstream.handler.StartAccountLogin, http.MethodPost, "/api/v1/sites/x/accounts/login/start", "", false)
	assertAdminErrorCode(t, rec, http.StatusBadGateway, "account_login_start_failed")
}

func assertJSONFields(t *testing.T, raw json.RawMessage, label string, fields map[string]string) {
	t.Helper()

	item := mapFromRawObject(t, raw)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	assertExactJSONKeys(t, item, label, keys...)
	for key, want := range fields {
		if compactJSON(t, item[key]) != want {
			t.Fatalf("%s %s = %s, want %s", label, key, item[key], want)
		}
	}
	if strings.Contains(string(raw), `"token"`) {
		t.Fatalf("%s contains a token field: %s", label, raw)
	}
	if _, ok := item["region"]; ok && fields["region"] == "" {
		t.Fatalf("%s includes region: %s", label, raw)
	}
	if _, wantRegion := fields["region"]; wantRegion && (item["region"] == nil || string(item["region"]) == `""`) {
		t.Fatalf("%s region = %s", label, item["region"])
	}
	for _, key := range []string{"remaining_quota", "used_quota", "quota_known"} {
		if _, want := fields[key]; want && item[key] == nil {
			t.Fatalf("%s omits %s: %s", label, key, raw)
		}
	}
}

func assertAccountShape(t *testing.T, siteType string, raw json.RawMessage, credentialID uuid.UUID, enabled bool) {
	t.Helper()

	item := mapFromRawObject(t, raw)
	keys := []string{"credential_id", "masked_token", "enabled", "remaining_quota", "used_quota", "quota_known", "model_count", "models"}
	assertExactJSONKeys(t, item, siteType+" account", keys...)
	enabledText := "true"
	if !enabled {
		enabledText = "false"
	}
	if string(item["credential_id"]) != `"`+credentialID.String()+`"` || string(item["masked_token"]) != `"user@example.com"` || string(item["enabled"]) != enabledText || compactJSON(t, item["models"]) != "[]" {
		t.Fatalf("%s account = %s", siteType, raw)
	}
	if item["region"] != nil {
		t.Fatalf("%s account includes region: %s", siteType, raw)
	}
}

func assertExpiresIn(t *testing.T, raw json.RawMessage, seconds int) {
	t.Helper()

	var expires time.Time
	if err := json.Unmarshal(raw, &expires); err != nil {
		t.Fatalf("expires_at: %v", err)
	}
	delta := time.Until(expires) - time.Duration(seconds)*time.Second
	if delta < -15*time.Second || delta > 15*time.Second {
		t.Fatalf("expires_at = %s, want about %ds ahead", expires, seconds)
	}
}

func decodeObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	return mapFromRawObject(t, raw)
}

func decodeArray(t *testing.T, raw json.RawMessage) []json.RawMessage {
	t.Helper()

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decode array: %v body=%s", err, raw)
	}
	return items
}

func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()

	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact json: %v body=%s", err, raw)
	}
	return buf.String()
}

type accountRouteFixture struct {
	t           *testing.T
	site        store.Site
	credentials []store.SiteCredential
	states      []store.SiteAPIKeyState
	service     *sitepkg.Service
	handler     Handler
}

func newAccountRouteFixture(t *testing.T, siteType string) *accountRouteFixture {
	t.Helper()

	fixture := &accountRouteFixture{t: t}
	fixture.site = store.Site{
		ID:       uuid.New(),
		Name:     siteType + " accounts",
		SiteType: siteType,
		BaseURL:  "https://example.test",
		Enabled:  true,
		Status:   "active",
		Meta:     store.JSON(`{}`),
	}
	db := adminTransactionGormWithCallbacks(t, adminGormCallbacks{
		query:          fixture.query,
		create:         fixture.create,
		update:         fixture.update,
		deleteCallback: fixture.delete,
	})
	fixture.service = sitepkg.NewService(adminStoreWithGorm(db), adminTestMasterKey)
	fixture.handler = Handler{sites: fixture.service}
	return fixture
}

func (f *accountRouteFixture) addAccount(id uuid.UUID, credentialType string, email string, extra map[string]any) uuid.UUID {
	f.t.Helper()

	if id == uuid.Nil {
		id = uuid.New()
	}
	meta := map[string]any{"email": email, "enabled": true}
	for key, value := range extra {
		meta[key] = value
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		f.t.Fatalf("marshal account meta: %v", err)
	}
	f.credentials = append(f.credentials, store.SiteCredential{
		ID:              id,
		SiteID:          f.site.ID,
		CredentialType:  credentialType,
		EncryptedSecret: "not-a-secret",
		MaskedSecret:    "****",
		Meta:            store.JSON(raw),
	})
	return id
}

func (f *accountRouteFixture) perform(handler func(http.ResponseWriter, *http.Request), method string, target string, body string, credential bool) *httptest.ResponseRecorder {
	f.t.Helper()
	return adminPerform(handler, f.request(method, target, body, credential, uuid.Nil))
}

func (f *accountRouteFixture) performTimeout(handler func(http.ResponseWriter, *http.Request), method string, target string, body string, credentialID uuid.UUID) *httptest.ResponseRecorder {
	f.t.Helper()

	req := f.request(method, target, body, credentialID != uuid.Nil, credentialID)
	ctx, cancel := context.WithTimeout(req.Context(), 3*time.Second)
	defer cancel()
	return adminPerform(handler, req.WithContext(ctx))
}

func (f *accountRouteFixture) request(method string, target string, body string, credential bool, credentialID uuid.UUID) *http.Request {
	f.t.Helper()

	vars := map[string]string{"siteID": f.site.ID.String()}
	if credential {
		if credentialID == uuid.Nil {
			if len(f.credentials) == 0 {
				credentialID = uuid.New()
			} else {
				credentialID = f.credentials[0].ID
			}
		}
		vars["credentialID"] = credentialID.String()
	}
	return adminRequestWithRouteParams(method, target, body, vars)
}

func (f *accountRouteFixture) setTransport(rt http.RoundTripper) {
	f.t.Helper()
	setUnexported(f.oauthService(), "httpClient", &http.Client{Transport: rt})
}

func (f *accountRouteFixture) setOAuth(service *oauthsvc.Service) {
	f.t.Helper()
	setUnexported(f.service, "oauth", service)
}

func (f *accountRouteFixture) oauthService() *oauthsvc.Service {
	f.t.Helper()
	return unexported(f.service, "oauth").(*oauthsvc.Service)
}

func (f *accountRouteFixture) query(tx *gorm.DB) {
	ids, _ := accountClauseValues(tx)
	switch dest := tx.Statement.Dest.(type) {
	case *store.Site:
		if len(ids) > 0 && !containsUUID(ids, f.site.ID) {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = f.site
		accountRows(tx, 1)
	case *store.SiteCredential:
		cred, ok := f.credentialByIDs(ids)
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = cred
		accountRows(tx, 1)
	case *[]store.SiteCredential:
		*dest = append([]store.SiteCredential(nil), f.credentials...)
		accountRows(tx, len(f.credentials))
	case *store.SiteAPIKeyState:
		for _, state := range f.states {
			if containsUUID(ids, state.SiteCredentialID) {
				*dest = state
				accountRows(tx, 1)
				return
			}
		}
		tx.AddError(gorm.ErrRecordNotFound)
	case *[]store.SiteAPIKeyState:
		*dest = append([]store.SiteAPIKeyState(nil), f.states...)
		accountRows(tx, len(f.states))
	case *store.SiteAPIKeyModel:
		tx.AddError(gorm.ErrRecordNotFound)
	case *[]store.SiteAPIKeyModel:
		*dest = []store.SiteAPIKeyModel{}
		accountRows(tx, 0)
	case *[]store.SiteModel:
		*dest = []store.SiteModel{}
		accountRows(tx, 0)
	case *store.SiteModel:
		tx.AddError(gorm.ErrRecordNotFound)
	default:
		tx.AddError(fmt.Errorf("unhandled query %T", tx.Statement.Dest))
	}
}

func (f *accountRouteFixture) create(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.SiteCredential:
		if dest.ID == uuid.Nil {
			dest.ID = uuid.New()
		}
		f.credentials = append(f.credentials, *dest)
	case *store.SiteAPIKeyState:
		f.states = append(f.states, *dest)
	default:
		tx.AddError(fmt.Errorf("unhandled create %T", tx.Statement.Dest))
		return
	}
	accountRows(tx, 1)
}

func (f *accountRouteFixture) update(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case map[string]any:
		meta, ok := accountMetaJSON(dest["meta"])
		if !ok {
			accountRows(tx, 1)
			return
		}
		cred, ok := f.credentialByIDs(accountClauseIDs(tx))
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		cred.Meta = meta
		f.replaceCredential(cred)
	case *store.SiteCredential:
		if dest.ID != uuid.Nil {
			f.replaceCredential(*dest)
		}
	case *store.SiteAPIKeyState:
		f.replaceState(*dest)
	case *store.Site:
		f.site = *dest
	}
	accountRows(tx, 1)
}

func (f *accountRouteFixture) delete(tx *gorm.DB) {
	id := uuid.Nil
	ids := accountClauseIDs(tx)
	if len(ids) > 0 {
		id = ids[0]
	}
	switch tx.Statement.Model.(type) {
	case *store.SiteCredential, store.SiteCredential:
		f.removeCredential(id)
	case *store.SiteAPIKeyState, store.SiteAPIKeyState:
		f.removeState(id)
	}
	accountRows(tx, 1)
}

func (f *accountRouteFixture) credentialByIDs(ids []uuid.UUID) (store.SiteCredential, bool) {
	for _, id := range ids {
		for _, cred := range f.credentials {
			if cred.ID == id {
				return cred, true
			}
		}
	}
	return store.SiteCredential{}, false
}

func (f *accountRouteFixture) replaceCredential(cred store.SiteCredential) {
	for index := range f.credentials {
		if f.credentials[index].ID == cred.ID {
			f.credentials[index] = cred
			return
		}
	}
	f.credentials = append(f.credentials, cred)
}

func (f *accountRouteFixture) removeCredential(id uuid.UUID) {
	kept := f.credentials[:0]
	for _, cred := range f.credentials {
		if cred.ID != id {
			kept = append(kept, cred)
		}
	}
	f.credentials = kept
}

func (f *accountRouteFixture) replaceState(state store.SiteAPIKeyState) {
	for index := range f.states {
		if f.states[index].SiteCredentialID == state.SiteCredentialID {
			f.states[index] = state
			return
		}
	}
	f.states = append(f.states, state)
}

func (f *accountRouteFixture) removeState(id uuid.UUID) {
	kept := f.states[:0]
	for _, state := range f.states {
		if state.SiteCredentialID != id {
			kept = append(kept, state)
		}
	}
	f.states = kept
}

func accountRows(tx *gorm.DB, n int) {
	tx.RowsAffected = int64(n)
	tx.Statement.RowsAffected = int64(n)
}

func accountMetaJSON(value any) (store.JSON, bool) {
	switch typed := value.(type) {
	case store.JSON:
		return typed, true
	case []byte:
		return store.JSON(typed), true
	case string:
		return store.JSON(typed), true
	default:
		return nil, false
	}
}

func accountClauseIDs(tx *gorm.DB) []uuid.UUID {
	ids, _ := accountClauseValues(tx)
	return ids
}

func accountClauseValues(tx *gorm.DB) ([]uuid.UUID, []string) {
	var ids []uuid.UUID
	var texts []string
	if tx.Statement != nil {
		if where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where); ok {
			for _, expr := range where.Exprs {
				collectAccountClauseValues(expr, &ids, &texts)
			}
		}
		for _, value := range tx.Statement.Vars {
			collectAccountClauseValue(value, &ids, &texts)
		}
	}
	return ids, texts
}

func collectAccountClauseValues(expr clause.Expression, ids *[]uuid.UUID, texts *[]string) {
	if expr == nil {
		return
	}
	switch typed := expr.(type) {
	case clause.Where:
		for _, item := range typed.Exprs {
			collectAccountClauseValues(item, ids, texts)
		}
	case clause.AndConditions:
		for _, item := range typed.Exprs {
			collectAccountClauseValues(item, ids, texts)
		}
	case clause.OrConditions:
		for _, item := range typed.Exprs {
			collectAccountClauseValues(item, ids, texts)
		}
	case clause.Eq:
		collectAccountClauseValue(typed.Value, ids, texts)
	case clause.IN:
		for _, value := range typed.Values {
			collectAccountClauseValue(value, ids, texts)
		}
	case clause.Expr:
		for _, value := range typed.Vars {
			collectAccountClauseValue(value, ids, texts)
		}
	case clause.NamedExpr:
		for _, value := range typed.Vars {
			collectAccountClauseValue(value, ids, texts)
		}
	}
}

func collectAccountClauseValue(value any, ids *[]uuid.UUID, texts *[]string) {
	switch typed := value.(type) {
	case uuid.UUID:
		*ids = append(*ids, typed)
	case *uuid.UUID:
		if typed != nil {
			*ids = append(*ids, *typed)
		}
	case string:
		if id, err := uuid.Parse(typed); err == nil {
			*ids = append(*ids, id)
			return
		}
		if typed != "" {
			*texts = append(*texts, typed)
		}
	}
}

func containsUUID(ids []uuid.UUID, id uuid.UUID) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

func setUnexported(target any, name string, value any) {
	field := reflect.ValueOf(target).Elem().FieldByName(name)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

func unexported(target any, name string) any {
	field := reflect.ValueOf(target).Elem().FieldByName(name)
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface()
}

type scriptTransport func(*http.Request) (*http.Response, error)

func (s scriptTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return s(r)
}

func jsonHTTP(status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func grokTokenTransport(body string, status int) http.RoundTripper {
	return scriptTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/oauth2/token") {
			return jsonHTTP(http.StatusNotFound, `{}`), nil
		}
		return jsonHTTP(status, body), nil
	})
}
