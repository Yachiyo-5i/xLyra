package gateway

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormcallbacks "gorm.io/gorm/callbacks"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	"xlyra/server/internal/credential"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

type softMappingFakeDriver struct{}

func (softMappingFakeDriver) Open(string) (driver.Conn, error) { return softMappingFakeConn{}, nil }

type softMappingFakeConn struct{}

func (softMappingFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported by the soft mapping fake driver")
}
func (softMappingFakeConn) Close() error              { return nil }
func (softMappingFakeConn) Begin() (driver.Tx, error) { return softMappingFakeTx{}, nil }

type softMappingFakeTx struct{}

func (softMappingFakeTx) Commit() error   { return nil }
func (softMappingFakeTx) Rollback() error { return nil }

var softMappingRegisterDriver sync.Once

func softMappingGorm(t *testing.T) *gorm.DB {
	t.Helper()

	softMappingRegisterDriver.Do(func() {
		sql.Register("soft-mapping-fake", softMappingFakeDriver{})
	})
	sqlDB, err := sql.Open("soft-mapping-fake", "")
	if err != nil {
		t.Fatalf("open fake sql db: %v", err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open fake gorm db: %v", err)
	}
	return db
}

type softMappingUpstreamCall struct {
	Path  string
	Model string
}

type softMappingHarness struct {
	mu            sync.Mutex
	upstreamCalls []softMappingUpstreamCall
	requestLogs   []store.RequestLog

	apiKey          store.APIKey
	canonicalModels map[string]store.CanonicalModel
	siteModels      []store.SiteModel
	sites           map[uuid.UUID]store.Site
	credentials     map[uuid.UUID]store.SiteCredential
}

func (h *softMappingHarness) recordUpstream(call softMappingUpstreamCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.upstreamCalls = append(h.upstreamCalls, call)
}

func (h *softMappingHarness) upstreamModels() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	models := make([]string, 0, len(h.upstreamCalls))
	for _, call := range h.upstreamCalls {
		models = append(models, call.Model)
	}
	return models
}

func (h *softMappingHarness) recordedLogs() []store.RequestLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]store.RequestLog(nil), h.requestLogs...)
}

func softMappingVarsContain(tx *gorm.DB, value string) bool {
	for _, item := range tx.Statement.Vars {
		switch typed := item.(type) {
		case string:
			if typed == value {
				return true
			}
		case uuid.UUID:
			if typed.String() == value {
				return true
			}
		}
	}
	return false
}

