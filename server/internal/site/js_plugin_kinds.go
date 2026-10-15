package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

// Plugin kinds other than quota_probe and protocol, as seen from the site service.
//
//	model_list, credential_check, pricing_parse  bound to one site by an admin
//	site_detect, model_metadata                  apply to every site once enabled
//
// Everything here is opt-in: with no plugin bound or enabled the code paths
// behave exactly as before.

const (
	maxPluginPricingBody = 4 << 20
	pluginPricingTimeout = 30 * time.Second
)

// boundSitePlugin returns the enabled plugin an admin bound to the site for a
// per-site kind. A binding whose plugin is no longer enabled is ignored, so
// disabling or uninstalling a plugin returns the site to the default behavior.
func (s *Service) boundSitePlugin(item store.Site, kind string) (*jsplugin.Plugin, bool) {
	id := SitePluginID(GatewayConfigFromSiteMeta(item.Meta), kind)
	if id == "" {
		return nil, false
	}
	registry := s.activeJSRegistry()
	if registry == nil {
		return nil, false
	}
	plugin, ok := registry.ByPluginID(id)
	if !ok || plugin.Manifest.Kind != kind {
		return nil, false
	}
	return plugin, true
}

// SitePluginID returns the plugin id bound to a site for a kind, or "".
func SitePluginID(cfg *GatewayConfig, kind string) string {
	if cfg == nil {
		return ""
	}
	return cfg.Plugins[kind]
}

// driveStepped runs a model_list, credential_check or site_detect plugin to
// completion. The host sends every request and adds the credential; secret is
// empty for kinds that take none, and then no Authorization header is sent.
func driveStepped(ctx context.Context, client *http.Client, plugin *jsplugin.Plugin, probeCtx jsplugin.ProbeContext, secret string) (result any, err error) {
	defer func() {
		if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
			breaker.RecordProbeResult(plugin, err == nil)
		}
	}()
	mode, defaultBase := plugin.Manifest.BaseURLSettings()
	canonical, err := canonicalFromMode(mode, defaultBase, probeCtx.BaseURL)
	if err != nil {
		return nil, err
	}
	probeCtx.BaseURL = canonical
	httpClient := http.Client{Timeout: quotaProbeRequestTimeout}
	if client != nil {
		httpClient = *client
	}
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if err := jsplugin.SameOrigin(canonical, req.URL.String()); err != nil {
			return &redirectRejectedError{err: err}
		}
		return nil
	}
	var steps []jsplugin.ProbeStep
	for step := 0; step < jsplugin.MaxProbeSteps; step++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		decision, callErr := plugin.CallStepped(ctx, probeCtx, steps)
		logs, dropped := decision.Logs, decision.DroppedLogs
		if call, ok := callErr.(*jsplugin.CallError); ok {
			logs, dropped = call.Logs, call.DroppedLogs
		}
		emitPluginLogs(ctx, plugin, step, logs, dropped)
		if callErr != nil {
			logProbeFailure(plugin, step, callErr)
			return nil, callErr
		}
		if decision.Error != "" {
			return nil, fmt.Errorf("plugin %s: %s", plugin.Manifest.ID, decision.Error)
		}
		if decision.Result != nil {
			return decision.Result, nil
		}
		if decision.Request == nil || step == jsplugin.MaxProbeSteps-1 {
			return nil, fmt.Errorf("plugin %s exceeded %d steps", plugin.Manifest.ID, jsplugin.MaxProbeSteps)
		}
		response, err := doProbeHTTP(ctx, &httpClient, canonical, secret, decision.Request)
		if err != nil {
			return nil, err
		}
		steps = append(steps, jsplugin.ProbeStep{Request: *decision.Request, Response: response})
	}
	return nil, fmt.Errorf("plugin %s exceeded %d steps", plugin.Manifest.ID, jsplugin.MaxProbeSteps)
}

// ---- model_list ----

