package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestDevPostgresReadOnlyRepositoriesSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := devPostgresSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	gormDB := db.DB()
	missingID := uuid.New()

	sites, err := NewSiteRepository(gormDB).ListIncludingDeleted(ctx)
	if err != nil {
		t.Fatalf("list sites including deleted: %v", err)
	}
	assertSitesNewestFirst(t, sites)

	smokeSiteID := missingID
	if len(sites) > 0 {
		smokeSiteID = sites[0].ID
	}

	siteStates := NewSiteStateRepository(gormDB)
	stateRows, err := siteStates.List(ctx)
	if err != nil {
		t.Fatalf("list site states: %v", err)
	}
	assertSiteStateMapStable(t, stateRows)
	_, err = siteStates.GetBySite(ctx, missingID)
	assertSmokeRecordNotFound(t, err)

	health := NewHealthRepository(gormDB)
	healthStates, err := health.ListSiteStates(ctx)
	if err != nil {
		t.Fatalf("list site health states: %v", err)
	}
	assertSiteHealthStateMapStable(t, healthStates)
	_, err = health.GetSiteState(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	snapshots, err := health.ListSiteSnapshots(ctx, missingID, 3)
	if err != nil {
		t.Fatalf("list missing site health snapshots: %v", err)
	}
	if len(snapshots) != 0 {
		t.Fatalf("missing site health snapshots length = %d, want 0", len(snapshots))
	}
	schedulerSnapshots, err := health.ListSiteSnapshotsBySource(ctx, missingID, 3, "scheduler")
	if err != nil {
		t.Fatalf("list missing scheduler health snapshots: %v", err)
	}
	if len(schedulerSnapshots) != 0 {
		t.Fatalf("missing scheduler health snapshots length = %d, want 0", len(schedulerSnapshots))
	}
	hourlyBuckets, err := health.ListSiteHourlyBuckets(ctx, missingID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("list missing site health hourly buckets: %v", err)
	}
	if len(hourlyBuckets) != 0 {
		t.Fatalf("missing site health hourly buckets length = %d, want 0", len(hourlyBuckets))
	}

	groups := NewSiteGroupRepository(gormDB)
	groupRows, err := groups.List(ctx)
	if err != nil {
		t.Fatalf("list site groups: %v", err)
	}
	assertSiteGroupsSorted(t, groupRows)
	if len(groupRows) > 0 {
		group, err := groups.GetByID(ctx, groupRows[0].ID)
		if err != nil {
			t.Fatalf("get site group: %v", err)
		}
		if group.ID != groupRows[0].ID {
			t.Fatalf("site group id = %s, want %s", group.ID, groupRows[0].ID)
		}
	}
	_, err = groups.GetByID(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	groupSites, err := groups.ListGroupSites(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site group sites: %v", err)
	}
	if len(groupSites) != 0 {
		t.Fatalf("missing site group sites length = %d, want 0", len(groupSites))
	}
	allGroupSites, err := groups.ListAllGroupSites(ctx)
	if err != nil {
		t.Fatalf("list all site group sites: %v", err)
	}
	assertSiteGroupSitesStable(t, allGroupSites)

	access := NewAPIKeyAccessRepository(gormDB)
	enabledSiteIDs, err := access.EnabledSiteIDsForGroups(ctx, siteGroupIDsForSmoke(groupRows))
	if err != nil {
		t.Fatalf("enabled site ids for groups: %v", err)
	}
	assertUUIDsNonNil(t, enabledSiteIDs)
	assertEmptyAPIKeyAccessReads(t, ctx, access, missingID)

	credentials := NewSiteCredentialRepository(gormDB)
	credentialRows, err := credentials.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all site credentials: %v", err)
	}
	assertSiteCredentialsSorted(t, credentialRows)
	if len(credentialRows) > 0 {
		credential, err := credentials.GetByID(ctx, credentialRows[0].ID)
		if err != nil {
			t.Fatalf("get site credential: %v", err)
		}
		if credential.ID != credentialRows[0].ID {
			t.Fatalf("site credential id = %s, want %s", credential.ID, credentialRows[0].ID)
		}
	}
	_, err = credentials.GetByID(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	_, err = credentials.GetBySiteAndType(ctx, missingID, "api_key")
	assertSmokeRecordNotFound(t, err)
	siteCredentials, err := credentials.ListBySite(ctx, smokeSiteID)
	if err != nil {
		t.Fatalf("list site credentials: %v", err)
	}
	assertSiteCredentialsBySiteSorted(t, siteCredentials)
	missingSiteCredentials, err := credentials.ListBySite(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site credentials: %v", err)
	}
	if len(missingSiteCredentials) != 0 {
		t.Fatalf("missing site credentials length = %d, want 0", len(missingSiteCredentials))
	}

	pricingGroups := NewSitePricingGroupRepository(gormDB)
	missingPricingGroups, err := pricingGroups.ListBySite(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site pricing groups: %v", err)
	}
	if len(missingPricingGroups) != 0 {
		t.Fatalf("missing site pricing groups length = %d, want 0", len(missingPricingGroups))
	}
	sitePricingGroups, err := pricingGroups.ListBySite(ctx, smokeSiteID)
	if err != nil {
		t.Fatalf("list site pricing groups: %v", err)
	}
	assertSitePricingGroupsSorted(t, sitePricingGroups)

	modelPricings := NewSiteModelPricingRepository(gormDB)
	modelPricingRows, err := modelPricings.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all site model pricings: %v", err)
	}
	assertSiteModelPricingsSorted(t, modelPricingRows)
	siteModelPricingRows, err := modelPricings.ListBySite(ctx, smokeSiteID)
	if err != nil {
		t.Fatalf("list site model pricings: %v", err)
	}
	assertSiteModelPricingsSorted(t, siteModelPricingRows)
	missingModelPricings, err := modelPricings.ListBySiteModelID(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site model pricings by model: %v", err)
	}
	if len(missingModelPricings) != 0 {
		t.Fatalf("missing site model pricings length = %d, want 0", len(missingModelPricings))
	}

	apiKeyModels := NewSiteAPIKeyModelRepository(gormDB)
	apiKeyModelRows, err := apiKeyModels.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all site api key models: %v", err)
	}
	assertSiteAPIKeyModelsSorted(t, apiKeyModelRows)
	missingAPIKeyModels, err := apiKeyModels.ListBySite(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site api key models: %v", err)
	}
	if len(missingAPIKeyModels) != 0 {
		t.Fatalf("missing site api key models length = %d, want 0", len(missingAPIKeyModels))
	}
	missingCredentialModels, err := apiKeyModels.ListByCredential(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing credential site api key models: %v", err)
	}
	if len(missingCredentialModels) != 0 {
		t.Fatalf("missing credential site api key models length = %d, want 0", len(missingCredentialModels))
	}

	apiKeyStates := NewSiteAPIKeyStateRepository(gormDB)
	apiKeyStateRows, err := apiKeyStates.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all site api key states: %v", err)
	}
	assertSiteAPIKeyStatesSorted(t, apiKeyStateRows)
	missingAPIKeyStates, err := apiKeyStates.ListBySite(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing site api key states: %v", err)
	}
	if len(missingAPIKeyStates) != 0 {
		t.Fatalf("missing site api key states length = %d, want 0", len(missingAPIKeyStates))
	}

	assertRouteReadModelsStable(t, ctx, gormDB, missingID)
	assertGatewayReadModelsStable(t, ctx, gormDB, missingID)
	assertAPIKeyReadModelsStable(t, ctx, gormDB, missingID)
	assertCanonicalModelReadModelsStable(t, ctx, gormDB, missingID, "xlyra-smoke-missing-"+missingID.String())
	assertSiteReadModelsStable(t, ctx, gormDB, missingID, "xlyra-smoke-missing-"+missingID.String())
	assertRequestLogReadModelsStable(t, ctx, gormDB, missingID)
}

