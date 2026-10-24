package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/accountauth"
	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

const (
	pluginQuotaExhaustedMax       = 7 * 24 * time.Hour
	pluginQuotaExhaustedReason    = "quota_exhausted"
	pluginAccountAccessTokenField = "access_token"
)

type pluginAccountCredentialResolver struct{}

func (pluginAccountCredentialResolver) Resolve(ctx context.Context, h Handler, candidate routeengine.Candidate, credential store.GatewayCredential, key string, accountID string) (resolvedUpstreamCredential, *credentialResolveFailure) {
	item, ok := site.LookupPluginSiteType(candidate.Site.SiteType)
	if !ok || !item.Enabled || item.Pool == nil {
		return resolvedUpstreamCredential{}, &credentialResolveFailure{
			ErrorType:         "plugin_account_credential_invalid",
			Err:               errors.New("plugin account pool is not configured"),
			TryNextCredential: true,
		}
	}
	fields, err := parseAccountCredentialFields(key)
	if err != nil {
		return resolvedUpstreamCredential{}, &credentialResolveFailure{
			ErrorType:         "plugin_account_credential_invalid",
			Err:               err,
			TryNextCredential: true,
		}
	}
	if accountCredentialNeedsRefresh(*item.Pool, fields, time.Now()) {
		if h.pluginAccounts == nil {
			return resolvedUpstreamCredential{}, &credentialResolveFailure{
				ErrorType:         "plugin_account_refresh_failed",
				Err:               errors.New("plugin account refresher is not configured"),
				TryNextCredential: true,
			}
		}
		refreshed, refreshErr := h.pluginAccounts.Refresh(ctx, candidate.Site.ID, credential.Credential.ID, item.Descriptor.SiteType, cloneStringMap(fields))
		if refreshErr != nil {
			return resolvedUpstreamCredential{}, &credentialResolveFailure{
				ErrorType:         "plugin_account_refresh_failed",
				Err:               refreshErr,
				TryNextCredential: true,
				MarkReauth:        errors.Is(refreshErr, accountauth.ErrReauthorizationRequired),
			}
		}
		if len(refreshed) == 0 {
			return resolvedUpstreamCredential{}, &credentialResolveFailure{
				ErrorType:         "plugin_account_refresh_failed",
				Err:               errors.New("plugin account refresh returned no credential fields"),
				TryNextCredential: true,
			}
		}
		fields = refreshed
	}
	accessName := item.Pool.Credential.AccessTokenField
	if accessName == "" {
		accessName = pluginAccountAccessTokenField
	}
	access := strings.TrimSpace(fields[accessName])
	if access == "" {
		return resolvedUpstreamCredential{}, &credentialResolveFailure{
			ErrorType:         "plugin_account_credential_invalid",
			Err:               fmt.Errorf("account credential is missing %s", accessName),
			TryNextCredential: true,
		}
	}
	baseURL, err := pluginAccountBaseURL(item, fields)
	if err != nil {
		return resolvedUpstreamCredential{}, &credentialResolveFailure{
			ErrorType:         "plugin_account_credential_invalid",
			Err:               err,
			TryNextCredential: true,
		}
	}
	return resolvedUpstreamCredential{
		Key:        access,
		AccountID:  accountID,
		BaseURL:    baseURL,
		HeaderVars: pluginAccountHeaderVars(*item.Pool, fields, accessName, access),
	}, nil
}

func pluginAccountPoolCredential(siteType, credentialType string) bool {
	if isGrokSite(siteType) {
		return false
	}
	if !store.IsAccountCredentialTypeOf(credentialType, siteType) {
		return false
	}
	item, ok := site.LookupPluginSiteType(siteType)
	return ok && item.Enabled && item.Pool != nil
}

func pluginSiteUpstreamStreamOnly(siteType string) bool {
	if isGrokSite(siteType) {
		return false
	}
	item, ok := site.LookupPluginSiteType(siteType)
	if !ok || !item.Enabled {
		return false
	}
	if item.Request.UpstreamStreamOnly {
		return true
	}
	return item.Pool != nil && item.Pool.Request.UpstreamStreamOnly
}

func applyPluginUpstreamStreamOnly(resolved *resolvedProtocolSpec, siteType string) {
	if resolved == nil || !pluginSiteUpstreamStreamOnly(siteType) {
		return
	}
	forced := make(map[string]any, len(resolved.RequestParams.Forced)+1)
	for key, value := range resolved.RequestParams.Forced {
		forced[key] = value
	}
	forced["stream"] = true
	resolved.RequestParams.Forced = forced
}

