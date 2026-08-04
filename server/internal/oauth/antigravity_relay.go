package oauth

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"xlyra/server/internal/store"
)

type antigravityCallbackRelay struct {
	mu      sync.RWMutex
	db      *store.Store
	server  *http.Server
	started bool
}

var antigravityRelay antigravityCallbackRelay

func ensureAntigravityCallbackRelay(db *store.Store) error {
	if db == nil {
		return fmt.Errorf("oauth store is not available")
	}
	antigravityRelay.mu.Lock()
	defer antigravityRelay.mu.Unlock()
	antigravityRelay.db = db
	if antigravityRelay.started {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/antigravity/callback", antigravityRelay.handleCallback)
	mux.HandleFunc("/success", antigravityRelay.handleSuccess)

	addr := fmt.Sprintf(":%d", antigravityCallbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("antigravity oauth callback relay failed to listen on localhost:%d: %w", antigravityCallbackPort, err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	antigravityRelay.server = server
	antigravityRelay.started = true

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			antigravityRelay.mu.Lock()
			antigravityRelay.started = false
			antigravityRelay.server = nil
			antigravityRelay.mu.Unlock()
		}
	}()
	return nil
}

func (r *antigravityCallbackRelay) handleCallback(w http.ResponseWriter, req *http.Request) {
	state := strings.TrimSpace(req.URL.Query().Get("state"))
	if state == "" {
		http.Error(w, "state is required", http.StatusBadRequest)
		return
	}
	db := r.store()
	if db == nil {
		http.Error(w, "oauth store is not available", http.StatusServiceUnavailable)
		return
	}
	session, err := store.NewOAuthSessionRepository(db.DB()).GetByState(req.Context(), state)
	if err != nil {
		http.Error(w, "oauth session was not found", http.StatusBadRequest)
		return
	}
	target := relayTargetURL(session.Metadata)
	if target == "" {
		http.Error(w, "oauth relay target is not available", http.StatusBadRequest)
		return
	}
	redirectURL, err := withRelayQuery(target, req.URL.Query())
	if err != nil {
		http.Error(w, "oauth relay target is invalid", http.StatusBadRequest)
		return
	}
	http.Redirect(w, req, redirectURL, http.StatusFound)
}

func (r *antigravityCallbackRelay) handleSuccess(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(antigravitySuccessHTML))
}

func (r *antigravityCallbackRelay) store() *store.Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}