func assertRouteReadModelsStable(t *testing.T, ctx context.Context, db *gorm.DB, missingID uuid.UUID) {
	t.Helper()

	since := time.Now().Add(-24 * time.Hour)
	insights := NewRouteInsightRepository(db)
	overviewRows, err := insights.ListOverview(ctx, since)
	if err != nil {
		t.Fatalf("list route overview: %v", err)
	}
	assertRouteOverviewRowsStable(t, overviewRows)

	availabilityRows, err := insights.ListCandidateAvailability(ctx, since)
	if err != nil {
		t.Fatalf("list route candidate availability: %v", err)
	}
	assertRouteOverviewRowsStable(t, availabilityRows)

	candidates, err := NewRouteCandidateRepository(db).ListByCanonicalModel(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	if candidates != nil {
		t.Fatalf("missing canonical route candidates = %#v, want nil on record-not-found", candidates)
	}

	cooldowns, err := NewRouteCooldownRepository(db).ListActive(ctx, time.Now())
	if err != nil {
		t.Fatalf("list active route cooldowns: %v", err)
	}
	assertRouteCooldownsSorted(t, cooldowns)
}

func assertGatewayReadModelsStable(t *testing.T, ctx context.Context, db *gorm.DB, missingID uuid.UUID) {
	t.Helper()

	repo := NewGatewayRepository(db)
	credentials, err := repo.ListCredentialsForSiteModel(ctx, missingID, missingID)
	if err != nil {
		t.Fatalf("list missing gateway credentials for site model: %v", err)
	}
	if len(credentials) != 0 {
		t.Fatalf("missing gateway credentials length = %d, want 0", len(credentials))
	}
	_, err = repo.GetCredentialForSiteModel(ctx, missingID, missingID)
	assertSmokeRecordNotFound(t, err)

	_, err = repo.GetFallbackCredentialForSite(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	_, err = repo.GetPricingForSiteModel(ctx, missingID, " default ")
	assertSmokeRecordNotFound(t, err)
	if repo.canonicalHasAvailableRoute(ctx, missingID) {
		t.Fatalf("missing canonical model %s should not have an available route", missingID)
	}
	if items := mustCredentialsForSiteModel(ctx, repo, missingID, missingID); len(items) != 0 {
		t.Fatalf("missing route credentials length = %d, want 0", len(items))
	}
	_, err = repo.ListModelsForAPIKey(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
}

func assertAPIKeyReadModelsStable(t *testing.T, ctx context.Context, db *gorm.DB, missingID uuid.UUID) {
	t.Helper()

	repo := NewAPIKeyRepository(db)
	missingHash := "xlyra-smoke-missing-" + missingID.String()
	_, err := repo.GetByID(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	_, err = repo.GetActiveByHash(ctx, missingHash, time.Now())
	assertSmokeRecordNotFound(t, err)
	_, err = repo.GetActiveGeneratedByHash(ctx, missingHash, time.Now())
	assertSmokeRecordNotFound(t, err)

	exists, err := repo.ExistsByHash(ctx, missingHash)
	if err != nil {
		t.Fatalf("check missing api key hash: %v", err)
	}
	if exists {
		t.Fatal("missing api key hash should not exist")
	}
	generatedExists, err := repo.ExistsGeneratedByHash(ctx, missingHash)
	if err != nil {
		t.Fatalf("check missing generated api key hash: %v", err)
	}
	if generatedExists {
		t.Fatal("missing generated api key hash should not exist")
	}

	sitePermissions, err := repo.ListSitePermissions(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing api key site permissions: %v", err)
	}
	if len(sitePermissions) != 0 {
		t.Fatalf("missing api key site permissions length = %d, want 0", len(sitePermissions))
	}
	allowed, err := repo.IsSiteAllowed(ctx, missingID, missingID)
	if err != nil {
		t.Fatalf("check missing api key site permission: %v", err)
	}
	if allowed {
		t.Fatal("missing api key should not be allowed for missing site")
	}
	allowedSiteIDs, err := repo.AllowedSiteIDs(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing api key allowed site ids: %v", err)
	}
	if len(allowedSiteIDs) != 0 {
		t.Fatalf("missing api key allowed site ids length = %d, want 0", len(allowedSiteIDs))
	}
}

func assertCanonicalModelReadModelsStable(t *testing.T, ctx context.Context, db *gorm.DB, missingID uuid.UUID, missingKey string) {
	t.Helper()

	repo := NewCanonicalModelRepository(db)
	_, err := repo.GetByKey(ctx, missingKey)
	assertSmokeRecordNotFound(t, err)
	_, err = repo.GetByNormalizedAlias(ctx, missingKey)
	assertSmokeRecordNotFound(t, err)

	aliases, err := repo.ListAliases(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing canonical model aliases: %v", err)
	}
	if len(aliases) != 0 {
		t.Fatalf("missing canonical model aliases length = %d, want 0", len(aliases))
	}

	matrix, err := repo.Matrix(ctx, missingID)
	if err != nil {
		t.Fatalf("missing canonical model matrix: %v", err)
	}
	if len(matrix) != 0 {
		t.Fatalf("missing canonical model matrix length = %d, want 0", len(matrix))
	}

	models, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list canonical models: %v", err)
	}
	if len(models) == 0 {
		return
	}

	model := models[0].CanonicalModel
	byKey, err := repo.GetByKey(ctx, model.ModelKey)
	if err != nil {
		t.Fatalf("get canonical model by key: %v", err)
	}
	if byKey.ID != model.ID {
		t.Fatalf("canonical model by key id = %s, want %s", byKey.ID, model.ID)
	}

	modelAliases, err := repo.ListAliases(ctx, model.ID)
	if err != nil {
		t.Fatalf("list canonical model aliases: %v", err)
	}
	assertCanonicalModelAliasesSorted(t, modelAliases)
	if len(modelAliases) > 0 {
		byAlias, err := repo.GetByNormalizedAlias(ctx, modelAliases[0].NormalizedAlias)
		if err != nil {
			t.Fatalf("get canonical model by alias: %v", err)
		}
		if byAlias.ID != model.ID {
			t.Fatalf("canonical model by alias id = %s, want %s", byAlias.ID, model.ID)
		}
	}

	modelMatrix, err := repo.Matrix(ctx, model.ID)
	if err != nil {
		t.Fatalf("canonical model matrix: %v", err)
	}
	assertCanonicalModelMatrixStable(t, modelMatrix)
}

func assertSiteReadModelsStable(t *testing.T, ctx context.Context, db *gorm.DB, missingID uuid.UUID, missingKey string) {
	t.Helper()

	repo := NewSiteRepository(db)
	_, err := repo.GetByID(ctx, missingID)
	assertSmokeRecordNotFound(t, err)
	_, err = repo.FindBySlug(ctx, missingKey)
	assertSmokeRecordNotFound(t, err)
	_, err = repo.FindOAuthSite(ctx, missingKey, missingKey, missingKey+"@example.invalid")
	assertSmokeRecordNotFound(t, err)

	if rows, err := repo.ListByIDsIncludingDeleted(ctx, nil); err != nil {
		t.Fatalf("list nil site ids including deleted: %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("nil site ids length = %d, want 0", len(rows))
	}
	if rows, err := repo.ListByIDsIncludingDeleted(ctx, []uuid.UUID{missingID}); err != nil {
		t.Fatalf("list missing site ids including deleted: %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("missing site ids length = %d, want 0", len(rows))
	}

	sites, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list active sites: %v", err)
	}
	if len(sites) == 0 {
		return
	}

	site := sites[0]
	byID, err := repo.GetByID(ctx, site.ID)
	if err != nil {
		t.Fatalf("get site by id: %v", err)
	}
	if byID.ID != site.ID {
		t.Fatalf("site by id = %s, want %s", byID.ID, site.ID)
	}
	bySlug, err := repo.FindBySlug(ctx, site.Slug)
	if err != nil {
		t.Fatalf("find site by slug: %v", err)
	}
	if bySlug.ID != site.ID {
		t.Fatalf("site by slug id = %s, want %s", bySlug.ID, site.ID)
	}
	byIDs, err := repo.ListByIDsIncludingDeleted(ctx, []uuid.UUID{site.ID, missingID, site.ID})
	if err != nil {
		t.Fatalf("list site ids including deleted: %v", err)
	}
	assertSitesMatchRequestedIDs(t, byIDs, map[uuid.UUID]bool{site.ID: true, missingID: true})
}

func assertRequestLogReadModelsStable(t *testing.T, ctx context.Context, db *gorm.DB, missingID uuid.UUID) {
	t.Helper()

	repo := NewRequestLogRepository(db)
	oldest, err := repo.OldestCreatedAt(ctx)
	if err != nil {
		t.Fatalf("oldest request log: %v", err)
	}
	if oldest != nil && oldest.After(time.Now().Add(time.Minute)) {
		t.Fatalf("oldest request log created_at is in the future: %s", oldest.Format(time.RFC3339Nano))
	}

	missingSummary, err := repo.CostSummary(ctx, ListRequestLogsParams{SiteID: &missingID})
	if err != nil {
		t.Fatalf("missing site request log cost summary: %v", err)
	}
	if missingSummary.TotalCost != 0 || missingSummary.TotalTokens != 0 {
		t.Fatalf("missing site cost summary = %#v, want zero totals", missingSummary)
	}

	future := time.Now().Add(24 * time.Hour)
	allSummary, err := repo.CostSummary(ctx, ListRequestLogsParams{CreatedFrom: &future})
	if err != nil {
		t.Fatalf("request log cost summary: %v", err)
	}
	if allSummary.TotalCost < 0 || allSummary.TotalTokens < 0 {
		t.Fatalf("request log cost summary should be non-negative: %#v", allSummary)
	}

	emptySiteIDs, err := repo.SiteIDsWithRequests(ctx, nil)
	if err != nil {
		t.Fatalf("nil site ids with requests: %v", err)
	}
	if len(emptySiteIDs) != 0 {
		t.Fatalf("nil site ids with requests length = %d, want 0", len(emptySiteIDs))
	}
	missingSiteIDs, err := repo.SiteIDsWithRequests(ctx, []uuid.UUID{uuid.Nil, missingID, missingID})
	if err != nil {
		t.Fatalf("missing site ids with requests: %v", err)
	}
	if len(missingSiteIDs) != 0 {
		t.Fatalf("missing site ids with requests length = %d, want 0", len(missingSiteIDs))
	}

	sites, err := NewSiteRepository(db).List(ctx)
	if err != nil {
		t.Fatalf("list active sites for request logs: %v", err)
	}
	siteIDs := siteIDsForReadSmoke(sites)
	withRequests, err := repo.SiteIDsWithRequests(ctx, append(siteIDs, missingID))
	if err != nil {
		t.Fatalf("site ids with requests: %v", err)
	}
	assertSiteIDsWithRequestsStable(t, withRequests, append(siteIDs, missingID))
}

func assertSiteStateMapStable(t *testing.T, rows map[uuid.UUID]SiteState) {
	t.Helper()
	for siteID, row := range rows {
		if siteID == uuid.Nil || row.SiteID == uuid.Nil || siteID != row.SiteID {
			t.Fatalf("site state map key mismatch: key=%s row=%#v", siteID, row)
		}
		if row.APIKeyCount < 0 || row.ModelCount < 0 {
			t.Fatalf("site state counts should be non-negative: %#v", row)
		}
	}
}

func assertSiteHealthStateMapStable(t *testing.T, rows map[uuid.UUID]SiteHealthState) {
	t.Helper()
	for siteID, row := range rows {
		if siteID == uuid.Nil || row.SiteID == uuid.Nil || siteID != row.SiteID {
			t.Fatalf("site health state map key mismatch: key=%s row=%#v", siteID, row)
		}
		if row.ConsecutiveFailures < 0 {
			t.Fatalf("site health consecutive failures should be non-negative: %#v", row)
		}
	}
}

func assertSiteGroupsSorted(t *testing.T, rows []SiteGroup) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SortOrder < rows[i-1].SortOrder {
			t.Fatalf("site groups are not sorted by sort order at index %d", i)
		}
		if rows[i].SortOrder == rows[i-1].SortOrder && rows[i].Name < rows[i-1].Name {
			t.Fatalf("site groups are not sorted by name at index %d", i)
		}
	}
	for _, row := range rows {
		if row.ID == uuid.Nil {
			t.Fatalf("site group id is nil: %#v", row)
		}
	}
}

func assertSiteGroupSitesStable(t *testing.T, rows []SiteGroupSite) {
	t.Helper()
	for _, row := range rows {
		if row.ID == uuid.Nil || row.GroupID == uuid.Nil || row.SiteID == uuid.Nil {
			t.Fatalf("site group site has nil id: %#v", row)
		}
	}
}

func siteGroupIDsForSmoke(rows []SiteGroup) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		if len(ids) == 3 {
			return ids
		}
	}
	return ids
}

