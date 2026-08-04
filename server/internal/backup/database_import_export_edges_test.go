package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestImportDatabaseRejectsInvalidDumpBeforeTransaction(t *testing.T) {
	t.Parallel()

	_, err := importDatabase(context.Background(), nil, "master-key", databaseDump{})
	assertBackupErrorContains(t, "importDatabase invalid dump", err, "missing tables")
}

func TestImportTableHandlesEmptyRowsAndRejectsInvalidSlice(t *testing.T) {
	t.Parallel()

	db := &gorm.DB{Config: &gorm.Config{NamingStrategy: schema.NamingStrategy{}}}
	if err := importTable(context.Background(), db, backupTable{Name: "empty", Model: &databaseHelperModel{}}, nil); err != nil {
		t.Fatalf("importTable empty rows: %v", err)
	}

	table := backupTable{
		Name:     "invalid_slice",
		Model:    &databaseHelperModel{},
		NewSlice: func() any { return databaseHelperModel{} },
	}
	err := importTable(context.Background(), db, table, []map[string]any{{"id": uuid.New().String()}})
	assertBackupErrorContains(t, "importTable invalid slice", err, "must provide a slice pointer")
}

func TestExportDatabaseSnapshotWrapsTableQueryError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("backup export query stopped")
	db := backupOfflineGorm(t)
	backupReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := exportDatabaseSnapshot(context.Background(), db, "master-key", zw)
	if !errors.Is(err, queryErr) {
		t.Fatalf("exportDatabaseSnapshot error = %v, want wrapped query error", err)
	}
	assertBackupErrorContains(t, "exportDatabaseSnapshot", err, "export")
	_ = zw.Close()
}
