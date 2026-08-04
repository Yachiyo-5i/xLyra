package router

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

func TestReadMethodsReturnRepositoryErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queryErr := errors.New("router query stopped")
	service := routerServiceWithQueryInterceptor(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	if _, err := service.ActiveCooldowns(ctx); !errors.Is(err, queryErr) {
		t.Fatalf("ActiveCooldowns error = %v, want wrapped query error", err)
	}
	if _, err := service.Overview(ctx, time.Time{}); !errors.Is(err, queryErr) {
		t.Fatalf("Overview error = %v, want wrapped query error", err)
	}
	if _, err := service.resolveCanonicalModel(ctx, "openai/gpt-5"); !errors.Is(err, queryErr) {
		t.Fatalf("resolveCanonicalModel error = %v, want wrapped query error", err)
	}
	if _, err := service.Candidates(ctx, CandidateQuery{ModelKey: "openai/gpt-5"}); !errors.Is(err, queryErr) {
		t.Fatalf("Candidates error = %v, want wrapped query error", err)
	}
}

func TestResolveCanonicalModelReportsAliasMiss(t *testing.T) {
	t.Parallel()

	service := routerServiceWithQueryInterceptor(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})

	_, err := service.resolveCanonicalModel(context.Background(), "provider/unknown-model")
	if err == nil {
		t.Fatal("expected missing canonical model error")
	}
	if got, want := err.Error(), `canonical model "provider/unknown-model" was not found`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestCooldownMutationsPrepareInputsBeforeRepositoryErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	updateErr := errors.New("cooldown update stopped")
	db := routerTransactionPostgresGorm(t)
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.AddError(updateErr)
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
	service := NewService(routerStoreBackedByGorm(t, db))

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	_, err := service.ActivateCooldown(ctx, CooldownInput{
		SiteID:           siteID,
		SiteModelID:      &modelID,
		SiteCredentialID: &credentialID,
		Metadata:         map[string]any{"reason_code": "unit-test"},
	})
	if !errors.Is(err, updateErr) || !strings.Contains(err.Error(), "clear existing route cooldowns") {
		t.Fatalf("ActivateCooldown error = %v, want wrapped clear update error", err)
	}

	err = service.ClearCooldown(ctx, siteID, &modelID, &credentialID, "manual")
	if !errors.Is(err, updateErr) || !strings.Contains(err.Error(), "clear route cooldowns") {
		t.Fatalf("ClearCooldown error = %v, want wrapped clear update error", err)
	}
}

func routerServiceWithQueryInterceptor(t *testing.T, callback func(*gorm.DB)) *Service {
	t.Helper()

	db := routerOfflinePostgresGorm(t)
	if err := db.Callback().Query().Replace("gorm:query", callback); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	return NewService(routerStoreBackedByGorm(t, db))
}

func routerOfflinePostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=xlyra dbname=xlyra sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open offline gorm db: %v", err)
	}
	return db
}

func routerTransactionPostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(routerTransactionConnector{})
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open transaction gorm db: %v", err)
	}
	return db
}

type routerTransactionConnector struct{}

func (routerTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return routerTransactionConn{}, nil
}

func (routerTransactionConnector) Driver() driver.Driver {
	return routerTransactionDriver{}
}

type routerTransactionDriver struct{}

func (routerTransactionDriver) Open(string) (driver.Conn, error) {
	return routerTransactionConn{}, nil
}

type routerTransactionConn struct{}

func (routerTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("router fake driver only supports transactions")
}

func (routerTransactionConn) Close() error {
	return nil
}

func (routerTransactionConn) Begin() (driver.Tx, error) {
	return routerTransactionTx{}, nil
}

func (routerTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return routerTransactionTx{}, nil
}

type routerTransactionTx struct{}

func (routerTransactionTx) Commit() error {
	return nil
}

func (routerTransactionTx) Rollback() error {
	return nil
}

func routerStoreBackedByGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}