func assertUUIDsNonNil(t *testing.T, ids []uuid.UUID) {
	t.Helper()
	for _, id := range ids {
		if id == uuid.Nil {
			t.Fatalf("uuid list contains nil id: %#v", ids)
		}
	}
}

func assertEmptyAPIKeyAccessReads(t *testing.T, ctx context.Context, repo APIKeyAccessRepository, missingID uuid.UUID) {
	t.Helper()

	groupPermissions, err := repo.ListSiteGroupPermissions(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing api key site group permissions: %v", err)
	}
	if len(groupPermissions) != 0 {
		t.Fatalf("missing api key site group permissions length = %d, want 0", len(groupPermissions))
	}
	modelPermissions, err := repo.ListSiteModelPermissions(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing api key site model permissions: %v", err)
	}
	if len(modelPermissions) != 0 {
		t.Fatalf("missing api key site model permissions length = %d, want 0", len(modelPermissions))
	}
	modelPermissionDetails, err := repo.ListSiteModelPermissionDetails(ctx, missingID)
	if err != nil {
		t.Fatalf("list missing api key site model permission details: %v", err)
	}
	if len(modelPermissionDetails) != 0 {
		t.Fatalf("missing api key site model permission details length = %d, want 0", len(modelPermissionDetails))
	}
	enabledModelIDs, err := repo.EnabledSiteModelIDs(ctx, missingID)
	if err != nil {
		t.Fatalf("enabled site model ids for missing api key: %v", err)
	}
	if len(enabledModelIDs) != 0 {
		t.Fatalf("missing api key enabled site model ids length = %d, want 0", len(enabledModelIDs))
	}
	canonicalModelIDs, err := repo.EnabledSiteModelIDsForCanonical(ctx, missingID, uuid.New())
	if err != nil {
		t.Fatalf("enabled site model ids for missing api key and canonical: %v", err)
	}
	if len(canonicalModelIDs) != 0 {
		t.Fatalf("missing api key canonical site model ids length = %d, want 0", len(canonicalModelIDs))
	}
}

