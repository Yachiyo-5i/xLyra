package backup

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"xlyra/server/internal/config"
	"xlyra/server/internal/credential"
)

const (
	automaticBackupTimeout = 10 * time.Minute
	automaticTaskRetention = time.Hour
)

type AutomaticConfigInput struct {
	Enabled          bool   `json:"enabled"`
	Cron             string `json:"cron"`
	RetentionCount   int    `json:"retention_count"`
	Endpoint         string `json:"endpoint"`
	Region           string `json:"region"`
	Bucket           string `json:"bucket"`
	Prefix           string `json:"prefix"`
	AccessKey        string `json:"access_key"`
	SecretKey        string `json:"secret_key"`
	BackupPassphrase string `json:"backup_passphrase"`
	ForcePathStyle   bool   `json:"force_path_style"`
	UseSSL           bool   `json:"use_ssl"`
	SkipTLSVerify    bool   `json:"skip_tls_verify"`
}

type AutomaticConfigPayload struct {
	Enabled                bool   `json:"enabled"`
	Cron                   string `json:"cron"`
	RetentionCount         int    `json:"retention_count"`
	Endpoint               string `json:"endpoint"`
	Region                 string `json:"region"`
	Bucket                 string `json:"bucket"`
	Prefix                 string `json:"prefix"`
	AccessKey              string `json:"access_key"`
	SecretKeyMasked        string `json:"secret_key_masked,omitempty"`
	BackupPassphraseMasked string `json:"backup_passphrase_masked,omitempty"`
	HasSecretKey           bool   `json:"has_secret_key"`
	HasBackupPassphrase    bool   `json:"has_backup_passphrase"`
	ForcePathStyle         bool   `json:"force_path_style"`
	UseSSL                 bool   `json:"use_ssl"`
	SkipTLSVerify          bool   `json:"skip_tls_verify"`
	Ready                  bool   `json:"ready"`
}

type AutomaticTestResult struct {
	OK        bool   `json:"ok"`
	Stage     string `json:"stage"`
	LatencyMS int64  `json:"latency_ms"`
	Message   string `json:"message"`
}

type AutomaticBackupFile struct {
	Key          string    `json:"key"`
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	Status       string    `json:"status,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type AutomaticRunResult struct {
	File         AutomaticBackupFile `json:"file"`
	DeletedCount int                 `json:"deleted_count"`
	DeletedKeys  []string            `json:"deleted_keys"`
}

type AutomaticRunTask struct {
	Key       string    `json:"key"`
	Filename  string    `json:"filename"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
}

type automaticBackupVersion struct {
	Key            string
	VersionID      string
	IsLatest       bool
	IsDeleteMarker bool
}

type AutomaticService struct {
	base        Service
	credentials credential.Service
	running     atomic.Bool
	tasksMu     sync.Mutex
	tasks       map[string]AutomaticBackupFile
}

func NewAutomaticService(base Service, masterKey string) *AutomaticService {
	return &AutomaticService{
		base:        base,
		credentials: credential.NewService(masterKey),
		tasks:       make(map[string]AutomaticBackupFile),
	}
}

func (s *AutomaticService) GetConfig() AutomaticConfigPayload {
	cfg, _ := config.ReadAutomaticBackupConfig(s.base.confFile)
	return automaticConfigPayload(cfg)
}

func (s *AutomaticService) UpdateConfig(input AutomaticConfigInput) (AutomaticConfigPayload, error) {
	if s.base.confFile == nil {
		return AutomaticConfigPayload{}, fmt.Errorf("config persistence is not available")
	}
	current, _ := config.ReadAutomaticBackupConfig(s.base.confFile)
	next, err := s.configFromInput(input, current)
	if err != nil {
		return AutomaticConfigPayload{}, err
	}
	if err := config.ValidateAutomaticBackupConfig(next); err != nil {
		return AutomaticConfigPayload{}, err
	}
	if err := s.base.confFile.Set(config.AutomaticBackupConfigPath, config.AutomaticBackupConfigToMap(next)); err != nil {
		return AutomaticConfigPayload{}, fmt.Errorf("save automatic backup config: %w", err)
	}
	return automaticConfigPayload(next), nil
}