func newSoftMappingHandler(t *testing.T, harness *softMappingHarness) Handler {
	t.Helper()

	db := softMappingGorm(t)
	gatewayReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		gormcallbacks.BuildQuerySQL(tx)
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = harness.apiKey
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			models := make([]store.CanonicalModel, 0, len(harness.canonicalModels))
			for _, model := range harness.canonicalModels {
				models = append(models, model)
			}
			*dest = models
			tx.Statement.RowsAffected = int64(len(models))
		case *store.CanonicalModel:
			for key, model := range harness.canonicalModels {
				if softMappingVarsContain(tx, key) || softMappingVarsContain(tx, model.ID.String()) {
					*dest = model
					tx.Statement.RowsAffected = 1
					return
				}
			}
			tx.AddError(gorm.ErrRecordNotFound)
		case *store.CanonicalModelAlias:
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]store.SiteModel:
			harness.mu.Lock()
			models := make([]store.SiteModel, 0, len(harness.siteModels))
			for _, model := range harness.siteModels {
				if model.CanonicalID.Valid && softMappingVarsContain(tx, model.CanonicalID.UUID.String()) {
					models = append(models, model)
				}
			}
			harness.mu.Unlock()
			*dest = models
			tx.Statement.RowsAffected = int64(len(models))
		case *[]store.Site:
			sites := make([]store.Site, 0, len(harness.sites))
			for _, site := range harness.sites {
				sites = append(sites, site)
			}
			*dest = sites
			tx.Statement.RowsAffected = int64(len(sites))
		case *store.Site:
			for siteID, site := range harness.sites {
				if softMappingVarsContain(tx, siteID.String()) {
					*dest = site
					tx.Statement.RowsAffected = 1
					return
				}
			}
			tx.AddError(gorm.ErrRecordNotFound)
		case *[]store.SiteCredential:
			matched := make([]store.SiteCredential, 0, len(harness.credentials))
			for siteID, siteCredential := range harness.credentials {
				if softMappingVarsContain(tx, siteID.String()) {
					matched = append(matched, siteCredential)
				}
			}
			if len(matched) == 0 && len(tx.Statement.Vars) == 0 {
				for _, siteCredential := range harness.credentials {
					matched = append(matched, siteCredential)
				}
			}
			*dest = matched
			tx.Statement.RowsAffected = int64(len(matched))
		case *store.SiteCredential:
			for siteID, siteCredential := range harness.credentials {
				if softMappingVarsContain(tx, siteID.String()) {
					*dest = siteCredential
					tx.Statement.RowsAffected = 1
					return
				}
			}
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		if log, ok := tx.Statement.Dest.(*store.RequestLog); ok {
			harness.mu.Lock()
			harness.requestLogs = append(harness.requestLogs, *log)
			harness.mu.Unlock()
		}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
	if err := db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
	if err := db.Callback().Row().Replace("gorm:row", func(tx *gorm.DB) {}); err != nil {
		t.Fatalf("replace row callback: %v", err)
	}
	if err := db.Callback().Raw().Replace("gorm:raw", func(tx *gorm.DB) {}); err != nil {
		t.Fatalf("replace raw callback: %v", err)
	}

	st := gatewayStoreWithGorm(t, db)
	handler := NewHandlerWithTimeZone(gatewayDiscardLogger(), auth.NewService(db, "test-master-key"), routeengine.NewService(st), st, "test-master-key", config.ResolveTimeZone())
	handler.rateLimits = nil
	return handler
}