func assertSiteCredentialsSorted(t *testing.T, rows []SiteCredential) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SiteID.String() < rows[i-1].SiteID.String() {
			t.Fatalf("site credentials are not sorted by site id at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && rows[i].CredentialType < rows[i-1].CredentialType {
			t.Fatalf("site credentials are not sorted by type at index %d", i)
		}
	}
	for _, row := range rows {
		if row.ID == uuid.Nil || row.SiteID == uuid.Nil {
			t.Fatalf("site credential has nil id: %#v", row)
		}
	}
}

func assertSiteCredentialsBySiteSorted(t *testing.T, rows []SiteCredential) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		leftAPIKey := isAPIKeyCredentialType(rows[i-1].CredentialType)
		rightAPIKey := isAPIKeyCredentialType(rows[i].CredentialType)
		if leftAPIKey && rightAPIKey {
			if rows[i].CreatedAt.Before(rows[i-1].CreatedAt) {
				t.Fatalf("site api key credentials are not oldest first at index %d", i)
			}
			if rows[i].CreatedAt.Equal(rows[i-1].CreatedAt) && rows[i].ID.String() < rows[i-1].ID.String() {
				t.Fatalf("site api key credentials are not sorted by id at index %d", i)
			}
			continue
		}
		if rows[i].CredentialType < rows[i-1].CredentialType {
			t.Fatalf("site credentials are not sorted by type at index %d", i)
		}
	}
}

