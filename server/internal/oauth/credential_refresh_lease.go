package oauth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

const credentialRefreshLeaseWait = 250 * time.Millisecond

func (s *Service) refreshCredentialWithLease(
	ctx context.Context,
	credentialID uuid.UUID,
	loadLabel string,
	inspect func(store.SiteCredential) (any, bool, error),
	refresh func(context.Context, store.SiteCredential) (any, error),
) (any, error) {
	repo := store.NewSiteCredentialRepository(s.db.DB())
	leases := store.NewSiteCredentialRefreshLeaseRepository(s.db.DB())
	var heldLease string
	defer func() {
		if heldLease == "" {
			return
		}
		_ = leases.Release(context.WithoutCancel(ctx), credentialID, heldLease)
	}()

	cred, err := repo.GetByID(ctx, credentialID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", loadLabel, err)
	}
	if result, fresh, err := inspect(cred); err != nil || fresh {
		return result, err
	}

	for {
		leaseID := uuid.NewString()
		acquired, err := leases.TryAcquire(ctx, credentialID, leaseID, time.Now(), 2*refreshTimeout)
		if err != nil {
			return nil, err
		}
		if !acquired {
			cred, err = repo.GetByID(ctx, credentialID)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", loadLabel, err)
			}
			if result, fresh, err := inspect(cred); err != nil || fresh {
				return result, err
			}
			if err := waitCredentialRefreshLease(ctx); err != nil {
				return nil, err
			}
			continue
		}

		heldLease = leaseID
		cred, err = repo.GetByID(ctx, credentialID)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", loadLabel, err)
		}
		if result, fresh, err := inspect(cred); err != nil || fresh {
			return result, err
		}
		return refresh(ctx, cred)
	}
}

func waitCredentialRefreshLease(ctx context.Context) error {
	timer := time.NewTimer(credentialRefreshLeaseWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
