package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

type databaseDump struct {
	Tables       map[string][]map[string]any `json:"tables"`
	TotalRows    int
	beforeCommit func() error
	// archivePath, when set, points at the on-disk archive so the large detail
	// tables (streamedImportTables) can be streamed from it during import instead
	// of being held in Tables. Empty means everything lives in Tables (tests).
	archivePath string
}

const (
	exportBatchSize       = 1000
	importBatchSize       = 2000
	importStreamChunkSize = 20000
	importParameterBudget = 60000
)

// streamedImportTables are the large detail tables imported by streaming from
// the archive in chunks rather than loading every row into memory at once.
var streamedImportTables = map[string]struct{}{
	"request_logs":  {},
	"usage_records": {},
}

var exportTables = []string{
	"sites",
	"site_groups",
	"site_group_sites",
	"site_states",
	"oauth_connections",
	"site_credentials",
	"site_api_key_states",
	"canonical_models",
	"api_keys",
	"api_key_site_permissions",
	"api_key_site_group_permissions",
	"site_models",
	"api_key_site_model_permissions",
	"canonical_model_aliases",
	"site_api_key_models",
	"site_pricing_groups",
	"site_model_pricings",
	"gateway_rate_limits",
	"request_logs",
	"usage_records",
	"request_usage_daily_summaries",
	"request_usage_summary_days",
	"playground_conversations",
	"playground_runs",
	"playground_turn_indexes",
	"playground_assets",
}

type backupTable struct {
	Name     string
	Model    any
	NewSlice func() any
}

var backupTables = []backupTable{
	{Name: "sites", Model: &store.Site{}, NewSlice: func() any { return &[]store.Site{} }},
	{Name: "site_groups", Model: &store.SiteGroup{}, NewSlice: func() any { return &[]store.SiteGroup{} }},
	{Name: "site_group_sites", Model: &store.SiteGroupSite{}, NewSlice: func() any { return &[]store.SiteGroupSite{} }},
	{Name: "site_states", Model: &store.SiteState{}, NewSlice: func() any { return &[]store.SiteState{} }},
	{Name: "oauth_connections", Model: &store.OAuthConnection{}, NewSlice: func() any { return &[]store.OAuthConnection{} }},
	{Name: "site_credentials", Model: &store.SiteCredential{}, NewSlice: func() any { return &[]store.SiteCredential{} }},
	{Name: "site_api_key_states", Model: &store.SiteAPIKeyState{}, NewSlice: func() any { return &[]store.SiteAPIKeyState{} }},
	{Name: "canonical_models", Model: &store.CanonicalModel{}, NewSlice: func() any { return &[]store.CanonicalModel{} }},
	{Name: "api_keys", Model: &store.APIKey{}, NewSlice: func() any { return &[]store.APIKey{} }},
	{Name: "api_key_site_permissions", Model: &store.APIKeySitePermission{}, NewSlice: func() any { return &[]store.APIKeySitePermission{} }},
	{Name: "api_key_site_group_permissions", Model: &store.APIKeySiteGroupPermission{}, NewSlice: func() any { return &[]store.APIKeySiteGroupPermission{} }},
	{Name: "site_models", Model: &store.SiteModel{}, NewSlice: func() any { return &[]store.SiteModel{} }},
	{Name: "api_key_site_model_permissions", Model: &store.APIKeySiteModelPermission{}, NewSlice: func() any { return &[]store.APIKeySiteModelPermission{} }},
	{Name: "canonical_model_aliases", Model: &store.CanonicalModelAlias{}, NewSlice: func() any { return &[]store.CanonicalModelAlias{} }},
	{Name: "site_api_key_models", Model: &store.SiteAPIKeyModel{}, NewSlice: func() any { return &[]store.SiteAPIKeyModel{} }},
	{Name: "site_pricing_groups", Model: &store.SitePricingGroup{}, NewSlice: func() any { return &[]store.SitePricingGroup{} }},
	{Name: "site_model_pricings", Model: &store.SiteModelPricing{}, NewSlice: func() any { return &[]store.SiteModelPricing{} }},
	{Name: "gateway_rate_limits", Model: &store.GatewayRateLimit{}, NewSlice: func() any { return &[]store.GatewayRateLimit{} }},
	{Name: "request_logs", Model: &store.RequestLog{}, NewSlice: func() any { return &[]store.RequestLog{} }},
	{Name: "usage_records", Model: &store.UsageRecord{}, NewSlice: func() any { return &[]store.UsageRecord{} }},
	{Name: "request_usage_daily_summaries", Model: &store.RequestUsageDailySummary{}, NewSlice: func() any { return &[]store.RequestUsageDailySummary{} }},
	{Name: "request_usage_summary_days", Model: &store.RequestUsageSummaryDay{}, NewSlice: func() any { return &[]store.RequestUsageSummaryDay{} }},
	{Name: "playground_conversations", Model: &store.PlaygroundConversation{}, NewSlice: func() any { return &[]store.PlaygroundConversation{} }},
	{Name: "playground_runs", Model: &store.PlaygroundRun{}, NewSlice: func() any { return &[]store.PlaygroundRun{} }},
	{Name: "playground_turn_indexes", Model: &store.PlaygroundTurnIndex{}, NewSlice: func() any { return &[]store.PlaygroundTurnIndex{} }},
	{Name: "playground_assets", Model: &store.PlaygroundAsset{}, NewSlice: func() any { return &[]store.PlaygroundAsset{} }},
}

