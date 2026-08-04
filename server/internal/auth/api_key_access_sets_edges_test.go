package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	authcrypto "xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

func authServiceWithGormCallbacks(t *testing.T, query func(*gorm.DB), create func(*gorm.DB), update func(*gorm.DB)) *Service {
	t.Helper()

	service := authServiceWithOptionalQueryCallback(t, "callback-test-master-key", query)
	if create != nil {
		authReplaceCreateCallback(t, service.db, create)
	}
	if update != nil {
		authReplaceUpdateCallback(t, service.db, update)
	}
	return service
}

func TestLoginSuccessAndTOTPGuardBranchesOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	passwordHash, err := authcrypto.HashPassword("login-callback-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	sessionID := uuid.New()
	updateCount := 0
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Admin:
			*dest = []store.Admin{{ID: adminID, Username: "Root", PasswordHash: passwordHash, Status: "active"}}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected login query destination"))
		}
	}, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.AdminSession)
		if !ok {
			tx.AddError(errors.New("unexpected session create destination"))
			return
		}
		session.ID = sessionID
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		updateCount++
		tx.Statement.RowsAffected = 1
	})

	result, err := service.Login(context.Background(), " root ", "login-callback-password", "", "login-callback-agent", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if result.Token == "" || result.CSRFToken == "" || result.SessionID != sessionID || result.Admin.ID != adminID {
		t.Fatalf("Login result = %#v, want issued session for admin", result)
	}
	if !result.Admin.LastLoginAt.Valid || updateCount != 1 {
		t.Fatalf("Login last login valid/update count = %v/%d, want true/1", result.Admin.LastLoginAt.Valid, updateCount)
	}

	totpService := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Admin:
			*dest = []store.Admin{{ID: adminID, Username: "root", PasswordHash: passwordHash, Status: "active", TOTPEnabled: true}}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected totp query destination"))
		}
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("totp guard should not create a session"))
	}, nil)

	if _, err := totpService.Login(context.Background(), "root", "login-callback-password", " \t ", "", ""); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("Login missing TOTP error = %v, want ErrTOTPRequired", err)
	}
	if _, err := totpService.Login(context.Background(), "root", "login-callback-password", "000000", "", ""); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("Login invalid TOTP error = %v, want ErrTOTPInvalid", err)
	}
}

func TestResolveAPIKeyAccessSetsIncludesDirectAndGroupSitesOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteID := uuid.New()
	groupSiteID := uuid.New()
	groupID := uuid.New()
	siteModelAllowed := uuid.New()
	siteModelFiltered := uuid.New()
	canonicalID := uuid.New()
	queryStep := 0
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		queryStep++
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = store.APIKey{ID: apiKeyID, Name: "access-set-api-key", Status: "active", ModelPolicy: "allow_list", SitePolicy: "allow_list"}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySitePermission:
			*dest = []store.APIKeySitePermission{
				{APIKeyID: apiKeyID, SiteID: uuid.Nil, Enabled: true},
				{APIKeyID: apiKeyID, SiteID: siteID, Enabled: true},
				{APIKeyID: apiKeyID, SiteID: siteID, Enabled: true},
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *[]store.APIKeySiteGroupPermission:
			*dest = []store.APIKeySiteGroupPermission{
				{APIKeyID: apiKeyID, GroupID: groupID, Enabled: true, CreatedAt: time.Now()},
				{APIKeyID: apiKeyID, GroupID: uuid.New(), Enabled: false, CreatedAt: time.Now().Add(time.Second)},
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *[]store.SiteGroupSite:
			*dest = []store.SiteGroupSite{{GroupID: groupID, SiteID: groupSiteID}}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteGroup:
			*dest = []store.SiteGroup{{ID: groupID, Enabled: true}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySiteModelPermission:
			*dest = []store.APIKeySiteModelPermission{
				{APIKeyID: apiKeyID, SiteModelID: siteModelAllowed, Enabled: true},
				{APIKeyID: apiKeyID, SiteModelID: siteModelFiltered, Enabled: true},
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		case *store.SiteModel:
			switch queryStep {
			case 7:
				*dest = store.SiteModel{ID: siteModelAllowed, SiteID: siteID}
			default:
				*dest = store.SiteModel{ID: siteModelFiltered, SiteID: uuid.New()}
			}
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: canonicalID, ModelKey: "access-set-model", Status: "active"}}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected access set query destination"))
		}
	}, nil, nil)

	sets, err := service.ResolveAPIKeyAccessSets(context.Background(), apiKeyID)
	if err != nil {
		t.Fatalf("ResolveAPIKeyAccessSets returned error: %v", err)
	}
	if len(sets.AllowedSiteIDs) != 2 || sets.AllowedSiteIDs[0] != siteID || sets.AllowedSiteIDs[1] != groupSiteID {
		t.Fatalf("AllowedSiteIDs = %#v, want direct plus enabled group site", sets.AllowedSiteIDs)
	}
	if len(sets.AllowedSiteModelIDs) != 1 || sets.AllowedSiteModelIDs[0] != siteModelAllowed {
		t.Fatalf("AllowedSiteModelIDs = %#v, want filtered site model", sets.AllowedSiteModelIDs)
	}

	allowed, err := service.CheckSiteAccess(context.Background(), apiKeyID, groupSiteID)
	if err != nil || !allowed {
		t.Fatalf("CheckSiteAccess group site = %v, %v; want true nil", allowed, err)
	}
	allowed, err = service.CheckSiteAccess(context.Background(), apiKeyID, uuid.New())
	if err != nil || allowed {
		t.Fatalf("CheckSiteAccess missing site = %v, %v; want false nil", allowed, err)
	}
}

