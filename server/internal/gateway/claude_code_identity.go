package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

const (
	// claudeCodeIdentityTTL is the sliding lifetime of an in-memory account
	// fingerprint. Every use refreshes it, so active accounts keep a stable
	// identity while abandoned ones are eventually forgotten.
	claudeCodeIdentityTTL = 7 * 24 * time.Hour
	// claudeCodeStickySessionTTL is how long a generated Claude Code session id
	// is reused for an account when the downstream client did not provide one.
	claudeCodeStickySessionTTL = 15 * time.Minute
	// claudeCodeIdentitySweepInterval bounds how often expired entries are purged.
	claudeCodeIdentitySweepInterval = 10 * time.Minute

	claudeCodeCLIUserAgentPrefix = "claude-cli/"
)

// claudeCodeFingerprint is the per-account client identity presented to the
// upstream. It mirrors what a single installed Claude Code CLI would expose:
// a stable device id plus the Stainless SDK runtime description.
type claudeCodeFingerprint struct {
	ClientID                string
	StainlessLang           string
	StainlessPackageVersion string
	StainlessOS             string
	StainlessArch           string
	StainlessRuntime        string
	StainlessRuntimeVersion string
}

func defaultClaudeCodeFingerprint() claudeCodeFingerprint {
	return claudeCodeFingerprint{
		StainlessLang:           "js",
		StainlessPackageVersion: "0.94.0",
		StainlessOS:             "Linux",
		StainlessArch:           "arm64",
		StainlessRuntime:        "node",
		StainlessRuntimeVersion: "v24.3.0",
	}
}

type claudeCodeIdentityEntry struct {
	fingerprint claudeCodeFingerprint
	expiresAt   time.Time
}

type claudeCodeSessionEntry struct {
	sessionID string
	expiresAt time.Time
}

// claudeCodeIdentityStore keeps account-level Claude Code identities in process
// memory. Identities are lost on restart, which simply produces a fresh device.
type claudeCodeIdentityStore struct {
	mu           sync.Mutex
	now          func() time.Time
	fingerprints map[string]claudeCodeIdentityEntry
	sessions     map[string]claudeCodeSessionEntry
	lastSweep    time.Time
}

func newClaudeCodeIdentityStore() *claudeCodeIdentityStore {
	return &claudeCodeIdentityStore{
		now:          time.Now,
		fingerprints: map[string]claudeCodeIdentityEntry{},
		sessions:     map[string]claudeCodeSessionEntry{},
	}
}

var claudeCodeIdentities = newClaudeCodeIdentityStore()

// claudeCodeIdentityKey identifies the upstream account behind a candidate.
func claudeCodeIdentityKey(candidate routeengine.Candidate) string {
	key := candidate.Site.ID.String()
	if candidate.Credential.ID != nil {
		key += ":" + candidate.Credential.ID.String()
	}
	return key
}

// Fingerprint returns the stable fingerprint for key, creating it on first use.
// When the first request comes from a genuine Claude Code client its runtime
// headers are adopted so the account looks consistent with that client.
func (s *claudeCodeIdentityStore) Fingerprint(key string, downstream http.Header) claudeCodeFingerprint {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)

	if entry, ok := s.fingerprints[key]; ok && now.Before(entry.expiresAt) {
		entry.expiresAt = now.Add(claudeCodeIdentityTTL)
		s.fingerprints[key] = entry
		return entry.fingerprint
	}

	fp := defaultClaudeCodeFingerprint()
	if isClaudeCodeCLIUserAgent(downstream) {
		fp.StainlessLang = headerOrDefault(downstream, "X-Stainless-Lang", fp.StainlessLang)
		fp.StainlessPackageVersion = headerOrDefault(downstream, "X-Stainless-Package-Version", fp.StainlessPackageVersion)
		fp.StainlessOS = headerOrDefault(downstream, "X-Stainless-OS", fp.StainlessOS)
		fp.StainlessArch = headerOrDefault(downstream, "X-Stainless-Arch", fp.StainlessArch)
		fp.StainlessRuntime = headerOrDefault(downstream, "X-Stainless-Runtime", fp.StainlessRuntime)
		fp.StainlessRuntimeVersion = headerOrDefault(downstream, "X-Stainless-Runtime-Version", fp.StainlessRuntimeVersion)
	}
	fp.ClientID = newClaudeCodeClientID()
	s.fingerprints[key] = claudeCodeIdentityEntry{fingerprint: fp, expiresAt: now.Add(claudeCodeIdentityTTL)}
	return fp
}

// StickySession returns a session id reused for key until it has been idle for
// claudeCodeStickySessionTTL.
func (s *claudeCodeIdentityStore) StickySession(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.sweepLocked(now)

	entry, ok := s.sessions[key]
	if !ok || !now.Before(entry.expiresAt) {
		entry = claudeCodeSessionEntry{sessionID: uuid.NewString()}
	}
	entry.expiresAt = now.Add(claudeCodeStickySessionTTL)
	s.sessions[key] = entry
	return entry.sessionID
}

func (s *claudeCodeIdentityStore) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < claudeCodeIdentitySweepInterval {
		return
	}
	s.lastSweep = now
	for key, entry := range s.fingerprints {
		if !now.Before(entry.expiresAt) {
			delete(s.fingerprints, key)
		}
	}
	for key, entry := range s.sessions {
		if !now.Before(entry.expiresAt) {
			delete(s.sessions, key)
		}
	}
}

func newClaudeCodeClientID() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to two UUIDs worth of entropy; still 64 hex characters.
		return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	}
	return hex.EncodeToString(buf)
}

func isClaudeCodeCLIUserAgent(header http.Header) bool {
	if header == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(header.Get("User-Agent"))), claudeCodeCLIUserAgentPrefix)
}

func headerOrDefault(header http.Header, key, fallback string) string {
	if header == nil {
		return fallback
	}
	if value := strings.TrimSpace(header.Get(key)); value != "" {
		return value
	}
	return fallback
}