var importDeleteOrder = []string{
	"playground_assets",
	"playground_turn_indexes",
	"playground_runs",
	"playground_conversations",
	"request_usage_summary_days",
	"request_usage_daily_summaries",
	"usage_records",
	"request_logs",
	"gateway_rate_limits",
	"site_model_pricings",
	"site_pricing_groups",
	"site_api_key_models",
	"canonical_model_aliases",
	"api_key_site_model_permissions",
	"site_models",
	"api_key_site_group_permissions",
	"api_key_site_permissions",
	"api_keys",
	"canonical_models",
	"site_api_key_states",
	"site_credentials",
	"oauth_connections",
	"site_states",
	"site_group_sites",
	"site_groups",
	"sites",
}

var textSecretColumns = map[string][]string{
	"api_keys":          {"encrypted_secret"},
	"site_credentials":  {"encrypted_secret"},
	"oauth_connections": {"encrypted_access_token", "encrypted_refresh_token", "encrypted_id_token"},
}

func exportDatabase(ctx context.Context, db *gorm.DB, masterKey string, playgroundRoot string, zw *zip.Writer) (map[string]int, error) {
	var rowCounts map[string]int
	var conversations []store.PlaygroundConversation
	var assets []store.PlaygroundAsset
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		rowCounts, err = exportDatabaseSnapshot(ctx, tx, masterKey, zw)
		if err != nil {
			return err
		}
		conversations, assets, err = listPlaygroundFileRefs(ctx, tx)
		return err
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	if _, _, err := exportPlaygroundFileEntries(ctx, db, zw, playgroundRoot, conversations, assets); err != nil {
		return nil, err
	}
	return rowCounts, nil
}

func exportDatabaseSnapshot(ctx context.Context, db *gorm.DB, masterKey string, zw *zip.Writer) (map[string]int, error) {
	rowCounts := make(map[string]int, len(backupTables))
	for _, table := range backupTables {
		rows, err := exportTable(ctx, db, table, masterKey, zw)
		if err != nil {
			return nil, err
		}
		rowCounts[table.Name] = rows
	}
	return rowCounts, nil
}

