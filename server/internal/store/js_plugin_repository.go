package store

import (
	"context"
	"encoding/json"
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

	// A binding mounts a plugin version on a scope. ScopeSite is one site,
	// ScopeEndpoint is a downstream path (/v1/plugins/<slug>).
	JSPluginScopeSite     = "site"
	JSPluginScopeEndpoint = "endpoint"
	// JSPluginScopeOAuthConnection is the subject of automation bindings.
	JSPluginScopeOAuthConnection = "oauth_connection"
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
	// GrantedPermissions are the automation actions an admin allowed this
	// version to return, as a JSON array of strings.
	GrantedPermissions JSON `gorm:"type:jsonb"`
	Signer             string
	Status             string
	SelfTest           JSON `gorm:"column:selftest;type:jsonb"`
	UploadedBy         uuid.NullUUID
	CreatedAt          time.Time
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

// JSPluginBinding mounts a plugin version on a scope. A scope holds one plugin
// per Slot; exclusive kinds use their kind as the slot, so a site has at most one
// plugin of each kind. Config carries the admin's parameters for the mount.
type JSPluginBinding struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	PluginID  string
	Version   string
	Kind      string
	ScopeType string
	ScopeID   string
	Slot      string
	Config    JSON `gorm:"type:jsonb"`
	// Events and State belong to automation bindings: the events the binding
	// is called for, and what its plugin saved last time.
	Events    JSON `gorm:"type:jsonb"`
	State     JSON `gorm:"type:jsonb"`
	CreatedAt time.Time
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
	if len(version.GrantedPermissions) == 0 {
		version.GrantedPermissions = JSON("[]")
	}
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

// DeleteVersion removes one version that is not enabled. When it was the last
// version, the plugin record goes too; otherwise an empty plugin would stay in
// the list with nothing left to uninstall.
func (r JSPluginRepository) DeleteVersion(ctx context.Context, pluginID, version string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.
			Where("plugin_id = ? AND version = ? AND status <> ?", pluginID, version, JSPluginStatusEnabled).
			Delete(&JSPluginVersion{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		var remaining int64
		if err := tx.Model(&JSPluginVersion{}).Where("plugin_id = ?", pluginID).Count(&remaining).Error; err != nil {
			return err
		}
		if remaining == 0 {
			return tx.Where("id = ?", pluginID).Delete(&JSPlugin{}).Error
		}
		return nil
	})
}

func (r JSPluginRepository) UpsertBinding(ctx context.Context, binding JSPluginBinding) error {
	// The raw insert below bypasses the column default, so a caller that leaves
	// ID empty would write the all-zero UUID and a second binding would collide.
	if binding.ID == uuid.Nil {
		binding.ID = uuid.New()
	}
	if binding.Slot == "" {
		binding.Slot = binding.Kind
	}
	config := []byte(binding.Config)
	if len(config) == 0 {
		config = []byte("{}")
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO js_plugin_bindings (id, plugin_id, version, kind, scope_type, scope_id, slot, config, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?)
		ON CONFLICT (scope_type, scope_id, slot) DO UPDATE SET
			plugin_id = EXCLUDED.plugin_id,
			version = EXCLUDED.version,
			kind = EXCLUDED.kind,
			config = EXCLUDED.config,
			created_at = EXCLUDED.created_at
	`, binding.ID, binding.PluginID, binding.Version, binding.Kind, binding.ScopeType, binding.ScopeID, binding.Slot, string(config), time.Now().UTC()).Error
}

// DeleteBinding removes the binding in one slot of a scope.
func (r JSPluginRepository) DeleteBinding(ctx context.Context, scopeType, scopeID, slot string) error {
	return r.db.WithContext(ctx).
		Where("scope_type = ? AND scope_id = ? AND slot = ?", scopeType, scopeID, slot).
		Delete(&JSPluginBinding{}).Error
}

func (r JSPluginRepository) ListBindings(ctx context.Context) ([]JSPluginBinding, error) {
	var items []JSPluginBinding
	err := r.db.WithContext(ctx).Order("created_at ASC").Find(&items).Error
	return items, err
}

// ProtocolSlugMap maps each bound downstream path to the plugin serving it.
func (r JSPluginRepository) ProtocolSlugMap(ctx context.Context) (map[string]string, error) {
	bindings, err := r.ListBindings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, binding := range bindings {
		if binding.ScopeType != JSPluginScopeEndpoint || binding.Kind != "protocol" {
			continue
		}
		out[binding.ScopeID] = binding.PluginID
	}
	return out, nil
}

func (r JSPluginRepository) MarkBroken(ctx context.Context, pluginID, version string, selftest any) error {
	return r.SetVersionStatus(ctx, pluginID, version, JSPluginStatusBroken, selftest)
}

// EnableVersion makes one version the plugin's enabled one. granted are the
// automation permissions the admin approved for it. Bindings follow the enabled
// version, so uninstalling an older version later cannot cascade-delete them.
func (r JSPluginRepository) EnableVersion(ctx context.Context, pluginID, version string, selftest any, granted []string) error {
	grants, err := json.Marshal(granted)
	if err != nil {
		return err
	}
	if granted == nil {
		grants = []byte("[]")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := JSPluginRepository{db: tx}
		if err := repo.SetVersionStatus(ctx, pluginID, version, JSPluginStatusEnabled, selftest); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE js_plugin_versions SET granted_permissions = ?::jsonb WHERE plugin_id = ? AND version = ?`,
			string(grants), pluginID, version).Error; err != nil {
			return err
		}
		if err := repo.DisableOtherVersions(ctx, pluginID, version); err != nil {
			return err
		}
		return tx.Exec(`UPDATE js_plugin_bindings SET version = ? WHERE plugin_id = ? AND version <> ?`,
			version, pluginID, version).Error
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
