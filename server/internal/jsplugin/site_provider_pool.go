package jsplugin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	maxCredentialFields       = 16
	maxAccountPoolRegions     = 8
	maxRegionListItems        = 8
	maxImportAliases          = 8
	maxImportContainers       = 8
	maxImportValueMapFields   = 8
	maxImportValueMapEntries  = 8
	maxImportValueBytes       = 64
	maxRegionAttrs            = 2
	maxRegionAttrBytes        = 256
	maxMillisThreshold        = 1_000_000_000_000_000
	maxRowPills               = 4
	maxPillLabels             = 8
	maxPillLabelKey           = 64
	minRefreshLeadSeconds     = 30
	maxRefreshLeadSeconds     = 3600
	defaultRefreshLeadSeconds = 300
	defaultAccessTokenField   = "access_token"
	defaultAccountSelection   = "ordered"
	defaultAccountQuota       = "sum"
)

var (
	credentialFieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	regionIDPattern        = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,15}$`)
	identityClaimsPattern  = regexp.MustCompile(`^claims\.[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$`)
	importNamePattern      = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	accountPoolUIOps       = map[string]struct{}{
		"add": {}, "reauth": {}, "refresh": {}, "delete": {}, "toggle": {}, "models": {}, "import": {},
	}
	regionAttrKeys = map[string]struct{}{
		"origin":  {},
		"referer": {},
	}
	accountPoolUITextKeys = map[string]struct{}{
		"title": {}, "login_reauth_title": {}, "login_title": {}, "login_starting": {},
		"login_instructions": {}, "login_authorize": {}, "login_waiting": {}, "cancel": {},
		"region_title": {}, "region_description": {}, "region_continue": {}, "loading": {},
		"add": {}, "import": {}, "refresh_all": {}, "unnamed": {}, "refresh": {},
		"delete": {}, "reauthorize": {}, "toggle_label": {}, "empty": {},
		"delete_title": {}, "delete_confirm": {}, "delete_delete": {}, "login_hint": {},
		"import_title": {}, "import_dropzone": {}, "import_hint": {}, "import_selected_count": {},
		"import_summary": {}, "import_imported": {}, "import_failed": {}, "import_unnamed": {},
		"import_done": {}, "import_with_count": {}, "import_submit": {}, "import_toast_done": {},
		"import_toast_failed": {},
	}
)

// SiteProviderField is one credential-bag field of an account pool.
type SiteProviderField struct {
	Name   string `json:"name"`
	Secret bool   `json:"secret,omitempty"`
	Type   string `json:"type,omitempty"`
}

// SiteProviderRegion is one upstream region. BaseURL's host must be in hosts.
// Attrs holds header-template values such as origin and referer. Keys are a
// fixed allow-list. {{region.<key>}} reads the account's region.
type SiteProviderRegion struct {
	ID      string            `json:"id"`
	Label   LocalizedText     `json:"label,omitempty"`
	BaseURL string            `json:"baseURL"`
	Realms  []string          `json:"realms,omitempty"`
	Domains []string          `json:"domains,omitempty"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

// SiteProviderLoginParam is one value the login UI collects.
type SiteProviderLoginParam struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Source string `json:"source"`
}

// SiteProviderLogin describes how an account is added.
// Device-code fields apply only when Kind is oauth2_device_code.
type SiteProviderLogin struct {
	Kind          string                   `json:"kind"`
	Params        []SiteProviderLoginParam `json:"params,omitempty"`
	SecretPaths   map[string]string        `json:"secretPaths,omitempty"`
	DeviceCodeURL string                   `json:"deviceCodeURL,omitempty"`
	TokenURL      string                   `json:"tokenURL,omitempty"`
	ClientID      string                   `json:"clientID,omitempty"`
	Scope         string                   `json:"scope,omitempty"`
	GrantType     string                   `json:"grantType,omitempty"`
	ExtraHeaders  map[string]string        `json:"extraHeaders,omitempty"`
}

// SiteProviderRefresh describes how an access token is renewed.
// LeadSeconds nil means the default, 300, except when Kind is none.
type SiteProviderRefresh struct {
	Kind        string            `json:"kind"`
	LeadSeconds *int              `json:"leadSeconds,omitempty"`
	SecretPaths map[string]string `json:"secretPaths,omitempty"`
	TokenURL    string            `json:"tokenURL,omitempty"`
}

// SiteProviderImport describes declarative account import.
// ValueMap rewrites a mapped field value. MsThreshold nil means 1e12:
// numbers at or above the threshold are Unix milliseconds.
type SiteProviderImport struct {
	Enabled     bool                         `json:"enabled,omitempty"`
	Containers  []string                     `json:"containers,omitempty"`
	FieldMap    map[string][]string          `json:"fieldMap,omitempty"`
	ValueMap    map[string]map[string]string `json:"valueMap,omitempty"`
	MsThreshold *int64                       `json:"msThreshold,omitempty"`
}

// SiteProviderQuota is the site-level quota rollup. Both credential models may set it.
type SiteProviderQuota struct {
	Aggregate string `json:"aggregate,omitempty"`
}

// SiteProviderRowPill is one non-secret field shown on an account row.
// Labels optionally renames field values.
type SiteProviderRowPill struct {
	Field  string                   `json:"field"`
	Labels map[string]LocalizedText `json:"labels,omitempty"`
}

// SiteProviderUI is the account-pool admin surface.
// Texts values are a plain string or {"zh","en","jp",...}.
type SiteProviderUI struct {
	RowPills   []SiteProviderRowPill            `json:"rowPills,omitempty"`
	Operations []string                         `json:"operations,omitempty"`
	Texts      map[string]LocalizedTextOrString `json:"texts,omitempty"`
}