func importDatabase(ctx context.Context, db *gorm.DB, masterKey string, dump databaseDump, adminID uuid.UUID, progress ...ProgressFunc) (int, int, error) {
	if err := validateDumpTables(dump); err != nil {
		return 0, 0, err
	}
	streaming := dump.archivePath != ""
	if !streaming {
		dump = normalizeImportDump(dump)
	}
	progressTotal := dump.TotalRows
	if !streaming {
		progressTotal = 0
		for _, table := range backupTables {
			progressTotal += len(dump.Tables[table.Name])
		}
	}

	var prog ProgressFunc
	if len(progress) > 0 {
		prog = progress[0]
	}
	emit := func(ev ProgressEvent) {
		if prog != nil {
			prog(ev)
		}
	}

	// Reference sets for the streamed detail tables' integrity filtering. The
	// dimension tables are small and always held in memory.
	requestLogRefs := map[string]map[string]struct{}{
		"api_key_id":         rowIDSet(dump.Tables["api_keys"]),
		"site_id":            rowIDSet(dump.Tables["sites"]),
		"canonical_model_id": rowIDSet(dump.Tables["canonical_models"]),
		"site_model_id":      rowIDSet(dump.Tables["site_models"]),
	}
	usageRecordRefs := map[string]map[string]struct{}{
		"api_key_id":         requestLogRefs["api_key_id"],
		"site_id":            requestLogRefs["site_id"],
		"canonical_model_id": requestLogRefs["canonical_model_id"],
	}
	requestLogIDs := make(map[string]struct{})

	requestLogTransform := func(rows []map[string]any) []map[string]any {
		rowsWithValidNullableReferences(rows, requestLogRefs)
		for _, row := range rows {
			if id := stringValue(row["id"]); id != "" {
				requestLogIDs[id] = struct{}{}
			}
		}
		return rows
	}
	usageRecordTransform := func(rows []map[string]any) []map[string]any {
		rows = rowsWithExistingParent(rows, "request_log_id", requestLogIDs)
		rowsWithValidNullableReferences(rows, usageRecordRefs)
		return rows
	}

	total := 0
	skippedTotal := 0
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clear every backup table before reloading. A single TRUNCATE (vs a
		// per-table DELETE) reclaims storage immediately instead of leaving dead
		// tuples that bloat each table on every restore, and is near-instant.
		// CASCADE also clears the transient non-backup tables that reference these
		// via ON DELETE CASCADE foreign keys (route_cooldowns, health_snapshots,
		// site_health_states, oauth_sessions) — matching the DELETE's prior cascade
		// behavior. TRUNCATE has no GORM API, so it is issued directly; table names
		// come from the fixed importDeleteOrder list, never user input.
		quoted := make([]string, 0, len(importDeleteOrder))
		for _, table := range importDeleteOrder {
			if _, ok := backupTableByName(table); !ok {
				return fmt.Errorf("backup table %s was not registered", table)
			}
			quoted = append(quoted, `"`+table+`"`)
		}
		if err := tx.Exec("TRUNCATE TABLE " + strings.Join(quoted, ", ") + " CASCADE").Error; err != nil {
			return fmt.Errorf("clear backup tables: %w", err)
		}
		for _, table := range backupTables {
			switch table.Name {
			case "request_logs", "usage_records":
				transform := requestLogTransform
				if table.Name == "usage_records" {
					transform = usageRecordTransform
				}
				baseSkipped := skippedTotal
				n, skipped, err := importStreamedTable(ctx, tx, dump, table, transform, func(rows int, skipped int) {
					emit(ProgressEvent{Step: "import", Status: "in_progress", Rows: total + rows, TotalRows: progressTotal - baseSkipped - skipped, Table: table.Name})
				})
				if err != nil {
					return err
				}
				total += n
				skippedTotal += skipped
			default:
				rows := cloneRows(dump.Tables[table.Name])
				if err := encryptTableSecrets(table.Name, rows, masterKey); err != nil {
					return err
				}
				if table.Name == "api_keys" {
					for _, row := range rows {
						row["created_by_admin_id"] = nil
					}
				}
				if table.Name == "playground_conversations" && adminID != uuid.Nil {
					reownPlaygroundConversations(rows, adminID)
				}
				if err := importTable(ctx, tx, table, rows, func(imported int) {
					emit(ProgressEvent{Step: "import", Status: "in_progress", Rows: total + imported, TotalRows: progressTotal - skippedTotal, Table: table.Name})
				}); err != nil {
					return err
				}
				if table.Name == "api_keys" {
					if err := restoreAPIKeyPeriodicQuotaFlags(ctx, tx, rows); err != nil {
						return err
					}
				}
				total += len(rows)
			}
		}
		if dump.beforeCommit != nil {
			if err := dump.beforeCommit(); err != nil {
				return err
			}
		}
		return nil
	})
	return total, progressTotal - skippedTotal, err
}

