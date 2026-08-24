package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/schema"

	"xlyra/server/internal/store"
)

type fieldValueConversionModel struct {
	ID        string       `gorm:"column:id;primaryKey"`
	Payload   store.JSON   `gorm:"column:payload"`
	EventTime time.Time    `gorm:"column:event_time"`
	SeenAt    sql.NullTime `gorm:"column:seen_at"`
}

func TestImportChecksReadinessBeforeReadingBackupPayload(t *testing.T) {
	t.Parallel()

	service := Service{db: &store.Store{}, confFile: nil, masterKey: "master-key"}

	_, err := service.Import(context.Background(), "passphrase", []byte("encrypted"), ImportOptions{})
	assertBackupErrorContains(t, "Import readiness", err, "database is not available")
}

func TestModelFieldValueConvertsJSONTimeAndNumericRows(t *testing.T) {
	t.Parallel()

	parsed, err := schema.Parse(&fieldValueConversionModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse helper schema: %v", err)
	}

	payloadField := parsed.FieldsByDBName["payload"]
	if got := modelFieldValue(map[string]any{"enabled": true}, payloadField); got != `{"enabled":true}` {
		t.Fatalf("JSON model field value = %#v, want encoded object", got)
	}
	if got := modelFieldValue(func() {}, payloadField); got != "null" {
		t.Fatalf("unsupported JSON model field value = %#v, want null", got)
	}

	when := time.Date(2026, 6, 23, 12, 34, 56, 789, time.UTC)
	eventField := parsed.FieldsByDBName["event_time"]
	if got := modelFieldValue(when.Format(time.RFC3339Nano), eventField); !reflect.DeepEqual(got, when) {
		t.Fatalf("time model field value = %#v, want %s", got, when.Format(time.RFC3339Nano))
	}
	if got := modelFieldValue("not-a-time", eventField); got != "not-a-time" {
		t.Fatalf("invalid time should fall back to row value, got %#v", got)
	}

	seenField := parsed.FieldsByDBName["seen_at"]
	if got := modelFieldValue(when, seenField); !reflect.DeepEqual(got, when) {
		t.Fatalf("sql.NullTime model field value = %#v, want time value", got)
	}

	idField := parsed.FieldsByDBName["id"]
	if got := modelFieldValue(json.Number("123"), idField); got != int64(123) {
		t.Fatalf("default model field value = %#v, want int64 conversion", got)
	}
}

func TestBackupTableLookupReturnsRegisteredTablesAndEmptyMissingTables(t *testing.T) {
	t.Parallel()

	table, ok := backupTableByName("sites")
	if !ok || table.Name != "sites" {
		t.Fatalf("expected sites backup table, got %#v ok=%v", table, ok)
	}

	table, ok = backupTableByName("not_registered")
	if ok || table.Name != "" || table.Model != nil || table.NewSlice != nil {
		t.Fatalf("expected empty missing backup table, got %#v ok=%v", table, ok)
	}
}
