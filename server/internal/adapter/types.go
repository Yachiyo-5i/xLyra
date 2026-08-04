package adapter

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type SiteConfig struct {
	ID       string
	Name     string
	SiteType string
	BaseURL  string
	Meta     map[string]any
	Client   *http.Client
}

type SystemAuth struct {
	Provider     string
	ConnectionID uuid.UUID
	AccessToken  string
	RefreshToken string
	IDToken      string
	AccountID    string
	Email        string
	UserID       int
	ExpiresAt    time.Time
	Metadata     map[string]any
}

type Capability string

const (
	CapabilityDetect             Capability = "detect"
	CapabilityHealthProbe        Capability = "health_probe"
	CapabilityValidateCredential Capability = "validate_credential"
	CapabilityListModels         Capability = "list_models"
	CapabilityListAPIKeys        Capability = "list_api_keys"
	CapabilitySummarizeAPIKey    Capability = "summarize_api_key"
	CapabilityFetchUserSummary   Capability = "fetch_user_summary"
	CapabilityFetchBalance       Capability = "fetch_balance"
	CapabilityFetchPricing       Capability = "fetch_pricing"
	CapabilityCheckin            Capability = "checkin"
	CapabilityFetchMetadata      Capability = "fetch_metadata"
)

type DetectResult struct {
	SiteType   string         `json:"site_type"`
	Matched    bool           `json:"matched"`
	Confidence float64        `json:"confidence"`
	Features   map[string]any `json:"features"`
	RawStatus  map[string]any `json:"raw_status,omitempty"`
}

type HealthProbeScope string

const (
	HealthProbeSiteScope       HealthProbeScope = "site"
	HealthProbeCredentialScope HealthProbeScope = "credential"
)

type Model struct {
	UpstreamName string
	DisplayName  string
	Capabilities map[string]any
}

type APIKey struct {
	ID         int
	ExternalID string
	Name       string
	Status     any
	Key        string
	MaskedKey  string
	Raw        map[string]any
}

type APIKeySummary struct {
	Usage  any
	Models []Model
	Raw    any
}

type UserSummary struct {
	User         any  `json:"user"`
	APIKeys      any  `json:"api_keys"`
	UserModels   any  `json:"user_models"`
	Pricing      any  `json:"pricing"`
	Checkin      any  `json:"checkin"`
	CheckinReady bool `json:"checkin_ready"`
}

type BalanceSnapshot struct {
	Raw any
}

type PricingGroup struct {
	GroupName   string
	DisplayName string
	Ratio       float64
	IsAuto      bool
	Raw         any
}

type ModelPricing struct {
	ModelName               string
	DisplayName             string
	GroupName               string
	QuotaType               int
	BillingType             string
	Currency                string
	GroupRatio              float64
	ModelRatio              float64
	HasModelRatio           bool
	CompletionRatio         float64
	HasCompletionRatio      bool
	CacheRatio              float64
	HasCacheRatio           bool
	CreateCacheRatio        float64
	HasCreateCacheRatio     bool
	CreateCache1hRatio      float64
	HasCreateCache1hRatio   bool
	ImageRatio              float64
	HasImageRatio           bool
	AudioRatio              float64
	HasAudioRatio           bool
	AudioCompletionRatio    float64
	HasAudioCompletionRatio bool
	ModelPrice              float64
	HasModelPrice           bool
	PerRequestValue         float64
	HasPerRequestValue      bool
	InputValue              float64
	HasInputValue           bool
	OutputValue             float64
	HasOutputValue          bool
	VendorID                int64
	HasVendorID             bool
	VendorName              string
	VendorIcon              string
	Description             string
	OwnerBy                 string
	Raw                     map[string]any
}

func containsEndpointType(endpoints any, target string) bool {
	switch v := endpoints.(type) {
	case []string:
		for _, e := range v {
			if e == target {
				return true
			}
		}
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok && s == target {
				return true
			}
		}
	}
	return false
}

