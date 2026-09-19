package claudeversion

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

const DefaultVersion = "2.1.278"

const registryURL = "https://registry.npmjs.org/@anthropic-ai%2Fclaude-code"

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

func Version() string {
	defaultState.mu.RLock()
	defer defaultState.mu.RUnlock()
	return defaultState.current
}

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
		return fmt.Errorf("registry returned an invalid claude code version")
	}

	defaultState.mu.Lock()
	defaultState.current = next
	defaultState.mu.Unlock()
	return nil
}

func normalizeVersion(value string) string {
	value = strings.TrimSpace(value)
	if !versionRegexp.MatchString(value) {
		return ""
	}
	return value
}

func fetchFromNPM(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, nil)
	if err != nil {
		return "", fmt.Errorf("create claude code version request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "xlyra-claude-code-version")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch claude code version: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("npm registry returned %d", resp.StatusCode)
	}

	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode claude code version payload: %w", err)
	}
	return payload.DistTags["latest"], nil
}