func assertSitePricingGroupsSorted(t *testing.T, rows []SitePricingGroup) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].GroupName < rows[i-1].GroupName {
			t.Fatalf("site pricing groups are not sorted by name at index %d", i)
		}
	}
}

func assertSiteModelPricingsSorted(t *testing.T, rows []SiteModelPricing) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SiteID.String() < rows[i-1].SiteID.String() {
			t.Fatalf("site model pricings are not sorted by site id at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && rows[i].ModelName < rows[i-1].ModelName {
			t.Fatalf("site model pricings are not sorted by model name at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && rows[i].ModelName == rows[i-1].ModelName && rows[i].GroupName < rows[i-1].GroupName {
			t.Fatalf("site model pricings are not sorted by group name at index %d", i)
		}
	}
}

func assertSiteAPIKeyModelsSorted(t *testing.T, rows []SiteAPIKeyModel) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SiteID.String() < rows[i-1].SiteID.String() {
			t.Fatalf("site api key models are not sorted by site id at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && rows[i].SiteCredentialID.String() < rows[i-1].SiteCredentialID.String() {
			t.Fatalf("site api key models are not sorted by credential id at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && rows[i].SiteCredentialID == rows[i-1].SiteCredentialID && rows[i].UpstreamModelName < rows[i-1].UpstreamModelName {
			t.Fatalf("site api key models are not sorted by upstream name at index %d", i)
		}
	}
}

