package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AgentRunPending = "pending"
	AgentRunActive  = "active"
	AgentRunEnded   = "ended"
)

// APIKeyKindAgentInternal marks the infrastructure key used for agent-originated
// LLM traffic; it is hidden from the admin API key management surface.
const APIKeyKindAgentInternal = "agent_internal"

type AgentRun struct {
	ID               uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	AgentInstanceID  string     `gorm:"size:128;not null;index"`
	SessionID        string     `gorm:"size:256;not null;uniqueIndex:agent_runs_session_run_idx,priority:1"`
	RunID            string     `gorm:"size:256;not null;uniqueIndex:agent_runs_session_run_idx,priority:2"`
	Model            string     `gorm:"size:256;not null"`
	Status           string     `gorm:"size:32;not null;index"`
	PendingExpiresAt *time.Time `gorm:"index"`
	EndedAt          *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AgentLLMToken struct {
	ID              uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	TokenHash       string    `gorm:"size:64;not null;uniqueIndex"`
	AgentInstanceID string    `gorm:"size:128;not null;index"`
	SessionID       string    `gorm:"size:256;not null;index:agent_llm_tokens_run_idx,priority:1"`
	RunID           string    `gorm:"size:256;not null;index:agent_llm_tokens_run_idx,priority:2"`
	Model           string    `gorm:"size:256;not null"`
	ExpiresAt       time.Time `gorm:"not null;index"`
	SupersededAt    *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
}

type AgentRunInput struct {
	AgentInstanceID string
	SessionID       string
	RunID           string
	Model           string
}

type AgentRepository struct {
	db *gorm.DB
}

func NewAgentRepository(db *gorm.DB) AgentRepository {
	return AgentRepository{db: db}
}

func (r AgentRepository) FindRun(ctx context.Context, input AgentRunInput) (AgentRun, error) {
	var run AgentRun
	err := r.db.WithContext(ctx).
		Where(&AgentRun{SessionID: input.SessionID, RunID: input.RunID}).
		First(&run).Error
	return run, err
}

func (r AgentRepository) Register(ctx context.Context, input AgentRunInput, now time.Time) (AgentRun, error) {
	var run AgentRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where(&AgentRun{SessionID: input.SessionID, RunID: input.RunID})
		err := query.First(&run).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			run = AgentRun{AgentInstanceID: input.AgentInstanceID, SessionID: input.SessionID, RunID: input.RunID, Model: input.Model, Status: AgentRunActive}
			return tx.Create(&run).Error
		}
		if err != nil {
			return err
		}
		if run.AgentInstanceID != input.AgentInstanceID || run.Model != input.Model || run.Status == AgentRunEnded {
			return fmt.Errorf("agent run identity conflict")
		}
		if run.Status == AgentRunPending {
			run.Status = AgentRunActive
			run.PendingExpiresAt = nil
			return tx.Save(&run).Error
		}
		return nil
	})
	if err != nil {
		return AgentRun{}, err
	}
	return run, nil
}

func (r AgentRepository) End(ctx context.Context, input AgentRunInput, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run AgentRun
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(&AgentRun{SessionID: input.SessionID, RunID: input.RunID}).First(&run).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if run.AgentInstanceID != input.AgentInstanceID || run.Model != input.Model {
			return fmt.Errorf("agent run identity conflict")
		}
		if run.Status != AgentRunEnded {
			run.Status = AgentRunEnded
			run.EndedAt = &now
			if err := tx.Save(&run).Error; err != nil {
				return err
			}
		}
		return tx.Model(&AgentLLMToken{}).
			Where(&AgentLLMToken{SessionID: input.SessionID, RunID: input.RunID}).
			Where(clause.Eq{Column: "revoked_at", Value: nil}).
			Updates(map[string]any{"revoked_at": now}).Error
	})
}

// SupersededGracePeriod keeps a freshly superseded token usable for requests
// started around the rotation instant; afterwards the old token is dead.
const SupersededGracePeriod = time.Minute

