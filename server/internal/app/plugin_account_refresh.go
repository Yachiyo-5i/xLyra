package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"xlyra/server/internal/site"
)

// pluginAccountRefresher persists a gateway account-pool refresh through the
// site service. The site service holds the refresh lease and the ciphertext.
type pluginAccountRefresher struct {
	sites *site.Service
}

func (r pluginAccountRefresher) Refresh(ctx context.Context, siteID, credentialID uuid.UUID, siteType string, fields map[string]string) (map[string]string, error) {
	if r.sites == nil {
		return nil, errors.New("site service is not available")
	}
	return r.sites.RefreshPluginAccountForGateway(ctx, siteID, credentialID, siteType, fields)
}
