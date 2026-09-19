package store

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestAPIKeyOrderRejectsDuplicateAndEmptyIDsBeforeDatabase(t *testing.T) {
	id := uuid.New()
	for _, ids := range [][]uuid.UUID{{id, id}, {uuid.Nil}} {
		if err := (APIKeyRepository{}).Reorder(context.Background(), ids); !errors.Is(err, ErrInvalidAPIKeyOrder) {
			t.Fatalf("invalid IDs returned %v", err)
		}
	}
}

func TestAPIKeyOrderPostgres(t *testing.T) {
	db, _, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	if err := db.Migrator().CreateTable(&schemaUpgradeMarker{}, &APIKey{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := NewAPIKeyRepository(db)
	base := time.Now().Add(-time.Hour)
	legacy := []APIKey{
		{ID: uuid.New(), Name: "disabled", Status: "disabled", KeyHash: "one", CreatedAt: base.Add(-time.Minute)},
		{ID: uuid.New(), Name: "new", Status: "active", KeyHash: "two", CreatedAt: base.Add(time.Minute)},
		{ID: uuid.New(), Name: "old", Status: "active", KeyHash: "three", CreatedAt: base},
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.InitializeOrder(ctx); err != nil {
		t.Fatal(err)
	}
	read := func() []APIKey {
		t.Helper()
		keys, err := repo.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return keys
	}
	ids := func(keys []APIKey) []uuid.UUID {
		result := make([]uuid.UUID, len(keys))
		for i, key := range keys {
			result[i] = key.ID
		}
		return result
	}
	keys := read()
	if !reflect.DeepEqual(ids(keys), []uuid.UUID{legacy[0].ID, legacy[2].ID, legacy[1].ID}) {
		t.Fatalf("legacy ordering = %v", ids(keys))
	}
	desired := []uuid.UUID{legacy[1].ID, legacy[2].ID, legacy[0].ID}
	if err := repo.Reorder(ctx, desired); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(read()), desired) {
		t.Fatal("manual order must take priority over status and creation time")
	}
	if err := repo.InitializeOrder(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(read()), desired) {
		t.Fatal("initialization must preserve saved ranks from a backup or restart")
	}
	options, err := repo.ListOptions(ctx)
	if err != nil || options[0].ID != desired[0] || options[2].ID != desired[2] {
		t.Fatalf("options do not preserve manual order: %v", err)
	}
	for _, invalid := range [][]uuid.UUID{desired[:2], {uuid.New(), desired[1], desired[2]}} {
		if err := repo.Reorder(ctx, invalid); !errors.Is(err, ErrInvalidAPIKeyOrder) {
			t.Fatalf("invalid membership = %v", err)
		}
	}
	updateCount := 0
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail_second_rank", func(tx *gorm.DB) {
		updateCount++
		if updateCount == 2 {
			tx.AddError(errors.New("injected rank failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Reorder(ctx, []uuid.UUID{desired[2], desired[0], desired[1]}); err == nil {
		t.Fatal("expected injected failure")
	}
	if err := db.Callback().Update().Remove("test:fail_second_rank"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids(read()), desired) {
		t.Fatal("failed reorder must roll back every rank")
	}
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	for _, order := range [][]uuid.UUID{{desired[2], desired[0], desired[1]}, {desired[1], desired[2], desired[0]}} {
		go func(order []uuid.UUID) {
			<-start
			errorsOut <- repo.Reorder(ctx, order)
		}(order)
	}
	close(start)
	successes := 0
	for range 2 {
		err := <-errorsOut
		if err == nil {
			successes++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 2 {
		t.Fatalf("concurrent saves: successes=%d", successes)
	}
	beforeCreate := read()
	var wg sync.WaitGroup
	created := make(chan APIKey, 4)
	createErrors := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := repo.Create(ctx, CreateAPIKeyParams{Name: "appended", KeyHash: uuid.NewString()})
			createErrors <- err
			created <- key
		}()
	}
	wg.Wait()
	for range 4 {
		if err := <-createErrors; err != nil {
			t.Fatal(err)
		}
	}
	afterCreate := read()
	if !reflect.DeepEqual(ids(afterCreate[:3]), ids(beforeCreate)) {
		t.Fatal("creation must append without changing existing order")
	}
	for i := 1; i < len(afterCreate); i++ {
		if afterCreate[i].SortOrder <= afterCreate[i-1].SortOrder {
			t.Fatal("concurrent creation must assign distinct increasing ranks")
		}
	}
	if err := repo.Delete(ctx, (<-created).ID); err != nil {
		t.Fatal(err)
	}
}
