package store

import (
	"database/sql"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func TestTypedValueGormDataTypesByDialect(t *testing.T) {
	t.Parallel()

	if got := (JSON{}).GormDataType(); got != "json" {
		t.Fatalf("JSON GormDataType = %q, want json", got)
	}
	if got := (JSON{}).GormDBDataType(storeTestDB("postgres"), nil); got != "JSONB" {
		t.Fatalf("postgres JSON DB type = %q, want JSONB", got)
	}
	if got := (JSON{}).GormDBDataType(storeTestDB("sqlite"), nil); got != "JSON" {
		t.Fatalf("generic JSON DB type = %q, want JSON", got)
	}

	if got := (IPAddress{}).GormDataType(); got != "inet" {
		t.Fatalf("IPAddress GormDataType = %q, want inet", got)
	}
	if got := (IPAddress{}).GormDBDataType(storeTestDB("postgres"), nil); got != "INET" {
		t.Fatalf("postgres IPAddress DB type = %q, want INET", got)
	}
	if got := (IPAddress{}).GormDBDataType(storeTestDB("sqlite"), nil); got != "TEXT" {
		t.Fatalf("generic IPAddress DB type = %q, want TEXT", got)
	}
}

func TestIPAddressValueScanRejectsEmptyInvalidAndUnsupported(t *testing.T) {
	t.Parallel()

	var ip IPAddress
	value, err := ip.Value()
	if err != nil {
		t.Fatalf("empty IP value: %v", err)
	}
	if value != nil {
		t.Fatalf("empty IP value = %#v, want nil", value)
	}
	if got := ip.NetIP(); got != nil {
		t.Fatalf("empty IP NetIP = %#v, want nil", got)
	}

	if err := ip.Scan("not-an-ip"); err == nil {
		t.Fatal("expected invalid IP string to fail")
	}
	if err := ip.Scan(123); err == nil {
		t.Fatal("expected unsupported IP scan type to fail")
	}

	if err := ip.Scan(net.ParseIP("10.0.0.1").String()); err != nil {
		t.Fatalf("scan valid IP before nil clear: %v", err)
	}
	if err := ip.Scan(nil); err != nil {
		t.Fatalf("scan nil IP: %v", err)
	}
	if ip != nil {
		t.Fatalf("nil IP scan should clear value, got %#v", ip)
	}
}

func TestJSONDefaultAndJSONFromAnyBranches(t *testing.T) {
	t.Parallel()

	if got := jsonDefault(JSON(`{"kept":true}`), "{}"); string(got) != `{"kept":true}` {
		t.Fatalf("jsonDefault kept value = %s", got)
	}

	tests := []struct {
		name     string
		value    any
		fallback string
		want     string
	}{
		{name: "nil", value: nil, fallback: "{}", want: "{}"},
		{name: "json value", value: JSON(`{"ok":true}`), fallback: "{}", want: `{"ok":true}`},
		{name: "empty json value", value: JSON{}, fallback: "{}", want: "{}"},
		{name: "bytes", value: []byte(`[1]`), fallback: "[]", want: "[1]"},
		{name: "empty bytes", value: []byte{}, fallback: "[]", want: "[]"},
		{name: "string", value: `{"text":true}`, fallback: "{}", want: `{"text":true}`},
		{name: "empty string", value: "", fallback: "{}", want: "{}"},
	}

	for _, tt := range tests {
		got := jsonFromAny(tt.value, tt.fallback)
		if string(got) != tt.want {
			t.Fatalf("%s jsonFromAny = %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestNullableHelpersKeepTypedValuesAndRejectUnsupported(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	id := uuid.New()

	if got := stringDefault("", "fallback"); got != "fallback" {
		t.Fatalf("stringDefault empty = %q, want fallback", got)
	}
	if got := stringDefault("value", "fallback"); got != "value" {
		t.Fatalf("stringDefault value = %q, want value", got)
	}

	if got := nullStringFromAny(sql.NullString{String: "typed", Valid: true}); !got.Valid || got.String != "typed" {
		t.Fatalf("nullStringFromAny typed = %#v", got)
	}
	if got := nullStringFromAny(nil); got.Valid {
		t.Fatalf("nullStringFromAny nil = %#v, want invalid", got)
	}

	if got := nullFloatFromAny(sql.NullFloat64{Float64: 1.25, Valid: true}); !got.Valid || got.Float64 != 1.25 {
		t.Fatalf("nullFloatFromAny typed = %#v", got)
	}
	if got := nullFloatFromAny(float64(2.5)); !got.Valid || got.Float64 != 2.5 {
		t.Fatalf("nullFloatFromAny float64 = %#v", got)
	}
	if got := nullFloatFromAny(int(3)); !got.Valid || got.Float64 != 3 {
		t.Fatalf("nullFloatFromAny int = %#v", got)
	}
	if got := nullFloatFromAny("3"); got.Valid {
		t.Fatalf("nullFloatFromAny unsupported = %#v, want invalid", got)
	}

	if got := nullInt64FromAny(sql.NullInt64{Int64: 4, Valid: true}); !got.Valid || got.Int64 != 4 {
		t.Fatalf("nullInt64FromAny typed = %#v", got)
	}
	if got := nullInt64FromAny(int(5)); !got.Valid || got.Int64 != 5 {
		t.Fatalf("nullInt64FromAny int = %#v", got)
	}
	if got := nullInt64FromAny(int64(6)); !got.Valid || got.Int64 != 6 {
		t.Fatalf("nullInt64FromAny int64 = %#v", got)
	}
	if got := nullInt64FromAny("6"); got.Valid {
		t.Fatalf("nullInt64FromAny unsupported = %#v, want invalid", got)
	}

	if got := nullBoolFromAny(sql.NullBool{Bool: true, Valid: true}); !got.Valid || !got.Bool {
		t.Fatalf("nullBoolFromAny typed = %#v", got)
	}
	if got := nullBoolFromAny("true"); got.Valid {
		t.Fatalf("nullBoolFromAny unsupported = %#v, want invalid", got)
	}

	if got := nullTimeFromAny(sql.NullTime{Time: now, Valid: true}); !got.Valid || !got.Time.Equal(now) {
		t.Fatalf("nullTimeFromAny typed = %#v", got)
	}
	if got := nullTimeFromAny(&now); !got.Valid || !got.Time.Equal(now) {
		t.Fatalf("nullTimeFromAny pointer = %#v", got)
	}
	var nilTime *time.Time
	if got := nullTimeFromAny(nilTime); got.Valid {
		t.Fatalf("nullTimeFromAny nil pointer = %#v, want invalid", got)
	}

	if got := timePtrFromAny(now); got == nil || !got.Equal(now) {
		t.Fatalf("timePtrFromAny time = %#v", got)
	}
	if got := timePtrFromAny(&now); got == nil || !got.Equal(now) {
		t.Fatalf("timePtrFromAny pointer = %#v", got)
	}
	if got := timePtrFromAny(nilTime); got != nil {
		t.Fatalf("timePtrFromAny nil pointer = %#v, want nil", got)
	}
	if got := timePtrFromAny("now"); got != nil {
		t.Fatalf("timePtrFromAny unsupported = %#v, want nil", got)
	}

	if got := uuidPtrFromAny(&id); got == nil || *got != id {
		t.Fatalf("uuidPtrFromAny pointer = %#v", got)
	}
	zeroID := uuid.Nil
	if got := uuidPtrFromAny(&zeroID); got != nil {
		t.Fatalf("uuidPtrFromAny nil UUID pointer = %#v, want nil", got)
	}
	var nilID *uuid.UUID
	if got := uuidPtrFromAny(nilID); got != nil {
		t.Fatalf("uuidPtrFromAny nil pointer = %#v, want nil", got)
	}

	if got := nullUUIDFromAny(uuid.NullUUID{UUID: id, Valid: true}); !got.Valid || got.UUID != id {
		t.Fatalf("nullUUIDFromAny typed = %#v", got)
	}
	if got := nullUUIDFromAny(id); !got.Valid || got.UUID != id {
		t.Fatalf("nullUUIDFromAny UUID = %#v", got)
	}
	if got := nullUUIDFromAny(uuid.Nil); got.Valid {
		t.Fatalf("nullUUIDFromAny nil UUID = %#v, want invalid", got)
	}
	if got := nullUUIDFromAny("uuid"); got.Valid {
		t.Fatalf("nullUUIDFromAny unsupported = %#v, want invalid", got)
	}
}

func TestRemainingModelTableNamesStayStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "SiteGroup", got: (SiteGroup{}).TableName(), want: "site_groups"},
		{name: "SiteGroupSite", got: (SiteGroupSite{}).TableName(), want: "site_group_sites"},
		{name: "SiteState", got: (SiteState{}).TableName(), want: "site_states"},
		{name: "OAuthSession", got: (OAuthSession{}).TableName(), want: "oauth_sessions"},
		{name: "SiteAPIKeyState", got: (SiteAPIKeyState{}).TableName(), want: "site_api_key_states"},
		{name: "SiteAPIKeyModel", got: (SiteAPIKeyModel{}).TableName(), want: "site_api_key_models"},
		{name: "CanonicalModel", got: (CanonicalModel{}).TableName(), want: "canonical_models"},
		{name: "CanonicalModelAlias", got: (CanonicalModelAlias{}).TableName(), want: "canonical_model_aliases"},
		{name: "SitePricingGroup", got: (SitePricingGroup{}).TableName(), want: "site_pricing_groups"},
		{name: "HealthSnapshot", got: (HealthSnapshot{}).TableName(), want: "health_snapshots"},
		{name: "SiteHealthState", got: (SiteHealthState{}).TableName(), want: "site_health_states"},
		{name: "RouteCooldown", got: (RouteCooldown{}).TableName(), want: "route_cooldowns"},
		{name: "UsageRecord", got: (UsageRecord{}).TableName(), want: "usage_records"},
		{name: "RequestUsageSummaryDay", got: (RequestUsageSummaryDay{}).TableName(), want: "request_usage_summary_days"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s table = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestGormSchemaMetadataForStoreModels(t *testing.T) {
	t.Parallel()

	apiKeySchema := parseStoreSchema(t, &APIKey{})
	if apiKeySchema.Table != "api_keys" {
		t.Fatalf("APIKey schema table = %q, want api_keys", apiKeySchema.Table)
	}
	modelMappings := apiKeySchema.LookUpField("ModelMappings")
	if modelMappings == nil || modelMappings.DBName != "model_mappings" || modelMappings.TagSettings["TYPE"] != "jsonb" {
		t.Fatalf("APIKey ModelMappings field metadata = %#v", modelMappings)
	}
	createdBy := apiKeySchema.LookUpField("CreatedByAdminID")
	if createdBy == nil || createdBy.DBName != "created_by_admin_id" {
		t.Fatalf("APIKey CreatedByAdminID metadata = %#v", createdBy)
	}

	siteStateSchema := parseStoreSchema(t, &SiteState{})
	if siteStateSchema.Table != "site_states" {
		t.Fatalf("SiteState schema table = %q, want site_states", siteStateSchema.Table)
	}
	if len(siteStateSchema.PrimaryFields) != 1 || siteStateSchema.PrimaryFields[0].DBName != "site_id" {
		t.Fatalf("SiteState primary fields = %#v, want site_id", siteStateSchema.PrimaryFields)
	}

	summarySchema := parseStoreSchema(t, &RequestUsageDailySummary{})
	if summarySchema.Table != "request_usage_daily_summaries" {
		t.Fatalf("RequestUsageDailySummary schema table = %q", summarySchema.Table)
	}
	if field := summarySchema.LookUpField("TimeZone"); field == nil || field.DBName != "timezone" {
		t.Fatalf("RequestUsageDailySummary TimeZone metadata = %#v", field)
	}
	if field := summarySchema.LookUpField("EstimatedCost"); field == nil || field.TagSettings["TYPE"] != "numeric(18,8)" {
		t.Fatalf("RequestUsageDailySummary EstimatedCost metadata = %#v", field)
	}
}

func TestRequestLogSchemaIncludesParentRequestIndex(t *testing.T) {
	t.Parallel()

	requestLogSchema := parseStoreSchema(t, &RequestLog{})
	field := requestLogSchema.LookUpField("ParentRequestID")
	if field == nil || field.DBName != "parent_request_id" {
		t.Fatalf("request log parent request field = %#v", field)
	}
	index := requestLogSchema.LookIndex("request_logs_parent_request_id_idx")
	if index == nil || len(index.Fields) != 1 {
		t.Fatalf("request log parent request index = %#v, want one field", index)
	}
	if index.Fields[0].Field == nil || index.Fields[0].Field.DBName != "parent_request_id" || index.Fields[0].Expression != "" {
		t.Fatalf("request log parent request index field = %#v", index.Fields[0])
	}
}

func parseStoreSchema(t *testing.T, model any) *schema.Schema {
	t.Helper()

	parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse schema for %T: %v", model, err)
	}
	return parsed
}

func storeTestDB(name string) *gorm.DB {
	return &gorm.DB{Config: &gorm.Config{Dialector: storeTestDialector{name: name}}}
}

type storeTestDialector struct {
	name string
}

func (d storeTestDialector) Name() string {
	return d.name
}

func (d storeTestDialector) Initialize(*gorm.DB) error {
	return nil
}

func (d storeTestDialector) Migrator(*gorm.DB) gorm.Migrator {
	return nil
}

func (d storeTestDialector) DataTypeOf(*schema.Field) string {
	return ""
}

func (d storeTestDialector) DefaultValueOf(*schema.Field) clause.Expression {
	return nil
}

func (d storeTestDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {
}

func (d storeTestDialector) QuoteTo(clause.Writer, string) {
}

func (d storeTestDialector) Explain(sql string, vars ...interface{}) string {
	return sql
}
