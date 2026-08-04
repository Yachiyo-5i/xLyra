package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/go-chi/chi/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/newapi"
	oauthsvc "xlyra/server/internal/oauth"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

const adminTestMasterKey = "test-master-key"

func adminTestRequest(method string, target string, body string) *http.Request {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	return httptest.NewRequest(method, target, reader)
}

func adminJSONRequest(t *testing.T, method string, target string, payload any) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func adminPerform(handler func(http.ResponseWriter, *http.Request), req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func adminRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func adminAssertStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
}

func adminDecodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var body T
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func assertAdminErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	adminAssertStatus(t, rec, status)
	body := adminDecodeJSON[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q", body.Error.Code, code)
	}
}

func adminAssertParserError(t *testing.T, rec *httptest.ResponseRecorder, ok bool, code string) {
	t.Helper()

	if ok {
		t.Fatalf("expected parser to fail with %s", code)
	}
	assertAdminErrorCode(t, rec, http.StatusBadRequest, code)
}

func adminRequireParserOK(t *testing.T, rec *httptest.ResponseRecorder, ok bool, label string) {
	t.Helper()

	if !ok {
		t.Fatalf("%s unexpectedly failed: status=%d body=%s", label, rec.Code, rec.Body.String())
	}
}

func adminRequestWithRouteVars(req *http.Request, vars map[string]string) *http.Request {
	routeCtx := chi.NewRouteContext()
	for key, value := range vars {
		routeCtx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func adminRequestWithRouteParam(method string, target string, body string, key string, value string) *http.Request {
	req := adminTestRequest(method, target, body)
	return adminRequestWithRouteVars(req, map[string]string{key: value})
}

func adminRequestWithRouteParams(method string, target string, body string, vars map[string]string) *http.Request {
	req := adminTestRequest(method, target, body)
	return adminRequestWithRouteVars(req, vars)
}

func withRouteParam(req *http.Request, key string, value string) *http.Request {
	routeCtx := chi.RouteContext(req.Context())
	if routeCtx == nil {
		routeCtx = chi.NewRouteContext()
	}
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func adminAuthService() *auth.Service {
	return auth.NewService(nil, adminTestMasterKey)
}

func adminSiteService() *sitepkg.Service {
	return sitepkg.NewService(nil, adminTestMasterKey)
}

func adminOAuthService() *oauthsvc.Service {
	return oauthsvc.NewService(nil, adminTestMasterKey)
}

func adminHandlerWithAuthService() Handler {
	return NewHandler(nil, adminAuthService(), nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func adminHandlerWithSiteService() Handler {
	return NewHandler(nil, nil, adminSiteService(), nil, nil, nil, nil, nil, nil, nil, nil)
}

func adminHandlerWithSiteAndOAuthServices() Handler {
	return NewHandler(nil, nil, adminSiteService(), nil, nil, nil, nil, nil, nil, nil, adminOAuthService())
}

func adminHandlerWithNewAPIService() Handler {
	return NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, newapi.NewService(), nil)
}

func adminHandlerWithSiteAndNewAPIService() Handler {
	return NewHandler(nil, nil, adminSiteService(), nil, nil, nil, nil, nil, nil, newapi.NewService(), nil)
}

func adminOfflineGorm(t *testing.T, configure func(*gorm.DB) error) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=xlyra dbname=xlyra sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open offline gorm db: %v", err)
	}
	if configure != nil {
		if err := configure(db); err != nil {
			t.Fatalf("configure offline gorm db: %v", err)
		}
	}
	return db
}

type adminGormCallbacks struct {
	query          func(*gorm.DB)
	create         func(*gorm.DB)
	update         func(*gorm.DB)
	deleteCallback func(*gorm.DB)
}

func adminGormWithCallbacks(t *testing.T, callbacks adminGormCallbacks) *gorm.DB {
	t.Helper()

	return adminOfflineGorm(t, func(db *gorm.DB) error {
		if callbacks.query != nil {
			if err := db.Callback().Query().Replace("gorm:query", callbacks.query); err != nil {
				return err
			}
		}
		if callbacks.create != nil {
			if err := db.Callback().Create().Replace("gorm:create", callbacks.create); err != nil {
				return err
			}
		}
		if callbacks.update != nil {
			if err := db.Callback().Update().Replace("gorm:update", callbacks.update); err != nil {
				return err
			}
		}
		if callbacks.deleteCallback != nil {
			return db.Callback().Delete().Replace("gorm:delete", callbacks.deleteCallback)
		}
		return nil
	})
}

func adminStoreWithCallbacks(t *testing.T, callbacks adminGormCallbacks) *store.Store {
	t.Helper()

	return adminStoreWithGorm(adminGormWithCallbacks(t, callbacks))
}

func adminStoreWithGorm(db *gorm.DB) *store.Store {
	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}
