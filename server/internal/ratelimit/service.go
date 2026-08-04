package ratelimit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

const (
	defaultReserveOutputTokens = 4096
	configCacheTTL             = 30 * time.Second
	windowSweepInterval        = time.Minute
	windowExpiry               = 2 * time.Minute
	rpmRetryAfterSeconds       = 5
)

var ErrLimited = errors.New("rate limit exceeded")

type ConfigInput struct {
	Status   string
	RPMLimit *int64
	TPMLimit *int64
}

type Config struct {
	Status   string
	RPMLimit *int64
	TPMLimit *int64
}

type AcquireInput struct {
	APIKeyID    uuid.UUID
	Endpoint    string
	Payload     map[string]any
	EstimateTPM bool
	RequestedAt time.Time
}

type Reservation struct {
	WindowStart     time.Time
	EstimatedTokens int64
	ReservedTokens  int64
	Scopes          []ReservationScope
}

type ReservationScope struct {
	Scope          string
	ScopeKey       string
	RPMLimit       *int64
	TPMLimit       *int64
	RPMUsed        int64
	TPMReserved    int64
	TPMActual      int64
	ReservedTokens int64
	RPMHeld        bool
}

type LimitError struct {
	Scope             string
	ScopeKey          string
	LimitType         string
	Limit             int64
	RetryAfterSeconds int64
	Message           string
}

func (e LimitError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return ErrLimited.Error()
}

func (e LimitError) Is(target error) bool {
	return target == ErrLimited
}

type Service struct {
	db        *store.Store
	mu        sync.Mutex
	windows   map[string]*scopeWindow
	configs   map[string]cachedScopeConfig
	queues    map[string][]*queueWaiter
	lastSweep time.Time
}

type scopeWindow struct {
	windowStart time.Time
	inflight    int64
	tpmReserved int64
	tpmActual   int64
	release     chan struct{}
}

type cachedScopeConfig struct {
	config    activeConfig
	active    bool
	fetchedAt time.Time
}

func NewService(db *store.Store) *Service {
	return &Service{
		db:      db,
		windows: map[string]*scopeWindow{},
		configs: map[string]cachedScopeConfig{},
		queues:  map[string][]*queueWaiter{},
	}
}

func DefaultConfig() Config {
	return Config{Status: store.RateLimitStatusDisabled}
}

func (s *Service) GetGlobal(ctx context.Context) (Config, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return DefaultConfig(), nil
	}
	item, err := store.NewGatewayRateLimitRepository(s.db.DB()).GetGlobal(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return configFromStore(item), nil
}

func (s *Service) SetGlobal(ctx context.Context, input ConfigInput) (Config, error) {
	return s.set(ctx, store.RateLimitScopeGlobal, uuid.Nil, input)
}

func (s *Service) GetAPIKey(ctx context.Context, apiKeyID uuid.UUID) (Config, error) {
	if s == nil || s.db == nil || s.db.DB() == nil || apiKeyID == uuid.Nil {
		return DefaultConfig(), nil
	}
	item, err := store.NewGatewayRateLimitRepository(s.db.DB()).GetByAPIKey(ctx, apiKeyID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return configFromStore(item), nil
}

func (s *Service) SetAPIKey(ctx context.Context, apiKeyID uuid.UUID, input ConfigInput) (Config, error) {
	if apiKeyID == uuid.Nil {
		return Config{}, fmt.Errorf("api_key_id is required")
	}
	return s.set(ctx, store.RateLimitScopeAPIKey, apiKeyID, input)
}

func (s *Service) set(ctx context.Context, scope string, apiKeyID uuid.UUID, input ConfigInput) (Config, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return Config{}, fmt.Errorf("store is not initialized")
	}
	status := normalizeStatus(input.Status)
	if status == "" {
		return Config{}, fmt.Errorf("rate_limit.status must be enabled or disabled")
	}
	if input.RPMLimit != nil && *input.RPMLimit <= 0 {
		return Config{}, fmt.Errorf("rate_limit.rpm_limit must be greater than 0")
	}
	if input.TPMLimit != nil && *input.TPMLimit <= 0 {
		return Config{}, fmt.Errorf("rate_limit.tpm_limit must be greater than 0")
	}
	var apiKeyParam any
	if scope == store.RateLimitScopeAPIKey {
		apiKeyParam = apiKeyID
	}
	item, err := store.NewGatewayRateLimitRepository(s.db.DB()).Upsert(ctx, store.UpsertGatewayRateLimitParams{
		Scope:    scope,
		APIKeyID: apiKeyParam,
		Status:   status,
		RPMLimit: int64PointerAsAny(input.RPMLimit),
		TPMLimit: int64PointerAsAny(input.TPMLimit),
	})
	if err != nil {
		return Config{}, err
	}
	s.invalidateConfig(scope, apiKeyID)
	return configFromStore(item), nil
}

