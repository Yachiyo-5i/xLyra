package config

import (
	"strings"
	"testing"
	"time"
)

func TestValidateRejectsInvalidLogLevel(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.LogLevel = "verbose"

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected LOG_LEVEL validation error, got %v", err)
	}
}

func TestValidateRejectsInvalidConnectionPoolBounds(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.DBMinConns = 5
	cfg.DBMaxConns = 4

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
		t.Fatalf("expected DB_MAX_CONNS validation error, got %v", err)
	}
}

func TestValidateRejectsInvalidDatabaseSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*Config){
		"DB_PORT":    func(cfg *Config) { cfg.DBPort = 70000 },
		"DB_HOST":    func(cfg *Config) { cfg.DBHost = "" },
		"DB_NAME":    func(cfg *Config) { cfg.DBName = "" },
		"DB_USER":    func(cfg *Config) { cfg.DBUser = "" },
		"DB_SSLMODE": func(cfg *Config) { cfg.DBSSLMode = "invalid" },
	}

	for name, mutate := range cases {
		cfg := validConfig()
		cfg.PostgresDSN = ""
		mutate(&cfg)

		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("expected %s validation error, got %v", name, err)
		}
	}
}

func TestDatabaseDSNFromParts(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.PostgresDSN = ""
	cfg.DBHost = "postgres"
	cfg.DBPort = 5432
	cfg.DBName = "xlyra"
	cfg.DBUser = "postgres"
	cfg.DBPassword = "secret"
	cfg.DBSSLMode = "disable"

	if got := cfg.DatabaseDSN(); got != "postgres://postgres:secret@postgres:5432/xlyra?sslmode=disable" {
		t.Fatalf("unexpected database dsn: %s", got)
	}
}

func TestDatabaseDSNUsesPostgresDSNAndExtractsDatabaseName(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.PostgresDSN = " postgres://user:pass@localhost:5432/xlyra%20prod?sslmode=require "

	if got := cfg.DatabaseDSN(); got != "postgres://user:pass@localhost:5432/xlyra%20prod?sslmode=require" {
		t.Fatalf("database dsn = %q", got)
	}
	if got := cfg.DatabaseName(); got != "xlyra prod" {
		t.Fatalf("database name = %q, want xlyra prod", got)
	}
	maintenance, err := cfg.MaintenanceDatabaseDSN()
	if err != nil {
		t.Fatalf("maintenance dsn: %v", err)
	}
	if maintenance != "postgres://user:pass@localhost:5432/postgres?sslmode=require" {
		t.Fatalf("maintenance dsn = %q", maintenance)
	}
}

func TestMaintenanceDatabaseDSNRejectsInvalidPostgresDSN(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.PostgresDSN = "http://localhost/xlyra"

	if _, err := cfg.MaintenanceDatabaseDSN(); err == nil || !strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("expected invalid scheme error, got %v", err)
	}
}

func TestDatabaseHelpersUsePartsForMaintenanceAndIPv6(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.PostgresDSN = ""
	cfg.DBHost = "2001:db8::1"
	cfg.DBUser = "postgres"
	cfg.DBPassword = "secret"
	cfg.DBSSLMode = "disable"

	if got := cfg.DatabaseName(); got != "xlyra" {
		t.Fatalf("database name = %q", got)
	}
	if got := cfg.DatabaseDSN(); got != "postgres://postgres:secret@[2001:db8::1]:5432/xlyra?sslmode=disable" {
		t.Fatalf("database dsn = %q", got)
	}
	maintenance, err := cfg.MaintenanceDatabaseDSN()
	if err != nil {
		t.Fatalf("maintenance dsn: %v", err)
	}
	if maintenance != "postgres://postgres:secret@[2001:db8::1]:5432/postgres?sslmode=disable" {
		t.Fatalf("maintenance dsn = %q", maintenance)
	}
}

func TestValidateRejectsInvalidLogFileSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*Config){
		"LOG_DIR":            func(cfg *Config) { cfg.LogDir = " " },
		"LOG_FILE_PREFIX":    func(cfg *Config) { cfg.LogFilePrefix = "" },
		"LOG_RETENTION_DAYS": func(cfg *Config) { cfg.LogRetentionDays = 0 },
	}

	for name, mutate := range cases {
		cfg := validConfig()
		mutate(&cfg)

		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("expected %s validation error, got %v", name, err)
		}
	}
}

func TestValidateRejectsInvalidSiteHealthSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*Config){
		"SITE_HEALTH_INTERVAL": func(cfg *Config) { cfg.SiteHealthInterval = 0 },
		"SITE_HEALTH_TIMEOUT":  func(cfg *Config) { cfg.SiteHealthTimeout = 0 },
		"SITE_HEALTH_WORKERS":  func(cfg *Config) { cfg.SiteHealthWorkers = 0 },
	}

	for name, mutate := range cases {
		cfg := validConfig()
		mutate(&cfg)

		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("expected %s validation error, got %v", name, err)
		}
	}
}

func TestValidateRejectsInvalidCoreSettings(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*Config){
		"APP_ENV":                func(cfg *Config) { cfg.AppEnv = "local" },
		"HTTP_PORT":              func(cfg *Config) { cfg.HTTPPort = 0 },
		"READ_HEADER_TIMEOUT":    func(cfg *Config) { cfg.ReadHeaderTimeout = 0 },
		"REQUEST_TIMEOUT":        func(cfg *Config) { cfg.RequestTimeout = 0 },
		"MAX_REQUEST_BODY_BYTES": func(cfg *Config) { cfg.MaxRequestBodyBytes = 0 },
		"SHUTDOWN_TIMEOUT":       func(cfg *Config) { cfg.ShutdownTimeout = 0 },
		"DB_CONNECT_TIMEOUT":     func(cfg *Config) { cfg.DBConnectTimeout = 0 },
		"DB_MIN_CONNS":           func(cfg *Config) { cfg.DBMinConns = -1 },
		"CORS_ALLOWED_ORIGINS": func(cfg *Config) {
			cfg.CORSAllowedOrigins = nil
		},
	}

	for name, mutate := range cases {
		cfg := validConfig()
		mutate(&cfg)

		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("expected %s validation error, got %v", name, err)
		}
	}
}

func TestLoadUsesEnvironmentAndSanitizesOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("HTTP_PORT", "5901")
	t.Setenv("POSTGRES_DSN", "postgres://user:pass@localhost:5432/xlyra?sslmode=disable")
	t.Setenv("CORS_ALLOWED_ORIGINS", " http://localhost:5173 , ,https://admin.example.com ")
	t.Setenv("SITE_HEALTH_INTERVAL", "5m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AppEnv != "test" || cfg.HTTPPort != 5901 {
		t.Fatalf("unexpected env config: %#v", cfg)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[1] != "https://admin.example.com" {
		t.Fatalf("origins were not sanitized: %#v", cfg.CORSAllowedOrigins)
	}
}

func TestSanitizeOriginsDropsBlankValues(t *testing.T) {
	t.Parallel()

	origins := sanitizeOrigins([]string{" http://localhost:5173 ", "", "  ", "https://admin.example.com"})
	if len(origins) != 2 {
		t.Fatalf("expected 2 origins after sanitize, got %d", len(origins))
	}

	if origins[0] != "http://localhost:5173" {
		t.Fatalf("unexpected first origin %q", origins[0])
	}
}

func validConfig() Config {
	return Config{
		AppEnv:              "development",
		AppName:             "xlyra-server",
		HTTPHost:            "0.0.0.0",
		HTTPPort:            5801,
		LogLevel:            "info",
		LogDir:              "logs",
		LogFilePrefix:       "xlyra-server",
		LogRetentionDays:    14,
		ReadHeaderTimeout:   5 * time.Second,
		RequestTimeout:      30 * time.Second,
		MaxRequestBodyBytes: 33554432,
		ShutdownTimeout:     10 * time.Second,
		DBConnectTimeout:    30 * time.Second,
		DBMinConns:          2,
		DBMaxConns:          10,
		CORSAllowedOrigins:  []string{"http://localhost:5173"},
		DBHost:              "postgres",
		DBPort:              5432,
		DBName:              "xlyra",
		DBUser:              "postgres",
		DBPassword:          "postgres",
		DBSSLMode:           "disable",
		SiteHealthInterval:  15 * time.Minute,
		SiteHealthTimeout:   10 * time.Second,
		SiteHealthWorkers:   4,
	}
}
