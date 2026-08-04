package oauth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestCallbackRelaysTrimStateBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		target  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{
			name:    "codex",
			target:  "/auth/callback",
			handler: (&codexCallbackRelay{}).handleCallback,
		},
		{
			name:    "antigravity",
			target:  "/oauth/antigravity/callback",
			handler: (&antigravityCallbackRelay{}).handleCallback,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertOAuthRelayResponse(t, tt.handler, tt.target+"?state=%20%20", http.StatusBadRequest, "state is required")
			assertOAuthRelayResponse(t, tt.handler, tt.target+"?state=%20oauth-state%20", http.StatusServiceUnavailable, "oauth store is not available")
		})
	}
}

func TestCallbackRelaysAlreadyStartedSkipListener(t *testing.T) {
	fakeCodexStore := &store.Store{}
	codexRelay.mu.Lock()
	originalCodexDB := codexRelay.db
	originalCodexServer := codexRelay.server
	originalCodexStarted := codexRelay.started
	codexRelay.db = nil
	codexRelay.server = nil
	codexRelay.started = true
	codexRelay.mu.Unlock()
	defer func() {
		codexRelay.mu.Lock()
		codexRelay.db = originalCodexDB
		codexRelay.server = originalCodexServer
		codexRelay.started = originalCodexStarted
		codexRelay.mu.Unlock()
	}()

	if err := ensureCodexCallbackRelay(fakeCodexStore); err != nil {
		t.Fatalf("ensureCodexCallbackRelay already started: %v", err)
	}
	codexRelay.mu.RLock()
	gotCodexDB := codexRelay.db
	gotCodexServer := codexRelay.server
	gotCodexStarted := codexRelay.started
	codexRelay.mu.RUnlock()
	if gotCodexDB != fakeCodexStore || gotCodexServer != nil || !gotCodexStarted {
		t.Fatalf("codex relay state = db:%p server:%p started:%v", gotCodexDB, gotCodexServer, gotCodexStarted)
	}

	fakeAntigravityStore := &store.Store{}
	antigravityRelay.mu.Lock()
	originalAntigravityDB := antigravityRelay.db
	originalAntigravityServer := antigravityRelay.server
	originalAntigravityStarted := antigravityRelay.started
	antigravityRelay.db = nil
	antigravityRelay.server = nil
	antigravityRelay.started = true
	antigravityRelay.mu.Unlock()
	defer func() {
		antigravityRelay.mu.Lock()
		antigravityRelay.db = originalAntigravityDB
		antigravityRelay.server = originalAntigravityServer
		antigravityRelay.started = originalAntigravityStarted
		antigravityRelay.mu.Unlock()
	}()

	if err := ensureAntigravityCallbackRelay(fakeAntigravityStore); err != nil {
		t.Fatalf("ensureAntigravityCallbackRelay already started: %v", err)
	}
	antigravityRelay.mu.RLock()
	gotAntigravityDB := antigravityRelay.db
	gotAntigravityServer := antigravityRelay.server
	gotAntigravityStarted := antigravityRelay.started
	antigravityRelay.mu.RUnlock()
	if gotAntigravityDB != fakeAntigravityStore || gotAntigravityServer != nil || !gotAntigravityStarted {
		t.Fatalf("antigravity relay state = db:%p server:%p started:%v", gotAntigravityDB, gotAntigravityServer, gotAntigravityStarted)
	}
}

func TestImportFallbackMetadataCompleteCases(t *testing.T) {
	t.Parallel()

	if missing := missingFlatTokenFallbackMetadata(" user@example.com ", " acct ", " plus ", " user_123 ", time.Unix(1770000000, 0)); len(missing) != 0 {
		t.Fatalf("complete flat fallback metadata missing fields: %v", missing)
	}

	if missing := missingSub2APIFallbackMetadata(Sub2APIAccount{}, " user@example.com ", " acct_123 "); len(missing) != 0 {
		t.Fatalf("complete Sub2API fallback metadata missing fields: %v", missing)
	}
}

func TestImportRefreshHelpersAvoidExternalRefresh(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil)
	if service.refreshImportedConnection(context.Background(), uuid.New()) {
		t.Fatal("nil oauth service should not refresh imported connections")
	}
	if service.refreshImportedConnection(context.Background(), uuid.Nil) {
		t.Fatal("nil connection id should not refresh imported connections")
	}

	applyImportTokenMetadata(nil, "refresh-token")
}
