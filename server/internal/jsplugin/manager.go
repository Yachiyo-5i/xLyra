package jsplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

const quotaProbePluginPrefix = "plugin:"

// EnableOptions controls optional enable-time checks.
type EnableOptions struct {
	ConfirmUntrusted bool
}

// Manager loads uploaded plugins and swaps catalog generations.
type Manager struct {
	repo    store.JSPluginRepository
	catalog *Catalog
	mu      sync.Mutex
	gen     int64
}

func NewManager(db *store.Store, catalog *Catalog) *Manager {
	if catalog == nil {
		catalog = DefaultCatalog()
	}
	m := &Manager{
		repo:    store.NewJSPluginRepository(db.DB()),
		catalog: catalog,
		gen:     0,
	}
	catalog.breaker = NewBreaker(m.onBreakerTrip)
	return m
}

func (m *Manager) Catalog() *Catalog {
	return m.catalog
}

func (m *Manager) Breaker() *Breaker {
	if m == nil || m.catalog == nil {
		return nil
	}
	return m.catalog.breaker
}

func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	enabledRows, err := m.repo.ListEnabledVersions(ctx)
	if err != nil {
		return err
	}
	slugMap, err := m.repo.ProtocolSlugMap(ctx)
	if err != nil {
		return err
	}
	enabled := make([]EnabledPlugin, 0, len(enabledRows))
	for _, row := range enabledRows {
		pkg, err := ReadPackage(row.Package, true)
		if err != nil {
			return fmt.Errorf("%s@%s: %w", row.PluginID, row.Version, err)
		}
		plugin, err := CompilePackage(pkg)
		if err != nil {
			return store.EnabledPluginCompileError(row.PluginID, row.Version, err)
		}
		enabled = append(enabled, EnabledPlugin{Plugin: plugin})
	}
	m.gen++
	snap, err := BuildGeneration(ctx, m.gen, enabled, slugMap)
	if err != nil {
		return err
	}
	m.catalog.Replace(snap)
	return nil
}

func (m *Manager) Upload(ctx context.Context, adminID string, raw []byte) (store.JSPluginVersion, error) {
	pkg, err := ReadPackage(raw, true)
	if err != nil {
		return store.JSPluginVersion{}, &InvalidPackageError{Err: err}
	}
	plugin, err := CompilePackage(pkg)
	if err != nil {
		return store.JSPluginVersion{}, &InvalidPackageError{Err: err}
	}
	selftestErr := plugin.SelfTest(ctx)
	selftest := map[string]any{"ok": selftestErr == nil}
	if selftestErr != nil {
		selftest["error"] = selftestErr.Error()
	}
	existing, findErr := m.repo.GetVersion(ctx, pkg.Manifest.ID, pkg.Manifest.Version)
	if findErr == nil {
		existingPkg, readErr := ReadPackage(existing.Package, true)
		if readErr != nil {
			return store.JSPluginVersion{}, fmt.Errorf("%s@%s: %w", existing.PluginID, existing.Version, readErr)
		}
		if existingPkg.SHA256 == pkg.SHA256 {
			return existing, nil
		}
		return store.JSPluginVersion{}, ErrVersionContentMismatch
	} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return store.JSPluginVersion{}, findErr
	}
	manifestJSON, err := json.Marshal(pkg.Manifest)
	if err != nil {
		return store.JSPluginVersion{}, err
	}
	row := store.JSPluginVersion{
		PluginID:      pkg.Manifest.ID,
		Version:       pkg.Manifest.Version,
		Manifest:      store.JSON(manifestJSON),
		Package:       raw,
		PackageSHA256: pkg.SHA256,
		Signer:        pkg.Signer,
		Status:        store.JSPluginStatusVerified,
		SelfTest:      store.JSON(mustJSON(selftest)),
	}
	if adminID != "" {
		if id, err := uuid.Parse(adminID); err == nil {
			row.UploadedBy = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if err := m.repo.CreatePluginVersion(ctx, row, store.JSPluginSourceUploaded); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return store.JSPluginVersion{}, ErrVersionContentMismatch
		}
		return store.JSPluginVersion{}, err
	}
	return row, nil
}

func (m *Manager) Enable(ctx context.Context, pluginID, version string, opts EnableOptions) error {
	row, err := m.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		return err
	}
	trust, err := m.TrustStatus(ctx, row.Signer)
	if err != nil {
		return err
	}
	if trust != TrustTrusted && !opts.ConfirmUntrusted {
		return &ConfirmRequiredError{Trust: trust}
	}
	pkg, err := ReadPackage(row.Package, true)
	if err != nil {
		return err
	}
	if spec, ok := lookupKind(pkg.Manifest.Kind); ok && !spec.Connected {
		return &KindNotConnectedError{Kind: pkg.Manifest.Kind}
	}
	plugin, err := CompilePackage(pkg)
	if err != nil {
		return err
	}
	if err := plugin.SelfTest(ctx); err != nil {
		_ = m.repo.SetVersionStatus(ctx, pluginID, version, store.JSPluginStatusBroken, map[string]any{"error": err.Error()})
		return err
	}
	if err := m.repo.EnableVersion(ctx, pluginID, version, map[string]any{"ok": true}); err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) Disable(ctx context.Context, pluginID string) error {
	versions, err := m.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return err
	}
	for _, row := range versions {
		if row.Status == store.JSPluginStatusEnabled {
			if err := m.repo.SetVersionStatus(ctx, pluginID, row.Version, store.JSPluginStatusDisabled, nil); err != nil {
				return err
			}
		}
	}
	return m.Reload(ctx)
}

