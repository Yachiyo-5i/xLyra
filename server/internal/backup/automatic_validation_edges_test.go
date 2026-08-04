package backup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xlyra/server/internal/config"
)

func TestServiceExportImportRequireReadyDatabase(t *testing.T) {
	t.Parallel()

	service := NewService(nil, nil, "master-key")
	if path, filename, err := service.Export(context.Background(), "secret"); path != "" || filename != "" {
		t.Fatalf("Export error = %v, path=%q filename=%q; want ready database validation", err, path, filename)
	} else {
		assertBackupErrorContains(t, "Export", err, "database is not available")
	}
	if summary, err := service.Import(context.Background(), "secret", []byte("encrypted")); err == nil {
		t.Fatalf("Import error = %v, summary=%#v; want ready database validation", err, summary)
	} else {
		assertBackupErrorContains(t, "Import", err, "database is not available")
	}
}

func TestAutomaticRunRejectsConcurrentExecution(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{}, "master-key")
	service.running.Store(true)

	if _, err := service.RunNow(); !errors.Is(err, ErrAutomaticAlreadyRunning) {
		t.Fatalf("RunNow error = %v, want already running", err)
	}
	if _, err := service.RunScheduled(context.Background()); !errors.Is(err, ErrAutomaticAlreadyRunning) {
		t.Fatalf("RunScheduled error = %v, want already running", err)
	}
}

func TestReadyClientValidatesScheduleAndSecrets(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		cfg  config.AutomaticBackupConfig
		want string
	}{
		{
			name: "rejects invalid cron expression",
			cfg: config.AutomaticBackupConfig{
				Cron:           "bad cron",
				RetentionCount: 7,
			},
			want: "cron",
		},
		{
			name: "rejects missing storage endpoint",
			cfg: config.AutomaticBackupConfig{
				Cron:           "0 3 * * *",
				RetentionCount: 7,
			},
			want: "storage.endpoint",
		},
		{
			name: "rejects missing storage secret key",
			cfg: config.AutomaticBackupConfig{
				Cron:           "0 3 * * *",
				RetentionCount: 7,
				Storage: config.AutomaticBackupStorageConfig{
					Endpoint:                  "s3.example.com",
					Bucket:                    "xlyra",
					AccessKey:                 "access",
					BackupPassphraseEncrypted: "encrypted-passphrase",
				},
			},
			want: "storage.secret_key",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service := NewAutomaticService(Service{confFile: automaticConfigFile(t, tc.cfg)}, "master-key")
			if _, _, err := service.readyClient(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readyClient error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAutomaticClientConfigValidatesEndpointAndTLS(t *testing.T) {
	t.Parallel()

	if _, _, err := normalizeS3Endpoint("https:///missing-host", true); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("normalizeS3Endpoint error = %v, want host validation", err)
	}

	cfg := backupReadyAutomaticConfigFromInput(t)
	cfg.Storage.Endpoint = "ftp://s3.example.com"
	service := NewAutomaticService(Service{confFile: automaticConfigFile(t, cfg)}, "master-key")
	if _, _, err := service.readyClient(); err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("readyClient error = %v, want endpoint scheme validation", err)
	}

	cfg.Storage.Endpoint = "https://s3.example.com"
	cfg.Storage.SkipTLSVerify = true
	clientBuilder := NewAutomaticService(Service{}, "master-key")
	client, err := clientBuilder.clientFromConfig(cfg)
	if err != nil {
		t.Fatalf("clientFromConfig with SkipTLSVerify: %v", err)
	}
	if client == nil {
		t.Fatal("expected S3 client")
	}
}

func TestRestoreAndDeleteValidateObjectKeysBeforeRemoteCall(t *testing.T) {
	t.Parallel()

	cfg := backupReadyAutomaticConfigFromInput(t)
	cfg.Storage.Endpoint = "https://s3.example.com"
	cfg.Storage.UseSSL = true
	service := NewAutomaticService(Service{confFile: automaticConfigFile(t, cfg)}, "master-key")

	if _, err := service.Restore(context.Background(), "other/xlyra-backup-20260621-030000.zip.xlyra"); err == nil || !strings.Contains(err.Error(), "outside configured prefix") {
		t.Fatalf("Restore error = %v, want prefix validation", err)
	}
	if err := service.Delete(context.Background(), "prod/notes.txt"); err == nil || !strings.Contains(err.Error(), "format") {
		t.Fatalf("Delete error = %v, want object format validation", err)
	}
}
