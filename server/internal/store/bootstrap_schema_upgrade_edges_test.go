package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"xlyra/server/internal/config"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

func TestEnsureDatabaseInitializedRequiresDatabaseName(t *testing.T) {
	t.Parallel()

	err := EnsureDatabaseInitialized(context.Background(), config.Config{DBName: " \t "})
	if err == nil || !strings.Contains(err.Error(), "target database name is required") {
		t.Fatalf("EnsureDatabaseInitialized blank database error = %v, want target database name requirement", err)
	}

	err = ensureDatabaseInitializedOnce(context.Background(), config.Config{DBName: "\n"})
	if err == nil || !strings.Contains(err.Error(), "target database name is required") {
		t.Fatalf("ensureDatabaseInitializedOnce blank database error = %v, want target database name requirement", err)
	}
}

func TestBootstrapModelsCoverRequiredTables(t *testing.T) {
	t.Parallel()

	models := bootstrapModelsByTable()
	for _, table := range requiredBootstrapTables {
		if _, ok := models[table]; !ok {
			t.Fatalf("required bootstrap table %q has no registered model", table)
		}
	}
	if len(bootstrapModels()) < len(requiredBootstrapTables) {
		t.Fatalf("bootstrapModels registered %d models for %d required tables", len(bootstrapModels()), len(requiredBootstrapTables))
	}
}

func TestPublicTableCountSkipsGooseTableAndWrapsErrors(t *testing.T) {
	t.Parallel()

	db := bootstrapSchemaGormWithMigrator(t, &bootstrapSchemaMigrator{
		tables: []string{"goose_db_version", "admins", "sites"},
	})
	count, err := publicTableCount(db)
	if err != nil || count != 2 {
		t.Fatalf("publicTableCount = %d, %v; want two non-goose tables", count, err)
	}

	db = bootstrapSchemaGormWithMigrator(t, &bootstrapSchemaMigrator{
		getTablesErr: errors.New("catalog unavailable"),
	})
	count, err = publicTableCount(db)
	if err == nil || count != 0 || !strings.Contains(err.Error(), "count public tables") {
		t.Fatalf("publicTableCount error = %d, %v; want wrapped count error", count, err)
	}
}

func TestEnsureSchemaIndexCreatesOnlyMissingIndexesAndWrapsErrors(t *testing.T) {
	t.Parallel()

	existing := &bootstrapSchemaMigrator{hasIndex: true}
	if err := ensureSchemaIndex(existing, &RequestUsageDailySummary{}, "summary_idx"); err != nil {
		t.Fatalf("ensureSchemaIndex existing index returned error: %v", err)
	}
	if existing.createIndexCalls != 0 {
		t.Fatalf("existing index create calls = %d, want 0", existing.createIndexCalls)
	}

	missing := &bootstrapSchemaMigrator{}
	if err := ensureSchemaIndex(missing, &RequestUsageDailySummary{}, "summary_idx"); err != nil {
		t.Fatalf("ensureSchemaIndex missing index returned error: %v", err)
	}
	if missing.createIndexCalls != 1 || missing.lastIndexName != "summary_idx" {
		t.Fatalf("create index calls = %d name = %q, want one summary_idx call", missing.createIndexCalls, missing.lastIndexName)
	}

	failing := &bootstrapSchemaMigrator{createIndexErr: errors.New("permission denied")}
	err := ensureSchemaIndex(failing, &RequestUsageDailySummary{}, "summary_idx")
	if err == nil || !strings.Contains(err.Error(), "ensure summary_idx index") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("ensureSchemaIndex error = %v, want wrapped create error", err)
	}
}

