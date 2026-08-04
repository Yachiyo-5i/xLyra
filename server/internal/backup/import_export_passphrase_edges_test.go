package backup

import (
	"context"
	"testing"
)

func TestImportRejectsDamagedEncryptedPayloadBeforeDatabaseImport(t *testing.T) {
	t.Parallel()

	service := backupImportExportService(t)

	summary, err := service.Import(context.Background(), "passphrase", []byte("not an encrypted backup"))
	assertBackupErrorContains(t, "Import damaged payload", err, "unsupported backup format")
	if summary != (ImportSummary{}) {
		t.Fatalf("Import damaged payload summary = %#v, want zero", summary)
	}
}

func TestExportPassphraseValidationLeavesNoTempPath(t *testing.T) {
	t.Parallel()

	service := backupImportExportService(t)

	path, filename, err := service.Export(context.Background(), "\t ")
	assertBackupErrorContains(t, "Export blank passphrase", err, "backup passphrase is required")
	if path != "" || filename != "" {
		t.Fatalf("Export blank passphrase path=%q filename=%q, want both empty", path, filename)
	}
}
