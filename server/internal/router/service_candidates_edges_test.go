package router

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestCandidatesFilterSortAndLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	keptAlphaSiteID := uuid.New()
	keptBetaSiteID := uuid.New()
	excludedSiteID := uuid.New()
	canonicalID := uuid.New()
	modelKey := "openai/gpt-5-mini"
	service := routerServiceWithRepositoryRows(t, routerRepositoryRows{
		canonicalModels: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		canonicalModelsByID: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		siteModels: []store.SiteModel{
			{
				ID:              uuid.New(),
				SiteID:          keptAlphaSiteID,
				CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
				UpstreamName:    "alpha-model",
				DisplayName:     "Alpha",
				Status:          "active",
				MatchSource:     "canonical",
				MatchConfidence: 100,
				Capabilities:    store.JSON(`{"supported_endpoint_types":["openai"]}`),
			},
			{
				ID:              uuid.New(),
				SiteID:          keptBetaSiteID,
				CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
				UpstreamName:    "beta-model",
				DisplayName:     "Beta",
				Status:          "active",
				MatchSource:     "canonical",
				MatchConfidence: 100,
				Capabilities:    store.JSON(`{"supported_endpoint_types":["openai"]}`),
			},
			{
				ID:              uuid.New(),
				SiteID:          excludedSiteID,
				CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
				UpstreamName:    "image-only",
				DisplayName:     "Image",
				Status:          "active",
				MatchSource:     "canonical",
				MatchConfidence: 100,
				Capabilities:    store.JSON(`{"supported_endpoint_types":["openai-image"]}`),
			},
		},
		sites: []store.Site{
			{ID: keptAlphaSiteID, Name: "Alpha Site", Slug: "alpha", SiteType: "openai", BaseURL: "https://alpha.example", Status: "active", Enabled: true, RoutingPriority: 1.0},
			{ID: keptBetaSiteID, Name: "Beta Site", Slug: "beta", SiteType: "openai", BaseURL: "https://beta.example", Status: "active", Enabled: true, RoutingPriority: 3.0},
			{ID: excludedSiteID, Name: "Image Site", Slug: "image", SiteType: "openai", BaseURL: "https://image.example", Status: "active", Enabled: true, RoutingPriority: 10.0},
		},
		credentials: []store.SiteCredential{
			{ID: uuid.New(), SiteID: keptAlphaSiteID, CredentialType: "api_key"},
			{ID: uuid.New(), SiteID: keptBetaSiteID, CredentialType: "api_key"},
			{ID: uuid.New(), SiteID: excludedSiteID, CredentialType: "api_key"},
		},
	})

	candidates, err := service.Candidates(ctx, CandidateQuery{
		ModelKey:     modelKey,
		EndpointType: "openai-response",
		Limit:        1,
		Debug:        true,
	})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(candidates.Items) != 1 {
		t.Fatalf("candidate count = %d, want 1", len(candidates.Items))
	}
	got := candidates.Items[0]
	if got.Site.ID != keptBetaSiteID {
		t.Fatalf("selected site ID = %s, want higher-priority beta site %s", got.Site.ID, keptBetaSiteID)
	}
	if got.Rank != 1 {
		t.Fatalf("candidate rank = %d, want 1", got.Rank)
	}
	if got.Site.RoutingPriority != 3.0 {
		t.Fatalf("selected routing priority = %v, want 3.0", got.Site.RoutingPriority)
	}
	if got.ScoreBreakdown == nil {
		t.Fatalf("score breakdown = nil, want populated breakdown in debug mode")
	}
	if _, ok := got.ScoreBreakdown["routing_priority"]; ok {
		t.Fatalf("score breakdown = %#v, want no routing_priority component", got.ScoreBreakdown)
	}
}

