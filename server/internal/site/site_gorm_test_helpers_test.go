package site

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

const siteTestMasterKey = "test-master-key"

func siteServiceWithoutStore() *Service {
	return NewService(nil, siteTestMasterKey)
}

func siteServiceWithUninitializedStore() *Service {
	return NewService(&store.Store{}, siteTestMasterKey)
}

func siteJSONMeta(t *testing.T, value any) store.JSON {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal site test JSON meta: %v", err)
	}
	return store.JSON(encoded)
}

func siteMustJSONMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("decode site test JSON object: %v", err)
	}
	return root
}

func assertSiteWrappedQueryError(t *testing.T, label string, err error, queryErr error, wantContext string) {
	t.Helper()

	if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), wantContext) {
		t.Fatalf("%s error = %v, want wrapped %q query error", label, err, wantContext)
	}
}

func assertSiteQueryError(t *testing.T, label string, err error, queryErr error) {
	t.Helper()

	if !errors.Is(err, queryErr) {
		t.Fatalf("%s error = %v, want query error", label, err)
	}
}

func assertSiteErrorContains(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want to contain %q", label, err, want)
	}
}

type siteGormCallbacks struct {
	query          func(*gorm.DB)
	create         func(*gorm.DB)
	update         func(*gorm.DB)
	deleteCallback func(*gorm.DB)
}

func siteServiceWithCallbacks(t *testing.T, callbacks siteGormCallbacks) *Service {
	t.Helper()

	return NewService(siteStoreWithGorm(t, siteGormWithCallbacks(t, callbacks)), siteTestMasterKey)
}

func siteServiceWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *Service {
	t.Helper()

	return siteServiceWithCallbacks(t, siteGormCallbacks{query: callback})
}

func siteServiceWithQueryError(t *testing.T, err error) *Service {
	t.Helper()

	return siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(err)
	})
}

func siteGormWithCallbacks(t *testing.T, callbacks siteGormCallbacks) *gorm.DB {
	t.Helper()

	db := sitePostgresGorm(t)
	if callbacks.query != nil {
		siteReplaceQueryCallback(t, db, callbacks.query)
	}
	if callbacks.create != nil {
		siteReplaceCreateCallback(t, db, callbacks.create)
	}
	if callbacks.update != nil {
		siteReplaceUpdateCallback(t, db, callbacks.update)
	}
	if callbacks.deleteCallback != nil {
		siteReplaceDeleteCallback(t, db, callbacks.deleteCallback)
	}
	return db
}

func siteGormWithQueryError(t *testing.T, err error) *gorm.DB {
	t.Helper()

	return siteGormWithCallbacks(t, siteGormCallbacks{query: func(tx *gorm.DB) {
		tx.AddError(err)
	}})
}

func siteReplaceQueryCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Query().Replace("gorm:query", callback); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
}

func siteReplaceCreateCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Create().Replace("gorm:create", callback); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
}

func siteReplaceUpdateCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Update().Replace("gorm:update", callback); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
}

func siteReplaceDeleteCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Delete().Replace("gorm:delete", callback); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
}

func siteCaptureCreate[T any](t *testing.T, label string, capture func(T)) func(*gorm.DB) {
	t.Helper()

	return func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*T)
		if !ok {
			tx.AddError(errors.New("unexpected " + label + " create destination"))
			return
		}
		if capture != nil {
			capture(*item)
		}
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	}
}

func siteCaptureUpdate[T any](t *testing.T, label string, capture func(T)) func(*gorm.DB) {
	t.Helper()

	return func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*T)
		if !ok {
			tx.AddError(errors.New("unexpected " + label + " update destination"))
			return
		}
		if capture != nil {
			capture(*item)
		}
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	}
}

func siteServiceReturningSite(t *testing.T, item store.Site) *Service {
	t.Helper()

	return siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = item
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
}

func siteServiceReturningSiteWithCredentialError(t *testing.T, item store.Site, credentialErr error) *Service {
	t.Helper()

	return siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.Site:
				*dest = item
				tx.RowsAffected = 1
				tx.Statement.RowsAffected = 1
			case *[]store.SiteCredential:
				tx.AddError(credentialErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
}

func siteServiceWithSiteAndCredentials(t *testing.T, item store.Site, credentials []store.SiteCredential) *Service {
	t.Helper()

	return siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = item
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		case *[]store.SiteCredential:
			*dest = append([]store.SiteCredential(nil), credentials...)
			tx.RowsAffected = int64(len(credentials))
			tx.Statement.RowsAffected = int64(len(credentials))
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	})
}

func siteServiceWithCredential(t *testing.T, credential store.SiteCredential) *Service {
	t.Helper()

	return siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.SiteCredential:
			*dest = credential
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	})
}

func siteServiceWithCredentialLookupOnly(t *testing.T, credential store.SiteCredential, followUpErr error) *Service {
	t.Helper()

	return siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch tx.Statement.Dest.(type) {
		case *store.SiteCredential:
			tx.Statement.ReflectValue.Set(reflect.ValueOf(credential))
		default:
			tx.AddError(followUpErr)
		}
	})
}

func sitePostgresGorm(t *testing.T) *gorm.DB {
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

func siteTransactionPostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(siteTransactionConnector{})
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

type siteTransactionConnector struct{}

func (siteTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return siteTransactionConn{}, nil
}

func (siteTransactionConnector) Driver() driver.Driver {
	return siteTransactionDriver{}
}

type siteTransactionDriver struct{}

func (siteTransactionDriver) Open(string) (driver.Conn, error) {
	return siteTransactionConn{}, nil
}

type siteTransactionConn struct{}

func (siteTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("site fake driver only supports transactions")
}

func (siteTransactionConn) Close() error {
	return nil
}

func (siteTransactionConn) Begin() (driver.Tx, error) {
	return siteTransactionTx{}, nil
}

func (siteTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return siteTransactionTx{}, nil
}

type siteTransactionTx struct{}

func (siteTransactionTx) Commit() error {
	return nil
}

func (siteTransactionTx) Rollback() error {
	return nil
}

func siteStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func siteEncryptedCredential(t *testing.T, credentialID uuid.UUID, siteID uuid.UUID, credentialType string, secret string, meta store.JSON) store.SiteCredential {
	t.Helper()

	service := siteServiceWithoutStore()
	encrypted, masked, err := service.credentials.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt test credential: %v", err)
	}
	if meta == nil {
		meta = store.JSON(`{}`)
	}
	return store.SiteCredential{
		ID:              credentialID,
		SiteID:          siteID,
		CredentialType:  credentialType,
		EncryptedSecret: encrypted,
		MaskedSecret:    masked,
		Meta:            meta,
	}
}