func accountCredentialNeedsRefresh(spec jsplugin.AccountPoolSpec, fields map[string]string, now time.Time) bool {
	return site.PluginAccountNeedsRefresh(spec, fields, now)
}

func pluginAccountBaseURL(item site.PluginSiteType, fields map[string]string) (string, error) {
	from := strings.TrimSpace(item.Request.BaseURLFrom)
	if from == "" && item.Pool != nil {
		from = strings.TrimSpace(item.Pool.Request.BaseURLFrom)
	}
	if from == "" {
		return "", nil
	}
	if from != "region" {
		return "", fmt.Errorf("baseURLFrom %q is not supported", from)
	}
	if item.Pool == nil {
		return "", errors.New("account region is not configured")
	}
	id := jsplugin.AccountRegionID(item.Pool.Regions, fields)
	if id == "" {
		id = item.Pool.DefaultRegion
	}
	for _, region := range item.Pool.Regions {
		if region.ID != id {
			continue
		}
		base := strings.TrimRight(strings.TrimSpace(region.BaseURL), "/")
		if base == "" {
			return "", fmt.Errorf("region %q is not configured", id)
		}
		return base, nil
	}
	return "", fmt.Errorf("region %q is not configured", id)
}

func pluginAccountHeaderVars(spec jsplugin.AccountPoolSpec, fields map[string]string, accessName, access string) map[string]string {
	vars := map[string]string{}
	for _, field := range spec.Credential.Fields {
		if field.Secret && field.Name != accessName {
			continue
		}
		value, ok := fields[field.Name]
		if !ok {
			continue
		}
		if field.Name == accessName {
			value = access
		}
		vars["cred."+field.Name] = value
	}
	if _, ok := vars["cred."+accessName]; !ok && access != "" {
		vars["cred."+accessName] = access
	}
	id := jsplugin.AccountRegionID(spec.Regions, fields)
	if id == "" {
		id = spec.DefaultRegion
	}
	for _, region := range spec.Regions {
		if region.ID != id {
			continue
		}
		for key, value := range region.Attrs {
			vars["region."+key] = value
		}
		break
	}
	return vars
}

func parseAccountCredentialFields(raw string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.UseNumber()
	var generic map[string]any
	if err := decoder.Decode(&generic); err != nil {
		return nil, fmt.Errorf("decode account credential bundle: %w", err)
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("decode account credential bundle")
	}
	if generic == nil {
		return nil, errors.New("decode account credential bundle")
	}
	fields := make(map[string]string, len(generic))
	for key, value := range generic {
		switch typed := value.(type) {
		case string:
			fields[key] = typed
		case json.Number:
			fields[key] = typed.String()
		case nil:
			continue
		default:
			return nil, fmt.Errorf("credential field %q is not a string", key)
		}
	}
	return fields, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func accountPoolCredentialFailover(credentialType string) bool {
	return store.IsAccountCredentialType(credentialType)
}

func accountPoolSyncProvider(siteType string) string {
	switch {
	case isGrokSite(siteType):
		return "grok"
	}
	siteType = strings.TrimSpace(siteType)
	if siteType == "" {
		return "account"
	}
	return siteType
}

func (h Handler) markAccountPoolAuthFailure(ctx context.Context, siteType string, credential store.SiteCredential, result gatewayAttemptResult) {
	if !store.IsAccountCredentialType(credential.CredentialType) {
		return
	}
	h.markAccountCredentialSyncFailed(ctx, credential, accountPoolSyncProvider(siteType), result)
}

type quotaExhaustedAccount struct {
	CredentialID uuid.UUID
	ResetAt      time.Time
}

type pluginQuotaWeight struct {
	value float64
	known bool
}

func (h Handler) preparePluginAccountCredentials(ctx context.Context, candidate routeengine.Candidate, credentials []store.GatewayCredential) ([]store.GatewayCredential, error) {
	ordered, cooled, ok := arrangePluginAccountCredentials(candidate.Site.SiteType, credentials, time.Now())
	if !ok {
		return credentials, nil
	}
	for _, item := range cooled {
		h.activateQuotaExhaustedCooldown(ctx, candidate.Site.ID, item)
	}
	if len(ordered) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return ordered, nil
}

func (h Handler) activateQuotaExhaustedCooldown(ctx context.Context, siteID uuid.UUID, item quotaExhaustedAccount) {
	input, ok := quotaExhaustedCooldownInput(siteID, item.CredentialID, item.ResetAt, time.Now())
	if !ok || h.router == nil {
		return
	}
	if _, err := h.router.ActivateCooldown(ctx, input); err != nil && h.logger != nil {
		h.logger.WarnContext(ctx, "failed to activate quota cooldown", "scope", "gateway", "site_id", siteID, "site_credential_id", item.CredentialID, "error", err)
	}
}

func arrangePluginAccountCredentials(siteType string, credentials []store.GatewayCredential, now time.Time) ([]store.GatewayCredential, []quotaExhaustedAccount, bool) {
	if isGrokSite(siteType) {
		return nil, nil, false
	}
	item, ok := site.LookupPluginSiteType(siteType)
	if !ok || !item.Enabled || item.Pool == nil {
		return nil, nil, false
	}
	kept := make([]store.GatewayCredential, 0, len(credentials))
	var cooled []quotaExhaustedAccount
	for _, credential := range credentials {
		if !store.IsAccountCredentialTypeOf(credential.Credential.CredentialType, item.Descriptor.SiteType) {
			kept = append(kept, credential)
			continue
		}
		resetAt, exhausted := accountQuotaExhaustedReset(credential.State.Usage, now)
		if !exhausted {
			kept = append(kept, credential)
			continue
		}
		cooled = append(cooled, quotaExhaustedAccount{CredentialID: credential.Credential.ID, ResetAt: resetAt})
	}
	if item.Pool.Selection == "quota_weighted" {
		sortPluginAccountsByQuota(kept)
	}
	return kept, cooled, true
}

func sortPluginAccountsByQuota(credentials []store.GatewayCredential) {
	type weighted struct {
		credential store.GatewayCredential
		weight     pluginQuotaWeight
	}
	items := make([]weighted, len(credentials))
	for i, credential := range credentials {
		items[i] = weighted{credential: credential, weight: pluginAccountQuotaWeight(credential.State.Usage)}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i].weight, items[j].weight
		if left.known != right.known {
			return left.known
		}
		return left.value > right.value
	})
	for i := range items {
		credentials[i] = items[i].credential
	}
}