func TestCandidatesRankHigherPriorityAboveHealthierLowerPriority(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	healthyLowPrioritySiteID := uuid.New()
	degradedHighPrioritySiteID := uuid.New()
	canonicalID := uuid.New()
	modelKey := "openai/gpt-5-mini"
	healthySuccessRate := 1.0
	degradedSuccessRate := 0.2
	service := routerServiceWithRepositoryRows(t, routerRepositoryRows{
		canonicalModels: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		canonicalModelsByID: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		siteModels: []store.SiteModel{
			{
				ID:              uuid.New(),
				SiteID:          healthyLowPrioritySiteID,
				CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
				UpstreamName:    "steady-model",
				DisplayName:     "Steady",
				Status:          "active",
				MatchSource:     "canonical",
				MatchConfidence: 100,
				Capabilities:    store.JSON(`{"supported_endpoint_types":["openai"]}`),
			},
			{
				ID:              uuid.New(),
				SiteID:          degradedHighPrioritySiteID,
				CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
				UpstreamName:    "recovering-model",
				DisplayName:     "Recovering",
				Status:          "active",
				MatchSource:     "canonical",
				MatchConfidence: 100,
				Capabilities:    store.JSON(`{"supported_endpoint_types":["openai"]}`),
			},
		},
		sites: []store.Site{
			{ID: healthyLowPrioritySiteID, Name: "Steady Site", Slug: "steady", SiteType: "openai", BaseURL: "https://steady.example", Status: "active", Enabled: true, RoutingPriority: 4.0},
			{ID: degradedHighPrioritySiteID, Name: "Recovering Site", Slug: "recovering", SiteType: "codex", BaseURL: "https://recovering.example", Status: "active", Enabled: true, RoutingPriority: 4.8},
		},
		healthStates: []store.SiteHealthState{
			{SiteID: healthyLowPrioritySiteID, Status: "healthy", RecentSuccessRate: sql.NullFloat64{Float64: healthySuccessRate, Valid: true}},
			{SiteID: degradedHighPrioritySiteID, Status: "degraded", RecentSuccessRate: sql.NullFloat64{Float64: degradedSuccessRate, Valid: true}},
		},
		credentials: []store.SiteCredential{
			{ID: uuid.New(), SiteID: healthyLowPrioritySiteID, CredentialType: "api_key"},
			{ID: uuid.New(), SiteID: degradedHighPrioritySiteID, CredentialType: "api_key"},
		},
	})

	candidates, err := service.Candidates(ctx, CandidateQuery{
		ModelKey:     modelKey,
		EndpointType: "openai",
	})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(candidates.Items) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(candidates.Items))
	}
	if candidates.Items[0].Site.ID != degradedHighPrioritySiteID {
		t.Fatalf("first candidate = %s, want degraded higher-priority site %s", candidates.Items[0].Site.ID, degradedHighPrioritySiteID)
	}
	if candidates.Items[0].Score >= candidates.Items[1].Score {
		t.Fatalf("degraded site score = %v, healthy site score = %v, want lower score ranked first by priority", candidates.Items[0].Score, candidates.Items[1].Score)
	}
}

func TestCandidatesDoNotFallbackWhenGenericModelHasUnusableBindings(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	modelKey := "openai/gpt-5-mini"
	service := routerServiceWithRepositoryRows(t, routerRepositoryRows{
		canonicalModels:     []store.CanonicalModel{{ID: canonicalID, ModelKey: modelKey, Status: "active"}},
		canonicalModelsByID: []store.CanonicalModel{{ID: canonicalID, ModelKey: modelKey, Status: "active"}},
		siteModels: []store.SiteModel{{
			ID:           siteModelID,
			SiteID:       siteID,
			CanonicalID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
			UpstreamName: "gpt-5-mini",
			Status:       "active",
			Capabilities: store.JSON(`{"supported_endpoint_types":["openai"]}`),
		}},
		sites: []store.Site{{
			ID: siteID, Name: "Generic", Slug: "generic", SiteType: "openai", BaseURL: "https://generic.example", Status: "active", Enabled: true,
		}},
		credentials: []store.SiteCredential{{ID: credentialID, SiteID: siteID, CredentialType: "api_key"}},
		apiKeyModels: []store.SiteAPIKeyModel{{
			SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: false,
		}},
	})

	candidates, err := service.Candidates(context.Background(), CandidateQuery{ModelKey: modelKey, EndpointType: "openai"})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(candidates.Items) != 0 {
		t.Fatalf("candidates = %#v, want no fallback when the model has an unusable binding", candidates.Items)
	}
}

