package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xlyra/server/internal/upstream"
)

const genericProviderDefaultModelsPath = "/v1/models"

type GenericProviderConfig struct {
	SiteType        string
	DisplayName     string
	OfficialBaseURL string
	ModelsPath      string // empty defaults to /v1/models
	Auth            string // "bearer" (default) or "header:<Header-Name>"
	EndpointTypes   []string
	AllowedHosts    []string
}

type GenericProvider struct {
	client          *http.Client
	siteType        string
	displayName     string
	officialBaseURL string
	modelsPath      string
	authHeader      string
	endpointTypes   []string
	allowedHosts    []string
}

func NewGenericProvider(cfg GenericProviderConfig) (Module, error) {
	siteType := strings.ToLower(strings.TrimSpace(cfg.SiteType))
	if siteType == "" {
		return nil, fmt.Errorf("site type is required")
	}
	if !strings.Contains(siteType, "/") {
		return nil, fmt.Errorf("site type %q must contain \"/\"", siteType)
	}
	officialBaseURL, err := validateGenericOfficialBaseURL(cfg.OfficialBaseURL)
	if err != nil {
		return nil, err
	}
	modelsPath := strings.TrimSpace(cfg.ModelsPath)
	if modelsPath == "" {
		modelsPath = genericProviderDefaultModelsPath
	} else if err := validateGenericModelsPath(modelsPath); err != nil {
		return nil, err
	}
	authHeader, err := parseGenericAuth(cfg.Auth)
	if err != nil {
		return nil, err
	}
	endpointTypes, err := normalizeGenericEndpointTypes(cfg.EndpointTypes)
	if err != nil {
		return nil, err
	}
	allowedHosts, err := normalizeGenericAllowedHosts(cfg.AllowedHosts)
	if err != nil {
		return nil, err
	}
	return GenericProvider{
		client:          &http.Client{Timeout: 20 * time.Second},
		siteType:        siteType,
		displayName:     strings.TrimSpace(cfg.DisplayName),
		officialBaseURL: officialBaseURL,
		modelsPath:      modelsPath,
		authHeader:      authHeader,
		endpointTypes:   endpointTypes,
		allowedHosts:    allowedHosts,
	}, nil
}

func (p GenericProvider) SiteTypes() []string {
	return []string{p.siteType}
}

func (p GenericProvider) DefaultBaseURL() string {
	return p.officialBaseURL
}

func (p GenericProvider) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityHealthProbe,
	}
}

func (p GenericProvider) Scope() HealthProbeScope {
	return HealthProbeCredentialScope
}

func (p GenericProvider) ProbeHealth(ctx context.Context, site SiteConfig, credential string) ([]Model, error) {
	return p.ListModels(ctx, site, credential)
}

func (p GenericProvider) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	_, err := p.ListModels(ctx, site, apiKey)
	return err
}

