package jsplugin

import (
	"fmt"
	"strings"
	"time"
)

// Plugin kinds. The set is fixed by this build: a package declares one of
// these in its manifest and cannot define its own.
const (
	KindQuotaProbe      = "quota_probe"
	KindProtocol        = "protocol"
	KindModelList       = "model_list"
	KindCredentialCheck = "credential_check"
	KindSiteDetect      = "site_detect"
	KindErrorClassifier = "error_classifier"
	KindModelMetadata   = "model_metadata"
	KindPricingParse    = "pricing_parse"
)

// KindFamily groups kinds that share a driver.
type KindFamily string

const (
	// FamilyStepped kinds return the next HTTP request or a final result, the
	// way quota probes do. The host sends the requests.
	FamilyStepped KindFamily = "stepped"
	// FamilyProtocol is the request/response translator for one upstream.
	FamilyProtocol KindFamily = "protocol"
	// FamilyOneShot kinds are pure functions: one call in, one result out.
	FamilyOneShot KindFamily = "one_shot"
)

// Optional protocol hooks.
const (
	// HookSignRequest decides what to sign for an upstream request; Go signs it.
	HookSignRequest = "signRequest"
	// HookParseError rewrites the error body of a non-2xx upstream response.
	HookParseError = "parseError"
)

// Scope says where a plugin of a kind takes effect once enabled. It is also the
// scope_type of the bindings an admin makes.
type Scope string

const (
	// ScopeGlobal applies to every site and needs no binding.
	ScopeGlobal Scope = "global"
	// ScopeSite needs an admin to bind the plugin to a site. A site holds one
	// plugin per kind.
	ScopeSite Scope = "site"
	// ScopeEndpoint gets a downstream path, /v1/plugins/<slug>.
	ScopeEndpoint Scope = "endpoint"
)

// kindSpec describes one kind: what it exports, how it is called and what it may touch.
type kindSpec struct {
	Name   string
	Family KindFamily
	Hooks  []string
	// OptionalHooks may be exported on top of Hooks. A plugin that omits one gets
	// the default behaviour; the host only calls a hook the plugin exports.
	OptionalHooks []string
	// Timeout is the budget of one hook call.
	Timeout time.Duration
	// HotPath kinds run while a client request is waiting and use the larger runtime pool.
	HotPath bool
	// Credential is what the host adds to requests: "none" or "api_key".
	Credential string
	// Connected reports whether xLyra calls plugins of this kind. A kind that is
	// defined but not connected can be built, verified and uploaded, but not enabled.
	Connected bool
	// Section is the manifest block that carries the site address settings, if any.
	Section string
	// Scope says where an enabled plugin of this kind takes effect.
	Scope Scope
}

var kindSpecs = []kindSpec{
	{Name: KindQuotaProbe, Family: FamilyStepped, Hooks: []string{"probe"}, Timeout: probeHookTimeout, Credential: "api_key", Connected: true, Section: "quotaProbe", Scope: ScopeSite},
	{Name: KindProtocol, Family: FamilyProtocol, Hooks: []string{"decodeRequest", "buildRequest", "parseResponse"}, OptionalHooks: []string{HookSignRequest, HookParseError}, Timeout: protocolHookTimeout, HotPath: true, Credential: "api_key", Connected: true, Scope: ScopeEndpoint},
	{Name: KindModelList, Family: FamilyStepped, Hooks: []string{"listModels"}, Timeout: probeHookTimeout, Credential: "api_key", Connected: true, Section: "site", Scope: ScopeSite},
	{Name: KindCredentialCheck, Family: FamilyStepped, Hooks: []string{"check"}, Timeout: probeHookTimeout, Credential: "api_key", Connected: true, Section: "site", Scope: ScopeSite},
	{Name: KindSiteDetect, Family: FamilyStepped, Hooks: []string{"detect"}, Timeout: probeHookTimeout, Credential: "none", Connected: true, Section: "site", Scope: ScopeGlobal},
	{Name: KindErrorClassifier, Family: FamilyOneShot, Hooks: []string{"classify"}, Timeout: protocolHookTimeout, HotPath: true, Credential: "none", Connected: true, Scope: ScopeSite},
	{Name: KindModelMetadata, Family: FamilyOneShot, Hooks: []string{"describeModels"}, Timeout: probeHookTimeout, Credential: "none", Connected: true, Scope: ScopeGlobal},
	{Name: KindPricingParse, Family: FamilyOneShot, Hooks: []string{"parsePricing"}, Timeout: probeHookTimeout, Credential: "api_key", Connected: true, Section: "site", Scope: ScopeSite},
}

func lookupKind(name string) (kindSpec, bool) {
	for _, spec := range kindSpecs {
		if spec.Name == name {
			return spec, true
		}
	}
	return kindSpec{}, false
}

// KindInfo is the public description of a kind, for the CLI and the admin API.
type KindInfo struct {
	Name   string
	Family KindFamily
	Hooks  []string
	// OptionalHooks are exports a plugin may add; the CLI and docs list them.
	OptionalHooks []string
	Credential    string
	Connected     bool
	Scope         Scope
}

// SupportedKinds lists every kind this build accepts in a manifest.
func SupportedKinds() []KindInfo {
	out := make([]KindInfo, 0, len(kindSpecs))
	for _, spec := range kindSpecs {
		out = append(out, KindInfo{
			Name:          spec.Name,
			Family:        spec.Family,
			Hooks:         append([]string(nil), spec.Hooks...),
			OptionalHooks: append([]string(nil), spec.OptionalHooks...),
			Credential:    spec.Credential,
			Connected:     spec.Connected,
			Scope:         spec.Scope,
		})
	}
	return out
}

// KindNotConnectedError is returned when a kind is defined but xLyra does not call it yet.
type KindNotConnectedError struct {
	Kind string
}

func (e *KindNotConnectedError) Error() string {
	return fmt.Sprintf("kind %q is defined but not connected to xLyra yet, so it cannot be enabled", e.Kind)
}

func unsupportedKindError(kind string) error {
	names := make([]string, 0, len(kindSpecs))
	for _, spec := range kindSpecs {
		names = append(names, spec.Name)
	}
	return fmt.Errorf("kind %q is not supported in this build (supported: %s)", kind, strings.Join(names, ", "))
}

// ScopeOf returns where plugins of a kind take effect; it is empty for an
// unknown kind.
func ScopeOf(kind string) Scope {
	spec, ok := lookupKind(kind)
	if !ok {
		return ""
	}
	return spec.Scope
}

// SiteBound reports whether plugins of this kind take effect only on sites an
// admin binds them to (as opposed to applying everywhere).
func SiteBound(kind string) bool {
	return ScopeOf(kind) == ScopeSite
}

// GlobalKind reports whether an enabled plugin of this kind applies to every site.
func GlobalKind(kind string) bool {
	return ScopeOf(kind) == ScopeGlobal
}
