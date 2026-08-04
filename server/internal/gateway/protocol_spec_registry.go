package gateway

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"

	"xlyra/server/internal/protocolspec"
	routeengine "xlyra/server/internal/router"
)

type protocolSpecRegistryConfig struct {
	Version   int                               `json:"version"`
	Protocols map[string]protocolSpecDefinition `json:"protocols"`
	Providers map[string]providerSpecDefinition `json:"providers"`
	Models    map[string]modelSpecDefinition    `json:"models"`
}

type protocolSpecDefinition struct {
	BaseProtocol  string                  `json:"base_protocol,omitempty"`
	Events        map[string]eventMapping `json:"events,omitempty"`
	ParamMappings []paramMappingConfig    `json:"param_mappings,omitempty"`
	RequestParams requestParamPolicy      `json:"request_params,omitempty"`
	Capabilities  map[string]bool         `json:"capabilities,omitempty"`
	Reasoning     map[string]string       `json:"reasoning,omitempty"`
	Documentation []string                `json:"documentation,omitempty"`
}

type providerSpecDefinition struct {
	Protocol              string                                 `json:"protocol,omitempty"`
	OfficialBaseURL       string                                 `json:"official_base_url,omitempty"`
	BasePath              string                                 `json:"base_path,omitempty"`
	Path                  string                                 `json:"path,omitempty"`
	Paths                 map[string]protocolPathDefinition      `json:"paths,omitempty"`
	AlternateProtocols    map[string]alternateProtocolDefinition `json:"alternate_protocols,omitempty"`
	RequestParams         requestParamPolicy                     `json:"request_params,omitempty"`
	ProtocolRequestParams map[string]requestParamPolicy          `json:"protocol_request_params,omitempty"`
	Capabilities          map[string]bool                        `json:"capabilities,omitempty"`
	Reasoning             map[string]string                      `json:"reasoning,omitempty"`
	Documentation         []string                               `json:"documentation,omitempty"`
}

type modelSpecDefinition struct {
	Provider           string                                 `json:"provider,omitempty"`
	OfficialBaseURL    string                                 `json:"official_base_url,omitempty"`
	BasePath           string                                 `json:"base_path,omitempty"`
	Path               string                                 `json:"path,omitempty"`
	Paths              map[string]protocolPathDefinition      `json:"paths,omitempty"`
	AlternateProtocols map[string]alternateProtocolDefinition `json:"alternate_protocols,omitempty"`
	RequestParams      requestParamPolicy                     `json:"request_params,omitempty"`
	Capabilities       map[string]bool                        `json:"capabilities,omitempty"`
	Reasoning          map[string]string                      `json:"reasoning,omitempty"`
	Documentation      []string                               `json:"documentation,omitempty"`
}

type eventMapping struct {
	Name      string            `json:"name,omitempty"`
	DataPath  string            `json:"data_path,omitempty"`
	DeltaPath string            `json:"delta_path,omitempty"`
	Condition map[string]string `json:"condition,omitempty"`
}

type paramMappingConfig struct {
	Canonical string `json:"canonical,omitempty"`
	Upstream  string `json:"upstream,omitempty"`
}

type requestParamPolicy struct {
	Supported                      []string                   `json:"supported,omitempty"`
	Unsupported                    []string                   `json:"unsupported,omitempty"`
	UnsupportedWithImageGeneration []string                   `json:"unsupported_with_image_generation,omitempty"`
	AllowedValues                  map[string][]any           `json:"allowed_values,omitempty"`
	Defaults                       map[string]any             `json:"defaults,omitempty"`
	Forced                         map[string]any             `json:"forced,omitempty"`
	Fixed                          map[string]any             `json:"fixed,omitempty"`
	Validation                     map[string]paramValidation `json:"validation,omitempty"`
}

type paramValidation struct {
	Type string   `json:"type,omitempty"`
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
}

type protocolPathDefinition struct {
	BasePath string `json:"base_path,omitempty"`
	Path     string `json:"path,omitempty"`
}