func (r AgentRepository) FindUsableToken(ctx context.Context, token string, now time.Time) (AgentLLMToken, error) {
	hash := HashAgentToken(token)
	var item AgentLLMToken
	err := r.db.WithContext(ctx).Where(&AgentLLMToken{TokenHash: hash}).First(&item).Error
	if err != nil {
		return AgentLLMToken{}, err
	}
	run, runErr := r.FindRun(ctx, AgentRunInput{SessionID: item.SessionID, RunID: item.RunID})
	if runErr != nil {
		return AgentLLMToken{}, gorm.ErrRecordNotFound
	}
	if !AgentTokenUsable(item, run, now) {
		return AgentLLMToken{}, gorm.ErrRecordNotFound
	}
	return item, nil
}

// AgentTokenUsable reports whether a token may authenticate a new LLM request:
// not revoked, not expired, superseded only within the grace period, and the
// owning run must be active (registered by the runner via /runs/register).
func AgentTokenUsable(item AgentLLMToken, run AgentRun, now time.Time) bool {
	if item.RevokedAt != nil || !item.ExpiresAt.After(now) {
		return false
	}
	if item.SupersededAt != nil && !item.SupersededAt.Add(SupersededGracePeriod).After(now) {
		return false
	}
	return run.Status == AgentRunActive
}

// FindRenewableToken resolves a token for holder-authenticated rotation: the
// token must not be revoked or superseded, must be within the renewal grace
// window (a token expired moments ago may still rotate — possession of the
// secret is the proof, and it can never be used for LLM calls again), and its
// run must be active.
func (r AgentRepository) FindRenewableToken(ctx context.Context, token string, now time.Time) (AgentLLMToken, error) {
	hash := HashAgentToken(token)
	var item AgentLLMToken
	err := r.db.WithContext(ctx).Where(&AgentLLMToken{TokenHash: hash}).First(&item).Error
	if err != nil {
		return AgentLLMToken{}, err
	}
	run, runErr := r.FindRun(ctx, AgentRunInput{SessionID: item.SessionID, RunID: item.RunID})
	if runErr != nil {
		return AgentLLMToken{}, gorm.ErrRecordNotFound
	}
	if !AgentTokenRenewable(item, run, now) {
		return AgentLLMToken{}, gorm.ErrRecordNotFound
	}
	return item, nil
}

// RenewalGracePeriod lets a recently expired token still rotate into a fresh
// one: a run idle past the TTL (long tool call, suspended machine) recovers
// without re-registering, while the expired token itself stays unusable for
// LLM calls.
const RenewalGracePeriod = 10 * time.Minute

// AgentTokenRenewable reports whether a token may be exchanged for a fresh
// credential via the renew endpoint.
func AgentTokenRenewable(item AgentLLMToken, run AgentRun, now time.Time) bool {
	if item.RevokedAt != nil || item.SupersededAt != nil {
		return false
	}
	if !item.ExpiresAt.Add(RenewalGracePeriod).After(now) {
		return false
	}
	return run.Status == AgentRunActive
}

func (r AgentRepository) CreateToken(ctx context.Context, input AgentRunInput, token string, expiresAt time.Time) (AgentLLMToken, error) {
	item := AgentLLMToken{TokenHash: HashAgentToken(token), AgentInstanceID: input.AgentInstanceID, SessionID: input.SessionID, RunID: input.RunID, Model: input.Model, ExpiresAt: expiresAt}
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, err
}

func (r AgentRepository) ListTokens(ctx context.Context, input AgentRunInput) ([]AgentLLMToken, error) {
	var items []AgentLLMToken
	err := r.db.WithContext(ctx).Where(&AgentLLMToken{SessionID: input.SessionID, RunID: input.RunID}).Find(&items).Error
	return items, err
}