func assertSiteAPIKeyStatesSorted(t *testing.T, rows []SiteAPIKeyState) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SiteID.String() < rows[i-1].SiteID.String() {
			t.Fatalf("site api key states are not sorted by site id at index %d", i)
		}
		left := rows[i-1].UpstreamID
		right := rows[i].UpstreamID
		if rows[i].SiteID == rows[i-1].SiteID && !left.Valid && right.Valid {
			t.Fatalf("site api key states with upstream ids should sort first at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && left.Valid && right.Valid && right.Int64 < left.Int64 {
			t.Fatalf("site api key states are not sorted by upstream id at index %d", i)
		}
		if rows[i].SiteID == rows[i-1].SiteID && left.Valid == right.Valid && (!left.Valid || left.Int64 == right.Int64) && rows[i].Name < rows[i-1].Name {
			t.Fatalf("site api key states are not sorted by name at index %d", i)
		}
	}
}

func assertCanonicalModelAliasesSorted(t *testing.T, rows []CanonicalModelAlias) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].Alias < rows[i-1].Alias {
			t.Fatalf("canonical model aliases are not sorted by alias at index %d", i)
		}
	}
	for _, row := range rows {
		if row.ID == uuid.Nil || row.CanonicalModelID == uuid.Nil {
			t.Fatalf("canonical model alias has nil id: %#v", row)
		}
	}
}