func (s *AutomaticService) Test(ctx context.Context) (AutomaticTestResult, error) {
	cfg, ok := config.ReadAutomaticBackupConfig(s.base.confFile)
	if !ok {
		return AutomaticTestResult{}, fmt.Errorf("automatic backup config is not available")
	}
	started := time.Now()
	fail := func(stage string, err error) (AutomaticTestResult, error) {
		return AutomaticTestResult{
			OK:        false,
			Stage:     stage,
			LatencyMS: time.Since(started).Milliseconds(),
			Message:   err.Error(),
		}, nil
	}
	client, err := s.clientFromConfig(cfg)
	if err != nil {
		return fail("config", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := client.BucketExists(ctx, cfg.Storage.Bucket); err != nil {
		return fail("bucket", err)
	}
	if _, err := s.listFilesWithClient(ctx, cfg, client, 1); err != nil {
		return fail("list", err)
	}
	if _, err := s.bucketUsesVersioning(ctx, cfg, client); err != nil {
		return fail("versioning", err)
	}
	return AutomaticTestResult{
		OK:        true,
		Stage:     "list",
		LatencyMS: time.Since(started).Milliseconds(),
		Message:   "S3 compatible storage is reachable",
	}, nil
}

func (s *AutomaticService) ListFiles(ctx context.Context) ([]AutomaticBackupFile, error) {
	cfg, client, err := s.readyClient()
	if err != nil {
		return nil, err
	}
	files, err := s.listFilesWithClient(ctx, cfg, client, 0)
	if err != nil {
		return nil, err
	}
	return s.mergeTaskFiles(cfg, files), nil
}

func (s *AutomaticService) RunNow() (AutomaticRunTask, error) {
	if !s.running.CompareAndSwap(false, true) {
		return AutomaticRunTask{}, ErrAutomaticAlreadyRunning
	}

	cfg, client, err := s.readyClient()
	if err != nil {
		s.running.Store(false)
		return AutomaticRunTask{}, err
	}
	passphrase, err := s.decryptBackupPassphrase(cfg)
	if err != nil {
		s.running.Store(false)
		return AutomaticRunTask{}, err
	}

	startedAt := s.base.now().UTC()
	filename := s.base.backupFilename(startedAt)
	key := backupObjectKey(cfg.Storage.Prefix, filename)
	taskFile := AutomaticBackupFile{
		Key:          key,
		Filename:     filename,
		LastModified: startedAt,
		Status:       "running",
	}
	s.storeTaskFile(taskFile)

	go func() {
		defer s.running.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), automaticBackupTimeout)
		defer cancel()
		_, err := s.run(ctx, cfg, client, passphrase, startedAt)
		if err != nil {
			taskFile.Status = "failed"
			taskFile.Error = err.Error()
			s.storeTaskFile(taskFile)
		} else {
			s.removeTaskFile(key)
		}
	}()

	return AutomaticRunTask{
		Key:       key,
		Filename:  filename,
		Status:    "running",
		StartedAt: startedAt,
	}, nil
}

func (s *AutomaticService) RunScheduled(ctx context.Context) (AutomaticRunResult, error) {
	if !s.running.CompareAndSwap(false, true) {
		return AutomaticRunResult{}, ErrAutomaticAlreadyRunning
	}
	defer s.running.Store(false)

	cfg, client, err := s.readyClient()
	if err != nil {
		return AutomaticRunResult{}, err
	}
	passphrase, err := s.decryptBackupPassphrase(cfg)
	if err != nil {
		return AutomaticRunResult{}, err
	}

	startedAt := s.base.now().UTC()
	filename := s.base.backupFilename(startedAt)
	key := backupObjectKey(cfg.Storage.Prefix, filename)
	taskFile := AutomaticBackupFile{
		Key:          key,
		Filename:     filename,
		LastModified: startedAt,
		Status:       "running",
	}
	s.storeTaskFile(taskFile)

	ctx, cancel := context.WithTimeout(ctx, automaticBackupTimeout)
	defer cancel()

	result, err := s.run(ctx, cfg, client, passphrase, startedAt)
	if err != nil {
		taskFile.Status = "failed"
		taskFile.Error = err.Error()
		s.storeTaskFile(taskFile)
		return result, err
	}
	s.removeTaskFile(key)
	return result, nil
}

