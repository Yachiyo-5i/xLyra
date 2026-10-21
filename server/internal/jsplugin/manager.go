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

// EnableOptions controls optional enable-time checks.
type EnableOptions struct {
	ConfirmUntrusted bool
	// GrantPermissions are the automation actions the admin approves. Enabling
	// an automation version needs every permission its manifest declares.
	GrantPermissions []string
	// ConfirmedHosts is the host list an admin confirms for a site_provider.
	// It must equal the manifest hosts as a set. Every enable asks again.
	ConfirmedHosts []string
}

// ErrSiteProviderUntrusted is returned when a site_provider package is unsigned
// or signed by a key that is not in the trusted key store. ConfirmUntrusted
// does not bypass it.
var ErrSiteProviderUntrusted = errors.New("site_provider plugins must be signed by a trusted key")

// SiteProviderUntrustedError is ErrSiteProviderUntrusted plus the trust state
// the enable saw.
type SiteProviderUntrustedError struct {
	Trust TrustStatus
}

func (e *SiteProviderUntrustedError) Error() string {
	return ErrSiteProviderUntrusted.Error()
}

func (e *SiteProviderUntrustedError) Unwrap() error {
	return ErrSiteProviderUntrusted
}

// HostsConfirmationRequiredError means a site_provider enable did not confirm
// the manifest host list. Hosts is that full list, in manifest order.
type HostsConfirmationRequiredError struct {
	Hosts []string
}

func (e *HostsConfirmationRequiredError) Error() string {
	return "this plugin needs its host list confirmed: " + strings.Join(e.Hosts, ", ")
}

// PluginInUseError means a site_provider still has sites of its site type, so
// the plugin or its last version cannot be deleted. Disable is allowed.
type PluginInUseError struct {
	PluginID string
	SiteType string
	Count    int
}

func (e *PluginInUseError) Error() string {
	if e == nil {
		return "plugin site type is still used by sites"
	}
	return fmt.Sprintf("plugin %q site type %q is still used by %d site(s)", e.PluginID, e.SiteType, e.Count)
}

// SiteTypeChangeError means a new site_provider version changes the site type
// id while sites still use the previous id.
type SiteTypeChangeError struct {
	PluginID string
	From     string
	To       string
	Count    int
}

func (e *SiteTypeChangeError) Error() string {
	if e == nil {
		return "plugin site type cannot change while sites use it"
	}
	return fmt.Sprintf("plugin %q cannot change site type from %q to %q while %d site(s) use it", e.PluginID, e.From, e.To, e.Count)
}

// PermissionsRequiredError means a version declares automation permissions the
// admin has not granted yet.
type PermissionsRequiredError struct {
	Required []string
}

func (e *PermissionsRequiredError) Error() string {
	return "this plugin needs permissions that have not been granted: " + strings.Join(e.Required, ", ")
}

// Manager loads uploaded plugins and swaps catalog generations.
type Manager struct {
	db              *store.Store
	repo            store.JSPluginRepository
	catalog         *Catalog
	mu              sync.Mutex
	reloadMu        sync.Mutex
	catalogListener func(*Registry)
	gen             int64
	seenGeneration  int64
	syncStopped     chan struct{}
	wakeup          chan struct{}
}

