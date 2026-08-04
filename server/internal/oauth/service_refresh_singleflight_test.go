package oauth

import (
	"context"
	"net/http"
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
}