type PricingSnapshot struct {
	Groups []PricingGroup
	Items  []ModelPricing
	Raw    any
}

type MetadataSnapshot struct {
	Raw any
}

type CheckinResult struct {
	Raw any
}

type Module interface {
	SiteTypes() []string
}

type DefaultBaseURLProvider interface {
	DefaultBaseURL() string
}

type CapabilityDescriber interface {
	Capabilities() []Capability
}

type Detector interface {
	Detect(ctx context.Context, baseURL string) (DetectResult, error)
}

type HealthProbe interface {
	ProbeHealth(ctx context.Context, site SiteConfig, credential string) ([]Model, error)
	Scope() HealthProbeScope
}

type CredentialValidator interface {
	ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error
}

type SystemCredentialValidator interface {
	ValidateSystemCredentials(ctx context.Context, site SiteConfig, auth SystemAuth) error
}

type GatewayModelLister interface {
	ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error)
}

type SystemModelLister interface {
	ListModelsWithAuth(ctx context.Context, site SiteConfig, auth SystemAuth) ([]Model, error)
}

type APIKeyInventoryFetcher interface {
	ListAPIKeys(ctx context.Context, site SiteConfig, auth SystemAuth) ([]APIKey, error)
}

type APIKeySummaryFetcher interface {
	SummarizeAPIKey(ctx context.Context, site SiteConfig, apiKey string) (APIKeySummary, error)
}

type UserSummaryFetcher interface {
	FetchUserSummary(ctx context.Context, site SiteConfig, auth SystemAuth) (UserSummary, error)
}

type BalanceFetcher interface {
	FetchBalance(ctx context.Context, site SiteConfig, auth SystemAuth) (BalanceSnapshot, error)
}

type PricingFetcher interface {
	FetchPricing(ctx context.Context, site SiteConfig, auth SystemAuth) (PricingSnapshot, error)
}

type CheckinExecutor interface {
	ExecuteCheckin(ctx context.Context, site SiteConfig, auth SystemAuth) (CheckinResult, error)
}

type MetadataFetcher interface {
	FetchMetadata(ctx context.Context, site SiteConfig, auth SystemAuth) (MetadataSnapshot, error)
}

type PricingParser interface {
	ParsePricing(payload any) PricingSnapshot
}

type Registry struct {
	modulesByType map[string]Module
	modules       []Module
}

func NewRegistry() Registry {
	registry := Registry{
		modulesByType: map[string]Module{},
	}
	registry.Register(NewCodex())
	registry.Register(NewAntigravity())
	registry.Register(NewClaudeCode())
	registry.Register(NewAnthropic())
	registry.Register(NewZhipu())
	registry.Register(NewGLMCode())
	registry.Register(NewOpenAICompatible())
	registry.Register(NewNewAPI())
	registry.Register(NewXLyra())
	registry.Register(NewGoogle())
	registry.Register(NewGrok())
	registry.Register(NewOpenCodeGo())
	return registry
}

func (r *Registry) Register(module Module) {
	r.modules = append(r.modules, module)
	for _, siteType := range module.SiteTypes() {
		normalized := normalizeSiteType(siteType)
		if normalized == "" {
			continue
		}
		r.modulesByType[normalized] = module
	}
}

func (r Registry) ModuleForSiteType(siteType string) (Module, bool) {
	module, ok := r.modulesByType[normalizeSiteType(siteType)]
	return module, ok
}

func (r Registry) Modules() []Module {
	modules := make([]Module, len(r.modules))
	copy(modules, r.modules)
	return modules
}

func AsCredentialValidator(module Module) (CredentialValidator, bool) {
	value, ok := module.(CredentialValidator)
	return value, ok
}

func AsHealthProbe(module Module) (HealthProbe, bool) {
	value, ok := module.(HealthProbe)
	return value, ok
}

func AsDefaultBaseURLProvider(module Module) (DefaultBaseURLProvider, bool) {
	value, ok := module.(DefaultBaseURLProvider)
	return value, ok
}