func NewManager(db *store.Store, catalog *Catalog) *Manager {
	if catalog == nil {
		catalog = DefaultCatalog()
	}
	m := &Manager{
		db:      db,
		repo:    store.NewJSPluginRepository(db.DB()),
		catalog: catalog,
		gen:     0,
		wakeup:  make(chan struct{}, 1),
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

// SetCatalogListener sets the function called with the new registry after each
// successful Reload, including a reload started by catalog sync. The listener
// runs synchronously and without the manager lock, so it can call back into
// the manager. A nil function clears it. A panic in the listener is logged and
// does not fail Reload. The listener must not call Reload.
func (m *Manager) SetCatalogListener(fn func(*Registry)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.catalogListener = fn
	m.mu.Unlock()
}

func (m *Manager) Reload(ctx context.Context) error {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()

	m.mu.Lock()
	snap, err := m.reloadLocked(ctx)
	fn := m.catalogListener
	var registry *Registry
	if err == nil && m.catalog != nil && snap != nil && snap.Registry != nil {
		registry = snap.Registry
	}
	m.mu.Unlock()
	if err != nil {
		return err
	}
	callCatalogListener(fn, registry)
	return nil
}

func (m *Manager) reloadLocked(ctx context.Context) (*Snapshot, error) {
	enabledRows, err := m.repo.ListEnabledVersions(ctx)
	if err != nil {
		return nil, err
	}
	slugMap, err := m.repo.ProtocolSlugMap(ctx)
	if err != nil {
		return nil, err
	}
	enabled := make([]EnabledPlugin, 0, len(enabledRows))
	for _, row := range enabledRows {
		pkg, err := ReadPackage(row.Package, true)
		if err != nil {
			return nil, fmt.Errorf("%s@%s: %w", row.PluginID, row.Version, err)
		}
		plugin, err := CompilePackage(pkg)
		if err != nil {
			return nil, store.EnabledPluginCompileError(row.PluginID, row.Version, err)
		}
		enabled = append(enabled, EnabledPlugin{Plugin: plugin})
	}
	m.gen++
	snap, err := BuildGeneration(ctx, m.gen, enabled, slugMap)
	if err != nil {
		return nil, err
	}
	m.catalog.Replace(snap)
	return snap, nil
}

func callCatalogListener(fn func(*Registry), registry *Registry) {
	if fn == nil || registry == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("js plugin catalog listener panicked", "error", recovered)
		}
	}()
	fn(registry)
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
	pkg, err := ReadPackage(row.Package, true)
	if err != nil {
		return err
	}
	if pkg.Manifest.Kind == KindSiteProvider {
		if trust != TrustTrusted {
			return &SiteProviderUntrustedError{Trust: trust}
		}
	} else if trust != TrustTrusted && !opts.ConfirmUntrusted {
		return &ConfirmRequiredError{Trust: trust}
	}
	if spec, ok := lookupKind(pkg.Manifest.Kind); ok && !spec.Connected {
		return &KindNotConnectedError{Kind: pkg.Manifest.Kind}
	}
	var granted []string
	if pkg.Manifest.Kind == KindSiteProvider {
		if !sameStringSet(opts.ConfirmedHosts, pkg.Manifest.SiteProvider.Hosts) {
			return &HostsConfirmationRequiredError{Hosts: append([]string(nil), pkg.Manifest.SiteProvider.Hosts...)}
		}
		granted = make([]string, 0, len(pkg.Manifest.SiteProvider.Hosts))
		for _, host := range pkg.Manifest.SiteProvider.Hosts {
			granted = append(granted, "host:"+host)
		}
		if err := m.ensureSiteTypeStable(ctx, pluginID, version, pkg.Manifest.SiteProvider.SiteType); err != nil {
			return err
		}
	}
	if pkg.Manifest.Kind == KindAutomation {
		var missing []string
		for _, permission := range pkg.Manifest.Automation.Permissions {
			if contains(opts.GrantPermissions, permission) {
				granted = append(granted, permission)
			} else {
				missing = append(missing, permission)
			}
		}
		if len(missing) > 0 {
			return &PermissionsRequiredError{Required: pkg.Manifest.Automation.Permissions}
		}
	}
	plugin, err := CompilePackage(pkg)
	if err != nil {
		return err
	}
	if err := plugin.SelfTest(ctx); err != nil {
		_ = m.repo.SetVersionStatus(ctx, pluginID, version, store.JSPluginStatusBroken, map[string]any{"error": err.Error()})
		return err
	}
	if err := m.repo.EnableVersion(ctx, pluginID, version, map[string]any{"ok": true}, granted); err != nil {
		return err
	}
	if pkg.Manifest.Kind == KindAutomation {
		// The new version may subscribe to different events.
		if err := m.repo.SetAutomationEvents(ctx, pluginID, pkg.Manifest.Automation.Subscribes); err != nil {
			return err
		}
	}
	return m.publishCatalog(ctx)
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
	return m.publishCatalog(ctx)
}

func (m *Manager) DeleteVersion(ctx context.Context, pluginID, version string) error {
	row, err := m.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		return err
	}
	if row.Status == store.JSPluginStatusEnabled {
		return ErrVersionEnabled
	}
	versions, err := m.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return err
	}
	if len(versions) <= 1 {
		if err := m.ensureSiteProviderUnused(ctx, pluginID); err != nil {
			return err
		}
	}
	if err := m.repo.DeleteVersion(ctx, pluginID, version); err != nil {
		return err
	}
	return m.publishCatalog(ctx)
}

// DeletePlugin uninstalls every version of a plugin, with its bindings and logs.
// An enabled plugin must be disabled first.
func (m *Manager) DeletePlugin(ctx context.Context, pluginID string) error {
	if err := m.ensureSiteProviderUnused(ctx, pluginID); err != nil {
		return err
	}
	err := m.repo.DeletePlugin(ctx, pluginID)
	if errors.Is(err, store.ErrJSPluginEnabled) {
		return ErrVersionEnabled
	}
	if err != nil {
		return err
	}
	return m.publishCatalog(ctx)
}

