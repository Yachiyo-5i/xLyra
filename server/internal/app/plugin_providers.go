package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"sync"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

// siteProviderCurrent, when set by the router, drops an apply whose registry
// is no longer the manager's current one so a newer reload wins.
var (
	siteProviderMu      sync.Mutex
	siteProviderCurrent func() *jsplugin.Registry
	siteProviderLast    siteProviderApplyState
)

type siteProviderApplyState struct {
	specs   map[string]json.RawMessage
	modules []adapter.Module
	types   []site.PluginSiteType
}

type preparedSiteProvider struct {
	pluginID string
	siteType string
	spec     json.RawMessage
	module   adapter.Module
	item     site.PluginSiteType
}

// applySiteProviders replaces the process-wide plugin site types, protocol
// overrides, and this service's dynamic adapter modules from the enabled
// site_provider plugins. An empty or nil registry clears all three. Site types
// are published last so a lookup cannot succeed before the adapter and the
// protocol spec exist. A plugin that fails to convert is skipped. Two plugins
// that declare one site type keep the first in plugin-id order.
func applySiteProviders(registry *jsplugin.Registry, siteSvc *site.Service, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	siteProviderMu.Lock()
	defer siteProviderMu.Unlock()
	if siteProviderCurrent != nil && siteProviderCurrent() != registry {
		return
	}
	if siteSvc == nil {
		logger.Error("site provider snapshot was not updated", "error", errors.New("site service is nil"))
		return
	}
	specs, modules, items := collectSiteProviders(registry, logger)
	if err := gateway.SetRuntimeProviderSpecs(specs); err != nil {
		logger.Error("site provider snapshot was not updated", "error", err)
		return
	}
	if err := siteSvc.SetPluginAdapterModules(modules); err != nil {
		logger.Error("site provider snapshot was not updated", "error", err)
		restoreSiteProviders(siteSvc, logger)
		return
	}
	if err := site.SetPluginSiteTypes(items); err != nil {
		logger.Error("site provider snapshot was not updated", "error", err)
		restoreSiteProviders(siteSvc, logger)
		return
	}
	siteProviderLast = siteProviderApplyState{specs: specs, modules: modules, types: items}
}

func restoreSiteProviders(siteSvc *site.Service, logger *slog.Logger) {
	if err := gateway.SetRuntimeProviderSpecs(siteProviderLast.specs); err != nil {
		logger.Error("site provider snapshot restore failed", "error", err)
	}
	if err := siteSvc.SetPluginAdapterModules(siteProviderLast.modules); err != nil {
		logger.Error("site provider snapshot restore failed", "error", err)
	}
	if err := site.SetPluginSiteTypes(siteProviderLast.types); err != nil {
		logger.Error("site provider snapshot restore failed", "error", err)
	}
}

func collectSiteProviders(registry *jsplugin.Registry, logger *slog.Logger) (map[string]json.RawMessage, []adapter.Module, []site.PluginSiteType) {
	var plugins []*jsplugin.Plugin
	if registry != nil {
		plugins = registry.ByKind(jsplugin.KindSiteProvider)
	}
	specs := map[string]json.RawMessage{}
	var modules []adapter.Module
	var items []site.PluginSiteType
	seen := make(map[string]string)
	for _, plugin := range plugins {
		prepared, err := convertSiteProvider(plugin)
		if err != nil {
			logger.Warn("site provider plugin skipped", "plugin_id", siteProviderPluginID(plugin), "error", err)
			continue
		}
		if previous, ok := seen[prepared.siteType]; ok {
			logger.Warn("site provider plugin skipped", "plugin_id", prepared.pluginID, "error", fmt.Errorf("site type %q is already declared by %s", prepared.siteType, previous))
			continue
		}
		seen[prepared.siteType] = prepared.pluginID
		specs[prepared.siteType] = prepared.spec
		modules = append(modules, prepared.module)
		items = append(items, prepared.item)
	}
	return specs, modules, items
}