type alternateProtocolDefinition struct {
	Protocol               string `json:"protocol,omitempty"`
	BaseURL                string `json:"base_url,omitempty"`
	BasePath               string `json:"base_path,omitempty"`
	Path                   string `json:"path,omitempty"`
	OfficialOnly           bool   `json:"official_only,omitempty"`
	WhenDownstreamProtocol string `json:"when_downstream_protocol,omitempty"`
}

type resolvedProtocolSpec struct {
	Protocol           canonicalProtocol
	Provider           string
	ModelPattern       string
	OfficialBaseURL    string
	BasePath           string
	Path               string
	AlternateProtocols map[string]alternateProtocolDefinition
	Events             map[string]eventMapping
	ParamMappings      []paramMappingConfig
	RequestParams      requestParamPolicy
	Capabilities       map[string]bool
	Reasoning          map[string]string
	Documentation      []string
}

var (
	protocolSpecRegistryOnce sync.Once
	protocolSpecRegistryData protocolSpecRegistryConfig
	protocolSpecRegistryErr  error
)

func loadProtocolSpecRegistry() (protocolSpecRegistryConfig, error) {
	protocolSpecRegistryOnce.Do(func() {
		raw := protocolspec.Data()
		if err := json.Unmarshal(raw, &protocolSpecRegistryData); err != nil {
			protocolSpecRegistryErr = fmt.Errorf("decode protocol specs: %w", err)
			return
		}
		if protocolSpecRegistryData.Protocols == nil {
			protocolSpecRegistryData.Protocols = map[string]protocolSpecDefinition{}
		}
		if protocolSpecRegistryData.Providers == nil {
			protocolSpecRegistryData.Providers = map[string]providerSpecDefinition{}
		}
		if protocolSpecRegistryData.Models == nil {
			protocolSpecRegistryData.Models = map[string]modelSpecDefinition{}
		}
		if err := validateProtocolSpecRegistry(protocolSpecRegistryData); err != nil {
			protocolSpecRegistryErr = fmt.Errorf("validate protocol specs: %w", err)
			return
		}
		if err := protocolspec.Validate(); err != nil {
			protocolSpecRegistryErr = fmt.Errorf("validate model endpoint specs: %w", err)
		}
	})
	return protocolSpecRegistryData, protocolSpecRegistryErr
}

func effectiveProtocolSpec(protocol canonicalProtocol, candidate routeengine.Candidate) resolvedProtocolSpec {
	config, err := loadProtocolSpecRegistry()
	if err != nil {
		return resolvedProtocolSpec{Protocol: protocol}
	}

	model := strings.TrimSpace(candidate.Model.UpstreamName)
	modelPattern, modelDef := matchModelSpec(config.Models, model)
	providerKey := normalizeSpecKey(candidate.Site.SiteType)
	if providerKey == "" || genericOpenAICompatibleSiteType(providerKey) {
		providerKey = normalizeSpecKey(modelDef.Provider)
	}

	resolved := resolvedProtocolSpec{
		Protocol:           protocol,
		Provider:           providerKey,
		ModelPattern:       modelPattern,
		AlternateProtocols: map[string]alternateProtocolDefinition{},
		Events:             map[string]eventMapping{},
		Capabilities:       map[string]bool{},
		Reasoning:          map[string]string{},
		Documentation:      []string{},
	}

	protocolKey := normalizeSpecKey(string(protocol))
	if providerKey != "" {
		if providerDef, ok := config.Providers[providerKey]; ok && strings.TrimSpace(providerDef.Protocol) != "" && protocolKey == "" {
			protocolKey = normalizeSpecKey(providerDef.Protocol)
		}
	}
	mergeProtocolDefinition(&resolved, config, protocolKey, map[string]bool{})
	if providerKey != "" {
		if providerDef, ok := config.Providers[providerKey]; ok {
			mergeProviderDefinition(&resolved, providerDef)
		}
	}
	if modelPattern != "" {
		mergeModelDefinition(&resolved, modelDef)
	}
	return resolved
}

func protocolEventMapping(protocol canonicalProtocol, event canonicalStreamEventType, candidate routeengine.Candidate) (eventMapping, bool) {
	spec := effectiveProtocolSpec(protocol, candidate)
	mapping, ok := spec.Events[string(event)]
	return mapping, ok
}

