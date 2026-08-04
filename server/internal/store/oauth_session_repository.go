package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type OAuthSession struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Provider           string
	State              string
	PKCEVerifier       string
	RedirectURI        string
	SuccessRedirectURL string
	FailureRedirectURL string
	SiteID             *uuid.UUID
	SitePayload        JSON `gorm:"type:jsonb"`
	Status             string
	ExpiresAt          time.Time
	CompletedAt        sql.NullTime
	Metadata           JSON `gorm:"type:jsonb"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type CreateOAuthSessionParams struct {
	Provider           string
	State              string
	PKCEVerifier       string
	RedirectURI        string
	SuccessRedirectURL string
	FailureRedirectURL string
	SiteID             uuid.UUID
	SitePayload        JSON
	Status             string
	ExpiresAt          time.Time
	Metadata           JSON
}

type OAuthSessionRepository struct {
	db *gorm.DB
}

func NewOAuthSessionRepository(db *gorm.DB) OAuthSessionRepository {
	return OAuthSessionRepository{db: db}
}

func (r OAuthSessionRepository) Create(ctx context.Context, params CreateOAuthSessionParams) (OAuthSession, error) {
	status := params.Status
	if status == "" {
		status = "pending"
	}
	session := OAuthSession{
		Provider:           params.Provider,
		State:              params.State,
		PKCEVerifier:       params.PKCEVerifier,
		RedirectURI:        params.RedirectURI,
		SuccessRedirectURL: params.SuccessRedirectURL,
		FailureRedirectURL: params.FailureRedirectURL,
		SitePayload:        jsonDefault(params.SitePayload, "{}"),
		Status:             status,
		ExpiresAt:          params.ExpiresAt,
		Metadata:           jsonDefault(params.Metadata, "{}"),
	}
	if params.SiteID != uuid.Nil {
		siteID := params.SiteID
		session.SiteID = &siteID
	}
	if err := r.db.WithContext(ctx).Create(&session).Error; err != nil {
		return OAuthSession{}, fmt.Errorf("create oauth session: %w", err)
	}
	return session, nil
}

func (r OAuthSessionRepository) GetByState(ctx context.Context, state string) (OAuthSession, error) {
	var session OAuthSession
	if err := r.db.WithContext(ctx).Where(&OAuthSession{State: state}).First(&session).Error; err != nil {
		return OAuthSession{}, fmt.Errorf("get oauth session: %w", err)
	}
	return session, nil
}

func (r OAuthSessionRepository) Complete(ctx context.Context, id uuid.UUID, status string, metadata JSON) (OAuthSession, error) {
	session, err := r.GetByID(ctx, id)
	if err != nil {
		return OAuthSession{}, err
	}
	if status == "" {
		status = "completed"
	}
	session.Status = status
	session.CompletedAt = sql.NullTime{Time: time.Now(), Valid: true}
	if len(metadata) > 0 {
		session.Metadata = metadata
	}
	if err := r.db.WithContext(ctx).Save(&session).Error; err != nil {
		return OAuthSession{}, fmt.Errorf("complete oauth session: %w", err)
	}
	return session, nil
}

func (r OAuthSessionRepository) GetByID(ctx context.Context, id uuid.UUID) (OAuthSession, error) {
	var session OAuthSession
	if err := r.db.WithContext(ctx).Where(&OAuthSession{ID: id}).First(&session).Error; err != nil {
		return OAuthSession{}, fmt.Errorf("get oauth session by id: %w", err)
	}
	return session, nil
}
