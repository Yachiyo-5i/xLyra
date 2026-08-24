package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestBackupTimeZoneFallsBackToResolvedZone(t *testing.T) {
	t.Setenv("TZ", "UTC")
	t.Setenv("TimeZone", "")

	tz := backupTimeZone(config.TimeZone{})
	got := tz.Format(time.Date(2026, 6, 23, 1, 2, 3, 0, time.UTC), time.RFC3339)
	if got != "2026-06-23T01:02:03Z" {
		t.Fatalf("backupTimeZone fallback formatted time = %q, want UTC", got)
	}
}

func TestRunScheduledReleasesGuardAfterPassphraseDecryptFailure(t *testing.T) {
	t.Parallel()

	cfg := automaticConfigWithUndecryptableBackupPassphrase(t)

	service := NewAutomaticService(Service{
		confFile: automaticConfigFile(t, cfg),
		now:      func() time.Time { return time.Date(2026, 6, 23, 1, 2, 3, 0, time.UTC) },
	}, "master-key")

	result, err := service.RunScheduled(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decrypt backup passphrase") {
		t.Fatalf("RunScheduled error = %v, result=%#v; want passphrase decrypt error", err, result)
	}
	if service.running.Load() {
		t.Fatal("RunScheduled should release running guard after passphrase decrypt failure")
	}
}

func TestRunNowReleasesGuardAfterPassphraseDecryptFailure(t *testing.T) {
	t.Parallel()

	cfg := automaticConfigWithUndecryptableBackupPassphrase(t)
	service := NewAutomaticService(Service{confFile: automaticConfigFile(t, cfg)}, "master-key")
	task, err := service.RunNow()
	if err == nil || !strings.Contains(err.Error(), "decrypt backup passphrase") {
		t.Fatalf("RunNow error = %v, task=%#v; want passphrase decrypt error", err, task)
	}
	if service.running.Load() {
		t.Fatal("RunNow should release running guard after passphrase decrypt failure")
	}
}

func automaticConfigWithUndecryptableBackupPassphrase(t *testing.T) config.AutomaticBackupConfig {
	t.Helper()

	cfg := backupReadyAutomaticConfigFromInput(t)
	cfg.Storage.BackupPassphraseEncrypted = "not encrypted"
	return cfg
}

func TestWriteArchiveJSONFileReportsEncodeError(t *testing.T) {
	t.Parallel()

	zw := zip.NewWriter(&bytes.Buffer{})
	err := writeArchiveJSONFile(zw, "config.json", map[string]any{"bad": func() {}})
	if err == nil || !strings.Contains(err.Error(), "encode config.json") {
		t.Fatalf("writeArchiveJSONFile error = %v, want encode context", err)
	}
}

func TestDecodeArchiveTableAcceptsEmptyFile(t *testing.T) {
	t.Parallel()

	rows, err := decodeArchiveTable(archivePayloadZipFiles(t, map[string]string{
		"database/sites.jsonl": "",
	}), "sites", true)
	if err != nil {
		t.Fatalf("decodeArchiveTable empty file: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("decodeArchiveTable empty file rows = %#v, want none", rows)
	}
}
