package site

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestSyncAPIKeyModelsForAddedGroupSitesCreatesOnlyActiveAllowListModels(t *testing.T) {
	t.Parallel()

	groupID := uuid.New()
	siteID := uuid.New()
	allowedAPIKeyID := uuid.New()
	blockedAPIKeyID := uuid.New()
	activeModelID := uuid.New()
	inactiveModelID := uuid.New()
	var created []store.APIKeySiteModelPermission
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.APIKeySiteGroupPermission:
				*dest = []store.APIKeySiteGroupPermission{
					{GroupID: groupID, APIKeyID: allowedAPIKeyID, Enabled: true},
					{GroupID: groupID, APIKeyID: blockedAPIKeyID, Enabled: true},
				}
				tx.RowsAffected = 2
			case *[]store.APIKey:
				*dest = []store.APIKey{
					{ID: allowedAPIKeyID, SitePolicy: "allow_list", ModelPolicy: "allow_list"},
					{ID: blockedAPIKeyID, SitePolicy: "allow_list", ModelPolicy: "deny_list"},
				}
				tx.RowsAffected = 2
			case *[]store.SiteModel:
				*dest = []store.SiteModel{
					{ID: activeModelID, SiteID: siteID, UpstreamName: "gpt-allow-list-active", Status: "active"},
					{ID: inactiveModelID, SiteID: siteID, UpstreamName: "gpt-allow-list-unavailable", Status: "unavailable"},
				}
				tx.RowsAffected = 2
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			items, ok := tx.Statement.Dest.(*[]store.APIKeySiteModelPermission)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			created = append(created, (*items)...)
			tx.RowsAffected = int64(len(*items))
		},
	})

	if err := syncAPIKeyModelsForAddedGroupSites(t.Context(), db, groupID, []uuid.UUID{siteID}); err != nil {
		t.Fatalf("syncAPIKeyModelsForAddedGroupSites() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("created permissions = %#v, want one permission for active allow-list model", created)
	}
	if created[0].APIKeyID != allowedAPIKeyID || created[0].SiteModelID != activeModelID || !created[0].Enabled {
		t.Fatalf("created permission = %#v, want allowed api key linked to active model", created[0])
	}
}

func TestSyncAPIKeyModelsForAddedGroupSitesPropagatesAPIKeyListError(t *testing.T) {
	t.Parallel()

	groupID := uuid.New()
	apiKeyID := uuid.New()
	queryErr := errors.New("api key list stopped")
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.APIKeySiteGroupPermission:
				*dest = []store.APIKeySiteGroupPermission{{GroupID: groupID, APIKeyID: apiKeyID, Enabled: true}}
				tx.RowsAffected = 1
			case *[]store.APIKey:
				tx.AddError(queryErr)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})

	err := syncAPIKeyModelsForAddedGroupSites(t.Context(), db, groupID, []uuid.UUID{uuid.New()})
	assertSiteWrappedQueryError(t, "syncAPIKeyModelsForAddedGroupSites", err, queryErr, "list api keys for site group model sync")
}

func TestMergePricingModelsStateSkipsUnavailableModelsAndDeduplicatesExistingIDs(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	db := siteGormWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteModel:
				*dest = store.SiteModel{ID: modelID, SiteID: siteID, UpstreamName: "priced-catalog-model", Status: "active"}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*store.SiteModel); !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			tx.RowsAffected = 1
		},
	})

	got, err := (&Service{}).mergePricingModelsState(
		t.Context(),
		store.Site{ID: siteID},
		adapter.PricingSnapshot{Items: []adapter.ModelPricing{{ModelName: "priced-catalog-model"}}},
		[]store.SiteModel{
			{ID: modelID, SiteID: siteID, UpstreamName: "adapter-duplicate-model", Status: "active"},
			{ID: uuid.New(), SiteID: siteID, UpstreamName: "adapter-unavailable-model", Status: "unavailable"},
		},
		store.NewSiteModelRepository(db),
	)
	if err != nil {
		t.Fatalf("mergePricingModelsState() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != modelID || got[0].Status != "active" {
		t.Fatalf("mergePricingModelsState() = %#v, want one active de-duplicated model", got)
	}
}

