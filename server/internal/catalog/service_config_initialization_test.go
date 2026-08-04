package catalog

import (
	"io"
	"log/slog"
	"testing"

	"xlyra/server/internal/config"
)

func TestServicesInitializeDependenciesFromConfigFile(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}

	service := NewService(nil, confFile)
	if service == nil {
		t.Fatal("NewService returned nil")
	}
	if service.db != nil {
		t.Fatalf("NewService db = %#v, want nil", service.db)
	}
	if service.capabilities == nil {
		t.Fatal("NewService should initialize capabilities with config file")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	syncService := NewSyncService(nil, logger, confFile)
	if syncService == nil {
		t.Fatal("NewSyncService returned nil")
	}
	if syncService.logger != logger {
		t.Fatal("NewSyncService did not preserve logger")
	}
	if syncService.client == nil {
		t.Fatal("NewSyncService should initialize an HTTP client with config file")
	}
}