// importStreamedTable imports a large detail table. With an archive path it
// decodes the table's rows in bounded chunks straight from the archive (peak
// memory ~one chunk plus the reference sets) instead of materializing every row;
// without one it falls back to the in-memory dump (already normalized). The
// transform applies referential-integrity filtering per chunk.
func importStreamedTable(ctx context.Context, tx *gorm.DB, dump databaseDump, table backupTable, transform func([]map[string]any) []map[string]any, progress func(int, int)) (int, int, error) {
	if dump.archivePath == "" {
		rows := dump.Tables[table.Name]
		if len(rows) == 0 {
			return 0, 0, nil
		}
		if err := importTable(ctx, tx, table, rows, func(imported int) {
			if progress != nil {
				progress(imported, 0)
			}
		}); err != nil {
			return 0, 0, err
		}
		return len(rows), 0, nil
	}

	total := 0
	skipped := 0
	err := forEachArchiveTableChunk(dump.archivePath, table.Name, importStreamChunkSize, func(chunk []map[string]any) error {
		rows := chunk
		if transform != nil {
			rows = transform(rows)
		}
		skipped += len(chunk) - len(rows)
		if len(rows) == 0 {
			if progress != nil {
				progress(total, skipped)
			}
			return nil
		}
		if err := importTable(ctx, tx, table, rows, func(imported int) {
			if progress != nil {
				progress(total+imported, skipped)
			}
		}); err != nil {
			return err
		}
		total += len(rows)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return total, skipped, nil
}

// forEachArchiveTableChunk decodes a table's JSONL entry from the archive and
// invokes fn with successive chunks of up to chunkSize rows, bounding peak
// memory to a single chunk. The chunk slice is reused between calls, so fn must
// not retain it beyond the call.
func forEachArchiveTableChunk(archivePath string, tableName string, chunkSize int, fn func([]map[string]any) error) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open backup archive: %w", err)
	}
	defer zr.Close()
	name := path.Join("database", tableName+".jsonl")
	file := archiveFile(zr.File, name)
	if file == nil {
		return nil
	}
	reader, err := file.Open()
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer reader.Close()

	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	chunk := make([]map[string]any, 0, chunkSize)
	for {
		var row map[string]any
		err := decoder.Decode(&row)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("decode %s: %w", name, err)
		}
		chunk = append(chunk, row)
		if len(chunk) >= chunkSize {
			if err := fn(chunk); err != nil {
				return err
			}
			chunk = chunk[:0]
		}
	}
	if len(chunk) > 0 {
		return fn(chunk)
	}
	return nil
}

func exportTable(ctx context.Context, db *gorm.DB, table backupTable, masterKey string, zw *zip.Writer) (int, error) {
	w, err := createArchiveEntry(zw, path.Join("database", table.Name+".jsonl"), zip.Deflate)
	if err != nil {
		return 0, err
	}
	encoder := json.NewEncoder(w)
	rows, err := exportTableRows(ctx, db, table, masterKey, encoder)
	if err != nil {
		return 0, fmt.Errorf("export %s: %w", table.Name, err)
	}
	return rows, nil
}

func exportTableRows(ctx context.Context, db *gorm.DB, table backupTable, masterKey string, encoder *json.Encoder) (int, error) {
	slice := table.NewSlice()
	query := db.WithContext(ctx).Model(table.Model)
	parsed, err := schema.Parse(table.Model, &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return 0, err
	}
	if order, err := primaryOrderClause(db, table.Model); err != nil {
		return 0, err
	} else if len(order.Columns) > 0 {
		query = query.Clauses(order)
	}
	total := 0
	err = query.FindInBatches(slice, exportBatchSize, func(_ *gorm.DB, _ int) error {
		rows, err := modelRows(ctx, parsed, slice)
		if err != nil {
			return err
		}
		if err := decryptTableSecrets(table.Name, rows, masterKey); err != nil {
			return err
		}
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return err
			}
		}
		total += len(rows)
		return nil
	}).Error
	return total, err
}

