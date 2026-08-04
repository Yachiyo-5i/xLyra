package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Admin struct {
	ID                         uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Username                   string
	Nickname                   string
	Avatar                     string
	PasswordHash               string
	Role                       string
	Status                     string
	TOTPEnabled                bool `gorm:"column:totp_enabled"`
	TOTPSecretEncrypted        string
	TOTPPendingSecretEncrypted string
	LastLoginAt                sql.NullTime
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type CreateAdminParams struct {
	Username     string
	Nickname     string
	Avatar       string
	PasswordHash string
	Role         string
	Status       string
}

type AdminRepository struct {
	db *gorm.DB
}

func NewAdminRepository(db *gorm.DB) AdminRepository {
	return AdminRepository{db: db}
}

func (r AdminRepository) Create(ctx context.Context, params CreateAdminParams) (Admin, error) {
	admin := Admin{
		Username:     params.Username,
		Nickname:     params.Nickname,
		Avatar:       params.Avatar,
		PasswordHash: params.PasswordHash,
		Role:         params.Role,
		Status:       params.Status,
	}
	if err := r.db.WithContext(ctx).Create(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("create admin: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) CreateInitial(ctx context.Context, params CreateAdminParams) (Admin, error) {
	var admin Admin
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Admin{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return gorm.ErrRecordNotFound
		}
		admin = Admin{
			Username:     params.Username,
			Nickname:     params.Nickname,
			Avatar:       params.Avatar,
			PasswordHash: params.PasswordHash,
			Role:         params.Role,
			Status:       params.Status,
		}
		return tx.Create(&admin).Error
	})
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return Admin{}, gorm.ErrRecordNotFound
		}
		return Admin{}, fmt.Errorf("create initial admin: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) GetByUsername(ctx context.Context, username string) (Admin, error) {
	var admins []Admin
	if err := r.db.WithContext(ctx).Find(&admins).Error; err != nil {
		return Admin{}, fmt.Errorf("get admin by username: %w", err)
	}
	for _, admin := range admins {
		if strings.EqualFold(admin.Username, username) {
			return admin, nil
		}
	}
	return Admin{}, fmt.Errorf("get admin by username: %w", gorm.ErrRecordNotFound)
}

func (r AdminRepository) GetByID(ctx context.Context, id uuid.UUID) (Admin, error) {
	var admin Admin
	if err := r.db.WithContext(ctx).Where(&Admin{ID: id}).First(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("get admin by id: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) UpdateProfile(ctx context.Context, id uuid.UUID, username string, nickname string, avatar string) (Admin, error) {
	admin, err := r.GetByID(ctx, id)
	if err != nil {
		return Admin{}, err
	}
	admin.Username = username
	admin.Nickname = nickname
	admin.Avatar = avatar
	if err := r.db.WithContext(ctx).Save(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("update admin profile: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) UpdatePasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) (Admin, error) {
	admin, err := r.GetByID(ctx, id)
	if err != nil {
		return Admin{}, err
	}
	admin.PasswordHash = passwordHash
	if err := r.db.WithContext(ctx).Save(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("update admin password: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) UpdateLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	return r.db.WithContext(ctx).Model(&Admin{}).Where(&Admin{ID: id}).Update("last_login_at", at).Error
}

func (r AdminRepository) SaveTOTPSetup(ctx context.Context, id uuid.UUID, encryptedSecret string) (Admin, error) {
	admin, err := r.GetByID(ctx, id)
	if err != nil {
		return Admin{}, err
	}
	admin.TOTPPendingSecretEncrypted = encryptedSecret
	if err := r.db.WithContext(ctx).Save(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("save admin totp setup: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) EnableTOTP(ctx context.Context, id uuid.UUID) (Admin, error) {
	admin, err := r.GetByID(ctx, id)
	if err != nil {
		return Admin{}, err
	}
	admin.TOTPSecretEncrypted = admin.TOTPPendingSecretEncrypted
	admin.TOTPPendingSecretEncrypted = ""
	admin.TOTPEnabled = true
	if err := r.db.WithContext(ctx).Save(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("enable admin totp: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) DisableTOTP(ctx context.Context, id uuid.UUID) (Admin, error) {
	admin, err := r.GetByID(ctx, id)
	if err != nil {
		return Admin{}, err
	}
	admin.TOTPSecretEncrypted = ""
	admin.TOTPPendingSecretEncrypted = ""
	admin.TOTPEnabled = false
	if err := r.db.WithContext(ctx).Save(&admin).Error; err != nil {
		return Admin{}, fmt.Errorf("disable admin totp: %w", err)
	}
	return admin, nil
}

func (r AdminRepository) Count(ctx context.Context) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&Admin{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return int(count), nil
}
