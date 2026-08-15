package config

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

const GeneralConfigPath = "global.general"

type GeneralTaskConfig struct {
	SiteRefreshCron   string `json:"site_refresh_cron"`
	NewAPICheckinCron string `json:"newapi_checkin_cron"`
}

type GeneralIPWhitelistConfig struct {
	Enabled bool     `json:"enabled"`
	Entries []string `json:"entries"`
}

type GeneralLogConfig struct {
	Level          string `json:"level"`
	CleanupEnabled bool   `json:"cleanup_enabled"`
	RetentionDays  int    `json:"retention_days"`
}

type GeneralDataConfig struct {
	RequestDetailCleanupEnabled bool `json:"request_detail_cleanup_enabled"`
	RequestDetailRetentionDays  int  `json:"request_detail_retention_days"`
}

type GeneralCacheConfig struct {
	Enabled                 bool `json:"enabled"`
	ObservationEnabled      bool `json:"observation_enabled"`
	ObservationTTLMinutes   int  `json:"observation_ttl_minutes"`
	ObservationHistoryLimit int  `json:"observation_history_limit"`
}

type GeneralSecurityConfig struct {
	SessionLifetimeHours int `json:"session_lifetime_hours"`
}

type GeneralConfig struct {
	Tasks       GeneralTaskConfig        `json:"tasks"`
	IPWhitelist GeneralIPWhitelistConfig `json:"ip_whitelist"`
	Log         GeneralLogConfig         `json:"log"`
	Data        GeneralDataConfig        `json:"data"`
	Cache       GeneralCacheConfig       `json:"cache"`
	Security    GeneralSecurityConfig    `json:"security"`
}

func DefaultGeneralConfig() GeneralConfig {
	return GeneralConfig{
		Tasks: GeneralTaskConfig{
			SiteRefreshCron:   "0 */15 * * *",
			NewAPICheckinCron: "0 9 * * *",
		},
		IPWhitelist: GeneralIPWhitelistConfig{
			Enabled: false,
			Entries: []string{},
		},
		Log: GeneralLogConfig{
			Level:          "info",
			CleanupEnabled: true,
			RetentionDays:  30,
		},
		Data: GeneralDataConfig{
			RequestDetailCleanupEnabled: true,
			RequestDetailRetentionDays:  90,
		},
		Cache: GeneralCacheConfig{
			Enabled:                 true,
			ObservationEnabled:      true,
			ObservationTTLMinutes:   30,
			ObservationHistoryLimit: 12,
		},
		Security: GeneralSecurityConfig{
			SessionLifetimeHours: 24,
		},
	}
}

func ReadGeneralConfig(confFile *ConfigFile) GeneralConfig {
	if confFile == nil {
		return DefaultGeneralConfig()
	}
	raw, ok := confFile.Get(GeneralConfigPath)
	if !ok || raw == nil {
		return DefaultGeneralConfig()
	}
	return GeneralConfigFromRaw(raw)
}