func protocolEventName(protocol canonicalProtocol, event canonicalStreamEventType, fallback string, candidate routeengine.Candidate) string {
	if mapping, ok := protocolEventMapping(protocol, event, candidate); ok && strings.TrimSpace(mapping.Name) != "" {
		return strings.TrimSpace(mapping.Name)
	}
	return fallback
}

func protocolEventNameDefault(protocol canonicalProtocol, event canonicalStreamEventType, fallback string) string {
	return protocolEventName(protocol, event, fallback, routeengine.Candidate{})
}

func applyResolvedRequestPolicy(payload map[string]any, spec resolvedProtocolSpec) map[string]any {
	if payload == nil {
		return payload
	}
	for _, key := range spec.RequestParams.Unsupported {
		delete(payload, key)
	}
	if payloadHasImageGenerationTool(payload) {
		for _, key := range spec.RequestParams.UnsupportedWithImageGeneration {
			delete(payload, key)
		}
	}
	for key, allowed := range spec.RequestParams.AllowedValues {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if !valueInAllowedSet(value, allowed) {
			delete(payload, key)
		}
	}
	for key, value := range spec.RequestParams.Defaults {
		if _, ok := payload[key]; !ok {
			payload[key] = cloneSpecValue(value)
		}
	}
	for key, value := range spec.RequestParams.Fixed {
		if _, ok := payload[key]; ok {
			payload[key] = cloneSpecValue(value)
		}
	}
	for key, value := range spec.RequestParams.Forced {
		payload[key] = cloneSpecValue(value)
	}
	return payload
}

func applyRequestPolicyForCandidate(payload map[string]any, protocol canonicalProtocol, candidate routeengine.Candidate) map[string]any {
	return applyResolvedRequestPolicy(payload, effectiveProtocolSpec(protocol, candidate))
}

func validatePayloadParams(payload map[string]any, protocolName string, candidate routeengine.Candidate) error {
	if payload == nil {
		return nil
	}
	protocol := inferCanonicalProtocol(protocolName)
	spec := effectiveProtocolSpec(protocol, candidate)
	if len(spec.RequestParams.Validation) == 0 {
		return nil
	}
	for key, rule := range spec.RequestParams.Validation {
		value, ok := payload[key]
		if !ok {
			continue
		}
		if err := validateSingleParam(key, value, rule); err != nil {
			return err
		}
	}
	return nil
}

func validateSingleParam(key string, value any, rule paramValidation) error {
	switch rule.Type {
	case "number", "integer":
		num, ok := toFloat64ForValidation(value)
		if !ok {
			return nil
		}
		if rule.Min != nil && num < *rule.Min {
			return fmt.Errorf("parameter %q: value %v is below minimum %v", key, value, *rule.Min)
		}
		if rule.Max != nil && num > *rule.Max {
			return fmt.Errorf("parameter %q: value %v is above maximum %v", key, value, *rule.Max)
		}
	}
	return nil
}

