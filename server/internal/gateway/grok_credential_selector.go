package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func isGrokSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "grok")
}

func grokCredentialCandidates(credentials []store.GatewayCredential, states []store.SiteAPIKeyState) []grokCredentialCandidate {
	statesByCredentialID := make(map[uuid.UUID]store.SiteAPIKeyState, len(states))
	for _, state := range states {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	result := make([]grokCredentialCandidate, 0, len(credentials))
	for _, credential := range credentials {
		if !strings.HasPrefix(credential.Credential.CredentialType, "grok_sso:") {
			continue
		}
		state, hasState := statesByCredentialID[credential.Credential.ID]
		if !grokCredentialUsable(credential.Credential, state, hasState) {
			continue
		}
		remainingQuota := int64(0)
		if state.UnlimitedQuota {
			remainingQuota = int64(^uint64(0) >> 1)
		} else if state.RemainQuota.Valid {
			remainingQuota = state.RemainQuota.Int64
		}
		result = append(result, grokCredentialCandidate{Credential: credential, RemainingQuota: remainingQuota})
	}
	return result
}

func grokCredentialUsable(credential store.SiteCredential, state store.SiteAPIKeyState, hasState bool) bool {
	if !gatewayCredentialUsable(credential) {
		return false
	}
	if !hasState {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(state.SyncStatus))
	if !state.Enabled || status == "failed" || status == "reauth_required" {
		return false
	}
	return true
}

func shouldMarkGrokCredentialSyncFailed(result gatewayAttemptResult) bool {
	if result.errorType != "upstream_http_error" {
		return false
	}
	return result.upstreamStatusCode == http.StatusUnauthorized
}

func (h Handler) markGrokCredentialSyncFailed(ctx context.Context, credential store.SiteCredential, result gatewayAttemptResult) {
	if h.db == nil || credential.ID == uuid.Nil || !shouldMarkGrokCredentialSyncFailed(result) {
		return
	}
	message := fmt.Sprintf("grok upstream returned status %d", result.upstreamStatusCode)
	h.markGrokCredentialReauthRequired(ctx, credential, message)
}

func (h Handler) markGrokCredentialReauthRequired(ctx context.Context, credential store.SiteCredential, message string) {
	if h.db == nil || credential.ID == uuid.Nil {
		return
	}
	err := h.db.WithinTx(ctx, func(tx store.Tx) error {
		meta, err := disabledGrokCredentialMeta(credential.Meta)
		if err != nil {
			return err
		}
		if _, err := store.NewSiteCredentialRepository(tx).UpdateMeta(ctx, credential.ID, meta); err != nil {
			return err
		}
		state, err := store.NewSiteAPIKeyStateRepository(tx).UpsertSyncResult(ctx, store.UpsertSiteAPIKeyStateParams{
			SiteCredentialID: credential.ID,
			SiteID:           credential.SiteID,
			Enabled:          false,
			SyncStatus:       "reauth_required",
			SyncMessage:      message,
			LastSyncedAt:     time.Now(),
		})
		if err != nil {
			return err
		}
		if state.Enabled {
			_, err = store.NewSiteAPIKeyStateRepository(tx).UpdateEnabled(ctx, credential.ID, false)
		}
		return err
	})
	if err != nil && h.logger != nil {
		h.logger.WarnContext(ctx, "failed to update grok credential sync state", "scope", "gateway", "site_id", credential.SiteID, "site_credential_id", credential.ID, "error", err)
	}
}

func disabledGrokCredentialMeta(raw store.JSON) (store.JSON, error) {
	meta := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("decode grok credential meta: %w", err)
		}
	}
	meta["enabled"] = false
	meta["reauth_required"] = true
	encoded, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("encode grok credential meta: %w", err)
	}
	return encoded, nil
}

type grokCredentialCandidate struct {
	Credential     store.GatewayCredential
	RemainingQuota int64
}

type credentialWindowState struct {
	attempts     []time.Time
	inflight     int
	lastSelected time.Time
}

type credentialWindowSelector struct {
	mu     sync.Mutex
	window time.Duration
	now    func() time.Time
	states map[uuid.UUID]*credentialWindowState
}

func newCredentialWindowSelector(window time.Duration) *credentialWindowSelector {
	return &credentialWindowSelector{
		window: window,
		now:    time.Now,
		states: map[uuid.UUID]*credentialWindowState{},
	}
}

func (s *credentialWindowSelector) selectCredential(candidates []grokCredentialCandidate) (grokCredentialCandidate, func(), bool) {
	if s == nil || len(candidates) == 0 {
		return grokCredentialCandidate{}, func() {}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	cutoff := now.Add(-s.window)
	for id, state := range s.states {
		state.attempts = recentCredentialAttempts(state.attempts, cutoff)
		if len(state.attempts) == 0 && state.inflight == 0 && !state.lastSelected.IsZero() && !state.lastSelected.After(cutoff) {
			delete(s.states, id)
		}
	}
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if s.prefer(candidate, best) {
			best = candidate
		}
	}
	id := best.Credential.Credential.ID
	state := s.state(id)
	state.attempts = append(state.attempts, now)
	state.inflight++
	state.lastSelected = now
	var once sync.Once
	release := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if current := s.states[id]; current != nil && current.inflight > 0 {
				current.inflight--
			}
		})
	}
	return best, release, true
}

func (s *credentialWindowSelector) prefer(left, right grokCredentialCandidate) bool {
	leftState := s.state(left.Credential.Credential.ID)
	rightState := s.state(right.Credential.Credential.ID)
	if len(leftState.attempts) != len(rightState.attempts) {
		return len(leftState.attempts) < len(rightState.attempts)
	}
	if leftState.inflight != rightState.inflight {
		return leftState.inflight < rightState.inflight
	}
	if left.RemainingQuota != right.RemainingQuota {
		return left.RemainingQuota > right.RemainingQuota
	}
	if !leftState.lastSelected.Equal(rightState.lastSelected) {
		return leftState.lastSelected.Before(rightState.lastSelected)
	}
	return left.Credential.Credential.ID.String() < right.Credential.Credential.ID.String()
}

func (s *credentialWindowSelector) state(id uuid.UUID) *credentialWindowState {
	state := s.states[id]
	if state == nil {
		state = &credentialWindowState{}
		s.states[id] = state
	}
	return state
}

func recentCredentialAttempts(attempts []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(attempts) && !attempts[first].After(cutoff) {
		first++
	}
	if first == len(attempts) {
		return nil
	}
	return attempts[first:]
}
