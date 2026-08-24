package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

const (
	currentFormatVersion         = 3
	minImportFormatVersion       = 2
	backupAppName                = "xlyra"
	MaxImportBytes         int64 = 4 << 30
)

var ErrOperationInProgress = errors.New("another backup or restore operation is in progress")

var operationGuards sync.Map

func operationGuardFor(db *store.Store) *atomic.Bool {
	guard, _ := operationGuards.LoadOrStore(db, &atomic.Bool{})
	return guard.(*atomic.Bool)
}

func beginSharedOperation(db *store.Store) bool {
	if db == nil {
		return true
	}
	return operationGuardFor(db).CompareAndSwap(false, true)
}

func endSharedOperation(db *store.Store) {
	if db == nil {
		return
	}
	operationGuardFor(db).Store(false)
}

type Service struct {
	db             *store.Store
	confFile       *config.ConfigFile
	masterKey      string
	playgroundRoot string
	preRestore     func(context.Context) error
	postRestore    func(context.Context) error
	now            func() time.Time
	timeZone       config.TimeZone
}

type ImportSummary struct {
	Tables        int   `json:"tables"`
	Rows          int   `json:"rows"`
	ConfigKeys    int   `json:"config_keys"`
	FormatVersion int   `json:"format_version"`
	Files         int   `json:"files"`
	FileBytes     int64 `json:"file_bytes"`
}

type ImportOptions struct {
	AdminID uuid.UUID
}

type ProgressEvent struct {
	Step      string         `json:"step"`
	Status    string         `json:"status"`
	Rows      int            `json:"rows,omitempty"`
	TotalRows int            `json:"total_rows,omitempty"`
	Table     string         `json:"table,omitempty"`
	Bytes     int64          `json:"bytes,omitempty"`
	Total     int64          `json:"total_bytes,omitempty"`
	Summary   *ImportSummary `json:"summary,omitempty"`
	Message   string         `json:"message,omitempty"`
}

type ProgressFunc func(ProgressEvent)

func NewService(db *store.Store, confFile *config.ConfigFile, masterKey string, playgroundRoot string, timeZones ...config.TimeZone) Service {
	return Service{
		db:             db,
		confFile:       confFile,
		masterKey:      masterKey,
		playgroundRoot: playgroundRoot,
		now:            func() time.Time { return time.Now().UTC() },
		timeZone:       backupTimeZone(timeZones...),
	}
}

