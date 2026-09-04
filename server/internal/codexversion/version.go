// Package codexversion tracks the latest stable Codex CLI version as
// published to the npm registry under @openai/codex. The real Codex client
// gates which models the /codex/models endpoint returns by the client_version
// it sends, so the proxy needs to follow that version instead of pinning a
// compile-time constant.
//
// The value is seeded with DefaultVersion and refreshed by an external caller
// (the scheduler). A failed refresh keeps the last known good version rather
// than reverting to the default.
package codexversion

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultVersion is the seed used until the first successful refresh. It
// matches the latest stable release at the time this was written.
const DefaultVersion = "0.153.3"

// registryURL is the npm registry endpoint for the @openai/codex package.
// The scoped package name must be encoded as @openai%2Fcodex.
const registryURL = "https://registry.npmjs.org/@openai%2Fcodex"

// Fetcher resolves the latest stable version string for the Codex CLI.
type Fetcher func(ctx context.Context) (string, error)

var versionRegexp = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type state struct {
	mu      sync.RWMutex
	current string
	fetcher Fetcher
}

var defaultState = &state{
	current: DefaultVersion,
	fetcher: fetchFromNPM,
}

// WithFetcher replaces the default upstream fetcher (primarily for tests) and
// resets the current value to the default. It returns the previous fetcher.
func WithFetcher(f Fetcher) func() {
	defaultState.mu.Lock()
	defer defaultState.mu.Unlock()
	previous := defaultState.fetcher
	defaultState.fetcher = f
	defaultState.current = DefaultVersion
	return func() {
		defaultState.mu.Lock()
		defer defaultState.mu.Unlock()
		defaultState.fetcher = previous
		defaultState.current = DefaultVersion
	}
}

// Version returns the last known good Codex CLI version. It is always a
// non-empty, validated version string; before the first successful refresh it
// returns DefaultVersion.
func Version() string {
	defaultState.mu.RLock()
	defer defaultState.mu.RUnlock()
	return defaultState.current
}

// Refresh fetches the latest version and stores it. On failure the current
// version is left untouched and the error is returned so callers can surface
// the degradation.
func Refresh(ctx context.Context) error {
	defaultState.mu.RLock()
	fetcher := defaultState.fetcher
	defaultState.mu.RUnlock()

	next, err := fetcher(ctx)
	if err != nil {
		return err
	}
	next = normalizeVersion(next)
	if next == "" {
		return fmt.Errorf("registry returned an invalid codex version")
	}

	defaultState.mu.Lock()
	defaultState.current = next
	defaultState.mu.Unlock()
	return nil
}

// normalizeVersion trims surrounding whitespace, strips a leading "rust-"
// prefix (used by the GitHub releases tag) and blank-ish values, and validates
// that the result is a dotted numeric version. It returns "" for any input that
// does not look like a Codex CLI version so callers can reject it.
func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "rust-")
	value = strings.TrimSpace(value)
	if !versionRegexp.MatchString(value) {
		return ""
	}
	return value
}

// fetchFromNPM queries the npm registry and returns the latest stable
// distribution tag for @openai/codex.
func fetchFromNPM(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, nil)
	if err != nil {
		return "", fmt.Errorf("create codex version request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xlyra-codex-version")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch codex version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("npm registry returned %d", resp.StatusCode)
	}

	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode codex version payload: %w", err)
	}
	return payload.DistTags["latest"], nil
}