// AccountPoolSpec is an immutable copy of the account-pool manifest sections.
// API-key site types leave Plugin site Pool nil.
type AccountPoolSpec struct {
	Credential       SiteProviderCredential
	Regions          []SiteProviderRegion
	DefaultRegion    string
	Login            SiteProviderLogin
	Refresh          SiteProviderRefresh
	Import           SiteProviderImport
	Selection        string
	QuotaAggregate   string
	ModelsPerAccount bool
	Request          SiteProviderRequest
	UI               SiteProviderUI
}

// AccountRegionID picks the region id for an account bag.
// An explicit region field wins, including an id this pool does not define.
// Otherwise a realm that equals a region id or one of its realms wins.
// Otherwise a domain listed on a region wins.
// An empty result means the caller should use its default region.
func AccountRegionID(regions []SiteProviderRegion, fields map[string]string) string {
	if fields == nil {
		return ""
	}
	if id := strings.TrimSpace(fields["region"]); id != "" {
		return id
	}
	if realm := strings.TrimSpace(fields["realm"]); realm != "" {
		for _, region := range regions {
			if strings.EqualFold(region.ID, realm) {
				return region.ID
			}
			for _, alias := range region.Realms {
				if strings.EqualFold(strings.TrimSpace(alias), realm) {
					return region.ID
				}
			}
		}
	}
	if domain := strings.TrimSpace(fields["domain"]); domain != "" {
		for _, region := range regions {
			for _, item := range region.Domains {
				if strings.EqualFold(strings.TrimSpace(item), domain) {
					return region.ID
				}
			}
		}
	}
	return ""
}

// DefaultBaseURL is the base URL of DefaultRegion.
func (s AccountPoolSpec) DefaultBaseURL() string {
	for _, region := range s.Regions {
		if region.ID == s.DefaultRegion {
			return strings.TrimRight(strings.TrimSpace(region.BaseURL), "/")
		}
	}
	return ""
}

// Clone returns a deep copy.
func (s AccountPoolSpec) Clone() AccountPoolSpec {
	return AccountPoolSpec{
		Credential:       cloneCredential(s.Credential),
		Regions:          cloneRegions(s.Regions),
		DefaultRegion:    s.DefaultRegion,
		Login:            cloneLogin(s.Login),
		Refresh:          cloneRefresh(s.Refresh),
		Import:           cloneImport(s.Import),
		Selection:        s.Selection,
		QuotaAggregate:   s.QuotaAggregate,
		ModelsPerAccount: s.ModelsPerAccount,
		Request:          cloneSiteProviderRequest(s.Request),
		UI:               cloneUI(s.UI),
	}
}

// NewAccountPoolSpec copies section into an immutable spec and fills defaults.
// section must already be an account_pool manifest; this does not revalidate it.
func NewAccountPoolSpec(section SiteProviderSection) (AccountPoolSpec, error) {
	if section.Credential.Model != "account_pool" {
		return AccountPoolSpec{}, fmt.Errorf("siteProvider.credential.model %q is not %q", section.Credential.Model, "account_pool")
	}
	if section.Login == nil {
		return AccountPoolSpec{}, fmt.Errorf("siteProvider.login is required")
	}
	if section.Refresh == nil {
		return AccountPoolSpec{}, fmt.Errorf("siteProvider.refresh is required")
	}
	spec := AccountPoolSpec{
		Credential:       cloneCredential(section.Credential),
		Regions:          cloneRegions(section.Regions),
		DefaultRegion:    section.DefaultRegion,
		Login:            cloneLogin(*section.Login),
		Refresh:          cloneRefresh(*section.Refresh),
		Selection:        section.Selection,
		ModelsPerAccount: section.Models.PerAccount,
		Request:          cloneSiteProviderRequest(section.Request),
	}
	if section.Import != nil {
		spec.Import = cloneImport(*section.Import)
	}
	if section.UI != nil {
		spec.UI = cloneUI(*section.UI)
	}
	if spec.Credential.AccessTokenField == "" {
		spec.Credential.AccessTokenField = defaultAccessTokenField
	}
	for i := range spec.Credential.Fields {
		if spec.Credential.Fields[i].Type == "" {
			spec.Credential.Fields[i].Type = "string"
		}
	}
	if spec.Selection == "" {
		spec.Selection = defaultAccountSelection
	}
	spec.QuotaAggregate = defaultAccountQuota
	if section.Quota != nil && section.Quota.Aggregate != "" {
		spec.QuotaAggregate = section.Quota.Aggregate
	}
	if spec.Refresh.Kind != "none" && spec.Refresh.LeadSeconds == nil {
		spec.Refresh.LeadSeconds = intPointer(defaultRefreshLeadSeconds)
	}
	if spec.Refresh.Kind == "none" {
		spec.Refresh.LeadSeconds = nil
	}
	return spec, nil
}

func (s *SiteProviderSection) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	type plain SiteProviderSection
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = SiteProviderSection(decoded)
	if _, ok := raw["models"]; ok {
		s.modelsSet = true
	}
	return nil
}

type credentialFieldMeta struct {
	secret bool
	typ    string
}

func rejectCredentialModelExtras(section SiteProviderSection) error {
	switch section.Credential.Model {
	case "api_key":
		return rejectAccountPoolSections(section)
	case "account_pool":
		return validateAccountPoolModels(section)
	default:
		return fmt.Errorf("siteProvider.credential.model must be %q or %q", "api_key", "account_pool")
	}
}