func TestResolveAPIKeyRouteAccessRejectsEmptySiteAndModelSetsOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	canonicalID := uuid.New()
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = store.APIKey{ID: apiKeyID, Name: "route-site-api-key", Status: "active", ModelPolicy: "allow_all", SitePolicy: "allow_list"}
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: canonicalID, ModelKey: "route-denied-model", Status: "active"}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySitePermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		case *[]store.APIKeySiteGroupPermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected route site query destination"))
		}
	}, nil, nil)

	result, err := service.ResolveAPIKeyRouteAccess(context.Background(), apiKeyID, "openai/route-denied-model")
	if !errors.Is(err, ErrSiteNotAllowed) {
		t.Fatalf("ResolveAPIKeyRouteAccess error = %v, want ErrSiteNotAllowed", err)
	}
	if result.Allowed || result.APIKey.ID != apiKeyID || result.ModelKey != "route-denied-model" {
		t.Fatalf("ResolveAPIKeyRouteAccess result = %#v, want denied result with canonical model", result)
	}

	modelService := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = store.APIKey{ID: apiKeyID, Name: "route-model-api-key", Status: "active", ModelPolicy: "allow_list", SitePolicy: "allow_all"}
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: canonicalID, ModelKey: "route-denied-model", Status: "active"}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySiteModelPermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected route model query destination"))
		}
	}, nil, nil)

	result, err = modelService.ResolveAPIKeyRouteAccess(context.Background(), apiKeyID, "route-denied-model")
	if !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("ResolveAPIKeyRouteAccess error = %v, want ErrModelNotAllowed", err)
	}
	if result.Allowed || result.APIKey.ID != apiKeyID || result.ModelKey != "route-denied-model" {
		t.Fatalf("ResolveAPIKeyRouteAccess result = %#v, want model-denied result", result)
	}
}

