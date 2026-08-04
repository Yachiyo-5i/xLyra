package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestStoreNilAndUninitializedGuards(t *testing.T) {
	t.Parallel()

	var nilStore *Store
	if nilStore.DB() != nil {
		t.Fatal("nil store DB should be nil")
	}
	nilStore.Close()

	uninitialized := &Store{}
	if uninitialized.DB() != nil {
		t.Fatal("uninitialized store DB should be nil")
	}
	uninitialized.Close()

	if err := nilStore.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("nil store Ping error = %v, want initialization error", err)
	}
	if err := uninitialized.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("uninitialized store Ping error = %v, want initialization error", err)
	}
}

func TestIsRecordNotFoundRecognizesWrappedGormError(t *testing.T) {
	t.Parallel()

	if !IsRecordNotFound(errors.Join(errors.New("get site"), gorm.ErrRecordNotFound)) {
		t.Fatal("wrapped gorm record-not-found error should be recognized")
	}
	if IsRecordNotFound(errors.New("connection refused")) {
		t.Fatal("unrelated error should not be recognized as record-not-found")
	}
}
