package store

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

type JSON []byte

func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSON) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch data := value.(type) {
	case []byte:
		*j = append((*j)[0:0], data...)
	case string:
		*j = append((*j)[0:0], data...)
	default:
		return fmt.Errorf("scan json: unsupported value type %T", value)
	}
	return nil
}

type IPAddress net.IP

func (ip IPAddress) Value() (driver.Value, error) {
	if len(ip) == 0 {
		return nil, nil
	}
	return net.IP(ip).String(), nil
}

func (ip *IPAddress) Scan(value any) error {
	if value == nil {
		*ip = nil
		return nil
	}

	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		return fmt.Errorf("scan ip address: unsupported value type %T", value)
	}

	parsed := net.ParseIP(raw)
	if parsed == nil {
		return fmt.Errorf("scan ip address: invalid ip %q", raw)
	}
	*ip = IPAddress(parsed)
	return nil
}

func (IPAddress) GormDataType() string {
	return "inet"
}

func (IPAddress) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Dialector.Name() {
	case "postgres":
		return "INET"
	default:
		return "TEXT"
	}
}

func (ip IPAddress) NetIP() net.IP {
	if len(ip) == 0 {
		return nil
	}
	return net.IP(ip)
}

func (JSON) GormDataType() string {
	return "json"
}

func (JSON) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Dialector.Name() {
	case "postgres":
		return "JSONB"
	default:
		return "JSON"
	}
}

func (Admin) TableName() string                { return "admins" }
func (AdminSession) TableName() string         { return "admin_sessions" }
func (AdminAccessToken) TableName() string     { return "admin_access_tokens" }
func (AdminAuditLog) TableName() string        { return "admin_audit_logs" }
func (APIKey) TableName() string               { return "api_keys" }
func (APIKeySitePermission) TableName() string { return "api_key_site_permissions" }
func (APIKeySiteGroupPermission) TableName() string {
	return "api_key_site_group_permissions"
}
func (APIKeySiteModelPermission) TableName() string {
	return "api_key_site_model_permissions"
}
func (Site) TableName() string                { return "sites" }
func (SiteGroup) TableName() string           { return "site_groups" }
func (SiteGroupSite) TableName() string       { return "site_group_sites" }
func (SiteCredential) TableName() string      { return "site_credentials" }
func (SiteState) TableName() string           { return "site_states" }
func (OAuthSession) TableName() string        { return "oauth_sessions" }
func (OAuthConnection) TableName() string     { return "oauth_connections" }
func (SiteAPIKeyState) TableName() string     { return "site_api_key_states" }
func (SiteSyncJob) TableName() string         { return "site_sync_jobs" }
func (SiteAPIKeyModel) TableName() string     { return "site_api_key_models" }
func (CanonicalModel) TableName() string      { return "canonical_models" }
func (CanonicalModelAlias) TableName() string { return "canonical_model_aliases" }
func (SiteModel) TableName() string           { return "site_models" }
func (SitePricingGroup) TableName() string    { return "site_pricing_groups" }
func (SiteModelPricing) TableName() string    { return "site_model_pricings" }
func (HealthSnapshot) TableName() string      { return "health_snapshots" }
func (SiteHealthState) TableName() string     { return "site_health_states" }
func (RouteCooldown) TableName() string       { return "route_cooldowns" }
func (AgentRun) TableName() string            { return "agent_runs" }
func (AgentLLMToken) TableName() string       { return "agent_llm_tokens" }
func (RequestLog) TableName() string          { return "request_logs" }
func (UsageRecord) TableName() string         { return "usage_records" }
func (RequestUsageDailySummary) TableName() string {
	return "request_usage_daily_summaries"
}
func (RequestUsageHourlySummary) TableName() string {
	return "request_usage_hourly_summaries"
}
func (RequestUsageSummaryDay) TableName() string {
	return "request_usage_summary_days"
}
func (GatewayRateLimit) TableName() string { return "gateway_rate_limits" }
func (GatewayRateLimitWindow) TableName() string {
	return "gateway_rate_limit_windows"
}

func jsonDefault(value JSON, fallback string) JSON {
	if len(value) > 0 {
		return value
	}
	return JSON([]byte(fallback))
}