func TestSyncCanonicalPricingFallbackCreatesOnlyValidMissingCanonicalPrice(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	existingCanonicalID := uuid.New()
	missingPriceCanonicalID := uuid.New()
	validCanonicalID := uuid.New()
	noCanonicalModelID := uuid.New()
	existingModelID := uuid.New()
	missingPriceModelID := uuid.New()
	validModelID := uuid.New()

	pricingListCalls := 0
	canonicalCalls := 0
	var created store.SiteModelPricing
	createCount := 0
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteModelPricing:
				pricingListCalls++
				if pricingListCalls == 2 {
					*dest = []store.SiteModelPricing{{SiteModelID: uuid.NullUUID{UUID: existingModelID, Valid: true}, Available: true}}
					tx.RowsAffected = 1
					return
				}
				*dest = nil
				tx.RowsAffected = 0
			case *store.CanonicalModel:
				canonicalCalls++
				switch canonicalCalls {
				case 1:
					*dest = store.CanonicalModel{ID: existingCanonicalID}
				case 2:
					*dest = store.CanonicalModel{ID: missingPriceCanonicalID}
				case 3:
					*dest = store.CanonicalModel{
						ID:          validCanonicalID,
						InputPrice:  sql.NullFloat64{Float64: 1.25, Valid: true},
						OutputPrice: sql.NullFloat64{Float64: 2.5, Valid: true},
					}
				default:
					tx.AddError(gorm.ErrInvalidData)
					return
				}
				tx.RowsAffected = 1
			case *store.SiteModelPricing:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		create: func(tx *gorm.DB) {
			item, ok := tx.Statement.Dest.(*store.SiteModelPricing)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			createCount++
			created = *item
			tx.RowsAffected = 1
		},
	})

	unpriced := service.syncCanonicalPricingFallback(t.Context(), siteID, []store.SiteModel{
		{ID: noCanonicalModelID, SiteID: siteID, UpstreamName: "no-canonical-model"},
		{ID: existingModelID, SiteID: siteID, UpstreamName: "already-priced-model", CanonicalID: uuid.NullUUID{UUID: existingCanonicalID, Valid: true}},
		{ID: missingPriceModelID, SiteID: siteID, UpstreamName: "missing-canonical-price-model", CanonicalID: uuid.NullUUID{UUID: missingPriceCanonicalID, Valid: true}},
		{ID: validModelID, SiteID: siteID, UpstreamName: "canonical-priced-model", CanonicalID: uuid.NullUUID{UUID: validCanonicalID, Valid: true}},
	})

	if pricingListCalls != 4 || canonicalCalls != 3 || createCount != 1 {
		t.Fatalf("calls pricing=%d canonical=%d create=%d, want guarded fallback path to create once", pricingListCalls, canonicalCalls, createCount)
	}
	if len(unpriced) != 2 || unpriced[0] != "no-canonical-model" || unpriced[1] != "missing-canonical-price-model" {
		t.Fatalf("unpriced = %#v, want models without any usable pricing reported", unpriced)
	}
	if created.SiteID != siteID || created.ModelName != "canonical-priced-model" || created.PricingSource != "canonical_fallback" || !created.Available {
		t.Fatalf("created fallback = %#v, want canonical fallback pricing for valid model", created)
	}
	if !created.SiteModelID.Valid || created.SiteModelID.UUID != validModelID {
		t.Fatalf("created fallback site model id = %#v, want %s", created.SiteModelID, validModelID)
	}
	if !created.InputValue.Valid || created.InputValue.Float64 != 1.25 || !created.OutputValue.Valid || created.OutputValue.Float64 != 2.5 {
		t.Fatalf("created fallback prices = input %#v output %#v, want canonical prices", created.InputValue, created.OutputValue)
	}
}

func TestSyncCanonicalPricingFallbackBackfillsOnlyMissingFallbackAudioRatios(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	canonicalID := uuid.New()
	preservedAudioRatio := 3.5
	items := []store.SiteModelPricing{
		{
			ID:            uuid.New(),
			SiteID:        siteID,
			SiteModelID:   uuid.NullUUID{UUID: modelID, Valid: true},
			PricingSource: "canonical_fallback",
			Available:     true,
		},
		{
			ID:            uuid.New(),
			SiteID:        siteID,
			SiteModelID:   uuid.NullUUID{UUID: modelID, Valid: true},
			PricingSource: "canonical_fallback",
			AudioRatio:    sql.NullFloat64{Float64: preservedAudioRatio, Valid: true},
			Available:     true,
		},
		{
			ID:             uuid.New(),
			SiteID:         siteID,
			SiteModelID:    uuid.NullUUID{UUID: modelID, Valid: true},
			PricingSource:  "canonical_fallback",
			ManualOverride: true,
			Available:      true,
		},
		{
			ID:            uuid.New(),
			SiteID:        siteID,
			SiteModelID:   uuid.NullUUID{UUID: modelID, Valid: true},
			PricingSource: "upstream",
			Available:     true,
		},
	}

	var saved []store.SiteModelPricing
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteModelPricing:
				*dest = items
				tx.RowsAffected = int64(len(items))
			case *store.CanonicalModel:
				*dest = store.CanonicalModel{
					ID:                   canonicalID,
					AudioRatio:           sql.NullFloat64{Float64: 2.5, Valid: true},
					AudioCompletionRatio: sql.NullFloat64{Float64: 1, Valid: true},
				}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: siteCaptureUpdate(t, "site model pricing", func(item store.SiteModelPricing) {
			saved = append(saved, item)
		}),
	})

	unpriced := service.syncCanonicalPricingFallback(t.Context(), siteID, []store.SiteModel{{
		ID:           modelID,
		SiteID:       siteID,
		UpstreamName: "gpt-4o-mini-tts",
		CanonicalID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
	}})

	if len(unpriced) != 0 {
		t.Fatalf("unpriced = %#v, want existing fallback to remain priced", unpriced)
	}
	if len(saved) != 2 {
		t.Fatalf("saved = %#v, want only non-manual fallback rows updated", saved)
	}
	if !saved[0].AudioRatio.Valid || saved[0].AudioRatio.Float64 != 2.5 || !saved[0].AudioCompletionRatio.Valid || saved[0].AudioCompletionRatio.Float64 != 1 {
		t.Fatalf("first saved audio ratios = %#v %#v, want canonical ratios", saved[0].AudioRatio, saved[0].AudioCompletionRatio)
	}
	if !saved[1].AudioRatio.Valid || saved[1].AudioRatio.Float64 != preservedAudioRatio || !saved[1].AudioCompletionRatio.Valid || saved[1].AudioCompletionRatio.Float64 != 1 {
		t.Fatalf("second saved audio ratios = %#v %#v, want existing audio ratio preserved", saved[1].AudioRatio, saved[1].AudioCompletionRatio)
	}
}

