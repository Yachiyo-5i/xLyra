package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestAdminSessionRepositoryDeleteByTokenHashDeletesMatchingSessionOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	if err := NewAdminSessionRepository(db).DeleteByTokenHash(context.Background(), "hash"); err != nil {
		t.Fatalf("DeleteByTokenHash returned error: %v", err)
	}
	if deleteCalls != 1 {
		t.Fatalf("deleteCalls=%d, want 1", deleteCalls)
	}
}

func TestAPIKeyRepositoryGetActiveByHashRejectsInactiveExpiredAndTouchesGeneratedOffline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	saveCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		item, ok := tx.Statement.Dest.(*APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected api key query destination"))
			return
		}
		switch queryCalls {
		case 1:
			*item = APIKey{ID: uuid.New(), KeyHash: "inactive", Status: "disabled", QuotaUnlimited: true}
		case 2:
			*item = APIKey{ID: uuid.New(), KeyHash: "expired", Status: "active", QuotaUnlimited: true, ExpiresAt: storeTimePtr(now)}
		default:
			*item = APIKey{
				ID:             uuid.New(),
				KeyHash:        "active",
				KeyKind:        "generated",
				Status:         "active",
				QuotaLimit:     sql.NullFloat64{Float64: 10, Valid: true},
				QuotaUsed:      2,
				QuotaUnlimited: false,
			}
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		saveCalls++
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			tx.AddError(errors.New("unexpected api key update destination"))
			return
		}
		ts, ok := updates["last_used_at"].(time.Time)
		if !ok || !ts.Equal(now) {
			tx.AddError(errors.New("last used timestamp was not updated"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	repo := NewAPIKeyRepository(db)
	if _, err := repo.GetActiveByHash(context.Background(), "inactive", now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("inactive error=%v, want record not found", err)
	}
	if _, err := repo.GetActiveByHash(context.Background(), "expired", now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expired error=%v, want record not found", err)
	}
	item, err := repo.GetActiveGeneratedByHash(context.Background(), "active", now)
	if err != nil {
		t.Fatalf("GetActiveGeneratedByHash returned error: %v", err)
	}
	if item.KeyKind != "generated" || saveCalls != 1 || queryCalls != 3 {
		t.Fatalf("item=%#v saveCalls=%d queryCalls=%d", item, saveCalls, queryCalls)
	}
}

func TestAPIKeyAccessRepositoryListSiteModelPermissionDetailsHydratesSortedMetadataOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteA := uuid.New()
	siteB := uuid.New()
	modelA := uuid.New()
	modelB := uuid.New()
	canonicalID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]APIKeySiteModelPermission:
			*dest = []APIKeySiteModelPermission{
				{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: modelB, Enabled: true},
				{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: modelA, Enabled: true},
			}
		case *[]SiteModel:
			*dest = []SiteModel{
				{ID: modelA, SiteID: siteA, UpstreamName: "z-model", DisplayName: "Z", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}},
				{ID: modelB, SiteID: siteB, UpstreamName: "a-model", DisplayName: "A"},
			}
		case *[]Site:
			*dest = []Site{
				{ID: siteB, Name: "Beta", Slug: "beta", SiteType: "newapi"},
				{ID: siteA, Name: "Alpha", Slug: "alpha", SiteType: "openai"},
			}
		case *[]CanonicalModel:
			*dest = []CanonicalModel{{ID: canonicalID, ModelKey: "gpt-permission"}}
		default:
			tx.AddError(errors.New("unexpected permission detail query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	rows, err := NewAPIKeyAccessRepository(db).ListSiteModelPermissionDetails(context.Background(), apiKeyID)
	if err != nil {
		t.Fatalf("ListSiteModelPermissionDetails returned error: %v", err)
	}
	if len(rows) != 2 || rows[0].SiteName != "Alpha" || rows[1].SiteName != "Beta" {
		t.Fatalf("rows=%#v, want sorted by site name", rows)
	}
	if !rows[0].CanonicalModelKey.Valid || rows[0].CanonicalModelKey.String != "gpt-permission" {
		t.Fatalf("first canonical key=%#v, want populated canonical key", rows[0].CanonicalModelKey)
	}
}

func TestAPIKeyAccessRepositoryListSiteModelPermissionDetailsEmptySkipsLookupsOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	apiKeyID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		permissions, ok := tx.Statement.Dest.(*[]APIKeySiteModelPermission)
		if !ok {
			tx.AddError(errors.New("unexpected empty permission detail query destination"))
			return
		}
		queryCalls++
		*permissions = nil
		tx.Statement.RowsAffected = 0
	})

	details, err := NewAPIKeyAccessRepository(db).ListSiteModelPermissionDetails(ctx, apiKeyID)
	if err != nil {
		t.Fatalf("ListSiteModelPermissionDetails returned error: %v", err)
	}
	if len(details) != 0 || queryCalls != 1 {
		t.Fatalf("details=%#v queryCalls=%d, want one permission query and no detail lookups", details, queryCalls)
	}
}