func toFloat64ForValidation(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func inferCanonicalProtocol(protocolName string) canonicalProtocol {
	normalized := normalizeSpecKey(protocolName)
	switch {
	case strings.Contains(normalized, "anthropic_messages"):
		return canonicalProtocolAnthropicMessages
	case strings.Contains(normalized, "codex"):
		return canonicalProtocolCodexResponses
	case strings.Contains(normalized, "antigravity"):
		return canonicalProtocolAntigravity
	case strings.Contains(normalized, "google_gemini"):
		return canonicalProtocolGoogleGemini
	case strings.Contains(normalized, "responses"):
		return canonicalProtocolOpenAIResponses
	case strings.Contains(normalized, "audio_speech"):
		return canonicalProtocolOpenAIAudioSpeech
	case strings.Contains(normalized, "images"):
		return canonicalProtocolOpenAIImages
	case strings.Contains(normalized, "embeddings"):
		return canonicalProtocolOpenAIEmbeddings
	default:
		return canonicalProtocolOpenAIChat
	}
}

func protocolParamMappings(protocol canonicalProtocol, candidate routeengine.Candidate) []paramMapping {
	spec := effectiveProtocolSpec(protocol, candidate)
	result := make([]paramMapping, 0, len(spec.ParamMappings))
	for _, item := range spec.ParamMappings {
		canonical := strings.TrimSpace(item.Canonical)
		upstream := strings.TrimSpace(item.Upstream)
		if canonical == "" || upstream == "" {
			continue
		}
		result = append(result, paramMapping{CanonicalKey: canonical, UpstreamKey: upstream})
	}
	return result
}

func modelCapability(protocol canonicalProtocol, candidate routeengine.Candidate, capability string) bool {
	capability = strings.TrimSpace(capability)
	if capability == "" {
		return false
	}
	spec := effectiveProtocolSpec(protocol, candidate)
	return spec.Capabilities[capability]
}

func alternateProtocolForCandidate(downstream canonicalProtocol, candidate routeengine.Candidate) (alternateProtocolDefinition, bool) {
	downstreamKey := normalizeSpecKey(string(downstream))
	if downstreamKey == "" {
		return alternateProtocolDefinition{}, false
	}
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate)
	alt, ok := spec.AlternateProtocols[downstreamKey]
	if !ok {
		return alternateProtocolDefinition{}, false
	}
	if when := normalizeSpecKey(alt.WhenDownstreamProtocol); when != "" && when != downstreamKey {
		return alternateProtocolDefinition{}, false
	}
	if strings.TrimSpace(alt.Protocol) == "" {
		alt.Protocol = downstreamKey
	}
	if strings.TrimSpace(alt.BaseURL) == "" && strings.TrimSpace(alt.BasePath) == "" {
		alt.BaseURL = spec.OfficialBaseURL
	}
	if alt.OfficialOnly {
		siteType := normalizeSpecKey(candidate.Site.SiteType)
		if genericOpenAICompatibleSiteType(siteType) {
			return alternateProtocolDefinition{}, false
		}
		if siteType == "" || spec.Provider == "" || siteType != normalizeSpecKey(spec.Provider) {
			return alternateProtocolDefinition{}, false
		}
	}
	return alt, true
}

func mergeProtocolDefinition(resolved *resolvedProtocolSpec, config protocolSpecRegistryConfig, protocolKey string, seen map[string]bool) {
	if protocolKey == "" || seen[protocolKey] {
		return
	}
	seen[protocolKey] = true
	def, ok := config.Protocols[protocolKey]
	if !ok {
		return
	}
	if base := normalizeSpecKey(def.BaseProtocol); base != "" {
		mergeProtocolDefinition(resolved, config, base, seen)
	}
	mergeEvents(resolved.Events, def.Events)
	resolved.ParamMappings = append(resolved.ParamMappings, def.ParamMappings...)
	mergeRequestParamPolicy(&resolved.RequestParams, def.RequestParams)
	mergeBoolMap(resolved.Capabilities, def.Capabilities)
	mergeStringMap(resolved.Reasoning, def.Reasoning)
	resolved.Documentation = appendUniqueStrings(resolved.Documentation, def.Documentation...)
}

func mergeProviderDefinition(resolved *resolvedProtocolSpec, def providerSpecDefinition) {
	if strings.TrimSpace(def.OfficialBaseURL) != "" {
		resolved.OfficialBaseURL = strings.TrimSpace(def.OfficialBaseURL)
	}
	mergeProtocolPathDefinition(resolved, def.Protocol, def.BasePath, def.Path, def.Paths)
	mergeAlternateProtocols(resolved.AlternateProtocols, def.AlternateProtocols)
	mergeRequestParamPolicy(&resolved.RequestParams, def.RequestParams)
	if policy, ok := def.ProtocolRequestParams[normalizeSpecKey(string(resolved.Protocol))]; ok {
		mergeRequestParamPolicy(&resolved.RequestParams, policy)
	}
	mergeBoolMap(resolved.Capabilities, def.Capabilities)
	mergeStringMap(resolved.Reasoning, def.Reasoning)
	resolved.Documentation = appendUniqueStrings(resolved.Documentation, def.Documentation...)
}

