package store

import (
	"bytes"
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/migrations"
)

type gooseVersionRecord struct {
	ID        int64
	VersionID int64
	IsApplied bool
	Tstamp    time.Time
}

func (gooseVersionRecord) TableName() string {
	return "goose_db_version"
}

func TestDevPostgresMigrationsInitializeNewSchema(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema migrations: %v", err)
	}

	migrator := db.Migrator()
	if !migrator.HasTable(&OAuthConnection{}) {
		t.Fatal("oauth_connections table was not created")
	}
	if !migrator.HasColumn(&OAuthConnection{}, "RefreshLeaseID") {
		t.Fatal("oauth_connections.refresh_lease_id column was not created")
	}
	if !migrator.HasColumn(&OAuthConnection{}, "RefreshLeaseUntil") {
		t.Fatal("oauth_connections.refresh_lease_until column was not created")
	}
	if !migrator.HasTable(&OAuthQuotaSnapshot{}) {
		t.Fatal("oauth_quota_snapshots table was not created")
	}
	if !migrator.HasTable(&OAuthQuotaWindowRound{}) {
		t.Fatal("oauth_quota_window_rounds table was not created")
	}
	if !migrator.HasTable(&CacheObservation{}) {
		t.Fatal("cache_observations table was not created")
	}
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
}

func TestDevPostgresMigrationsUpgradeExistingSchema(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get postgres sql db: %v", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatalf("apply initial schema migration: %v", err)
	}
	if db.Migrator().HasColumn(&OAuthConnection{}, "RefreshLeaseID") {
		t.Fatal("refresh lease column exists before upgrade migration")
	}

	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("upgrade schema migrations: %v", err)
	}
	if !db.Migrator().HasColumn(&OAuthConnection{}, "RefreshLeaseID") {
		t.Fatal("refresh lease column was not added by upgrade migration")
	}
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
}

func TestDevPostgresMigrationsAreRepeatable(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()

	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
}

func TestDevPostgresFailedMigrationIsNotRecordedAndCanRetry(t *testing.T) {
	db, _, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get postgres sql db: %v", err)
	}

	if err := runMigrations(ctx, sqlDB, os.DirFS("testdata/migrations_failure")); err == nil {
		t.Fatal("failing migration run returned nil error")
	}
	assertAppliedMigrationVersions(t, db, []int64{0, 1})

	if err := runMigrations(ctx, sqlDB, os.DirFS("testdata/migrations_retry")); err != nil {
		t.Fatalf("retry migration run: %v", err)
	}
	if !db.Migrator().HasColumn("migration_retry_probe", "recovered") {
		t.Fatal("retry migration did not add recovered column")
	}
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2})
}

