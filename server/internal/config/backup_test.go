package config

import (
	"strings"
	"testing"
)

func TestSanitizeConfigForBackupRemovesAutomaticBackupSecrets(t *testing.T) {
	input := map[string]any{
		"backup": map[string]any{
			"automatic": map[string]any{
				"enabled":         true,
				"cron":            "0 2 * * *",
				"retention_count": 5,
				"storage": map[string]any{
					"endpoint":                    "https://s3.example.com",
					"region":                      "us-east-1",
					"bucket":                      "xlyra",
					"prefix":                      "prod",
					"access_key":                  "ak",
					"secret_key_encrypted":        "encrypted-secret",
					"secret_key_masked":           "sk...cret",
					"backup_passphrase_encrypted": "encrypted-passphrase",
					"backup_passphrase_masked":    "pa...rase",
					"force_path_style":            true,
					"use_ssl":                     true,
				},
			},
		},
	}

	got := SanitizeConfigForBackup(input)
	cfg, ok := automaticBackupFromMap(got)
	if !ok {
		t.Fatal("expected automatic backup config to be preserved")
	}
	if cfg.Storage.SecretKeyEncrypted != "" || cfg.Storage.SecretKeyMasked != "" {
		t.Fatalf("storage secret leaked into backup config: %#v", cfg.Storage)
	}
	if cfg.Storage.BackupPassphraseEncrypted != "" || cfg.Storage.BackupPassphraseMasked != "" {
		t.Fatalf("backup passphrase leaked into backup config: %#v", cfg.Storage)
	}
	if cfg.Storage.Endpoint != "https://s3.example.com" || cfg.Storage.AccessKey != "ak" {
		t.Fatalf("non-secret storage config was not preserved: %#v", cfg.Storage)
	}
	if cfg.Storage.Prefix != "prod/" {
		t.Fatalf("prefix was not normalized: %q", cfg.Storage.Prefix)
	}
}

func TestMergeImportedConfigKeepsExistingAutomaticBackupConfig(t *testing.T) {
	current := map[string]any{
		"backup": map[string]any{
			"automatic": map[string]any{
				"enabled":         true,
				"cron":            "0 4 * * *",
				"retention_count": 9,
				"storage": map[string]any{
					"endpoint":                    "https://current.example.com",
					"bucket":                      "current",
					"prefix":                      "current/",
					"access_key":                  "current-ak",
					"secret_key_encrypted":        "current-secret",
					"backup_passphrase_encrypted": "current-passphrase",
					"use_ssl":                     true,
				},
			},
		},
	}
	imported := map[string]any{
		"backup": map[string]any{
			"automatic": map[string]any{
				"enabled":         false,
				"cron":            "0 1 * * *",
				"retention_count": 2,
				"storage": map[string]any{
					"endpoint": "https://imported.example.com",
					"bucket":   "imported",
					"prefix":   "imported/",
				},
			},
		},
	}

	got := MergeImportedConfig(current, imported)
	cfg, ok := automaticBackupFromMap(got)
	if !ok {
		t.Fatal("expected automatic backup config")
	}
	if cfg.Storage.Endpoint != "https://current.example.com" || cfg.Storage.Bucket != "current" {
		t.Fatalf("current automatic backup config was not preserved: %#v", cfg.Storage)
	}
	if cfg.Cron != "0 4 * * *" || cfg.RetentionCount != 9 {
		t.Fatalf("current schedule config was not preserved: %#v", cfg)
	}
}

func TestMergeImportedConfigRestoresSanitizedAutomaticBackupWhenCurrentMissing(t *testing.T) {
	imported := map[string]any{
		"backup": map[string]any{
			"automatic": map[string]any{
				"enabled":         true,
				"cron":            "0 1 * * *",
				"retention_count": 3,
				"storage": map[string]any{
					"endpoint":                    "https://imported.example.com",
					"bucket":                      "imported",
					"prefix":                      "imported/",
					"access_key":                  "ak",
					"secret_key_encrypted":        "imported-secret",
					"backup_passphrase_encrypted": "imported-passphrase",
					"use_ssl":                     true,
				},
			},
		},
	}

	got := MergeImportedConfig(nil, imported)
	cfg, ok := automaticBackupFromMap(got)
	if !ok {
		t.Fatal("expected automatic backup config")
	}
	if cfg.Storage.Endpoint != "https://imported.example.com" || cfg.Storage.AccessKey != "ak" {
		t.Fatalf("imported non-secret config was not restored: %#v", cfg.Storage)
	}
	if cfg.Storage.SecretKeyEncrypted != "" || cfg.Storage.BackupPassphraseEncrypted != "" {
		t.Fatalf("imported secrets should not be restored: %#v", cfg.Storage)
	}
}