// pluginModelLister adapts a model_list plugin to the interface the model
// sync uses, so the sync code does not care where the list comes from.
type pluginModelLister struct {
	plugin *jsplugin.Plugin
}

func (l pluginModelLister) ListModels(ctx context.Context, site adapter.SiteConfig, apiKey string) ([]adapter.Model, error) {
	result, err := driveStepped(ctx, site.Client, l.plugin, jsplugin.ProbeContext{
		SiteType:       site.SiteType,
		BaseURL:        site.BaseURL,
		CredentialType: defaultCredentialType,
		Now:            time.Now().UnixMilli(),
	}, apiKey)
	if err != nil {
		return nil, err
	}
	list, ok := result.(*jsplugin.ModelListResult)
	if !ok {
		return nil, fmt.Errorf("plugin %s returned an unexpected result", l.plugin.Manifest.ID)
	}
	models := make([]adapter.Model, 0, len(list.Models))
	for _, entry := range list.Models {
		capabilities := map[string]any{}
		for key, value := range entry.Capabilities {
			capabilities[key] = value
		}
		display := entry.DisplayName
		if display == "" {
			display = entry.Name
		}
		models = append(models, adapter.Model{UpstreamName: entry.Name, DisplayName: display, Capabilities: capabilities})
	}
	return models, nil
}

// modelListerFor returns the plugin bound to the site if there is one, and the
// adapter's own lister otherwise.
func (s *Service) modelListerFor(item store.Site, module adapter.Module) (adapter.GatewayModelLister, bool) {
	if plugin, ok := s.boundSitePlugin(item, jsplugin.KindModelList); ok {
		return pluginModelLister{plugin: plugin}, true
	}
	return adapter.AsGatewayModelLister(module)
}

// ---- credential_check ----

// pluginCredentialValidator adapts a credential_check plugin to
// adapter.CredentialValidator. A rejected key becomes an error that classifies
// as credential_invalid; an unanswered check becomes a transient one, so a site
// outage is never mistaken for a bad key.
type pluginCredentialValidator struct {
	plugin *jsplugin.Plugin
}

func (v pluginCredentialValidator) ValidateCredentials(ctx context.Context, site adapter.SiteConfig, apiKey string) error {
	result, err := driveStepped(ctx, site.Client, v.plugin, jsplugin.ProbeContext{
		SiteType:       site.SiteType,
		BaseURL:        site.BaseURL,
		CredentialType: defaultCredentialType,
		Now:            time.Now().UnixMilli(),
	}, apiKey)
	if err != nil {
		return err
	}
	check, ok := result.(*jsplugin.CredentialCheckResult)
	if !ok {
		return fmt.Errorf("plugin %s returned an unexpected result", v.plugin.Manifest.ID)
	}
	message := strings.TrimSpace(check.Message)
	switch check.Status {
	case "ok":
		return nil
	case "invalid":
		return credentialCheckError(http.StatusUnauthorized, upstream.FailureCredentialInvalid, nonEmpty(message, "credential rejected"))
	default:
		return credentialCheckError(http.StatusServiceUnavailable, upstream.FailureTransient, nonEmpty(message, "site could not answer"))
	}
}