func (s *Service) Acquire(ctx context.Context, input AcquireInput) (*Reservation, error) {
	return s.acquire(ctx, input, "")
}

func (s *Service) acquire(ctx context.Context, input AcquireInput, queueHeadScope string) (*Reservation, error) {
	if s == nil || s.db == nil || s.db.DB() == nil || input.APIKeyID == uuid.Nil {
		return nil, nil
	}
	now := input.RequestedAt
	if now.IsZero() {
		now = time.Now()
	}
	windowStart := now.UTC().Truncate(time.Minute)
	estimatedTokens := int64(0)
	if input.EstimateTPM {
		estimatedTokens = EstimateTokens(input.Payload)
	}

	configs, err := s.activeConfigsCached(ctx, input.APIKeyID, now)
	if err != nil {
		return nil, err
	}

	reservation := &Reservation{
		WindowStart:     windowStart,
		EstimatedTokens: estimatedTokens,
		ReservedTokens:  estimatedTokens,
		Scopes:          []ReservationScope{},
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)

	tpmRetryAfter := int64(math.Ceil(windowStart.Add(time.Minute).Sub(now).Seconds()))
	if tpmRetryAfter < 1 {
		tpmRetryAfter = 1
	}

	type pendingScope struct {
		config   activeConfig
		holdSlot bool
		tpmDelta int64
	}
	pending := make([]pendingScope, 0, len(configs))
	for _, cfg := range configs {
		holdSlot := cfg.RPMLimit != nil
		tpmDelta := int64(0)
		if cfg.TPMLimit != nil {
			tpmDelta = estimatedTokens
		}
		if !holdSlot && tpmDelta == 0 {
			continue
		}
		window := s.windowLocked(cfg.ScopeKey, windowStart)
		if holdSlot {
			queuedAhead := len(s.queues[cfg.ScopeKey]) > 0 && cfg.ScopeKey != queueHeadScope
			if queuedAhead || window.inflight+1 > *cfg.RPMLimit {
				return nil, LimitError{Scope: cfg.Scope, ScopeKey: cfg.ScopeKey, LimitType: "rpm", Limit: *cfg.RPMLimit, RetryAfterSeconds: rpmRetryAfterSeconds}
			}
		}
		if cfg.TPMLimit != nil && estimatedTokens > 0 && window.tpmActual+window.tpmReserved+estimatedTokens > *cfg.TPMLimit {
			return nil, LimitError{Scope: cfg.Scope, ScopeKey: cfg.ScopeKey, LimitType: "tpm", Limit: *cfg.TPMLimit, RetryAfterSeconds: tpmRetryAfter}
		}
		pending = append(pending, pendingScope{config: cfg, holdSlot: holdSlot, tpmDelta: tpmDelta})
	}
	if len(pending) == 0 {
		return nil, nil
	}

	for _, item := range pending {
		window := s.windowLocked(item.config.ScopeKey, windowStart)
		if item.holdSlot {
			window.inflight++
		}
		window.tpmReserved += item.tpmDelta
		reservation.Scopes = append(reservation.Scopes, ReservationScope{
			Scope:          item.config.Scope,
			ScopeKey:       item.config.ScopeKey,
			RPMLimit:       item.config.RPMLimit,
			TPMLimit:       item.config.TPMLimit,
			RPMUsed:        window.inflight,
			TPMReserved:    window.tpmReserved,
			TPMActual:      window.tpmActual,
			ReservedTokens: item.tpmDelta,
			RPMHeld:        item.holdSlot,
		})
	}
	return reservation, nil
}

