package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SiteCredentialRefreshLeaseRepository struct {
	db *gorm.DB
}

func NewSiteCredentialRefreshLeaseRepository(db *gorm.DB) SiteCredentialRefreshLeaseRepository {
	return SiteCredentialRefreshLeaseRepository{db: db}
}

func (r SiteCredentialRefreshLeaseRepository) TryAcquire(ctx context.Context, credentialID uuid.UUID, leaseID string, now time.Time, ttl time.Duration) (bool, error) {
	until := now.Add(ttl)
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO site_credential_refresh_leases (site_credential_id, lease_id, lease_until)
		VALUES (?, ?, ?)
		ON CONFLICT (site_credential_id) DO UPDATE
		SET lease_id = EXCLUDED.lease_id, lease_until = EXCLUDED.lease_until
		WHERE site_credential_refresh_leases.lease_until < ?`,
		credentialID, leaseID, until, now)
	if result.Error != nil {
		return false, fmt.Errorf("acquire site credential refresh lease: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (r SiteCredentialRefreshLeaseRepository) Release(ctx context.Context, credentialID uuid.UUID, leaseID string) error {
	result := r.db.WithContext(ctx).Exec(`
		DELETE FROM site_credential_refresh_leases
		WHERE site_credential_id = ? AND lease_id = ?`,
		credentialID, leaseID)
	if result.Error != nil {
		return fmt.Errorf("release site credential refresh lease: %w", result.Error)
	}
	return nil
}