// credentialCheckError carries the plugin's verdict as an already classified
// upstream error, so the callers that branch on the class (disabling a key,
// retrying later) do not have to guess it from the text.
func credentialCheckError(statusCode int, class upstream.FailureClass, message string) error {
	return &upstream.HTTPError{
		Prefix: "credential check returned",
		Body:   []byte(message),
		Failure: upstream.Failure{
			Class:      class,
			StatusCode: statusCode,
			Message:    message,
		},
	}
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

// credentialValidatorFor returns the plugin bound to the site if there is one,
// and the adapter's own validator otherwise.
func (s *Service) credentialValidatorFor(item store.Site, module adapter.Module) (adapter.CredentialValidator, bool) {
	if plugin, ok := s.boundSitePlugin(item, jsplugin.KindCredentialCheck); ok {
		return pluginCredentialValidator{plugin: plugin}, true
	}
	return adapter.AsCredentialValidator(module)
}

// runPluginHealthCheck runs the bound credential_check plugin against the
// site's first usable API key.
func (s *Service) runPluginHealthCheck(ctx context.Context, item store.Site, plugin *jsplugin.Plugin, start time.Time, check siteHealthCheck) siteHealthCheck {
	check.endpoint = "credential_check " + plugin.Manifest.ID
	check.metadata["plugin"] = plugin.Manifest.ID
	_, credential, err := s.siteWithAPIKeyCredential(ctx, item.ID)
	if err != nil {
		check.latency = time.Since(start)
		check.errorType = "missing_credential"
		check.message = err.Error()
		return check
	}
	err = pluginCredentialValidator{plugin: plugin}.ValidateCredentials(ctx, s.toAdapterSite(ctx, item), credential.Secret)
	check.latency = time.Since(start)
	if err == nil {
		check.success = true
		check.message = "ok"
		return check
	}
	failure := upstream.ClassifyError(err)
	switch {
	case failure.CredentialInvalid():
		check.errorType = "validation_failed"
	case failure.Limited():
		check.success = true
		check.message = "ok"
		check.metadata["credential_limited"] = healthFailureMetadata(failure)
		return check
	default:
		check.errorType = "upstream_unreachable"
	}
	check.message = err.Error()
	return check
}

// ---- site_detect ----

// detectWithPlugins runs every enabled site_detect plugin and returns the most
// confident match. A plugin must name a site type xLyra already supports;
// anything else is ignored, because a plugin cannot add a site type.
func (s *Service) detectWithPlugins(ctx context.Context, baseURL string) (DetectResult, bool) {
	registry := s.activeJSRegistry()
	if registry == nil || s.httpClients == nil {
		return DetectResult{}, false
	}
	plugins := registry.ByKind(jsplugin.KindSiteDetect)
	if len(plugins) == 0 {
		return DetectResult{}, false
	}
	client, err := s.httpClients.Client(httpclient.DefaultProfile())
	if err != nil {
		return DetectResult{}, false
	}
	var best DetectResult
	for _, plugin := range plugins {
		result, err := driveStepped(ctx, client, plugin, jsplugin.ProbeContext{BaseURL: baseURL, Now: time.Now().UnixMilli()}, "")
		if err != nil {
			slog.Default().Debug("site_detect plugin failed", "plugin_id", plugin.Manifest.ID, "error", err)
			continue
		}
		detected, ok := result.(*jsplugin.SiteDetectResult)
		if !ok || !detected.Matched {
			continue
		}
		siteType := normalizeSiteType(detected.SiteType)
		if _, supported := s.adapters.ModuleForSiteType(siteType); !supported {
			slog.Default().Warn("site_detect plugin named an unsupported site type", "plugin_id", plugin.Manifest.ID, "site_type", detected.SiteType)
			continue
		}
		confidence := 0.5
		if detected.Confidence != nil {
			confidence = *detected.Confidence
		}
		if best.Matched && confidence <= best.Confidence {
			continue
		}
		features := map[string]any{}
		for key, value := range detected.Features {
			features[key] = value
		}
		features["plugin"] = plugin.Manifest.ID
		best = DetectResult{SiteType: siteType, Matched: true, Confidence: confidence, Features: features}
	}
	return best, best.Matched
}

// ---- model_metadata ----

// applyModelMetadataPlugins lets enabled model_metadata plugins fill in what is
// still missing on a model. Plugins have the lowest priority: they never
// replace a value the upstream, the curated list or the catalog already gave.
func (s *Service) applyModelMetadataPlugins(ctx context.Context, item store.Site, model adapter.Model) adapter.Model {
	registry := s.activeJSRegistry()
	if registry == nil || strings.TrimSpace(model.UpstreamName) == "" {
		return model
	}
	for _, plugin := range registry.ByKind(jsplugin.KindModelMetadata) {
		result, err := plugin.CallModelMetadata(ctx,
			jsplugin.ModelMetadataContext{SiteType: item.SiteType},
			jsplugin.ModelMetadataInput{Models: []string{model.UpstreamName}})
		if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
			breaker.RecordProbeResult(plugin, err == nil)
		}
		if err != nil {
			slog.Default().Debug("model_metadata plugin failed", "plugin_id", plugin.Manifest.ID, "error", err)
			continue
		}
		for _, entry := range result.Models {
			if entry.ID != model.UpstreamName {
				continue
			}
			if model.DisplayName == "" || model.DisplayName == model.UpstreamName {
				if entry.Name != "" {
					model.DisplayName = entry.Name
				}
			}
			if len(entry.Capabilities) > 0 && model.Capabilities == nil {
				model.Capabilities = map[string]any{}
			}
			for key, value := range entry.Capabilities {
				if _, present := model.Capabilities[key]; !present {
					model.Capabilities[key] = value
				}
			}
		}
	}
	return model
}

