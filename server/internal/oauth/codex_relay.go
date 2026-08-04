package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"xlyra/server/internal/store"
)

type codexCallbackRelay struct {
	mu      sync.RWMutex
	db      *store.Store
	server  *http.Server
	started bool
}

var codexRelay codexCallbackRelay

func ensureCodexCallbackRelay(db *store.Store) error {
	if db == nil {
		return fmt.Errorf("oauth store is not available")
	}
	codexRelay.mu.Lock()
	defer codexRelay.mu.Unlock()
	codexRelay.db = db
	if codexRelay.started {
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", codexRelay.handleCallback)
	mux.HandleFunc("/success", codexRelay.handleSuccess)

	addr := fmt.Sprintf(":%d", codexCallbackPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("codex oauth callback relay failed to listen on localhost:%d: %w", codexCallbackPort, err)
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	codexRelay.server = server
	codexRelay.started = true

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			codexRelay.mu.Lock()
			codexRelay.started = false
			codexRelay.server = nil
			codexRelay.mu.Unlock()
		}
	}()
	return nil
}

func (r *codexCallbackRelay) handleCallback(w http.ResponseWriter, req *http.Request) {
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

func (r *codexCallbackRelay) handleSuccess(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(codexSuccessHTML))
}

func (r *codexCallbackRelay) store() *store.Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.db
}

func relayTargetURL(raw store.JSON) string {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	target, _ := meta["relay_target_url"].(string)
	return strings.TrimSpace(target)
}

func withRelayQuery(target string, values url.Values) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid relay target")
	}
	query := parsed.Query()
	for key, vals := range values {
		for _, value := range vals {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
