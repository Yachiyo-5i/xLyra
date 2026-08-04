package backup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestNormalizeS3Endpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		endpoint       string
		fallbackSecure bool
		wantEndpoint   string
		wantSecure     bool
		wantErr        bool
	}{
		{
			name:           "https URL",
			endpoint:       " https://s3.example.com/ ",
			wantEndpoint:   "s3.example.com",
			wantSecure:     true,
			fallbackSecure: false,
		},
		{
			name:           "http URL overrides fallback",
			endpoint:       "http://localhost:9000",
			wantEndpoint:   "localhost:9000",
			wantSecure:     false,
			fallbackSecure: true,
		},
		{
			name:           "host without scheme keeps fallback",
			endpoint:       "minio.internal/",
			wantEndpoint:   "minio.internal",
			wantSecure:     true,
			fallbackSecure: true,
		},
		{
			name:    "empty endpoint",
			wantErr: true,
		},
		{
			name:     "unsupported scheme",
			endpoint: "ftp://s3.example.com",
			wantErr:  true,
		},
		{
			name:     "path is rejected",
			endpoint: "https://s3.example.com/backups",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotEndpoint, gotSecure, err := normalizeS3Endpoint(tt.endpoint, tt.fallbackSecure)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize endpoint: %v", err)
			}
			if gotEndpoint != tt.wantEndpoint || gotSecure != tt.wantSecure {
				t.Fatalf("got endpoint=%q secure=%v, want endpoint=%q secure=%v", gotEndpoint, gotSecure, tt.wantEndpoint, tt.wantSecure)
			}
		})
	}
}

func TestAutomaticConfigPayloadReportsSecretsAndReadiness(t *testing.T) {
	t.Parallel()

	cfg := config.AutomaticBackupConfig{
		Enabled:        true,
		Cron:           "",
		RetentionCount: 0,
		Storage: config.AutomaticBackupStorageConfig{
			Endpoint:                  " s3.example.com ",
			Region:                    " us-east-1 ",
			Bucket:                    " xlyra ",
			Prefix:                    "prod",
			AccessKey:                 " access ",
			SecretKeyEncrypted:        " encrypted-secret ",
			SecretKeyMasked:           " sk***",
			BackupPassphraseEncrypted: " encrypted-passphrase ",
			BackupPassphraseMasked:    " pa***",
			ForcePathStyle:            true,
			UseSSL:                    true,
			SkipTLSVerify:             true,
		},
	}

	payload := automaticConfigPayload(cfg)

	if !payload.Enabled || !payload.Ready {
		t.Fatalf("expected enabled ready payload, got %#v", payload)
	}
	if payload.Cron != config.DefaultAutomaticBackupConfig().Cron || payload.RetentionCount != config.DefaultAutomaticBackupConfig().RetentionCount {
		t.Fatalf("expected default cron/retention, got %#v", payload)
	}
	if payload.Endpoint != "s3.example.com" || payload.Bucket != "xlyra" || payload.Prefix != "prod/" || payload.AccessKey != "access" {
		t.Fatalf("unexpected normalized storage payload: %#v", payload)
	}
	if !payload.HasSecretKey || payload.SecretKeyMasked != "sk***" {
		t.Fatalf("expected secret key metadata, got %#v", payload)
	}
	if !payload.HasBackupPassphrase || payload.BackupPassphraseMasked != "pa***" {
		t.Fatalf("expected passphrase metadata, got %#v", payload)
	}
	if !payload.ForcePathStyle || !payload.UseSSL || !payload.SkipTLSVerify {
		t.Fatalf("expected storage flags to be preserved, got %#v", payload)
	}
}

