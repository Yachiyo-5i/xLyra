package jsplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"
)

const (
	maxSiteProviderHosts     = 16
	maxSiteProviderIconBytes = 64 << 10
)

// SiteTypeIDPattern is <plugin id>/<name>. The plugin id is 3–64 characters
// and must also match pluginIDPattern; the name is a single path segment.
var SiteTypeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,62}[a-z0-9]/[a-z][a-z0-9_]{0,31}$`)

var brandColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

var (
	siteProviderProtocolKinds = map[string]struct{}{
		"openai_chat":        {},
		"openai_responses":   {},
		"anthropic_messages": {},
		"google_gemini":      {},
	}
	siteProviderEndpointTypes = map[string]struct{}{
		"openai":             {},
		"openai-response":    {},
		"anthropic-messages": {},
		"google-gemini":      {},
	}
)

// ValidateSiteTypeID reports whether id is a plugin site type id.
// The part before the slash must satisfy pluginIDPattern and be 3–64 characters.
func ValidateSiteTypeID(id string) error {
	if !SiteTypeIDPattern.MatchString(id) {
		return fmt.Errorf("invalid site type id %q", id)
	}
	pluginID, _, _ := strings.Cut(id, "/")
	if !pluginIDPattern.MatchString(pluginID) || len(pluginID) < 3 || len(pluginID) > 64 {
		return fmt.Errorf("invalid site type id %q", id)
	}
	return nil
}

// SiteProviderSection is the site_provider manifest block. One package declares
// one site type. Account-pool fields apply when credential.model is account_pool.
type SiteProviderSection struct {
	SiteType      string                 `json:"siteType"`
	Display       SiteProviderDisplay    `json:"display"`
	Hosts         []string               `json:"hosts"`
	Credential    SiteProviderCredential `json:"credential"`
	Protocol      SiteProviderProtocol   `json:"protocol"`
	Models        SiteProviderModels     `json:"models,omitzero"`
	Request       SiteProviderRequest    `json:"request,omitempty"`
	Regions       []SiteProviderRegion   `json:"regions,omitempty"`
	DefaultRegion string                 `json:"defaultRegion,omitempty"`
	Login         *SiteProviderLogin     `json:"login,omitempty"`
	Refresh       *SiteProviderRefresh   `json:"refresh,omitempty"`
	Import        *SiteProviderImport    `json:"import,omitempty"`
	Selection     string                 `json:"selection,omitempty"`
	Quota         *SiteProviderQuota     `json:"quota,omitempty"`
	UI            *SiteProviderUI        `json:"ui,omitempty"`
	// modelsSet is true when the JSON object contained "models", including {}.
	modelsSet bool `json:"-"`
}

// SiteProviderRequest is the optional upstream header template.
// Headers maps a header name to a template string or a conditional object.
// StripDownstreamHeaders keeps only those names plus Content-Type and Accept.
type SiteProviderRequest struct {
	Headers                map[string]HeaderSpec `json:"headers,omitempty"`
	StripDownstreamHeaders bool                  `json:"stripDownstreamHeaders,omitempty"`
	UpstreamStreamOnly     bool                  `json:"upstreamStreamOnly,omitempty"`
	BaseURLFrom            string                `json:"baseURLFrom,omitempty"`
}

// SiteProviderDisplay is the copy and mark shown for the site type.
type SiteProviderDisplay struct {
	Name       LocalizedText `json:"name"`
	Subtitle   LocalizedText `json:"subtitle,omitempty"`
	Icon       string        `json:"icon,omitempty"`
	BrandColor string        `json:"brandColor,omitempty"`
}

// SiteProviderCredential is how a site of this type authenticates.
// Fields, token names, identity, and display apply only when Model is account_pool.
type SiteProviderCredential struct {
	Model             string              `json:"model"`
	Fields            []SiteProviderField `json:"fields,omitempty"`
	AccessTokenField  string              `json:"accessTokenField,omitempty"`
	RefreshTokenField string              `json:"refreshTokenField,omitempty"`
	ExpiresAtField    string              `json:"expiresAtField,omitempty"`
	ExpiresFrom       string              `json:"expiresFrom,omitempty"`
	Identity          CredentialSources   `json:"identity,omitempty"`
	Display           CredentialSources   `json:"display,omitempty"`
}

// SiteProviderProtocol is the upstream protocol this site type speaks.
// RequestParams is kept as written; protocol-spec checks happen later.
type SiteProviderProtocol struct {
	Kind                 string          `json:"kind"`
	Path                 string          `json:"path"`
	OfficialBaseURL      string          `json:"officialBaseURL"`
	DefaultEndpointTypes []string        `json:"defaultEndpointTypes"`
	RequestParams        json.RawMessage `json:"requestParams,omitempty"`
}

// SiteProviderModels is the model-list request.
// API-key sites use Path and Auth. An empty path means /v1/models and an empty
// auth means bearer. Account-pool sites may set PerAccount and nothing else.
type SiteProviderModels struct {
	Path       string `json:"path,omitempty"`
	Auth       string `json:"auth,omitempty"`
	PerAccount bool   `json:"perAccount,omitempty"`
}

// siteProviderOptionalHooks are exports a site_provider module may include.
// None of them is required. listModels, check, probe, classify, and
// rewritePayload are called for the site type.
var siteProviderOptionalHooks = []string{"probe", "listModels", "check", "classify", "rewritePayload"}

func validateSiteProvider(manifest Manifest, files map[string][]byte) error {
	if err := otherKindSectionPresent(manifest); err != nil {
		return err
	}
	section := manifest.SiteProvider
	if err := ValidateSiteTypeID(section.SiteType); err != nil {
		return fmt.Errorf("siteProvider.siteType: %w", err)
	}
	pluginID, _, _ := strings.Cut(section.SiteType, "/")
	if pluginID != manifest.ID {
		return fmt.Errorf("siteProvider.siteType %q must start with %q", section.SiteType, manifest.ID+"/")
	}
	if err := section.Display.Name.validate(); err != nil {
		return fmt.Errorf("siteProvider.display.name: %w", err)
	}
	if !localizedHasText(section.Display.Name) {
		return fmt.Errorf("siteProvider.display.name is required")
	}
	if err := section.Display.Subtitle.validate(); err != nil {
		return fmt.Errorf("siteProvider.display.subtitle: %w", err)
	}
	if section.Display.BrandColor != "" && !brandColorPattern.MatchString(section.Display.BrandColor) {
		return fmt.Errorf("siteProvider.display.brandColor %q must be #RRGGBB", section.Display.BrandColor)
	}
	if err := validateSiteProviderIcon(section.Display.Icon, files); err != nil {
		return err
	}
	if err := validateSiteProviderHosts(section.Hosts); err != nil {
		return err
	}
	if err := rejectCredentialModelExtras(section); err != nil {
		return err
	}
	if err := validateSiteProviderProtocol(section.Protocol, section.Hosts); err != nil {
		return err
	}
	if section.Credential.Model == "api_key" {
		if err := validateSiteProviderModels(section.Models); err != nil {
			return err
		}
	}
	if err := validateSiteProviderQuota(section.Quota); err != nil {
		return err
	}
	if section.Credential.Model == "account_pool" {
		if err := validateAccountPool(section); err != nil {
			return err
		}
	}
	if err := ValidateSiteProviderRequest(section.Request, siteProviderHeaderFields(section), regionTemplateKeys(section.Regions)); err != nil {
		return err
	}
	if err := validateRequestBaseURLFrom(section); err != nil {
		return err
	}
	return nil
}

func otherKindSectionPresent(manifest Manifest) error {
	if manifest.QuotaProbe != (QuotaProbeSection{}) {
		return fmt.Errorf("quotaProbe is not allowed for site_provider")
	}
	if manifest.Site != (SiteSection{}) {
		return fmt.Errorf("site is not allowed for site_provider")
	}
	if manifest.Protocol != (ProtocolSection{}) {
		return fmt.Errorf("protocol is not allowed for site_provider")
	}
	form := manifest.Automation.Form
	if len(manifest.Automation.Subscribes) > 0 || len(manifest.Automation.Permissions) > 0 || len(manifest.Automation.Inputs) > 0 ||
		manifest.Automation.Schedule != (AutomationSchedule{}) ||
		len(form.Title) > 0 || len(form.Description) > 0 || len(form.AddLabel) > 0 || len(form.EmptyText) > 0 {
		return fmt.Errorf("automation is not allowed for site_provider")
	}
	return nil
}

func localizedHasText(text LocalizedText) bool {
	for _, value := range text {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func validateSiteProviderHosts(hosts []string) error {
	if len(hosts) < 1 || len(hosts) > maxSiteProviderHosts {
		return fmt.Errorf("siteProvider.hosts must list 1 to %d hostnames", maxSiteProviderHosts)
	}
	seen := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		if err := validSiteProviderHost(host); err != nil {
			return fmt.Errorf("siteProvider.hosts: %w", err)
		}
		if _, ok := seen[host]; ok {
			return fmt.Errorf("siteProvider.hosts: duplicate host %q", host)
		}
		seen[host] = struct{}{}
	}
	return nil
}

func validSiteProviderHost(host string) error {
	if net.ParseIP(host) != nil {
		return fmt.Errorf("host %q must not be an IP address", host)
	}
	if host == "" || strings.Contains(host, "://") || strings.ContainsAny(host, ":/?#@\\ ") {
		return fmt.Errorf("host %q must be a DNS hostname without a scheme, port, or path", host)
	}
	if len(host) > 253 || !strings.Contains(host, ".") {
		return fmt.Errorf("host %q must be a DNS hostname", host)
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("host %q must be a DNS hostname", host)
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("host %q must be a lowercase DNS hostname", host)
			}
		}
	}
	return nil
}

func validateSiteProviderProtocol(section SiteProviderProtocol, hosts []string) error {
	if _, ok := siteProviderProtocolKinds[section.Kind]; !ok {
		return fmt.Errorf("siteProvider.protocol.kind %q is not supported", section.Kind)
	}
	if err := validatePluginPath(section.Path); err != nil {
		return fmt.Errorf("siteProvider.protocol.path: %w", err)
	}
	if err := validateOfficialBaseURL(section.OfficialBaseURL, hosts); err != nil {
		return err
	}
	if len(section.DefaultEndpointTypes) == 0 {
		return fmt.Errorf("siteProvider.protocol.defaultEndpointTypes is required")
	}
	for _, value := range section.DefaultEndpointTypes {
		if _, ok := siteProviderEndpointTypes[value]; !ok {
			return fmt.Errorf("siteProvider.protocol.defaultEndpointTypes: %q is not supported", value)
		}
	}
	return nil
}

func validateOfficialBaseURL(raw string, hosts []string) error {
	return validateHTTPSURL(raw, hosts, "siteProvider.protocol.officialBaseURL")
}

func validateHTTPSURL(raw string, hosts []string, what string) error {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return fmt.Errorf("%s %q must be an https URL with a host", what, raw)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s %q must not contain userinfo", what, raw)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return fmt.Errorf("%s %q must not contain a query", what, raw)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("%s %q must not contain a fragment", what, raw)
	}
	host := strings.ToLower(parsed.Hostname())
	for _, item := range hosts {
		if item == host {
			return nil
		}
	}
	return fmt.Errorf("%s host %q is not in hosts", what, host)
}

func validateSiteProviderModels(section SiteProviderModels) error {
	if section.PerAccount {
		return fmt.Errorf("siteProvider.models.perAccount is not allowed for credential.model %q", "api_key")
	}
	if section.Path != "" {
		if err := validatePluginPath(section.Path); err != nil {
			return fmt.Errorf("siteProvider.models.path: %w", err)
		}
	}
	if err := validateSiteProviderAuth(section.Auth); err != nil {
		return err
	}
	return nil
}

func validateSiteProviderAuth(auth string) error {
	if auth == "" || auth == "bearer" {
		return nil
	}
	name, ok := strings.CutPrefix(auth, "header:")
	if !ok || !validHeaderToken(name) {
		return fmt.Errorf("siteProvider.models.auth %q must be bearer or header:<Name>", auth)
	}
	return nil
}

func validHeaderToken(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return false
			}
		}
	}
	return true
}

func validateSiteProviderIcon(icon string, files map[string][]byte) error {
	if icon == "" {
		return nil
	}
	cleaned, err := siteProviderIconPath(icon)
	if err != nil {
		return err
	}
	if files == nil {
		return nil
	}
	data, ok := files[cleaned]
	if !ok {
		return fmt.Errorf("siteProvider.display.icon %q is not in the package", cleaned)
	}
	if len(data) > maxSiteProviderIconBytes {
		return fmt.Errorf("siteProvider.display.icon exceeds %d bytes", maxSiteProviderIconBytes)
	}
	if !siteProviderIconMagic(path.Ext(cleaned), data) {
		return fmt.Errorf("siteProvider.display.icon is not a valid %s file", path.Ext(cleaned))
	}
	return nil
}

func siteProviderIconPath(icon string) (string, error) {
	if strings.Contains(icon, `\`) || strings.Contains(icon, ":") || strings.HasPrefix(icon, "/") {
		return "", fmt.Errorf("siteProvider.display.icon %q must be a relative path inside the package", icon)
	}
	cleaned := path.Clean(icon)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("siteProvider.display.icon %q must be a relative path inside the package", icon)
	}
	switch path.Ext(cleaned) {
	case ".png", ".webp":
		return cleaned, nil
	default:
		return "", fmt.Errorf("siteProvider.display.icon must be a .png or .webp file")
	}
}

func siteProviderIconMagic(ext string, data []byte) bool {
	switch ext {
	case ".png":
		return bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	case ".webp":
		return len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	default:
		return false
	}
}
