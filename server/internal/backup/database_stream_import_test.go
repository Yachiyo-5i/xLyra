package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func writeStreamTestArchive(t *testing.T, rows int) string {
	t.Helper()

	archivePath := filepath.Join(t.TempDir(), "stream.zip")
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: path.Join("database", "request_logs.jsonl"), Method: zip.Deflate})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	for i := 0; i < rows; i++ {
		if _, err := fmt.Fprintf(w, "{\"id\":\"row-%d\",\"latency_ms\":%d}\n", i, i); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return archivePath
}

func writeStreamDatabaseTestArchive(t *testing.T, tables map[string][]map[string]any) string {
	t.Helper()

	archivePath := filepath.Join(t.TempDir(), "database-stream.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}
	zw := zip.NewWriter(file)
	for table, rows := range tables {
		writer, err := zw.CreateHeader(&zip.FileHeader{Name: path.Join("database", table+".jsonl"), Method: zip.Deflate})
		if err != nil {
			t.Fatalf("create %s entry: %v", table, err)
		}
		encoder := json.NewEncoder(writer)
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				t.Fatalf("encode %s row: %v", table, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return archivePath
}

func TestForEachArchiveTableChunkChunksAndDecodesRows(t *testing.T) {
	t.Parallel()

	archivePath := writeStreamTestArchive(t, 5)

	var chunkSizes []int
	seen := map[string]bool{}
	err := forEachArchiveTableChunk(archivePath, "request_logs", 2, func(chunk []map[string]any) error {
		chunkSizes = append(chunkSizes, len(chunk))
		for _, row := range chunk {
			seen[stringValue(row["id"])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("forEachArchiveTableChunk: %v", err)
	}

	// 5 rows at chunk size 2 => 2 + 2 + 1.
	if len(chunkSizes) != 3 || chunkSizes[0] != 2 || chunkSizes[1] != 2 || chunkSizes[2] != 1 {
		t.Fatalf("chunk sizes = %v, want [2 2 1]", chunkSizes)
	}
	if len(seen) != 5 {
		t.Fatalf("decoded %d distinct rows, want 5 (%v)", len(seen), seen)
	}
	for i := 0; i < 5; i++ {
		if !seen[fmt.Sprintf("row-%d", i)] {
			t.Fatalf("row-%d was not streamed", i)
		}
	}
}

func TestCountArchiveRows(t *testing.T) {
	t.Parallel()

	archivePath := writeStreamTestArchive(t, 5)
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zr.Close()
	file := archiveFile(zr.File, path.Join("database", "request_logs.jsonl"))
	rows, err := countArchiveRows(file)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rows != 5 {
		t.Fatalf("rows = %d, want 5", rows)
	}
}

func TestCountArchiveRowsStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	archivePath := writeStreamTestArchive(t, 5)
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer zr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = countArchiveRowsContext(ctx, archiveFile(zr.File, path.Join("database", "request_logs.jsonl")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("count rows error = %v, want context canceled", err)
	}
}

func TestImportStreamedTableReportsFilteredRows(t *testing.T) {
	t.Parallel()

	db := backupOfflineGorm(t)
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	table := backupTable{
		Name:     "request_logs",
		Model:    &databaseHelperModel{},
		NewSlice: func() any { return &[]databaseHelperModel{} },
	}
	dump := databaseDump{archivePath: writeStreamDatabaseTestArchive(t, map[string][]map[string]any{
		"request_logs": {
			{"id": "00000000-0000-0000-0000-000000000001"},
			{"id": "00000000-0000-0000-0000-000000000002"},
		},
	})}
	progressRows := 0
	progressSkipped := 0
	rows, skipped, err := importStreamedTable(context.Background(), db, dump, table, func(rows []map[string]any) []map[string]any {
		return rows[:1]
	}, func(imported int, filtered int) {
		progressRows = imported
		progressSkipped = filtered
	})
	if err != nil {
		t.Fatalf("importStreamedTable: %v", err)
	}
	if rows != 1 || skipped != 1 || progressRows != 1 || progressSkipped != 1 {
		t.Fatalf("rows/skipped/progress = %d/%d/%d/%d, want 1/1/1/1", rows, skipped, progressRows, progressSkipped)
	}
}

func TestImportDatabaseAdjustsProgressTotalForFilteredRows(t *testing.T) {
	t.Parallel()

	db := backupTransactionGorm(t, nil)
	if err := db.Callback().Raw().Replace("gorm:raw", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatalf("replace raw callback: %v", err)
	}
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}

	requestLogID := "00000000-0000-0000-0000-000000000001"
	dump := databaseDump{
		Tables:    make(map[string][]map[string]any, len(backupTables)),
		TotalRows: 3,
		archivePath: writeStreamDatabaseTestArchive(t, map[string][]map[string]any{
			"request_logs": {{"id": requestLogID}},
			"usage_records": {
				{"id": "00000000-0000-0000-0000-000000000002", "request_log_id": requestLogID},
				{"id": "00000000-0000-0000-0000-000000000003", "request_log_id": "00000000-0000-0000-0000-000000000004"},
			},
		}),
	}
	for _, table := range backupTables {
		dump.Tables[table.Name] = nil
	}
	var last ProgressEvent
	rows, totalRows, err := importDatabase(context.Background(), db, "master-key", dump, uuid.Nil, func(event ProgressEvent) {
		last = event
	})
	if err != nil {
		t.Fatalf("importDatabase: %v", err)
	}
	if rows != 2 || totalRows != 2 || last.Rows != 2 || last.TotalRows != 2 {
		t.Fatalf("rows/total/progress = %d/%d/%d/%d, want 2/2/2/2", rows, totalRows, last.Rows, last.TotalRows)
	}
}

func TestImportDatabaseRunsCommitGuardInsideTransaction(t *testing.T) {
	db, state := backupTransactionGormWithState(t, nil)
	originalDeleteOrder := importDeleteOrder
	importDeleteOrder = nil
	t.Cleanup(func() {
		importDeleteOrder = originalDeleteOrder
	})
	called := false
	dump := databaseDump{
		Tables: make(map[string][]map[string]any, len(backupTables)),
		beforeCommit: func() error {
			called = true
			return context.Canceled
		},
	}
	for _, table := range backupTables {
		dump.Tables[table.Name] = nil
	}

	_, _, err := importDatabase(context.Background(), db, "master-key", dump, uuid.Nil)
	if !called || !errors.Is(err, context.Canceled) {
		t.Fatalf("commit guard called=%v error=%v", called, err)
	}
	commits, rollbacks := state.counts()
	if commits != 0 || rollbacks != 1 {
		t.Fatalf("transaction commits=%d rollbacks=%d, want 0/1", commits, rollbacks)
	}
}

func TestForEachArchiveTableChunkMissingEntryYieldsNoRows(t *testing.T) {
	t.Parallel()

	archivePath := writeStreamTestArchive(t, 1)
	calls := 0
	err := forEachArchiveTableChunk(archivePath, "usage_records", 2, func([]map[string]any) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected missing table entry to be tolerated, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected no chunks for missing table entry, got %d", calls)
	}
}
