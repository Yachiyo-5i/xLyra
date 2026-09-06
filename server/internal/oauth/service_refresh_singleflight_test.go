package oauth

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

// Concurrent refreshes of the same connection must collapse into a single
// upstream token call. Codex/Antigravity rotate refresh_token on refresh, so a
// second concurrent refresh would present an invalidated token, get
// invalid_grant, and wrongly disable a healthy site. (F11 regression.)
func TestRefreshCodexConnectionCollapsesConcurrentRefreshes(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	bootstrap := NewService(nil, "master-key")
	encryptedRefresh, _, err := bootstrap.credentials.Encrypt("refresh-token")
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}
	connection := store.OAuthConnection{
		ID:                    connectionID,
		Provider:              codexProvider,
		Status:                "connected",
		EncryptedRefreshToken: encryptedRefresh,
		Metadata:              store.JSON(`{"token_mode":"oauth_refresh"}`),
	}
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*store.OAuthConnection); ok {
			*item = connection
			tx.Statement.RowsAffected = 1
			return
		}
		// Site lookup on the disable path: report not found so it exits early.
		tx.Statement.RowsAffected = 0
	}, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	})

	var calls int32
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		// Hold the flight open long enough for the other callers to join.
		time.Sleep(40 * time.Millisecond)
		return oauthHTTPResponse(http.StatusUnauthorized, ` invalid_grant `), nil
	})}

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = service.RefreshCodexConnection(context.Background(), connectionID)
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("concurrent refresh hit the token endpoint %d times, want 1 (singleflight dedup)", got)
	}
	pool, ok := service.db.DB().Config.ConnPool.(*oauthTestConnPool)
	if !ok {
		t.Fatal("refresh did not use the test transaction pool")
	}
	if got := pool.begins.Load(); got != 1 {
		t.Fatalf("refresh transactions = %d, want exactly one transaction", got)
	}
}

// A failed token refresh must persist the connection's reconnect_required
// state, not lose it to a transaction rollback. The old flow returned the
// refresh error from inside WithTransaction, so GORM rolled back the status
// write while the site disable (committed on autocommit) survived, leaving the
// site disabled but the connection still marked connected.
func TestRefreshCodexConnectionFailureCommitsReconnectStatus(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	bootstrap := NewService(nil, "master-key")
	encryptedRefresh, _, err := bootstrap.credentials.Encrypt("refresh-token")
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}
	connection := store.OAuthConnection{
		ID:                    connectionID,
		Provider:              codexProvider,
		Status:                "connected",
		EncryptedRefreshToken: encryptedRefresh,
	}
	var saved *store.OAuthConnection
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*store.OAuthConnection); ok {
			*item = connection
			tx.Statement.RowsAffected = 1
			return
		}
		tx.Statement.RowsAffected = 0
	}, func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*store.OAuthConnection); ok {
			copy := *item
			saved = &copy
		}
		tx.Statement.RowsAffected = 1
	})
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return oauthHTTPResponse(http.StatusUnauthorized, ` invalid_grant `), nil
	})}

	if _, err := service.RefreshCodexConnection(context.Background(), connectionID); err == nil {
		t.Fatal("RefreshCodexConnection returned nil error, want refresh failure")
	}

	pool, ok := service.db.DB().Config.ConnPool.(*oauthTestConnPool)
	if !ok {
		t.Fatal("refresh did not use the test transaction pool")
	}
	if got := pool.rollbacks.Load(); got != 0 {
		t.Fatalf("refresh rollbacks = %d, want 0 (failure state must commit, not roll back)", got)
	}
	if got := pool.commits.Load(); got != 1 {
		t.Fatalf("refresh commits = %d, want 1", got)
	}
	if saved == nil || saved.Status != "reconnect_required" {
		t.Fatalf("saved connection = %#v, want reconnect_required persisted", saved)
	}
}

func TestRefreshCodexConnectionDoesNotRetryAfterWaitedRefreshFailure(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	connection := store.OAuthConnection{
		ID:                connectionID,
		Provider:          codexProvider,
		Status:            "connected",
		RefreshLeaseID:    "another-refresh",
		RefreshLeaseUntil: sql.NullTime{Time: time.Now().Add(100 * time.Millisecond), Valid: true},
		Metadata:          store.JSON(`{"token_mode":"oauth_refresh"}`),
	}
	var queries atomic.Int32
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			t.Fatalf("unexpected refresh query destination %T", tx.Statement.Dest)
		}
		if queries.Add(1) == 1 {
			*item = connection
		} else {
			*item = connection
			item.Status = "reconnect_required"
			item.RefreshLeaseID = ""
			item.RefreshLeaseUntil = sql.NullTime{}
			item.Metadata = store.JSON(`{"token_mode":"oauth_refresh","last_error":"codex token refresh returned 401: invalid_grant"}`)
		}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	})

	var calls atomic.Int32
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return oauthHTTPResponse(http.StatusUnauthorized, ` invalid_grant `), nil
	})}

	_, err := service.RefreshCodexConnection(context.Background(), connectionID)
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("RefreshCodexConnection error = %v, want stored refresh failure", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("refresh retried upstream %d times after waited failure, want 0", calls.Load())
	}
	if queries.Load() < 2 {
		t.Fatalf("refresh queries = %d, want lease wait followed by failure-state read", queries.Load())
	}
}