func assertCanonicalModelMatrixStable(t *testing.T, rows []CanonicalModelMatrixRow) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].SiteName < rows[i-1].SiteName {
			t.Fatalf("canonical model matrix is not sorted by site name at index %d", i)
		}
		if rows[i].SiteName == rows[i-1].SiteName && rows[i].UpstreamModelName < rows[i-1].UpstreamModelName {
			t.Fatalf("canonical model matrix is not sorted by upstream model at index %d", i)
		}
		if rows[i].SiteName == rows[i-1].SiteName && rows[i].UpstreamModelName == rows[i-1].UpstreamModelName && rows[i].GroupName.String < rows[i-1].GroupName.String {
			t.Fatalf("canonical model matrix is not sorted by group name at index %d", i)
		}
	}
	for _, row := range rows {
		if row.SiteID == uuid.Nil || row.SiteModelID == uuid.Nil {
			t.Fatalf("canonical model matrix row has nil id: %#v", row)
		}
		if row.APIKeyCount < 0 || row.AvailableAPIKeyCount < 0 || row.AvailableAPIKeyCount > row.APIKeyCount {
			t.Fatalf("canonical model matrix api key counts are invalid: %#v", row)
		}
	}
}

func assertSitesMatchRequestedIDs(t *testing.T, rows []Site, requested map[uuid.UUID]bool) {
	t.Helper()
	for _, row := range rows {
		if row.ID == uuid.Nil {
			t.Fatalf("listed site has nil id: %#v", row)
		}
		if !requested[row.ID] {
			t.Fatalf("listed unexpected site id %s", row.ID)
		}
	}
}