func TestEnsureAdminSessionExpiresAtNullableHandlesColumnStatesAndErrors(t *testing.T) {
	t.Parallel()

	nullable := &bootstrapSchemaMigrator{
		columnTypes: []gorm.ColumnType{
			bootstrapSchemaColumnType{name: "expires_at", nullable: true, nullableOK: true},
		},
	}
	if err := ensureAdminSessionExpiresAtNullable(nullable); err != nil {
		t.Fatalf("nullable expires_at returned error: %v", err)
	}
	if nullable.alterColumnCalls != 0 {
		t.Fatalf("nullable expires_at alter calls = %d, want 0", nullable.alterColumnCalls)
	}

	notNullable := &bootstrapSchemaMigrator{
		columnTypes: []gorm.ColumnType{
			bootstrapSchemaColumnType{name: "id"},
			bootstrapSchemaColumnType{name: "EXPIRES_AT", nullable: false, nullableOK: true},
		},
	}
	if err := ensureAdminSessionExpiresAtNullable(notNullable); err != nil {
		t.Fatalf("not-null expires_at returned error: %v", err)
	}
	if notNullable.alterColumnCalls != 1 || notNullable.lastAlterField != "ExpiresAt" {
		t.Fatalf("not-null expires_at alter calls = %d field = %q, want one ExpiresAt alter", notNullable.alterColumnCalls, notNullable.lastAlterField)
	}

	unknownNullable := &bootstrapSchemaMigrator{
		columnTypes: []gorm.ColumnType{
			bootstrapSchemaColumnType{name: "expires_at", nullableOK: false},
		},
	}
	if err := ensureAdminSessionExpiresAtNullable(unknownNullable); err != nil {
		t.Fatalf("unknown-nullability expires_at returned error: %v", err)
	}
	if unknownNullable.alterColumnCalls != 1 {
		t.Fatalf("unknown-nullability alter calls = %d, want 1", unknownNullable.alterColumnCalls)
	}

	missingColumn := &bootstrapSchemaMigrator{
		columnTypes: []gorm.ColumnType{bootstrapSchemaColumnType{name: "id"}},
	}
	if err := ensureAdminSessionExpiresAtNullable(missingColumn); err != nil {
		t.Fatalf("missing expires_at returned error: %v", err)
	}
	if missingColumn.alterColumnCalls != 0 {
		t.Fatalf("missing expires_at alter calls = %d, want 0", missingColumn.alterColumnCalls)
	}

	inspectFail := &bootstrapSchemaMigrator{columnTypesErr: errors.New("metadata unavailable")}
	err := ensureAdminSessionExpiresAtNullable(inspectFail)
	if err == nil || !strings.Contains(err.Error(), "inspect admin_sessions.expires_at column") {
		t.Fatalf("column inspection error = %v, want wrapped inspection error", err)
	}

	alterFail := &bootstrapSchemaMigrator{
		columnTypes:    []gorm.ColumnType{bootstrapSchemaColumnType{name: "expires_at", nullable: false, nullableOK: true}},
		alterColumnErr: errors.New("alter denied"),
	}
	err = ensureAdminSessionExpiresAtNullable(alterFail)
	if err == nil || !strings.Contains(err.Error(), "ensure admin_sessions.expires_at nullable") {
		t.Fatalf("alter error = %v, want wrapped alter error", err)
	}
}

func TestOAuthConnectionEmailIdentityDuplicateIndexErrorRejectsNilError(t *testing.T) {
	t.Parallel()

	if oauthConnectionEmailIdentityDuplicateIndexError(nil) {
		t.Fatal("nil duplicate index error should be false")
	}
}

func bootstrapSchemaGormWithMigrator(t *testing.T, migrator gorm.Migrator) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(bootstrapSchemaDialector{migrator: migrator}, &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("open bootstrap schema gorm db: %v", err)
	}
	return db
}

type bootstrapSchemaDialector struct {
	migrator gorm.Migrator
}

func (d bootstrapSchemaDialector) Name() string {
	return "bootstrap_schema"
}

func (d bootstrapSchemaDialector) Initialize(*gorm.DB) error {
	return nil
}

func (d bootstrapSchemaDialector) Migrator(*gorm.DB) gorm.Migrator {
	return d.migrator
}

func (d bootstrapSchemaDialector) DataTypeOf(*schema.Field) string {
	return ""
}

func (d bootstrapSchemaDialector) DefaultValueOf(*schema.Field) clause.Expression {
	return nil
}