func (s *AutomaticService) run(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, passphrase string, createdAt time.Time) (AutomaticRunResult, error) {
	filePath, filename, err := s.base.exportAt(ctx, passphrase, createdAt)
	if err != nil {
		return AutomaticRunResult{}, err
	}
	defer os.Remove(filePath)

	file, err := os.Open(filePath)
	if err != nil {
		return AutomaticRunResult{}, fmt.Errorf("open generated backup: %w", err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return AutomaticRunResult{}, fmt.Errorf("stat generated backup: %w", err)
	}
	key := backupObjectKey(cfg.Storage.Prefix, filename)
	if _, err := client.PutObject(ctx, cfg.Storage.Bucket, key, file, stat.Size(), minio.PutObjectOptions{
		ContentType: "application/vnd.xlyra.backup",
	}); err != nil {
		return AutomaticRunResult{}, fmt.Errorf("upload backup to S3: %w", err)
	}

	result := AutomaticRunResult{
		File: AutomaticBackupFile{
			Key:          key,
			Filename:     filename,
			Size:         stat.Size(),
			LastModified: createdAt,
		},
	}
	deleted, err := s.pruneBackups(ctx, cfg, client)
	if err != nil {
		return result, err
	}
	result.DeletedKeys = deleted
	result.DeletedCount = len(deleted)
	return result, nil
}

func (s *AutomaticService) Restore(ctx context.Context, key string) (ImportSummary, error) {
	cfg, client, err := s.readyClient()
	if err != nil {
		return ImportSummary{}, err
	}
	key, err = normalizeBackupObjectKey(cfg.Storage.Prefix, key)
	if err != nil {
		return ImportSummary{}, err
	}
	passphrase, err := s.decryptBackupPassphrase(cfg)
	if err != nil {
		return ImportSummary{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, automaticBackupTimeout)
	defer cancel()
	object, err := client.GetObject(ctx, cfg.Storage.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return ImportSummary{}, fmt.Errorf("download backup from S3: %w", err)
	}
	defer object.Close()
	data, err := io.ReadAll(object)
	if err != nil {
		return ImportSummary{}, fmt.Errorf("read backup from S3: %w", err)
	}
	return s.base.Import(ctx, passphrase, data)
}

func (s *AutomaticService) Delete(ctx context.Context, key string) error {
	cfg, client, err := s.readyClient()
	if err != nil {
		return err
	}
	key, err = normalizeBackupObjectKey(cfg.Storage.Prefix, key)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.deleteBackupKeys(ctx, cfg, client, []string{key}); err != nil {
		return fmt.Errorf("delete backup from S3: %w", err)
	}
	s.removeTaskFile(key)
	return nil
}

func (s *AutomaticService) storeTaskFile(file AutomaticBackupFile) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	if s.tasks == nil {
		s.tasks = make(map[string]AutomaticBackupFile)
	}
	s.tasks[file.Key] = file
}

func (s *AutomaticService) removeTaskFile(key string) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()
	delete(s.tasks, key)
}

func (s *AutomaticService) mergeTaskFiles(cfg config.AutomaticBackupConfig, files []AutomaticBackupFile) []AutomaticBackupFile {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	if len(s.tasks) == 0 {
		return files
	}
	now := time.Now().UTC()
	existing := make(map[string]int, len(files))
	for i, file := range files {
		existing[file.Key] = i
	}
	for key, task := range s.tasks {
		if task.Status != "running" && now.Sub(task.LastModified) > automaticTaskRetention {
			delete(s.tasks, key)
			continue
		}
		if index, ok := existing[key]; ok {
			if task.Status == "failed" {
				files[index].Status = task.Status
				files[index].Error = task.Error
				continue
			}
			delete(s.tasks, key)
			continue
		}
		if !isBackupObject(cfg.Storage.Prefix, key) {
			continue
		}
		files = append(files, task)
	}
	sortBackupFiles(files)
	return files
}

