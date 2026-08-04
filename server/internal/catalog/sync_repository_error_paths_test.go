package catalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/store"
)

type catalogSyncRoundTripFunc func(*http.Request) (*http.Response, error)

func (f catalogSyncRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSyncOfficialDisplayNameEarlyReturnsWithoutRepository(t *testing.T) {
	t.Parallel()

	canonical := store.CanonicalModel{
		ID:          uuid.New(),
		ModelKey:    "gpt-4o",
		DisplayName: "gpt-4o",
		Provider:    "openai",
		Status:      "active",
	}
	(&Service{}).syncOfficialDisplayName(context.Background(), store.CanonicalModelRepository{}, &canonical)
	if canonical.DisplayName != "gpt-4o" {
		t.Fatalf("DisplayName = %q, want unchanged gpt-4o", canonical.DisplayName)
	}

	requested := false
	service := &Service{capabilities: modelcapabilities.NewWithConfig(modelcapabilities.Config{
		HTTPClient: &http.Client{Transport: catalogSyncRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})},
	})}
	canonical.DisplayName = "GPT-4o"
	service.syncOfficialDisplayName(context.Background(), store.CanonicalModelRepository{}, &canonical)
	if requested {
		t.Fatal("OfficialName lookup should not run when display name already differs from model key")
	}
	if canonical.DisplayName != "GPT-4o" {
		t.Fatalf("DisplayName = %q, want unchanged GPT-4o", canonical.DisplayName)
	}
}

func TestSyncOfficialDisplayNameKeepsCanonicalWhenRepositoryUpdateFails(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("catalog canonical lookup failed")
	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	service := &Service{
		capabilities: modelcapabilities.NewWithConfig(modelcapabilities.Config{
			HTTPClient: &http.Client{Transport: catalogSyncRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"openai": {
							"models": {
								"gpt-4o": {"name": "GPT-4o"}
							}
						}
					}`)),
				}, nil
			})},
		}),
	}
	canonical := store.CanonicalModel{
		ID:          uuid.New(),
		ModelKey:    "gpt-4o",
		DisplayName: "gpt-4o",
		Provider:    "openai",
		Category:    "chat",
		Status:      "active",
	}

	service.syncOfficialDisplayName(context.Background(), store.NewCanonicalModelRepository(db), &canonical)

	if canonical.DisplayName != "gpt-4o" {
		t.Fatalf("DisplayName = %q, want unchanged after repository error", canonical.DisplayName)
	}
}

func TestSyncModelPropagatesRepositoryLookupError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("catalog sync lookup failed")
	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	err := (&SyncService{}).syncModel(context.Background(), store.NewCanonicalModelRepository(db), "openai", "gpt-4o", modelsDevSyncModel{
		Name:       "GPT-4o",
		Cost:       map[string]any{"input": 2.5, "output": 10.0, "cache_read": 0.625, "cache_write": 1.25},
		Limit:      map[string]any{"context": 128000, "output": 16384},
		Modalities: map[string][]string{"input": {"text", "image"}, "output": {"text"}},
	})
	if !errors.Is(err, queryErr) {
		t.Fatalf("syncModel error = %v, want repository lookup error", err)
	}
}