func jsonFromAny(value any, fallback string) JSON {
	switch v := value.(type) {
	case nil:
		return JSON([]byte(fallback))
	case JSON:
		if len(v) > 0 {
			return v
		}
		return JSON([]byte(fallback))
	case []byte:
		if len(v) > 0 {
			return JSON(v)
		}
		return JSON([]byte(fallback))
	case string:
		if v != "" {
			return JSON([]byte(v))
		}
		return JSON([]byte(fallback))
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return JSON([]byte(fallback))
		}
		return JSON(encoded)
	}
}

func stringDefault(value string, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func jsonKeepNonEmpty(value JSON, existing JSON) JSON {
	switch strings.TrimSpace(string(value)) {
	case "", "[]", "{}", "null":
		if len(existing) > 0 {
			return existing
		}
		return jsonDefault(value, "[]")
	default:
		return value
	}
}

func nullInt32OrKeep(value sql.NullInt32, existing sql.NullInt32) sql.NullInt32 {
	if value.Valid {
		return value
	}
	return existing
}

func nullStringFromAny(value any) sql.NullString {
	switch v := value.(type) {
	case nil:
		return sql.NullString{}
	case sql.NullString:
		return v
	case string:
		return sql.NullString{String: v, Valid: true}
	default:
		return sql.NullString{}
	}
}

func nullFloatFromAny(value any) sql.NullFloat64 {
	switch v := value.(type) {
	case nil:
		return sql.NullFloat64{}
	case sql.NullFloat64:
		return v
	case float64:
		return sql.NullFloat64{Float64: v, Valid: true}
	case float32:
		return sql.NullFloat64{Float64: float64(v), Valid: true}
	case int:
		return sql.NullFloat64{Float64: float64(v), Valid: true}
	case int64:
		return sql.NullFloat64{Float64: float64(v), Valid: true}
	default:
		return sql.NullFloat64{}
	}
}

func nullInt64FromAny(value any) sql.NullInt64 {
	switch v := value.(type) {
	case nil:
		return sql.NullInt64{}
	case sql.NullInt64:
		return v
	case int:
		return sql.NullInt64{Int64: int64(v), Valid: true}
	case int64:
		return sql.NullInt64{Int64: v, Valid: true}
	case int32:
		return sql.NullInt64{Int64: int64(v), Valid: true}
	default:
		return sql.NullInt64{}
	}
}

func nullBoolFromAny(value any) sql.NullBool {
	switch v := value.(type) {
	case nil:
		return sql.NullBool{}
	case sql.NullBool:
		return v
	case bool:
		return sql.NullBool{Bool: v, Valid: true}
	default:
		return sql.NullBool{}
	}
}

func nullTimeFromAny(value any) sql.NullTime {
	switch v := value.(type) {
	case nil:
		return sql.NullTime{}
	case sql.NullTime:
		return v
	case time.Time:
		if v.IsZero() {
			return sql.NullTime{}
		}
		return sql.NullTime{Time: v, Valid: true}
	case *time.Time:
		if v == nil || v.IsZero() {
			return sql.NullTime{}
		}
		return sql.NullTime{Time: *v, Valid: true}
	default:
		return sql.NullTime{}
	}
}

func timePtrFromAny(value any) *time.Time {
	switch v := value.(type) {
	case nil:
		return nil
	case time.Time:
		if v.IsZero() {
			return nil
		}
		return &v
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil
		}
		return v
	case sql.NullTime:
		if !v.Valid {
			return nil
		}
		t := v.Time
		return &t
	default:
		return nil
	}
}

func uuidPtrFromAny(value any) *uuid.UUID {
	switch v := value.(type) {
	case nil:
		return nil
	case uuid.UUID:
		if v == uuid.Nil {
			return nil
		}
		return &v
	case *uuid.UUID:
		if v == nil || *v == uuid.Nil {
			return nil
		}
		return v
	default:
		return nil
	}
}

func nullUUIDFromAny(value any) uuid.NullUUID {
	switch v := value.(type) {
	case nil:
		return uuid.NullUUID{}
	case uuid.NullUUID:
		return v
	case uuid.UUID:
		if v == uuid.Nil {
			return uuid.NullUUID{}
		}
		return uuid.NullUUID{UUID: v, Valid: true}
	case *uuid.UUID:
		if v == nil || *v == uuid.Nil {
			return uuid.NullUUID{}
		}
		return uuid.NullUUID{UUID: *v, Valid: true}
	default:
		return uuid.NullUUID{}
	}
}