func (s *AutomaticService) configFromInput(input AutomaticConfigInput, current config.AutomaticBackupConfig) (config.AutomaticBackupConfig, error) {
	next := config.AutomaticBackupConfig{
		Enabled:        input.Enabled,
		Cron:           input.Cron,
		RetentionCount: input.RetentionCount,
		Storage: config.AutomaticBackupStorageConfig{
			Endpoint:                  input.Endpoint,
			Region:                    input.Region,
			Bucket:                    input.Bucket,
			Prefix:                    input.Prefix,
			AccessKey:                 input.AccessKey,
			SecretKeyEncrypted:        current.Storage.SecretKeyEncrypted,
			SecretKeyMasked:           current.Storage.SecretKeyMasked,
			ForcePathStyle:            input.ForcePathStyle,
			UseSSL:                    input.UseSSL,
			SkipTLSVerify:             input.SkipTLSVerify,
			BackupPassphraseEncrypted: current.Storage.BackupPassphraseEncrypted,
			BackupPassphraseMasked:    current.Storage.BackupPassphraseMasked,
		},
	}
	if strings.TrimSpace(input.SecretKey) != "" {
		encrypted, masked, err := s.credentials.Encrypt(input.SecretKey)
		if err != nil {
			return config.AutomaticBackupConfig{}, fmt.Errorf("encrypt storage secret key: %w", err)
		}
		next.Storage.SecretKeyEncrypted = encrypted
		next.Storage.SecretKeyMasked = masked
	}
	if strings.TrimSpace(input.BackupPassphrase) != "" {
		encrypted, masked, err := s.credentials.Encrypt(input.BackupPassphrase)
		if err != nil {
			return config.AutomaticBackupConfig{}, fmt.Errorf("encrypt backup passphrase: %w", err)
		}
		next.Storage.BackupPassphraseEncrypted = encrypted
		next.Storage.BackupPassphraseMasked = masked
	}
	return config.NormalizeAutomaticBackupConfig(next), nil
}

func (s *AutomaticService) readyClient() (config.AutomaticBackupConfig, *minio.Client, error) {
	cfg, ok := config.ReadAutomaticBackupConfig(s.base.confFile)
	if !ok {
		return cfg, nil, fmt.Errorf("automatic backup config is not available")
	}
	if err := config.ValidateAutomaticBackupConfig(cfg); err != nil {
		return cfg, nil, err
	}
	if err := config.ValidateAutomaticBackupStorageConfig(cfg); err != nil {
		return cfg, nil, err
	}
	client, err := s.clientFromConfig(cfg)
	if err != nil {
		return cfg, nil, err
	}
	return cfg, client, nil
}

func (s *AutomaticService) clientFromConfig(cfg config.AutomaticBackupConfig) (*minio.Client, error) {
	secretKey, err := s.decryptStorageSecret(cfg)
	if err != nil {
		return nil, err
	}
	endpoint, secure, err := normalizeS3Endpoint(cfg.Storage.Endpoint, cfg.Storage.UseSSL)
	if err != nil {
		return nil, err
	}
	options := &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.Storage.AccessKey, secretKey, ""),
		Secure:       secure,
		Region:       cfg.Storage.Region,
		BucketLookup: minio.BucketLookupDNS,
	}
	if cfg.Storage.ForcePathStyle {
		options.BucketLookup = minio.BucketLookupPath
	}
	if cfg.Storage.SkipTLSVerify {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
		options.Transport = transport
	}
	client, err := minio.New(endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	return client, nil
}

func (s *AutomaticService) decryptStorageSecret(cfg config.AutomaticBackupConfig) (string, error) {
	if strings.TrimSpace(cfg.Storage.SecretKeyEncrypted) == "" {
		return "", fmt.Errorf("storage.secret_key is required")
	}
	secret, err := s.credentials.Decrypt(cfg.Storage.SecretKeyEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt storage secret key: %w", err)
	}
	return secret, nil
}

