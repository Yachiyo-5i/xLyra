package jsplugin

import (
	"context"
	"errors"
	"strings"

	"xlyra/server/internal/store"
)

// ErrPluginIconNotFound means this plugin has no site_provider icon to serve.
// The plugin may be missing, not a site_provider, or its display.icon may be
// absent or unreadable.
var ErrPluginIconNotFound = errors.New("plugin icon not found")

// SiteProviderIcon is the bitmap declared by siteProvider.display.icon.
type SiteProviderIcon struct {
	Bytes       []byte
	ContentType string
	SHA256      string
}

// SiteProviderIcon loads the icon for pluginID. An enabled version wins; otherwise
// the latest uploaded version is used. A disabled plugin still has an icon.
// A deleted plugin, a non-site_provider, a package with no icon, or a missing
// icon file returns ErrPluginIconNotFound.
func (m *Manager) SiteProviderIcon(ctx context.Context, pluginID string) (SiteProviderIcon, error) {
	if m == nil {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	versions, err := m.repo.ListVersions(ctx, pluginID)
	if err != nil {
		return SiteProviderIcon{}, err
	}
	return siteProviderIconFromVersions(versions)
}

func siteProviderIconFromVersions(versions []store.JSPluginVersion) (SiteProviderIcon, error) {
	row, ok := selectSiteProviderIconVersion(versions)
	if !ok {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	pkg, err := ReadPackage(row.Package, true)
	if err != nil {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	if pkg.Manifest.Kind != KindSiteProvider {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	icon := strings.TrimSpace(pkg.Manifest.SiteProvider.Display.Icon)
	if icon == "" {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	cleaned, err := siteProviderIconPath(icon)
	if err != nil {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	data, err := packageFile(row.Package, cleaned)
	if err != nil {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	contentType := siteProviderIconContentType(data)
	if contentType == "" || len(data) > maxSiteProviderIconBytes {
		return SiteProviderIcon{}, ErrPluginIconNotFound
	}
	return SiteProviderIcon{Bytes: data, ContentType: contentType, SHA256: pkg.SHA256}, nil
}

// selectSiteProviderIconVersion prefers an enabled version. With none, it uses
// the latest upload (CreatedAt). Equal timestamps keep the earlier row.
func selectSiteProviderIconVersion(versions []store.JSPluginVersion) (store.JSPluginVersion, bool) {
	var enabled store.JSPluginVersion
	var latest store.JSPluginVersion
	var hasEnabled bool
	var hasLatest bool
	for _, row := range versions {
		if row.Status == store.JSPluginStatusEnabled && (!hasEnabled || row.CreatedAt.After(enabled.CreatedAt)) {
			enabled = row
			hasEnabled = true
		}
		if !hasLatest || row.CreatedAt.After(latest.CreatedAt) {
			latest = row
			hasLatest = true
		}
	}
	if hasEnabled {
		return enabled, true
	}
	return latest, hasLatest
}

func siteProviderIconContentType(data []byte) string {
	switch {
	case siteProviderIconMagic(".png", data):
		return "image/png"
	case siteProviderIconMagic(".webp", data):
		return "image/webp"
	default:
		return ""
	}
}