func TestAPIKeyPermissionReadersReturnSiteModelAndGroupDetailsOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	canonicalID := uuid.New()
	groupID := uuid.New()
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.APIKeySiteModelPermission:
			*dest = []store.APIKeySiteModelPermission{{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: siteModelID, Enabled: true}}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteModel:
			*dest = []store.SiteModel{{ID: siteModelID, SiteID: siteID, UpstreamName: "upstream", DisplayName: "display", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}}}
			tx.Statement.RowsAffected = 1
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, Name: "Permission Reader Site", Slug: "permission-reader", SiteType: "openai"}}
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: canonicalID, ModelKey: "permission-reader-model"}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySiteGroupPermission:
			*dest = []store.APIKeySiteGroupPermission{{ID: uuid.New(), APIKeyID: apiKeyID, GroupID: groupID, Enabled: true, CreatedAt: time.Now()}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySitePermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected permission reader destination"))
		}
	}, nil, nil)

	models, err := service.APIKeySiteModels(context.Background(), apiKeyID)
	if err != nil {
		t.Fatalf("APIKeySiteModels returned error: %v", err)
	}
	if len(models) != 1 || models[0].SiteID != siteID || !models[0].CanonicalModelKey.Valid {
		t.Fatalf("APIKeySiteModels = %#v, want populated detail", models)
	}
	effective, err := service.APIKeyEffectiveSiteModels(context.Background(), store.APIKey{ID: apiKeyID, SitePolicy: "allow_all"})
	if err != nil || len(effective) != 1 || effective[0].SiteModelID != siteModelID {
		t.Fatalf("APIKeyEffectiveSiteModels = %#v, %v; want same allow_all models", effective, err)
	}
	groups, err := service.APIKeySiteGroups(context.Background(), apiKeyID)
	if err != nil || len(groups) != 1 || groups[0].GroupID != groupID {
		t.Fatalf("APIKeySiteGroups = %#v, %v; want one group", groups, err)
	}

	emptyAccessService := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.APIKeySiteModelPermission:
			*dest = []store.APIKeySiteModelPermission{{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: siteModelID, Enabled: true}}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteModel:
			*dest = []store.SiteModel{{ID: siteModelID, SiteID: siteID, UpstreamName: "upstream"}}
			tx.Statement.RowsAffected = 1
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, Name: "Permission Reader Site"}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySitePermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		case *[]store.APIKeySiteGroupPermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected empty effective destination"))
		}
	}, nil, nil)
	effective, err = emptyAccessService.APIKeyEffectiveSiteModels(context.Background(), store.APIKey{ID: apiKeyID, SitePolicy: "allow_list"})
	if err != nil || len(effective) != 0 {
		t.Fatalf("APIKeyEffectiveSiteModels allow_list = %#v, %v; want empty without allowed sites", effective, err)
	}
}

func TestAPIKeyMutationGuardsValidateModelMappingsAndDuplicateKeysOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = store.APIKey{
				ID:             apiKeyID,
				Name:           "mutation-guard-current",
				Status:         "active",
				ModelPolicy:    "allow_all",
				SitePolicy:     "allow_all",
				ModelMappings:  store.JSON(`{"kept":true}`),
				QuotaLimit:     sql.NullFloat64{Float64: 20, Valid: true},
				QuotaUnlimited: false,
			}
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: uuid.New(), ModelKey: "mutation-guard-upstream"}}
			tx.Statement.RowsAffected = 1
		case *int64:
			*dest = 1
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected mutation guard query destination"))
		}
	}, nil, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 1
	})

	_, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{Name: "mutation-guard", ModelRules: []store.APIKeyModelRule{{Pattern: "downstream", Target: "missing-upstream"}}}, uuid.New())
	assertAuthErrorContains(t, "CreateAPIKey invalid mapping", err, "mapped model")
	if key, kind, err := service.createGatewayAPIKeyValue(context.Background(), "mutation-guard-custom-key"); key != "" || kind != "" || err == nil || err.Error() != "api key already exists" {
		t.Fatalf("createGatewayAPIKeyValue duplicate = %q/%q/%v, want duplicate error", key, kind, err)
	}
	_, err = service.UpdateAPIKey(context.Background(), apiKeyID, UpdateAPIKeyInput{ModelRules: []store.APIKeyModelRule{{Pattern: "downstream", Target: "missing-upstream"}}})
	assertAuthErrorContains(t, "UpdateAPIKey invalid mapping", err, "mapped model")
	_, err = service.SetAPIKeyModelMappings(context.Background(), apiKeyID, []store.APIKeyModelRule{{Pattern: "downstream", Target: "missing-upstream"}})
	assertAuthErrorContains(t, "SetAPIKeyModelMappings invalid mapping", err, "mapped model")
}