func (s *AutomaticService) decryptBackupPassphrase(cfg config.AutomaticBackupConfig) (string, error) {
	if strings.TrimSpace(cfg.Storage.BackupPassphraseEncrypted) == "" {
		return "", fmt.Errorf("backup passphrase is required")
	}
	passphrase, err := s.credentials.Decrypt(cfg.Storage.BackupPassphraseEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt backup passphrase: %w", err)
	}
	return passphrase, nil
}

func (s *AutomaticService) listFilesWithClient(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, limit int) ([]AutomaticBackupFile, error) {
	return s.listFiles(ctx, cfg, client, limit, 30*time.Second)
}

func (s *AutomaticService) listAllFilesWithClient(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client) ([]AutomaticBackupFile, error) {
	return s.listFiles(ctx, cfg, client, 0, 0)
}

func (s *AutomaticService) listFiles(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, limit int, timeout time.Duration) ([]AutomaticBackupFile, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	files := make([]AutomaticBackupFile, 0)
	for item := range client.ListObjects(ctx, cfg.Storage.Bucket, minio.ListObjectsOptions{
		Prefix:    cfg.Storage.Prefix,
		Recursive: true,
	}) {
		if item.Err != nil {
			return nil, fmt.Errorf("list backup files from S3: %w", item.Err)
		}
		if !isBackupObject(cfg.Storage.Prefix, item.Key) {
			continue
		}
		files = append(files, AutomaticBackupFile{
			Key:          item.Key,
			Filename:     path.Base(item.Key),
			Size:         item.Size,
			LastModified: item.LastModified,
		})
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	sortBackupFiles(files)
	return files, nil
}

func (s *AutomaticService) pruneBackups(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client) ([]string, error) {
	files, err := s.listAllFilesWithClient(ctx, cfg, client)
	if err != nil {
		return nil, err
	}
	retainedCount := cfg.RetentionCount
	if retainedCount > len(files) {
		retainedCount = len(files)
	}
	retained := make(map[string]struct{}, retainedCount)
	for _, file := range files[:retainedCount] {
		retained[file.Key] = struct{}{}
	}
	versioned, err := s.bucketUsesVersioning(ctx, cfg, client)
	if err != nil {
		return nil, err
	}
	if versioned {
		return s.pruneVersionedBackups(ctx, cfg, client, retained)
	}
	if len(files) <= cfg.RetentionCount {
		return nil, nil
	}
	toDelete := make([]string, 0, len(files)-cfg.RetentionCount)
	for _, file := range files[cfg.RetentionCount:] {
		toDelete = append(toDelete, file.Key)
	}
	if err := s.deleteUnversionedBackupKeys(ctx, cfg, client, toDelete); err != nil {
		return nil, err
	}
	return toDelete, nil
}

func (s *AutomaticService) bucketUsesVersioning(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client) (bool, error) {
	versioning, err := client.GetBucketVersioning(ctx, cfg.Storage.Bucket)
	if err != nil {
		return false, fmt.Errorf("read backup bucket versioning: %w", err)
	}
	return versioning.Enabled() || versioning.Suspended(), nil
}

func (s *AutomaticService) deleteBackupKeys(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, keys []string) error {
	versioned, err := s.bucketUsesVersioning(ctx, cfg, client)
	if err != nil {
		return err
	}
	if !versioned {
		return s.deleteUnversionedBackupKeys(ctx, cfg, client, keys)
	}
	versions, err := s.listBackupVersionsWithClient(ctx, cfg, client)
	if err != nil {
		return err
	}
	keysSet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keysSet[key] = struct{}{}
	}
	targets := make([]automaticBackupVersion, 0)
	for _, version := range versions {
		if _, ok := keysSet[version.Key]; ok {
			targets = append(targets, version)
		}
	}
	return s.deleteBackupVersions(ctx, cfg, client, targets)
}

