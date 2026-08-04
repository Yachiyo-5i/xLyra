package site

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	ResponsesToolPolicyPassthrough   = "passthrough"
	ResponsesToolPolicyCompatibility = "compatibility"

	ResponsesHostedToolImageGeneration = "image_generation"

	QuotaProbeTypeSub2API = "sub2api"
	QuotaProbeTypeNewAPI  = "newapi"
	QuotaProbeTypeXLyra   = "xlyra"
	QuotaProbeTypeKimi    = "kimi"
)

type GatewayConfig struct {
	RequestTimeoutMS               *int     `json:"request_timeout_ms,omitempty"`
	ConnectTimeoutMS               *int     `json:"connect_timeout_ms,omitempty"`
	ResponseHeaderTimeoutMS        *int     `json:"response_header_timeout_ms,omitempty"`
	MaxConcurrency                 *int     `json:"max_concurrency,omitempty"`
	MaxModelConcurrency            *int     `json:"max_model_concurrency,omitempty"`
	MaxCredentialConcurrency       *int     `json:"max_credential_concurrency,omitempty"`
	MaxIdleConns                   *int     `json:"max_idle_conns,omitempty"`
	MaxIdleConnsPerHost            *int     `json:"max_idle_conns_per_host,omitempty"`
	MaxConnsPerHost                *int     `json:"max_conns_per_host,omitempty"`
	IdleConnTimeoutMS              *int     `json:"idle_conn_timeout_ms,omitempty"`
	ResponsesToolPolicy            string   `json:"responses_tool_policy,omitempty"`
	DisabledResponsesTools         []string `json:"disabled_responses_tools,omitempty"`
	ResponsesImageGenerationPolicy string   `json:"responses_image_generation_policy,omitempty"`
	ImpersonateCodexClient         *bool    `json:"impersonate_codex_client,omitempty"`
	ImpersonateClaudeCodeClient    *bool    `json:"impersonate_claude_code_client,omitempty"`
	QuotaProbe                     *string  `json:"quota_probe,omitempty"`
}

func NormalizeQuotaProbeType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", "none":
		return "", nil
	case QuotaProbeTypeSub2API:
		return QuotaProbeTypeSub2API, nil
	case QuotaProbeTypeNewAPI:
		return QuotaProbeTypeNewAPI, nil
	case QuotaProbeTypeXLyra:
		return QuotaProbeTypeXLyra, nil
	case QuotaProbeTypeKimi:
		return QuotaProbeTypeKimi, nil
	default:
		return "", fmt.Errorf("unsupported quota_probe type %q", value)
	}
}

func QuotaProbeTypeFromConfig(cfg *GatewayConfig) string {
	if cfg == nil || cfg.QuotaProbe == nil {
		return ""
	}
	normalized, err := NormalizeQuotaProbeType(*cfg.QuotaProbe)
	if err != nil {
		return ""
	}
	return normalized
}

func NormalizeGatewayConfig(input *GatewayConfig) (*GatewayConfig, error) {
	if input == nil {
		return nil, nil
	}

	normalized := *input
	normalizeLegacyResponsesToolConfig(&normalized)
	if normalized.ResponsesToolPolicy != "" {
		normalized.ResponsesToolPolicy = NormalizeResponsesToolPolicy(normalized.ResponsesToolPolicy)
	}
	hasDisabledResponsesTools := normalized.DisabledResponsesTools != nil
	normalized.DisabledResponsesTools = NormalizeDisabledResponsesTools(normalized.DisabledResponsesTools)
	if hasDisabledResponsesTools && normalized.DisabledResponsesTools == nil {
		normalized.DisabledResponsesTools = []string{}
	}
	if normalized.QuotaProbe != nil {
		probeType, err := NormalizeQuotaProbeType(*normalized.QuotaProbe)
		if err != nil {
			return nil, err
		}
		normalized.QuotaProbe = &probeType
	}
	for _, field := range []struct {
		name  string
		value *int
	}{
		{name: "request_timeout_ms", value: normalized.RequestTimeoutMS},
		{name: "connect_timeout_ms", value: normalized.ConnectTimeoutMS},
		{name: "response_header_timeout_ms", value: normalized.ResponseHeaderTimeoutMS},
		{name: "max_concurrency", value: normalized.MaxConcurrency},
		{name: "max_model_concurrency", value: normalized.MaxModelConcurrency},
		{name: "max_credential_concurrency", value: normalized.MaxCredentialConcurrency},
		{name: "max_idle_conns", value: normalized.MaxIdleConns},
		{name: "max_idle_conns_per_host", value: normalized.MaxIdleConnsPerHost},
		{name: "max_conns_per_host", value: normalized.MaxConnsPerHost},
		{name: "idle_conn_timeout_ms", value: normalized.IdleConnTimeoutMS},
	} {
		if field.value != nil && *field.value <= 0 {
			return nil, fmt.Errorf("%s must be greater than 0", field.name)
		}
	}

	return &normalized, nil
}

func NormalizeResponsesToolPolicy(value string) string {
	switch strings.TrimSpace(value) {
	case ResponsesToolPolicyCompatibility:
		return ResponsesToolPolicyCompatibility
	default:
		return ResponsesToolPolicyPassthrough
	}
}

func NormalizeDisabledResponsesTools(values []string) []string {
	seen := map[string]struct{}{}
	tools := make([]string, 0, len(values))
	for _, value := range values {
		tool := normalizeResponsesHostedTool(value)
		if tool == "" {
			continue
		}
		if _, ok := seen[tool]; ok {
			continue
		}
		seen[tool] = struct{}{}
		tools = append(tools, tool)
	}
	if len(tools) == 0 {
		return nil
	}
	return tools
}

