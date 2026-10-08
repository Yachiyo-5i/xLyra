package site

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

// PatchGatewayPlugin binds a plugin to a site for one per-site kind. An empty
// pluginID removes the binding. Only the plugins entry of the gateway config
// changes.
func (s *Service) PatchGatewayPlugin(ctx context.Context, siteID uuid.UUID, kind, pluginID string) (store.Site, error) {
	if s == nil || s.db == nil {
		return store.Site{}, fmt.Errorf("site service is not available")
	}
	if siteID == uuid.Nil {
		return store.Site{}, fmt.Errorf("site id is required")
	}
	known := false
	for _, candidate := range SitePluginKinds {
		known = known || candidate == kind
	}
	if !known {
		return store.Site{}, fmt.Errorf("kind %q is not bound to a site", kind)
	}
	pluginID = strings.TrimSpace(pluginID)
	var updated store.Site
	err := s.db.WithinTx(ctx, func(tx store.Tx) error {
		siteRepo := store.NewSiteRepository(tx)
		existing, err := siteRepo.GetByID(ctx, siteID)
		if err != nil {
			return err
		}
		meta, err := MergeSiteGatewayConfig(existing.Meta, &GatewayConfig{Plugins: map[string]string{kind: pluginID}})
		if err != nil {
			return err
		}
		updated, err = siteRepo.Update(ctx, store.UpdateSiteParams{
			ID:              existing.ID,
			Name:            existing.Name,
			Slug:            existing.Slug,
			SiteType:        existing.SiteType,
			BaseURL:         existing.BaseURL,
			Status:          existing.Status,
			Enabled:         existing.Enabled,
			RoutingPriority: existing.RoutingPriority,
			Meta:            meta,
		})
		return err
	})
	return updated, err
}