func (s *AutomaticService) pruneVersionedBackups(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, retained map[string]struct{}) ([]string, error) {
	versions, err := s.listBackupVersionsWithClient(ctx, cfg, client)
	if err != nil {
		return nil, err
	}
	targets := make([]automaticBackupVersion, 0, len(versions))
	deletedKeys := make([]string, 0)
	seenKeys := make(map[string]struct{})
	for _, version := range versions {
		_, keep := retained[version.Key]
		if keep && version.IsLatest && !version.IsDeleteMarker {
			continue
		}
		targets = append(targets, version)
		if !keep {
			if _, seen := seenKeys[version.Key]; seen {
				continue
			}
			seenKeys[version.Key] = struct{}{}
			deletedKeys = append(deletedKeys, version.Key)
		}
	}
	if err := s.deleteBackupVersions(ctx, cfg, client, targets); err != nil {
		return nil, err
	}
	return deletedKeys, nil
}

func (s *AutomaticService) deleteUnversionedBackupKeys(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, key := range keys {
		if err := client.RemoveObject(ctx, cfg.Storage.Bucket, key, minio.RemoveObjectOptions{}); err != nil {
			return fmt.Errorf("delete backup %s: %w", key, err)
		}
	}
	files, err := s.listAllFilesWithClient(ctx, cfg, client)
	if err != nil {
		return err
	}
	remaining := make(map[string]struct{}, len(files))
	for _, file := range files {
		remaining[file.Key] = struct{}{}
	}
	for _, key := range keys {
		if _, ok := remaining[key]; ok {
			return fmt.Errorf("verify deleted backup %s: object is still present", key)
		}
	}
	return nil
}

func (s *AutomaticService) deleteBackupVersions(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client, targets []automaticBackupVersion) error {
	if len(targets) == 0 {
		return nil
	}
	for _, target := range targets {
		if strings.TrimSpace(target.VersionID) == "" {
			return fmt.Errorf("delete backup %s: version ID is missing", target.Key)
		}
	}
	objects := make(chan minio.ObjectInfo)
	go func() {
		defer close(objects)
		for _, target := range targets {
			select {
			case objects <- minio.ObjectInfo{Key: target.Key, VersionID: target.VersionID}:
			case <-ctx.Done():
				return
			}
		}
	}()
	var deleteErr error
	for result := range client.RemoveObjects(ctx, cfg.Storage.Bucket, objects, minio.RemoveObjectsOptions{}) {
		if result.Err == nil || deleteErr != nil {
			continue
		}
		if result.ObjectName == "" {
			deleteErr = fmt.Errorf("delete backup versions: %w", result.Err)
			continue
		}
		deleteErr = fmt.Errorf("delete backup %s version %s: %w", result.ObjectName, result.VersionID, result.Err)
	}
	if deleteErr != nil {
		return deleteErr
	}
	versions, err := s.listBackupVersionsWithClient(ctx, cfg, client)
	if err != nil {
		return err
	}
	remaining := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		remaining[backupVersionKey(version.Key, version.VersionID)] = struct{}{}
	}
	for _, target := range targets {
		if _, ok := remaining[backupVersionKey(target.Key, target.VersionID)]; ok {
			return fmt.Errorf("verify deleted backup %s version %s: object version is still present", target.Key, target.VersionID)
		}
	}
	return nil
}

func (s *AutomaticService) listBackupVersionsWithClient(ctx context.Context, cfg config.AutomaticBackupConfig, client *minio.Client) ([]automaticBackupVersion, error) {
	versions := make([]automaticBackupVersion, 0)
	for item := range client.ListObjects(ctx, cfg.Storage.Bucket, minio.ListObjectsOptions{
		Prefix:       cfg.Storage.Prefix,
		Recursive:    true,
		WithVersions: true,
	}) {
		if item.Err != nil {
			return nil, fmt.Errorf("list backup versions from S3: %w", item.Err)
		}
		if !isBackupObject(cfg.Storage.Prefix, item.Key) {
			continue
		}
		versions = append(versions, automaticBackupVersion{
			Key:            item.Key,
			VersionID:      item.VersionID,
			IsLatest:       item.IsLatest,
			IsDeleteMarker: item.IsDeleteMarker,
		})
	}
	return versions, nil
}

