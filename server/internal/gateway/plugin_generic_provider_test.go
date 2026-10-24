package gateway

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	"xlyra/server/internal/httpclient"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestPluginSiteTypeGenericProvider(t *testing.T) {
	site.ClearPluginSiteTypes()
	ClearRuntimeProviderSpecs()
	t.Cleanup(func() {
		site.ClearPluginSiteTypes()
		ClearRuntimeProviderSpecs()
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4","object":"model"}]}`))
	}))
	t.Cleanup(server.Close)

	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{
			SiteType:        "acme.example/chat",
			DisplayName:     "Acme",
			OfficialBaseURL: "http://a.test",
		},
		PluginID: "acme.example",
		Enabled:  true,
		Hosts:    []string{"a.test"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		"acme.example/chat": json.RawMessage(`{
			"protocol": "openai_chat",
			"official_base_url": "https://api.example.com",
			"path": "/api/chat/completions"
		}`),
	}); err != nil {
		t.Fatal(err)
	}

	mem := &pluginProviderMemory{}
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := confFile.Set("network", httpclient.NetworkConfig{Proxies: []httpclient.ProxyProfile{{
		ID: "plugin-upstream", Name: "plugin-upstream", Type: "http", URL: server.URL,
	}}}); err != nil {
		t.Fatal(err)
	}
	svc := site.NewService(gatewayStoreWithGorm(t, pluginProviderGorm(t, mem)), "test-master-key", confFile)
	module, err := adapter.NewGenericProvider(adapter.GenericProviderConfig{
		SiteType:        "acme.example/chat",
		DisplayName:     "Acme",
		OfficialBaseURL: "http://a.test",
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    []string{"a.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPluginAdapterModules([]adapter.Module{module}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.SetPluginAdapterModules(nil) })

	ctx := context.Background()
	_, _, err = svc.Create(ctx, site.CreateSiteParams{
		Name:     "Bad",
		Slug:     "acme-bad",
		SiteType: "acme.example/chat",
		BaseURL:  "ftp://example.com",
		Enabled:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "base_url scheme must be http or https") {
		t.Fatalf("invalid base_url error = %v", err)
	}

	explicit, _, err := svc.Create(ctx, pluginProviderSiteParams("Acme Explicit", "acme-explicit", "http://a.test/tenant"))
	if err != nil {
		t.Fatal(err)
	}
	if explicit.BaseURL != "http://a.test/tenant" || explicit.SiteType != "acme.example/chat" {
		t.Fatalf("explicit site = %#v", explicit)
	}

	created, _, err := svc.Create(ctx, pluginProviderSiteParams("Acme", "acme", ""))
	if err != nil {
		t.Fatal(err)
	}
	if created.BaseURL != "http://a.test" || created.SiteType != "acme.example/chat" {
		t.Fatalf("default base site = %#v", created)
	}

	wantCapabilities := []string{"validate_credential", "list_models", "health_probe"}
	if got := svc.Capabilities("acme.example/chat"); !reflect.DeepEqual(got, wantCapabilities) {
		t.Fatalf("capabilities = %#v", got)
	}
	if err := svc.Validate(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	lister, ok := adapter.AsGatewayModelLister(module)
	if !ok {
		t.Fatal("generic provider does not list models")
	}
	models, err := lister.ListModels(ctx, adapter.SiteConfig{SiteType: created.SiteType, BaseURL: created.BaseURL, Client: pluginHostDialClient(map[string]string{"a.test": server.Listener.Addr().String()})}, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].UpstreamName != "claude-sonnet-4" {
		t.Fatalf("listed models = %#v", models)
	}
	assertEndpointTypes(t, models[0].Capabilities["supported_endpoint_types"], []string{"openai"})

	health, err := svc.CheckSiteHealth(ctx, created.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !health.Snapshot.Success || health.State.Status != "healthy" {
		t.Fatalf("health success=%v status=%s message=%v", health.Snapshot.Success, health.State.Status, health.State.Message)
	}

	synced, err := svc.SyncModels(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if synced.ModelCount != 1 || len(synced.Models) != 1 || synced.Models[0].UpstreamName != "claude-sonnet-4" {
		t.Fatalf("sync = %#v", synced)
	}
	assertStoredEndpointTypes(t, synced.Models[0].Capabilities)
	stored, err := svc.ListModels(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored models = %#v", stored)
	}
	assertStoredEndpointTypes(t, stored[0].Capabilities)

	protocol, err := (openAIProtocolResolver{}).Resolve(ctx, typedGatewayRequest(gatewayEndpointChatCompletions, map[string]any{
		"model": "claude-sonnet-4",
	}), routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: "acme.example/chat", BaseURL: server.URL},
		Model: routeengine.CandidateModel{
			UpstreamName:           "claude-sonnet-4",
			SupportedEndpointTypes: []string{"openai"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if protocol.ProtocolName() != "openai_chat_completions" {
		t.Fatalf("protocol = %q", protocol.ProtocolName())
	}
	if got, want := protocol.UpstreamPath(server.URL), server.URL+"/api/chat/completions"; got != want {
		t.Fatalf("upstream path = %q, want %q", got, want)
	}

	if err := svc.SetPluginAdapterModules(nil); err != nil {
		t.Fatal(err)
	}
	site.ClearPluginSiteTypes()
	_, _, err = svc.Create(ctx, pluginProviderSiteParams("Acme Again", "acme-again", server.URL))
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type`) {
		t.Fatalf("create after clear = %v", err)
	}
	_, _, err = svc.Update(ctx, site.UpdateSiteParams{
		ID:       created.ID,
		Name:     "Acme",
		Slug:     "acme",
		SiteType: "acme.example/chat",
		BaseURL:  "http://a.test",
		Enabled:  true,
	})
	if err == nil || !strings.Contains(err.Error(), `unsupported site_type`) {
		t.Fatalf("update after clear = %v", err)
	}

	for _, siteType := range []string{"openai", "deepseek"} {
		createdBuiltin, _, err := svc.Create(ctx, site.CreateSiteParams{
			Name:     siteType,
			Slug:     siteType + "-kept",
			SiteType: siteType,
			BaseURL:  server.URL,
			Enabled:  true,
			Credential: &site.CredentialInput{
				Secret: "sk-test",
			},
		})
		if err != nil {
			t.Fatalf("create %s: %v", siteType, err)
		}
		if createdBuiltin.SiteType != siteType || createdBuiltin.BaseURL != server.URL {
			t.Fatalf("%s site = %#v", siteType, createdBuiltin)
		}
		listed, err := svc.SyncModels(ctx, createdBuiltin.ID)
		if err != nil {
			t.Fatalf("list %s models: %v", siteType, err)
		}
		if listed.ModelCount != 1 || len(listed.Models) != 1 || listed.Models[0].UpstreamName != "claude-sonnet-4" {
			t.Fatalf("%s models = %#v", siteType, listed)
		}
	}
}

func pluginProviderSiteParams(name, slug, baseURL string) site.CreateSiteParams {
	proxyID := "plugin-upstream"
	return site.CreateSiteParams{
		Name:     name,
		Slug:     slug,
		SiteType: "acme.example/chat",
		BaseURL:  baseURL,
		Enabled:  true,
		ProxyID:  &proxyID,
		Credential: &site.CredentialInput{
			Secret: "sk-test",
		},
	}
}

func assertEndpointTypes(t *testing.T, value any, want []string) {
	t.Helper()
	got, ok := value.([]string)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoint types = %#v, want %#v", value, want)
	}
}

func assertStoredEndpointTypes(t *testing.T, raw store.JSON) {
	t.Helper()
	var caps map[string]any
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	values, _ := caps["supported_endpoint_types"].([]any)
	got := make([]string, 0, len(values))
	for _, value := range values {
		text, _ := value.(string)
		got = append(got, text)
	}
	if !reflect.DeepEqual(got, []string{"openai"}) {
		t.Fatalf("stored endpoint types = %#v", caps["supported_endpoint_types"])
	}
}

type pluginProviderMemory struct {
	mu          sync.Mutex
	sites       map[uuid.UUID]store.Site
	credentials []store.SiteCredential
	models      []store.SiteModel
	snapshots   []store.HealthSnapshot
	apiModels   []store.SiteAPIKeyModel
}

func pluginProviderGorm(t *testing.T, mem *pluginProviderMemory) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(pluginProviderConnector{})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open offline gorm db: %v", err)
	}
	pluginProviderReplaceCallback(t, db.Callback().Query(), "gorm:query", mem.query)
	pluginProviderReplaceCallback(t, db.Callback().Create(), "gorm:create", mem.create)
	pluginProviderReplaceCallback(t, db.Callback().Update(), "gorm:update", pluginProviderRows)
	pluginProviderReplaceCallback(t, db.Callback().Delete(), "gorm:delete", pluginProviderRows)
	return db
}

type pluginProviderCallbackSet interface {
	Replace(string, func(*gorm.DB)) error
}

func pluginProviderReplaceCallback(t *testing.T, processor pluginProviderCallbackSet, name string, callback func(*gorm.DB)) {
	t.Helper()
	if err := processor.Replace(name, callback); err != nil {
		t.Fatalf("replace %s: %v", name, err)
	}
}

func pluginProviderRows(tx *gorm.DB) {
	tx.RowsAffected = 1
	tx.Statement.RowsAffected = 1
}

func (m *pluginProviderMemory) query(tx *gorm.DB) {
	where := pluginProviderWhere(tx)
	switch dest := tx.Statement.Dest.(type) {
	case *store.Site:
		id, ok := pluginProviderUUID(where, "id")
		if !ok {
			tx.AddError(fmt.Errorf("site query missing id: %s", pluginProviderClauseDump(tx)))
			return
		}
		m.mu.Lock()
		item, found := m.sites[id]
		m.mu.Unlock()
		if !found {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = item
	case *[]store.SiteCredential:
		siteID, ok := pluginProviderUUID(where, "site_id")
		if !ok {
			tx.AddError(fmt.Errorf("credential query missing site_id: %s", pluginProviderClauseDump(tx)))
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		items := make([]store.SiteCredential, 0)
		for _, item := range m.credentials {
			if item.SiteID == siteID {
				items = append(items, item)
			}
		}
		*dest = items
	case *[]store.SiteModel:
		siteID, ok := pluginProviderUUID(where, "site_id")
		if !ok {
			tx.AddError(fmt.Errorf("model query missing site_id: %s", pluginProviderClauseDump(tx)))
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		items := make([]store.SiteModel, 0)
		for _, item := range m.models {
			if item.SiteID == siteID {
				items = append(items, item)
			}
		}
		*dest = items
	case *[]store.HealthSnapshot:
		siteID, ok := pluginProviderUUID(where, "site_id")
		if !ok {
			tx.AddError(fmt.Errorf("snapshot query missing site_id: %s", pluginProviderClauseDump(tx)))
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		items := make([]store.HealthSnapshot, 0)
		for _, item := range m.snapshots {
			if item.SiteID == siteID {
				items = append(items, item)
			}
		}
		*dest = items
	case *[]store.SiteAPIKeyModel:
		m.mu.Lock()
		defer m.mu.Unlock()
		items := make([]store.SiteAPIKeyModel, 0)
		for _, item := range m.apiModels {
			if id, ok := pluginProviderUUID(where, "site_credential_id"); ok && item.SiteCredentialID != id {
				continue
			}
			if id, ok := pluginProviderUUID(where, "site_id"); ok && item.SiteID != id {
				continue
			}
			items = append(items, item)
		}
		*dest = items
	default:
		value := reflect.ValueOf(tx.Statement.Dest)
		if value.Kind() == reflect.Ptr && value.Elem().Kind() == reflect.Slice {
			value.Elem().Set(reflect.MakeSlice(value.Elem().Type(), 0, 0))
			return
		}
		if value.Kind() == reflect.Ptr && (value.Elem().Kind() == reflect.Int || value.Elem().Kind() == reflect.Int64) {
			value.Elem().SetZero()
			return
		}
		tx.AddError(gorm.ErrRecordNotFound)
	}
}

func (m *pluginProviderMemory) create(tx *gorm.DB) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sites == nil {
		m.sites = map[uuid.UUID]store.Site{}
	}
	switch item := tx.Statement.Dest.(type) {
	case *store.Site:
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		item.Meta = append(store.JSON(nil), item.Meta...)
		m.sites[item.ID] = *item
	case *store.SiteCredential:
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		item.Meta = append(store.JSON(nil), item.Meta...)
		m.credentials = append(m.credentials, *item)
	case *store.SiteModel:
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		item.Capabilities = append(store.JSON(nil), item.Capabilities...)
		m.models = append(m.models, *item)
	case *store.HealthSnapshot:
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		item.Metadata = append(store.JSON(nil), item.Metadata...)
		m.snapshots = append(m.snapshots, *item)
	case *store.SiteHealthState:
	case *store.SiteAPIKeyState:
	case *store.SiteAPIKeyModel:
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		item.Raw = append(store.JSON(nil), item.Raw...)
		m.apiModels = append(m.apiModels, *item)
	default:
		tx.AddError(fmt.Errorf("unexpected create %T", tx.Statement.Dest))
		return
	}
	pluginProviderRows(tx)
}

func pluginProviderWhere(tx *gorm.DB) map[string]any {
	out := map[string]any{}
	c, ok := tx.Statement.Clauses["WHERE"]
	if !ok || c.Expression == nil {
		return out
	}
	walkPluginProviderClause(c.Expression, out)
	return out
}

func walkPluginProviderClause(expr clause.Expression, out map[string]any) {
	switch e := expr.(type) {
	case clause.Where:
		for _, item := range e.Exprs {
			walkPluginProviderClause(item, out)
		}
	case clause.AndConditions:
		for _, item := range e.Exprs {
			walkPluginProviderClause(item, out)
		}
	case clause.OrConditions:
		for _, item := range e.Exprs {
			walkPluginProviderClause(item, out)
		}
	case clause.Eq:
		if name := pluginProviderColumn(e.Column); name != "" {
			out[name] = e.Value
		}
	}
}

func pluginProviderColumn(column any) string {
	switch c := column.(type) {
	case string:
		return c
	case clause.Column:
		return c.Name
	default:
		return ""
	}
}

func pluginProviderUUID(where map[string]any, key string) (uuid.UUID, bool) {
	raw, ok := where[key]
	if !ok || raw == nil {
		return uuid.Nil, false
	}
	switch v := raw.(type) {
	case uuid.UUID:
		return v, true
	case string:
		id, err := uuid.Parse(v)
		return id, err == nil
	case []byte:
		id, err := uuid.ParseBytes(v)
		return id, err == nil
	default:
		id, err := uuid.Parse(fmt.Sprint(v))
		return id, err == nil
	}
}

func pluginProviderClauseDump(tx *gorm.DB) string {
	c, ok := tx.Statement.Clauses["WHERE"]
	if !ok {
		return "no where"
	}
	return fmt.Sprintf("%T %#v", c.Expression, c.Expression)
}

type pluginProviderConnector struct{}

func (pluginProviderConnector) Connect(context.Context) (driver.Conn, error) {
	return pluginProviderConn{}, nil
}

func (pluginProviderConnector) Driver() driver.Driver {
	return pluginProviderDriver{}
}

type pluginProviderDriver struct{}

func (pluginProviderDriver) Open(string) (driver.Conn, error) {
	return pluginProviderConn{}, nil
}

type pluginProviderConn struct{}

func (pluginProviderConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("plugin provider fake driver only supports transactions")
}

func (pluginProviderConn) Close() error { return nil }

func (pluginProviderConn) Begin() (driver.Tx, error) { return pluginProviderTx{}, nil }

func (pluginProviderConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return pluginProviderTx{}, nil
}

type pluginProviderTx struct{}

func (pluginProviderTx) Commit() error { return nil }

func (pluginProviderTx) Rollback() error { return nil }
