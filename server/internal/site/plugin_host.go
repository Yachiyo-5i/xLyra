package site

import (
	"fmt"
	"net/url"
	"strings"
)

// HostNotAllowedError is returned when a plugin site type cannot call a host.
// Reason is set when the type is disabled or missing from the enabled snapshot,
// so there is no allow-list to apply. Host is the URL hostname without a port.
type HostNotAllowedError struct {
	SiteType string
	Host     string
	Reason   string
}

func (e *HostNotAllowedError) Error() string {
	if e == nil {
		return "host is not allowed"
	}
	if e.Reason != "" {
		return fmt.Sprintf("host %q is not allowed for plugin site type %q: %s", e.Host, e.SiteType, e.Reason)
	}
	return fmt.Sprintf("host %q is not allowed for plugin site type %q", e.Host, e.SiteType)
}

// PluginSiteTypeHosts returns a copy of the allow-list for an enabled plugin
// site type. Built-in types and disabled or unknown plugin types return false.
func PluginSiteTypeHosts(siteType string) ([]string, bool) {
	item, ok := LookupPluginSiteType(siteType)
	if !ok || !item.Enabled {
		return nil, false
	}
	return append([]string(nil), item.Hosts...), true
}

// CheckPluginHost reports whether rawURL's hostname is allowed for siteType.
// Built-in site types (no slash) are always allowed. An enabled plugin site
// type must match one allow-list entry exactly: lowercase, port stripped, no
// subdomain or wildcard match. A slash type that is disabled or not in the
// enabled snapshot is rejected, because there is no allow-list to apply.
func CheckPluginHost(siteType, rawURL string) error {
	key := strings.ToLower(strings.TrimSpace(siteType))
	if !strings.Contains(key, "/") {
		return nil
	}
	host := pluginRequestHost(rawURL)
	item, ok := LookupPluginSiteType(key)
	if !ok || !item.Enabled {
		return &HostNotAllowedError{SiteType: key, Host: host, Reason: "provider unavailable"}
	}
	if host != "" {
		for _, allowed := range item.Hosts {
			if strings.ToLower(allowed) == host {
				return nil
			}
		}
	}
	return &HostNotAllowedError{SiteType: key, Host: host}
}

func pluginRequestHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
