package catalog

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestSyncAllEmptyCatalogSkipsRepositoryWrites(t *testing.T) {
	t.Parallel()

	requests := 0
	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(*gorm.DB) {})
	service := &SyncService{
		db:     catalogStoreWithGorm(t, db),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		client: &http.Client{Transport: catalogSyncRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			body := `{"schema_version":1,"catalog_version":"test","updated_at":"2026-01-01T00:00:00Z","brands":{}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
	}

	if err := service.SyncAll(context.Background()); err != nil {
		t.Fatalf("SyncAll empty catalog: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (catalog)", requests)
	}
}
