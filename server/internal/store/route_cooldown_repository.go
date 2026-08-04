package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RouteCooldown struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID           uuid.UUID
	SiteModelID      uuid.NullUUID
	SiteCredentialID uuid.NullUUID
	Scope            string
	Source           string
	Reason           string
	ActiveUntil      time.Time
	ClearedAt        sql.NullTime
	Metadata         JSON `gorm:"type:jsonb"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CooldownReasonCodexUsageLimitReached marks a cooldown created because a Codex
// account exhausted its 5-hour or weekly quota window. It is the reset time of the
// exhausted window, so a quota recovery (OpenAI auto-reset or a reset-credit
// redemption) should be allowed to clear it early.
const CooldownReasonCodexUsageLimitReached = "codex_usage_limit_reached"

const (
	CooldownReasonUpstreamModelNotFound          = "upstream_model_not_found"
	CooldownReasonUpstreamCredentialsExhausted   = "upstream_credentials_exhausted"
	CooldownReasonUpstreamStreamUnstable         = "upstream_stream_unstable"
	CooldownReasonUpstreamCredentialUnauthorized = "upstream_credential_unauthorized"
	CooldownReasonUpstreamCredentialLimited      = "upstream_credential_limited"
	CooldownReasonOpenCodeGoUsageLimitReached    = "opencode_go_usage_limit_reached"
)

func TransientRouteCooldown(item RouteCooldown) bool {
	return item.Source == "gateway" &&
		item.SiteModelID.Valid &&
		!item.SiteCredentialID.Valid &&
		item.Reason != CooldownReasonUpstreamModelNotFound
}

// CodexQuotaCooldownReasons lists the cooldown reasons that a Codex quota recovery
// is permitted to clear. It intentionally excludes auth, capacity, and transport
// failures, which are unrelated to quota availability and must not be cleared just
// because the account regained quota.
func CodexQuotaCooldownReasons() []string {
	return []string{CooldownReasonCodexUsageLimitReached}
}

// ClearActiveCooldownFilter selects which active (uncleared) cooldowns to clear.
// SiteID is required. When SiteCredentialID is set, only that credential's
// cooldowns are matched. When Reasons is non-empty, only cooldowns whose reason is
// in the list are matched.
type ClearActiveCooldownFilter struct {
	SiteID           uuid.UUID
	SiteCredentialID uuid.NullUUID
	Reasons          []string
}

type ActivateRouteCooldownParams struct {
	SiteID           uuid.UUID
	SiteModelID      any
	SiteCredentialID any
	Scope            string
	Source           string
	Reason           string
	ActiveUntil      time.Time
	Metadata         JSON
}

type RouteCooldownRepository struct {
	db *gorm.DB
}

func NewRouteCooldownRepository(db *gorm.DB) RouteCooldownRepository {
	return RouteCooldownRepository{db: db}
}

func (r RouteCooldownRepository) Activate(ctx context.Context, params ActivateRouteCooldownParams) (RouteCooldown, error) {
	siteModelID := nullUUIDFromAny(params.SiteModelID)
	siteCredentialID := nullUUIDFromAny(params.SiteCredentialID)
	if params.Scope == "" {
		if siteCredentialID.Valid {
			params.Scope = "credential"
		} else if !siteModelID.Valid {
			params.Scope = "site"
		} else {
			params.Scope = "model"
		}
	}
	if params.Source == "" {
		params.Source = "manual"
	}
	if params.Reason == "" {
		params.Reason = "cooldown"
	}
	if params.ActiveUntil.IsZero() {
		params.ActiveUntil = time.Now()
	}
	item := RouteCooldown{
		SiteID:           params.SiteID,
		SiteModelID:      siteModelID,
		SiteCredentialID: siteCredentialID,
		Scope:            params.Scope,
		Source:           params.Source,
		Reason:           params.Reason,
		ActiveUntil:      params.ActiveUntil,
		Metadata:         jsonDefault(params.Metadata, "{}"),
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scoped := RouteCooldownRepository{db: tx}
		if err := scoped.clearMatching(ctx, params.SiteID, siteModelID, siteCredentialID, params.Source); err != nil {
			return fmt.Errorf("clear existing route cooldowns: %w", err)
		}
		if err := tx.Create(&item).Error; err != nil {
			return fmt.Errorf("activate route cooldown: %w", err)
		}
		return nil
	}); err != nil {
		return RouteCooldown{}, err
	}
	return item, nil
}

type CountRouteCooldownActivationsParams struct {
	SiteID           uuid.UUID
	SiteModelID      uuid.NullUUID
	SiteCredentialID uuid.NullUUID
	Source           string
	Reasons          []string
	Since            time.Time
}

func (r RouteCooldownRepository) CountRecentActivations(ctx context.Context, params CountRouteCooldownActivationsParams) (int64, error) {
	if params.SiteID == uuid.Nil {
		return 0, nil
	}
	exprs := []clause.Expression{
		clause.Eq{Column: clause.Column{Name: "site_id"}, Value: params.SiteID},
		clause.Gte{Column: clause.Column{Name: "created_at"}, Value: params.Since},
	}
	exprs = append(exprs, nullableUUIDExpr("site_model_id", params.SiteModelID))
	exprs = append(exprs, nullableUUIDExpr("site_credential_id", params.SiteCredentialID))
	if params.Source != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "source"}, Value: params.Source})
	}
	if len(params.Reasons) > 0 {
		values := make([]any, 0, len(params.Reasons))
		for _, reason := range params.Reasons {
			values = append(values, reason)
		}
		exprs = append(exprs, clause.IN{Column: clause.Column{Name: "reason"}, Values: values})
	}
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&RouteCooldown{}).
		Clauses(clause.Where{Exprs: exprs}).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count recent route cooldown activations: %w", err)
	}
	return count, nil
}

func (r RouteCooldownRepository) ClearActive(ctx context.Context, siteID uuid.UUID, siteModelID any, siteCredentialID any, source string) error {
	if err := r.clearMatching(ctx, siteID, nullUUIDFromAny(siteModelID), nullUUIDFromAny(siteCredentialID), source); err != nil {
		return fmt.Errorf("clear route cooldowns: %w", err)
	}
	return nil
}

// ClearActiveMatching marks matching active cooldowns as cleared and returns the
// number of rows affected. Used by Codex quota-recovery flows to lift a quota
// cooldown as soon as the account regains usable quota, without waiting for the
// original reset time to elapse.
func (r RouteCooldownRepository) ClearActiveMatching(ctx context.Context, filter ClearActiveCooldownFilter) (int64, error) {
	if filter.SiteID == uuid.Nil {
		return 0, nil
	}
	exprs := []clause.Expression{
		clause.Eq{Column: clause.Column{Name: "site_id"}, Value: filter.SiteID},
		clause.Eq{Column: clause.Column{Name: "cleared_at"}, Value: nil},
	}
	if filter.SiteCredentialID.Valid {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "site_credential_id"}, Value: filter.SiteCredentialID.UUID})
	}
	if len(filter.Reasons) > 0 {
		values := make([]any, 0, len(filter.Reasons))
		for _, reason := range filter.Reasons {
			values = append(values, reason)
		}
		exprs = append(exprs, clause.IN{Column: clause.Column{Name: "reason"}, Values: values})
	}
	result := r.db.WithContext(ctx).
		Model(&RouteCooldown{}).
		Clauses(clause.Where{Exprs: exprs}).
		Update("cleared_at", sql.NullTime{Time: time.Now(), Valid: true})
	if result.Error != nil {
		return 0, fmt.Errorf("clear active route cooldowns: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (r RouteCooldownRepository) DeleteBySiteModel(ctx context.Context, siteID uuid.UUID, siteModelID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&RouteCooldown{
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
	}).Delete(&RouteCooldown{}).Error; err != nil {
		return fmt.Errorf("delete route cooldowns by site model: %w", err)
	}
	return nil
}

func (r RouteCooldownRepository) DeleteBySiteCredential(ctx context.Context, siteID uuid.UUID, siteCredentialID uuid.UUID) error {
	if err := r.db.WithContext(ctx).Where(&RouteCooldown{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: siteCredentialID, Valid: true},
	}).Delete(&RouteCooldown{}).Error; err != nil {
		return fmt.Errorf("delete route cooldowns by site credential: %w", err)
	}
	return nil
}

func (r RouteCooldownRepository) ListActive(ctx context.Context, now time.Time) ([]RouteCooldown, error) {
	if now.IsZero() {
		now = time.Now()
	}
	var all []RouteCooldown
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "cleared_at"}, Value: nil},
			clause.Gt{Column: clause.Column{Name: "active_until"}, Value: now},
		}}).
		Find(&all).Error; err != nil {
		return nil, fmt.Errorf("list active route cooldowns: %w", err)
	}
	items := make([]RouteCooldown, 0)
	for _, item := range all {
		if !item.ClearedAt.Valid && item.ActiveUntil.After(now) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].ActiveUntil.Equal(items[j].ActiveUntil) {
			return items[i].ActiveUntil.After(items[j].ActiveUntil)
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (r RouteCooldownRepository) clearMatching(ctx context.Context, siteID uuid.UUID, siteModelID uuid.NullUUID, siteCredentialID uuid.NullUUID, source string) error {
	exprs := []clause.Expression{
		clause.Eq{Column: clause.Column{Name: "site_id"}, Value: siteID},
		clause.Eq{Column: clause.Column{Name: "cleared_at"}, Value: nil},
		nullableUUIDExpr("site_model_id", siteModelID),
		nullableUUIDExpr("site_credential_id", siteCredentialID),
	}
	if source != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "source"}, Value: source})
	}
	return r.db.WithContext(ctx).
		Model(&RouteCooldown{}).
		Clauses(clause.Where{Exprs: exprs}).
		Update("cleared_at", sql.NullTime{Time: time.Now(), Valid: true}).Error
}

func nullableUUIDExpr(column string, value uuid.NullUUID) clause.Expression {
	if value.Valid {
		return clause.Eq{Column: clause.Column{Name: column}, Value: value.UUID}
	}
	return clause.Eq{Column: clause.Column{Name: column}, Value: nil}
}
