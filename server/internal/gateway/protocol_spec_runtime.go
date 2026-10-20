package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
)

// runtimeProviderSpecs is replaced as a whole. Readers load the pointer and
// do not take a lock. The map inside a published snapshot is not mutated.
var runtimeProviderSpecs atomic.Pointer[runtimeProviderSpecSnapshot]

type runtimeProviderSpecSnapshot struct {
	providers map[string]providerSpecDefinition
}

// runtimeProviderSpecFields is the phase-2 subset of providers.<name>.
// Alternate protocols stay closed so official_only is not part of a plugin spec.
var runtimeProviderSpecFields = map[string]struct{}{
	"protocol":          {},
	"official_base_url": {},
	"path":              {},
	"request_params":    {},
	"documentation":     {},
}

// runtimeRequestParamFields are the request_params keys a plugin spec may set.
// supported is stored and merged; it does not filter request parameters.
var runtimeRequestParamFields = map[string]struct{}{
	"unsupported":    {},
	"allowed_values": {},
	"defaults":       {},
	"fixed":          {},
	"forced":         {},
	"validation":     {},
	"supported":      {},
}

var runtimeProviderProtocols = map[string]struct{}{
	string(canonicalProtocolOpenAIChat):        {},
	string(canonicalProtocolOpenAIResponses):   {},
	string(canonicalProtocolAnthropicMessages): {},
	string(canonicalProtocolGoogleGemini):      {},
}

// SetRuntimeProviderSpecs replaces the runtime provider-spec snapshot.
// Keys are plugin site type ids and must contain "/". Values use the same
// schema as providers.<name> in protocol_specs.json, limited to the phase-2
// field allowlist. Any invalid item returns an error and leaves the current
// snapshot in place.
func SetRuntimeProviderSpecs(items map[string]json.RawMessage) error {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	normalized := make(map[string]json.RawMessage, len(keys))
	original := make(map[string]string, len(keys))
	for _, key := range keys {
		id := normalizeSpecKey(key)
		if id == "" {
			return fmt.Errorf("runtime provider spec key is empty")
		}
		if previous, ok := original[id]; ok {
			return fmt.Errorf("runtime provider spec key %q duplicates %q", key, previous)
		}
		if !strings.Contains(id, "/") {
			return fmt.Errorf("runtime provider spec key %q must contain \"/\"", id)
		}
		original[id] = key
		normalized[id] = items[key]
	}

	ids := make([]string, 0, len(normalized))
	for id := range normalized {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	providers := make(map[string]providerSpecDefinition, len(ids))
	for _, id := range ids {
		def, err := parseRuntimeProviderSpec(id, normalized[id])
		if err != nil {
			return err
		}
		providers[id] = def
	}
	runtimeProviderSpecs.Store(&runtimeProviderSpecSnapshot{providers: providers})
	return nil
}

// ClearRuntimeProviderSpecs drops the runtime provider-spec snapshot.
func ClearRuntimeProviderSpecs() {
	runtimeProviderSpecs.Store(nil)
}

// ValidateRuntimeProviderSpec reports whether one provider spec would be
// accepted by SetRuntimeProviderSpecs. The key is a plugin site type id.
func ValidateRuntimeProviderSpec(key string, raw json.RawMessage) error {
	id := normalizeSpecKey(key)
	if id == "" {
		return fmt.Errorf("runtime provider spec key is empty")
	}
	if !strings.Contains(id, "/") {
		return fmt.Errorf("runtime provider spec key %q must contain \"/\"", id)
	}
	_, err := parseRuntimeProviderSpec(id, raw)
	return err
}

// providerSpecByKey reads the embedded spec first, then the runtime snapshot.
// Built-in keys do not contain "/" and override keys must, so they do not overlap.
func providerSpecByKey(provider string) (providerSpecDefinition, bool) {
	key := normalizeSpecKey(provider)
	if key == "" {
		return providerSpecDefinition{}, false
	}
	if config, err := loadProtocolSpecRegistry(); err == nil {
		if def, ok := config.Providers[key]; ok {
			return def, true
		}
	}
	snapshot := runtimeProviderSpecs.Load()
	if snapshot == nil {
		return providerSpecDefinition{}, false
	}
	def, ok := snapshot.providers[key]
	return def, ok
}

func parseRuntimeProviderSpec(key string, raw json.RawMessage) (providerSpecDefinition, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: empty spec", key)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: %w", key, err)
	}
	if fields == nil {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: spec must be an object", key)
	}
	if unknown := unknownRuntimeFields(fields, runtimeProviderSpecFields); len(unknown) > 0 {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: unknown field %q", key, unknown[0])
	}
	if rawParams, ok := fields["request_params"]; ok && !isJSONNull(rawParams) {
		var paramFields map[string]json.RawMessage
		if err := json.Unmarshal(rawParams, &paramFields); err != nil {
			return providerSpecDefinition{}, fmt.Errorf("provider %q: request_params: %w", key, err)
		}
		if paramFields == nil {
			return providerSpecDefinition{}, fmt.Errorf("provider %q: request_params must be an object", key)
		}
		if unknown := unknownRuntimeFields(paramFields, runtimeRequestParamFields); len(unknown) > 0 {
			return providerSpecDefinition{}, fmt.Errorf("provider %q: request_params: unknown field %q", key, unknown[0])
		}
	}

	var def providerSpecDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: %w", key, err)
	}
	def.Protocol = normalizeSpecKey(def.Protocol)
	if def.Protocol == "" {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: protocol is required", key)
	}
	if _, ok := runtimeProviderProtocols[def.Protocol]; !ok {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: protocol %q is not allowed", key, def.Protocol)
	}
	def.OfficialBaseURL = strings.TrimSpace(def.OfficialBaseURL)
	if err := validateRuntimeOfficialBaseURL(def.OfficialBaseURL); err != nil {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: %w", key, err)
	}
	def.Path = strings.TrimSpace(def.Path)
	if err := validateRuntimeProviderPath(def.Path); err != nil {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: %w", key, err)
	}
	if conflicts := validateRequestParamPolicyConflicts(def.RequestParams); len(conflicts) > 0 {
		return providerSpecDefinition{}, fmt.Errorf("provider %q: request_params: %s", key, strings.Join(conflicts, "; "))
	}
	return def, nil
}