func importTable(ctx context.Context, db *gorm.DB, table backupTable, rows []map[string]any, progress ...func(int)) error {
	if len(rows) == 0 {
		return nil
	}
	parsed, err := schema.Parse(table.Model, &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return err
	}
	items := table.NewSlice()
	slice := reflect.ValueOf(items)
	if slice.Kind() != reflect.Pointer || slice.IsNil() || slice.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("backup table %s must provide a slice pointer", table.Name)
	}
	sliceValue := slice.Elem()
	itemType := sliceValue.Type().Elem()
	for _, row := range rows {
		item := reflect.New(itemType).Elem()
		if err := applyRowToModel(ctx, parsed, item, row); err != nil {
			return fmt.Errorf("prepare %s row: %w", table.Name, err)
		}
		if item.IsZero() {
			continue
		}
		sliceValue.Set(reflect.Append(sliceValue, item))
	}
	if sliceValue.Len() == 0 {
		return nil
	}
	var report func(int)
	if len(progress) > 0 {
		report = progress[0]
	}
	batchSize := importBatchSizeForSchema(parsed)
	for start := 0; start < sliceValue.Len(); start += batchSize {
		end := min(start+batchSize, sliceValue.Len())
		if err := db.WithContext(ctx).Create(sliceValue.Slice(start, end).Interface()).Error; err != nil {
			return fmt.Errorf("insert %s: %w", table.Name, err)
		}
		if report != nil {
			report(end)
		}
	}
	return nil
}

func importBatchSizeForSchema(parsed *schema.Schema) int {
	columns := len(parsed.DBNames)
	if columns == 0 {
		return importBatchSize
	}
	return max(1, min(importBatchSize, importParameterBudget/columns))
}

func restoreAPIKeyPeriodicQuotaFlags(ctx context.Context, db *gorm.DB, rows []map[string]any) error {
	for _, column := range []string{"quota_daily_unlimited", "quota_weekly_unlimited"} {
		ids, err := apiKeyIDsWithExplicitFalse(rows, column)
		if err != nil {
			return err
		}
		for start := 0; start < len(ids); start += importBatchSize {
			end := min(start+importBatchSize, len(ids))
			values := make([]any, end-start)
			for i, id := range ids[start:end] {
				values[i] = id
			}
			result := db.WithContext(ctx).Model(&store.APIKey{}).Clauses(clause.Where{Exprs: []clause.Expression{
				clause.IN{Column: clause.Column{Name: "id"}, Values: values},
			}}).Updates(map[string]any{column: false})
			if result.Error != nil {
				return fmt.Errorf("restore api_keys.%s: %w", column, result.Error)
			}
			if result.RowsAffected != int64(len(values)) {
				return fmt.Errorf("restore api_keys.%s: updated %d rows, want %d", column, result.RowsAffected, len(values))
			}
		}
	}
	return nil
}

