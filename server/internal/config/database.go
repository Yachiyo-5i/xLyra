package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const maintenanceDatabaseName = "postgres"

func (c Config) DatabaseDSN() string {
	if strings.TrimSpace(c.PostgresDSN) != "" {
		return strings.TrimSpace(c.PostgresDSN)
	}
	return c.databaseDSNFor(c.DatabaseName())
}

func (c Config) MaintenanceDatabaseDSN() (string, error) {
	if strings.TrimSpace(c.PostgresDSN) == "" {
		return c.databaseDSNFor(maintenanceDatabaseName), nil
	}

	parsed, err := url.Parse(strings.TrimSpace(c.PostgresDSN))
	if err != nil {
		return "", fmt.Errorf("parse POSTGRES_DSN: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("POSTGRES_DSN must use postgres:// or postgresql:// when automatic database creation is enabled")
	}

	parsed.Path = "/" + maintenanceDatabaseName
	return parsed.String(), nil
}

func (c Config) DatabaseName() string {
	if strings.TrimSpace(c.PostgresDSN) == "" {
		return strings.TrimSpace(c.DBName)
	}

	parsed, err := url.Parse(strings.TrimSpace(c.PostgresDSN))
	if err != nil {
		return ""
	}
	name := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if name == "" {
		return ""
	}
	unescaped, err := url.PathUnescape(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(unescaped)
}

func (c Config) databaseDSNFor(database string) string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(strings.TrimSpace(c.DBUser), c.DBPassword),
		Host:   netHostPort(strings.TrimSpace(c.DBHost), c.DBPort),
		Path:   "/" + strings.TrimSpace(database),
	}
	query := u.Query()
	query.Set("sslmode", strings.TrimSpace(c.DBSSLMode))
	u.RawQuery = query.Encode()
	return u.String()
}

func netHostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]:" + strconv.Itoa(port)
	}
	return host + ":" + strconv.Itoa(port)
}
