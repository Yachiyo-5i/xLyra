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
	if !migrator.HasTable(&CacheObservation{}) {
		t.Fatal("cache_observations table was not created")
	}
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2, 3, 4})
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
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2, 3, 4})
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
	assertAppliedMigrationVersions(t, db, []int64{0, 1, 2, 3, 4})
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
