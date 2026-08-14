package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/migrations"

	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const databaseInitRetryInterval = 500 * time.Millisecond

const apiKeyPeriodicQuotaBackfillMarker = "api_keys_periodic_quota_v1"
const apiKeyTotalQuotaBackfillMarker = "api_keys_total_quota_v1"
const openAIKeyPricingUpgradeMarker = "openai_key_pricing_v1"

type schemaUpgradeMarker struct {
	Name        string `gorm:"primaryKey;size:128"`
	CompletedAt time.Time
}

func (schemaUpgradeMarker) TableName() string {
	return "schema_upgrade_markers"
}

var requiredBootstrapTables = []string{
	"admins",
	"admin_sessions",
	"admin_access_tokens",
	"admin_audit_logs",
	"api_keys",
	"sites",
	"site_groups",
	"site_group_sites",
	"site_states",
	"oauth_sessions",
	"oauth_connections",
	"site_credentials",
	"site_api_key_states",
	"canonical_models",
	"api_key_site_permissions",
	"api_key_site_group_permissions",
	"site_models",
	"api_key_site_model_permissions",
	"canonical_model_aliases",
	"site_api_key_models",
	"site_pricing_groups",
	"site_model_pricings",
	"route_cooldowns",
	"request_logs",
	"usage_records",
	"gateway_rate_limits",
	"gateway_rate_limit_windows",
	"health_snapshots",
	"site_health_states",
}

func EnsureDatabaseInitialized(ctx context.Context, cfg config.Config) error {
	for {
		err := ensureDatabaseInitializedOnce(ctx, cfg)
		if err == nil {
			return nil
		}
		if !databaseInitRetryable(err) {
			return err
		}

		timer := time.NewTimer(databaseInitRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}

func ensureDatabaseInitializedOnce(ctx context.Context, cfg config.Config) error {
	if strings.TrimSpace(cfg.DatabaseName()) == "" {
		return fmt.Errorf("target database name is required")
	}
	store, err := Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("ping target database: %w", err)
	}
	defer store.Close()
	db := store.DB().WithContext(ctx)

	missing, err := missingRequiredTables(db)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return ensureSchemaUpgrades(ctx, db)
	}

	tableCount, err := publicTableCount(db)
	if err != nil {
		return err
	}
	if tableCount > 0 {
		return fmt.Errorf("database %q has partial or incompatible schema; missing required tables: %s", cfg.DatabaseName(), strings.Join(missing, ", "))
	}

	if err := runSchemaMigrations(ctx, db); err != nil {
		return fmt.Errorf("initialize database schema: %w", err)
	}

	missing, err = missingRequiredTables(db)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("database schema initialization completed with missing tables: %s", strings.Join(missing, ", "))
	}
	return ensureSchemaUpgrades(ctx, db)
}