func openTemporaryMigrationStore(t *testing.T) (*gorm.DB, config.Config, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cfg, err := devPostgresSmokeConfig()
	if err != nil {
		cancel()
		t.Skipf("dev PostgreSQL migrations disabled: %v", err)
	}
	baseCfg := cfg
	schemaName := "xlyra_migration_test_" + uuid.NewString()
	schemaName = strings.ReplaceAll(schemaName, "-", "")
	setupFS := migrationTestFS(t, "migrations_setup", schemaName)
	cleanupFS := migrationTestFS(t, "migrations_cleanup", schemaName)
	if err := runUnversionedMigrations(ctx, baseCfg, setupFS); err != nil {
		cancel()
		t.Skipf("prepare migration test schema: %v", err)
	}
	parsed, err := url.Parse(cfg.DatabaseDSN())
	if err != nil {
		cleanupMigrationTestSchema(t, baseCfg, cleanupFS)
		cancel()
		t.Fatalf("parse postgres DSN: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	cfg.PostgresDSN = parsed.String()
	cfg.DBMinConns = 0
	cfg.DBMaxConns = 1
	store, err := Open(ctx, cfg)
	if err != nil {
		cleanupMigrationTestSchema(t, baseCfg, cleanupFS)
		cancel()
		t.Skipf("dev PostgreSQL unavailable: %s", redactDatabaseOpenError(err, cfg))
	}
	return store.DB(), cfg, func() {
		store.Close()
		cleanupMigrationTestSchema(t, baseCfg, cleanupFS)
		cancel()
	}
}

func migrationTestFS(t *testing.T, sourceDir string, schemaName string) fs.FS {
	t.Helper()
	root := t.TempDir()
	sourceRoot := filepath.Join("testdata", sourceDir)
	if err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = bytes.ReplaceAll(data, []byte("xlyra_migration_test"), []byte(schemaName))
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("prepare migration test files: %v", err)
	}
	return os.DirFS(root)
}

func cleanupMigrationTestSchema(t *testing.T, cfg config.Config, migrationFS fs.FS) {
	t.Helper()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runUnversionedMigrations(cleanupCtx, cfg, migrationFS); err != nil {
		t.Errorf("cleanup migration test schema: %v", err)
	}
}

func runUnversionedMigrations(ctx context.Context, cfg config.Config, migrationFS fs.FS) error {
	store, err := Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	sqlDB, err := store.DB().DB()
	if err != nil {
		return err
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetBaseFS(migrationFS)
	return goose.UpContext(ctx, sqlDB, ".", goose.WithNoVersioning())
}

func runMigrations(ctx context.Context, db *sql.DB, migrationFS fs.FS) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	goose.SetBaseFS(migrationFS)
	return goose.UpContext(ctx, db, ".")
}

func assertAppliedMigrationVersions(t *testing.T, db *gorm.DB, want []int64) {
	t.Helper()
	var records []gooseVersionRecord
	if err := db.Find(&records).Error; err != nil {
		t.Fatalf("list goose migration versions: %v", err)
	}
	got := make([]int64, 0, len(records))
	for _, record := range records {
		if record.IsApplied {
			got = append(got, record.VersionID)
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != len(want) {
		t.Fatalf("applied migration versions = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("applied migration versions = %v, want %v", got, want)
		}
	}
}

// Bindings mount a plugin on a scope in a slot. Every site kind must be accepted
// without touching the table (the old CHECK had to be widened per kind), a scope
// holds one plugin per slot, and an unknown scope type is rejected.
func TestJSPluginBindingsMountOnScopes(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema migrations: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO js_plugins (id, source) VALUES ('acme-models', 'uploaded')`,
		`INSERT INTO js_plugin_versions (plugin_id, version, manifest, package, package_sha256, status, selftest)
		 VALUES ('acme-models', '1.0.0', '{}', '\x00', 'sha', 'enabled', '{}')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed plugin: %v", err)
		}
	}
	repo := NewJSPluginRepository(db)
	siteID := uuid.NewString()
	for _, kind := range []string{"quota_probe", "model_list", "credential_check", "error_classifier", "pricing_parse"} {
		// No ID or slot, like the real callers: bindings must not collide on a default one.
		err := repo.UpsertBinding(ctx, JSPluginBinding{
			PluginID: "acme-models", Version: "1.0.0",
			Kind: kind, ScopeType: JSPluginScopeSite, ScopeID: siteID,
		})
		if err != nil {
			t.Fatalf("bind %s: %v", kind, err)
		}
	}
	// Binding the same slot again replaces the mount instead of adding a second.
	if err := repo.UpsertBinding(ctx, JSPluginBinding{
		PluginID: "acme-models", Version: "1.0.0",
		Kind: "model_list", ScopeType: JSPluginScopeSite, ScopeID: siteID,
		Config: JSON(`{"mode":"strict"}`),
	}); err != nil {
		t.Fatalf("rebind model_list: %v", err)
	}
	bindings, err := repo.ListBindings(ctx)
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	if len(bindings) != 5 {
		t.Fatalf("bindings = %d, want 5 (one per slot)", len(bindings))
	}
	for _, binding := range bindings {
		if binding.Kind == "model_list" && string(binding.Config) != `{"mode": "strict"}` {
			t.Fatalf("model_list config = %s, want the rebind's config", binding.Config)
		}
	}
	if err := repo.UpsertBinding(ctx, JSPluginBinding{
		PluginID: "acme-models", Version: "1.0.0", Kind: "model_list", ScopeType: "made_up", ScopeID: siteID,
	}); err == nil {
		t.Fatal("an unknown scope type was accepted")
	}
	if err := repo.UpsertBinding(ctx, JSPluginBinding{
		PluginID: "acme-models", Version: "1.0.0", Kind: "protocol", ScopeType: JSPluginScopeEndpoint, ScopeID: "acme",
	}); err != nil {
		t.Fatalf("bind endpoint: %v", err)
	}
	slugs, err := repo.ProtocolSlugMap(ctx)
	if err != nil || slugs["acme"] != "acme-models" || len(slugs) != 1 {
		t.Fatalf("protocol slug map = %v, %v", slugs, err)
	}
	if err := repo.DeleteBinding(ctx, JSPluginScopeSite, siteID, "model_list"); err != nil {
		t.Fatalf("unbind: %v", err)
	}
}

// The mount migration rewrites bindings written under the old target_kind scheme.
func TestJSPluginBindingMigrationConvertsLegacyRows(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get postgres sql db: %v", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatalf("create migration provider: %v", err)
	}
	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatalf("apply migrations up to 8: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO js_plugins (id, source) VALUES ('acme', 'uploaded')`,
		`INSERT INTO js_plugin_versions (plugin_id, version, manifest, package, package_sha256, status, selftest)
		 VALUES ('acme', '1.0.0', '{}', '\x00', 'sha', 'enabled', '{}')`,
		`INSERT INTO js_plugin_bindings (plugin_id, version, target_kind, target_id) VALUES
		 ('acme', '1.0.0', 'site_quota_probe', 'site-1'),
		 ('acme', '1.0.0', 'protocol_endpoint', 'acme-slug'),
		 ('acme', '1.0.0', 'site_plugin:model_list', 'site-1')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed legacy rows: %v", err)
		}
	}
	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatalf("apply mount migration: %v", err)
	}
	_ = cfg
	got := map[string]string{}
	bindings, err := NewJSPluginRepository(db).ListBindings(ctx)
	if err != nil {
		t.Fatalf("list bindings: %v", err)
	}
	for _, binding := range bindings {
		got[binding.Kind] = binding.ScopeType + ":" + binding.ScopeID + ":" + binding.Slot
	}
	want := map[string]string{
		"quota_probe": "site:site-1:quota_probe",
		"protocol":    "endpoint:acme-slug:protocol",
		"model_list":  "site:site-1:model_list",
	}
	for kind, value := range want {
		if got[kind] != value {
			t.Fatalf("binding %s = %q, want %q (all: %v)", kind, got[kind], value, got)
		}
	}
}

// A connection that prepared a statement before another connection changed the
// table must not fail afterwards. The default (prepared statement) store does,
// which is why the startup schema step opens its own store.
func TestSchemaWorkStoreSurvivesTableChangesFromAnotherConnection(t *testing.T) {
	_, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	cfg.DBMinConns, cfg.DBMaxConns = 0, 2

	staleAfterAlter := func(open func(context.Context, config.Config) (*Store, error)) error {
		store, err := open(ctx, cfg)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer store.Close()
		sqlDB, err := store.DB().DB()
		if err != nil {
			t.Fatal(err)
		}
		table := "plan_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		if _, err := sqlDB.ExecContext(ctx, "CREATE TABLE "+table+" (a int)"); err != nil {
			t.Fatalf("create: %v", err)
		}
		reader, err := sqlDB.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		query := func() error {
			rows, err := reader.QueryContext(ctx, "SELECT * FROM "+table)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
			}
			return rows.Err()
		}
		if err := query(); err != nil {
			t.Fatalf("first query: %v", err)
		}
		// The change comes from a different connection, like a migration would.
		if _, err := sqlDB.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN b int"); err != nil {
			t.Fatalf("alter: %v", err)
		}
		return query()
	}

	if err := staleAfterAlter(Open); err == nil {
		t.Log("the prepared statement store no longer fails here; the guard below is still correct")
	} else {
		t.Logf("prepared statement store, as expected: %v", err)
	}
	if err := staleAfterAlter(openForSchemaWork); err != nil {
		t.Fatalf("schema work store failed after a table change: %v", err)
	}
}

// Uninstalling the last version must not leave an empty plugin behind, while
// uninstalling one of several versions keeps the plugin.
func TestJSPluginDeleteVersionRemovesEmptyPlugin(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema migrations: %v", err)
	}
	for _, statement := range []string{
		`INSERT INTO js_plugins (id, source) VALUES ('acme-models', 'uploaded')`,
		`INSERT INTO js_plugin_versions (plugin_id, version, manifest, package, package_sha256, status, selftest)
		 VALUES ('acme-models', '1.0.0', '{}', '\x00', 'a', 'disabled', '{}'),
		        ('acme-models', '1.0.1', '{}', '\x00', 'b', 'disabled', '{}')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed plugin: %v", err)
		}
	}
	repo := NewJSPluginRepository(db)
	pluginCount := func() int64 {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM js_plugins WHERE id = 'acme-models'`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := repo.DeleteVersion(ctx, "acme-models", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if pluginCount() != 1 {
		t.Fatal("the plugin was removed while a version was still installed")
	}
	if err := repo.DeleteVersion(ctx, "acme-models", "1.0.1"); err != nil {
		t.Fatal(err)
	}
	if pluginCount() != 0 {
		t.Fatal("an empty plugin was left behind after the last version was uninstalled")
	}
}
