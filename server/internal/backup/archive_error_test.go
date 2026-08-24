package backup

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

func TestWriteArchiveReturnsDatabaseWriterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database export failed")
	payload := archivePayload{
		Manifest: manifest{
			FormatVersion: currentFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			CreatedAt:     time.Now().UTC(),
			Tables:        exportTables,
		},
		Config: map[string]any{"general": map[string]any{"log": map[string]any{"level": "info"}}},
	}

	err := writeArchive(payload, func(*zip.Writer) (map[string]int, error) {
		return nil, wantErr
	}, &bytes.Buffer{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("writeArchive error = %v, want %v", err, wantErr)
	}
}

func TestParseArchiveToleratesMissingDatabaseTables(t *testing.T) {
	t.Parallel()

	filename := writeArchiveFixture(t, func(zw *zip.Writer) {
		if err := writeArchiveJSONFile(zw, "manifest.json", manifest{
			FormatVersion: minImportFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			CreatedAt:     time.Now().UTC(),
			Tables:        exportTables[:len(exportTables)-4],
		}); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		if err := writeArchiveJSONFile(zw, "config.json", map[string]any{}); err != nil {
			t.Fatalf("write config: %v", err)
		}
	})

	_, dump, err := parseArchive(filename)
	if err != nil {
		t.Fatalf("parseArchive error = %v, want nil", err)
	}
	if dump.TotalRows != 0 {
		t.Fatalf("expected zero total rows, got %d", dump.TotalRows)
	}
	if err := validateDumpTables(dump); err != nil {
		t.Fatalf("expected dump with empty tables to validate: %v", err)
	}
	for _, table := range backupTables {
		if _, streamed := streamedImportTables[table.Name]; streamed {
			continue
		}
		if rows := dump.Tables[table.Name]; len(rows) != 0 {
			t.Fatalf("expected table %s to be empty, got %d rows", table.Name, len(rows))
		}
	}
}

func TestParseArchiveRejectsCurrentVersionArchiveMissingTables(t *testing.T) {
	t.Parallel()

	filename := writeArchiveFixture(t, func(zw *zip.Writer) {
		if err := writeArchiveJSONFile(zw, "manifest.json", manifest{
			FormatVersion: currentFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			CreatedAt:     time.Now().UTC(),
			Tables:        exportTables,
		}); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
		if err := writeArchiveJSONFile(zw, "config.json", map[string]any{}); err != nil {
			t.Fatalf("write config: %v", err)
		}
	})

	_, _, err := parseArchive(filename)
	assertBackupErrorContains(t, "parseArchive", err, "backup archive missing database/")
}

func TestDecodeArchiveJSONRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	var decoded manifest
	err := decodeArchiveJSON(archivePayloadZipFiles(t, map[string]string{"manifest.json": "{"}), "manifest.json", &decoded)
	assertBackupErrorContains(t, "decodeArchiveJSON", err, "decode manifest.json")
}

func TestDecodeArchiveTableRejectsMalformedJSONLRow(t *testing.T) {
	t.Parallel()

	_, err := decodeArchiveTable(archivePayloadZipFiles(t, map[string]string{"database/sites.jsonl": "{"}), "sites", true)
	assertBackupErrorContains(t, "decodeArchiveTable", err, "decode database/sites.jsonl")
}

func writeArchiveFixture(t *testing.T, write func(*zip.Writer)) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "backup-*.zip")
	if err != nil {
		t.Fatalf("create fixture archive: %v", err)
	}
	filename := file.Name()
	zw := zip.NewWriter(file)
	write(zw)
	if err := zw.Close(); err != nil {
		t.Fatalf("close fixture archive: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture file: %v", err)
	}
	return filename
}