func TestCanonicalModelRepositoryLifecycleReadsUpdatesArchivesAndDeletesAliasesOffline(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	aliasID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	savedStatuses := []string{}
	createdAliases := 0
	deletedAliases := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch dest := tx.Statement.Dest.(type) {
		case *CanonicalModelAlias:
			*dest = CanonicalModelAlias{ID: aliasID, CanonicalModelID: canonicalID, NormalizedAlias: "lifecycle-alias"}
		case *CanonicalModel:
			*dest = CanonicalModel{ID: canonicalID, ModelKey: "lifecycle-old", Status: "active", Capabilities: JSON(`{"auto_created":true,"source":"catalog_match"}`)}
		default:
			tx.AddError(errors.New("unexpected canonical lifecycle query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*CanonicalModelAlias); ok {
			createdAliases++
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*CanonicalModel); ok {
			savedStatuses = append(savedStatuses, item.Status)
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deletedAliases++
		tx.Statement.RowsAffected = 1
	})

	repo := NewCanonicalModelRepository(db)
	byAlias, err := repo.GetByNormalizedAlias(context.Background(), "lifecycle-alias")
	if err != nil {
		t.Fatalf("GetByNormalizedAlias returned error: %v", err)
	}
	updated, err := repo.Update(context.Background(), UpdateCanonicalModelParams{
		ID: canonicalID, ModelKey: "lifecycle-new", Status: "active", Capabilities: JSON(`{"manual_created":true}`),
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	archived, err := repo.Archive(context.Background(), canonicalID)
	if err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	_, err = repo.CreateAlias(context.Background(), CreateCanonicalModelAliasParams{
		CanonicalModelID: canonicalID,
		Alias:            "Lifecycle Alias",
		NormalizedAlias:  "lifecycle-new",
		Source:           "test",
	})
	if err != nil {
		t.Fatalf("CreateAlias returned error: %v", err)
	}
	if err := repo.DeleteAlias(context.Background(), canonicalID, aliasID); err != nil {
		t.Fatalf("DeleteAlias returned error: %v", err)
	}

	if byAlias.ID != canonicalID || updated.ModelKey != "lifecycle-new" || archived.Status != "archived" {
		t.Fatalf("byAlias=%#v updated=%#v archived=%#v", byAlias, updated, archived)
	}
	if createdAliases != 0 || deletedAliases != 1 || queryCalls < 5 || len(savedStatuses) != 2 {
		t.Fatalf("createdAliases=%d deletedAliases=%d queryCalls=%d savedStatuses=%#v", createdAliases, deletedAliases, queryCalls, savedStatuses)
	}
}

func TestCanonicalModelRepositoryMatrixCountsAvailabilityAndArchivesUnusedAutoCreatedOffline(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	usedCanonicalID := uuid.New()
	siteID := uuid.New()
	activeModelID := uuid.New()
	credentialID := uuid.New()
	unusedModelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	archived := []uuid.UUID{}
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]SiteModel:
			*dest = []SiteModel{
				{ID: activeModelID, SiteID: siteID, UpstreamName: "matrix-upstream", Status: "active", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}},
				{ID: uuid.New(), SiteID: siteID, UpstreamName: "disabled", Status: "unavailable", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}},
			}
		case *[]Site:
			*dest = []Site{{ID: siteID, Name: "Matrix Site", Slug: "matrix-site", Status: "active", Enabled: true}}
		case *[]SiteAPIKeyModel:
			*dest = []SiteAPIKeyModel{
				{SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: activeModelID, Valid: true}, Available: true, Enabled: true},
				{SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: activeModelID, Valid: true}, Available: false, Enabled: true},
			}
		case *[]SiteCredential:
			*dest = []SiteCredential{{ID: credentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)}}
		case *[]SiteAPIKeyState:
			*dest = nil
		case *[]RouteCooldown:
			*dest = nil
		case *[]SiteModelPricing:
			*dest = []SiteModelPricing{{SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: activeModelID, Valid: true}, Available: true, GroupName: "default", Currency: "USD"}}
		case *[]CanonicalModel:
			*dest = []CanonicalModel{
				{ID: usedCanonicalID, ModelKey: "used", Status: "active", Capabilities: JSON(`{"auto_created":true,"source":"catalog_match"}`)},
				{ID: unusedModelID, ModelKey: "unused", Status: "active", Capabilities: JSON(`{"auto_created":true,"source":"catalog_match"}`)},
				{ID: uuid.New(), ModelKey: "manual", Status: "active", Capabilities: JSON(`{"manual_created":true}`)},
			}
		default:
			tx.AddError(errors.New("unexpected canonical matrix query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected canonical archive destination"))
			return
		}
		archived = append(archived, item.ID)
		tx.Statement.RowsAffected = 1
	})

	repo := NewCanonicalModelRepository(db)
	count, err := repo.ActiveSiteModelCount(context.Background(), canonicalID)
	if err != nil {
		t.Fatalf("ActiveSiteModelCount returned error: %v", err)
	}
	matrix, err := repo.Matrix(context.Background(), canonicalID)
	if err != nil {
		t.Fatalf("Matrix returned error: %v", err)
	}
	n, err := repo.ArchiveUnusedAutoCreated(context.Background())
	if err != nil {
		t.Fatalf("ArchiveUnusedAutoCreated returned error: %v", err)
	}
	if count != 1 || len(matrix) != 1 || matrix[0].AvailableAPIKeyCount != 1 || n != 2 || len(archived) != 2 {
		t.Fatalf("count=%d matrix=%#v n=%d archived=%#v", count, matrix, n, archived)
	}
}