func GeneralConfigFromRaw(raw any) GeneralConfig {
	cfg := DefaultGeneralConfig()
	root, ok := raw.(map[string]any)
	if !ok {
		return cfg
	}

	if tasks, ok := root["tasks"].(map[string]any); ok {
		cfg.Tasks.SiteRefreshCron = stringFromMap(tasks, "site_refresh_cron", cfg.Tasks.SiteRefreshCron)
		cfg.Tasks.NewAPICheckinCron = stringFromMap(tasks, "newapi_checkin_cron", cfg.Tasks.NewAPICheckinCron)
	}
	if whitelist, ok := root["ip_whitelist"].(map[string]any); ok {
		cfg.IPWhitelist.Enabled = boolFromMap(whitelist, "enabled", cfg.IPWhitelist.Enabled)
		cfg.IPWhitelist.Entries = stringSliceFromMap(whitelist, "entries", cfg.IPWhitelist.Entries)
	}
	if logConfig, ok := root["log"].(map[string]any); ok {
		cfg.Log.Level = stringFromMap(logConfig, "level", cfg.Log.Level)
		cfg.Log.CleanupEnabled = boolFromMap(logConfig, "cleanup_enabled", cfg.Log.CleanupEnabled)
		cfg.Log.RetentionDays = intFromMap(logConfig, "retention_days", cfg.Log.RetentionDays)
	}
	if dataConfig, ok := root["data"].(map[string]any); ok {
		cfg.Data.RequestDetailCleanupEnabled = boolFromMap(dataConfig, "request_detail_cleanup_enabled", cfg.Data.RequestDetailCleanupEnabled)
		cfg.Data.RequestDetailRetentionDays = intFromMap(dataConfig, "request_detail_retention_days", cfg.Data.RequestDetailRetentionDays)
	}
	if cacheConfig, ok := root["cache"].(map[string]any); ok {
		cfg.Cache.Enabled = boolFromMap(cacheConfig, "enabled", cfg.Cache.Enabled)
		cfg.Cache.ObservationEnabled = boolFromMap(cacheConfig, "observation_enabled", cfg.Cache.ObservationEnabled)
		cfg.Cache.ObservationTTLMinutes = intFromMap(cacheConfig, "observation_ttl_minutes", cfg.Cache.ObservationTTLMinutes)
		cfg.Cache.ObservationHistoryLimit = intFromMap(cacheConfig, "observation_history_limit", cfg.Cache.ObservationHistoryLimit)
	}
	if security, ok := root["security"].(map[string]any); ok {
		cfg.Security.SessionLifetimeHours = intFromMap(security, "session_lifetime_hours", cfg.Security.SessionLifetimeHours)
	}
	return NormalizeGeneralConfig(cfg)
}

func NormalizeGeneralConfig(cfg GeneralConfig) GeneralConfig {
	defaults := DefaultGeneralConfig()
	cfg.Tasks.SiteRefreshCron = strings.TrimSpace(cfg.Tasks.SiteRefreshCron)
	if cfg.Tasks.SiteRefreshCron == "" {
		cfg.Tasks.SiteRefreshCron = defaults.Tasks.SiteRefreshCron
	}
	cfg.Tasks.NewAPICheckinCron = strings.TrimSpace(cfg.Tasks.NewAPICheckinCron)
	if cfg.Tasks.NewAPICheckinCron == "" {
		cfg.Tasks.NewAPICheckinCron = defaults.Tasks.NewAPICheckinCron
	}

	entries := make([]string, 0, len(cfg.IPWhitelist.Entries))
	for _, entry := range cfg.IPWhitelist.Entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	cfg.IPWhitelist.Entries = entries

	cfg.Log.Level = strings.ToLower(strings.TrimSpace(cfg.Log.Level))
	if cfg.Log.Level == "" {
		cfg.Log.Level = defaults.Log.Level
	}
	if cfg.Log.RetentionDays == 0 {
		cfg.Log.RetentionDays = defaults.Log.RetentionDays
	}
	if cfg.Data.RequestDetailRetentionDays == 0 {
		cfg.Data.RequestDetailRetentionDays = defaults.Data.RequestDetailRetentionDays
	}
	if cfg.Cache.ObservationTTLMinutes == 0 {
		cfg.Cache.ObservationTTLMinutes = defaults.Cache.ObservationTTLMinutes
	}
	if cfg.Cache.ObservationHistoryLimit == 0 {
		cfg.Cache.ObservationHistoryLimit = defaults.Cache.ObservationHistoryLimit
	}
	return cfg
}

