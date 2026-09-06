package oauth

import (
	"context"
	"database/sql"
	"reflect"
	"sync/atomic"
	"testing"
	"unsafe"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

type oauthGormCallbacks struct {
	query  func(*gorm.DB)
	create func(*gorm.DB)
	update func(*gorm.DB)
	delete func(*gorm.DB)
}

func oauthServiceWithCallbacks(t *testing.T, callbacks oauthGormCallbacks) *Service {
	t.Helper()

	return NewService(oauthStoreWithGorm(t, oauthGormWithCallbacks(t, callbacks)), "master-key")
}

func oauthGormWithCallbacks(t *testing.T, callbacks oauthGormCallbacks) *gorm.DB {
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
	pool := &oauthTestConnPool{ConnPool: db.Statement.ConnPool}
	db.Config.ConnPool = pool
	db.Statement.ConnPool = pool
	if callbacks.query != nil {
		if err := db.Callback().Query().Replace("gorm:query", callbacks.query); err != nil {
			t.Fatalf("replace query callback: %v", err)
		}
	}
	if callbacks.create != nil {
		if err := db.Callback().Create().Replace("gorm:create", callbacks.create); err != nil {
			t.Fatalf("replace create callback: %v", err)
		}
	}
	if callbacks.update != nil {
		if err := db.Callback().Update().Replace("gorm:update", callbacks.update); err != nil {
			t.Fatalf("replace update callback: %v", err)
		}
	}
	if callbacks.delete != nil {
		if err := db.Callback().Delete().Replace("gorm:delete", callbacks.delete); err != nil {
			t.Fatalf("replace delete callback: %v", err)
		}
	}
	return db
}

func oauthServiceWithQueryUpdate(t *testing.T, query func(*gorm.DB), update func(*gorm.DB)) *Service {
	t.Helper()

	return oauthServiceWithCallbacks(t, oauthGormCallbacks{query: query, update: update})
}

func oauthGormWithQueryUpdate(t *testing.T, query func(*gorm.DB), update func(*gorm.DB)) *gorm.DB {
	t.Helper()

	return oauthGormWithCallbacks(t, oauthGormCallbacks{query: query, update: update})
}

func oauthGormWithCreate(t *testing.T, create func(*gorm.DB)) *gorm.DB {
	t.Helper()

	return oauthGormWithCallbacks(t, oauthGormCallbacks{create: create})
}

func oauthStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

type oauthTestConnPool struct {
	gorm.ConnPool
	begins    atomic.Int32
	commits   atomic.Int32
	rollbacks atomic.Int32
}

func (p *oauthTestConnPool) BeginTx(context.Context, *sql.TxOptions) (gorm.ConnPool, error) {
	p.begins.Add(1)
	return &oauthTestTx{ConnPool: p.ConnPool, pool: p}, nil
}

type oauthTestTx struct {
	gorm.ConnPool
	pool *oauthTestConnPool
}

func (tx *oauthTestTx) Commit() error {
	tx.pool.commits.Add(1)
	return nil
}

func (tx *oauthTestTx) Rollback() error {
	tx.pool.rollbacks.Add(1)
	return nil
}