func unknownRuntimeFields(fields map[string]json.RawMessage, allowed map[string]struct{}) []string {
	unknown := make([]string, 0)
	for field := range fields {
		if _, ok := allowed[field]; !ok {
			unknown = append(unknown, field)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func isJSONNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

func validateRuntimeOfficialBaseURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("official_base_url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" {
		return fmt.Errorf("official_base_url %q must be an https URL with a host", raw)
	}
	if parsed.User != nil {
		return fmt.Errorf("official_base_url %q must not contain userinfo", raw)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return fmt.Errorf("official_base_url %q must not contain a query", raw)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("official_base_url %q must not contain a fragment", raw)
	}
	return nil
}

// validateRuntimeProviderPath matches jsplugin.validatePluginPath. That helper
// is unexported, so the checks are repeated here.
func validateRuntimeProviderPath(requestPath string) error {
	if requestPath == "" || !strings.HasPrefix(requestPath, "/") || strings.HasPrefix(requestPath, "//") {
		return fmt.Errorf("path %q must start with a single /", requestPath)
	}
	if strings.Contains(requestPath, `\`) || strings.Contains(requestPath, "#") || strings.Contains(requestPath, "://") {
		return fmt.Errorf("path %q must not contain \\, #, or ://", requestPath)
	}
	raw, _, _ := strings.Cut(requestPath, "?")
	for _, segment := range strings.Split(raw, "/") {
		if segment == "" {
			continue
		}
		if segment == "." || segment == ".." {
			return fmt.Errorf("path %q is not allowed", requestPath)
		}
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return fmt.Errorf("path %q: %w", requestPath, err)
		}
		if decoded == "." || decoded == ".." || strings.ContainsAny(decoded, `/\\`) {
			return fmt.Errorf("path %q is not allowed", requestPath)
		}
	}
	return nil
}