// CheckSiteBinding checks that an enabled plugin version may be bound to a
// site, and returns its kind. Which plugin serves which site is kept in the
// site's gateway config, which routing and the site pages already read, so this
// only validates; there is no second copy to keep in step.
func (m *Manager) CheckSiteBinding(ctx context.Context, pluginID, version string) (string, error) {
	row, err := m.repo.GetVersion(ctx, pluginID, version)
	if err != nil {
		return "", err
	}
	if row.Status != store.JSPluginStatusEnabled {
		return "", fmt.Errorf("plugin version must be enabled before binding")
	}
	pkg, err := ReadPackage(row.Package, true)
	if err != nil {
		return "", err
	}
	kind := pkg.Manifest.Kind
	if kind == KindSiteProvider {
		return "", fmt.Errorf("kind %q does not support per-site binding; it takes effect automatically for its site type", kind)
	}
	if !SiteBound(kind) {
		return "", fmt.Errorf("kind %q is not bound to a site", kind)
	}
	return kind, nil
}

func (m *Manager) ensureSiteProviderUnused(ctx context.Context, pluginID string) error {
	types, err := m.siteProviderSiteTypes(ctx, pluginID)
	if err != nil {
		return err
	}
	for _, siteType := range types {
		count, err := m.countSitesByType(ctx, siteType)
		if err != nil {
			return err
		}
		if count > 0 {
			return &PluginInUseError{PluginID: pluginID, SiteType: siteType, Count: count}
		}
	}
	return nil
}

func (m *Manager) ensureSiteTypeStable(ctx context.Context, pluginID, version, nextType string) error {
	next := pluginSiteTypeKey(nextType)
	versions, err := m.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, row := range versions {
		if row.Version == version {
			continue
		}
		old, ok, err := manifestSiteProviderType(row.Manifest)
		if err != nil {
			return err
		}
		if !ok || old == next {
			continue
		}
		if _, dup := seen[old]; dup {
			continue
		}
		seen[old] = struct{}{}
		count, err := m.countSitesByType(ctx, old)
		if err != nil {
			return err
		}
		if count > 0 {
			return &SiteTypeChangeError{PluginID: pluginID, From: old, To: next, Count: count}
		}
	}
	return nil
}

func (m *Manager) siteProviderSiteTypes(ctx context.Context, pluginID string) ([]string, error) {
	versions, err := m.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	types := make([]string, 0)
	for _, row := range versions {
		siteType, ok, err := manifestSiteProviderType(row.Manifest)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if _, dup := seen[siteType]; dup {
			continue
		}
		seen[siteType] = struct{}{}
		types = append(types, siteType)
	}
	return types, nil
}

func (m *Manager) countSitesByType(ctx context.Context, siteType string) (int, error) {
	if m == nil || m.db == nil {
		return 0, fmt.Errorf("site repository is not available")
	}
	return store.NewSiteRepository(m.db.DB()).CountBySiteType(ctx, siteType)
}

func manifestSiteProviderType(raw store.JSON) (string, bool, error) {
	if len(raw) == 0 {
		return "", false, nil
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", false, fmt.Errorf("read plugin manifest: %w", err)
	}
	if manifest.Kind != KindSiteProvider {
		return "", false, nil
	}
	siteType := pluginSiteTypeKey(manifest.SiteProvider.SiteType)
	if siteType == "" {
		return "", false, nil
	}
	return siteType, true, nil
}

func pluginSiteTypeKey(siteType string) string {
	return strings.ToLower(strings.TrimSpace(siteType))
}

func sameStringSet(a, b []string) bool {
	left := make(map[string]struct{}, len(a))
	for _, item := range a {
		left[item] = struct{}{}
	}
	right := make(map[string]struct{}, len(b))
	for _, item := range b {
		right[item] = struct{}{}
	}
	if len(left) != len(right) {
		return false
	}
	for item := range left {
		if _, ok := right[item]; !ok {
			return false
		}
	}
	return true
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
		// A fixture run takes well under a millisecond, so report microseconds.
		"duration_us": time.Since(started).Microseconds(),
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
		PluginID:  pluginID,
		Version:   version,
		Kind:      KindProtocol,
		ScopeType: store.JSPluginScopeEndpoint,
		ScopeID:   slug,
	}); err != nil {
		return err
	}
	return m.publishCatalog(ctx)
}

func (m *Manager) onBreakerTrip(pluginID, version, reason string) {
	slog.Warn("js plugin breaker tripped", "plugin_id", pluginID, "version", version, "reason", reason)
	ctx := context.Background()
	if err := m.repo.SetVersionStatus(ctx, pluginID, version, store.JSPluginStatusDisabled, map[string]any{"breaker": reason}); err != nil {
		slog.Warn("js plugin breaker disable failed", "plugin_id", pluginID, "version", version, "error", err)
		_ = m.Reload(ctx)
		return
	}
	if err := m.publishCatalog(ctx); err != nil {
		slog.Warn("js plugin catalog publish failed", "plugin_id", pluginID, "version", version, "error", err)
	}
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}