func mergeModelDefinition(resolved *resolvedProtocolSpec, def modelSpecDefinition) {
	if strings.TrimSpace(def.OfficialBaseURL) != "" {
		resolved.OfficialBaseURL = strings.TrimSpace(def.OfficialBaseURL)
	}
	mergeProtocolPathDefinition(resolved, "", def.BasePath, def.Path, def.Paths)
	mergeAlternateProtocols(resolved.AlternateProtocols, def.AlternateProtocols)
	mergeRequestParamPolicy(&resolved.RequestParams, def.RequestParams)
	mergeBoolMap(resolved.Capabilities, def.Capabilities)
	mergeStringMap(resolved.Reasoning, def.Reasoning)
	resolved.Documentation = appendUniqueStrings(resolved.Documentation, def.Documentation...)
}

func mergeProtocolPathDefinition(resolved *resolvedProtocolSpec, defaultProtocol string, basePath string, requestPath string, paths map[string]protocolPathDefinition) {
	if protocolHint := normalizeSpecKey(defaultProtocol); protocolHint != "" && protocolHint != normalizeSpecKey(string(resolved.Protocol)) {
		basePath = ""
		requestPath = ""
	}
	if strings.TrimSpace(basePath) != "" {
		resolved.BasePath = strings.TrimSpace(basePath)
	}
	if strings.TrimSpace(requestPath) != "" {
		resolved.Path = strings.TrimSpace(requestPath)
	}
	for key, override := range paths {
		if normalizeSpecKey(key) != normalizeSpecKey(string(resolved.Protocol)) {
			continue
		}
		if strings.TrimSpace(override.BasePath) != "" {
			resolved.BasePath = strings.TrimSpace(override.BasePath)
		}
		if strings.TrimSpace(override.Path) != "" {
			resolved.Path = strings.TrimSpace(override.Path)
		}
	}
}

func mergeEvents(dst map[string]eventMapping, src map[string]eventMapping) {
	for key, value := range src {
		dst[key] = value
	}
}

func mergeAlternateProtocols(dst map[string]alternateProtocolDefinition, src map[string]alternateProtocolDefinition) {
	for key, value := range src {
		normalized := normalizeSpecKey(key)
		if normalized == "" {
			continue
		}
		dst[normalized] = value
	}
}

func mergeRequestParamPolicy(dst *requestParamPolicy, src requestParamPolicy) {
	dst.Supported = appendUniqueStrings(dst.Supported, src.Supported...)
	dst.Unsupported = appendUniqueStrings(dst.Unsupported, src.Unsupported...)
	dst.UnsupportedWithImageGeneration = appendUniqueStrings(dst.UnsupportedWithImageGeneration, src.UnsupportedWithImageGeneration...)
	if dst.AllowedValues == nil {
		dst.AllowedValues = map[string][]any{}
	}
	for key, values := range src.AllowedValues {
		dst.AllowedValues[key] = cloneSpecSlice(values)
	}
	if dst.Defaults == nil {
		dst.Defaults = map[string]any{}
	}
	for key, value := range src.Defaults {
		dst.Defaults[key] = cloneSpecValue(value)
	}
	if dst.Forced == nil {
		dst.Forced = map[string]any{}
	}
	for key, value := range src.Forced {
		dst.Forced[key] = cloneSpecValue(value)
	}
	if dst.Fixed == nil {
		dst.Fixed = map[string]any{}
	}
	for key, value := range src.Fixed {
		dst.Fixed[key] = cloneSpecValue(value)
	}
	if dst.Validation == nil {
		dst.Validation = map[string]paramValidation{}
	}
	for key, value := range src.Validation {
		dst.Validation[key] = value
	}
}

func valueInAllowedSet(value any, allowed []any) bool {
	for _, item := range allowed {
		if fmt.Sprintf("%v", value) == fmt.Sprintf("%v", item) {
			return true
		}
	}
	return false
}

func payloadHasImageGenerationTool(payload map[string]any) bool {
	return codexPayloadHasImageGenerationTool(payload)
}

func mergeBoolMap(dst map[string]bool, src map[string]bool) {
	for key, value := range src {
		dst[key] = value
	}
}

func mergeStringMap(dst map[string]string, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}

