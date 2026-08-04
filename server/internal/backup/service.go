package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

const (
	currentFormatVersion = 2
	backupAppName        = "xlyra"
)

type Service struct {
	db        *store.Store
	confFile  *config.ConfigFile
	masterKey string
	now       func() time.Time
	timeZone  config.TimeZone
}

type ImportSummary struct {
	Tables        int `json:"tables"`
	Rows          int `json:"rows"`
	ConfigKeys    int `json:"config_keys"`
	FormatVersion int `json:"format_version"`
}

func NewService(db *store.Store, confFile *config.ConfigFile, masterKey string, timeZones ...config.TimeZone) Service {
	return Service{
		db:        db,
		confFile:  confFile,
		masterKey: masterKey,
		now:       func() time.Time { return time.Now().UTC() },
		timeZone:  backupTimeZone(timeZones...),
	}
}

func (s Service) Export(ctx context.Context, passphrase string) (string, string, error) {
	return s.exportAt(ctx, passphrase, s.now())
}

func (s Service) exportAt(ctx context.Context, passphrase string, createdAt time.Time) (string, string, error) {
	if err := s.ready(); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(passphrase) == "" {
		return "", "", fmt.Errorf("backup passphrase is required")
	}

	encryptedFile, err := os.CreateTemp("", "xlyra-backup-*.xlyra")
	if err != nil {
		return "", "", fmt.Errorf("create encrypted backup: %w", err)
	}
	encryptedPath := encryptedFile.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(encryptedPath)
		}
	}()

	plainReader, plainWriter := io.Pipe()
	payload := archivePayload{
		Manifest: manifest{
			FormatVersion: currentFormatVersion,
			App:           backupAppName,
			Payload:       backupPayload,
			CreatedAt:     createdAt,
			Tables:        append([]string(nil), exportTables...),
		},
		Config: config.SanitizeConfigForBackup(s.confFile.Data()),
	}
	writeErrs := make(chan error, 1)
	go func() {
		writeErr := writeArchive(payload, func(zw *zip.Writer) error {
			return exportDatabase(ctx, s.db.DB(), s.masterKey, zw)
		}, plainWriter)
		if writeErr != nil {
			_ = plainWriter.CloseWithError(writeErr)
			writeErrs <- writeErr
			return
		}
		if err := plainWriter.Close(); err != nil {
			writeErrs <- fmt.Errorf("close backup archive stream: %w", err)
			return
		}
		writeErrs <- nil
	}()

	encryptErr := encryptStream(passphrase, plainReader, encryptedFile)
	if encryptErr != nil {
		_ = plainReader.CloseWithError(encryptErr)
	}
	writeErr := <-writeErrs
	closeEncryptedErr := encryptedFile.Close()
	if writeErr != nil {
		return "", "", writeErr
	}
	if encryptErr != nil {
		return "", "", encryptErr
	}
	if closeEncryptedErr != nil {
		return "", "", fmt.Errorf("close encrypted backup: %w", closeEncryptedErr)
	}

	success = true
	return encryptedPath, s.backupFilename(createdAt), nil
}

func (s Service) backupFilename(createdAt time.Time) string {
	return fmt.Sprintf("xlyra-backup-%s.zip.xlyra", s.timeZone.Format(createdAt, "20060102-150405"))
}

func backupTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	return config.TimeZoneOrDefault(timeZones...)
}

func (s Service) Import(ctx context.Context, passphrase string, encrypted []byte) (ImportSummary, error) {
	if err := s.ready(); err != nil {
		return ImportSummary{}, err
	}
	if strings.TrimSpace(passphrase) == "" {
		return ImportSummary{}, fmt.Errorf("backup passphrase is required")
	}
	if len(encrypted) == 0 {
		return ImportSummary{}, fmt.Errorf("backup file is required")
	}

	archiveFile, err := os.CreateTemp("", "xlyra-backup-import-*.zip")
	if err != nil {
		return ImportSummary{}, fmt.Errorf("create backup archive: %w", err)
	}
	archivePath := archiveFile.Name()
	defer os.Remove(archivePath)

	err = decryptStream(passphrase, bytes.NewReader(encrypted), archiveFile)
	closeErr := archiveFile.Close()
	if err != nil {
		return ImportSummary{}, err
	}
	if closeErr != nil {
		return ImportSummary{}, fmt.Errorf("close backup archive: %w", closeErr)
	}
	payload, dbDump, err := parseArchive(archivePath)
	if err != nil {
		return ImportSummary{}, err
	}
	if err := validateManifest(payload.Manifest); err != nil {
		return ImportSummary{}, err
	}
	importedRows, err := importDatabase(ctx, s.db.DB(), s.masterKey, dbDump)
	if err != nil {
		return ImportSummary{}, err
	}
	if err := s.confFile.Replace(config.MergeImportedConfig(s.confFile.Data(), payload.Config)); err != nil {
		return ImportSummary{}, fmt.Errorf("replace config: %w", err)
	}

	return ImportSummary{
		Tables:        len(backupTables),
		Rows:          importedRows,
		ConfigKeys:    len(payload.Config),
		FormatVersion: payload.Manifest.FormatVersion,
	}, nil
}

func (s Service) ready() error {
	if s.db == nil || s.db.DB() == nil {
		return fmt.Errorf("database is not available")
	}
	if s.confFile == nil {
		return fmt.Errorf("config persistence is not available")
	}
	if strings.TrimSpace(s.masterKey) == "" {
		return fmt.Errorf("master key is not available")
	}
	return nil
}

func validateManifest(value manifest) error {
	if value.App != backupAppName {
		return fmt.Errorf("backup app mismatch")
	}
	if value.FormatVersion != currentFormatVersion {
		return fmt.Errorf("unsupported backup format version %d", value.FormatVersion)
	}
	if value.Payload != backupPayload {
		return fmt.Errorf("unsupported backup payload %q", value.Payload)
	}
	if len(value.Tables) != len(exportTables) {
		return fmt.Errorf("backup table list mismatch")
	}
	for i, table := range exportTables {
		if value.Tables[i] != table {
			return fmt.Errorf("backup table list mismatch")
		}
	}
	return nil
}
