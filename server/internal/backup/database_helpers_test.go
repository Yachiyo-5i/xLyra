package backup

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"xlyra/server/internal/store"
)

type databaseHelperModel struct {
	ID         uuid.UUID     `gorm:"column:id;primaryKey"`
	OptionalID uuid.NullUUID `gorm:"column:optional_id"`
	Payload    store.JSON    `gorm:"column:payload"`
	Raw        []byte        `gorm:"column:raw"`
	CreatedAt  time.Time     `gorm:"column:created_at"`
	Ignored    string        `gorm:"-"`
}

type compositePrimaryModel struct {
	AccountID string    `gorm:"column:account_id;primaryKey"`
	SiteID    uuid.UUID `gorm:"column:site_id;primaryKey"`
	Name      string    `gorm:"column:name"`
}

type noPrimaryModel struct {
	Name string `gorm:"column:name"`
}

type testDriverValuer struct {
	value driver.Value
	err   error
}

func (v testDriverValuer) Value() (driver.Value, error) {
	return v.value, v.err
}

func TestValidateDumpTablesRequiresRegisteredTables(t *testing.T) {
	t.Parallel()

	if err := validateDumpTables(databaseDump{}); err == nil || !strings.Contains(err.Error(), "missing tables") {
		t.Fatalf("expected nil table payload to be rejected, got %v", err)
	}

	dump := databaseDump{Tables: map[string][]map[string]any{}}
	for _, table := range backupTables[1:] {
		dump.Tables[table.Name] = nil
	}
	if err := validateDumpTables(dump); err != nil {
		t.Fatalf("expected missing registered table to be tolerated, got %v", err)
	}
	filled, ok := dump.Tables[backupTables[0].Name]
	if !ok || filled == nil || len(filled) != 0 {
		t.Fatalf("expected missing table to be filled with empty rows, got %#v", filled)
	}

	dump.Tables[backupTables[0].Name] = nil
	if err := validateDumpTables(dump); err != nil {
		t.Fatalf("expected complete table payload to pass: %v", err)
	}
}

func TestModelRowsConvertsSchemaFieldsToBackupValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	id := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	optionalID := uuid.MustParse("00000000-0000-0000-0000-000000000102")
	createdAt := time.Date(2026, 6, 22, 9, 30, 1, 123, time.UTC)
	parsed, err := schema.Parse(&databaseHelperModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse helper schema: %v", err)
	}

	items := []*databaseHelperModel{{
		ID:         id,
		OptionalID: uuid.NullUUID{UUID: optionalID, Valid: true},
		Payload:    store.JSON(`{"tier":"prod"}`),
		Raw:        []byte(`{"tokens":42}`),
		CreatedAt:  createdAt,
		Ignored:    "not exported",
	}}
	rows, err := modelRows(ctx, parsed, &items)
	if err != nil {
		t.Fatalf("model rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	row := rows[0]
	if row["id"] != id.String() || row["optional_id"] != optionalID.String() {
		t.Fatalf("unexpected UUID conversion: %#v", row)
	}
	payload, ok := row["payload"].(json.RawMessage)
	if !ok || string(payload) != `{"tier":"prod"}` {
		t.Fatalf("expected JSON payload to remain raw, got %#v", row["payload"])
	}
	raw, ok := row["raw"].(map[string]any)
	if !ok || raw["tokens"] != json.Number("42") {
		t.Fatalf("expected raw bytes to decode with json.Number, got %#v", row["raw"])
	}
	if row["created_at"] != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected created_at backup value: %#v", row["created_at"])
	}
	if _, ok := row[""]; ok {
		t.Fatalf("ignored field should not be exported: %#v", row)
	}
}

