package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"xlyra/server/internal/store"
)

func TestImportDatabaseRejectsInvalidDumpBeforeTransaction(t *testing.T) {
	t.Parallel()

	_, _, err := importDatabase(context.Background(), nil, "master-key", databaseDump{}, uuid.Nil)
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

func TestImportStreamedTableReportsInsertedRows(t *testing.T) {
	t.Parallel()

	db := backupOfflineGorm(t)
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	table := backupTable{
		Name:     "progress_rows",
		Model:    &databaseHelperModel{},
		NewSlice: func() any { return &[]databaseHelperModel{} },
	}
	rowCount := importBatchSize*2 + 1
	rowsToImport := make([]map[string]any, rowCount)
	for index := range rowsToImport {
		rowsToImport[index] = map[string]any{"id": uuid.New().String()}
	}
	dump := databaseDump{Tables: map[string][]map[string]any{
		table.Name: rowsToImport,
	}}
	progress := make([]int, 0, 3)

	rows, skipped, err := importStreamedTable(context.Background(), db, dump, table, nil, func(imported int, _ int) {
		progress = append(progress, imported)
	})
	if err != nil {
		t.Fatalf("importStreamedTable: %v", err)
	}
	wantProgress := []int{importBatchSize, importBatchSize * 2, rowCount}
	if rows != rowCount || skipped != 0 || !reflect.DeepEqual(progress, wantProgress) {
		t.Fatalf("rows/skipped/progress = %d/%d/%v, want %d/0/%v", rows, skipped, progress, rowCount, wantProgress)
	}
}

func TestImportBatchSizeRespectsPostgresParameterLimit(t *testing.T) {
	t.Parallel()

	pricingSchema, err := schema.Parse(&store.SiteModelPricing{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse pricing schema: %v", err)
	}
	pricingBatchSize := importBatchSizeForSchema(pricingSchema)
	if parameters := pricingBatchSize * len(pricingSchema.DBNames); parameters > importParameterBudget {
		t.Fatalf("pricing batch uses %d parameters, budget %d", parameters, importParameterBudget)
	}
	if pricingBatchSize >= importBatchSize {
		t.Fatalf("pricing batch size = %d, want below %d", pricingBatchSize, importBatchSize)
	}

	requestLogSchema, err := schema.Parse(&store.RequestLog{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse request log schema: %v", err)
	}
	if got := importBatchSizeForSchema(requestLogSchema); got != importBatchSize {
		t.Fatalf("request log batch size = %d, want %d", got, importBatchSize)
	}
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
	_, err := exportDatabaseSnapshot(context.Background(), db, "master-key", zw)
	if !errors.Is(err, queryErr) {
		t.Fatalf("exportDatabaseSnapshot error = %v, want wrapped query error", err)
	}
	assertBackupErrorContains(t, "exportDatabaseSnapshot", err, "export")
	_ = zw.Close()
}