func TestCanonicalModelRepositoryMatrixOnlyFallsBackWithoutCredentialBindingsOffline(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	hasBinding := false
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]SiteModel:
			*dest = []SiteModel{{ID: siteModelID, SiteID: siteID, UpstreamName: "fallback-model", Status: "active", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}}}
		case *[]Site:
			*dest = []Site{{ID: siteID, Name: "Generic Site", Slug: "generic-site", SiteType: "openai", Status: "active", Enabled: true}}
		case *[]SiteAPIKeyModel:
			if hasBinding {
				*dest = []SiteAPIKeyModel{{SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: false}}
			}
		case *[]SiteCredential:
			*dest = []SiteCredential{{ID: credentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)}}
		case *[]SiteAPIKeyState, *[]RouteCooldown, *[]SiteModelPricing:
		case *SiteModelPricing:
		default:
			tx.AddError(errors.New("unexpected canonical fallback matrix query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	repo := NewCanonicalModelRepository(db)
	matrix, err := repo.Matrix(context.Background(), canonicalID)
	if err != nil {
		t.Fatalf("Matrix without bindings returned error: %v", err)
	}
	if len(matrix) != 1 || matrix[0].APIKeyCount != 1 || matrix[0].AvailableAPIKeyCount != 1 {
		t.Fatalf("matrix without bindings = %#v, want one fallback credential", matrix)
	}

	hasBinding = true
	matrix, err = repo.Matrix(context.Background(), canonicalID)
	if err != nil {
		t.Fatalf("Matrix with disabled binding returned error: %v", err)
	}
	if len(matrix) != 1 || matrix[0].APIKeyCount != 1 || matrix[0].AvailableAPIKeyCount != 0 {
		t.Fatalf("matrix with disabled binding = %#v, want binding count without fallback availability", matrix)
	}
}

func TestGatewayRepositoryListsCredentialsByQuotaAndSelectsRequestedPricingOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	credentialFast := uuid.New()
	credentialSlow := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]SiteAPIKeyModel:
			*dest = []SiteAPIKeyModel{
				{SiteID: siteID, SiteCredentialID: credentialSlow, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true, UpdatedAt: time.Date(2026, 6, 23, 8, 0, 0, 0, time.UTC)},
				{SiteID: siteID, SiteCredentialID: credentialFast, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true, UpdatedAt: time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)},
			}
		case *[]SiteCredential:
			*dest = []SiteCredential{
				{ID: credentialSlow, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`), CreatedAt: time.Date(2026, 6, 23, 7, 0, 0, 0, time.UTC)},
				{ID: credentialFast, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`), CreatedAt: time.Date(2026, 6, 23, 7, 30, 0, 0, time.UTC)},
			}
		case *[]SiteAPIKeyState:
			*dest = []SiteAPIKeyState{
				{SiteID: siteID, SiteCredentialID: credentialSlow, Enabled: true, RemainQuota: sql.NullInt64{Int64: 5, Valid: true}, GroupName: sql.NullString{String: "slow", Valid: true}},
				{SiteID: siteID, SiteCredentialID: credentialFast, Enabled: true, RemainQuota: sql.NullInt64{Int64: 10, Valid: true}, GroupName: sql.NullString{String: "fast", Valid: true}},
			}
		case *[]RouteCooldown:
			*dest = nil
		case *[]SiteModelPricing:
			*dest = []SiteModelPricing{
				{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, GroupName: "other", InputValue: sql.NullFloat64{Float64: 0.2, Valid: true}},
				{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, GroupName: "fast", Currency: "USD", InputValue: sql.NullFloat64{Float64: 1, Valid: true}},
				{SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, GroupName: "default", InputValue: sql.NullFloat64{Float64: 0.01, Valid: true}},
			}
		default:
			tx.AddError(errors.New("unexpected gateway query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	repo := NewGatewayRepository(db)
	credentials, err := repo.ListCredentialsForSiteModel(context.Background(), siteID, modelID)
	if err != nil {
		t.Fatalf("ListCredentialsForSiteModel returned error: %v", err)
	}
	pricing, err := repo.GetPricingForSiteModel(context.Background(), modelID, "fast")
	if err != nil {
		t.Fatalf("GetPricingForSiteModel returned error: %v", err)
	}
	if len(credentials) != 2 || credentials[0].Credential.ID != credentialFast || credentials[0].GroupName.String != "fast" {
		t.Fatalf("credentials=%#v, want highest quota first", credentials)
	}
	hasBindings, err := repo.HasCredentialBindingsForSiteModel(context.Background(), siteID, modelID)
	if err != nil {
		t.Fatalf("HasCredentialBindingsForSiteModel returned error: %v", err)
	}
	if !hasBindings {
		t.Fatal("HasCredentialBindingsForSiteModel = false, want true")
	}
	if !pricing.GroupName.Valid || pricing.GroupName.String != "fast" || !pricing.Currency.Valid {
		t.Fatalf("pricing=%#v, want requested group pricing", pricing)
	}
}

func TestOAuthConnectionAndGatewayRateLimitRepositoriesCreateAndUpdateUpsertsOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	createCalls := 0
	updateCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch dest := tx.Statement.Dest.(type) {
		case *OAuthConnection:
			if queryCalls == 1 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*dest = OAuthConnection{ID: uuid.New(), Provider: "codex", Email: "old@example.com", SiteID: &siteID}
		case *GatewayRateLimit:
			if queryCalls == 3 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*dest = GatewayRateLimit{ID: uuid.New(), Scope: RateLimitScopeAPIKey, APIKeyID: uuid.NullUUID{UUID: apiKeyID, Valid: true}, Status: RateLimitStatusDisabled}
		default:
			tx.AddError(errors.New("unexpected oauth/rate query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		createCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateCalls++
		tx.Statement.RowsAffected = 1
	})

	oauthRepo := NewOAuthConnectionRepository(db)
	created, err := oauthRepo.UpsertByProviderEmail(context.Background(), UpsertOAuthConnectionParams{Provider: " codex ", Email: " new@example.com "})
	if err != nil {
		t.Fatalf("oauth create upsert returned error: %v", err)
	}
	updated, err := oauthRepo.UpsertByProviderEmail(context.Background(), UpsertOAuthConnectionParams{Provider: "codex", Email: "old@example.com", Status: "refreshing", SiteID: siteID})
	if err != nil {
		t.Fatalf("oauth update upsert returned error: %v", err)
	}
	rateRepo := NewGatewayRateLimitRepository(db)
	createdLimit, err := rateRepo.Upsert(context.Background(), UpsertGatewayRateLimitParams{Scope: RateLimitScopeAPIKey, APIKeyID: apiKeyID, Status: RateLimitStatusEnabled, RPMLimit: int64(60)})
	if err != nil {
		t.Fatalf("rate limit create upsert returned error: %v", err)
	}
	updatedLimit, err := rateRepo.Upsert(context.Background(), UpsertGatewayRateLimitParams{Scope: RateLimitScopeAPIKey, APIKeyID: apiKeyID, Status: RateLimitStatusDisabled})
	if err != nil {
		t.Fatalf("rate limit update upsert returned error: %v", err)
	}
	if _, err := rateRepo.Upsert(context.Background(), UpsertGatewayRateLimitParams{Scope: "bad", Status: RateLimitStatusEnabled}); err == nil {
		t.Fatal("invalid rate limit scope error=nil, want error")
	}
	for _, tc := range []struct {
		name string
		in   UpsertGatewayRateLimitParams
		want string
	}{
		{
			name: "missing scope",
			in:   UpsertGatewayRateLimitParams{Status: RateLimitStatusEnabled},
			want: "rate limit scope is required",
		},
		{
			name: "invalid status",
			in:   UpsertGatewayRateLimitParams{Scope: RateLimitScopeGlobal, Status: "paused"},
			want: "rate limit status must be enabled or disabled",
		},
		{
			name: "api key scope missing api key id",
			in:   UpsertGatewayRateLimitParams{Scope: RateLimitScopeAPIKey, Status: RateLimitStatusEnabled},
			want: "api_key_id is required for api_key rate limit",
		},
	} {
		if _, err := NewGatewayRateLimitRepository(nil).Upsert(context.Background(), tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s Upsert error = %v, want %q", tc.name, err, tc.want)
		}
	}

	if created.Email != "new@example.com" || updated.Status != "refreshing" || createdLimit.Status != RateLimitStatusEnabled || updatedLimit.Status != RateLimitStatusDisabled {
		t.Fatalf("created=%#v updated=%#v createdLimit=%#v updatedLimit=%#v", created, updated, createdLimit, updatedLimit)
	}
	if createCalls != 2 || updateCalls != 2 {
		t.Fatalf("createCalls=%d updateCalls=%d, want 2 each", createCalls, updateCalls)
	}
}

func TestRequestLogRepositoryCreatesDeletesRecentAttemptsAndSearchExpressionsOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	canonicalID := uuid.New()
	apiKeyID := uuid.New()
	logID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	createCalls := 0
	deleteCalls := 0
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		createCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 2
	})
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]RequestLog:
			*dest = []RequestLog{{ID: logID, SiteID: uuid.NullUUID{UUID: siteID, Valid: true}, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, CreatedAt: time.Now()}}
		case *[]Site:
			*dest = []Site{{ID: siteID, Name: "Request Search", Slug: "request-search", SiteType: "openai"}}
		case *[]CanonicalModel:
			*dest = []CanonicalModel{{ID: canonicalID, ModelKey: "request-search-canonical"}}
		case *[]SiteModel:
			*dest = []SiteModel{{ID: siteModelID, UpstreamName: "request-search-upstream", DisplayName: "Request Search Display"}}
		default:
			tx.AddError(errors.New("unexpected request helper query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	repo := NewRequestLogRepository(db)
	created, err := repo.Create(context.Background(), CreateRequestLogParams{
		RequestID: "req-search", SiteID: siteID, SiteModelID: siteModelID, Endpoint: "/v1/chat", Success: true, Metadata: JSON(`{}`),
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	attempts, err := repo.ListRecentSiteModelAttempts(context.Background(), siteID, siteModelID, time.Now().Add(-time.Hour), 0)
	if err != nil {
		t.Fatalf("ListRecentSiteModelAttempts returned error: %v", err)
	}
	recent, err := repo.ListRecentByAPIKeyAndCanonicalModel(context.Background(), apiKeyID, canonicalID, 999)
	if err != nil {
		t.Fatalf("ListRecentByAPIKeyAndCanonicalModel returned error: %v", err)
	}
	deleted, err := repo.DeleteBefore(context.Background(), time.Now(), 1)
	if err != nil {
		t.Fatalf("DeleteBefore returned error: %v", err)
	}
	ids, err := repo.requestLogSearchIDs(context.Background(), "request-search")
	if err != nil {
		t.Fatalf("requestLogSearchIDs returned error: %v", err)
	}
	expr, err := repo.requestLogSearchExpression(context.Background(), "request-search")
	if err != nil {
		t.Fatalf("requestLogSearchExpression returned error: %v", err)
	}
	if created.RequestID != "req-search" || len(attempts) != 1 || len(recent) != 1 || deleted != 2 || createCalls != 1 || deleteCalls != 1 {
		t.Fatalf("created=%#v attempts=%#v recent=%#v deleted=%d createCalls=%d deleteCalls=%d", created, attempts, recent, deleted, createCalls, deleteCalls)
	}
	if len(ids.SiteIDs) != 1 || len(ids.CanonicalModelIDs) != 1 || len(ids.SiteModelIDs) != 1 {
		t.Fatalf("ids=%#v, want matches in all dimensions", ids)
	}
	if _, ok := expr.(clause.OrConditions); !ok {
		t.Fatalf("expr type=%T, want clause.OrConditions", expr)
	}
}

func TestHealthAndRouteCooldownRepositoriesBucketSnapshotsAndManageCooldownsOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	now := time.Date(2026, 6, 23, 10, 30, 0, 0, time.UTC)
	db := storeTransactionGorm(t, "health cooldown")
	saveCalls := 0
	createCalls := 0
	deleteCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]HealthSnapshot:
			*dest = []HealthSnapshot{
				{SiteID: siteID, Scope: "site", Source: "scheduler", Success: true, CheckedAt: now.Add(-20 * time.Minute)},
				{SiteID: siteID, Scope: "site", Source: "scheduler", Success: false, CheckedAt: now.Add(-10 * time.Minute)},
				{SiteID: siteID, Scope: "site", Source: "scheduler", Success: true, CheckedAt: now.Add(-2 * time.Hour)},
			}
		case *[]RouteCooldown:
			*dest = []RouteCooldown{
				{ID: uuid.New(), SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true}, Source: "gateway", ActiveUntil: now.Add(time.Hour), CreatedAt: now},
				{ID: uuid.New(), SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Source: "manual", ActiveUntil: now.Add(time.Hour), CreatedAt: now},
				{ID: uuid.New(), SiteID: siteID, Source: "gateway", ActiveUntil: now.Add(-time.Hour), CreatedAt: now},
			}
		default:
			tx.AddError(errors.New("unexpected health/cooldown query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		saveCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		createCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	buckets, err := NewHealthRepository(db).ListSiteHourlyBuckets(context.Background(), siteID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListSiteHourlyBuckets returned error: %v", err)
	}
	cooldownRepo := NewRouteCooldownRepository(db)
	active, err := cooldownRepo.ListActive(context.Background(), now)
	if err != nil {
		t.Fatalf("ListActive returned error: %v", err)
	}
	activated, err := cooldownRepo.Activate(context.Background(), ActivateRouteCooldownParams{SiteID: siteID, SiteModelID: modelID, SiteCredentialID: credentialID, Source: "gateway", ActiveUntil: now.Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("Activate returned error: %v", err)
	}
	if err := cooldownRepo.DeleteBySiteModel(context.Background(), siteID, modelID); err != nil {
		t.Fatalf("DeleteBySiteModel returned error: %v", err)
	}
	if err := cooldownRepo.DeleteBySiteCredential(context.Background(), siteID, credentialID); err != nil {
		t.Fatalf("DeleteBySiteCredential returned error: %v", err)
	}
	if len(buckets) != 1 || buckets[0].TotalCount != 2 || buckets[0].SuccessCount != 1 || len(active) != 2 || activated.Scope != "credential" {
		t.Fatalf("buckets=%#v active=%#v activated=%#v", buckets, active, activated)
	}
	if saveCalls != 1 || createCalls != 1 || deleteCalls != 2 {
		t.Fatalf("saveCalls=%d createCalls=%d deleteCalls=%d", saveCalls, createCalls, deleteCalls)
	}
}

func TestSiteStateAPIKeyModelAndAPIKeyStateRepositoriesCreateAndUpdateUpsertsOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	modelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	createCalls := 0
	updateCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch dest := tx.Statement.Dest.(type) {
		case *SiteState:
			if queryCalls == 1 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*dest = SiteState{SiteID: siteID, SyncStatus: "old"}
		case *SiteAPIKeyModel:
			if queryCalls == 3 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*dest = SiteAPIKeyModel{ID: uuid.New(), SiteID: siteID, SiteCredentialID: credentialID, UpstreamModelName: "old", Enabled: true}
		case *SiteAPIKeyState:
			if queryCalls == 5 {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*dest = SiteAPIKeyState{SiteCredentialID: credentialID, SiteID: siteID, Name: "old", Enabled: true}
		default:
			tx.AddError(errors.New("unexpected site state/model query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		createCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateCalls++
		tx.Statement.RowsAffected = 1
	})

	siteRepo := NewSiteStateRepository(db)
	createdState, err := siteRepo.Upsert(context.Background(), UpsertSiteStateParams{SiteID: siteID})
	if err != nil {
		t.Fatalf("site state create upsert returned error: %v", err)
	}
	updatedState, err := siteRepo.Upsert(context.Background(), UpsertSiteStateParams{SiteID: siteID, SyncStatus: "synced", RawStatus: JSON(`{}`)})
	if err != nil {
		t.Fatalf("site state update upsert returned error: %v", err)
	}
	modelRepo := NewSiteAPIKeyModelRepository(db)
	createdModel, err := modelRepo.Upsert(context.Background(), UpsertSiteAPIKeyModelParams{SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: modelID, UpstreamModelName: "new", Enabled: true})
	if err != nil {
		t.Fatalf("api key model create upsert returned error: %v", err)
	}
	updatedModel, err := modelRepo.Upsert(context.Background(), UpsertSiteAPIKeyModelParams{SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: modelID, UpstreamModelName: "old", Enabled: false})
	if err != nil {
		t.Fatalf("api key model update upsert returned error: %v", err)
	}
	stateRepo := NewSiteAPIKeyStateRepository(db)
	createdKeyState, err := stateRepo.Upsert(context.Background(), UpsertSiteAPIKeyStateParams{SiteID: siteID, SiteCredentialID: credentialID, Name: "new", Enabled: true})
	if err != nil {
		t.Fatalf("api key state create upsert returned error: %v", err)
	}
	updatedKeyState, err := stateRepo.Upsert(context.Background(), UpsertSiteAPIKeyStateParams{SiteID: siteID, SiteCredentialID: credentialID, Name: "old", Enabled: false, SyncStatus: "failed"})
	if err != nil {
		t.Fatalf("api key state update upsert returned error: %v", err)
	}

	if createdState.SyncStatus != "pending" || updatedState.SyncStatus != "synced" || !createdModel.Enabled || !updatedModel.Enabled || createdKeyState.SyncStatus != "synced" || updatedKeyState.SyncStatus != "failed" {
		t.Fatalf("states/models=%#v %#v %#v %#v %#v %#v", createdState, updatedState, createdModel, updatedModel, createdKeyState, updatedKeyState)
	}
	if createCalls != 3 || updateCalls != 3 {
		t.Fatalf("createCalls=%d updateCalls=%d, want 3 each", createCalls, updateCalls)
	}
}
