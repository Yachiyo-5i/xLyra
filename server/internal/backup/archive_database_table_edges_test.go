package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type preparedRowImportModel struct {
	ID   uuid.UUID `gorm:"column:id;primaryKey"`
	Name string    `gorm:"column:name"`
}

func TestParseArchiveReportsOpenError(t *testing.T) {
	t.Parallel()

	_, _, err := parseArchive(filepath.Join(t.TempDir(), "missing.zip"))
	if err == nil || !strings.Contains(err.Error(), "open backup archive") {
		t.Fatalf("parseArchive error = %v, want open archive context", err)
	}
}

func TestWriteArchiveRejectsUnencodableConfig(t *testing.T) {
	t.Parallel()

	calledDatabaseWriter := false
	payload := archivePayload{
		Manifest: manifest{
			FormatVersion: currentFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			Tables:        append([]string(nil), exportTables...),
		},
		Config: map[string]any{"bad": func() {}},
	}

	err := writeArchive(payload, func(*zip.Writer) error {
		calledDatabaseWriter = true
		return nil
	}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "encode config.json") {
		t.Fatalf("writeArchive error = %v, want config encode context", err)
	}
	if calledDatabaseWriter {
		t.Fatal("database writer should not run after config encoding fails")
	}
}

func TestRowsWithExistingParentKeepsEmptyInput(t *testing.T) {
	t.Parallel()

	if got := rowsWithExistingParent(nil, "parent_id", map[string]struct{}{"parent": {}}); got != nil {
		t.Fatalf("rowsWithExistingParent nil input = %#v, want nil", got)
	}

	rows := []map[string]any{}
	got := rowsWithExistingParent(rows, "parent_id", map[string]struct{}{"parent": {}})
	if len(got) != 0 {
		t.Fatalf("rowsWithExistingParent empty input = %#v, want empty", got)
	}
}

func TestImportTableSkipsRowsThatPrepareAsZeroValues(t *testing.T) {
	t.Parallel()

	table := preparedRowImportTable()
	db := schemaOnlyGormDB()

	err := importTable(context.Background(), db, table, []map[string]any{{"unknown_column": "ignored"}})
	if err != nil {
		t.Fatalf("importTable zero prepared rows: %v", err)
	}
}

func TestImportTableWrapsPrepareRowErrors(t *testing.T) {
	t.Parallel()

	table := preparedRowImportTable()
	db := schemaOnlyGormDB()

	err := importTable(context.Background(), db, table, []map[string]any{{"id": map[string]any{"bad": "uuid"}}})
	if err == nil || !strings.Contains(err.Error(), "prepare prepared_row_import row") || !strings.Contains(err.Error(), "set id") {
		t.Fatalf("importTable error = %v, want prepare row context", err)
	}
}

func TestPrimaryOrderClauseReportsSchemaError(t *testing.T) {
	t.Parallel()

	if _, err := primaryOrderClause(schemaOnlyGormDB(), 42); err == nil {
		t.Fatal("primaryOrderClause error = nil, want schema parse error")
	}
}

func preparedRowImportTable() backupTable {
	return backupTable{
		Name:     "prepared_row_import",
		Model:    &preparedRowImportModel{},
		NewSlice: func() any { return &[]preparedRowImportModel{} },
	}
}

func schemaOnlyGormDB() *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
}
