package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type openAIKeyPricingGroup struct {
	target SiteModelPricing
	source SiteModelPricing
	rows   []SiteModelPricing
}

func ensureOpenAIKeyPricingUpgrade(ctx context.Context, db *gorm.DB, migrate bool) error {
	var marker schemaUpgradeMarker
	err := db.WithContext(ctx).Where(&schemaUpgradeMarker{Name: openAIKeyPricingUpgradeMarker}).First(&marker).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check openai key pricing upgrade marker: %w", err)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if migrate {
			if err := migrateOpenAIKeyPricing(ctx, tx); err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaUpgradeMarker{
			Name:        openAIKeyPricingUpgradeMarker,
			CompletedAt: time.Now(),
		}).Error
	}); err != nil {
		return fmt.Errorf("upgrade openai key pricing: %w", err)
	}
	return nil
}

func migrateOpenAIKeyPricing(ctx context.Context, db *gorm.DB) error {
	sites, err := NewSiteRepository(db).List(ctx)
	if err != nil {
		return err
	}
	credentials, err := NewSiteCredentialRepository(db).ListAll(ctx)
	if err != nil {
		return err
	}
	pricings, err := NewSiteModelPricingRepository(db).ListAll(ctx)
	if err != nil {
		return err
	}

	openAISites := make(map[uuid.UUID]struct{})
	for _, site := range sites {
		if strings.EqualFold(strings.TrimSpace(site.SiteType), "openai") || strings.TrimSpace(site.SiteType) == "" {
			openAISites[site.ID] = struct{}{}
		}
	}
	credentialsBySite := make(map[uuid.UUID][]SiteCredential)
	for _, credential := range credentials {
		if _, ok := openAISites[credential.SiteID]; ok && isAPIKeyCredentialType(credential.CredentialType) {
			credentialsBySite[credential.SiteID] = append(credentialsBySite[credential.SiteID], credential)
		}
	}
	pricingsBySite := make(map[uuid.UUID][]SiteModelPricing)
	for _, pricing := range pricings {
		if _, ok := openAISites[pricing.SiteID]; ok {
			pricingsBySite[pricing.SiteID] = append(pricingsBySite[pricing.SiteID], pricing)
		}
	}

	credentialRepo := NewSiteCredentialRepository(db)
	pricingRepo := NewSiteModelPricingRepository(db)
	for siteID, sitePricings := range pricingsBySite {
		groups := openAIKeyPricingGroups(sitePricings)
		multiplier, useKeyMultiplier := sharedOpenAIKeyPricingMultiplier(groups)
		if useKeyMultiplier && len(credentialsBySite[siteID]) > 0 {
			for _, credential := range credentialsBySite[siteID] {
				if _, err := credentialRepo.UpdateRoutingConfig(ctx, credential.ID, UpdateSiteCredentialRoutingConfigParams{UpstreamCostMultiplier: &multiplier}); err != nil {
					return err
				}
			}
		} else {
			useKeyMultiplier = false
		}
		for _, group := range groups {
			normalized := group.source
			normalized.ID = group.target.ID
			normalized.CreatedAt = group.target.CreatedAt
			normalized.GroupName = "default"
			if !useKeyMultiplier {
				scaleOpenAIKeyPricing(&normalized, normalized.GroupRatio)
			}
			normalized.GroupRatio = 1
			if err := pricingRepo.Save(ctx, normalized); err != nil {
				return err
			}
			for _, row := range group.rows {
				if row.ID == group.target.ID {
					continue
				}
				row.Available = false
				if err := pricingRepo.Save(ctx, row); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func openAIKeyPricingGroups(pricings []SiteModelPricing) []openAIKeyPricingGroup {
	byModel := make(map[string][]SiteModelPricing)
	for _, pricing := range pricings {
		key := strings.ToLower(strings.TrimSpace(pricing.ModelName))
		byModel[key] = append(byModel[key], pricing)
	}
	keys := make([]string, 0, len(byModel))
	for key := range byModel {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	groups := make([]openAIKeyPricingGroup, 0, len(keys))
	for _, key := range keys {
		rows := byModel[key]
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Available != rows[j].Available {
				return rows[i].Available
			}
			leftDefault := strings.EqualFold(strings.TrimSpace(rows[i].GroupName), "default")
			rightDefault := strings.EqualFold(strings.TrimSpace(rows[j].GroupName), "default")
			if leftDefault != rightDefault {
				return leftDefault
			}
			if rows[i].GroupName != rows[j].GroupName {
				return rows[i].GroupName < rows[j].GroupName
			}
			return rows[i].ID.String() < rows[j].ID.String()
		})
		source := rows[0]
		target := source
		for _, row := range rows {
			if strings.EqualFold(strings.TrimSpace(row.GroupName), "default") {
				target = row
				break
			}
		}
		groups = append(groups, openAIKeyPricingGroup{target: target, source: source, rows: rows})
	}
	return groups
}

func sharedOpenAIKeyPricingMultiplier(groups []openAIKeyPricingGroup) (float64, bool) {
	var multiplier float64
	found := false
	for _, group := range groups {
		if !group.source.Available {
			continue
		}
		ratio := group.source.GroupRatio
		if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0.01 || ratio > 100 || math.Abs(ratio*10000-math.Round(ratio*10000)) > 1e-9 {
			return 0, false
		}
		if !found {
			multiplier = ratio
			found = true
			continue
		}
		if math.Abs(multiplier-ratio) > 1e-9 {
			return 0, false
		}
	}
	return multiplier, found
}

func scaleOpenAIKeyPricing(pricing *SiteModelPricing, ratio float64) {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		ratio = 1
	}
	pricing.InputValue = scaledNullFloat(pricing.InputValue, ratio)
	pricing.OutputValue = scaledNullFloat(pricing.OutputValue, ratio)
	pricing.PerRequestValue = scaledNullFloat(pricing.PerRequestValue, ratio)
	pricing.ModelPrice = scaledNullFloat(pricing.ModelPrice, ratio)
}

func scaledNullFloat(value sql.NullFloat64, ratio float64) sql.NullFloat64 {
	if value.Valid {
		value.Float64 *= ratio
	}
	return value
}