func (m *Manager) DeleteVersion(ctx context.Context, pluginID, version string) error {
	row, err := m.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		return err
	}
	if row.Status == store.JSPluginStatusEnabled {
		return ErrVersionEnabled
	}
	if err := m.repo.DeleteVersion(ctx, pluginID, version); err != nil {
		return err
	}
	return nil
}

func (m *Manager) BindSiteQuotaProbe(ctx context.Context, pluginID, version, siteID string) error {
	siteID = strings.TrimSpace(siteID)
	if siteID == "" {
		return fmt.Errorf("site_id is required")
	}
	row, err := m.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		return err
	}
	if row.Status != store.JSPluginStatusEnabled {
		return fmt.Errorf("plugin version must be enabled before binding")
	}
	pkg, err := ReadPackage(row.Package, true)
	if err != nil {
		return err
	}
	if pkg.Manifest.Kind != KindQuotaProbe {
		return fmt.Errorf("only quota_probe plugins can bind to sites")
	}
	return m.repo.UpsertBinding(ctx, store.JSPluginBinding{
		PluginID:   pluginID,
		Version:    version,
		TargetKind: store.JSPluginBindingQuotaProbe,
		TargetID:   siteID,
	})
}

func (m *Manager) SyncSiteQuotaProbeFromConfig(ctx context.Context, siteID string, quotaProbe string) error {
	siteID = strings.TrimSpace(siteID)
	if siteID == "" {
		return fmt.Errorf("site_id is required")
	}
	probe := strings.TrimSpace(quotaProbe)
	if !strings.HasPrefix(probe, quotaProbePluginPrefix) {
		return m.repo.DeleteBinding(ctx, store.JSPluginBindingQuotaProbe, siteID)
	}
	pluginID := strings.TrimSpace(strings.TrimPrefix(probe, quotaProbePluginPrefix))
	if pluginID == "" {
		return m.repo.DeleteBinding(ctx, store.JSPluginBindingQuotaProbe, siteID)
	}
	versions, err := m.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return err
	}
	var enabledVersion string
	for _, row := range versions {
		if row.Status == store.JSPluginStatusEnabled {
			enabledVersion = row.Version
			break
		}
	}
	if enabledVersion == "" {
		return m.repo.DeleteBinding(ctx, store.JSPluginBindingQuotaProbe, siteID)
	}
	return m.repo.UpsertBinding(ctx, store.JSPluginBinding{
		PluginID:   pluginID,
		Version:    enabledVersion,
		TargetKind: store.JSPluginBindingQuotaProbe,
		TargetID:   siteID,
	})
}

func (m *Manager) TryVersion(ctx context.Context, pluginID, version string) (map[string]any, error) {
	row, err := m.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		return nil, err
	}
	pkg, err := ReadPackage(row.Package, true)
	if err != nil {
		return nil, err
	}
	plugin, err := CompilePackage(pkg)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	err = plugin.SelfTest(ctx)
	out := map[string]any{
		"plugin_id": pluginID,
		"version":   version,
		"ok":        err == nil,
		"duration_ms": time.Since(started).Milliseconds(),
	}
	if err != nil {
		out["error"] = err.Error()
	}
	return out, nil
}

func (m *Manager) Metrics(pluginID, version string) PluginMetricsWindow {
	if m == nil || m.catalog == nil || m.catalog.breaker == nil {
		return PluginMetricsWindow{}
	}
	return m.catalog.breaker.Metrics(pluginID, version)
}

func (m *Manager) BindProtocolSlug(ctx context.Context, pluginID, version, slug string) error {
	if slug == "" {
		return fmt.Errorf("slug is required")
	}
	if err := m.repo.UpsertBinding(ctx, store.JSPluginBinding{
		PluginID:   pluginID,
		Version:    version,
		TargetKind: store.JSPluginBindingProtocolSlug,
		TargetID:   slug,
	}); err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) onBreakerTrip(pluginID, version, reason string) {
	slog.Warn("js plugin breaker tripped", "plugin_id", pluginID, "version", version, "reason", reason)
	ctx := context.Background()
	_ = m.repo.SetVersionStatus(ctx, pluginID, version, store.JSPluginStatusDisabled, map[string]any{"breaker": reason})
	_ = m.Reload(ctx)
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}
