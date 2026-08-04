package scheduler

import (
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"gorm.io/gorm"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/site"
)

func TestSiteJobsReleaseGuardsAfterRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("scheduler site query stopped")
	siteService := schedulerSiteServiceFailingQueries(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	})

	tests := []struct {
		name       string
		run        func(*Scheduler)
		guardValue func(*Scheduler) bool
	}{
		{
			name: "site health",
			run: func(scheduler *Scheduler) {
				scheduler.runSiteHealthChecks()
			},
			guardValue: func(scheduler *Scheduler) bool {
				return scheduler.running.Load()
			},
		},
		{
			name: "configured site refresh",
			run: func(scheduler *Scheduler) {
				scheduler.runConfiguredSiteRefresh()
			},
			guardValue: func(scheduler *Scheduler) bool {
				return scheduler.refreshing.Load()
			},
		},
		{
			name: "configured newapi checkins",
			run: func(scheduler *Scheduler) {
				scheduler.runConfiguredNewAPICheckins()
			},
			guardValue: func(scheduler *Scheduler) bool {
				return scheduler.checkingIn.Load()
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheduler := New(schedulerDiscardLogger(), Options{}, siteService, nil, nil)
			tt.run(scheduler)

			if tt.guardValue(scheduler) {
				t.Fatalf("%s guard should be released after repository error", tt.name)
			}
		})
	}
}

func TestModelsDevSyncReleasesGuardAfterRepositoryWriteError(t *testing.T) {
	t.Parallel()

	writeErr := errors.New("scheduler catalog write stopped")
	db := schedulerPostgresGorm(t)
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.AddError(writeErr)
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}

	syncService := catalog.NewSyncService(
		schedulerStoreWithGorm(t, db),
		schedulerDiscardLogger(),
	)
	schedulerSetSyncClient(t, syncService, &http.Client{
		Transport: schedulerCatalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"openai":{"models":{"gpt-sync":{"id":"gpt-sync","name":"GPT Sync"}}}}`)),
			}, nil
		}),
	})

	scheduler := New(schedulerDiscardLogger(), Options{}, nil, syncService, nil)
	scheduler.runModelsDevSync()

	if scheduler.syncing.Load() {
		t.Fatal("models.dev sync guard should be released after repository write error")
	}
}

func TestModelsDevSyncInvalidatesModelsCacheAfterSuccess(t *testing.T) {
	t.Parallel()

	db := schedulerPostgresGorm(t)
	if err := db.Callback().Query().Replace("gorm:query", func(*gorm.DB) {}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	syncService := catalog.NewSyncService(schedulerStoreWithGorm(t, db), schedulerDiscardLogger())
	schedulerSetSyncClient(t, syncService, &http.Client{
		Transport: schedulerCatalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"openai":{"models":{}}}`
			if strings.Contains(req.URL.String(), "model-price-repo") {
				body = `{"ignored":{"litellm_provider":"bedrock"}}`
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	})

	invalidated := false
	scheduler := New(schedulerDiscardLogger(), Options{}, nil, syncService, nil).WithModelsCacheInvalidator(func() {
		invalidated = true
	})
	scheduler.runModelsDevSync()
	if !invalidated {
		t.Fatal("models cache was not invalidated after successful sync")
	}
}

type schedulerCatalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn schedulerCatalogRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func schedulerSiteServiceFailingQueries(t *testing.T, callback func(*gorm.DB)) *site.Service {
	t.Helper()

	db := schedulerPostgresGorm(t)
	if err := db.Callback().Query().Replace("gorm:query", callback); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	return site.NewService(schedulerStoreWithGorm(t, db), schedulerTestMasterKey)
}

func schedulerSetSyncClient(t *testing.T, service *catalog.SyncService, client *http.Client) {
	t.Helper()

	field := reflect.ValueOf(service).Elem().FieldByName("client")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(client))
}