func TestConfigFromInputEncryptsNewSecretsAndKeepsExistingWhenBlank(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{}, "master-key")
	current := config.AutomaticBackupConfig{
		Storage: config.AutomaticBackupStorageConfig{
			SecretKeyEncrypted:        "existing-secret",
			SecretKeyMasked:           "ex***",
			BackupPassphraseEncrypted: "existing-passphrase",
			BackupPassphraseMasked:    "pa***",
		},
	}

	kept, err := service.configFromInput(AutomaticConfigInput{
		Enabled:          true,
		Cron:             " 0 4 * * * ",
		RetentionCount:   3,
		Endpoint:         " s3.example.com ",
		Region:           " us-east-1 ",
		Bucket:           " xlyra ",
		Prefix:           " backups ",
		AccessKey:        " access ",
		ForcePathStyle:   true,
		UseSSL:           true,
		SkipTLSVerify:    true,
		SecretKey:        "   ",
		BackupPassphrase: "",
	}, current)
	if err != nil {
		t.Fatalf("config from blank-secret input: %v", err)
	}
	if kept.Storage.SecretKeyEncrypted != "existing-secret" || kept.Storage.SecretKeyMasked != "ex***" {
		t.Fatalf("expected existing storage secret metadata to remain, got %#v", kept.Storage)
	}
	if kept.Storage.BackupPassphraseEncrypted != "existing-passphrase" || kept.Storage.BackupPassphraseMasked != "pa***" {
		t.Fatalf("expected existing passphrase metadata to remain, got %#v", kept.Storage)
	}
	if kept.Cron != "0 4 * * *" || kept.Storage.Endpoint != "s3.example.com" || kept.Storage.Prefix != "backups/" {
		t.Fatalf("expected normalized config, got %#v", kept)
	}

	updated, err := service.configFromInput(AutomaticConfigInput{
		Cron:             "0 5 * * *",
		RetentionCount:   5,
		Endpoint:         "s3.example.com",
		Bucket:           "xlyra",
		AccessKey:        "access",
		SecretKey:        "new-secret",
		BackupPassphrase: "new-passphrase",
	}, current)
	if err != nil {
		t.Fatalf("config from new-secret input: %v", err)
	}
	if updated.Storage.SecretKeyEncrypted == "" || updated.Storage.SecretKeyEncrypted == current.Storage.SecretKeyEncrypted {
		t.Fatalf("expected new encrypted storage secret, got %#v", updated.Storage)
	}
	if updated.Storage.BackupPassphraseEncrypted == "" || updated.Storage.BackupPassphraseEncrypted == current.Storage.BackupPassphraseEncrypted {
		t.Fatalf("expected new encrypted backup passphrase, got %#v", updated.Storage)
	}
	if !strings.Contains(updated.Storage.SecretKeyMasked, "cret") {
		t.Fatalf("expected masked storage secret to reflect new secret, got %q", updated.Storage.SecretKeyMasked)
	}
	if !strings.Contains(updated.Storage.BackupPassphraseMasked, "rase") {
		t.Fatalf("expected masked backup passphrase to reflect new passphrase, got %q", updated.Storage.BackupPassphraseMasked)
	}
}

func TestServiceReadyAndBackupFilenameUseConfiguredTimezone(t *testing.T) {
	t.Parallel()

	if err := (Service{}).ready(); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("expected unavailable database error, got %v", err)
	}
	if err := (Service{confFile: &config.ConfigFile{}, masterKey: "master"}).ready(); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("expected database to be checked first, got %v", err)
	}

	loc := time.FixedZone("UTC+2", 2*60*60)
	service := NewService(nil, nil, "master", config.TimeZone{Name: "UTC+2", Location: loc})
	createdAt := time.Date(2026, 6, 21, 23, 30, 5, 0, time.UTC)

	if got := service.backupFilename(createdAt); got != "xlyra-backup-20260622-013005.zip.xlyra" {
		t.Fatalf("unexpected backup filename: %s", got)
	}
}