func TestAutomaticBackupConfigReadyDoesNotRequireEnabled(t *testing.T) {
	cfg := DefaultAutomaticBackupConfig()
	cfg.Enabled = false
	cfg.Storage.Endpoint = "https://s3.example.com"
	cfg.Storage.Bucket = "xlyra"
	cfg.Storage.AccessKey = "ak"
	cfg.Storage.SecretKeyEncrypted = "encrypted-secret"
	cfg.Storage.BackupPassphraseEncrypted = "encrypted-passphrase"

	if !AutomaticBackupConfigReady(cfg) {
		t.Fatal("complete automatic backup config should be ready for manual actions even when cron is disabled")
	}
}

func TestValidateAutomaticBackupConfigChecksScheduleRetentionAndEnabledStorage(t *testing.T) {
	t.Parallel()

	valid := DefaultAutomaticBackupConfig()
	valid.Cron = "0 3 * * *"
	valid.RetentionCount = 7
	if err := ValidateAutomaticBackupConfig(valid); err != nil {
		t.Fatalf("disabled config with default storage should validate: %v", err)
	}

	cases := []struct {
		want   string
		mutate func(*AutomaticBackupConfig)
	}{
		{want: "cron", mutate: func(cfg *AutomaticBackupConfig) { cfg.Cron = "bad cron" }},
		{want: "retention_count", mutate: func(cfg *AutomaticBackupConfig) { cfg.RetentionCount = 0 }},
		{want: "retention_count", mutate: func(cfg *AutomaticBackupConfig) { cfg.RetentionCount = 366 }},
		{want: "storage.endpoint", mutate: func(cfg *AutomaticBackupConfig) {
			cfg.Enabled = true
			cfg.Storage.Endpoint = ""
		}},
		{want: "storage.bucket", mutate: func(cfg *AutomaticBackupConfig) {
			cfg.Enabled = true
			cfg.Storage.Endpoint = "s3.example.com"
			cfg.Storage.Bucket = ""
		}},
		{want: "storage.access_key", mutate: func(cfg *AutomaticBackupConfig) {
			cfg.Enabled = true
			cfg.Storage.Endpoint = "s3.example.com"
			cfg.Storage.Bucket = "xlyra"
			cfg.Storage.AccessKey = ""
		}},
		{want: "storage.secret_key", mutate: func(cfg *AutomaticBackupConfig) {
			cfg.Enabled = true
			cfg.Storage.Endpoint = "s3.example.com"
			cfg.Storage.Bucket = "xlyra"
			cfg.Storage.AccessKey = "ak"
			cfg.Storage.SecretKeyEncrypted = ""
		}},
		{want: "storage.backup_passphrase", mutate: func(cfg *AutomaticBackupConfig) {
			cfg.Enabled = true
			cfg.Storage.Endpoint = "s3.example.com"
			cfg.Storage.Bucket = "xlyra"
			cfg.Storage.AccessKey = "ak"
			cfg.Storage.SecretKeyEncrypted = "secret"
			cfg.Storage.BackupPassphraseEncrypted = ""
		}},
	}

	for _, tt := range cases {
		cfg := valid
		cfg.Storage.Endpoint = "s3.example.com"
		cfg.Storage.Bucket = "xlyra"
		cfg.Storage.AccessKey = "ak"
		cfg.Storage.SecretKeyEncrypted = "secret"
		cfg.Storage.BackupPassphraseEncrypted = "passphrase"
		tt.mutate(&cfg)

		err := ValidateAutomaticBackupConfig(cfg)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("expected %q validation error, got %v", tt.want, err)
		}
	}
}

