package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	JSPluginSourceUploaded = "uploaded"
	JSPluginStatusUploaded = "uploaded"
	JSPluginStatusVerified = "verified"
	JSPluginStatusEnabled  = "enabled"
	JSPluginStatusDisabled = "disabled"
	JSPluginStatusBroken   = "broken"

	JSPluginBindingQuotaProbe   = "site_quota_probe"
	JSPluginBindingProtocolSlug = "protocol_endpoint"
)

type JSPlugin struct {
	ID        string `gorm:"primaryKey"`
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type JSPluginVersion struct {
	PluginID      string `gorm:"primaryKey"`
	Version       string `gorm:"primaryKey"`
	Manifest      JSON   `gorm:"type:jsonb"`
	Package       []byte
	PackageSHA256 string
	Signer        string
	Status        string
	SelfTest      JSON `gorm:"column:selftest;type:jsonb"`
	UploadedBy    uuid.NullUUID
	CreatedAt     time.Time
}

type JSPluginTrustedKey struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string
	PublicKey   string
	Fingerprint string
	CreatedBy   uuid.NullUUID
	CreatedAt   time.Time
}

func (JSPluginTrustedKey) TableName() string { return "js_plugin_trusted_keys" }

type JSPluginBinding struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	PluginID    string
	Version     string
	TargetKind  string
	TargetID    string
	CreatedAt   time.Time
}

type JSPluginRepository struct {
	db *gorm.DB
}

func NewJSPluginRepository(db *gorm.DB) JSPluginRepository {
	return JSPluginRepository{db: db}
}

func (r JSPluginRepository) UpsertPlugin(ctx context.Context, id, source string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO js_plugins (id, source, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET updated_at = EXCLUDED.updated_at
	`, id, source, now, now).Error
}

func (r JSPluginRepository) CreateVersion(ctx context.Context, version JSPluginVersion) error {
	return r.db.WithContext(ctx).Create(&version).Error
}

// CreatePluginVersion upserts the parent plugin and inserts the version atomically.
func (r JSPluginRepository) CreatePluginVersion(ctx context.Context, version JSPluginVersion, source string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := JSPluginRepository{db: tx}
		if err := repo.UpsertPlugin(ctx, version.PluginID, source); err != nil {
			return err
		}
		return repo.CreateVersion(ctx, version)
	})
}

func (r JSPluginRepository) GetVersion(ctx context.Context, pluginID, ver string) (JSPluginVersion, error) {
	var item JSPluginVersion
	err := r.db.WithContext(ctx).Where("plugin_id = ? AND version = ?", pluginID, ver).First(&item).Error
	return item, err
}

func (r JSPluginRepository) ListVersions(ctx context.Context, pluginID string) ([]JSPluginVersion, error) {
	var items []JSPluginVersion
	err := r.db.WithContext(ctx).Where("plugin_id = ?", pluginID).Order("created_at DESC").Find(&items).Error
	return items, err
}

func (r JSPluginRepository) ListPlugins(ctx context.Context) ([]JSPlugin, error) {
	var items []JSPlugin
	err := r.db.WithContext(ctx).Order("id ASC").Find(&items).Error
	return items, err
}

func (r JSPluginRepository) ListEnabledVersions(ctx context.Context) ([]JSPluginVersion, error) {
	var items []JSPluginVersion
	err := r.db.WithContext(ctx).Where("status = ?", JSPluginStatusEnabled).Find(&items).Error
	return items, err
}

func (r JSPluginRepository) SetVersionStatus(ctx context.Context, pluginID, version, status string, selftest any) error {
	updates := map[string]any{"status": status}
	if selftest != nil {
		updates["selftest"] = selftest
	}
	return r.db.WithContext(ctx).Model(&JSPluginVersion{}).
		Where("plugin_id = ? AND version = ?", pluginID, version).
		Updates(updates).Error
}

func (r JSPluginRepository) DisableOtherVersions(ctx context.Context, pluginID, enabledVersion string) error {
	return r.db.WithContext(ctx).Model(&JSPluginVersion{}).
		Where("plugin_id = ? AND version <> ? AND status = ?", pluginID, enabledVersion, JSPluginStatusEnabled).
		Update("status", JSPluginStatusDisabled).Error
}

func (r JSPluginRepository) DeleteVersion(ctx context.Context, pluginID, version string) error {
	result := r.db.WithContext(ctx).
		Where("plugin_id = ? AND version = ? AND status <> ?", pluginID, version, JSPluginStatusEnabled).
		Delete(&JSPluginVersion{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r JSPluginRepository) UpsertBinding(ctx context.Context, binding JSPluginBinding) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO js_plugin_bindings (id, plugin_id, version, target_kind, target_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (target_kind, target_id) DO UPDATE SET
			plugin_id = EXCLUDED.plugin_id,
			version = EXCLUDED.version,
			created_at = EXCLUDED.created_at
	`, binding.ID, binding.PluginID, binding.Version, binding.TargetKind, binding.TargetID, time.Now().UTC()).Error
}

func (r JSPluginRepository) DeleteBinding(ctx context.Context, targetKind, targetID string) error {
	return r.db.WithContext(ctx).
		Where("target_kind = ? AND target_id = ?", targetKind, targetID).
		Delete(&JSPluginBinding{}).Error
}

func (r JSPluginRepository) ListBindings(ctx context.Context) ([]JSPluginBinding, error) {
	var items []JSPluginBinding
	err := r.db.WithContext(ctx).Order("created_at ASC").Find(&items).Error
	return items, err
}

func (r JSPluginRepository) ProtocolSlugMap(ctx context.Context) (map[string]string, error) {
	bindings, err := r.ListBindings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, binding := range bindings {
		if binding.TargetKind != JSPluginBindingProtocolSlug {
			continue
		}
		out[binding.TargetID] = binding.PluginID
	}
	return out, nil
}

func (r JSPluginRepository) MarkBroken(ctx context.Context, pluginID, version string, selftest any) error {
	return r.SetVersionStatus(ctx, pluginID, version, JSPluginStatusBroken, selftest)
}

func (r JSPluginRepository) EnableVersion(ctx context.Context, pluginID, version string, selftest any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := JSPluginRepository{db: tx}
		if err := repo.SetVersionStatus(ctx, pluginID, version, JSPluginStatusEnabled, selftest); err != nil {
			return err
		}
		return repo.DisableOtherVersions(ctx, pluginID, version)
	})
}

func (r JSPluginRepository) ListTrustedKeys(ctx context.Context) ([]JSPluginTrustedKey, error) {
	var items []JSPluginTrustedKey
	err := r.db.WithContext(ctx).Order("created_at DESC").Find(&items).Error
	return items, err
}

func (r JSPluginRepository) CreateTrustedKey(ctx context.Context, row *JSPluginTrustedKey) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r JSPluginRepository) GetTrustedKeyByFingerprint(ctx context.Context, fingerprint string) (JSPluginTrustedKey, error) {
	var item JSPluginTrustedKey
	err := r.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).First(&item).Error
	return item, err
}

func (r JSPluginRepository) IsTrustedFingerprint(ctx context.Context, fingerprint string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&JSPluginTrustedKey{}).Where("fingerprint = ?", fingerprint).Count(&count).Error
	return count > 0, err
}

func (r JSPluginRepository) DeleteTrustedKey(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&JSPluginTrustedKey{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func EnabledPluginCompileError(pluginID, version string, err error) error {
	return fmt.Errorf("compile %s@%s: %w", pluginID, version, err)
}