func softMappingUpstreamServer(t *testing.T, harness *softMappingHarness, failPrefixes map[string]int) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		model, _ := payload["model"].(string)
		harness.recordUpstream(softMappingUpstreamCall{Path: r.URL.Path, Model: model})
		for prefix, status := range failPrefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"upstream exploded","type":"server_error"}}`))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-soft","object":"chat.completion","model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"soft-ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func softMappingSiteModel(canonical store.CanonicalModel, site store.Site, upstreamModel string) store.SiteModel {
	return store.SiteModel{
		ID:           uuid.New(),
		SiteID:       site.ID,
		CanonicalID:  uuid.NullUUID{UUID: canonical.ID, Valid: true},
		UpstreamName: upstreamModel,
		DisplayName:  upstreamModel,
		Capabilities: store.JSON(`{"supported_endpoint_types":["openai"]}`),
		Status:       "active",
	}
}

type softMappingFixture struct {
	harness *softMappingHarness
	handler Handler
	apiKey  store.APIKey
}

func newSoftMappingFixture(t *testing.T, rules string, origHasRoute bool, origUpstreamStatus int) softMappingFixture {
	t.Helper()

	harness := &softMappingHarness{}
	failPrefixes := map[string]int{}
	if origUpstreamStatus >= 400 {
		failPrefixes["/orig"] = origUpstreamStatus
	}
	server := softMappingUpstreamServer(t, harness, failPrefixes)

	origCanonical := store.CanonicalModel{ID: uuid.New(), ModelKey: "orig-model", Status: "active"}
	fallbackCanonical := store.CanonicalModel{ID: uuid.New(), ModelKey: "fallback-model", Status: "active"}
	origSite := store.Site{ID: uuid.New(), Name: "Orig Site", Slug: "orig-site", SiteType: "openai", BaseURL: server.URL + "/orig", Enabled: true, Status: "active"}
	fallbackSite := store.Site{ID: uuid.New(), Name: "Fallback Site", Slug: "fallback-site", SiteType: "openai", BaseURL: server.URL + "/fallback", Enabled: true, Status: "active"}

	credentialService := credential.NewService("test-master-key")
	encrypted, masked, err := credentialService.Encrypt("sk-soft-mapping-test")
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}

	harness.apiKey = store.APIKey{
		ID:             uuid.New(),
		Name:           "soft-mapping-key",
		Status:         "active",
		ModelPolicy:    "allow_all",
		SitePolicy:     "allow_all",
		QuotaUnlimited: true,
		ModelMappings:  store.JSON(rules),
	}
	harness.canonicalModels = map[string]store.CanonicalModel{
		origCanonical.ModelKey:     origCanonical,
		fallbackCanonical.ModelKey: fallbackCanonical,
	}
	harness.siteModels = []store.SiteModel{softMappingSiteModel(fallbackCanonical, fallbackSite, "fallback-upstream-model")}
	if origHasRoute {
		harness.siteModels = append(harness.siteModels, softMappingSiteModel(origCanonical, origSite, "orig-upstream-model"))
	}
	harness.sites = map[uuid.UUID]store.Site{origSite.ID: origSite, fallbackSite.ID: fallbackSite}
	harness.credentials = map[uuid.UUID]store.SiteCredential{
		origSite.ID:     {ID: uuid.New(), SiteID: origSite.ID, CredentialType: "api_key", EncryptedSecret: encrypted, MaskedSecret: masked},
		fallbackSite.ID: {ID: uuid.New(), SiteID: fallbackSite.ID, CredentialType: "api_key", EncryptedSecret: encrypted, MaskedSecret: masked},
	}

	return softMappingFixture{harness: harness, handler: newSoftMappingHandler(t, harness), apiKey: harness.apiKey}
}

func (f softMappingFixture) do(t *testing.T, model string) *httptest.ResponseRecorder {
	t.Helper()

	body := `{"model":"` + model + `","messages":[{"role":"user","content":"hi"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.WithAPIKey(req.Context(), f.apiKey))
	recorder := httptest.NewRecorder()
	f.handler.ChatCompletions(recorder, req)
	return recorder
}

func softMappingLogMetadata(t *testing.T, log store.RequestLog) map[string]any {
	t.Helper()

	metadata := map[string]any{}
	if err := json.Unmarshal(log.Metadata, &metadata); err != nil {
		t.Fatalf("parse request log metadata: %v", err)
	}
	return metadata
}

func TestSoftMappingRoutesOriginalModelWhenAvailable(t *testing.T) {
	t.Parallel()

	fixture := newSoftMappingFixture(t, `[{"pattern":"orig-model","target":"fallback-model","mode":"soft"}]`, true, 0)
	recorder := fixture.do(t, "orig-model")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 via original model", recorder.Code, recorder.Body.String())
	}
	models := fixture.harness.upstreamModels()
	if len(models) != 1 || models[0] != "orig-upstream-model" {
		t.Fatalf("upstream models = %v, want single call with original upstream model", models)
	}
	logs := fixture.harness.recordedLogs()
	if len(logs) != 1 || !logs[0].Success {
		t.Fatalf("request logs = %d success=%v, want single success log", len(logs), len(logs) > 0 && logs[0].Success)
	}
	metadata := softMappingLogMetadata(t, logs[0])
	if _, ok := metadata["mapped_model"]; ok {
		t.Fatalf("metadata unexpectedly contains mapping info: %v", metadata["mapped_model"])
	}
}

func TestSoftMappingFallsBackWhenOriginalHasNoRoute(t *testing.T) {
	t.Parallel()

	fixture := newSoftMappingFixture(t, `[{"pattern":"orig-model","target":"fallback-model","mode":"soft"}]`, false, 0)
	recorder := fixture.do(t, "orig-model")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 via fallback", recorder.Code, recorder.Body.String())
	}
	models := fixture.harness.upstreamModels()
	if len(models) != 1 || models[0] != "fallback-upstream-model" {
		t.Fatalf("upstream models = %v, want single call with fallback upstream model", models)
	}
	logs := fixture.harness.recordedLogs()
	if len(logs) != 1 || !logs[0].Success {
		t.Fatalf("request logs = %d, want exactly one success log and no probe failure log", len(logs))
	}
	metadata := softMappingLogMetadata(t, logs[0])
	if metadata["original_model"] != "orig-model" || metadata["mapped_model"] != "fallback-model" || metadata["mapping_mode"] != "soft" {
		t.Fatalf("mapping metadata = original=%v mapped=%v mode=%v, want soft fallback recorded", metadata["original_model"], metadata["mapped_model"], metadata["mapping_mode"])
	}
}