func ValidateGeneralConfig(cfg GeneralConfig) error {
	if err := ValidateCronExpression(cfg.Tasks.SiteRefreshCron); err != nil {
		return fmt.Errorf("tasks.site_refresh_cron: %w", err)
	}
	if err := ValidateCronExpression(cfg.Tasks.NewAPICheckinCron); err != nil {
		return fmt.Errorf("tasks.newapi_checkin_cron: %w", err)
	}
	for _, entry := range cfg.IPWhitelist.Entries {
		if err := ValidateIPWhitelistEntry(entry); err != nil {
			return fmt.Errorf("ip_whitelist.entries: %s is invalid", entry)
		}
	}
	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be one of debug, info, warn, error")
	}
	if cfg.Log.RetentionDays <= 0 {
		return fmt.Errorf("log.retention_days must be greater than 0")
	}
	if cfg.Data.RequestDetailRetentionDays <= 0 {
		return fmt.Errorf("data.request_detail_retention_days must be greater than 0")
	}
	if cfg.Cache.Enabled && cfg.Cache.ObservationEnabled {
		if cfg.Cache.ObservationTTLMinutes <= 0 || cfg.Cache.ObservationTTLMinutes > 1440 {
			return fmt.Errorf("cache.observation_ttl_minutes must be between 1 and 1440")
		}
		if cfg.Cache.ObservationHistoryLimit <= 0 || cfg.Cache.ObservationHistoryLimit > 200 {
			return fmt.Errorf("cache.observation_history_limit must be between 1 and 200")
		}
	}
	if cfg.Security.SessionLifetimeHours < 0 || cfg.Security.SessionLifetimeHours > 720 {
		return fmt.Errorf("security.session_lifetime_hours must be between 0 and 720")
	}
	return nil
}

func GeneralConfigToMap(cfg GeneralConfig) map[string]any {
	return map[string]any{
		"tasks": map[string]any{
			"site_refresh_cron":   cfg.Tasks.SiteRefreshCron,
			"newapi_checkin_cron": cfg.Tasks.NewAPICheckinCron,
		},
		"ip_whitelist": map[string]any{
			"enabled": cfg.IPWhitelist.Enabled,
			"entries": cfg.IPWhitelist.Entries,
		},
		"log": map[string]any{
			"level":           cfg.Log.Level,
			"cleanup_enabled": cfg.Log.CleanupEnabled,
			"retention_days":  cfg.Log.RetentionDays,
		},
		"data": map[string]any{
			"request_detail_cleanup_enabled": cfg.Data.RequestDetailCleanupEnabled,
			"request_detail_retention_days":  cfg.Data.RequestDetailRetentionDays,
		},
		"cache": map[string]any{
			"enabled":                   cfg.Cache.Enabled,
			"observation_enabled":       cfg.Cache.ObservationEnabled,
			"observation_ttl_minutes":   cfg.Cache.ObservationTTLMinutes,
			"observation_history_limit": cfg.Cache.ObservationHistoryLimit,
		},
		"security": map[string]any{
			"session_lifetime_hours": cfg.Security.SessionLifetimeHours,
		},
	}
}

func ValidateCronExpression(value string) error {
	parts := strings.Fields(value)
	if len(parts) != 5 {
		return fmt.Errorf("cron expression must have 5 fields")
	}
	ranges := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, part := range parts {
		for _, segment := range strings.Split(part, ",") {
			if !validateCronSegment(segment, ranges[i][0], ranges[i][1]) {
				return fmt.Errorf("invalid cron segment %q", segment)
			}
		}
	}
	return nil
}

func ValidateIPWhitelistEntry(value string) error {
	if strings.Contains(value, "/") {
		_, err := netip.ParsePrefix(value)
		return err
	}
	_, err := netip.ParseAddr(value)
	return err
}

func validateCronSegment(segment string, min int, max int) bool {
	if segment == "*" {
		return true
	}
	if strings.HasPrefix(segment, "*/") {
		step, err := strconv.Atoi(strings.TrimPrefix(segment, "*/"))
		return err == nil && step > 0
	}
	if strings.Contains(segment, "-") {
		parts := strings.Split(segment, "-")
		if len(parts) != 2 {
			return false
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return false
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return false
		}
		return start >= min && end <= max && start <= end
	}
	parsed, err := strconv.Atoi(segment)
	return err == nil && parsed >= min && parsed <= max
}

func stringFromMap(values map[string]any, key string, fallback string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	return fallback
}

func boolFromMap(values map[string]any, key string, fallback bool) bool {
	if value, ok := values[key].(bool); ok {
		return value
	}
	return fallback
}

func intFromMap(values map[string]any, key string, fallback int) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		if value != float64(int(value)) {
			return fallback
		}
		return int(value)
	default:
		return fallback
	}
}

func stringSliceFromMap(values map[string]any, key string, fallback []string) []string {
	switch value := values[key].(type) {
	case []string:
		return value
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return items
	default:
		return fallback
	}
}