func TestValidateManifestRejectsMismatches(t *testing.T) {
	t.Parallel()

	valid := manifest{
		FormatVersion: currentFormatVersion,
		App:           backupAppName,
		Payload:       backupPayload,
		Tables:        append([]string(nil), exportTables...),
	}
	if err := validateManifest(valid); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*manifest)
	}{
		{name: "app", mutate: func(m *manifest) { m.App = "other" }},
		{name: "version", mutate: func(m *manifest) { m.FormatVersion++ }},
		{name: "payload", mutate: func(m *manifest) { m.Payload = "other" }},
		{name: "table count", mutate: func(m *manifest) { m.Tables = m.Tables[:len(m.Tables)-1] }},
		{name: "table order", mutate: func(m *manifest) { m.Tables[0], m.Tables[1] = m.Tables[1], m.Tables[0] }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			value := valid
			value.Tables = append([]string(nil), valid.Tables...)
			tt.mutate(&value)
			if err := validateManifest(value); err == nil {
				t.Fatal("expected mismatch to be rejected")
			}
		})
	}
}

func TestIsAlreadyRunningError(t *testing.T) {
	t.Parallel()

	if !IsAlreadyRunningError(ErrAutomaticAlreadyRunning) {
		t.Fatal("expected sentinel error to match")
	}
	if !IsAlreadyRunningError(errors.Join(errors.New("wrap"), ErrAutomaticAlreadyRunning)) {
		t.Fatal("expected joined sentinel error to match")
	}
	if IsAlreadyRunningError(nil) || IsAlreadyRunningError(errors.New("other")) {
		t.Fatal("unexpected already-running match")
	}
}