func TestModelRowsRejectsInvalidSliceInputs(t *testing.T) {
	t.Parallel()

	parsed, err := schema.Parse(&databaseHelperModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse helper schema: %v", err)
	}

	tests := []struct {
		name  string
		slice any
		want  string
	}{
		{name: "non pointer", slice: []databaseHelperModel{}, want: "non-nil pointer"},
		{name: "nil pointer", slice: (*[]databaseHelperModel)(nil), want: "non-nil pointer"},
		{name: "pointer to non slice", slice: new(databaseHelperModel), want: "point to a slice"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := modelRows(context.Background(), parsed, tt.slice); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestDecodeJSONValuePreservesNumbersAndFallsBackToString(t *testing.T) {
	t.Parallel()

	if got := decodeJSONValue(nil); got != nil {
		t.Fatalf("expected empty JSON bytes to decode as nil, got %#v", got)
	}

	object, ok := decodeJSONValue([]byte(`{"count":9007199254740993,"name":"daily"}`)).(map[string]any)
	if !ok {
		t.Fatalf("expected JSON object, got %#v", object)
	}
	if object["count"] != json.Number("9007199254740993") || object["name"] != "daily" {
		t.Fatalf("unexpected decoded object: %#v", object)
	}

	array, ok := decodeJSONValue([]byte(`[1,2]`)).([]any)
	if !ok || !reflect.DeepEqual(array, []any{json.Number("1"), json.Number("2")}) {
		t.Fatalf("unexpected decoded array: %#v", array)
	}

	if got := decodeJSONValue([]byte(`not-json`)); got != "not-json" {
		t.Fatalf("expected invalid JSON to fall back to string, got %#v", got)
	}
}

func TestPrimaryOrderClauseUsesPrimaryFieldDBNames(t *testing.T) {
	t.Parallel()

	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
	order, err := primaryOrderClause(db, &compositePrimaryModel{})
	if err != nil {
		t.Fatalf("primary order clause: %v", err)
	}
	if len(order.Columns) != 2 {
		t.Fatalf("expected two primary columns, got %#v", order.Columns)
	}
	if order.Columns[0].Column.Name != "account_id" || order.Columns[1].Column.Name != "site_id" {
		t.Fatalf("unexpected primary column order: %#v", order.Columns)
	}

	order, err = primaryOrderClause(db, &noPrimaryModel{})
	if err != nil {
		t.Fatalf("primary order clause without primary key: %v", err)
	}
	if len(order.Columns) != 0 {
		t.Fatalf("expected no ordering columns, got %#v", order.Columns)
	}
}

func TestCloneRowsCopiesRowMaps(t *testing.T) {
	t.Parallel()

	source := []map[string]any{{
		"id":   "row-1",
		"meta": map[string]any{"tier": "prod"},
	}}
	clone := cloneRows(source)

	if len(clone) != 1 || clone[0]["id"] != "row-1" {
		t.Fatalf("unexpected clone: %#v", clone)
	}
	clone[0]["id"] = "changed"
	if source[0]["id"] != "row-1" {
		t.Fatalf("top-level row map was aliased: %#v", source[0])
	}
	clone[0]["new"] = true
	if _, ok := source[0]["new"]; ok {
		t.Fatalf("new clone key leaked into source: %#v", source[0])
	}

	nested := clone[0]["meta"].(map[string]any)
	nested["tier"] = "dev"
	if source[0]["meta"].(map[string]any)["tier"] != "dev" {
		t.Fatalf("expected cloneRows to preserve shallow value references")
	}
	if got := cloneRows(nil); len(got) != 0 {
		t.Fatalf("expected nil input to produce an empty clone, got %#v", got)
	}
}

func TestBackupValueConvertsNullableAndScalarBranches(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-000000000201")
	when := time.Date(2026, 6, 22, 10, 1, 2, 3, time.UTC)

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "nil", value: nil, want: nil},
		{name: "empty bytes", value: []byte{}, want: nil},
		{name: "invalid JSON bytes", value: []byte("plain"), want: "plain"},
		{name: "uuid", value: id, want: id.String()},
		{name: "nil uuid", value: uuid.Nil, want: nil},
		{name: "valid null uuid", value: uuid.NullUUID{UUID: id, Valid: true}, want: id.String()},
		{name: "invalid null uuid", value: uuid.NullUUID{UUID: id, Valid: false}, want: nil},
		{name: "nil null uuid", value: uuid.NullUUID{UUID: uuid.Nil, Valid: true}, want: nil},
		{name: "time", value: when, want: when.Format(time.RFC3339Nano)},
		{name: "zero time", value: time.Time{}, want: nil},
		{name: "time pointer", value: &when, want: when.Format(time.RFC3339Nano)},
		{name: "nil time pointer", value: (*time.Time)(nil), want: nil},
		{name: "uuid pointer", value: &id, want: id.String()},
		{name: "nil uuid pointer", value: (*uuid.UUID)(nil), want: nil},
		{name: "valuer string", value: testDriverValuer{value: "from-valuer"}, want: "from-valuer"},
		{name: "valuer error", value: testDriverValuer{err: errors.New("boom")}, want: nil},
		{name: "default", value: true, want: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := backupValue(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("backupValue(%T) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}

	decoded, ok := backupValue(testDriverValuer{value: []byte(`{"ok":true}`)}).(map[string]any)
	if !ok || decoded["ok"] != true {
		t.Fatalf("expected valuer bytes to be decoded recursively, got %#v", decoded)
	}
}