func TestCandidatesDemoteTransientCooldownAndFilterHardAndCredentialCooldowns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	steadySiteID := uuid.New()
	coolingSiteID := uuid.New()
	notFoundSiteID := uuid.New()
	keyCooledSiteID := uuid.New()
	coolingModelID := uuid.New()
	notFoundModelID := uuid.New()
	keyCooledCredentialID := uuid.New()
	canonicalID := uuid.New()
	modelKey := "openai/gpt-5-mini"
	activeUntil := time.Now().Add(time.Hour)
	siteModel := func(siteID uuid.UUID, modelID uuid.UUID, name string) store.SiteModel {
		return store.SiteModel{
			ID:              modelID,
			SiteID:          siteID,
			CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
			UpstreamName:    name,
			DisplayName:     name,
			Status:          "active",
			MatchSource:     "canonical",
			MatchConfidence: 100,
			Capabilities:    store.JSON(`{"supported_endpoint_types":["openai"]}`),
		}
	}
	service := routerServiceWithRepositoryRows(t, routerRepositoryRows{
		canonicalModels: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		canonicalModelsByID: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		siteModels: []store.SiteModel{
			siteModel(steadySiteID, uuid.New(), "steady-model"),
			siteModel(coolingSiteID, coolingModelID, "cooling-model"),
			siteModel(notFoundSiteID, notFoundModelID, "missing-model"),
			siteModel(keyCooledSiteID, uuid.New(), "key-cooled-model"),
		},
		sites: []store.Site{
			{ID: steadySiteID, Name: "Steady Site", Slug: "steady", SiteType: "openai", BaseURL: "https://steady.example", Status: "active", Enabled: true, RoutingPriority: 1.0},
			{ID: coolingSiteID, Name: "Cooling Site", Slug: "cooling", SiteType: "openai", BaseURL: "https://cooling.example", Status: "active", Enabled: true, RoutingPriority: 5.0},
			{ID: notFoundSiteID, Name: "Missing Site", Slug: "missing", SiteType: "openai", BaseURL: "https://missing.example", Status: "active", Enabled: true, RoutingPriority: 5.0},
			{ID: keyCooledSiteID, Name: "Key Cooled Site", Slug: "key-cooled", SiteType: "openai", BaseURL: "https://key-cooled.example", Status: "active", Enabled: true, RoutingPriority: 5.0},
		},
		credentials: []store.SiteCredential{
			{ID: uuid.New(), SiteID: steadySiteID, CredentialType: "api_key"},
			{ID: uuid.New(), SiteID: coolingSiteID, CredentialType: "api_key"},
			{ID: uuid.New(), SiteID: notFoundSiteID, CredentialType: "api_key"},
			{ID: keyCooledCredentialID, SiteID: keyCooledSiteID, CredentialType: "api_key"},
		},
		cooldowns: []store.RouteCooldown{
			{
				ID:          uuid.New(),
				SiteID:      coolingSiteID,
				SiteModelID: uuid.NullUUID{UUID: coolingModelID, Valid: true},
				Source:      "gateway",
				Reason:      "upstream_timeout",
				ActiveUntil: activeUntil,
			},
			{
				ID:          uuid.New(),
				SiteID:      notFoundSiteID,
				SiteModelID: uuid.NullUUID{UUID: notFoundModelID, Valid: true},
				Source:      "gateway",
				Reason:      store.CooldownReasonUpstreamModelNotFound,
				ActiveUntil: activeUntil,
			},
			{
				ID:               uuid.New(),
				SiteID:           keyCooledSiteID,
				SiteCredentialID: uuid.NullUUID{UUID: keyCooledCredentialID, Valid: true},
				Source:           "gateway",
				Reason:           "upstream_credential_rate_limited",
				ActiveUntil:      activeUntil,
			},
		},
	})

	candidates, err := service.Candidates(ctx, CandidateQuery{
		ModelKey:     modelKey,
		EndpointType: "openai",
	})
	if err != nil {
		t.Fatalf("Candidates error = %v", err)
	}
	if len(candidates.Items) != 2 {
		t.Fatalf("candidate count = %d, want steady and half-open cooling candidates, got %#v", len(candidates.Items), candidates.Items)
	}
	if candidates.Items[0].Site.ID != steadySiteID || candidates.Items[0].Cooling {
		t.Fatalf("first candidate = %s cooling=%v, want healthy steady site ranked above higher-priority cooling site", candidates.Items[0].Site.ID, candidates.Items[0].Cooling)
	}
	if candidates.Items[1].Site.ID != coolingSiteID || !candidates.Items[1].Cooling {
		t.Fatalf("second candidate = %s cooling=%v, want half-open cooling site demoted to last", candidates.Items[1].Site.ID, candidates.Items[1].Cooling)
	}
	if candidates.Items[1].CoolingReason != "upstream_timeout" {
		t.Fatalf("cooling reason = %q, want upstream_timeout", candidates.Items[1].CoolingReason)
	}
}

func TestPlanUsesDefaultAndExplicitFailoverLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	canonicalID := uuid.New()
	modelKey := "openai/gpt-5-mini"
	rows := routerPlanRepositoryRows(canonicalID, modelKey, []string{"selected", "failover-1", "failover-2", "failover-3", "failover-4"})
	service := routerServiceWithRepositoryRows(t, rows)

	defaultPlan, err := service.Plan(ctx, CandidateQuery{ModelKey: "openai/gpt-5-mini"})
	if err != nil {
		t.Fatalf("Plan default error = %v", err)
	}
	if defaultPlan.Selected.Site.Name != "selected" || len(defaultPlan.Failover) != 3 {
		t.Fatalf("default plan = %#v, want selected with 3 failovers", defaultPlan)
	}

	explicitPlan, err := service.Plan(ctx, CandidateQuery{ModelKey: "openai/gpt-5-mini", FailoverLimit: 2})
	if err != nil {
		t.Fatalf("Plan explicit error = %v", err)
	}
	if len(explicitPlan.Failover) != 2 || explicitPlan.Failover[1].Site.Name != "failover-2" {
		t.Fatalf("explicit plan failover = %#v, want first two failovers", explicitPlan.Failover)
	}
}

