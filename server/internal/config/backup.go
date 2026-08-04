package config

import (
	"fmt"
	"strings"
)

const AutomaticBackupConfigPath = "backup.automatic"

type AutomaticBackupStorageConfig struct {
	Endpoint                  string `json:"endpoint"`
	Region                    string `json:"region"`
	Bucket                    string `json:"bucket"`
	Prefix                    string `json:"prefix"`
	AccessKey                 string `json:"access_key"`
	SecretKeyEncrypted        string `json:"secret_key_encrypted,omitempty"`
	SecretKeyMasked           string `json:"secret_key_masked,omitempty"`
	ForcePathStyle            bool   `json:"force_path_style"`
	UseSSL                    bool   `json:"use_ssl"`
	SkipTLSVerify             bool   `json:"skip_tls_verify"`
	BackupPassphraseEncrypted string `json:"backup_passphrase_encrypted,omitempty"`
	BackupPassphraseMasked    string `json:"backup_passphrase_masked,omitempty"`
}

type AutomaticBackupConfig struct {
	Enabled        bool                         `json:"enabled"`
	Cron           string                       `json:"cron"`
	RetentionCount int                          `json:"retention_count"`
	Storage        AutomaticBackupStorageConfig `json:"storage"`
}

func DefaultAutomaticBackupConfig() AutomaticBackupConfig {
	return AutomaticBackupConfig{
		Enabled:        false,
		Cron:           "0 3 * * *",
		RetentionCount: 7,
		Storage: AutomaticBackupStorageConfig{
			Prefix:         "xlyra/",
			ForcePathStyle: true,
			UseSSL:         true,
		},
	}
}

func ReadAutomaticBackupConfig(confFile *ConfigFile) (AutomaticBackupConfig, bool) {
	if confFile == nil {
		return DefaultAutomaticBackupConfig(), false
	}
	raw, ok := confFile.Get(AutomaticBackupConfigPath)
	if !ok || raw == nil {
		return DefaultAutomaticBackupConfig(), false
	}
	return AutomaticBackupConfigFromRaw(raw), true
}

func AutomaticBackupConfigFromRaw(raw any) AutomaticBackupConfig {
	cfg := DefaultAutomaticBackupConfig()
	root, ok := raw.(map[string]any)
	if !ok {
		return cfg
	}

	cfg.Enabled = boolFromMap(root, "enabled", cfg.Enabled)
	cfg.Cron = stringFromMap(root, "cron", cfg.Cron)
	cfg.RetentionCount = intFromMap(root, "retention_count", cfg.RetentionCount)
	if storage, ok := root["storage"].(map[string]any); ok {
		cfg.Storage.Endpoint = stringFromMap(storage, "endpoint", cfg.Storage.Endpoint)
		cfg.Storage.Region = stringFromMap(storage, "region", cfg.Storage.Region)
		cfg.Storage.Bucket = stringFromMap(storage, "bucket", cfg.Storage.Bucket)
		cfg.Storage.Prefix = stringFromMap(storage, "prefix", cfg.Storage.Prefix)
		cfg.Storage.AccessKey = stringFromMap(storage, "access_key", cfg.Storage.AccessKey)
		cfg.Storage.SecretKeyEncrypted = stringFromMap(storage, "secret_key_encrypted", cfg.Storage.SecretKeyEncrypted)
		cfg.Storage.SecretKeyMasked = stringFromMap(storage, "secret_key_masked", cfg.Storage.SecretKeyMasked)
		cfg.Storage.ForcePathStyle = boolFromMap(storage, "force_path_style", cfg.Storage.ForcePathStyle)
		cfg.Storage.UseSSL = boolFromMap(storage, "use_ssl", cfg.Storage.UseSSL)
		cfg.Storage.SkipTLSVerify = boolFromMap(storage, "skip_tls_verify", cfg.Storage.SkipTLSVerify)
		cfg.Storage.BackupPassphraseEncrypted = stringFromMap(storage, "backup_passphrase_encrypted", cfg.Storage.BackupPassphraseEncrypted)
		cfg.Storage.BackupPassphraseMasked = stringFromMap(storage, "backup_passphrase_masked", cfg.Storage.BackupPassphraseMasked)
	}
	return NormalizeAutomaticBackupConfig(cfg)
}

