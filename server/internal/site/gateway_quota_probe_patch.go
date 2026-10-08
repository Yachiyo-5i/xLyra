package site

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

// PatchGatewayQuotaProbe updates only gateway_config.quota_probe for a site.
func (s *Service) PatchGatewayQuotaProbe(ctx context.Context, siteID uuid.UUID, quotaProbe string) (store.Site, error) {
	if s == nil || s.db == nil {
		return store.Site{}, fmt.Errorf("site service is not available")
	}
	if siteID == uuid.Nil {
		return store.Site{}, fmt.Errorf("site id is required")
	}
	probeType, err := NormalizeQuotaProbeType(quotaProbe)
	if err != nil {
		return store.Site{}, err
	}
	var updated store.Site
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		siteRepo := store.NewSiteRepository(tx)
		existing, err := siteRepo.GetByID(ctx, siteID)
		if err != nil {
			return err
		}
		patch := &GatewayConfig{}
		if probeType != "" {
			patch.QuotaProbe = &probeType
		} else {
			cleared := ""
			patch.QuotaProbe = &cleared
		}
		meta, err := MergeSiteGatewayConfig(existing.Meta, patch)
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
