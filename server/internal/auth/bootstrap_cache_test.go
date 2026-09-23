package auth

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

func TestWarmBootstrapCacheServesLaterStatusWithoutAnotherCount(t *testing.T) {
	t.Parallel()

	queries := 0
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		queries++
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(fmt.Errorf("unexpected bootstrap destination %T", tx.Statement.Dest))
			return
		}
		*count = 2
		tx.Statement.RowsAffected = 1
	})

	if err := service.WarmBootstrapCache(context.Background()); err != nil {
		t.Fatalf("WarmBootstrapCache: %v", err)
	}
	warmQueries := queries
	if warmQueries == 0 {
		t.Fatal("warmup did not count admins")
	}

	status, err := service.BootstrapStatus(context.Background())
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if !status.Initialized || status.AdminCount != 2 {
		t.Fatalf("cached status = %#v, want initialized count 2", status)
	}
	if queries != warmQueries {
		t.Fatalf("status queries = %d, want cached warmup count %d", queries, warmQueries)
	}

	initialized, ok := service.CachedBootstrapInitialized()
	if !ok || !initialized {
		t.Fatalf("CachedBootstrapInitialized = %v, %v", initialized, ok)
	}
}

func TestBootstrapStatusCachesTheFirstCount(t *testing.T) {
	t.Parallel()

	queries := 0
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		queries++
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(fmt.Errorf("unexpected bootstrap destination %T", tx.Statement.Dest))
			return
		}
		*count = 0
		tx.Statement.RowsAffected = 1
	})

	status, err := service.BootstrapStatus(context.Background())
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if status.Initialized || status.AdminCount != 0 {
		t.Fatalf("status = %#v, want uninitialized", status)
	}
	firstQueries := queries
	if _, err := service.BootstrapStatus(context.Background()); err != nil {
		t.Fatalf("second BootstrapStatus: %v", err)
	}
	if queries != firstQueries {
		t.Fatalf("second status re-counted admins: queries %d, want %d", queries, firstQueries)
	}

	service.MarkBootstrapInitialized()
	status, err = service.BootstrapStatus(context.Background())
	if err != nil {
		t.Fatalf("BootstrapStatus after mark: %v", err)
	}
	if !status.Initialized || status.AdminCount != 1 || queries != firstQueries {
		t.Fatalf("marked status = %#v queries %d, want initialized count 1 without a new query", status, queries)
	}
}

func TestInFlightBootstrapCountCannotClearInitializedMark(t *testing.T) {
	t.Parallel()

	var service *Service
	service = authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		service.MarkBootstrapInitialized()
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(fmt.Errorf("unexpected bootstrap destination %T", tx.Statement.Dest))
			return
		}
		*count = 0
		tx.Statement.RowsAffected = 1
	})

	status, err := service.BootstrapStatus(context.Background())
	if err != nil {
		t.Fatalf("BootstrapStatus: %v", err)
	}
	if !status.Initialized || status.AdminCount != 1 {
		t.Fatalf("status = %#v, want the mark to win over the stale zero count", status)
	}
	again, err := service.BootstrapStatus(context.Background())
	if err != nil {
		t.Fatalf("second BootstrapStatus: %v", err)
	}
	if !again.Initialized || again.AdminCount != 1 {
		t.Fatalf("cached status = %#v, want the mark to stay initialized", again)
	}
}

func TestRefreshBootstrapCacheFollowsAdminCountInBothDirections(t *testing.T) {
	t.Parallel()

	var admins atomic.Int64
	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(fmt.Errorf("unexpected bootstrap destination %T", tx.Statement.Dest))
			return
		}
		*count = admins.Load()
		tx.Statement.RowsAffected = 1
	})

	if err := service.WarmBootstrapCache(context.Background()); err != nil {
		t.Fatalf("WarmBootstrapCache: %v", err)
	}
	status, err := service.BootstrapStatus(context.Background())
	if err != nil || status.Initialized || status.AdminCount != 0 {
		t.Fatalf("initial status = %#v, %v; want uninitialized", status, err)
	}

	admins.Store(1)
	if err := service.RefreshBootstrapCache(context.Background()); err != nil {
		t.Fatalf("RefreshBootstrapCache to one admin: %v", err)
	}
	status, err = service.BootstrapStatus(context.Background())
	if err != nil || !status.Initialized || status.AdminCount != 1 {
		t.Fatalf("status after restore with an admin = %#v, %v", status, err)
	}

	admins.Store(0)
	if err := service.RefreshBootstrapCache(context.Background()); err != nil {
		t.Fatalf("RefreshBootstrapCache to zero admins: %v", err)
	}
	status, err = service.BootstrapStatus(context.Background())
	if err != nil || status.Initialized || status.AdminCount != 0 {
		t.Fatalf("status after restore without admins = %#v, %v", status, err)
	}
}