func convertSiteProvider(plugin *jsplugin.Plugin) (preparedSiteProvider, error) {
	if plugin == nil {
		return preparedSiteProvider{}, errors.New("plugin is nil")
	}
	section := plugin.Manifest.SiteProvider
	siteType := strings.ToLower(strings.TrimSpace(section.SiteType))
	if conflictsWithBuiltinSiteType(siteType) {
		return preparedSiteProvider{}, fmt.Errorf("site type %q conflicts with a built-in site type", siteType)
	}
	if err := site.ValidatePluginSiteTypeID(siteType); err != nil {
		return preparedSiteProvider{}, err
	}
	if err := officialBaseHostAllowed(section.Protocol.OfficialBaseURL, section.Hosts); err != nil {
		return preparedSiteProvider{}, err
	}
	spec, err := siteProviderSpecJSON(section)
	if err != nil {
		return preparedSiteProvider{}, err
	}
	if err := gateway.ValidateRuntimeProviderSpec(siteType, spec); err != nil {
		return preparedSiteProvider{}, err
	}
	displayName := siteProviderText(section.Display.Name)
	module, err := adapter.NewGenericProvider(adapter.GenericProviderConfig{
		SiteType:        siteType,
		DisplayName:     displayName,
		OfficialBaseURL: section.Protocol.OfficialBaseURL,
		ModelsPath:      section.Models.Path,
		Auth:            section.Models.Auth,
		EndpointTypes:   section.Protocol.DefaultEndpointTypes,
		AllowedHosts:    append([]string(nil), section.Hosts...),
	})
	if err != nil {
		return preparedSiteProvider{}, err
	}
	return preparedSiteProvider{
		pluginID: plugin.Manifest.ID,
		siteType: siteType,
		spec:     spec,
		module:   module,
		item: site.PluginSiteType{
			Descriptor: site.SiteTypeDescriptor{
				SiteType:           siteType,
				DisplayName:        displayName,
				Subtitle:           siteProviderText(section.Display.Subtitle),
				CredentialKey:      siteType,
				RequiresBaseURL:    false,
				OfficialBaseURL:    section.Protocol.OfficialBaseURL,
				ShowInCreateDialog: true,
				IconURL:            "",
			},
			PluginID: plugin.Manifest.ID,
			Enabled:  true,
			Hosts:    append([]string(nil), section.Hosts...),
		},
	}, nil
}

func siteProviderSpecJSON(section jsplugin.SiteProviderSection) (json.RawMessage, error) {
	payload := map[string]any{
		"protocol":          section.Protocol.Kind,
		"official_base_url": section.Protocol.OfficialBaseURL,
		"path":              section.Protocol.Path,
	}
	rawParams := bytes.TrimSpace(section.Protocol.RequestParams)
	if len(rawParams) > 0 && !bytes.Equal(rawParams, []byte("null")) {
		if !json.Valid(rawParams) {
			return nil, errors.New("request_params is not valid JSON")
		}
		payload["request_params"] = json.RawMessage(append([]byte(nil), rawParams...))
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func officialBaseHostAllowed(raw string, hosts []string) error {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return fmt.Errorf("official base url %q must be an https URL with a host", raw)
	}
	host := strings.ToLower(parsed.Hostname())
	for _, item := range hosts {
		if item == host {
			return nil
		}
	}
	return fmt.Errorf("official base url host %q is not in hosts", host)
}

func conflictsWithBuiltinSiteType(id string) bool {
	desc, ok := site.LookupSiteType(id)
	if !ok {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(id))
	return desc.SiteType == key && !strings.Contains(desc.SiteType, "/")
}

func siteProviderPluginID(plugin *jsplugin.Plugin) string {
	if plugin == nil {
		return ""
	}
	return plugin.Manifest.ID
}

// siteProviderText picks zh, then en, then the first non-empty value in key order.
func siteProviderText(text jsplugin.LocalizedText) string {
	if value := strings.TrimSpace(text["zh"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(text["en"]); value != "" {
		return value
	}
	keys := make([]string, 0, len(text))
	for key := range text {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if value := strings.TrimSpace(text[key]); value != "" {
			return value
		}
	}
	return ""
}