func NormalizeAutomaticBackupConfig(cfg AutomaticBackupConfig) AutomaticBackupConfig {
	defaults := DefaultAutomaticBackupConfig()
	cfg.Cron = strings.TrimSpace(cfg.Cron)
	if cfg.Cron == "" {
		cfg.Cron = defaults.Cron
	}
	if cfg.RetentionCount == 0 {
		cfg.RetentionCount = defaults.RetentionCount
	}
	cfg.Storage.Endpoint = strings.TrimSpace(cfg.Storage.Endpoint)
	cfg.Storage.Region = strings.TrimSpace(cfg.Storage.Region)
	cfg.Storage.Bucket = strings.TrimSpace(cfg.Storage.Bucket)
	cfg.Storage.Prefix = NormalizeBackupPrefix(cfg.Storage.Prefix)
	cfg.Storage.AccessKey = strings.TrimSpace(cfg.Storage.AccessKey)
	cfg.Storage.SecretKeyEncrypted = strings.TrimSpace(cfg.Storage.SecretKeyEncrypted)
	cfg.Storage.SecretKeyMasked = strings.TrimSpace(cfg.Storage.SecretKeyMasked)
	cfg.Storage.BackupPassphraseEncrypted = strings.TrimSpace(cfg.Storage.BackupPassphraseEncrypted)
	cfg.Storage.BackupPassphraseMasked = strings.TrimSpace(cfg.Storage.BackupPassphraseMasked)
	return cfg
}

func ValidateAutomaticBackupConfig(cfg AutomaticBackupConfig) error {
	if err := ValidateCronExpression(cfg.Cron); err != nil {
		return fmt.Errorf("cron: %w", err)
	}
	if cfg.RetentionCount <= 0 {
		return fmt.Errorf("retention_count must be greater than 0")
	}
	if cfg.RetentionCount > 365 {
		return fmt.Errorf("retention_count must be less than or equal to 365")
	}
	if cfg.Enabled {
		if strings.TrimSpace(cfg.Storage.Endpoint) == "" {
			return fmt.Errorf("storage.endpoint is required")
		}
		if strings.TrimSpace(cfg.Storage.Bucket) == "" {
			return fmt.Errorf("storage.bucket is required")
		}
		if strings.TrimSpace(cfg.Storage.AccessKey) == "" {
			return fmt.Errorf("storage.access_key is required")
		}
		if strings.TrimSpace(cfg.Storage.SecretKeyEncrypted) == "" {
			return fmt.Errorf("storage.secret_key is required")
		}
		if strings.TrimSpace(cfg.Storage.BackupPassphraseEncrypted) == "" {
			return fmt.Errorf("storage.backup_passphrase is required")
		}
	}
	return nil
}

func ValidateAutomaticBackupStorageConfig(cfg AutomaticBackupConfig) error {
	cfg = NormalizeAutomaticBackupConfig(cfg)
	if strings.TrimSpace(cfg.Storage.Endpoint) == "" {
		return fmt.Errorf("storage.endpoint is required")
	}
	if strings.TrimSpace(cfg.Storage.Bucket) == "" {
		return fmt.Errorf("storage.bucket is required")
	}
	if strings.TrimSpace(cfg.Storage.AccessKey) == "" {
		return fmt.Errorf("storage.access_key is required")
	}
	if strings.TrimSpace(cfg.Storage.SecretKeyEncrypted) == "" {
		return fmt.Errorf("storage.secret_key is required")
	}
	if strings.TrimSpace(cfg.Storage.BackupPassphraseEncrypted) == "" {
		return fmt.Errorf("storage.backup_passphrase is required")
	}
	return nil
}