func (d bootstrapSchemaDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {
}

func (d bootstrapSchemaDialector) QuoteTo(clause.Writer, string) {
}

func (d bootstrapSchemaDialector) Explain(statement string, vars ...interface{}) string {
	return statement
}

type bootstrapSchemaMigrator struct {
	tables           []string
	getTablesErr     error
	hasIndex         bool
	createIndexErr   error
	createIndexCalls int
	lastIndexName    string
	columnTypes      []gorm.ColumnType
	columnTypesErr   error
	alterColumnErr   error
	alterColumnCalls int
	lastAlterField   string
}

func (m *bootstrapSchemaMigrator) AutoMigrate(...interface{}) error {
	return nil
}

func (m *bootstrapSchemaMigrator) CurrentDatabase() string {
	return ""
}

func (m *bootstrapSchemaMigrator) FullDataTypeOf(*schema.Field) clause.Expr {
	return clause.Expr{}
}

func (m *bootstrapSchemaMigrator) GetTypeAliases(string) []string {
	return nil
}

func (m *bootstrapSchemaMigrator) CreateTable(...interface{}) error {
	return nil
}

func (m *bootstrapSchemaMigrator) DropTable(...interface{}) error {
	return nil
}

func (m *bootstrapSchemaMigrator) HasTable(interface{}) bool {
	return false
}

func (m *bootstrapSchemaMigrator) RenameTable(interface{}, interface{}) error {
	return nil
}

func (m *bootstrapSchemaMigrator) GetTables() ([]string, error) {
	return append([]string(nil), m.tables...), m.getTablesErr
}

func (m *bootstrapSchemaMigrator) TableType(interface{}) (gorm.TableType, error) {
	return nil, nil
}

func (m *bootstrapSchemaMigrator) AddColumn(interface{}, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) DropColumn(interface{}, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) AlterColumn(_ interface{}, field string) error {
	m.alterColumnCalls++
	m.lastAlterField = field
	return m.alterColumnErr
}

func (m *bootstrapSchemaMigrator) MigrateColumn(interface{}, *schema.Field, gorm.ColumnType) error {
	return nil
}

func (m *bootstrapSchemaMigrator) MigrateColumnUnique(interface{}, *schema.Field, gorm.ColumnType) error {
	return nil
}

func (m *bootstrapSchemaMigrator) HasColumn(interface{}, string) bool {
	return false
}

func (m *bootstrapSchemaMigrator) RenameColumn(interface{}, string, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) ColumnTypes(interface{}) ([]gorm.ColumnType, error) {
	return append([]gorm.ColumnType(nil), m.columnTypes...), m.columnTypesErr
}

func (m *bootstrapSchemaMigrator) CreateView(string, gorm.ViewOption) error {
	return nil
}

func (m *bootstrapSchemaMigrator) DropView(string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) CreateConstraint(interface{}, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) DropConstraint(interface{}, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) HasConstraint(interface{}, string) bool {
	return false
}

func (m *bootstrapSchemaMigrator) CreateIndex(_ interface{}, name string) error {
	m.createIndexCalls++
	m.lastIndexName = name
	return m.createIndexErr
}

func (m *bootstrapSchemaMigrator) DropIndex(interface{}, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) HasIndex(interface{}, string) bool {
	return m.hasIndex
}

func (m *bootstrapSchemaMigrator) RenameIndex(interface{}, string, string) error {
	return nil
}

func (m *bootstrapSchemaMigrator) GetIndexes(interface{}) ([]gorm.Index, error) {
	return nil, nil
}

type bootstrapSchemaColumnType struct {
	name       string
	nullable   bool
	nullableOK bool
}

func (c bootstrapSchemaColumnType) Name() string {
	return c.name
}

func (c bootstrapSchemaColumnType) DatabaseTypeName() string {
	return ""
}

func (c bootstrapSchemaColumnType) ColumnType() (string, bool) {
	return "", false
}

func (c bootstrapSchemaColumnType) PrimaryKey() (bool, bool) {
	return false, false
}

func (c bootstrapSchemaColumnType) AutoIncrement() (bool, bool) {
	return false, false
}

func (c bootstrapSchemaColumnType) Length() (int64, bool) {
	return 0, false
}

func (c bootstrapSchemaColumnType) DecimalSize() (int64, int64, bool) {
	return 0, 0, false
}

func (c bootstrapSchemaColumnType) Nullable() (bool, bool) {
	return c.nullable, c.nullableOK
}

func (c bootstrapSchemaColumnType) Unique() (bool, bool) {
	return false, false
}

func (c bootstrapSchemaColumnType) ScanType() reflect.Type {
	return reflect.TypeOf(sql.NullString{})
}

func (c bootstrapSchemaColumnType) Comment() (string, bool) {
	return "", false
}

func (c bootstrapSchemaColumnType) DefaultValue() (string, bool) {
	return "", false
}