func ensureSchemaUpgrades(ctx context.Context, db *gorm.DB) error {
	migrator := db.Migrator()
	if !migrator.HasTable(&schemaUpgradeMarker{}) {
		if err := migrator.CreateTable(&schemaUpgradeMarker{}); err != nil {
			return fmt.Errorf("ensure schema upgrade markers table: %w", err)
		}
	}
	if !migrator.HasTable(&RequestUsageDailySummary{}) {
		if err := migrator.CreateTable(&RequestUsageDailySummary{}); err != nil {
			return fmt.Errorf("ensure request_usage_daily_summaries table: %w", err)
		}
	}
	if !migrator.HasTable(&RequestUsageSummaryDay{}) {
		if err := migrator.CreateTable(&RequestUsageSummaryDay{}); err != nil {
			return fmt.Errorf("ensure request_usage_summary_days table: %w", err)
		}
	}
	if err := ensureSchemaIndex(migrator, &RequestUsageDailySummary{}, "request_usage_daily_summaries_bucket_idx"); err != nil {
		return err
	}
	if err := ensureSchemaIndex(migrator, &RequestUsageDailySummary{}, "request_usage_daily_summaries_site_bucket_idx"); err != nil {
		return err
	}
	if err := ensureSchemaIndex(migrator, &RequestUsageDailySummary{}, "request_usage_daily_summaries_model_bucket_idx"); err != nil {
		return err
	}
	if err := ensureSchemaIndex(migrator, &RequestUsageDailySummary{}, "request_usage_daily_summaries_currency_bucket_idx"); err != nil {
		return err
	}
	if err := ensureSchemaIndex(migrator, &RequestUsageSummaryDay{}, "request_usage_summary_days_status_idx"); err != nil {
		return err
	}
	if err := ensureOAuthConnectionEmailIdentity(db, migrator); err != nil {
		return err
	}
	if !migrator.HasColumn(&APIKey{}, "KeyKind") {
		if err := migrator.AddColumn(&APIKey{}, "KeyKind"); err != nil {
			return fmt.Errorf("ensure api_keys.key_kind column: %w", err)
		}
	}
	hadUpstreamCostMultiplier := migrator.HasColumn(&SiteCredential{}, "UpstreamCostMultiplier")
	for _, field := range []string{"DisplayName", "RoutingPriority", "UpstreamCostMultiplier"} {
		if migrator.HasColumn(&SiteCredential{}, field) {
			continue
		}
		if err := migrator.AddColumn(&SiteCredential{}, field); err != nil {
			return fmt.Errorf("ensure site_credentials routing column %s: %w", field, err)
		}
	}
	if err := ensureOpenAIKeyPricingUpgrade(ctx, db, !hadUpstreamCostMultiplier); err != nil {
		return err
	}
	if !migrator.HasColumn(&SiteModelPricing{}, "CreateCache1hRatio") {
		if err := migrator.AddColumn(&SiteModelPricing{}, "CreateCache1hRatio"); err != nil {
			return fmt.Errorf("ensure site_model_pricings.create_cache_1h_ratio column: %w", err)
		}
	}
	if !migrator.HasColumn(&CanonicalModel{}, "CacheWrite1hRatio") {
		if err := migrator.AddColumn(&CanonicalModel{}, "CacheWrite1hRatio"); err != nil {
			return fmt.Errorf("ensure canonical_models.cache_write_1h_ratio column: %w", err)
		}
	}
	if !migrator.HasColumn(&CanonicalModel{}, "AudioRatio") {
		if err := migrator.AddColumn(&CanonicalModel{}, "AudioRatio"); err != nil {
			return fmt.Errorf("ensure canonical_models.audio_ratio column: %w", err)
		}
	}
	if !migrator.HasColumn(&CanonicalModel{}, "AudioCompletionRatio") {
		if err := migrator.AddColumn(&CanonicalModel{}, "AudioCompletionRatio"); err != nil {
			return fmt.Errorf("ensure canonical_models.audio_completion_ratio column: %w", err)
		}
	}
	if !migrator.HasColumn(&APIKey{}, "ImageToolBridge") {
		if err := migrator.AddColumn(&APIKey{}, "ImageToolBridge"); err != nil {
			return fmt.Errorf("ensure api_keys.image_tool_bridge column: %w", err)
		}
	}
	for _, field := range []string{
		"QuotaTotalUsed",
		"QuotaTotalResetAt",
		"QuotaDailyLimit",
		"QuotaDailyUsed",
		"QuotaDailyUnlimited",
		"QuotaDailyWindowStart",
		"QuotaWeeklyLimit",
		"QuotaWeeklyUsed",
		"QuotaWeeklyUnlimited",
		"QuotaWeeklyWindowStart",
	} {
		if migrator.HasColumn(&APIKey{}, field) {
			continue
		}
		if err := migrator.AddColumn(&APIKey{}, field); err != nil {
			return fmt.Errorf("ensure api_keys quota column %s: %w", field, err)
		}
	}
	if err := ensureAPIKeyTotalQuotaBackfill(ctx, db, time.Now()); err != nil {
		return err
	}
	if err := ensureAPIKeyPeriodicQuotaBackfill(ctx, db, config.ResolveTimeZone(), time.Now()); err != nil {
		return err
	}
	if !migrator.HasColumn(&RequestLog{}, "Internal") {
		if err := migrator.AddColumn(&RequestLog{}, "Internal"); err != nil {
			return fmt.Errorf("ensure request_logs.internal column: %w", err)
		}
	}
	if !migrator.HasColumn(&RequestLog{}, "ParentRequestLogID") {
		if err := migrator.AddColumn(&RequestLog{}, "ParentRequestLogID"); err != nil {
			return fmt.Errorf("ensure request_logs.parent_request_log_id column: %w", err)
		}
	}
	if err := ensureSchemaIndex(migrator, &RequestLog{}, "request_logs_parent_request_log_id_idx"); err != nil {
		return err
	}
	if !migrator.HasColumn(&RequestUsageDailySummary{}, "Internal") {
		if err := migrator.AddColumn(&RequestUsageDailySummary{}, "Internal"); err != nil {
			return fmt.Errorf("ensure request_usage_daily_summaries.internal column: %w", err)
		}
	}
	if !migrator.HasColumn(&UsageRecord{}, "CachedTokens") {
		if err := migrator.AddColumn(&UsageRecord{}, "CachedTokens"); err != nil {
			return fmt.Errorf("ensure usage_records.cached_tokens column: %w", err)
		}
	}
	for _, field := range []string{"CacheWriteTokens", "CacheCreationInputTokens", "CacheCreation5mInputTokens", "CacheCreation1hInputTokens"} {
		if migrator.HasColumn(&UsageRecord{}, field) {
			continue
		}
		if err := migrator.AddColumn(&UsageRecord{}, field); err != nil {
			return fmt.Errorf("ensure usage_records cache write column %s: %w", field, err)
		}
	}
	if !migrator.HasColumn(&RequestUsageDailySummary{}, "CachedTokens") {
		if err := migrator.AddColumn(&RequestUsageDailySummary{}, "CachedTokens"); err != nil {
			return fmt.Errorf("ensure request_usage_daily_summaries.cached_tokens column: %w", err)
		}
	}
	for _, field := range []string{"CacheWriteTokens", "CacheCreationInputTokens", "CacheCreation5mInputTokens", "CacheCreation1hInputTokens"} {
		if migrator.HasColumn(&RequestUsageDailySummary{}, field) {
			continue
		}
		if err := migrator.AddColumn(&RequestUsageDailySummary{}, field); err != nil {
			return fmt.Errorf("ensure request_usage_daily_summaries cache write column %s: %w", field, err)
		}
	}
	if err := ensureAPIKeyModelRuleFormat(db); err != nil {
		return err
	}
	if err := ensureAdminSessionExpiresAtNullable(migrator); err != nil {
		return err
	}
	return nil
}

