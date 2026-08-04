package auth

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

func TestCreateAPIKeyWithCustomValuePersistsOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	adminID := uuid.New()
	service := NewService(authTransactionOnlyGorm(t), "auth-transaction-master-key")
	queryCounts := 0
	var createdAPIKey store.APIKey
	deleteCount := 0
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected create api key query destination"))
			return
		}
		queryCounts++
		*count = 0
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Row().Replace("gorm:row", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected create api key row destination"))
			return
		}
		queryCounts++
		*count = 0
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace row callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			createdAPIKey = *dest
			dest.ID = apiKeyID
		default:
			tx.AddError(errors.New("unexpected create api key create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	if err := service.db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		deleteCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}

	result, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		Name:           " Custom Gateway Key ",
		CustomKey:      "custom-gateway-key",
		ModelPolicy:    "allow_all",
		SitePolicy:     "allow_all",
		QuotaUnlimited: true,
	}, adminID)
	if err != nil {
		t.Fatalf("CreateAPIKey returned error: %v", err)
	}

	if result.Key != "custom-gateway-key" || result.KeyPrefix != apiKeyStoragePrefix("custom-gateway-key", apiKeyCustomKind) ||
		result.APIKey.ID != apiKeyID {
		t.Fatalf("CreateAPIKey result = %#v, want custom key result", result)
	}
	if createdAPIKey.Name != "Custom Gateway Key" || createdAPIKey.KeyKind != apiKeyCustomKind ||
		createdAPIKey.CreatedByAdminID == nil || *createdAPIKey.CreatedByAdminID != adminID ||
		!createdAPIKey.QuotaUnlimited || createdAPIKey.EncryptedSecret.String == "" {
		t.Fatalf("created api key = %#v, want trimmed custom gateway key", createdAPIKey)
	}
	if queryCounts != 1 || deleteCount != 2 {
		t.Fatalf("queryCounts=%d deleteCount=%d, want custom hash check and empty permission clears", queryCounts, deleteCount)
	}
}

func TestAPIKeyPermissionMutationsPersistAccessSetsOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteID := uuid.New()
	groupID := uuid.New()
	siteModelID := uuid.New()
	canonicalID := uuid.New()
	service := NewService(authTransactionOnlyGorm(t), "auth-transaction-master-key")
	queryStep := 0
	deleteCount := 0
	createCount := 0
	updateCount := 0
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		queryStep++
		switch dest := tx.Statement.Dest.(type) {
		case *store.SiteModel:
			*dest = store.SiteModel{ID: siteModelID, SiteID: siteID, UpstreamName: "transaction-upstream", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}}
			tx.Statement.RowsAffected = 1
		case *store.APIKey:
			*dest = store.APIKey{
				ID:             apiKeyID,
				Name:           "transaction key",
				Status:         "active",
				ModelPolicy:    "allow_all",
				SitePolicy:     "allow_all",
				QuotaUnlimited: true,
			}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySiteModelPermission:
			*dest = []store.APIKeySiteModelPermission{{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: siteModelID, Enabled: true, CreatedAt: time.Now()}}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteModel:
			*dest = []store.SiteModel{{ID: siteModelID, SiteID: siteID, UpstreamName: "transaction-upstream", DisplayName: "Transaction Model", CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}}}
			tx.Statement.RowsAffected = 1
		case *[]store.Site:
			*dest = []store.Site{{ID: siteID, Name: "Transaction Site", Slug: "transaction", SiteType: "openai"}}
			tx.Statement.RowsAffected = 1
		case *[]store.CanonicalModel:
			*dest = []store.CanonicalModel{{ID: canonicalID, ModelKey: "transaction-canonical"}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySitePermission:
			*dest = []store.APIKeySitePermission{{ID: uuid.New(), APIKeyID: apiKeyID, SiteID: siteID, Enabled: true}}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySiteGroupPermission:
			*dest = []store.APIKeySiteGroupPermission{{ID: uuid.New(), APIKeyID: apiKeyID, GroupID: groupID, Enabled: true}}
			tx.Statement.RowsAffected = 1
		default:
			tx.AddError(errors.New("unexpected permission mutation query destination"))
		}
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		deleteCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		createCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	if err := service.db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		updateCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}

	apiKey, models, err := service.SetAPIKeySiteModels(context.Background(), apiKeyID, []uuid.UUID{siteModelID, siteModelID}, "allow_list")
	if err != nil {
		t.Fatalf("SetAPIKeySiteModels returned error: %v", err)
	}
	if apiKey.ID != apiKeyID || len(models) != 1 || models[0].SiteModelID != siteModelID || !models[0].CanonicalModelKey.Valid {
		t.Fatalf("site model mutation = %#v %#v, want populated permission details", apiKey, models)
	}
	apiKey, sites, err := service.SetAPIKeySites(context.Background(), apiKeyID, []uuid.UUID{siteID}, "allow_list")
	if err != nil {
		t.Fatalf("SetAPIKeySites returned error: %v", err)
	}
	if apiKey.ID != apiKeyID || len(sites) != 1 || sites[0].SiteID != siteID {
		t.Fatalf("site mutation = %#v %#v, want site permission", apiKey, sites)
	}
	apiKey, groups, err := service.SetAPIKeySiteGroups(context.Background(), apiKeyID, []uuid.UUID{uuid.Nil, groupID, groupID})
	if err != nil {
		t.Fatalf("SetAPIKeySiteGroups returned error: %v", err)
	}
	if apiKey.ID != apiKeyID || len(groups) != 1 || groups[0].GroupID != groupID {
		t.Fatalf("group mutation = %#v %#v, want group permission", apiKey, groups)
	}
	if deleteCount != 3 || createCount != 3 || updateCount < 2 || queryStep == 0 {
		t.Fatalf("delete=%d create=%d update=%d queryStep=%d, want writes and lookups", deleteCount, createCount, updateCount, queryStep)
	}
}

