package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/schema"

	"xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

func TestEncryptStreamHidesArchiveAndRejectsWrongPassphrase(t *testing.T) {
	plain := []byte(`{"secret":"plain-value"}`)
	var encrypted bytes.Buffer
	if err := encryptStream("correct horse battery staple", bytes.NewReader(plain), &encrypted); err != nil {
		t.Fatalf("encrypt stream: %v", err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("plain-value")) {
		t.Fatal("encrypted backup envelope leaked plaintext")
	}
	if err := decryptStream("wrong passphrase", bytes.NewReader(encrypted.Bytes()), &bytes.Buffer{}); err == nil {
		t.Fatal("expected wrong passphrase to fail")
	}
	var decrypted bytes.Buffer
	if err := decryptStream("correct horse battery staple", bytes.NewReader(encrypted.Bytes()), &decrypted); err != nil {
		t.Fatalf("decrypt stream: %v", err)
	}
	if !bytes.Equal(decrypted.Bytes(), plain) {
		t.Fatalf("unexpected decrypted payload: %s", decrypted.String())
	}
}

func TestArchiveRoundTripUsesZipJSONLPayload(t *testing.T) {
	payload := archivePayload{
		Manifest: manifest{
			FormatVersion: currentFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			CreatedAt:     time.Now().UTC(),
			Tables:        exportTables,
		},
		Config: map[string]any{"general": map[string]any{"log": map[string]any{"level": "debug"}}},
	}

	file, err := os.CreateTemp(t.TempDir(), "backup-*.zip")
	if err != nil {
		t.Fatalf("create archive file: %v", err)
	}
	filename := file.Name()
	err = writeArchive(payload, writeTestDatabaseArchive, file)
	closeErr := file.Close()
	if err != nil {
		t.Fatalf("write archive: %v", err)
	}
	if closeErr != nil {
		t.Fatalf("close archive: %v", closeErr)
	}

	decoded, dump, err := parseArchive(filename)
	if err != nil {
		t.Fatalf("parse archive: %v", err)
	}
	if decoded.Manifest.Payload != backupPayload {
		t.Fatalf("expected %s payload, got %q", backupPayload, decoded.Manifest.Payload)
	}
	if decoded.Config["general"] == nil {
		t.Fatal("config payload was not preserved")
	}
	if len(dump.Tables["sites"]) != 1 {
		t.Fatal("database payload was not preserved")
	}
	if dump.TotalRows != 1 {
		t.Fatalf("total rows = %d, want 1", dump.TotalRows)
	}
}

func TestApplyRowToModelRestoresTypedFields(t *testing.T) {
	ctx := context.Background()
	siteID := "00000000-0000-0000-0000-000000000001"
	createdAt := "2026-06-21T03:00:00Z"
	parsed, err := schema.Parse(&store.Site{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse site schema: %v", err)
	}
	var site store.Site
	row := map[string]any{
		"id":               siteID,
		"name":             "OpenAI",
		"enabled":          true,
		"routing_priority": json.Number("1.5"),
		"meta":             map[string]any{"tier": "prod"},
		"created_at":       createdAt,
	}
	if err := applyRowToModel(ctx, parsed, reflect.ValueOf(&site).Elem(), row); err != nil {
		t.Fatalf("apply site row: %v", err)
	}
	if site.ID.String() != siteID {
		t.Fatalf("expected site id %s, got %s", siteID, site.ID)
	}
	if string(site.Meta) != `{"tier":"prod"}` {
		t.Fatalf("expected JSON meta, got %s", string(site.Meta))
	}
	if site.RoutingPriority != 1.5 {
		t.Fatalf("expected routing priority 1.5, got %f", site.RoutingPriority)
	}
	if site.CreatedAt.IsZero() {
		t.Fatal("expected created_at to be restored")
	}

	parsed, err = schema.Parse(&store.RequestLog{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse request log schema: %v", err)
	}
	var log store.RequestLog
	if err := applyRowToModel(ctx, parsed, reflect.ValueOf(&log).Elem(), map[string]any{
		"id":         "00000000-0000-0000-0000-000000000002",
		"site_id":    siteID,
		"latency_ms": json.Number("120"),
	}); err != nil {
		t.Fatalf("apply request log row: %v", err)
	}
	if !log.SiteID.Valid || log.SiteID.UUID != uuid.MustParse(siteID) {
		t.Fatalf("expected nullable site id to be restored, got %#v", log.SiteID)
	}
	if !log.LatencyMS.Valid || log.LatencyMS.Int64 != 120 {
		t.Fatalf("expected nullable latency to be restored, got %#v", log.LatencyMS)
	}

	parsed, err = schema.Parse(&store.SiteState{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse site state schema: %v", err)
	}
	var state store.SiteState
	if err := applyRowToModel(ctx, parsed, reflect.ValueOf(&state).Elem(), map[string]any{
		"site_id":              siteID,
		"last_sync_started_at": createdAt,
	}); err != nil {
		t.Fatalf("apply site state row: %v", err)
	}
	if !state.LastSyncStartedAt.Valid || state.LastSyncStartedAt.Time.IsZero() {
		t.Fatalf("expected nullable sync start to be restored, got %#v", state.LastSyncStartedAt)
	}
}

func TestBackupTablesIncludeRequestUsageSummaries(t *testing.T) {
	dailySummary := "request_usage_daily_summaries"
	summaryDays := "request_usage_summary_days"

	if !stringSliceContains(exportTables, dailySummary) || !stringSliceContains(exportTables, summaryDays) {
		t.Fatalf("expected summary tables in export list: %#v", exportTables)
	}
	if _, ok := backupTableByName(dailySummary); !ok {
		t.Fatalf("expected backup table %s to be registered", dailySummary)
	}
	if _, ok := backupTableByName(summaryDays); !ok {
		t.Fatalf("expected backup table %s to be registered", summaryDays)
	}
	if len(importDeleteOrder) < 2 || importDeleteOrder[0] != summaryDays || importDeleteOrder[1] != dailySummary {
		t.Fatalf("expected summary tables to be cleared first, got %#v", importDeleteOrder[:min(len(importDeleteOrder), 4)])
	}
}

func writeTestDatabaseArchive(zw *zip.Writer) (map[string]int, error) {
	rowCounts := make(map[string]int, len(backupTables))
	for _, table := range backupTables {
		w, err := zw.Create("database/" + table.Name + ".jsonl")
		if err != nil {
			return nil, err
		}
		if table.Name != "sites" {
			rowCounts[table.Name] = 0
			continue
		}
		encoder := json.NewEncoder(w)
		if err := encoder.Encode(map[string]any{"id": "site-1", "name": "Site"}); err != nil {
			return nil, err
		}
		rowCounts[table.Name] = 1
	}
	return rowCounts, nil
}

func TestNormalizeImportDumpDropsUsageRecordsWithoutRequestLogs(t *testing.T) {
	validRequestLogID := "00000000-0000-0000-0000-000000000001"
	missingRequestLogID := "00000000-0000-0000-0000-000000000002"
	apiKeyID := "00000000-0000-0000-0000-000000000003"
	dump := databaseDump{Tables: map[string][]map[string]any{
		"request_logs": {{
			"id":                 validRequestLogID,
			"api_key_id":         "00000000-0000-0000-0000-000000000004",
			"site_id":            nil,
			"canonical_model_id": nil,
			"site_model_id":      nil,
		}},
		"usage_records": {{
			"id":                 "usage-1",
			"request_log_id":     validRequestLogID,
			"api_key_id":         apiKeyID,
			"site_id":            "00000000-0000-0000-0000-000000000005",
			"canonical_model_id": nil,
		}, {
			"id":             "usage-2",
			"request_log_id": missingRequestLogID,
		}},
		"api_keys":         {{"id": apiKeyID}},
		"sites":            {},
		"canonical_models": {},
		"site_models":      {},
	}}

	normalized := normalizeImportDump(dump)

	if got := len(normalized.Tables["usage_records"]); got != 1 {
		t.Fatalf("expected one valid usage record, got %d", got)
	}
	usage := normalized.Tables["usage_records"][0]
	if usage["request_log_id"] != validRequestLogID {
		t.Fatalf("unexpected usage request_log_id: %#v", usage["request_log_id"])
	}
	if usage["api_key_id"] != apiKeyID {
		t.Fatalf("expected valid api_key_id to be preserved, got %#v", usage["api_key_id"])
	}
	if usage["site_id"] != nil {
		t.Fatalf("expected missing site reference to be cleared, got %#v", usage["site_id"])
	}
	if normalized.Tables["request_logs"][0]["api_key_id"] != nil {
		t.Fatalf("expected missing request log api key reference to be cleared, got %#v", normalized.Tables["request_logs"][0]["api_key_id"])
	}
}

func TestSecretColumnsDecryptAndReencryptWithNewMasterKey(t *testing.T) {
	oldMasterKey := "old-master-key"
	newMasterKey := "new-master-key"
	encrypted, err := crypto.EncryptSecret(oldMasterKey, "sk-sensitive")
	if err != nil {
		t.Fatalf("encrypt old secret: %v", err)
	}
	rows := []map[string]any{{
		"id":               "key-1",
		"encrypted_secret": encrypted,
		"masked_key":       "old-mask",
	}}

	if err := decryptTableSecrets("api_keys", rows, oldMasterKey); err != nil {
		t.Fatalf("decrypt table secrets: %v", err)
	}
	if rows[0]["secret"] != "sk-sensitive" {
		t.Fatalf("expected plaintext secret, got %#v", rows[0]["secret"])
	}
	if _, ok := rows[0]["encrypted_secret"]; ok {
		t.Fatal("encrypted secret should not be present in exported row")
	}

	if err := encryptTableSecrets("api_keys", rows, newMasterKey); err != nil {
		t.Fatalf("encrypt table secrets: %v", err)
	}
	nextEncrypted, _ := rows[0]["encrypted_secret"].(string)
	if nextEncrypted == "" || nextEncrypted == encrypted {
		t.Fatal("secret was not re-encrypted with a fresh payload")
	}
	plain, err := crypto.DecryptSecret(newMasterKey, nextEncrypted)
	if err != nil {
		t.Fatalf("decrypt new secret: %v", err)
	}
	if plain != "sk-sensitive" {
		t.Fatalf("unexpected re-encrypted plaintext: %q", plain)
	}
	if rows[0]["masked_key"] != "sk-s...tive" {
		t.Fatalf("masked key was not refreshed: %#v", rows[0]["masked_key"])
	}
}

func TestBackupValueKeepsJSONAsObject(t *testing.T) {
	value := backupValue(store.JSON(`{"model":"gpt","tokens":123}`))
	encoded, err := json.Marshal(map[string]any{"metadata": value})
	if err != nil {
		t.Fatalf("marshal backup value: %v", err)
	}
	if string(encoded) != `{"metadata":{"model":"gpt","tokens":123}}` {
		t.Fatalf("expected metadata to encode as JSON object, got %s", encoded)
	}
}

func stringSliceContains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