func siteIDsForReadSmoke(sites []Site) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(sites))
	for _, site := range sites {
		ids = append(ids, site.ID)
		if len(ids) == 3 {
			return ids
		}
	}
	return ids
}

func assertSiteIDsWithRequestsStable(t *testing.T, rows map[uuid.UUID]bool, requested []uuid.UUID) {
	t.Helper()
	requestedSet := map[uuid.UUID]bool{}
	for _, id := range requested {
		if id != uuid.Nil {
			requestedSet[id] = true
		}
	}
	for id, hasRequests := range rows {
		if id == uuid.Nil {
			t.Fatalf("site ids with requests contains nil id: %#v", rows)
		}
		if !hasRequests {
			t.Fatalf("site ids with requests contains false value for %s", id)
		}
		if !requestedSet[id] {
			t.Fatalf("site ids with requests contains unexpected id %s", id)
		}
	}
}

func assertRouteOverviewRowsStable(t *testing.T, rows []RouteOverviewRow) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].ModelKey < rows[i-1].ModelKey {
			t.Fatalf("route overview rows are not sorted by model key at index %d", i)
		}
	}
	for _, row := range rows {
		if row.CanonicalModelID == uuid.Nil {
			t.Fatalf("route overview canonical id is nil: %#v", row)
		}
		if row.SiteModelCount < 0 || row.SiteCount < 0 || row.EligibleCount < 0 || row.CooldownCount < 0 || row.RequestCount24h < 0 || row.SuccessCount24h < 0 {
			t.Fatalf("route overview counts should be non-negative: %#v", row)
		}
	}
}

func assertRouteCooldownsSorted(t *testing.T, rows []RouteCooldown) {
	t.Helper()
	for i := 1; i < len(rows); i++ {
		if rows[i].ActiveUntil.After(rows[i-1].ActiveUntil) {
			t.Fatalf("route cooldowns are not sorted by active_until desc at index %d", i)
		}
		if rows[i].ActiveUntil.Equal(rows[i-1].ActiveUntil) && rows[i].CreatedAt.After(rows[i-1].CreatedAt) {
			t.Fatalf("route cooldowns are not sorted by created_at desc at index %d", i)
		}
	}
	for _, row := range rows {
		if row.ID == uuid.Nil || row.SiteID == uuid.Nil {
			t.Fatalf("route cooldown has nil id: %#v", row)
		}
		if row.ClearedAt.Valid || !row.ActiveUntil.After(time.Now()) {
			t.Fatalf("route cooldown should be active: %#v", row)
		}
	}
}