func backupVersionKey(key string, versionID string) string {
	return key + "\x00" + versionID
}

func automaticConfigPayload(cfg config.AutomaticBackupConfig) AutomaticConfigPayload {
	cfg = config.NormalizeAutomaticBackupConfig(cfg)
	return AutomaticConfigPayload{
		Enabled:                cfg.Enabled,
		Cron:                   cfg.Cron,
		RetentionCount:         cfg.RetentionCount,
		Endpoint:               cfg.Storage.Endpoint,
		Region:                 cfg.Storage.Region,
		Bucket:                 cfg.Storage.Bucket,
		Prefix:                 cfg.Storage.Prefix,
		AccessKey:              cfg.Storage.AccessKey,
		SecretKeyMasked:        cfg.Storage.SecretKeyMasked,
		BackupPassphraseMasked: cfg.Storage.BackupPassphraseMasked,
		HasSecretKey:           cfg.Storage.SecretKeyEncrypted != "",
		HasBackupPassphrase:    cfg.Storage.BackupPassphraseEncrypted != "",
		ForcePathStyle:         cfg.Storage.ForcePathStyle,
		UseSSL:                 cfg.Storage.UseSSL,
		SkipTLSVerify:          cfg.Storage.SkipTLSVerify,
		Ready:                  config.AutomaticBackupConfigReady(cfg),
	}
}

func normalizeS3Endpoint(endpoint string, fallbackSecure bool) (string, bool, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", false, fmt.Errorf("storage.endpoint is required")
	}
	secure := fallbackSecure
	if strings.Contains(endpoint, "://") {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return "", false, fmt.Errorf("parse storage endpoint: %w", err)
		}
		if parsed.Host == "" {
			return "", false, fmt.Errorf("storage.endpoint host is required")
		}
		switch parsed.Scheme {
		case "http":
			secure = false
		case "https":
			secure = true
		default:
			return "", false, fmt.Errorf("storage.endpoint scheme must be http or https")
		}
		endpoint = parsed.Host
		if parsed.Path != "" && parsed.Path != "/" {
			return "", false, fmt.Errorf("storage.endpoint path is not supported")
		}
	}
	return strings.TrimRight(endpoint, "/"), secure, nil
}

func backupObjectKey(prefix string, filename string) string {
	prefix = config.NormalizeBackupPrefix(prefix)
	return prefix + strings.TrimLeft(path.Base(filename), "/")
}

func normalizeBackupObjectKey(prefix string, key string) (string, error) {
	prefix = config.NormalizeBackupPrefix(prefix)
	key = strings.TrimSpace(strings.TrimLeft(key, "/"))
	if key == "" {
		return "", fmt.Errorf("backup object key is required")
	}
	clean := path.Clean(key)
	if clean == "." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("backup object key is invalid")
	}
	if !strings.HasPrefix(clean, prefix) {
		return "", fmt.Errorf("backup object is outside configured prefix")
	}
	if !isBackupObject(prefix, clean) {
		return "", fmt.Errorf("backup object format is invalid")
	}
	return clean, nil
}

func isBackupObject(prefix string, key string) bool {
	prefix = config.NormalizeBackupPrefix(prefix)
	key = strings.TrimSpace(strings.TrimLeft(key, "/"))
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	name := path.Base(key)
	return strings.HasPrefix(name, "xlyra-backup-") && strings.HasSuffix(name, ".zip.xlyra")
}

func sortBackupFiles(files []AutomaticBackupFile) {
	sort.SliceStable(files, func(i int, j int) bool {
		left := files[i].LastModified
		right := files[j].LastModified
		if left.Equal(right) {
			return files[i].Key > files[j].Key
		}
		return left.After(right)
	})
}

func IsAlreadyRunningError(err error) bool {
	return err != nil && errors.Is(err, ErrAutomaticAlreadyRunning)
}

var ErrAutomaticAlreadyRunning = errors.New("automatic backup is already running")
