package backup

import (
	"archive/zip"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
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

func TestForEachArchiveTableChunkMissingEntryErrors(t *testing.T) {
	t.Parallel()

	archivePath := writeStreamTestArchive(t, 1)
	err := forEachArchiveTableChunk(archivePath, "usage_records", 2, func([]map[string]any) error { return nil })
	if err == nil {
		t.Fatal("expected error for missing table entry")
	}
}
