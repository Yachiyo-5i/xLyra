package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv              string        `env:"APP_ENV" envDefault:"development"`
	AppName             string        `env:"APP_NAME" envDefault:"xlyra-server"`
	HTTPHost            string        `env:"HTTP_HOST" envDefault:"0.0.0.0"`
	HTTPPort            int           `env:"HTTP_PORT" envDefault:"5801"`
	LogLevel            string        `env:"LOG_LEVEL" envDefault:"info"`
	LogDebugEnabled     bool          `env:"LOG_DEBUG_ENABLED" envDefault:"false"`
	LogDir              string        `env:"LOG_DIR" envDefault:"logs"`
	LogFilePrefix       string        `env:"LOG_FILE_PREFIX" envDefault:"xlyra-server"`
	LogRetentionDays    int           `env:"LOG_RETENTION_DAYS" envDefault:"14"`
	LogToStdout         bool          `env:"LOG_TO_STDOUT" envDefault:"true"`
	ReadHeaderTimeout   time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	RequestTimeout      time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s"`
	MaxRequestBodyBytes int64         `env:"MAX_REQUEST_BODY_BYTES" envDefault:"33554432"`
	ShutdownTimeout     time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	DBConnectTimeout    time.Duration `env:"DB_CONNECT_TIMEOUT" envDefault:"30s"`
	DBMinConns          int32         `env:"DB_MIN_CONNS" envDefault:"2"`
	DBMaxConns          int32         `env:"DB_MAX_CONNS" envDefault:"10"`
	CORSAllowedOrigins  []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:5173"`
	PostgresDSN         string        `env:"POSTGRES_DSN"`
	DBHost              string        `env:"DB_HOST"`
	DBPort              int           `env:"DB_PORT" envDefault:"5432"`
	DBName              string        `env:"DB_NAME" envDefault:"xlyra"`
	DBUser              string        `env:"DB_USER" envDefault:"postgres"`
	DBPassword          string        `env:"DB_PASSWORD"`
	DBSSLMode           string        `env:"DB_SSLMODE" envDefault:"disable"`
	SiteHealthInterval  time.Duration `env:"SITE_HEALTH_INTERVAL" envDefault:"15m"`
	SiteHealthTimeout   time.Duration `env:"SITE_HEALTH_TIMEOUT" envDefault:"10s"`
	SiteHealthWorkers   int           `env:"SITE_HEALTH_WORKERS" envDefault:"4"`
	StaticDir           string        `env:"STATIC_DIR" envDefault:"/app/dist"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}

	cfg.CORSAllowedOrigins = sanitizeOrigins(cfg.CORSAllowedOrigins)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	switch c.AppEnv {
	case "development", "test", "staging", "production":
	default:
		return fmt.Errorf("invalid APP_ENV %q", c.AppEnv)
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid LOG_LEVEL %q", c.LogLevel)
	}

	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return fmt.Errorf("HTTP_PORT must be between 1 and 65535")
	}

	if c.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("READ_HEADER_TIMEOUT must be greater than 0")
	}

	if c.RequestTimeout <= 0 {
		return fmt.Errorf("REQUEST_TIMEOUT must be greater than 0")
	}

	if c.MaxRequestBodyBytes <= 0 {
		return fmt.Errorf("MAX_REQUEST_BODY_BYTES must be greater than 0")
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("SHUTDOWN_TIMEOUT must be greater than 0")
	}

	if c.DBConnectTimeout <= 0 {
		return fmt.Errorf("DB_CONNECT_TIMEOUT must be greater than 0")
	}

	if c.DBMinConns < 0 {
		return fmt.Errorf("DB_MIN_CONNS must be greater than or equal to 0")
	}

	if c.DBMaxConns <= 0 {
		return fmt.Errorf("DB_MAX_CONNS must be greater than 0")
	}

	if c.DBMaxConns < c.DBMinConns {
		return fmt.Errorf("DB_MAX_CONNS must be greater than or equal to DB_MIN_CONNS")
	}

	if len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS must contain at least one origin")
	}

	if strings.TrimSpace(c.LogDir) == "" {
		return fmt.Errorf("LOG_DIR must not be empty")
	}

	if strings.TrimSpace(c.LogFilePrefix) == "" {
		return fmt.Errorf("LOG_FILE_PREFIX must not be empty")
	}

	if c.LogRetentionDays <= 0 {
		return fmt.Errorf("LOG_RETENTION_DAYS must be greater than 0")
	}

	if c.DBPort < 1 || c.DBPort > 65535 {
		return fmt.Errorf("DB_PORT must be between 1 and 65535")
	}

	if strings.TrimSpace(c.PostgresDSN) == "" {
		if strings.TrimSpace(c.DBHost) == "" {
			return fmt.Errorf("DB_HOST must not be empty")
		}
		if strings.TrimSpace(c.DBName) == "" {
			return fmt.Errorf("DB_NAME must not be empty")
		}
		if strings.TrimSpace(c.DBUser) == "" {
			return fmt.Errorf("DB_USER must not be empty")
		}
	}

	switch strings.TrimSpace(c.DBSSLMode) {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
	default:
		return fmt.Errorf("DB_SSLMODE must be one of disable, allow, prefer, require, verify-ca, verify-full")
	}

	if c.SiteHealthInterval <= 0 {
		return fmt.Errorf("SITE_HEALTH_INTERVAL must be greater than 0")
	}

	if c.SiteHealthTimeout <= 0 {
		return fmt.Errorf("SITE_HEALTH_TIMEOUT must be greater than 0")
	}

	if c.SiteHealthWorkers <= 0 {
		return fmt.Errorf("SITE_HEALTH_WORKERS must be greater than 0")
	}

	return nil
}

func sanitizeOrigins(origins []string) []string {
	sanitized := make([]string, 0, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}

		sanitized = append(sanitized, trimmed)
	}

	return sanitized
}
