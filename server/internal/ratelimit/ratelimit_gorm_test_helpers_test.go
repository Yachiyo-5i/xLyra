package ratelimit

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"unsafe"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

type ratelimitGormCallbacks struct {
	create         func(*gorm.DB)
	query          func(*gorm.DB)
	update         func(*gorm.DB)
	deleteCallback func(*gorm.DB)
}

func ratelimitGormWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *gorm.DB {
	t.Helper()

	return ratelimitPostgresGormWithCallbacks(t, ratelimitGormCallbacks{query: callback})
}

func ratelimitPostgresGorm(t *testing.T) *gorm.DB {
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
	return db
}

func ratelimitTransactionGormWithCallbacks(t *testing.T, callbacks ratelimitGormCallbacks) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(ratelimitTransactionConnector{})
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
	if callbacks.create != nil {
		if err := db.Callback().Create().Replace("gorm:create", callbacks.create); err != nil {
			t.Fatalf("replace create callback: %v", err)
		}
	}
	if callbacks.query != nil {
		if err := db.Callback().Query().Replace("gorm:query", callbacks.query); err != nil {
			t.Fatalf("replace query callback: %v", err)
		}
	}
	if callbacks.update != nil {
		if err := db.Callback().Update().Replace("gorm:update", callbacks.update); err != nil {
			t.Fatalf("replace update callback: %v", err)
		}
	}
	deleteCallback := callbacks.deleteCallback
	if deleteCallback == nil {
		deleteCallback = func(tx *gorm.DB) {
			tx.Statement.RowsAffected = 1
		}
	}
	if err := db.Callback().Delete().Replace("gorm:delete", deleteCallback); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
	return db
}

func ratelimitPostgresGormWithCallbacks(t *testing.T, callbacks ratelimitGormCallbacks) *gorm.DB {
	t.Helper()

	db := ratelimitPostgresGorm(t)
	if callbacks.create != nil {
		if err := db.Callback().Create().Replace("gorm:create", callbacks.create); err != nil {
			t.Fatalf("replace create callback: %v", err)
		}
	}
	if callbacks.query != nil {
		if err := db.Callback().Query().Replace("gorm:query", callbacks.query); err != nil {
			t.Fatalf("replace query callback: %v", err)
		}
	}
	if callbacks.update != nil {
		if err := db.Callback().Update().Replace("gorm:update", callbacks.update); err != nil {
			t.Fatalf("replace update callback: %v", err)
		}
	}
	if callbacks.deleteCallback != nil {
		if err := db.Callback().Delete().Replace("gorm:delete", callbacks.deleteCallback); err != nil {
			t.Fatalf("replace delete callback: %v", err)
		}
	}
	return db
}

func ratelimitStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func ratelimitGormWithWindowCallbacks(t *testing.T, window store.GatewayRateLimitWindow, err error) *gorm.DB {
	t.Helper()

	db := ratelimitPostgresGorm(t)
	if window.ID == uuid.Nil {
		window.ID = uuid.New()
	}
	if replaceErr := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		if err != nil {
			tx.AddError(err)
			return
		}
		tx.Statement.RowsAffected = 1
	}); replaceErr != nil {
		t.Fatalf("replace create callback: %v", replaceErr)
	}
	if replaceErr := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		if err != nil {
			tx.AddError(err)
			return
		}
		if dest, ok := tx.Statement.Dest.(*store.GatewayRateLimitWindow); ok {
			*dest = window
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(gorm.ErrRecordNotFound)
	}); replaceErr != nil {
		t.Fatalf("replace query callback: %v", replaceErr)
	}
	if replaceErr := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		if err != nil {
			tx.AddError(err)
			return
		}
		tx.Statement.RowsAffected = 1
	}); replaceErr != nil {
		t.Fatalf("replace update callback: %v", replaceErr)
	}
	return db
}

type ratelimitTransactionConnector struct{}

func (ratelimitTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return ratelimitTransactionConn{}, nil
}

func (ratelimitTransactionConnector) Driver() driver.Driver {
	return ratelimitTransactionDriver{}
}

type ratelimitTransactionDriver struct{}

func (ratelimitTransactionDriver) Open(string) (driver.Conn, error) {
	return ratelimitTransactionConn{}, nil
}

type ratelimitTransactionConn struct{}

func (ratelimitTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fake driver only supports transactions")
}

func (ratelimitTransactionConn) Close() error {
	return nil
}

func (ratelimitTransactionConn) Begin() (driver.Tx, error) {
	return ratelimitTransactionTx{}, nil
}

func (ratelimitTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return ratelimitTransactionTx{}, nil
}

type ratelimitTransactionTx struct{}

func (ratelimitTransactionTx) Commit() error {
	return nil
}

func (ratelimitTransactionTx) Rollback() error {
	return nil
}