// ---- pricing_parse ----

// fetchPluginPricing downloads the price table a pricing_parse plugin declared
// in its manifest and returns the plugin's reading of it. The host makes the
// request; the plugin only sees the JSON that came back.
func (s *Service) fetchPluginPricing(ctx context.Context, item store.Site, plugin *jsplugin.Plugin) (adapter.PricingSnapshot, error) {
	_, credential, err := s.siteWithAPIKeyCredential(ctx, item.ID)
	if err != nil {
		return adapter.PricingSnapshot{}, err
	}
	mode, defaultBase := plugin.Manifest.BaseURLSettings()
	canonical, err := canonicalFromMode(nonEmpty(mode, "as_is"), defaultBase, item.BaseURL)
	if err != nil {
		return adapter.PricingSnapshot{}, err
	}
	client, err := s.httpClientForSite(ctx, item, false)
	if err != nil {
		return adapter.PricingSnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, pluginPricingTimeout)
	defer cancel()
	payload, err := fetchJSON(ctx, client, canonical, plugin.Manifest.Site.PricingPath, credential.Secret)
	if err != nil {
		return adapter.PricingSnapshot{}, err
	}
	parsed, err := plugin.CallPricingParse(ctx, jsplugin.PricingParseContext{SiteType: item.SiteType}, payload)
	if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
		breaker.RecordProbeResult(plugin, err == nil)
	}
	if err != nil {
		return adapter.PricingSnapshot{}, err
	}
	return pricingSnapshotFromPlugin(parsed, payload), nil
}

func fetchJSON(ctx context.Context, client *http.Client, base, path, secret string) (any, error) {
	target, err := jsplugin.ResolveProbeURL(base, path, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", jsplugin.KindURLRejected, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/json")
	httpClient := *client
	httpClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return jsplugin.SameOrigin(base, next.URL.String())
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPluginPricingBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPluginPricingBody {
		return nil, fmt.Errorf("%s: pricing response is too large", jsplugin.KindResponseTooLarge)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("pricing endpoint returned %d", resp.StatusCode)
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("pricing endpoint did not return JSON: %w", err)
	}
	return payload, nil
}

