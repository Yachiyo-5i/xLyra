package store

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AdminSession struct {
	ID               uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	AdminID          uuid.UUID
	SessionTokenHash string `gorm:"column:session_token_hash"`
	ExpiresAt        *time.Time
	LastSeenAt       sql.NullTime
	IPAddress        IPAddress `gorm:"column:ip_address"`
	UserAgent        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CreateAdminSessionParams struct {
	AdminID          uuid.UUID
	SessionTokenHash string
	ExpiresAt        *time.Time
	IPAddress        net.IP
	UserAgent        string
}

type AdminSessionRepository struct {
	db *gorm.DB
}

func NewAdminSessionRepository(db *gorm.DB) AdminSessionRepository {
	return AdminSessionRepository{db: db}
}

func (r AdminSessionRepository) Create(ctx context.Context, params CreateAdminSessionParams) (AdminSession, error) {
	session := AdminSession{
		AdminID:          params.AdminID,
		SessionTokenHash: params.SessionTokenHash,
		ExpiresAt:        params.ExpiresAt,
		IPAddress:        IPAddress(params.IPAddress),
		UserAgent:        params.UserAgent,
	}
	if err := r.db.WithContext(ctx).Create(&session).Error; err != nil {
		return AdminSession{}, fmt.Errorf("create admin session: %w", err)
	}
	return session, nil
}

func (r AdminSessionRepository) GetActiveByTokenHash(ctx context.Context, tokenHash string, now time.Time) (AdminSession, error) {
	var sessions []AdminSession
	if err := r.db.WithContext(ctx).Where(&AdminSession{SessionTokenHash: tokenHash}).Find(&sessions).Error; err != nil {
		return AdminSession{}, fmt.Errorf("get active admin session: %w", err)
	}
	for _, session := range sessions {
		if adminSessionActive(session, now) {
			return session, nil
		}
	}
	return AdminSession{}, fmt.Errorf("get active admin session: %w", gorm.ErrRecordNotFound)
}

func (r AdminSessionRepository) DeleteByTokenHash(ctx context.Context, tokenHash string) error {
	if err := r.db.WithContext(ctx).Where(&AdminSession{SessionTokenHash: tokenHash}).Delete(&AdminSession{}).Error; err != nil {
		return fmt.Errorf("delete admin session: %w", err)
	}
	return nil
}

func (r AdminSessionRepository) DeleteByID(ctx context.Context, adminID uuid.UUID, sessionID uuid.UUID) error {
	result := r.db.WithContext(ctx).Where(&AdminSession{ID: sessionID, AdminID: adminID}).Delete(&AdminSession{})
	if result.Error != nil {
		return fmt.Errorf("delete admin session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("delete admin session: %w", gorm.ErrRecordNotFound)
	}
	return nil
}

func (r AdminSessionRepository) DeleteOthers(ctx context.Context, adminID uuid.UUID, keepSessionID uuid.UUID) error {
	var sessions []AdminSession
	if err := r.db.WithContext(ctx).Where(&AdminSession{AdminID: adminID}).Find(&sessions).Error; err != nil {
		return fmt.Errorf("delete other admin sessions: %w", err)
	}
	for _, session := range sessions {
		if keepSessionID != uuid.Nil && session.ID == keepSessionID {
			continue
		}
		if err := r.db.WithContext(ctx).Delete(&session).Error; err != nil {
			return fmt.Errorf("delete other admin sessions: %w", err)
		}
	}
	return nil
}

func (r AdminSessionRepository) ListByAdmin(ctx context.Context, adminID uuid.UUID, now time.Time) ([]AdminSession, error) {
	var sessions []AdminSession
	if err := r.db.WithContext(ctx).Where(&AdminSession{AdminID: adminID}).Find(&sessions).Error; err != nil {
		return nil, fmt.Errorf("list admin sessions: %w", err)
	}
	active := sessions[:0]
	for _, session := range sessions {
		if adminSessionActive(session, now) {
			active = append(active, session)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].CreatedAt.After(active[j].CreatedAt)
	})
	return active, nil
}

func (r AdminSessionRepository) TouchLastSeen(ctx context.Context, sessionID uuid.UUID, now time.Time, throttle time.Duration) error {
	where := clause.Where{Exprs: []clause.Expression{
		clause.Eq{Column: clause.Column{Name: "id"}, Value: sessionID},
		clause.Or(
			clause.Eq{Column: clause.Column{Name: "last_seen_at"}, Value: nil},
			clause.Lte{Column: clause.Column{Name: "last_seen_at"}, Value: now.Add(-throttle)},
		),
	}}
	if err := r.db.WithContext(ctx).Model(&AdminSession{}).Clauses(where).Update("last_seen_at", now).Error; err != nil {
		return fmt.Errorf("touch admin session: %w", err)
	}
	return nil
}

func adminSessionActive(session AdminSession, now time.Time) bool {
	return session.ExpiresAt == nil || session.ExpiresAt.After(now)
}
