package config

import (
	"strings"
	"testing"
)

func TestValidateAutomaticBackupStorageConfigRequiresManualActionSecrets(t *testing.T) {
	t.Parallel()

	valid := DefaultAutomaticBackupConfig()
	valid.Storage.Endpoint = "https://s3.example.com"
	valid.Storage.Bucket = "xlyra"
	valid.Storage.AccessKey = "ak"
	valid.Storage.SecretKeyEncrypted = "secret"
	valid.Storage.BackupPassphraseEncrypted = "passphrase"
	if err := ValidateAutomaticBackupStorageConfig(valid); err != nil {
		t.Fatalf("valid storage config returned error: %v", err)
	}

	for _, tc := range []struct {
		name   string
		want   string
		mutate func(*AutomaticBackupConfig)
	}{
		{name: "endpoint", want: "storage.endpoint", mutate: func(cfg *AutomaticBackupConfig) { cfg.Storage.Endpoint = " \t\n " }},
		{name: "bucket", want: "storage.bucket", mutate: func(cfg *AutomaticBackupConfig) { cfg.Storage.Bucket = " \t\n " }},
		{name: "access key", want: "storage.access_key", mutate: func(cfg *AutomaticBackupConfig) { cfg.Storage.AccessKey = " \t\n " }},
		{name: "secret key", want: "storage.secret_key", mutate: func(cfg *AutomaticBackupConfig) { cfg.Storage.SecretKeyEncrypted = " \t\n " }},
		{name: "backup passphrase", want: "storage.backup_passphrase", mutate: func(cfg *AutomaticBackupConfig) { cfg.Storage.BackupPassphraseEncrypted = " \t\n " }},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := valid
			tc.mutate(&cfg)
			err := ValidateAutomaticBackupStorageConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateAutomaticBackupStorageConfig error = %v, want %q", err, tc.want)
			}
		})
	}
}