func TestSoftMappingFallsBackWhenOriginalModelUnknown(t *testing.T) {
	t.Parallel()

	fixture := newSoftMappingFixture(t, `[{"pattern":"my-alias","target":"fallback-model","mode":"soft"}]`, false, 0)
	recorder := fixture.do(t, "my-alias")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 via fallback for unknown original", recorder.Code, recorder.Body.String())
	}
	models := fixture.harness.upstreamModels()
	if len(models) != 1 || models[0] != "fallback-upstream-model" {
		t.Fatalf("upstream models = %v, want fallback upstream model", models)
	}
	logs := fixture.harness.recordedLogs()
	if len(logs) != 1 || !logs[0].Success {
		t.Fatalf("request logs = %d, want single success log without probe failure", len(logs))
	}
	metadata := softMappingLogMetadata(t, logs[0])
	if metadata["original_model"] != "my-alias" || metadata["mapping_mode"] != "soft" {
		t.Fatalf("mapping metadata = %v/%v, want alias recorded as original with soft mode", metadata["original_model"], metadata["mapping_mode"])
	}
}

func TestSoftMappingFallsBackAfterUpstreamFailures(t *testing.T) {
	t.Parallel()

	fixture := newSoftMappingFixture(t, `[{"pattern":"orig-model","target":"fallback-model","mode":"soft"}]`, true, http.StatusInternalServerError)
	recorder := fixture.do(t, "orig-model")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200 after mid-request fallback", recorder.Code, recorder.Body.String())
	}
	models := fixture.harness.upstreamModels()
	if len(models) != 2 || models[0] != "orig-upstream-model" || models[1] != "fallback-upstream-model" {
		t.Fatalf("upstream models = %v, want original attempt then fallback", models)
	}
	logs := fixture.harness.recordedLogs()
	successCount := 0
	for _, log := range logs {
		if log.Success {
			successCount++
		}
	}
	if successCount != 1 {
		t.Fatalf("success logs = %d of %d, want exactly one", successCount, len(logs))
	}
	var successMetadata map[string]any
	for _, log := range logs {
		if log.Success {
			successMetadata = softMappingLogMetadata(t, log)
		}
	}
	if successMetadata["mapping_mode"] != "soft" || successMetadata["mapped_model"] != "fallback-model" {
		t.Fatalf("success metadata mapping = %v/%v, want soft fallback", successMetadata["mapping_mode"], successMetadata["mapped_model"])
	}
}

func TestSoftMappingFailsOnceWhenFallbackAlsoUnavailable(t *testing.T) {
	t.Parallel()

	fixture := newSoftMappingFixture(t, `[{"pattern":"orig-model","target":"fallback-model","mode":"soft"}]`, false, 0)
	fixture.harness.mu.Lock()
	fixture.harness.siteModels = nil
	fixture.harness.mu.Unlock()
	recorder := fixture.do(t, "orig-model")

	if recorder.Code < 400 {
		t.Fatalf("status = %d, want failure when both models unavailable", recorder.Code)
	}
	logs := fixture.harness.recordedLogs()
	if len(logs) != 1 || logs[0].Success {
		t.Fatalf("request logs = %d, want exactly one failure log", len(logs))
	}
	if calls := fixture.harness.upstreamModels(); len(calls) != 0 {
		t.Fatalf("upstream calls = %v, want none", calls)
	}
}

func TestHardMappingRewritesBeforePlanning(t *testing.T) {
	t.Parallel()

	fixture := newSoftMappingFixture(t, `[{"pattern":"orig-model","target":"fallback-model","mode":"hard"}]`, true, 0)
	recorder := fixture.do(t, "orig-model")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	models := fixture.harness.upstreamModels()
	if len(models) != 1 || models[0] != "fallback-upstream-model" {
		t.Fatalf("upstream models = %v, want direct rewrite to fallback upstream model", models)
	}
	metadata := softMappingLogMetadata(t, fixture.harness.recordedLogs()[0])
	if metadata["mapping_mode"] != "hard" || metadata["mapped_model"] != "fallback-model" {
		t.Fatalf("mapping metadata = %v/%v, want hard mapping recorded", metadata["mapping_mode"], metadata["mapped_model"])
	}
}