func TestAutomaticServiceConfigLifecycleAndEarlyFailures(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	service := NewAutomaticService(Service{confFile: confFile}, "master-key")

	defaultPayload := service.GetConfig()
	if defaultPayload.Cron != config.DefaultAutomaticBackupConfig().Cron || defaultPayload.Ready {
		t.Fatalf("unexpected default config payload: %#v", defaultPayload)
	}

	_, err = NewAutomaticService(Service{}, "master-key").UpdateConfig(AutomaticConfigInput{})
	if err == nil || !strings.Contains(err.Error(), "config persistence") {
		t.Fatalf("missing config UpdateConfig error = %v, want config persistence error", err)
	}
	_, err = service.UpdateConfig(AutomaticConfigInput{Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "storage.endpoint") {
		t.Fatalf("invalid enabled config error = %v, want endpoint validation", err)
	}

	updated, err := service.UpdateConfig(AutomaticConfigInput{
		Enabled:          true,
		Cron:             "0 4 * * *",
		RetentionCount:   3,
		Endpoint:         "https://s3.example.com",
		Bucket:           "xlyra",
		Prefix:           "prod",
		AccessKey:        "access",
		SecretKey:        "secret",
		BackupPassphrase: "passphrase",
		UseSSL:           true,
		ForcePathStyle:   true,
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if !updated.Enabled || !updated.Ready || !updated.HasSecretKey || !updated.HasBackupPassphrase {
		t.Fatalf("expected ready updated payload, got %#v", updated)
	}

	reloaded, ok := config.ReadAutomaticBackupConfig(confFile)
	if !ok {
		t.Fatal("expected automatic backup config to persist")
	}
	if reloaded.Storage.SecretKeyEncrypted == "" || reloaded.Storage.BackupPassphraseEncrypted == "" {
		t.Fatalf("expected encrypted secrets in persisted config, got %#v", reloaded.Storage)
	}
	if reloaded.Storage.SecretKeyMasked == "" || reloaded.Storage.BackupPassphraseMasked == "" {
		t.Fatalf("expected masked secrets in persisted config, got %#v", reloaded.Storage)
	}
}

func TestAutomaticServiceReadyClientAndRunEarlyFailures(t *testing.T) {
	t.Parallel()

	missingConfig := NewAutomaticService(Service{}, "master-key")
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "ready client",
			call: func() error {
				_, _, err := missingConfig.readyClient()
				return err
			},
		},
		{
			name: "list files",
			call: func() error {
				_, err := missingConfig.ListFiles(context.Background())
				return err
			},
		},
		{
			name: "run now",
			call: func() error {
				_, err := missingConfig.RunNow()
				return err
			},
		},
		{
			name: "run scheduled",
			call: func() error {
				_, err := missingConfig.RunScheduled(context.Background())
				return err
			},
		},
		{
			name: "restore",
			call: func() error {
				_, err := missingConfig.Restore(context.Background(), "xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra")
				return err
			},
		},
		{
			name: "delete",
			call: func() error {
				return missingConfig.Delete(context.Background(), "xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra")
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.call(); err == nil || !strings.Contains(err.Error(), "automatic backup config") {
				t.Fatalf("%s missing config error = %v", tc.name, err)
			}
		})
	}

	missingPassphraseConfig := backupReadyAutomaticConfigFromInput(t)
	missingPassphraseConfig.Storage.BackupPassphraseEncrypted = ""
	missingPassphraseConfig.Storage.BackupPassphraseMasked = ""
	missingPassphrase := NewAutomaticService(Service{confFile: automaticConfigFile(t, missingPassphraseConfig)}, "master-key")
	if _, _, err := missingPassphrase.readyClient(); err == nil || !strings.Contains(err.Error(), "storage.backup_passphrase") {
		t.Fatalf("readyClient missing passphrase error = %v", err)
	}

	service := NewAutomaticService(Service{}, "master-key")
	cfg := backupReadyAutomaticConfigFromInput(t)
	secret, err := service.decryptStorageSecret(cfg)
	if err != nil {
		t.Fatalf("decrypt storage secret: %v", err)
	}
	if secret != "secret" {
		t.Fatalf("storage secret = %q, want secret", secret)
	}
	passphrase, err := service.decryptBackupPassphrase(cfg)
	if err != nil {
		t.Fatalf("decrypt backup passphrase: %v", err)
	}
	if passphrase != "passphrase" {
		t.Fatalf("backup passphrase = %q, want passphrase", passphrase)
	}
	client, err := service.clientFromConfig(cfg)
	if err != nil {
		t.Fatalf("client from config: %v", err)
	}
	if client == nil {
		t.Fatal("expected minio client")
	}

	if _, err := service.decryptStorageSecret(config.AutomaticBackupConfig{}); err == nil || !strings.Contains(err.Error(), "storage.secret_key") {
		t.Fatalf("empty storage secret error = %v", err)
	}
	if _, err := service.decryptBackupPassphrase(config.AutomaticBackupConfig{}); err == nil || !strings.Contains(err.Error(), "backup passphrase") {
		t.Fatalf("empty backup passphrase error = %v", err)
	}
}

func TestAutomaticServiceTestReportsConfigFailure(t *testing.T) {
	t.Parallel()

	result, err := NewAutomaticService(Service{}, "master-key").Test(context.Background())
	if err == nil || !strings.Contains(err.Error(), "automatic backup config") {
		t.Fatalf("Test missing config error = %v", err)
	}
	if result != (AutomaticTestResult{}) {
		t.Fatalf("expected zero result for missing config, got %#v", result)
	}

	confFile := automaticConfigFile(t, config.AutomaticBackupConfig{
		Enabled:        true,
		Cron:           "0 3 * * *",
		RetentionCount: 7,
		Storage: config.AutomaticBackupStorageConfig{
			Endpoint:                  "s3.example.com",
			Bucket:                    "xlyra",
			AccessKey:                 "access",
			SecretKeyEncrypted:        "",
			BackupPassphraseEncrypted: "encrypted-passphrase",
			UseSSL:                    true,
		},
	})
	result, err = NewAutomaticService(Service{confFile: confFile}, "master-key").Test(context.Background())
	if err != nil {
		t.Fatalf("Test should return config failure as result, got error %v", err)
	}
	if result.OK || result.Stage != "config" || !strings.Contains(result.Message, "storage.secret_key") {
		t.Fatalf("unexpected config failure result: %#v", result)
	}
}

func automaticConfigFile(t *testing.T, cfg config.AutomaticBackupConfig) *config.ConfigFile {
	t.Helper()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set(config.AutomaticBackupConfigPath, config.AutomaticBackupConfigToMap(cfg)); err != nil {
		t.Fatalf("set automatic backup config: %v", err)
	}
	return confFile
}