type retireNoPricingModule struct{}

func (retireNoPricingModule) SiteTypes() []string { return []string{"codex"} }

type retireFetcherModule struct{}

func (retireFetcherModule) SiteTypes() []string { return []string{"codex"} }

func (retireFetcherModule) FetchPricing(context.Context, adapter.SiteConfig, adapter.SystemAuth) (adapter.PricingSnapshot, error) {
	return adapter.PricingSnapshot{}, nil
}

type retireParserModule struct{}

func (retireParserModule) SiteTypes() []string { return []string{"codex"} }

func (retireParserModule) ParsePricing(any) adapter.PricingSnapshot {
	return adapter.PricingSnapshot{}
}

func TestRetireSyncedPricingRowsSkipsAdaptersWithPricingCapability(t *testing.T) {
	t.Parallel()

	// Any query on the offline gorm DB surfaces as an error, so a module with
	// pricing capability must not touch the pricing tables at all.
	service := siteServiceWithQueryError(t, errors.New("unexpected pricing query"))
	for name, module := range map[string]adapter.Module{
		"pricing fetcher": retireFetcherModule{},
		"pricing parser":  retireParserModule{},
	} {
		if err := service.retireSyncedPricingRows(t.Context(), store.Site{ID: uuid.New()}, module); err != nil {
			t.Fatalf("%s: retireSyncedPricingRows() error = %v, want no repo access", name, err)
		}
	}
}

func TestRetireSyncedPricingRowsRetiresStaleNonManualRows(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	pricings := []store.SiteModelPricing{
		{ID: uuid.New(), SiteID: siteID, ModelName: "gpt-legacy-official", GroupName: "default", PricingSource: "openai_official", Available: true},
		{ID: uuid.New(), SiteID: siteID, ModelName: "gpt-manual", GroupName: "default", PricingSource: "openai_official", ManualOverride: true, Available: true},
		{ID: uuid.New(), SiteID: siteID, ModelName: "gpt-already-retired", GroupName: "default", PricingSource: "openai_official", Available: false},
	}
	groups := []store.SitePricingGroup{
		{ID: uuid.New(), SiteID: siteID, GroupName: "default", Available: true},
		{ID: uuid.New(), SiteID: siteID, GroupName: "vip", Available: false},
	}
	var savedPricings []store.SiteModelPricing
	var savedGroups []store.SitePricingGroup
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteModelPricing:
				*dest = pricings
				tx.RowsAffected = int64(len(pricings))
			case *[]store.SitePricingGroup:
				*dest = groups
				tx.RowsAffected = int64(len(groups))
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
		update: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.SiteModelPricing:
				savedPricings = append(savedPricings, *dest)
				tx.RowsAffected = 1
			case *store.SitePricingGroup:
				savedGroups = append(savedGroups, *dest)
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})

	if err := service.retireSyncedPricingRows(t.Context(), store.Site{ID: siteID}, retireNoPricingModule{}); err != nil {
		t.Fatalf("retireSyncedPricingRows() error = %v", err)
	}

	if len(savedPricings) != 1 || savedPricings[0].ModelName != "gpt-legacy-official" || savedPricings[0].Available {
		t.Fatalf("saved pricings = %#v, want only the available non-manual row retired", savedPricings)
	}
	if len(savedGroups) != 1 || savedGroups[0].GroupName != "default" || savedGroups[0].Available {
		t.Fatalf("saved groups = %#v, want only the available group retired", savedGroups)
	}
}