func AutomaticBackupConfigToMap(cfg AutomaticBackupConfig) map[string]any {
	cfg = NormalizeAutomaticBackupConfig(cfg)
	return map[string]any{
		"enabled":         cfg.Enabled,
		"cron":            cfg.Cron,
		"retention_count": cfg.RetentionCount,
		"storage": map[string]any{
			"endpoint":                    cfg.Storage.Endpoint,
			"region":                      cfg.Storage.Region,
			"bucket":                      cfg.Storage.Bucket,
			"prefix":                      cfg.Storage.Prefix,
			"access_key":                  cfg.Storage.AccessKey,
			"secret_key_encrypted":        cfg.Storage.SecretKeyEncrypted,
			"secret_key_masked":           cfg.Storage.SecretKeyMasked,
			"force_path_style":            cfg.Storage.ForcePathStyle,
			"use_ssl":                     cfg.Storage.UseSSL,
			"skip_tls_verify":             cfg.Storage.SkipTLSVerify,
			"backup_passphrase_encrypted": cfg.Storage.BackupPassphraseEncrypted,
			"backup_passphrase_masked":    cfg.Storage.BackupPassphraseMasked,
		},
	}
}

func AutomaticBackupConfigForExport(cfg AutomaticBackupConfig) map[string]any {
	cfg = NormalizeAutomaticBackupConfig(cfg)
	return map[string]any{
		"enabled":         cfg.Enabled,
		"cron":            cfg.Cron,
		"retention_count": cfg.RetentionCount,
		"storage": map[string]any{
			"endpoint":         cfg.Storage.Endpoint,
			"region":           cfg.Storage.Region,
			"bucket":           cfg.Storage.Bucket,
			"prefix":           cfg.Storage.Prefix,
			"access_key":       cfg.Storage.AccessKey,
			"force_path_style": cfg.Storage.ForcePathStyle,
			"use_ssl":          cfg.Storage.UseSSL,
			"skip_tls_verify":  cfg.Storage.SkipTLSVerify,
		},
	}
}

func AutomaticBackupConfigHasCredentials(cfg AutomaticBackupConfig) bool {
	cfg = NormalizeAutomaticBackupConfig(cfg)
	return cfg.Storage.SecretKeyEncrypted != "" && cfg.Storage.BackupPassphraseEncrypted != ""
}

func AutomaticBackupConfigReady(cfg AutomaticBackupConfig) bool {
	return ValidateAutomaticBackupStorageConfig(cfg) == nil
}

func AutomaticBackupConfigHasStorage(cfg AutomaticBackupConfig) bool {
	cfg = NormalizeAutomaticBackupConfig(cfg)
	return cfg.Storage.Endpoint != "" || cfg.Storage.Bucket != "" || cfg.Storage.AccessKey != "" || cfg.Storage.Prefix != ""
}

func NormalizeBackupPrefix(value string) string {
	prefix := strings.TrimSpace(strings.TrimLeft(value, "/"))
	if prefix == "" {
		return DefaultAutomaticBackupConfig().Storage.Prefix
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

func SanitizeConfigForBackup(data map[string]any) map[string]any {
	result := deepCopyMap(data)
	raw, ok := getByPath(result, AutomaticBackupConfigPath)
	if !ok || raw == nil {
		return result
	}
	setByPath(result, AutomaticBackupConfigPath, AutomaticBackupConfigForExport(AutomaticBackupConfigFromRaw(raw)))
	return result
}

func MergeImportedConfig(current map[string]any, imported map[string]any) map[string]any {
	result := deepCopyMap(imported)
	currentAutomatic, currentHasAutomatic := automaticBackupFromMap(current)
	if currentHasAutomatic && AutomaticBackupConfigHasStorage(currentAutomatic) {
		setByPath(result, AutomaticBackupConfigPath, AutomaticBackupConfigToMap(currentAutomatic))
		return result
	}
	importedAutomatic, importedHasAutomatic := automaticBackupFromMap(imported)
	if importedHasAutomatic {
		setByPath(result, AutomaticBackupConfigPath, AutomaticBackupConfigForExport(importedAutomatic))
	}
	return result
}

func automaticBackupFromMap(data map[string]any) (AutomaticBackupConfig, bool) {
	if data == nil {
		return DefaultAutomaticBackupConfig(), false
	}
	raw, ok := getByPath(data, AutomaticBackupConfigPath)
	if !ok || raw == nil {
		return DefaultAutomaticBackupConfig(), false
	}
	return AutomaticBackupConfigFromRaw(raw), true
}