func TestAutomaticBackupConfigHelpersHandleDefaultsAndRawTypes(t *testing.T) {
	t.Parallel()

	cfg := AutomaticBackupConfigFromRaw(map[string]any{
		"enabled":         true,
		"cron":            " ",
		"retention_count": float64(0),
		"storage": map[string]any{
			"endpoint": " s3.example.com ",
			"prefix":   "",
			"use_ssl":  false,
		},
	})

	if cfg.Cron != DefaultAutomaticBackupConfig().Cron || cfg.RetentionCount != DefaultAutomaticBackupConfig().RetentionCount {
		t.Fatalf("expected defaults for blank schedule config, got %#v", cfg)
	}
	if cfg.Storage.Endpoint != "s3.example.com" || cfg.Storage.Prefix != DefaultAutomaticBackupConfig().Storage.Prefix {
		t.Fatalf("storage defaults/normalization mismatch: %#v", cfg.Storage)
	}
	if cfg.Storage.UseSSL {
		t.Fatalf("explicit false use_ssl should be preserved")
	}
	if AutomaticBackupConfigHasCredentials(cfg) {
		t.Fatal("blank encrypted credentials should not count as present")
	}
	if !AutomaticBackupConfigHasStorage(cfg) {
		t.Fatal("endpoint should count as storage config")
	}
}

func TestReadAutomaticBackupConfigAndExportMap(t *testing.T) {
	t.Parallel()

	defaults, ok := ReadAutomaticBackupConfig(nil)
	if ok {
		t.Fatal("nil config file should report missing automatic backup config")
	}
	if defaults.Cron != DefaultAutomaticBackupConfig().Cron {
		t.Fatalf("nil config cron = %q, want default", defaults.Cron)
	}

	missing, ok := ReadAutomaticBackupConfig(&ConfigFile{data: map[string]any{}})
	if ok {
		t.Fatal("missing path should report missing automatic backup config")
	}
	if missing.Storage.Prefix != DefaultAutomaticBackupConfig().Storage.Prefix {
		t.Fatalf("missing config prefix = %q, want default", missing.Storage.Prefix)
	}

	confFile := &ConfigFile{data: map[string]any{
		"backup": map[string]any{
			"automatic": map[string]any{
				"enabled":         true,
				"cron":            " 0 5 * * * ",
				"retention_count": float64(14),
				"storage": map[string]any{
					"endpoint":                    " https://s3.example.com ",
					"region":                      " us-east-1 ",
					"bucket":                      " xlyra ",
					"prefix":                      " prod ",
					"access_key":                  " access ",
					"secret_key_encrypted":        " encrypted-secret ",
					"secret_key_masked":           " sk...ret ",
					"force_path_style":            false,
					"use_ssl":                     false,
					"skip_tls_verify":             true,
					"backup_passphrase_encrypted": " encrypted-passphrase ",
					"backup_passphrase_masked":    " pp...se ",
				},
			},
		},
	}}

	cfg, ok := ReadAutomaticBackupConfig(confFile)
	if !ok {
		t.Fatal("expected nested automatic backup config to be present")
	}
	if !cfg.Enabled || cfg.Cron != "0 5 * * *" || cfg.RetentionCount != 14 {
		t.Fatalf("unexpected schedule config: %#v", cfg)
	}
	if cfg.Storage.Endpoint != "https://s3.example.com" || cfg.Storage.Prefix != "prod/" || cfg.Storage.SkipTLSVerify != true {
		t.Fatalf("unexpected normalized storage config: %#v", cfg.Storage)
	}

	exported := AutomaticBackupConfigForExport(cfg)
	storage := exported["storage"].(map[string]any)
	if _, ok := storage["secret_key_encrypted"]; ok {
		t.Fatalf("export map leaked secret key: %#v", storage)
	}
	if _, ok := storage["backup_passphrase_encrypted"]; ok {
		t.Fatalf("export map leaked backup passphrase: %#v", storage)
	}
	if storage["endpoint"] != "https://s3.example.com" || storage["access_key"] != "access" || storage["prefix"] != "prod/" {
		t.Fatalf("export map lost non-secret storage fields: %#v", storage)
	}
}