func (r AgentRepository) EnsureInternalAPIKey(ctx context.Context) (APIKey, error) {
	var item APIKey
	err := r.db.WithContext(ctx).Where(&APIKey{Name: "xlyra-agent-internal", KeyKind: APIKeyKindAgentInternal}).First(&item).Error
	if err == nil {
		if item.Status != "active" {
			// The internal key is infrastructure, not a user credential: it must
			// never stay disabled, otherwise every agent LLM call would 503.
			if err := r.db.WithContext(ctx).Model(&APIKey{}).Where(&APIKey{ID: item.ID}).Update("status", "active").Error; err != nil {
				return APIKey{}, fmt.Errorf("reactivate internal agent api key: %w", err)
			}
			item.Status = "active"
		}
		return item, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return APIKey{}, err
	}
	return NewAPIKeyRepository(r.db).Create(ctx, CreateAPIKeyParams{
		Name:                 "xlyra-agent-internal",
		KeyPrefix:            "internal",
		KeyHash:              HashAgentToken("xlyra-agent-internal"),
		MaskedKey:            "internal",
		KeyKind:              APIKeyKindAgentInternal,
		Scope:                "gateway",
		Status:               "active",
		ModelPolicy:          "allow_all",
		SitePolicy:           "allow_all",
		QuotaUnlimited:       true,
		QuotaDailyUnlimited:  true,
		QuotaWeeklyUnlimited: true,
	})
}

// SyncInternalAPIKeyPolicy mirrors the agent site/model allowlist settings onto
// the internal key so the gateway's regular access enforcement applies to
// agent-originated LLM traffic.
func (r AgentRepository) SyncInternalAPIKeyPolicy(ctx context.Context, sitePolicy string, siteIDs []uuid.UUID, modelPolicy string, siteModelIDs []uuid.UUID) (APIKey, error) {
	key, err := r.EnsureInternalAPIKey(ctx)
	if err != nil {
		return APIKey{}, err
	}
	if sitePolicy != "allow_list" {
		sitePolicy = "allow_all"
	}
	if modelPolicy != "allow_list" {
		modelPolicy = "allow_all"
	}
	if key.SitePolicy != sitePolicy || key.ModelPolicy != modelPolicy {
		if err := r.db.WithContext(ctx).Model(&APIKey{}).Where(&APIKey{ID: key.ID}).
			Updates(map[string]any{"site_policy": sitePolicy, "model_policy": modelPolicy}).Error; err != nil {
			return APIKey{}, fmt.Errorf("update internal agent api key policy: %w", err)
		}
		key.SitePolicy = sitePolicy
		key.ModelPolicy = modelPolicy
	}
	if err := NewAPIKeyRepository(r.db).SetSitePermissions(ctx, key.ID, siteIDs); err != nil {
		return APIKey{}, fmt.Errorf("sync internal agent site permissions: %w", err)
	}
	if err := NewAPIKeyAccessRepository(r.db).SetSiteModelPermissions(ctx, key.ID, siteModelIDs); err != nil {
		return APIKey{}, fmt.Errorf("sync internal agent site model permissions: %w", err)
	}
	return key, nil
}

func (r AgentRepository) SupersedeTokens(ctx context.Context, input AgentRunInput, now time.Time) error {
	return r.db.WithContext(ctx).Model(&AgentLLMToken{}).
		Where(&AgentLLMToken{SessionID: input.SessionID, RunID: input.RunID}).
		Where(clause.Eq{Column: "superseded_at", Value: nil}).
		Updates(map[string]any{"superseded_at": now}).Error
}

// ModelMemory returns the last-used model overall and per session, derived
// from agent_runs (the server-side record of which model each run used).
func (r AgentRepository) ModelMemory(ctx context.Context) (defaultModel string, bySession map[string]string, err error) {
	var rows []struct {
		SessionID string
		Model     string
	}
	if err = r.db.WithContext(ctx).
		Raw(`SELECT DISTINCT ON (session_id) session_id, model FROM agent_runs ORDER BY session_id, created_at DESC`).
		Scan(&rows).Error; err != nil {
		return "", nil, err
	}
	bySession = make(map[string]string, len(rows))
	for _, row := range rows {
		if row.SessionID != "" && row.Model != "" {
			bySession[row.SessionID] = row.Model
		}
	}
	var latest AgentRun
	if err = r.db.WithContext(ctx).Order("created_at DESC").First(&latest).Error; err == nil {
		defaultModel = latest.Model
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return defaultModel, bySession, err
}

func HashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