func (s *Service) Settle(ctx context.Context, reservation *Reservation, actualTokens int64) error {
	if s == nil || reservation == nil || len(reservation.Scopes) == 0 {
		return nil
	}
	if actualTokens < 0 {
		actualTokens = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, scope := range reservation.Scopes {
		window, ok := s.windows[scope.ScopeKey]
		if !ok {
			continue
		}
		if scope.RPMHeld {
			window.inflight--
			if window.inflight < 0 {
				window.inflight = 0
			}
			if window.release != nil {
				select {
				case window.release <- struct{}{}:
				default:
				}
			}
		}
		if scope.ReservedTokens > 0 && window.windowStart.Equal(reservation.WindowStart) {
			window.tpmReserved -= scope.ReservedTokens
			if window.tpmReserved < 0 {
				window.tpmReserved = 0
			}
			window.tpmActual += actualTokens
		}
	}
	return nil
}

func (s *Service) releaseSignal(scopeKey string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	window := s.windows[scopeKey]
	if window == nil {
		window = &scopeWindow{}
		s.windows[scopeKey] = window
	}
	if window.release == nil {
		window.release = make(chan struct{}, 1)
	}
	return window.release
}

func (s *Service) windowLocked(scopeKey string, windowStart time.Time) *scopeWindow {
	window, ok := s.windows[scopeKey]
	if !ok {
		window = &scopeWindow{windowStart: windowStart}
		s.windows[scopeKey] = window
		return window
	}
	if !window.windowStart.Equal(windowStart) {
		window.windowStart = windowStart
		window.tpmReserved = 0
		window.tpmActual = 0
	}
	return window
}

func (s *Service) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < windowSweepInterval {
		return
	}
	s.lastSweep = now
	windowCutoff := now.UTC().Add(-windowExpiry)
	for key, window := range s.windows {
		if window.inflight == 0 && len(s.queues[key]) == 0 && window.windowStart.Before(windowCutoff) {
			delete(s.windows, key)
		}
	}
	configCutoff := now.Add(-configCacheTTL)
	for key, cached := range s.configs {
		if cached.fetchedAt.Before(configCutoff) {
			delete(s.configs, key)
		}
	}
}