func TestListAPIKeyDetailsFiltersModelsByEffectiveSitesOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	directSiteID := uuid.New()
	groupSiteID := uuid.New()
	disabledGroupSiteID := uuid.New()
	directModelID := uuid.New()
	groupModelID := uuid.New()
	disabledGroupModelID := uuid.New()
	enabledGroupID := uuid.New()
	disabledGroupID := uuid.New()
	service := NewService(authTransactionOnlyGorm(t), "auth-transaction-master-key")

	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.APIKey:
			*dest = []store.APIKey{{ID: apiKeyID, SitePolicy: "allow_list"}}
		case *[]store.APIKeySiteModelPermission:
			*dest = []store.APIKeySiteModelPermission{
				{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: directModelID, Enabled: true},
				{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: groupModelID, Enabled: true},
				{ID: uuid.New(), APIKeyID: apiKeyID, SiteModelID: disabledGroupModelID, Enabled: true},
			}
		case *[]store.SiteModel:
			*dest = []store.SiteModel{
				{ID: directModelID, SiteID: directSiteID},
				{ID: groupModelID, SiteID: groupSiteID},
				{ID: disabledGroupModelID, SiteID: disabledGroupSiteID},
			}
		case *[]store.Site:
			*dest = []store.Site{
				{ID: directSiteID, Name: "direct"},
				{ID: groupSiteID, Name: "group"},
				{ID: disabledGroupSiteID, Name: "disabled group"},
			}
		case *[]store.CanonicalModel:
			*dest = nil
		case *[]store.APIKeySitePermission:
			*dest = []store.APIKeySitePermission{{APIKeyID: apiKeyID, SiteID: directSiteID, Enabled: true}}
		case *[]store.APIKeySiteGroupPermission:
			*dest = []store.APIKeySiteGroupPermission{
				{APIKeyID: apiKeyID, GroupID: enabledGroupID, Enabled: true},
				{APIKeyID: apiKeyID, GroupID: disabledGroupID, Enabled: true},
			}
		case *[]store.SiteGroup:
			*dest = []store.SiteGroup{
				{ID: enabledGroupID, Enabled: true},
				{ID: disabledGroupID, Enabled: false},
			}
		case *[]store.SiteGroupSite:
			*dest = []store.SiteGroupSite{
				{GroupID: enabledGroupID, SiteID: groupSiteID},
				{GroupID: disabledGroupID, SiteID: disabledGroupSiteID},
			}
		case *[]store.GatewayRateLimit:
			*dest = nil
		default:
			tx.AddError(errors.New("unexpected api key details query destination"))
		}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}

	items, err := service.ListAPIKeyDetails(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeyDetails returned error: %v", err)
	}
	if len(items) != 1 || len(items[0].Models) != 2 {
		t.Fatalf("details = %#v, want direct and enabled-group models", items)
	}
	modelIDs := map[uuid.UUID]bool{}
	for _, model := range items[0].Models {
		modelIDs[model.SiteModelID] = true
	}
	if !modelIDs[directModelID] || !modelIDs[groupModelID] || modelIDs[disabledGroupModelID] {
		t.Fatalf("model ids = %#v, want effective sites only", modelIDs)
	}
	if items[0].RateLimit.Status != store.RateLimitStatusDisabled {
		t.Fatalf("rate limit = %#v, want disabled default", items[0].RateLimit)
	}
}

