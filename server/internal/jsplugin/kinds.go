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

// kindSpec describes one kind: what it exports, how it is called and what it may touch.
type kindSpec struct {
	Name   string
	Family KindFamily
	Hooks  []string
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
}

var kindSpecs = []kindSpec{
	{Name: KindQuotaProbe, Family: FamilyStepped, Hooks: []string{"probe"}, Timeout: probeHookTimeout, Credential: "api_key", Connected: true, Section: "quotaProbe"},
	{Name: KindProtocol, Family: FamilyProtocol, Hooks: []string{"decodeRequest", "buildRequest", "parseResponse"}, Timeout: protocolHookTimeout, HotPath: true, Credential: "api_key", Connected: true},
	{Name: KindModelList, Family: FamilyStepped, Hooks: []string{"listModels"}, Timeout: probeHookTimeout, Credential: "api_key", Section: "site"},
	{Name: KindCredentialCheck, Family: FamilyStepped, Hooks: []string{"check"}, Timeout: probeHookTimeout, Credential: "api_key", Section: "site"},
	{Name: KindSiteDetect, Family: FamilyStepped, Hooks: []string{"detect"}, Timeout: probeHookTimeout, Credential: "none", Section: "site"},
	{Name: KindErrorClassifier, Family: FamilyOneShot, Hooks: []string{"classify"}, Timeout: protocolHookTimeout, HotPath: true, Credential: "none"},
	{Name: KindModelMetadata, Family: FamilyOneShot, Hooks: []string{"describeModels"}, Timeout: probeHookTimeout, Credential: "none"},
	{Name: KindPricingParse, Family: FamilyOneShot, Hooks: []string{"parsePricing"}, Timeout: probeHookTimeout, Credential: "none"},
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
	Name       string
	Family     KindFamily
	Hooks      []string
	Credential string
	Connected  bool
}

// SupportedKinds lists every kind this build accepts in a manifest.
func SupportedKinds() []KindInfo {
	out := make([]KindInfo, 0, len(kindSpecs))
	for _, spec := range kindSpecs {
		out = append(out, KindInfo{
			Name:       spec.Name,
			Family:     spec.Family,
			Hooks:      append([]string(nil), spec.Hooks...),
			Credential: spec.Credential,
			Connected:  spec.Connected,
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
