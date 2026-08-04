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
)

type AdminAccessToken struct {
	ID                uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	AdminID           uuid.UUID
	TokenHash         string
	MaskedToken       string
	Enabled           bool
	LastUsedAt        sql.NullTime
	LastUsedIP        IPAddress `gorm:"column:last_used_ip"`
	LastUsedUserAgent string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateAdminAccessTokenParams struct {
	AdminID     uuid.UUID
	TokenHash   string
	MaskedToken string
	Enabled     bool
}

type AdminAccessTokenRepository struct {
	db *gorm.DB
}

func NewAdminAccessTokenRepository(db *gorm.DB) AdminAccessTokenRepository {
	return AdminAccessTokenRepository{db: db}
}

func (r AdminAccessTokenRepository) Get(ctx context.Context) (AdminAccessToken, error) {
	var items []AdminAccessToken
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return AdminAccessToken{}, fmt.Errorf("get admin access token: %w", err)
	}
	if len(items) == 0 {
		return AdminAccessToken{}, fmt.Errorf("get admin access token: %w", gorm.ErrRecordNotFound)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items[0], nil
}

func (r AdminAccessTokenRepository) Replace(ctx context.Context, params CreateAdminAccessTokenParams) (AdminAccessToken, error) {
	var item AdminAccessToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&AdminAccessToken{}).Error; err != nil {
			return err
		}
		item = AdminAccessToken{
			AdminID:     params.AdminID,
			TokenHash:   params.TokenHash,
			MaskedToken: params.MaskedToken,
			Enabled:     params.Enabled,
		}
		return tx.Create(&item).Error
	})
	if err != nil {
		return AdminAccessToken{}, fmt.Errorf("replace admin access token: %w", err)
	}
	return item, nil
}

func (r AdminAccessTokenRepository) GetActiveByTokenHash(ctx context.Context, tokenHash string) (AdminAccessToken, error) {
	var item AdminAccessToken
	if err := r.db.WithContext(ctx).Where(&AdminAccessToken{TokenHash: tokenHash, Enabled: true}).First(&item).Error; err != nil {
		return AdminAccessToken{}, fmt.Errorf("get active admin access token: %w", err)
	}
	return item, nil
}

func (r AdminAccessTokenRepository) SetEnabled(ctx context.Context, enabled bool) (AdminAccessToken, error) {
	item, err := r.Get(ctx)
	if err != nil {
		return AdminAccessToken{}, err
	}
	item.Enabled = enabled
	if err := r.db.WithContext(ctx).Save(&item).Error; err != nil {
		return AdminAccessToken{}, fmt.Errorf("set admin access token enabled: %w", err)
	}
	return item, nil
}

func (r AdminAccessTokenRepository) Delete(ctx context.Context) error {
	if err := r.db.WithContext(ctx).Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&AdminAccessToken{}).Error; err != nil {
		return fmt.Errorf("delete admin access token: %w", err)
	}
	return nil
}

func (r AdminAccessTokenRepository) TouchLastUsed(ctx context.Context, id uuid.UUID, ip net.IP, userAgent string, now time.Time) error {
	updates := map[string]any{
		"last_used_at":         now,
		"last_used_ip":         IPAddress(ip),
		"last_used_user_agent": userAgent,
	}
	if err := r.db.WithContext(ctx).Model(&AdminAccessToken{}).Where(&AdminAccessToken{ID: id}).Updates(updates).Error; err != nil {
		return fmt.Errorf("touch admin access token: %w", err)
	}
	return nil
}