func pricingSnapshotFromPlugin(parsed jsplugin.PricingParseResult, raw any) adapter.PricingSnapshot {
	snapshot := adapter.PricingSnapshot{Raw: raw}
	for _, group := range parsed.Groups {
		entry := adapter.PricingGroup{GroupName: group.Name, DisplayName: group.DisplayName, IsAuto: group.Auto, Ratio: 1}
		if group.Ratio != nil {
			entry.Ratio = *group.Ratio
		}
		snapshot.Groups = append(snapshot.Groups, entry)
	}
	for _, item := range parsed.Items {
		entry := adapter.ModelPricing{
			ModelName:   item.Model,
			DisplayName: item.DisplayName,
			GroupName:   item.Group,
			BillingType: item.BillingType,
			Currency:    item.Currency,
			GroupRatio:  1,
		}
		setRatio := func(dst *float64, has *bool, value *float64) {
			if value != nil {
				*dst, *has = *value, true
			}
		}
		if item.GroupRatio != nil {
			entry.GroupRatio = *item.GroupRatio
		}
		setRatio(&entry.ModelRatio, &entry.HasModelRatio, item.ModelRatio)
		setRatio(&entry.CompletionRatio, &entry.HasCompletionRatio, item.CompletionRatio)
		setRatio(&entry.CacheRatio, &entry.HasCacheRatio, item.CacheRatio)
		setRatio(&entry.CreateCacheRatio, &entry.HasCreateCacheRatio, item.CreateCacheRatio)
		setRatio(&entry.CreateCache1hRatio, &entry.HasCreateCache1hRatio, item.CreateCache1hRatio)
		setRatio(&entry.ImageRatio, &entry.HasImageRatio, item.ImageRatio)
		setRatio(&entry.AudioRatio, &entry.HasAudioRatio, item.AudioRatio)
		setRatio(&entry.AudioCompletionRatio, &entry.HasAudioCompletionRatio, item.AudioCompletionRatio)
		setRatio(&entry.ModelPrice, &entry.HasModelPrice, item.ModelPrice)
		setRatio(&entry.InputValue, &entry.HasInputValue, item.InputValue)
		setRatio(&entry.OutputValue, &entry.HasOutputValue, item.OutputValue)
		setRatio(&entry.PerRequestValue, &entry.HasPerRequestValue, item.PerRequestValue)
		snapshot.Items = append(snapshot.Items, entry)
	}
	return snapshot
}

// syncPluginPricing is the pricing step of a model refresh for a site with a
// pricing_parse plugin bound. bound is false when there is nothing to do.
func (s *Service) syncPluginPricing(ctx context.Context, item store.Site, models []store.SiteModel, now time.Time) (snapshot adapter.PricingSnapshot, groups []store.SitePricingGroup, pricings []store.SiteModelPricing, bound bool, err error) {
	plugin, ok := s.boundSitePlugin(item, jsplugin.KindPricingParse)
	if !ok {
		return adapter.PricingSnapshot{}, nil, nil, false, nil
	}
	snapshot, err = s.fetchPluginPricing(ctx, item, plugin)
	if err != nil {
		return adapter.PricingSnapshot{}, nil, nil, true, fmt.Errorf("pricing plugin %s: %w", plugin.Manifest.ID, err)
	}
	byName := make(map[string]store.SiteModel, len(models))
	for _, model := range models {
		byName[model.UpstreamName] = model
		byName[model.DisplayName] = model
	}
	groups, pricings, err = syncPricingState(ctx, item.ID, item.SiteType, snapshot, byName,
		store.NewSitePricingGroupRepository(s.db.DB()), store.NewSiteModelPricingRepository(s.db.DB()), now)
	if err != nil {
		return snapshot, nil, nil, true, fmt.Errorf("pricing plugin %s: %w", plugin.Manifest.ID, err)
	}
	return snapshot, groups, pricings, true, nil
}

// PreviewPluginPricing fetches and parses a site's price table without saving
// anything, so an admin can read the result before binding the plugin.
func (s *Service) PreviewPluginPricing(ctx context.Context, siteID uuid.UUID, plugin *jsplugin.Plugin) (adapter.PricingSnapshot, error) {
	item, err := s.Get(ctx, siteID)
	if err != nil {
		return adapter.PricingSnapshot{}, err
	}
	return s.fetchPluginPricing(ctx, item, plugin)
}