func ResponsesToolDisabled(cfg *GatewayConfig, tool string) bool {
	if cfg == nil || NormalizeResponsesToolPolicy(cfg.ResponsesToolPolicy) != ResponsesToolPolicyCompatibility {
		return false
	}
	tool = normalizeResponsesHostedTool(tool)
	if tool == "" {
		return false
	}
	for _, item := range NormalizeDisabledResponsesTools(cfg.DisabledResponsesTools) {
		if item == tool {
			return true
		}
	}
	return false
}

func normalizeResponsesHostedTool(value string) string {
	switch strings.TrimSpace(value) {
	case ResponsesHostedToolImageGeneration:
		return ResponsesHostedToolImageGeneration
	default:
		return ""
	}
}

func normalizeLegacyResponsesToolConfig(cfg *GatewayConfig) {
	switch strings.TrimSpace(cfg.ResponsesImageGenerationPolicy) {
	case "strip_auto_tool", "disabled":
		if cfg.ResponsesToolPolicy == "" {
			cfg.ResponsesToolPolicy = ResponsesToolPolicyCompatibility
		}
		cfg.DisabledResponsesTools = append(cfg.DisabledResponsesTools, ResponsesHostedToolImageGeneration)
	}
	cfg.ResponsesImageGenerationPolicy = ""
}

func GatewayConfigFromSiteMeta(raw []byte) *GatewayConfig {
	if len(raw) == 0 {
		return nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}

	value, ok := root["gateway"]
	if !ok || len(value) == 0 || string(value) == "null" {
		return nil
	}

	var cfg GatewayConfig
	if err := json.Unmarshal(value, &cfg); err != nil {
		return nil
	}

	normalized, err := NormalizeGatewayConfig(&cfg)
	if err != nil {
		return nil
	}
	return normalized
}

func MergeSiteGatewayConfig(existingRaw []byte, gateway *GatewayConfig) ([]byte, error) {
	if gateway == nil {
		if len(existingRaw) == 0 {
			return []byte(`{}`), nil
		}
		return existingRaw, nil
	}

	normalized, err := NormalizeGatewayConfig(gateway)
	if err != nil {
		return nil, err
	}

	root := map[string]any{}
	if len(existingRaw) > 0 {
		_ = json.Unmarshal(existingRaw, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	existing := GatewayConfigFromSiteMeta(existingRaw)
	root["gateway"] = mergeGatewayConfig(existing, normalized)

	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("marshal site meta: %w", err)
	}
	return encoded, nil
}

func RequestHeadersFromSiteMeta(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}

	value, ok := root["request_headers"]
	if !ok || len(value) == 0 || string(value) == "null" {
		return nil
	}

	var items []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(value, &items); err != nil {
		return nil
	}

	result := make(map[string]string, len(items))
	for _, item := range items {
		if item.Key != "" {
			result[item.Key] = item.Value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func mergeGatewayConfig(existing *GatewayConfig, patch *GatewayConfig) *GatewayConfig {
	if patch == nil {
		return existing
	}
	if existing == nil {
		return patch
	}
	merged := *existing
	if patch.RequestTimeoutMS != nil {
		merged.RequestTimeoutMS = patch.RequestTimeoutMS
	}
	if patch.ConnectTimeoutMS != nil {
		merged.ConnectTimeoutMS = patch.ConnectTimeoutMS
	}
	if patch.ResponseHeaderTimeoutMS != nil {
		merged.ResponseHeaderTimeoutMS = patch.ResponseHeaderTimeoutMS
	}
	if patch.MaxConcurrency != nil {
		merged.MaxConcurrency = patch.MaxConcurrency
	}
	if patch.MaxModelConcurrency != nil {
		merged.MaxModelConcurrency = patch.MaxModelConcurrency
	}
	if patch.MaxCredentialConcurrency != nil {
		merged.MaxCredentialConcurrency = patch.MaxCredentialConcurrency
	}
	if patch.MaxIdleConns != nil {
		merged.MaxIdleConns = patch.MaxIdleConns
	}
	if patch.MaxIdleConnsPerHost != nil {
		merged.MaxIdleConnsPerHost = patch.MaxIdleConnsPerHost
	}
	if patch.MaxConnsPerHost != nil {
		merged.MaxConnsPerHost = patch.MaxConnsPerHost
	}
	if patch.IdleConnTimeoutMS != nil {
		merged.IdleConnTimeoutMS = patch.IdleConnTimeoutMS
	}
	if patch.ResponsesToolPolicy != "" {
		merged.ResponsesToolPolicy = NormalizeResponsesToolPolicy(patch.ResponsesToolPolicy)
	}
	if patch.DisabledResponsesTools != nil {
		merged.DisabledResponsesTools = NormalizeDisabledResponsesTools(patch.DisabledResponsesTools)
	}
	if patch.ImpersonateCodexClient != nil {
		merged.ImpersonateCodexClient = patch.ImpersonateCodexClient
	}
	if patch.ImpersonateClaudeCodeClient != nil {
		merged.ImpersonateClaudeCodeClient = patch.ImpersonateClaudeCodeClient
	}
	if patch.QuotaProbe != nil {
		if value, err := NormalizeQuotaProbeType(*patch.QuotaProbe); err == nil {
			if value == "" {
				merged.QuotaProbe = nil
			} else {
				merged.QuotaProbe = &value
			}
		}
	}
	return &merged
}
