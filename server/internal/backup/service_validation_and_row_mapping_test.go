package backup

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/schema"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestServiceValidatesPassphraseAndImportPayloadAfterReadiness(t *testing.T) {
	t.Parallel()

	service := readyBackupService(t)

	path, filename, err := service.exportAt(context.Background(), "  ", service.now())
	if path != "" || filename != "" {
		t.Fatalf("exportAt path=%q filename=%q, want empty result on passphrase validation", path, filename)
	}
	assertBackupErrorContains(t, "exportAt blank passphrase", err, "backup passphrase is required")

	summary, err := service.Import(context.Background(), "  ", []byte("encrypted"))
	if summary != (ImportSummary{}) {
		t.Fatalf("Import blank passphrase summary=%#v, want zero", summary)
	}
	assertBackupErrorContains(t, "Import blank passphrase", err, "backup passphrase is required")

	summary, err = service.Import(context.Background(), "passphrase", nil)
	if summary != (ImportSummary{}) {
		t.Fatalf("Import empty file summary=%#v, want zero", summary)
	}
	assertBackupErrorContains(t, "Import empty file", err, "backup file is required")
}

func TestServiceReadinessRequiresConfigPersistenceAndMasterKey(t *testing.T) {
	t.Parallel()

	assertBackupErrorContains(t, "ready missing config", (Service{db: backupEmptyStore(t), masterKey: "master-key"}).ready(), "config persistence is not available")
	assertBackupErrorContains(t, "ready missing master key", (Service{db: backupEmptyStore(t), confFile: &config.ConfigFile{}, masterKey: "  "}).ready(), "master key is not available")
}

func TestApplyRowToModelIgnoresUnknownColumnsAndReportsFieldErrors(t *testing.T) {
	t.Parallel()

	parsed, err := schema.Parse(&store.Site{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse site schema: %v", err)
	}

	var site store.Site
	if err := applyRowToModel(context.Background(), parsed, reflect.ValueOf(&site).Elem(), map[string]any{
		"name":        "primary",
		"unknown_col": "ignored",
	}); err != nil {
		t.Fatalf("apply row with unknown column: %v", err)
	}
	if site.Name != "primary" {
		t.Fatalf("site name = %q, want primary", site.Name)
	}

	if err := applyRowToModel(context.Background(), parsed, reflect.ValueOf(&site).Elem(), map[string]any{
		"id": map[string]any{"not": "a uuid"},
	}); err != nil {
		assertBackupErrorContains(t, "apply row set", err, "set id")
	} else {
		t.Fatal("apply row set error = nil, want id field error")
	}
}

func TestSecretColumnTransformsHandleBlankInvalidAndUnregisteredRows(t *testing.T) {
	t.Parallel()

	unknownRows := []map[string]any{{"encrypted_secret": "still-present"}}
	if err := decryptTableSecrets("not_registered", unknownRows, "master-key"); err != nil {
		t.Fatalf("decrypt unknown table: %v", err)
	}
	if unknownRows[0]["encrypted_secret"] != "still-present" {
		t.Fatalf("unknown table row was modified: %#v", unknownRows[0])
	}

	emptyRows := []map[string]any{{"encrypted_secret": "  "}}
	if err := decryptTableSecrets("api_keys", emptyRows, "master-key"); err != nil {
		t.Fatalf("decrypt empty secret: %v", err)
	}
	if _, ok := emptyRows[0]["encrypted_secret"]; ok || emptyRows[0]["secret"] != "" {
		t.Fatalf("empty encrypted secret was not converted to blank plain secret: %#v", emptyRows[0])
	}

	assertBackupErrorContains(t, "decrypt invalid secret", decryptTableSecrets("api_keys", []map[string]any{{"encrypted_secret": "not-ciphertext"}}, "master-key"), "decrypt api_keys.encrypted_secret")

	oauthRows := []map[string]any{{
		"access_token":  "",
		"refresh_token": "  ",
		"id_token":      nil,
	}}
	if err := encryptTableSecrets("oauth_connections", oauthRows, "master-key"); err != nil {
		t.Fatalf("encrypt blank oauth secrets: %v", err)
	}
	for _, column := range []string{"encrypted_access_token", "encrypted_refresh_token", "encrypted_id_token"} {
		if oauthRows[0][column] != "" {
			t.Fatalf("%s = %#v, want blank encrypted value in %#v", column, oauthRows[0][column], oauthRows[0])
		}
	}
	if _, ok := oauthRows[0]["access_token"]; ok {
		t.Fatalf("plain access token should be removed: %#v", oauthRows[0])
	}
}

func readyBackupService(t *testing.T) Service {
	t.Helper()

	return Service{
		db:        backupEmptyStore(t),
		confFile:  &config.ConfigFile{},
		masterKey: "master-key",
		now:       func() time.Time { return time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC) },
	}
}