func TestUpdateAPIKeyPersistsAccessSetsInOneMutationOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	siteID := uuid.New()
	groupID := uuid.New()
	siteModelID := uuid.New()
	service := NewService(authTransactionOnlyGorm(t), "auth-transaction-master-key")
	deleteCount := 0
	createCount := 0
	updateCount := 0
	unlimited := true

	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = store.APIKey{
				ID:                   apiKeyID,
				Name:                 "before",
				Status:               "active",
				ModelPolicy:          "allow_all",
				SitePolicy:           "allow_all",
				QuotaUnlimited:       true,
				QuotaDailyUnlimited:  true,
				QuotaWeeklyUnlimited: true,
			}
		case *store.SiteModel:
			*dest = store.SiteModel{ID: siteModelID, SiteID: siteID}
		default:
			tx.AddError(errors.New("unexpected api key update query destination"))
		}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		deleteCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
	if err := service.db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		createCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	if err := service.db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		updateCount++
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}

	updated, err := service.UpdateAPIKey(context.Background(), apiKeyID, UpdateAPIKeyInput{
		Name:                 "after",
		ModelPolicy:          "allow_list",
		SiteModelIDs:         []uuid.UUID{siteModelID},
		SitePolicy:           "allow_list",
		SiteIDs:              []uuid.UUID{siteID},
		SiteGroupIDs:         []uuid.UUID{groupID},
		QuotaUnlimited:       true,
		QuotaDailyUnlimited:  &unlimited,
		QuotaWeeklyUnlimited: &unlimited,
	})
	if err != nil {
		t.Fatalf("UpdateAPIKey returned error: %v", err)
	}
	if updated.Name != "after" || updated.ModelPolicy != "allow_list" || updated.SitePolicy != "allow_list" {
		t.Fatalf("updated api key = %#v, want new policies", updated)
	}
	if deleteCount != 3 || createCount != 3 || updateCount != 1 {
		t.Fatalf("delete=%d create=%d update=%d, want three access replacements and one api key update", deleteCount, createCount, updateCount)
	}
}

func authTransactionOnlyGorm(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(authTransactionOnlyConnector{})
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:     true,
		DisableNestedTransaction: true,
		SkipDefaultTransaction:   true,
		Logger:                   gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open auth transaction-only gorm db: %v", err)
	}
	return db
}

type authTransactionOnlyConnector struct{}

func (authTransactionOnlyConnector) Connect(context.Context) (driver.Conn, error) {
	return authTransactionOnlyConn{}, nil
}

func (authTransactionOnlyConnector) Driver() driver.Driver {
	return authTransactionOnlyDriver{}
}

type authTransactionOnlyDriver struct{}

func (authTransactionOnlyDriver) Open(string) (driver.Conn, error) {
	return authTransactionOnlyConn{}, nil
}

type authTransactionOnlyConn struct{}

func (authTransactionOnlyConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("auth transaction-only fake driver does not support statements")
}

func (authTransactionOnlyConn) Close() error {
	return nil
}

func (authTransactionOnlyConn) Begin() (driver.Tx, error) {
	return authTransactionOnlyTx{}, nil
}

func (authTransactionOnlyConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return authTransactionOnlyTx{}, nil
}

type authTransactionOnlyTx struct{}

func (authTransactionOnlyTx) Commit() error {
	return nil
}

func (authTransactionOnlyTx) Rollback() error {
	return nil
}