func (s *Service) activeConfigsCached(ctx context.Context, apiKeyID uuid.UUID, now time.Time) ([]activeConfig, error) {
	scopeKeys := []string{"global", "api_key:" + apiKeyID.String()}
	result := make([]activeConfig, 0, 2)
	missing := make([]string, 0, 2)

	s.mu.Lock()
	for _, scopeKey := range scopeKeys {
		cached, ok := s.configs[scopeKey]
		if !ok || now.Sub(cached.fetchedAt) > configCacheTTL {
			missing = append(missing, scopeKey)
			continue
		}
		if cached.active {
			result = append(result, cached.config)
		}
	}
	s.mu.Unlock()

	if len(missing) == 0 {
		return result, nil
	}

	repo := store.NewGatewayRateLimitRepository(s.db.DB())
	fetched := make(map[string]cachedScopeConfig, len(missing))
	for _, scopeKey := range missing {
		var item store.GatewayRateLimit
		var err error
		if scopeKey == "global" {
			item, err = repo.GetGlobal(ctx)
		} else {
			item, err = repo.GetByAPIKey(ctx, apiKeyID)
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		cfg, active := activeConfigFromStore(item, scopeKey)
		fetched[scopeKey] = cachedScopeConfig{config: cfg, active: active, fetchedAt: now}
		if active {
			result = append(result, cfg)
		}
	}

	s.mu.Lock()
	for scopeKey, cached := range fetched {
		s.configs[scopeKey] = cached
	}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) invalidateConfig(scope string, apiKeyID uuid.UUID) {
	scopeKey := "global"
	if scope == store.RateLimitScopeAPIKey {
		scopeKey = "api_key:" + apiKeyID.String()
	}
	s.mu.Lock()
	delete(s.configs, scopeKey)
	s.mu.Unlock()
}

func (r *Reservation) Metadata(actualTokens int64) map[string]any {
	if r == nil {
		return nil
	}
	scopes := make([]map[string]any, 0, len(r.Scopes))
	for _, scope := range r.Scopes {
		scopes = append(scopes, map[string]any{
			"scope":           scope.Scope,
			"scope_key":       scope.ScopeKey,
			"rpm_limit":       int64PointerValue(scope.RPMLimit),
			"tpm_limit":       int64PointerValue(scope.TPMLimit),
			"rpm_used":        scope.RPMUsed,
			"tpm_reserved":    scope.TPMReserved,
			"tpm_actual":      scope.TPMActual,
			"reserved_tokens": scope.ReservedTokens,
		})
	}
	return map[string]any{
		"estimated_tokens": r.EstimatedTokens,
		"reserved_tokens":  r.ReservedTokens,
		"actual_tokens":    actualTokens,
		"scope_results":    scopes,
		"window_start":     r.WindowStart.Format(time.RFC3339),
	}
}

type activeConfig struct {
	Scope    string
	ScopeKey string
	RPMLimit *int64
	TPMLimit *int64
}

func activeConfigFromStore(item store.GatewayRateLimit, scopeKey string) (activeConfig, bool) {
	if item.Status != store.RateLimitStatusEnabled {
		return activeConfig{}, false
	}
	if !item.RPMLimit.Valid && !item.TPMLimit.Valid {
		return activeConfig{}, false
	}
	return activeConfig{
		Scope:    item.Scope,
		ScopeKey: scopeKey,
		RPMLimit: nullInt64Pointer(item.RPMLimit),
		TPMLimit: nullInt64Pointer(item.TPMLimit),
	}, true
}

func EstimateTokens(payload map[string]any) int64 {
	if payload == nil {
		return defaultReserveOutputTokens
	}
	promptTokens := estimatePromptTokens(payload)
	outputTokens := outputTokenReservation(payload)
	return promptTokens + outputTokens
}

func estimatePromptTokens(payload map[string]any) int64 {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		switch key {
		case "max_tokens", "max_completion_tokens", "max_output_tokens", "stream":
			continue
		default:
			cloned[key] = value
		}
	}
	encoded, err := json.Marshal(cloned)
	if err != nil {
		return 1
	}
	tokens := int64(len(encoded) / 4)
	if tokens < 1 {
		return 1
	}
	return tokens
}

func outputTokenReservation(payload map[string]any) int64 {
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if value, ok := int64FromAny(payload[key]); ok && value > 0 {
			return value
		}
	}
	return defaultReserveOutputTokens
}

func configFromStore(item store.GatewayRateLimit) Config {
	status := normalizeStatus(item.Status)
	if status == "" {
		status = store.RateLimitStatusDisabled
	}
	return Config{
		Status:   status,
		RPMLimit: nullInt64Pointer(item.RPMLimit),
		TPMLimit: nullInt64Pointer(item.TPMLimit),
	}
}

func normalizeStatus(value string) string {
	switch strings.TrimSpace(value) {
	case store.RateLimitStatusEnabled:
		return store.RateLimitStatusEnabled
	case store.RateLimitStatusDisabled, "":
		return store.RateLimitStatusDisabled
	default:
		return ""
	}
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func int64PointerValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func int64PointerAsAny(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func int64FromAny(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case json.Number:
		parsed, err := v.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