func TestCandidatesReturnsCooldownRepositoryError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	canonicalID := uuid.New()
	modelKey := "openai/gpt-5-mini"
	cooldownErr := errors.New("cooldown repository offline")
	service := routerServiceWithRepositoryRows(t, routerRepositoryRows{
		errOnRouteCooldowns: cooldownErr,
		canonicalModels: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		canonicalModelsByID: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
	})

	_, err := service.Candidates(ctx, CandidateQuery{ModelKey: modelKey})
	if !errors.Is(err, cooldownErr) || !strings.Contains(err.Error(), "list active route cooldowns") {
		t.Fatalf("Candidates error = %v, want wrapped cooldown repository error", err)
	}
}

type routerRepositoryRows struct {
	errOnRouteCooldowns error
	canonicalModels     []store.CanonicalModel
	canonicalModelsByID []store.CanonicalModel
	siteModels          []store.SiteModel
	sites               []store.Site
	healthStates        []store.SiteHealthState
	healthSnapshots     []store.HealthSnapshot
	apiKeyModels        []store.SiteAPIKeyModel
	credentials         []store.SiteCredential
	keyStates           []store.SiteAPIKeyState
	pricings            []store.SiteModelPricing
	cooldowns           []store.RouteCooldown
}

func routerPlanRepositoryRows(canonicalID uuid.UUID, modelKey string, siteNames []string) routerRepositoryRows {
	rows := routerRepositoryRows{
		canonicalModels: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
		canonicalModelsByID: []store.CanonicalModel{
			{ID: canonicalID, ModelKey: modelKey, Status: "active"},
		},
	}
	for index, name := range siteNames {
		siteID := uuid.New()
		rows.siteModels = append(rows.siteModels, store.SiteModel{
			ID:              uuid.New(),
			SiteID:          siteID,
			CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
			UpstreamName:    name,
			DisplayName:     name,
			Status:          "active",
			MatchSource:     "canonical",
			MatchConfidence: 100,
			Capabilities:    store.JSON(`{"supported_endpoint_types":["openai"]}`),
		})
		rows.sites = append(rows.sites, store.Site{
			ID:              siteID,
			Name:            name,
			Slug:            name,
			SiteType:        "openai",
			BaseURL:         "https://" + name + ".example",
			Status:          "active",
			Enabled:         true,
			RoutingPriority: float64(len(siteNames) - index),
		})
		rows.credentials = append(rows.credentials, store.SiteCredential{
			ID:             uuid.New(),
			SiteID:         siteID,
			CredentialType: "api_key",
		})
	}
	return rows
}

func routerServiceWithRepositoryRows(t *testing.T, rows routerRepositoryRows) *Service {
	t.Helper()

	db := routerOfflinePostgresGorm(t)
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "RouteCooldown" && rows.errOnRouteCooldowns != nil {
			tx.AddError(rows.errOnRouteCooldowns)
			return
		}
		routerAssignRepositoryRows(tx, rows)
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	return NewService(routerStoreBackedByGorm(t, db))
}

func routerAssignRepositoryRows(tx *gorm.DB, rows routerRepositoryRows) {
	if tx.Statement.Schema == nil {
		return
	}
	switch tx.Statement.Schema.Name {
	case "CanonicalModel":
		target := rows.canonicalModels
		if len(tx.Statement.Clauses) > 0 {
			target = rows.canonicalModelsByID
		}
		routerAssignRowsToDestination(tx.Statement.Dest, target)
	case "SiteModel":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.siteModels)
	case "Site":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.sites)
	case "SiteHealthState":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.healthStates)
	case "HealthSnapshot":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.healthSnapshots)
	case "SiteAPIKeyModel":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.apiKeyModels)
	case "SiteCredential":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.credentials)
	case "SiteAPIKeyState":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.keyStates)
	case "SiteModelPricing":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.pricings)
	case "RouteCooldown":
		routerAssignRowsToDestination(tx.Statement.Dest, rows.cooldowns)
	}
}

func routerAssignRowsToDestination(dest any, value any) {
	out := reflect.ValueOf(dest)
	if out.Kind() != reflect.Ptr || out.IsNil() {
		return
	}
	out = out.Elem()
	if !out.CanSet() {
		return
	}
	source := reflect.ValueOf(value)
	if source.Type().AssignableTo(out.Type()) {
		out.Set(source)
		return
	}
	if out.Kind() == reflect.Struct && source.Kind() == reflect.Slice && source.Len() > 0 {
		item := source.Index(0)
		if item.Type().AssignableTo(out.Type()) {
			out.Set(item)
		}
	}
}