func ensureAPIKeyTotalQuotaBackfill(ctx context.Context, db *gorm.DB, now time.Time) error {
	var marker schemaUpgradeMarker
	markerErr := db.WithContext(ctx).Where(&schemaUpgradeMarker{Name: apiKeyTotalQuotaBackfillMarker}).First(&marker).Error
	if errors.Is(markerErr, gorm.ErrRecordNotFound) {
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var apiKeys []APIKey
			if err := tx.Find(&apiKeys).Error; err != nil {
				return fmt.Errorf("inspect api keys for total quota backfill: %w", err)
			}
			for i := range apiKeys {
				apiKeys[i].QuotaTotalUsed = apiKeys[i].QuotaUsed
				if err := tx.Save(&apiKeys[i]).Error; err != nil {
					return fmt.Errorf("backfill api key total quota: %w", err)
				}
			}
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaUpgradeMarker{
				Name:        apiKeyTotalQuotaBackfillMarker,
				CompletedAt: now,
			}).Error
		})
		if err != nil {
			return fmt.Errorf("backfill api key total quota: %w", err)
		}
	} else if markerErr != nil {
		return fmt.Errorf("check api key total quota backfill marker: %w", markerErr)
	}
	return nil
}

func ensureAPIKeyPeriodicQuotaBackfill(ctx context.Context, db *gorm.DB, timeZone config.TimeZone, now time.Time) error {
	var marker schemaUpgradeMarker
	markerErr := db.Where(&schemaUpgradeMarker{Name: apiKeyPeriodicQuotaBackfillMarker}).First(&marker).Error
	if errors.Is(markerErr, gorm.ErrRecordNotFound) {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := backfillCurrentAPIKeyPeriodicQuota(ctx, tx, timeZone, now); err != nil {
				return err
			}
			return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaUpgradeMarker{
				Name:        apiKeyPeriodicQuotaBackfillMarker,
				CompletedAt: now,
			}).Error
		})
		if err != nil {
			return fmt.Errorf("backfill api key periodic quota: %w", err)
		}
	} else if markerErr != nil {
		return fmt.Errorf("check api key periodic quota backfill marker: %w", markerErr)
	}
	return nil
}