func (s Service) WithRestoreHooks(pre func(context.Context) error, post func(context.Context) error) Service {
	s.preRestore = pre
	s.postRestore = post
	return s
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
		writeErr := writeArchive(payload, func(zw *zip.Writer) (map[string]int, error) {
			return exportDatabase(ctx, s.db.DB(), s.masterKey, s.playgroundRoot, zw)
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
	stat, err := os.Stat(encryptedPath)
	if err != nil {
		return "", "", fmt.Errorf("stat encrypted backup: %w", err)
	}
	if stat.Size() > MaxImportBytes {
		return "", "", fmt.Errorf("backup size %d exceeds maximum restorable size %d", stat.Size(), MaxImportBytes)
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

func (s Service) Import(ctx context.Context, passphrase string, encrypted []byte, opts ImportOptions, progress ...ProgressFunc) (ImportSummary, error) {
	if len(encrypted) == 0 {
		return ImportSummary{}, fmt.Errorf("backup file is required")
	}
	return s.ImportReader(ctx, passphrase, bytes.NewReader(encrypted), opts, progress...)
}

func (s Service) ImportReader(ctx context.Context, passphrase string, encrypted io.Reader, opts ImportOptions, progress ...ProgressFunc) (ImportSummary, error) {
	return s.importReader(ctx, passphrase, encrypted, opts, nil, progress...)
}

func (s Service) importReader(ctx context.Context, passphrase string, encrypted io.Reader, opts ImportOptions, beforeCommit func() error, progress ...ProgressFunc) (ImportSummary, error) {
	if !beginSharedOperation(s.db) {
		return ImportSummary{}, ErrOperationInProgress
	}
	defer endSharedOperation(s.db)
	return s.importReaderLocked(ctx, passphrase, encrypted, opts, beforeCommit, progress...)
}

func (s Service) importReaderLocked(ctx context.Context, passphrase string, encrypted io.Reader, opts ImportOptions, beforeCommit func() error, progress ...ProgressFunc) (ImportSummary, error) {
	if err := s.ready(); err != nil {
		return ImportSummary{}, err
	}
	if strings.TrimSpace(passphrase) == "" {
		return ImportSummary{}, fmt.Errorf("backup passphrase is required")
	}
	if encrypted == nil {
		return ImportSummary{}, fmt.Errorf("backup file is required")
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
	emit(ProgressEvent{Step: "decrypt", Status: "in_progress", Message: "Decrypting backup archive"})

	archiveFile, err := os.CreateTemp("", "xlyra-backup-import-*.zip")
	if err != nil {
		return ImportSummary{}, fmt.Errorf("create backup archive: %w", err)
	}
	archivePath := archiveFile.Name()
	defer os.Remove(archivePath)

	err = decryptStream(passphrase, &backupContextReader{ctx: ctx, reader: encrypted}, archiveFile)
	closeErr := archiveFile.Close()
	if err != nil {
		return ImportSummary{}, err
	}
	if closeErr != nil {
		return ImportSummary{}, fmt.Errorf("close backup archive: %w", closeErr)
	}

	emit(ProgressEvent{Step: "decrypt", Status: "complete"})
	emit(ProgressEvent{Step: "parse", Status: "in_progress", Message: "Parsing backup archive"})

	payload, dbDump, err := parseArchiveContext(ctx, archivePath)
	if err != nil {
		return ImportSummary{}, err
	}
	if err := validateManifest(payload.Manifest); err != nil {
		return ImportSummary{}, err
	}
	dbDump.beforeCommit = beforeCommit
	preparedConfig, err := s.confFile.PrepareReplace(config.MergeImportedConfig(s.confFile.Data(), payload.Config))
	if err != nil {
		return ImportSummary{}, fmt.Errorf("prepare config replacement: %w", err)
	}
	defer preparedConfig.Discard()

	emit(ProgressEvent{Step: "parse", Status: "complete", TotalRows: dbDump.TotalRows})

	quiesced := false
	if s.preRestore != nil {
		if err := s.preRestore(ctx); err != nil {
			return ImportSummary{}, fmt.Errorf("prepare restore: %w", err)
		}
		quiesced = true
	}
	converge := func() {
		if quiesced && s.postRestore != nil {
			_ = s.postRestore(ctx)
		}
	}

	emit(ProgressEvent{Step: "import", Status: "in_progress", TotalRows: dbDump.TotalRows, Message: "Writing backup data to database"})

	importedRows, totalRows, err := importDatabase(ctx, s.db.DB(), s.masterKey, dbDump, opts.AdminID, prog)
	if err != nil {
		converge()
		return ImportSummary{}, err
	}

	emit(ProgressEvent{Step: "import", Status: "complete", Rows: importedRows, TotalRows: totalRows})
	emit(ProgressEvent{Step: "files", Status: "in_progress", Message: "Restoring playground session files"})

	lastFileProgress := int64(0)
	restoredFiles, restoredBytes, err := restorePlaygroundFiles(archivePath, s.playgroundRoot, func(restored int64) {
		if restored-lastFileProgress >= 4<<20 {
			lastFileProgress = restored
			emit(ProgressEvent{Step: "files", Status: "in_progress", Bytes: restored})
		}
	})
	if err != nil {
		converge()
		return ImportSummary{}, err
	}

	emit(ProgressEvent{Step: "files", Status: "complete", Bytes: restoredBytes})

	if s.postRestore != nil {
		if err := s.postRestore(ctx); err != nil {
			return ImportSummary{}, fmt.Errorf("finish restore: %w", err)
		}
	}

	if err := preparedConfig.Commit(); err != nil {
		return ImportSummary{}, fmt.Errorf("replace config: %w", err)
	}

	summary := ImportSummary{
		Tables:        len(backupTables),
		Rows:          importedRows,
		ConfigKeys:    len(payload.Config),
		FormatVersion: payload.Manifest.FormatVersion,
		Files:         restoredFiles,
		FileBytes:     restoredBytes,
	}
	emit(ProgressEvent{Step: "complete", Status: "complete", Rows: importedRows, TotalRows: totalRows, Summary: &summary})

	return summary, nil
}

type backupContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *backupContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
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
	if value.FormatVersion < minImportFormatVersion || value.FormatVersion > currentFormatVersion {
		return fmt.Errorf("unsupported backup format version %d", value.FormatVersion)
	}
	if value.Payload != backupPayload {
		return fmt.Errorf("unsupported backup payload %q", value.Payload)
	}
	if value.FormatVersion == currentFormatVersion {
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
	for _, table := range value.Tables {
		if _, ok := backupTableByName(table); !ok {
			return fmt.Errorf("backup table list mismatch")
		}
	}
	return nil
}
