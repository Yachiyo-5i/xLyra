package site

import (
	"context"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func (s *Service) recoverCredentialCooldownAfterRefresh(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) {
	if credentialID == uuid.Nil {
		return
	}
	_, _ = store.NewRouteCooldownRepository(s.db.DB()).ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Reasons: []string{
			store.CooldownReasonUpstreamCredentialLimited,
			store.CooldownReasonUpstreamCredentialUnauthorized,
		},
	})
}
