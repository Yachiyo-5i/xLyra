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
			body := `{"unknown":{"models":{"ignored":{"id":"ignored"}}},"openai":{"models":{}}}`
			if strings.Contains(req.URL.String(), "model-price-repo") {
				body = `{"ignored":{"litellm_provider":"bedrock"}}`
			}
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
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (models.dev + litellm)", requests)
	}
}
