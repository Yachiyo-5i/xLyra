package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type OAuthConnection struct {
	ID                    uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Provider              string    `gorm:"uniqueIndex:oauth_connections_provider_email_unique,priority:1"`
	SiteID                *uuid.UUID
	Status                string
	AccountID             string
	Email                 string `gorm:"uniqueIndex:oauth_connections_provider_email_unique,priority:2"`
	EncryptedAccessToken  string
	MaskedAccessToken     string
	EncryptedRefreshToken string
	MaskedRefreshToken    string
	EncryptedIDToken      string
	MaskedIDToken         string
	TokenType             string
	Scopes                string
	ExpiresAt             sql.NullTime
	LastRefreshAt         sql.NullTime
	LastSyncAt            sql.NullTime
	RawProfile            JSON `gorm:"type:jsonb"`
	Metadata              JSON `gorm:"type:jsonb"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type UpsertOAuthConnectionParams struct {
	Provider              string
	SiteID                uuid.UUID
	Status                string
	AccountID             string
	Email                 string
	EncryptedAccessToken  string
	MaskedAccessToken     string
	EncryptedRefreshToken string
	MaskedRefreshToken    string
	EncryptedIDToken      string
	MaskedIDToken         string
	TokenType             string
	Scopes                string
	ExpiresAt             any
	LastRefreshAt         any
	LastSyncAt            any
	RawProfile            JSON
	Metadata              JSON
}

type OAuthConnectionRepository struct {
	db *gorm.DB
}

func NewOAuthConnectionRepository(db *gorm.DB) OAuthConnectionRepository {
	return OAuthConnectionRepository{db: db}
}

func (r OAuthConnectionRepository) UpsertByProviderEmail(ctx context.Context, params UpsertOAuthConnectionParams) (OAuthConnection, error) {
	provider := strings.TrimSpace(params.Provider)
	email := strings.TrimSpace(params.Email)
	if email == "" {
		return OAuthConnection{}, fmt.Errorf("upsert oauth connection: email is required")
	}

	var connection OAuthConnection
	db := r.db.WithContext(ctx)
	err := db.Where(&OAuthConnection{Provider: provider, Email: email}).First(&connection).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return OAuthConnection{}, fmt.Errorf("upsert oauth connection: %w", err)
	}
	connection.Provider = provider
	if params.SiteID != uuid.Nil {
		siteID := params.SiteID
		connection.SiteID = &siteID
	} else if err == gorm.ErrRecordNotFound {
		connection.SiteID = nil
	}
	connection.Status = defaultOAuthConnectionString(params.Status, "connected")
	connection.AccountID = params.AccountID
	connection.Email = email
	connection.EncryptedAccessToken = params.EncryptedAccessToken
	connection.MaskedAccessToken = params.MaskedAccessToken
	connection.EncryptedRefreshToken = params.EncryptedRefreshToken
	connection.MaskedRefreshToken = params.MaskedRefreshToken
	connection.EncryptedIDToken = params.EncryptedIDToken
	connection.MaskedIDToken = params.MaskedIDToken
	connection.TokenType = defaultOAuthConnectionString(params.TokenType, "Bearer")
	connection.Scopes = params.Scopes
	connection.ExpiresAt = nullTimeFromAny(params.ExpiresAt)
	connection.LastRefreshAt = nullTimeFromAny(params.LastRefreshAt)
	connection.LastSyncAt = nullTimeFromAny(params.LastSyncAt)
	connection.RawProfile = jsonDefault(params.RawProfile, "{}")
	connection.Metadata = jsonDefault(params.Metadata, "{}")
	if err == gorm.ErrRecordNotFound {
		if err := db.Create(&connection).Error; err != nil {
			return OAuthConnection{}, fmt.Errorf("upsert oauth connection: %w", err)
		}
		return connection, nil
	}
	if err := db.Save(&connection).Error; err != nil {
		return OAuthConnection{}, fmt.Errorf("upsert oauth connection: %w", err)
	}
	return connection, nil
}

func (r OAuthConnectionRepository) GetByID(ctx context.Context, id uuid.UUID) (OAuthConnection, error) {
	var connection OAuthConnection
	if err := r.db.WithContext(ctx).Where(&OAuthConnection{ID: id}).First(&connection).Error; err != nil {
		return OAuthConnection{}, fmt.Errorf("get oauth connection: %w", err)
	}
	return connection, nil
}

func (r OAuthConnectionRepository) GetBySiteID(ctx context.Context, siteID uuid.UUID) (OAuthConnection, error) {
	var connections []OAuthConnection
	if err := r.db.WithContext(ctx).Find(&connections).Error; err != nil {
		return OAuthConnection{}, fmt.Errorf("get oauth connection by site: %w", err)
	}
	for _, connection := range connections {
		if connection.SiteID != nil && *connection.SiteID == siteID {
			return connection, nil
		}
	}
	return OAuthConnection{}, fmt.Errorf("get oauth connection by site: %w", gorm.ErrRecordNotFound)
}

func (r OAuthConnectionRepository) List(ctx context.Context) ([]OAuthConnection, error) {
	var items []OAuthConnection
	if err := r.db.WithContext(ctx).Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list oauth connections: %w", err)
	}
	filtered := items[:0]
	for _, item := range items {
		if item.SiteID != nil {
			filtered = append(filtered, item)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].ID.String() < filtered[j].ID.String()
	})
	return filtered, nil
}

func (r OAuthConnectionRepository) DeleteBySiteID(ctx context.Context, siteID uuid.UUID) error {
	if siteID == uuid.Nil {
		return nil
	}
	var connections []OAuthConnection
	if err := r.db.WithContext(ctx).Find(&connections).Error; err != nil {
		return fmt.Errorf("delete oauth connections by site: %w", err)
	}
	for _, connection := range connections {
		if connection.SiteID == nil || *connection.SiteID != siteID {
			continue
		}
		if err := r.db.WithContext(ctx).Delete(&connection).Error; err != nil {
			return fmt.Errorf("delete oauth connections by site: %w", err)
		}
	}
	return nil
}

func (r OAuthConnectionRepository) Save(ctx context.Context, connection OAuthConnection) (OAuthConnection, error) {
	if err := r.db.WithContext(ctx).Save(&connection).Error; err != nil {
		return OAuthConnection{}, fmt.Errorf("save oauth connection: %w", err)
	}
	return connection, nil
}

func defaultOAuthConnectionString(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