func backfillCurrentAPIKeyPeriodicQuota(ctx context.Context, db *gorm.DB, timeZone config.TimeZone, now time.Time) error {
	weeklyStart := timeZone.StartOfWeek(now)
	weeklyEnd := weeklyStart.AddDate(0, 0, 7)
	rows, err := NewRequestUsageSummaryRepository(db).List(ctx, RequestUsageSummaryQuery{
		TimeZone: timeZone.Name,
		From:     &weeklyStart,
		To:       &weeklyEnd,
	})
	if err != nil {
		return fmt.Errorf("backfill api key periodic quota summaries: %w", err)
	}
	dailyStart := timeZone.StartOfDay(now)
	type usage struct {
		daily  float64
		weekly float64
	}
	usageByAPIKey := map[uuid.UUID]usage{}
	for _, row := range rows {
		if !row.APIKeyID.Valid {
			continue
		}
		item := usageByAPIKey[row.APIKeyID.UUID]
		item.weekly += row.EstimatedCost
		if row.BucketStart.Equal(dailyStart) {
			item.daily += row.EstimatedCost
		}
		usageByAPIKey[row.APIKeyID.UUID] = item
	}
	var apiKeys []APIKey
	if err := db.Find(&apiKeys).Error; err != nil {
		return fmt.Errorf("backfill api key periodic quota keys: %w", err)
	}
	for _, apiKey := range apiKeys {
		item := usageByAPIKey[apiKey.ID]
		apiKey.QuotaDailyUsed = item.daily
		apiKey.QuotaDailyWindowStart = &dailyStart
		apiKey.QuotaWeeklyUsed = item.weekly
		apiKey.QuotaWeeklyWindowStart = &weeklyStart
		if err := db.Save(&apiKey).Error; err != nil {
			return fmt.Errorf("backfill api key periodic quota: %w", err)
		}
	}
	return nil
}

func ensureAPIKeyModelRuleFormat(db *gorm.DB) error {
	var apiKeys []APIKey
	if err := db.Find(&apiKeys).Error; err != nil {
		return fmt.Errorf("inspect api key model mappings: %w", err)
	}
	for _, apiKey := range apiKeys {
		migrated, ok := migrateLegacyModelMappings(apiKey.ModelMappings)
		if !ok {
			continue
		}
		if err := db.Model(&APIKey{}).Where(&APIKey{ID: apiKey.ID}).Updates(map[string]any{"model_mappings": migrated}).Error; err != nil {
			return fmt.Errorf("migrate api key model mappings: %w", err)
		}
	}
	return nil
}

func migrateLegacyModelMappings(data JSON) (JSON, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var legacy map[string]string
	if err := json.Unmarshal(trimmed, &legacy); err != nil {
		return nil, false
	}
	rules := LegacyAPIKeyModelRules(legacy)
	if rules == nil {
		rules = []APIKeyModelRule{}
	}
	encoded, err := json.Marshal(rules)
	if err != nil {
		return nil, false
	}
	return JSON(encoded), true
}

func ensureOAuthConnectionEmailIdentity(db *gorm.DB, migrator gorm.Migrator) error {
	if !migrator.HasIndex(&OAuthConnection{}, "oauth_connections_provider_email_unique") {
		duplicated, err := oauthConnectionEmailIdentityHasDuplicates(db)
		if err != nil {
			return err
		}
		if !duplicated {
			if err := migrator.CreateIndex(&OAuthConnection{}, "oauth_connections_provider_email_unique"); err != nil {
				if !oauthConnectionEmailIdentityDuplicateIndexError(err) {
					return fmt.Errorf("ensure oauth_connections_provider_email_unique index: %w", err)
				}
			}
		}
	}
	if migrator.HasConstraint(&OAuthConnection{}, "oauth_connections_provider_account_id_key") {
		if err := migrator.DropConstraint(&OAuthConnection{}, "oauth_connections_provider_account_id_key"); err != nil {
			return fmt.Errorf("drop oauth_connections_provider_account_id_key constraint: %w", err)
		}
	}
	return nil
}

