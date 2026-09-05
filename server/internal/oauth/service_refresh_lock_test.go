package oauth

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestRefreshConnectionDistributedLockSerializesIndependentDatabaseSessions(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := devPostgresOAuthSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	firstStore, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("first PostgreSQL session unavailable: %v", redactOAuthDatabaseOpenError(err, cfg))
	}
	defer firstStore.Close()
	secondStore, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("second PostgreSQL session unavailable: %v", redactOAuthDatabaseOpenError(err, cfg))
	}
	defer secondStore.Close()
	firstDB, err := firstStore.DB().DB()
	if err != nil {
		t.Fatalf("first SQL database: %v", err)
	}
	secondDB, err := secondStore.DB().DB()
	if err != nil {
		t.Fatalf("second SQL database: %v", err)
	}

	key := "test-xlyra-oauth-refresh-lock"
	firstEntered := make(chan struct{})
	secondEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var wg sync.WaitGroup
	var firstErr, secondErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		firstErr = withPostgresAdvisoryLock(ctx, firstDB, key, func(_ *sql.Conn) error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	select {
	case <-firstEntered:
	case <-ctx.Done():
		t.Fatalf("first lock was not acquired: %v", ctx.Err())
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		secondErr = withPostgresAdvisoryLock(ctx, secondDB, key, func(_ *sql.Conn) error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second independent database session entered while first lock was held")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	wg.Wait()
	if firstErr != nil {
		t.Fatalf("first lock callback: %v", firstErr)
	}
	if secondErr != nil {
		t.Fatalf("second lock callback: %v", secondErr)
	}
}