func apiKeyIDsWithExplicitFalse(rows []map[string]any, column string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	for _, row := range rows {
		value, ok := row[column].(bool)
		if !ok || value {
			continue
		}
		id, err := uuid.Parse(stringValue(row["id"]))
		if err != nil {
			return nil, fmt.Errorf("restore api_keys.%s: invalid api key id: %w", column, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func applyRowToModel(ctx context.Context, parsed *schema.Schema, item reflect.Value, row map[string]any) error {
	for col, value := range row {
		field, ok := parsed.FieldsByDBName[col]
		if !ok {
			continue
		}
		if err := field.Set(ctx, item, modelFieldValue(value, field)); err != nil {
			return fmt.Errorf("set %s: %w", col, err)
		}
	}
	return nil
}

func modelFieldValue(value any, field *schema.Field) any {
	if field.FieldType == reflect.TypeOf(store.JSON{}) {
		return rowValue(value, true)
	}
	if field.FieldType == reflect.TypeOf(sql.NullTime{}) || field.FieldType == reflect.TypeOf(time.Time{}) || field.FieldType == reflect.TypeOf((*time.Time)(nil)) {
		if parsed, ok := parseTimeValue(value); ok {
			return parsed
		}
	}
	return rowValue(value, false)
}

func parseTimeValue(value any) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return time.Time{}, false
		}
		return v, true
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return time.Time{}, false
		}
		if parsed, err := time.Parse(time.RFC3339Nano, trimmed); err == nil {
			return parsed, true
		}
		if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
			return parsed, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func validateDumpTables(dump databaseDump) error {
	if dump.Tables == nil {
		return fmt.Errorf("backup database payload is missing tables")
	}
	for _, table := range backupTables {
		if _, streamed := streamedImportTables[table.Name]; streamed {
			// Streamed tables are validated against the archive in parseArchive.
			continue
		}
		if _, ok := dump.Tables[table.Name]; !ok {
			dump.Tables[table.Name] = []map[string]any{}
		}
	}
	return nil
}

func normalizeImportDump(dump databaseDump) databaseDump {
	dump.Tables["request_logs"] = rowsWithValidNullableReferences(dump.Tables["request_logs"], map[string]map[string]struct{}{
		"api_key_id":         rowIDSet(dump.Tables["api_keys"]),
		"site_id":            rowIDSet(dump.Tables["sites"]),
		"canonical_model_id": rowIDSet(dump.Tables["canonical_models"]),
		"site_model_id":      rowIDSet(dump.Tables["site_models"]),
	})
	dump.Tables["usage_records"] = rowsWithExistingParent(dump.Tables["usage_records"], "request_log_id", rowIDSet(dump.Tables["request_logs"]))
	dump.Tables["usage_records"] = rowsWithValidNullableReferences(dump.Tables["usage_records"], map[string]map[string]struct{}{
		"api_key_id":         rowIDSet(dump.Tables["api_keys"]),
		"site_id":            rowIDSet(dump.Tables["sites"]),
		"canonical_model_id": rowIDSet(dump.Tables["canonical_models"]),
	})
	return dump
}

func rowsWithExistingParent(rows []map[string]any, column string, parentIDs map[string]struct{}) []map[string]any {
	if len(rows) == 0 {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		id := stringValue(row[column])
		if id == "" {
			continue
		}
		if _, ok := parentIDs[id]; !ok {
			continue
		}
		filtered = append(filtered, row)
	}
	return filtered
}

func rowsWithValidNullableReferences(rows []map[string]any, parentsByColumn map[string]map[string]struct{}) []map[string]any {
	for _, row := range rows {
		for column, parentIDs := range parentsByColumn {
			id := stringValue(row[column])
			if id == "" {
				row[column] = nil
				continue
			}
			if _, ok := parentIDs[id]; !ok {
				row[column] = nil
			}
		}
	}
	return rows
}

func rowIDSet(rows []map[string]any) map[string]struct{} {
	ids := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := stringValue(row["id"])
		if id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case uuid.UUID:
		if v == uuid.Nil {
			return ""
		}
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func decryptTableSecrets(table string, rows []map[string]any, masterKey string) error {
	for _, column := range textSecretColumns[table] {
		plainColumn := plainSecretColumn(column)
		for _, row := range rows {
			encrypted, _ := row[column].(string)
			delete(row, column)
			if strings.TrimSpace(encrypted) == "" {
				row[plainColumn] = ""
				continue
			}
			plain, err := crypto.DecryptSecret(masterKey, encrypted)
			if err != nil {
				return fmt.Errorf("decrypt %s.%s: %w", table, column, err)
			}
			row[plainColumn] = plain
		}
	}
	return nil
}

func encryptTableSecrets(table string, rows []map[string]any, masterKey string) error {
	for _, column := range textSecretColumns[table] {
		plainColumn := plainSecretColumn(column)
		for _, row := range rows {
			plain, _ := row[plainColumn].(string)
			delete(row, plainColumn)
			if strings.TrimSpace(plain) == "" {
				row[column] = ""
			} else {
				encrypted, err := crypto.EncryptSecret(masterKey, plain)
				if err != nil {
					return fmt.Errorf("encrypt %s.%s: %w", table, column, err)
				}
				row[column] = encrypted
			}
			switch table {
			case "api_keys":
				if plain != "" {
					row["masked_key"] = crypto.MaskSecret(plain)
				}
			case "site_credentials":
				if plain != "" {
					row["masked_secret"] = crypto.MaskSecret(plain)
				}
			case "oauth_connections":
				maskedColumn := strings.Replace(column, "encrypted_", "masked_", 1)
				if plain != "" {
					row[maskedColumn] = crypto.MaskSecret(plain)
				}
			}
		}
	}
	return nil
}

func plainSecretColumn(column string) string {
	return strings.TrimPrefix(column, "encrypted_")
}

func backupTableByName(name string) (backupTable, bool) {
	for _, table := range backupTables {
		if table.Name == name {
			return table, true
		}
	}
	return backupTable{}, false
}

func modelRows(ctx context.Context, parsed *schema.Schema, slice any) ([]map[string]any, error) {
	value := reflect.ValueOf(slice)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, fmt.Errorf("backup slice must be a non-nil pointer")
	}
	items := value.Elem()
	if items.Kind() != reflect.Slice {
		return nil, fmt.Errorf("backup slice must point to a slice")
	}
	rows := make([]map[string]any, 0, items.Len())
	for i := 0; i < items.Len(); i++ {
		item := items.Index(i)
		if item.Kind() == reflect.Pointer {
			item = item.Elem()
		}
		row := make(map[string]any, len(parsed.Fields))
		for _, field := range parsed.Fields {
			if field.DBName == "" {
				continue
			}
			raw, _ := field.ValueOf(ctx, item)
			row[field.DBName] = backupValue(raw)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func backupValue(value any) any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case store.JSON:
		return json.RawMessage(v)
	case []byte:
		return decodeJSONValue(v)
	case uuid.UUID:
		if v == uuid.Nil {
			return nil
		}
		return v.String()
	case uuid.NullUUID:
		if !v.Valid || v.UUID == uuid.Nil {
			return nil
		}
		return v.UUID.String()
	case time.Time:
		if v.IsZero() {
			return nil
		}
		return v.Format(time.RFC3339Nano)
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil
		}
		return v.Format(time.RFC3339Nano)
	case *uuid.UUID:
		if v == nil || *v == uuid.Nil {
			return nil
		}
		return v.String()
	case driver.Valuer:
		driverValue, err := v.Value()
		if err != nil {
			return nil
		}
		return backupValue(driverValue)
	default:
		return value
	}
}

func decodeJSONValue(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return string(raw)
	}
	return decoded
}

func primaryOrderClause(db *gorm.DB, model any) (clause.OrderBy, error) {
	parsed, err := schema.Parse(model, &sync.Map{}, db.NamingStrategy)
	if err != nil {
		return clause.OrderBy{}, err
	}
	columns := make([]clause.OrderByColumn, 0, len(parsed.PrimaryFields))
	for _, field := range parsed.PrimaryFields {
		if field.DBName == "" {
			continue
		}
		columns = append(columns, clause.OrderByColumn{Column: clause.Column{Name: field.DBName}})
	}
	return clause.OrderBy{Columns: columns}, nil
}

func rowValue(value any, asJSON bool) any {
	if asJSON {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "null"
		}
		return string(encoded)
	}
	switch v := value.(type) {
	case map[string]any, []any:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	default:
		return v
	}
}

func cloneRows(rows []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		next := make(map[string]any, len(row))
		for k, v := range row {
			next[k] = v
		}
		out = append(out, next)
	}
	return out
}

func reownPlaygroundConversations(rows []map[string]any, adminID uuid.UUID) {
	for _, row := range rows {
		row["admin_id"] = adminID.String()
	}
}