func oauthConnectionEmailIdentityHasDuplicates(db *gorm.DB) (bool, error) {
	var items []OAuthConnection
	if err := db.Find(&items).Error; err != nil {
		return false, fmt.Errorf("inspect oauth connection email identity: %w", err)
	}
	return hasDuplicateOAuthConnectionEmailIdentity(items), nil
}

func hasDuplicateOAuthConnectionEmailIdentity(items []OAuthConnection) bool {
	seen := map[string]struct{}{}
	for _, item := range items {
		key := item.Provider + "\x00" + item.Email
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
	}
	return false
}

func oauthConnectionEmailIdentityDuplicateIndexError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicated key") || strings.Contains(message, "duplicate key")
}

func ensureSchemaIndex(migrator gorm.Migrator, model any, name string) error {
	if migrator.HasIndex(model, name) {
		return nil
	}
	if err := migrator.CreateIndex(model, name); err != nil {
		return fmt.Errorf("ensure %s index: %w", name, err)
	}
	return nil
}

func ensureAdminSessionExpiresAtNullable(migrator gorm.Migrator) error {
	columns, err := migrator.ColumnTypes(&AdminSession{})
	if err != nil {
		return fmt.Errorf("inspect admin_sessions.expires_at column: %w", err)
	}
	for _, column := range columns {
		if !strings.EqualFold(column.Name(), "expires_at") {
			continue
		}
		nullable, ok := column.Nullable()
		if ok && nullable {
			return nil
		}
		if err := migrator.AlterColumn(&AdminSession{}, "ExpiresAt"); err != nil {
			return fmt.Errorf("ensure admin_sessions.expires_at nullable: %w", err)
		}
		return nil
	}
	return nil
}

func databaseInitRetryable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "ping maintenance database") || strings.Contains(message, "ping target database")
}

func missingRequiredTables(db *gorm.DB) ([]string, error) {
	migrator := db.Migrator()
	models := bootstrapModelsByTable()
	missing := make([]string, 0)
	for _, table := range requiredBootstrapTables {
		model, ok := models[table]
		if !ok {
			return nil, fmt.Errorf("bootstrap model for table %s was not registered", table)
		}
		if !migrator.HasTable(model) {
			missing = append(missing, table)
		}
	}
	return missing, nil
}

func publicTableCount(db *gorm.DB) (int, error) {
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return 0, fmt.Errorf("count public tables: %w", err)
	}
	count := 0
	for _, table := range tables {
		if table != "goose_db_version" {
			count++
		}
	}
	return count, nil
}

func runSchemaMigrations(ctx context.Context, db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	goose.SetBaseFS(migrations.FS)
	return goose.UpContext(ctx, sqlDB, ".")
}

func bootstrapModelsByTable() map[string]any {
	result := map[string]any{}
	for _, model := range bootstrapModels() {
		switch item := model.(type) {
		case interface{ TableName() string }:
			result[item.TableName()] = model
		}
	}
	return result
}

func bootstrapModels() []any {
	return []any{
		&Admin{},
		&AdminSession{},
		&AdminAccessToken{},
		&AdminAuditLog{},
		&APIKey{},
		&Site{},
		&SiteGroup{},
		&SiteGroupSite{},
		&SiteState{},
		&OAuthSession{},
		&OAuthConnection{},
		&SiteCredential{},
		&SiteAPIKeyState{},
		&CanonicalModel{},
		&APIKeySitePermission{},
		&APIKeySiteGroupPermission{},
		&SiteModel{},
		&APIKeySiteModelPermission{},
		&CanonicalModelAlias{},
		&SiteAPIKeyModel{},
		&SitePricingGroup{},
		&SiteModelPricing{},
		&RouteCooldown{},
		&RequestLog{},
		&UsageRecord{},
		&RequestUsageDailySummary{},
		&RequestUsageSummaryDay{},
		&GatewayRateLimit{},
		&GatewayRateLimitWindow{},
		&HealthSnapshot{},
		&SiteHealthState{},
	}
}