func matchModelSpec(models map[string]modelSpecDefinition, model string) (string, modelSpecDefinition) {
	model = normalizeModelPattern(model)
	if model == "" {
		return "", modelSpecDefinition{}
	}
	if def, ok := models[model]; ok {
		return model, def
	}
	keys := make([]string, 0, len(models))
	for key := range models {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})
	for _, pattern := range keys {
		if !strings.Contains(pattern, "*") {
			continue
		}
		ok, err := path.Match(normalizeModelPattern(pattern), model)
		if err == nil && ok {
			return pattern, models[pattern]
		}
	}
	return "", modelSpecDefinition{}
}

func normalizeSpecKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func genericOpenAICompatibleSiteType(value string) bool {
	switch normalizeSpecKey(value) {
	case "openai-compatible", "newapi", "oneapi", "one_api", "xlyra":
		return true
	default:
		return false
	}
}

func OfficialBaseURLForProvider(provider string) string {
	config, err := loadProtocolSpecRegistry()
	if err != nil {
		return ""
	}
	key := normalizeSpecKey(provider)
	if def, ok := config.Providers[key]; ok {
		return strings.TrimSpace(def.OfficialBaseURL)
	}
	return ""
}

func upstreamPathFromSpec(baseURL string, spec resolvedProtocolSpec, fallbackPath string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(spec.OfficialBaseURL), "/")
	}
	basePath := strings.Trim(strings.TrimSpace(spec.BasePath), "/")
	if basePath != "" {
		base += "/" + basePath
	}
	requestPath := strings.TrimSpace(spec.Path)
	if requestPath == "" {
		requestPath = fallbackPath
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	return base + requestPath
}

func normalizeModelPattern(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := map[string]bool{}
	for _, value := range dst {
		seen[value] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		dst = append(dst, value)
		seen[value] = true
	}
	return dst
}

func cloneSpecValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = cloneSpecValue(item)
		}
		return out
	case []any:
		if stringsOnly(typed) {
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				out = append(out, item.(string))
			}
			return out
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneSpecValue(item))
		}
		return out
	default:
		return value
	}
}

func cloneSpecSlice(values []any) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, cloneSpecValue(value))
	}
	return out
}

func stringsOnly(items []any) bool {
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

func validateProtocolSpecRegistry(config protocolSpecRegistryConfig) error {
	var errs []string
	for name, def := range config.Protocols {
		if conflicts := validateRequestParamPolicyConflicts(def.RequestParams); len(conflicts) > 0 {
			errs = append(errs, fmt.Sprintf("protocol %q: %s", name, strings.Join(conflicts, "; ")))
		}
	}
	for name, def := range config.Providers {
		if conflicts := validateRequestParamPolicyConflicts(def.RequestParams); len(conflicts) > 0 {
			errs = append(errs, fmt.Sprintf("provider %q: %s", name, strings.Join(conflicts, "; ")))
		}
		for protocol, policy := range def.ProtocolRequestParams {
			if conflicts := validateRequestParamPolicyConflicts(policy); len(conflicts) > 0 {
				errs = append(errs, fmt.Sprintf("provider %q protocol %q: %s", name, protocol, strings.Join(conflicts, "; ")))
			}
		}
	}
	for name, def := range config.Models {
		if conflicts := validateRequestParamPolicyConflicts(def.RequestParams); len(conflicts) > 0 {
			errs = append(errs, fmt.Sprintf("model %q: %s", name, strings.Join(conflicts, "; ")))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("request_params conflicts: %s", strings.Join(errs, ", "))
	}
	return nil
}

func validateRequestParamPolicyConflicts(policy requestParamPolicy) []string {
	var conflicts []string
	for key := range policy.Defaults {
		if _, ok := policy.Fixed[key]; ok {
			conflicts = append(conflicts, fmt.Sprintf("%q has both defaults and fixed", key))
		}
		if _, ok := policy.Forced[key]; ok {
			conflicts = append(conflicts, fmt.Sprintf("%q has both defaults and forced", key))
		}
	}
	for key := range policy.Fixed {
		if _, ok := policy.Forced[key]; ok {
			conflicts = append(conflicts, fmt.Sprintf("%q has both fixed and forced", key))
		}
	}
	sort.Strings(conflicts)
	return conflicts
}