func rejectAccountPoolSections(section SiteProviderSection) error {
	checks := []struct {
		present bool
		path    string
	}{
		{section.Regions != nil, "siteProvider.regions"},
		{section.DefaultRegion != "", "siteProvider.defaultRegion"},
		{section.Login != nil, "siteProvider.login"},
		{section.Refresh != nil, "siteProvider.refresh"},
		{section.Import != nil, "siteProvider.import"},
		{section.Selection != "", "siteProvider.selection"},
		{section.UI != nil, "siteProvider.ui"},
		{section.Credential.Fields != nil, "siteProvider.credential.fields"},
		{section.Credential.AccessTokenField != "", "siteProvider.credential.accessTokenField"},
		{section.Credential.RefreshTokenField != "", "siteProvider.credential.refreshTokenField"},
		{section.Credential.ExpiresAtField != "", "siteProvider.credential.expiresAtField"},
		{section.Credential.ExpiresFrom != "", "siteProvider.credential.expiresFrom"},
		{!section.Credential.Identity.Empty(), "siteProvider.credential.identity"},
		{!section.Credential.Display.Empty(), "siteProvider.credential.display"},
	}
	for _, check := range checks {
		if check.present {
			return fmt.Errorf("%s is not allowed for credential.model %q", check.path, "api_key")
		}
	}
	return nil
}

func validateAccountPoolModels(section SiteProviderSection) error {
	if !section.modelsSet && section.Models == (SiteProviderModels{}) {
		return nil
	}
	if section.Models.Path != "" {
		return fmt.Errorf("siteProvider.models.path is not allowed for credential.model %q", "account_pool")
	}
	if section.Models.Auth != "" {
		return fmt.Errorf("siteProvider.models.auth is not allowed for credential.model %q", "account_pool")
	}
	if !section.Models.PerAccount {
		return fmt.Errorf("siteProvider.models.perAccount must be true")
	}
	return nil
}

func validateSiteProviderQuota(section *SiteProviderQuota) error {
	if section == nil || section.Aggregate == "" {
		return nil
	}
	switch section.Aggregate {
	case "sum", "min", "none":
		return nil
	default:
		return fmt.Errorf("siteProvider.quota.aggregate %q is not supported", section.Aggregate)
	}
}

func validateRequestBaseURLFrom(section SiteProviderSection) error {
	switch section.Request.BaseURLFrom {
	case "":
		return nil
	case "region":
		if len(section.Regions) == 0 {
			return fmt.Errorf("siteProvider.request.baseURLFrom %q requires regions", "region")
		}
		return nil
	default:
		return fmt.Errorf("siteProvider.request.baseURLFrom %q is not supported", section.Request.BaseURLFrom)
	}
}

func validateAccountPool(section SiteProviderSection) error {
	fields, accessField, err := validateCredentialFields(section.Credential)
	if err != nil {
		return err
	}
	ids, err := validateAccountRegions(section.Regions, section.Hosts)
	if err != nil {
		return err
	}
	if section.DefaultRegion == "" {
		return fmt.Errorf("siteProvider.defaultRegion is required")
	}
	if _, ok := ids[section.DefaultRegion]; !ok {
		return fmt.Errorf("siteProvider.defaultRegion %q is not a region id", section.DefaultRegion)
	}
	if err := validateAccountLogin(section, fields, accessField); err != nil {
		return err
	}
	if err := validateAccountRefresh(section, fields); err != nil {
		return err
	}
	declared := make(map[string]struct{}, len(fields))
	for name := range fields {
		declared[name] = struct{}{}
	}
	if err := validateAccountImport(section.Import, section.Credential, declared); err != nil {
		return err
	}
	if err := validateAccountSelection(section.Selection); err != nil {
		return err
	}
	importEnabled := section.Import != nil && section.Import.Enabled
	return validateAccountUI(section.UI, fields, importEnabled)
}

func validateCredentialFields(cred SiteProviderCredential) (map[string]credentialFieldMeta, string, error) {
	if len(cred.Fields) < 1 || len(cred.Fields) > maxCredentialFields {
		return nil, "", fmt.Errorf("siteProvider.credential.fields must list 1 to %d fields", maxCredentialFields)
	}
	fields := make(map[string]credentialFieldMeta, len(cred.Fields))
	secrets := 0
	for i, field := range cred.Fields {
		if !credentialFieldPattern.MatchString(field.Name) {
			return nil, "", fmt.Errorf("siteProvider.credential.fields[%d].name %q is invalid", i, field.Name)
		}
		if _, ok := fields[field.Name]; ok {
			return nil, "", fmt.Errorf("siteProvider.credential.fields[%d].name %q is duplicated", i, field.Name)
		}
		typ := field.Type
		if typ == "" {
			typ = "string"
		}
		if typ != "string" && typ != "unix" {
			return nil, "", fmt.Errorf("siteProvider.credential.fields[%d].type %q is not supported", i, field.Type)
		}
		if field.Secret {
			secrets++
		}
		fields[field.Name] = credentialFieldMeta{secret: field.Secret, typ: typ}
	}
	if secrets == 0 {
		return nil, "", fmt.Errorf("siteProvider.credential.fields must include a secret field")
	}
	access := cred.AccessTokenField
	if access == "" {
		access = defaultAccessTokenField
	}
	got, ok := fields[access]
	if !ok {
		return nil, "", fmt.Errorf("siteProvider.credential.accessTokenField %q is not declared", access)
	}
	if !got.secret {
		return nil, "", fmt.Errorf("siteProvider.credential.accessTokenField %q must be a secret field", access)
	}
	if cred.RefreshTokenField != "" {
		got, ok := fields[cred.RefreshTokenField]
		if !ok {
			return nil, "", fmt.Errorf("siteProvider.credential.refreshTokenField %q is not declared", cred.RefreshTokenField)
		}
		if !got.secret {
			return nil, "", fmt.Errorf("siteProvider.credential.refreshTokenField %q must be a secret field", cred.RefreshTokenField)
		}
	}
	if cred.ExpiresAtField != "" {
		got, ok := fields[cred.ExpiresAtField]
		if !ok {
			return nil, "", fmt.Errorf("siteProvider.credential.expiresAtField %q is not declared", cred.ExpiresAtField)
		}
		if got.typ != "unix" {
			return nil, "", fmt.Errorf("siteProvider.credential.expiresAtField %q must have type %q", cred.ExpiresAtField, "unix")
		}
	}
	if err := validateAccountIdentity(cred.Identity, fields); err != nil {
		return nil, "", err
	}
	if err := validateAccountDisplay(cred.Display, fields); err != nil {
		return nil, "", err
	}
	if err := validateExpiresFrom(cred.ExpiresFrom, cred.ExpiresAtField); err != nil {
		return nil, "", err
	}
	return fields, access, nil
}