func pluginAccountQuotaWeight(raw store.JSON) pluginQuotaWeight {
	entries := accountStateEntries(raw)
	if len(entries) == 0 {
		return pluginQuotaWeight{}
	}
	sum := 0.0
	sawNumeric := false
	unlimitedOnly := true
	for _, entry := range entries {
		if entry.Unlimited {
			continue
		}
		unlimitedOnly = false
		if entry.Remaining == nil || math.IsNaN(*entry.Remaining) || math.IsInf(*entry.Remaining, 0) {
			continue
		}
		sawNumeric = true
		sum += *entry.Remaining
	}
	if unlimitedOnly {
		return pluginQuotaWeight{value: math.Inf(1), known: true}
	}
	if !sawNumeric {
		return pluginQuotaWeight{known: true}
	}
	return pluginQuotaWeight{value: sum, known: true}
}

func accountQuotaExhaustedReset(raw store.JSON, now time.Time) (time.Time, bool) {
	var latest time.Time
	for _, entry := range accountStateEntries(raw) {
		if entry.Unlimited || entry.Remaining == nil || *entry.Remaining > 0 || entry.ResetAt == nil {
			continue
		}
		if math.IsNaN(*entry.Remaining) || math.IsInf(*entry.Remaining, 0) {
			continue
		}
		resetAt, ok := parseResetAt(*entry.ResetAt)
		if !ok || !resetAt.After(now) {
			continue
		}
		if resetAt.After(latest) {
			latest = resetAt
		}
	}
	if latest.IsZero() {
		return time.Time{}, false
	}
	return latest, true
}

func accountStateEntries(raw store.JSON) []site.QuotaProbeEntry {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		Entries []site.QuotaProbeEntry `json:"entries"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return payload.Entries
}

func parseResetAt(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func quotaExhaustedCooldownInput(siteID, credentialID uuid.UUID, resetAt, now time.Time) (routeengine.CooldownInput, bool) {
	if credentialID == uuid.Nil || !resetAt.After(now) {
		return routeengine.CooldownInput{}, false
	}
	until := resetAt
	maxUntil := now.Add(pluginQuotaExhaustedMax)
	if until.After(maxUntil) {
		until = maxUntil
	}
	duration := until.Sub(now)
	if duration <= 0 {
		return routeengine.CooldownInput{}, false
	}
	id := credentialID
	return routeengine.CooldownInput{
		SiteID:           siteID,
		SiteCredentialID: &id,
		Scope:            "credential",
		Source:           "gateway",
		Reason:           pluginQuotaExhaustedReason,
		Duration:         duration,
		Metadata: map[string]any{
			"reset_at": resetAt.UTC().Format(time.RFC3339Nano),
		},
	}, true
}