func (p GenericProvider) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(site.BaseURL), "/")
	if err := p.checkAllowedHost(baseURL); err != nil {
		return nil, err
	}
	endpoint := baseURL + p.modelsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	p.setAuth(req, apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient(site).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call upstream models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, upstream.NewHTTPError("upstream returned", resp.StatusCode, resp.Header, body)
	}

	var payload struct {
		Data []struct {
			ID      string         `json:"id"`
			Object  string         `json:"object"`
			OwnedBy string         `json:"owned_by"`
			Meta    map[string]any `json:"metadata"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode upstream models: %w", err)
	}

	endpointTypes := append([]string(nil), p.endpointTypes...)
	models := make([]Model, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID == "" {
			continue
		}
		capabilities := map[string]any{
			"object":                   item.Object,
			"supported_endpoint_types": append([]string(nil), endpointTypes...),
		}
		if item.OwnedBy != "" {
			capabilities["owned_by"] = item.OwnedBy
		}
		if len(item.Meta) > 0 {
			capabilities["metadata"] = item.Meta
		}
		models = append(models, Model{
			UpstreamName: item.ID,
			DisplayName:  item.ID,
			Capabilities: capabilities,
		})
	}
	return models, nil
}

func (p GenericProvider) checkAllowedHost(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("base url host is not allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	for _, allowed := range p.allowedHosts {
		if allowed == host {
			return nil
		}
	}
	return fmt.Errorf("base url host %q is not allowed", host)
}

func (p GenericProvider) httpClient(site SiteConfig) *http.Client {
	return HTTPClientWithAllowedHosts(httpClientForSite(site, p.client), p.allowedHosts)
}

func (p GenericProvider) setAuth(req *http.Request, apiKey string) {
	if strings.TrimSpace(apiKey) == "" {
		return
	}
	if p.authHeader == "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return
	}
	req.Header.Set(p.authHeader, apiKey)
}

func validateGenericOfficialBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("official base url %q must be an http or https URL with a host", raw)
	}
	return raw, nil
}

// validateGenericModelsPath matches jsplugin.validatePluginPath and
// gateway.validateRuntimeProviderPath. Neither helper is usable here:
// the first is unexported, and adapter must not import gateway.
func validateGenericModelsPath(requestPath string) error {
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

func parseGenericAuth(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "bearer") {
		return "", nil
	}
	scheme, name, ok := strings.Cut(raw, ":")
	if !ok || !strings.EqualFold(strings.TrimSpace(scheme), "header") {
		return "", fmt.Errorf("auth %q must be bearer or header:<Header-Name>", raw)
	}
	name = strings.TrimSpace(name)
	if !validGenericHeaderName(name) {
		return "", fmt.Errorf("auth header name %q is not valid", name)
	}
	return name, nil
}

func validGenericHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !isGenericHeaderToken(name[i]) {
			return false
		}
	}
	return true
}

func isGenericHeaderToken(c byte) bool {
	switch c {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
}

const genericProviderMaxAllowedHosts = 16

func normalizeGenericAllowedHosts(values []string) ([]string, error) {
	if len(values) < 1 || len(values) > genericProviderMaxAllowedHosts {
		return nil, fmt.Errorf("allowed hosts must list 1 to %d hostnames", genericProviderMaxAllowedHosts)
	}
	out := make([]string, len(values))
	seen := make(map[string]struct{}, len(values))
	for i, host := range values {
		if err := validGenericAllowedHost(host); err != nil {
			return nil, fmt.Errorf("allowed hosts: %w", err)
		}
		if _, ok := seen[host]; ok {
			return nil, fmt.Errorf("allowed hosts: duplicate host %q", host)
		}
		seen[host] = struct{}{}
		out[i] = host
	}
	return out, nil
}

func validGenericAllowedHost(host string) error {
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

// HTTPClientWithAllowedHosts clones base and refuses redirects that leave the
// allow-list, downgrade https, or exceed five hops. The base client is unchanged.
// Custom auth headers are not stripped on cross-host redirects, so the hop is
// rejected instead of followed.
func HTTPClientWithAllowedHosts(base *http.Client, hosts []string) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	clone := *base
	allowed := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		allowed[strings.ToLower(strings.TrimSpace(host))] = struct{}{}
	}
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return fmt.Errorf("stopped after 5 redirects")
		}
		if req == nil || req.URL == nil {
			return fmt.Errorf("redirect target is not allowed")
		}
		nextScheme := strings.ToLower(req.URL.Scheme)
		originScheme := ""
		if len(via) > 0 && via[0] != nil && via[0].URL != nil {
			originScheme = strings.ToLower(via[0].URL.Scheme)
		}
		if originScheme == "https" && nextScheme == "http" {
			return fmt.Errorf("redirect from https to http is not allowed")
		}
		if nextScheme != "https" && nextScheme != originScheme {
			return fmt.Errorf("redirect scheme %q is not allowed", req.URL.Scheme)
		}
		host := strings.ToLower(req.URL.Hostname())
		if _, ok := allowed[host]; !ok {
			return fmt.Errorf("redirect host %q is not allowed", host)
		}
		return nil
	}
	return &clone
}

func normalizeGenericEndpointTypes(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("endpoint types are required")
	}
	out := make([]string, len(values))
	for i, value := range values {
		value = strings.TrimSpace(value)
		switch value {
		case "openai", "openai-response", "anthropic-messages", "google-gemini":
			out[i] = value
		default:
			return nil, fmt.Errorf("endpoint type %q is not supported", value)
		}
	}
	return out, nil
}