func AsSystemCredentialValidator(module Module) (SystemCredentialValidator, bool) {
	value, ok := module.(SystemCredentialValidator)
	return value, ok
}

func AsDetector(module Module) (Detector, bool) {
	value, ok := module.(Detector)
	return value, ok
}

func AsGatewayModelLister(module Module) (GatewayModelLister, bool) {
	value, ok := module.(GatewayModelLister)
	return value, ok
}

func AsSystemModelLister(module Module) (SystemModelLister, bool) {
	value, ok := module.(SystemModelLister)
	return value, ok
}

func AsAPIKeyInventoryFetcher(module Module) (APIKeyInventoryFetcher, bool) {
	value, ok := module.(APIKeyInventoryFetcher)
	return value, ok
}

func AsAPIKeySummaryFetcher(module Module) (APIKeySummaryFetcher, bool) {
	value, ok := module.(APIKeySummaryFetcher)
	return value, ok
}

func AsUserSummaryFetcher(module Module) (UserSummaryFetcher, bool) {
	value, ok := module.(UserSummaryFetcher)
	return value, ok
}

func AsBalanceFetcher(module Module) (BalanceFetcher, bool) {
	value, ok := module.(BalanceFetcher)
	return value, ok
}

func AsPricingFetcher(module Module) (PricingFetcher, bool) {
	value, ok := module.(PricingFetcher)
	return value, ok
}

func AsCheckinExecutor(module Module) (CheckinExecutor, bool) {
	value, ok := module.(CheckinExecutor)
	return value, ok
}

func AsMetadataFetcher(module Module) (MetadataFetcher, bool) {
	value, ok := module.(MetadataFetcher)
	return value, ok
}

func AsPricingParser(module Module) (PricingParser, bool) {
	value, ok := module.(PricingParser)
	return value, ok
}

func ModuleCapabilities(module Module) []Capability {
	if describer, ok := module.(CapabilityDescriber); ok {
		return uniqueCapabilities(describer.Capabilities())
	}

	capabilities := make([]Capability, 0)
	if _, ok := AsDetector(module); ok {
		capabilities = append(capabilities, CapabilityDetect)
	}
	if _, ok := AsHealthProbe(module); ok {
		capabilities = append(capabilities, CapabilityHealthProbe)
	}
	if _, ok := AsCredentialValidator(module); ok {
		capabilities = append(capabilities, CapabilityValidateCredential)
	}
	if _, ok := AsSystemCredentialValidator(module); ok {
		capabilities = append(capabilities, CapabilityValidateCredential)
	}
	if _, ok := AsGatewayModelLister(module); ok {
		capabilities = append(capabilities, CapabilityListModels)
	}
	if _, ok := AsSystemModelLister(module); ok {
		capabilities = append(capabilities, CapabilityListModels)
	}
	if _, ok := AsAPIKeyInventoryFetcher(module); ok {
		capabilities = append(capabilities, CapabilityListAPIKeys)
	}
	if _, ok := AsAPIKeySummaryFetcher(module); ok {
		capabilities = append(capabilities, CapabilitySummarizeAPIKey)
	}
	if _, ok := AsUserSummaryFetcher(module); ok {
		capabilities = append(capabilities, CapabilityFetchUserSummary)
	}
	if _, ok := AsBalanceFetcher(module); ok {
		capabilities = append(capabilities, CapabilityFetchBalance)
	}
	if _, ok := AsPricingFetcher(module); ok {
		capabilities = append(capabilities, CapabilityFetchPricing)
	}
	if _, ok := AsCheckinExecutor(module); ok {
		capabilities = append(capabilities, CapabilityCheckin)
	}
	if _, ok := AsMetadataFetcher(module); ok {
		capabilities = append(capabilities, CapabilityFetchMetadata)
	}

	return uniqueCapabilities(capabilities)
}

func normalizeSiteType(siteType string) string {
	return strings.TrimSpace(siteType)
}

func uniqueCapabilities(values []Capability) []Capability {
	seen := map[Capability]struct{}{}
	result := make([]Capability, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}