func validateAccountIdentity(identity CredentialSources, fields map[string]credentialFieldMeta) error {
	if identity.Empty() {
		return fmt.Errorf("siteProvider.credential.identity is required")
	}
	if len(identity.Items) > maxCredentialSources {
		return fmt.Errorf("siteProvider.credential.identity must list at most %d sources", maxCredentialSources)
	}
	seen := make(map[string]struct{}, len(identity.Items))
	for i, item := range identity.Items {
		if _, ok := seen[item]; ok {
			return fmt.Errorf("siteProvider.credential.identity duplicate source %q", item)
		}
		seen[item] = struct{}{}
		if err := validateIdentitySource(item, fields, sourcePath("siteProvider.credential.identity", i, len(identity.Items))); err != nil {
			return err
		}
	}
	return nil
}

func validateAccountDisplay(display CredentialSources, fields map[string]credentialFieldMeta) error {
	if display.Empty() {
		return nil
	}
	if len(display.Items) > maxCredentialSources {
		return fmt.Errorf("siteProvider.credential.display must list at most %d sources", maxCredentialSources)
	}
	seen := make(map[string]struct{}, len(display.Items))
	for i, item := range display.Items {
		if _, ok := seen[item]; ok {
			return fmt.Errorf("siteProvider.credential.display duplicate source %q", item)
		}
		seen[item] = struct{}{}
		path := sourcePath("siteProvider.credential.display", i, len(display.Items))
		if item == summaryLabelSource {
			continue
		}
		if err := validateIdentitySource(item, fields, path); err != nil {
			return err
		}
	}
	return nil
}

func validateExpiresFrom(value, expiresAtField string) error {
	if value == "" {
		return nil
	}
	if value != expiresFromClaimExp {
		return fmt.Errorf("siteProvider.credential.expiresFrom %q is not supported", value)
	}
	if expiresAtField == "" {
		return fmt.Errorf("siteProvider.credential.expiresFrom requires expiresAtField")
	}
	return nil
}

func sourcePath(prefix string, index, count int) string {
	if count == 1 {
		return prefix
	}
	return fmt.Sprintf("%s[%d]", prefix, index)
}

func validateIdentitySource(identity string, fields map[string]credentialFieldMeta, path string) error {
	if identityClaimsPattern.MatchString(identity) {
		return nil
	}
	name, ok := strings.CutPrefix(identity, "field.")
	if !ok || !credentialFieldPattern.MatchString(name) {
		return fmt.Errorf("%s %q is invalid", path, identity)
	}
	meta, exists := fields[name]
	if !exists {
		return fmt.Errorf("%s field %q is not declared", path, name)
	}
	if meta.secret {
		return fmt.Errorf("%s field %q must not be secret", path, name)
	}
	return nil
}

func validateAccountRegions(regions []SiteProviderRegion, hosts []string) (map[string]struct{}, error) {
	if len(regions) < 1 || len(regions) > maxAccountPoolRegions {
		return nil, fmt.Errorf("siteProvider.regions must list 1 to %d regions", maxAccountPoolRegions)
	}
	ids := make(map[string]struct{}, len(regions))
	for i, region := range regions {
		if !regionIDPattern.MatchString(region.ID) {
			return nil, fmt.Errorf("siteProvider.regions[%d].id %q is invalid", i, region.ID)
		}
		if _, ok := ids[region.ID]; ok {
			return nil, fmt.Errorf("siteProvider.regions[%d].id %q is duplicated", i, region.ID)
		}
		ids[region.ID] = struct{}{}
		if err := region.Label.validate(); err != nil {
			return nil, fmt.Errorf("siteProvider.regions[%d].label: %w", i, err)
		}
		if err := validateHTTPSURL(region.BaseURL, hosts, fmt.Sprintf("siteProvider.regions[%d].baseURL", i)); err != nil {
			return nil, err
		}
		if err := validateRealms(region.Realms, i); err != nil {
			return nil, err
		}
		if err := validateRegionDomains(region.Domains, i); err != nil {
			return nil, err
		}
		if err := validateRegionAttrs(region.Attrs, i); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func validateRegionAttrs(attrs map[string]string, index int) error {
	if len(attrs) > maxRegionAttrs {
		return fmt.Errorf("siteProvider.regions[%d].attrs must list at most %d items", index, maxRegionAttrs)
	}
	for _, key := range mapKeys(attrs) {
		if _, ok := regionAttrKeys[key]; !ok {
			return fmt.Errorf("siteProvider.regions[%d].attrs[%q] is not allowed", index, key)
		}
		value := attrs[key]
		if value == "" || len(value) > maxRegionAttrBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("siteProvider.regions[%d].attrs[%q] must be 1 to %d bytes without control characters", index, key, maxRegionAttrBytes)
		}
	}
	return nil
}

// regionTemplateKeys is the set of attr keys declared on every region.
// A header template may use {{region.<key>}} only for a key in this set.
func regionTemplateKeys(regions []SiteProviderRegion) map[string]struct{} {
	if len(regions) == 0 {
		return map[string]struct{}{}
	}
	keys := map[string]struct{}{}
	for key := range regions[0].Attrs {
		if _, ok := regionAttrKeys[key]; ok && regions[0].Attrs[key] != "" {
			keys[key] = struct{}{}
		}
	}
	for _, region := range regions[1:] {
		for key := range keys {
			if strings.TrimSpace(region.Attrs[key]) == "" {
				delete(keys, key)
			}
		}
	}
	return keys
}

func validateRealms(realms []string, index int) error {
	if len(realms) > maxRegionListItems {
		return fmt.Errorf("siteProvider.regions[%d].realms must list at most %d items", index, maxRegionListItems)
	}
	for i, realm := range realms {
		if !validRealm(realm) {
			return fmt.Errorf("siteProvider.regions[%d].realms[%d] %q must be lowercase", index, i, realm)
		}
	}
	return nil
}

func validRealm(value string) bool {
	if value == "" || value != strings.ToLower(value) {
		return false
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func validateRegionDomains(domains []string, index int) error {
	if len(domains) > maxRegionListItems {
		return fmt.Errorf("siteProvider.regions[%d].domains must list at most %d items", index, maxRegionListItems)
	}
	for i, domain := range domains {
		if err := validSiteProviderHost(domain); err != nil {
			return fmt.Errorf("siteProvider.regions[%d].domains[%d]: %w", index, i, err)
		}
	}
	return nil
}

func validateAccountLogin(section SiteProviderSection, fields map[string]credentialFieldMeta, accessField string) error {
	login := section.Login
	if login == nil {
		return fmt.Errorf("siteProvider.login is required")
	}
	secrets := secretFieldNames(fields)
	switch login.Kind {
	case "custom_poll":
		if err := rejectOAuthLoginFields(login); err != nil {
			return err
		}
	case "oauth2_device_code":
		if err := validateOAuthLogin(login, section.Hosts); err != nil {
			return err
		}
	default:
		return fmt.Errorf("siteProvider.login.kind %q is not supported", login.Kind)
	}
	if err := validateLoginParams(login.Params, section.Regions); err != nil {
		return err
	}
	if err := validateSecretPaths(login.SecretPaths, secrets, "siteProvider.login.secretPaths"); err != nil {
		return err
	}
	if login.Kind == "custom_poll" && !secretPathsCover(login.SecretPaths, accessField) {
		return fmt.Errorf("siteProvider.login.secretPaths must map a path to %q", accessField)
	}
	return nil
}

func rejectOAuthLoginFields(login *SiteProviderLogin) error {
	forbidden := []struct {
		present bool
		path    string
	}{
		{login.DeviceCodeURL != "", "siteProvider.login.deviceCodeURL"},
		{login.TokenURL != "", "siteProvider.login.tokenURL"},
		{login.ClientID != "", "siteProvider.login.clientID"},
		{login.Scope != "", "siteProvider.login.scope"},
		{login.GrantType != "", "siteProvider.login.grantType"},
		{login.ExtraHeaders != nil, "siteProvider.login.extraHeaders"},
	}
	for _, item := range forbidden {
		if item.present {
			return fmt.Errorf("%s is not allowed when kind is %q", item.path, "custom_poll")
		}
	}
	return nil
}

func validateOAuthLogin(login *SiteProviderLogin, hosts []string) error {
	if strings.TrimSpace(login.DeviceCodeURL) == "" {
		return fmt.Errorf("siteProvider.login.deviceCodeURL is required")
	}
	if err := validateHTTPSURL(login.DeviceCodeURL, hosts, "siteProvider.login.deviceCodeURL"); err != nil {
		return err
	}
	if strings.TrimSpace(login.TokenURL) == "" {
		return fmt.Errorf("siteProvider.login.tokenURL is required")
	}
	if err := validateHTTPSURL(login.TokenURL, hosts, "siteProvider.login.tokenURL"); err != nil {
		return err
	}
	if strings.TrimSpace(login.ClientID) == "" {
		return fmt.Errorf("siteProvider.login.clientID is required")
	}
	if err := rejectControl(login.ClientID, "siteProvider.login.clientID"); err != nil {
		return err
	}
	if err := rejectControl(login.Scope, "siteProvider.login.scope"); err != nil {
		return err
	}
	if err := rejectControl(login.GrantType, "siteProvider.login.grantType"); err != nil {
		return err
	}
	return validateLoginExtraHeaders(login.ExtraHeaders)
}

func validateLoginParams(params []SiteProviderLoginParam, regions []SiteProviderRegion) error {
	for i, param := range params {
		if strings.TrimSpace(param.Name) == "" {
			return fmt.Errorf("siteProvider.login.params[%d].name is required", i)
		}
		if param.Type != "select" {
			return fmt.Errorf("siteProvider.login.params[%d].type %q is not supported", i, param.Type)
		}
		if param.Source != "regions" {
			return fmt.Errorf("siteProvider.login.params[%d].source %q is not supported", i, param.Source)
		}
		if len(regions) == 0 {
			return fmt.Errorf("siteProvider.login.params[%d].source %q requires regions", i, param.Source)
		}
	}
	return nil
}

func validateAccountRefresh(section SiteProviderSection, fields map[string]credentialFieldMeta) error {
	refresh := section.Refresh
	if refresh == nil {
		return fmt.Errorf("siteProvider.refresh is required")
	}
	switch refresh.Kind {
	case "none":
		if refresh.SecretPaths != nil {
			return fmt.Errorf("siteProvider.refresh.secretPaths is not allowed when kind is %q", "none")
		}
		if refresh.TokenURL != "" {
			return fmt.Errorf("siteProvider.refresh.tokenURL is not allowed when kind is %q", "none")
		}
		if refresh.LeadSeconds != nil {
			return fmt.Errorf("siteProvider.refresh.leadSeconds is not allowed when kind is %q", "none")
		}
		return nil
	case "custom_steps", "oauth2_refresh":
	default:
		return fmt.Errorf("siteProvider.refresh.kind %q is not supported", refresh.Kind)
	}
	if err := validateLeadSeconds(refresh.LeadSeconds); err != nil {
		return err
	}
	if err := validateSecretPaths(refresh.SecretPaths, secretFieldNames(fields), "siteProvider.refresh.secretPaths"); err != nil {
		return err
	}
	if refresh.Kind != "oauth2_refresh" {
		return nil
	}
	if section.Credential.RefreshTokenField == "" {
		return fmt.Errorf("siteProvider.credential.refreshTokenField is required when refresh.kind is %q", "oauth2_refresh")
	}
	if refresh.TokenURL != "" {
		return validateHTTPSURL(refresh.TokenURL, section.Hosts, "siteProvider.refresh.tokenURL")
	}
	if section.Login == nil || strings.TrimSpace(section.Login.TokenURL) == "" {
		return fmt.Errorf("siteProvider.refresh.tokenURL is required")
	}
	return nil
}

func validateLeadSeconds(value *int) error {
	if value == nil {
		return nil
	}
	if *value < minRefreshLeadSeconds || *value > maxRefreshLeadSeconds {
		return fmt.Errorf("siteProvider.refresh.leadSeconds %d must be between %d and %d", *value, minRefreshLeadSeconds, maxRefreshLeadSeconds)
	}
	return nil
}

func validateAccountImport(section *SiteProviderImport, cred SiteProviderCredential, declared map[string]struct{}) error {
	if section == nil {
		return nil
	}
	if len(section.Containers) > maxImportContainers {
		return fmt.Errorf("siteProvider.import.containers must list at most %d items", maxImportContainers)
	}
	for i, name := range section.Containers {
		if !importNamePattern.MatchString(name) {
			return fmt.Errorf("siteProvider.import.containers[%d] %q is invalid", i, name)
		}
	}
	if err := validateImportFieldMap(section.FieldMap, declared); err != nil {
		return err
	}
	if err := validateImportValueMap(section.ValueMap, declared, cred.ExpiresAtField); err != nil {
		return err
	}
	if err := validateImportThreshold(section.MsThreshold); err != nil {
		return err
	}
	if !section.Enabled {
		return nil
	}
	needed := make([]string, 0, len(cred.Fields))
	seen := map[string]struct{}{}
	for _, field := range cred.Fields {
		if !field.Secret {
			continue
		}
		needed = append(needed, field.Name)
		seen[field.Name] = struct{}{}
	}
	for _, source := range append(append([]string{}, cred.Identity.Items...), cred.Display.Items...) {
		name, ok := strings.CutPrefix(source, "field.")
		if !ok || !credentialFieldPattern.MatchString(name) {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		needed = append(needed, name)
		seen[name] = struct{}{}
	}
	for _, name := range needed {
		if len(section.FieldMap[name]) == 0 {
			return fmt.Errorf("siteProvider.import.fieldMap must include %q", name)
		}
	}
	return nil
}

func validateImportValueMap(valueMap map[string]map[string]string, declared map[string]struct{}, expiresAtField string) error {
	if len(valueMap) > maxImportValueMapFields {
		return fmt.Errorf("siteProvider.import.valueMap must list at most %d fields", maxImportValueMapFields)
	}
	for _, field := range mapKeys(valueMap) {
		if _, ok := declared[field]; !ok {
			return fmt.Errorf("siteProvider.import.valueMap[%q] is not a declared field", field)
		}
		if field == expiresAtField {
			return fmt.Errorf("siteProvider.import.valueMap[%q] cannot rewrite the expires field", field)
		}
		entries := valueMap[field]
		if len(entries) < 1 || len(entries) > maxImportValueMapEntries {
			return fmt.Errorf("siteProvider.import.valueMap[%q] must list 1 to %d entries", field, maxImportValueMapEntries)
		}
		for _, from := range mapKeys(entries) {
			if err := validImportValue(from); err != nil {
				return fmt.Errorf("siteProvider.import.valueMap[%q] key: %w", field, err)
			}
			if err := validImportValue(entries[from]); err != nil {
				return fmt.Errorf("siteProvider.import.valueMap[%q] value: %w", field, err)
			}
		}
	}
	return nil
}

func validImportValue(value string) error {
	if value == "" || len(value) > maxImportValueBytes || strings.IndexFunc(value, unicode.IsControl) >= 0 || !importNamePattern.MatchString(value) {
		return fmt.Errorf("%q is invalid", value)
	}
	return nil
}

func validateImportThreshold(value *int64) error {
	if value == nil {
		return nil
	}
	if *value < 1 || *value > maxMillisThreshold {
		return fmt.Errorf("siteProvider.import.msThreshold must be between 1 and %d", maxMillisThreshold)
	}
	return nil
}

func validateImportFieldMap(fieldMap map[string][]string, declared map[string]struct{}) error {
	keys := mapKeys(fieldMap)
	for _, key := range keys {
		if _, ok := declared[key]; !ok {
			return fmt.Errorf("siteProvider.import.fieldMap[%q] is not a declared field", key)
		}
		aliases := fieldMap[key]
		if len(aliases) < 1 || len(aliases) > maxImportAliases {
			return fmt.Errorf("siteProvider.import.fieldMap[%q] must list 1 to %d aliases", key, maxImportAliases)
		}
		seen := make(map[string]struct{}, len(aliases))
		for i, alias := range aliases {
			if !importNamePattern.MatchString(alias) {
				return fmt.Errorf("siteProvider.import.fieldMap[%q][%d] %q is invalid", key, i, alias)
			}
			if _, ok := seen[alias]; ok {
				return fmt.Errorf("siteProvider.import.fieldMap[%q] duplicate alias %q", key, alias)
			}
			seen[alias] = struct{}{}
		}
	}
	return nil
}

func validateAccountSelection(selection string) error {
	switch selection {
	case "", "ordered", "quota_weighted":
		return nil
	default:
		return fmt.Errorf("siteProvider.selection %q is not supported", selection)
	}
}

func validateAccountUI(ui *SiteProviderUI, fields map[string]credentialFieldMeta, importEnabled bool) error {
	if ui == nil {
		return nil
	}
	if len(ui.RowPills) > maxRowPills {
		return fmt.Errorf("siteProvider.ui.rowPills must list at most %d items", maxRowPills)
	}
	for i, pill := range ui.RowPills {
		meta, ok := fields[pill.Field]
		if !ok {
			return fmt.Errorf("siteProvider.ui.rowPills[%d].field %q is not declared", i, pill.Field)
		}
		if meta.secret {
			return fmt.Errorf("siteProvider.ui.rowPills[%d].field %q must not be secret", i, pill.Field)
		}
		if len(pill.Labels) > maxPillLabels {
			return fmt.Errorf("siteProvider.ui.rowPills[%d].labels must list at most %d items", i, maxPillLabels)
		}
		for key, label := range pill.Labels {
			if key == "" || len(key) > maxPillLabelKey || strings.IndexFunc(key, unicode.IsControl) >= 0 {
				return fmt.Errorf("siteProvider.ui.rowPills[%d].labels key is invalid", i)
			}
			if err := label.validate(); err != nil {
				return fmt.Errorf("siteProvider.ui.rowPills[%d].labels[%q]: %w", i, key, err)
			}
		}
	}
	seen := make(map[string]struct{}, len(ui.Operations))
	for i, op := range ui.Operations {
		if _, ok := accountPoolUIOps[op]; !ok {
			return fmt.Errorf("siteProvider.ui.operations[%d] %q is not supported", i, op)
		}
		if _, ok := seen[op]; ok {
			return fmt.Errorf("siteProvider.ui.operations[%d] %q is duplicated", i, op)
		}
		seen[op] = struct{}{}
		if op == "import" && !importEnabled {
			return fmt.Errorf("siteProvider.ui.operations %q requires import.enabled", op)
		}
	}
	for key, text := range ui.Texts {
		if _, ok := accountPoolUITextKeys[key]; !ok {
			return fmt.Errorf("siteProvider.ui.texts[%q] is not supported", key)
		}
		if err := text.validate(); err != nil {
			return fmt.Errorf("siteProvider.ui.texts[%q]: %w", key, err)
		}
	}
	return nil
}

func validateSecretPaths(paths map[string]string, secrets map[string]struct{}, what string) error {
	keys := mapKeys(paths)
	for _, key := range keys {
		if !validSecretPath(key) {
			return fmt.Errorf("%s[%q]: invalid path", what, key)
		}
		name := paths[key]
		if _, ok := secrets[name]; !ok {
			return fmt.Errorf("%s[%q]: %q is not a secret field", what, key, name)
		}
	}
	return nil
}

func secretPathsCover(paths map[string]string, field string) bool {
	for _, name := range paths {
		if name == field {
			return true
		}
	}
	return false
}

func validSecretPath(path string) bool {
	if path == "" || strings.ContainsAny(path, "*? \t") {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if !validSecretPathPart(part) {
			return false
		}
	}
	return true
}

func validSecretPathPart(part string) bool {
	if part == "" {
		return false
	}
	i := 0
	if part[0] != '[' {
		start := i
		for i < len(part) && part[i] != '[' {
			c := part[i]
			if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' {
				return false
			}
			i++
		}
		if i == start {
			return false
		}
	}
	for i < len(part) {
		if part[i] != '[' {
			return false
		}
		end := strings.IndexByte(part[i:], ']')
		if end < 0 {
			return false
		}
		end += i
		if !validPathIndex(part[i+1 : end]) {
			return false
		}
		i = end + 1
	}
	return true
}

func validPathIndex(idx string) bool {
	if idx == "" {
		return false
	}
	for _, c := range idx {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func validateLoginExtraHeaders(headers map[string]string) error {
	names := mapKeys(headers)
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !validHeaderToken(name) {
			return fmt.Errorf("siteProvider.login.extraHeaders: %q is not a valid HTTP token", name)
		}
		lower := strings.ToLower(name)
		if _, ok := seen[lower]; ok {
			return fmt.Errorf("siteProvider.login.extraHeaders: duplicate header %q", name)
		}
		seen[lower] = struct{}{}
		if forbiddenExtraHeader(lower) {
			return fmt.Errorf("siteProvider.login.extraHeaders: %q is not allowed", name)
		}
		value := headers[name]
		if value == "" || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return fmt.Errorf("siteProvider.login.extraHeaders[%q]: value contains a control character or is empty", name)
		}
	}
	return nil
}

func forbiddenExtraHeader(lowerName string) bool {
	switch lowerName {
	case "authorization", "cookie", "host", "content-length", "transfer-encoding", "connection", "proxy-authorization":
		return true
	default:
		return strings.HasPrefix(lowerName, "x-forwarded-")
	}
}

func rejectControl(value, what string) error {
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s contains a control character", what)
	}
	return nil
}

func secretFieldNames(fields map[string]credentialFieldMeta) map[string]struct{} {
	out := make(map[string]struct{}, len(fields))
	for name, meta := range fields {
		if meta.secret {
			out[name] = struct{}{}
		}
	}
	return out
}

func rejectListModelsWithoutPerAccount(manifest Manifest, program *program) error {
	if manifest.SiteProvider.Credential.Model != "account_pool" || manifest.SiteProvider.Models.PerAccount || program == nil {
		return nil
	}
	if _, ok := program.hooks["listModels"]; !ok {
		return nil
	}
	return fmt.Errorf("siteProvider.models.perAccount must be true when listModels is exported")
}

func accountPoolRequiredExports(section SiteProviderSection) []string {
	if section.Credential.Model != "account_pool" {
		return nil
	}
	names := []string{"summarizeAccount"}
	if section.Login != nil && section.Login.Kind == "custom_poll" {
		names = append(names, "startLogin", "pollLogin")
	}
	if section.Refresh != nil && section.Refresh.Kind == "custom_steps" {
		names = append(names, "refreshTokens")
	}
	if section.Models.PerAccount {
		names = append(names, "listModels")
	}
	return names
}

func mapKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func intPointer(value int) *int {
	return &value
}

func cloneCredential(in SiteProviderCredential) SiteProviderCredential {
	out := in
	out.Fields = append([]SiteProviderField(nil), in.Fields...)
	out.Identity = cloneSources(in.Identity)
	out.Display = cloneSources(in.Display)
	return out
}

func cloneRegions(in []SiteProviderRegion) []SiteProviderRegion {
	if in == nil {
		return nil
	}
	out := make([]SiteProviderRegion, len(in))
	for i, region := range in {
		out[i] = region
		out[i].Label = cloneLocalized(region.Label)
		out[i].Realms = append([]string(nil), region.Realms...)
		out[i].Domains = append([]string(nil), region.Domains...)
		out[i].Attrs = cloneStringMap(region.Attrs)
	}
	return out
}

func cloneLogin(in SiteProviderLogin) SiteProviderLogin {
	out := in
	if in.Params != nil {
		out.Params = append([]SiteProviderLoginParam(nil), in.Params...)
	}
	out.SecretPaths = cloneStringMap(in.SecretPaths)
	out.ExtraHeaders = cloneStringMap(in.ExtraHeaders)
	return out
}

func cloneRefresh(in SiteProviderRefresh) SiteProviderRefresh {
	out := in
	out.SecretPaths = cloneStringMap(in.SecretPaths)
	if in.LeadSeconds != nil {
		out.LeadSeconds = intPointer(*in.LeadSeconds)
	}
	return out
}

func cloneImport(in SiteProviderImport) SiteProviderImport {
	out := in
	out.Containers = append([]string(nil), in.Containers...)
	if in.FieldMap == nil {
		out.FieldMap = nil
		return out
	}
	out.FieldMap = make(map[string][]string, len(in.FieldMap))
	for key, aliases := range in.FieldMap {
		out.FieldMap[key] = append([]string(nil), aliases...)
	}
	if in.ValueMap != nil {
		out.ValueMap = make(map[string]map[string]string, len(in.ValueMap))
		for field, entries := range in.ValueMap {
			out.ValueMap[field] = cloneStringMap(entries)
		}
	}
	if in.MsThreshold != nil {
		value := *in.MsThreshold
		out.MsThreshold = &value
	}
	return out
}

func cloneUI(in SiteProviderUI) SiteProviderUI {
	out := SiteProviderUI{Operations: append([]string(nil), in.Operations...)}
	if len(in.RowPills) > 0 {
		out.RowPills = make([]SiteProviderRowPill, len(in.RowPills))
		for i, pill := range in.RowPills {
			out.RowPills[i] = SiteProviderRowPill{Field: pill.Field, Labels: cloneLocalizedMap(pill.Labels)}
		}
	}
	if len(in.Texts) > 0 {
		out.Texts = make(map[string]LocalizedTextOrString, len(in.Texts))
		for key, text := range in.Texts {
			out.Texts[key] = cloneLocalized(text)
		}
	}
	return out
}

func cloneLocalizedMap(in map[string]LocalizedText) map[string]LocalizedText {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]LocalizedText, len(in))
	for key, text := range in {
		out[key] = cloneLocalized(text)
	}
	return out
}

func cloneSiteProviderRequest(in SiteProviderRequest) SiteProviderRequest {
	out := in
	if in.Headers == nil {
		out.Headers = nil
		return out
	}
	out.Headers = make(map[string]HeaderSpec, len(in.Headers))
	for key, value := range in.Headers {
		out.Headers[key] = value
	}
	return out
}

func cloneLocalized(in LocalizedText) LocalizedText {
	if in == nil {
		return nil
	}
	out := make(LocalizedText, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
