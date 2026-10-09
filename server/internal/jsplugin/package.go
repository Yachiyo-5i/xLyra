package jsplugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"xlyra/server/internal/version"
)

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*[a-z0-9]$`)

// Manifest is the plugin package manifest.
type Manifest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	APIVersion  int               `json:"apiVersion"`
	HostAPI     int               `json:"hostApi"`
	XLyra       string            `json:"xlyra"`
	Kind        string            `json:"kind"`
	Description string            `json:"description"`
	License     string            `json:"license"`
	QuotaProbe  QuotaProbeSection `json:"quotaProbe"`
	Site        SiteSection       `json:"site"`
	Protocol    ProtocolSection   `json:"protocol"`
	SHA256      map[string]string `json:"sha256"`
}

// QuotaProbeSection is the quota_probe manifest block.
type QuotaProbeSection struct {
	BaseURLMode    string `json:"baseURLMode"`
	DefaultBaseURL string `json:"defaultBaseURL,omitempty"`
	Replaces       string `json:"replaces,omitempty"`
}

// SiteSection is the manifest block for the site-facing kinds other than
// quota_probe (model_list, credential_check, site_detect).
type SiteSection struct {
	BaseURLMode    string `json:"baseURLMode,omitempty"`
	DefaultBaseURL string `json:"defaultBaseURL,omitempty"`
	// PricingPath is where xLyra fetches the site's price table for a
	// pricing_parse plugin. Required for that kind, ignored by the others.
	PricingPath string `json:"pricingPath,omitempty"`
}

// BaseURLSettings returns how the site address is prepared for the plugin.
func (m Manifest) BaseURLSettings() (mode, defaultBase string) {
	if m.Kind == KindQuotaProbe {
		return m.QuotaProbe.BaseURLMode, m.QuotaProbe.DefaultBaseURL
	}
	return m.Site.BaseURLMode, m.Site.DefaultBaseURL
}

// ProtocolSection is the protocol manifest block. Method and Auth are applied
// by Go; the plugin never sees the upstream credential.
type ProtocolSection struct {
	Name           string `json:"name"`
	DownstreamPath string `json:"downstreamPath"`
	EndpointType   string `json:"endpointType"`
	Method         string `json:"method"`
	Auth           string `json:"auth"`
	DefaultBaseURL string `json:"defaultBaseURL,omitempty"`
}

// AuthHeader returns the header that carries the credential, or "" for auth "none".
func (s ProtocolSection) AuthHeader() string {
	switch {
	case s.Auth == "bearer":
		return "Authorization"
	case strings.HasPrefix(s.Auth, "header:"):
		return strings.TrimSpace(strings.TrimPrefix(s.Auth, "header:"))
	default:
		return ""
	}
}

// Plugin is a loaded, compiled plugin of one kind.
type Plugin struct {
	Manifest Manifest
	program  *program
	pool     *pool
	fixtures []Fixture
	gen      int64
	source   string
}

// ProbeType is the Go probe type this plugin replaces, such as "kimi".
func (p *Plugin) ProbeType() string {
	return p.Manifest.QuotaProbe.Replaces
}

// ProtocolName is the gateway protocol name from the manifest.
func (p *Plugin) ProtocolName() string {
	return p.Manifest.Protocol.Name
}

// Fixtures returns the package samples.
func (p *Plugin) Fixtures() []Fixture {
	if p == nil {
		return nil
	}
	return append([]Fixture(nil), p.fixtures...)
}

// NewPlugin compiles one module and checks its manifest. resident is the
// number of runtimes kept warm; builtins use the default pool size.
func NewPlugin(manifest Manifest, source string, fixtures []Fixture, resident int) (*Plugin, error) {
	if manifest.Kind == KindQuotaProbe && manifest.QuotaProbe.BaseURLMode == "" {
		manifest.QuotaProbe.BaseURLMode = "as_is"
	}
	if spec, ok := lookupKind(manifest.Kind); ok && spec.Section == "site" && manifest.Site.BaseURLMode == "" {
		manifest.Site.BaseURLMode = "as_is"
	}
	if manifest.Kind == KindProtocol {
		manifest.Protocol.Method = strings.ToUpper(strings.TrimSpace(manifest.Protocol.Method))
		if manifest.Protocol.Method == "" {
			manifest.Protocol.Method = "POST"
		}
		if manifest.Protocol.Auth == "" {
			manifest.Protocol.Auth = "bearer"
		}
	}
	if err := validateManifest(manifest, source); err != nil {
		return nil, err
	}
	program, err := compileProgram(manifest.ID+".js", source, kindHooksOf(manifest.Kind)...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.ID, err)
	}
	if spec, ok := lookupKind(manifest.Kind); ok {
		if err := program.addOptionalHooks(spec.OptionalHooks); err != nil {
			return nil, fmt.Errorf("%s: %w", manifest.ID, err)
		}
	}
	if _, signs := program.hooks[HookSignRequest]; signs && manifest.Protocol.Auth != "none" {
		return nil, fmt.Errorf("%s: signRequest needs protocol.auth \"none\", so the credential is only used for signing", manifest.ID)
	}
	loaded, err := newSession(program)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.ID, err)
	}
	meta, err := loaded.exportMeta()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.ID, err)
	}
	if err := metaMatches(manifest, meta); err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.ID, err)
	}
	maxPool := probePoolMax
	if spec, _ := lookupKind(manifest.Kind); spec.HotPath {
		if resident == 0 {
			resident = protocolPoolResident
		}
		maxPool = protocolPoolMax
	} else if resident == 0 {
		resident = probePoolResident
	}
	plugin := &Plugin{Manifest: manifest, program: program, fixtures: fixtures, gen: 1, source: source}
	pool, err := newPool(resident, maxPool, func() (*session, error) {
		return newSession(program)
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", manifest.ID, err)
	}
	plugin.pool = pool
	return plugin, nil
}

// CallProbe runs the probe hook once.
func (p *Plugin) CallProbe(ctx context.Context, probeCtx ProbeContext, steps []ProbeStep) (Decision, error) {
	if p == nil || p.pool == nil {
		return Decision{}, fmt.Errorf("plugin is not loaded")
	}
	started := time.Now()
	session, err := p.pool.Acquire(ctx)
	if err != nil {
		return Decision{}, annotate(p, err, "probe", len(steps), time.Since(started).Milliseconds())
	}
	hook, ok := p.program.hooks["probe"]
	if !ok {
		p.pool.Release(session, true)
		return Decision{}, fmt.Errorf("probe hook is missing")
	}
	out, logs, dropped, discard, callErr := session.call(ctx, probeHookTimeout, hook, []any{probeCtx.asMap(), stepsAsValue(steps)})
	p.pool.Release(session, discard || fatalRuntime(callErr))
	duration := time.Since(started).Milliseconds()
	if callErr != nil {
		return Decision{Logs: logs, DroppedLogs: dropped}, annotate(p, callErr, "probe", len(steps), duration)
	}
	decision, err := decodeDecision(out)
	if err != nil {
		return Decision{Logs: logs, DroppedLogs: dropped}, annotate(p, err, "probe", len(steps), duration)
	}
	decision.Logs = logs
	decision.DroppedLogs = dropped
	return decision, nil
}

func kindHooksOf(kind string) []string {
	spec, _ := lookupKind(kind)
	return spec.Hooks
}

func validateBaseURLMode(mode string) error {
	switch mode {
	case "trim_v1", "trim_v1_fold", "origin", "as_is":
		return nil
	}
	return fmt.Errorf("baseURLMode %q is not supported", mode)
}

func validateManifest(manifest Manifest, source string) error {
	if !pluginIDPattern.MatchString(manifest.ID) || len(manifest.ID) < 3 || len(manifest.ID) > 64 {
		return fmt.Errorf("invalid plugin id %q", manifest.ID)
	}
	if manifest.APIVersion != hookAPIVersion {
		return fmt.Errorf("apiVersion %d is not supported", manifest.APIVersion)
	}
	if manifest.HostAPI > hostAPIVersion {
		return fmt.Errorf("hostApi %d is not supported", manifest.HostAPI)
	}
	if manifest.HostAPI < 1 {
		return fmt.Errorf("hostApi must be at least 1")
	}
	if err := versionAllowed(manifest.XLyra, version.Current().Version); err != nil {
		return err
	}
	sum, ok := manifest.SHA256["plugin.js"]
	if !ok || !strings.EqualFold(sum, hashSource(source)) {
		return fmt.Errorf("plugin.js sha256 does not match the manifest")
	}
	switch manifest.Kind {
	case KindQuotaProbe:
		if err := validateBaseURLMode(manifest.QuotaProbe.BaseURLMode); err != nil {
			return fmt.Errorf("quotaProbe.%w", err)
		}
		if manifest.QuotaProbe.Replaces != "" && !strings.HasPrefix(manifest.ID, "xlyra.") {
			return fmt.Errorf("only xlyra plugins can set quotaProbe.replaces")
		}
	case KindProtocol:
		if strings.TrimSpace(manifest.Protocol.Name) == "" {
			return fmt.Errorf("protocol.name is required")
		}
		if strings.TrimSpace(manifest.Protocol.DownstreamPath) == "" {
			return fmt.Errorf("protocol.downstreamPath is required")
		}
		if strings.TrimSpace(manifest.Protocol.EndpointType) == "" {
			return fmt.Errorf("protocol.endpointType is required")
		}
		switch manifest.Protocol.Auth {
		case "none", "bearer":
		default:
			if !strings.HasPrefix(manifest.Protocol.Auth, "header:") {
				return fmt.Errorf("protocol.auth %q is not supported", manifest.Protocol.Auth)
			}
		}
	case KindModelList, KindCredentialCheck, KindSiteDetect:
		if mode := manifest.Site.BaseURLMode; mode != "" {
			if err := validateBaseURLMode(mode); err != nil {
				return fmt.Errorf("site.%w", err)
			}
		}
	case KindPricingParse:
		if mode := manifest.Site.BaseURLMode; mode != "" {
			if err := validateBaseURLMode(mode); err != nil {
				return fmt.Errorf("site.%w", err)
			}
		}
		if err := validatePluginPath(manifest.Site.PricingPath); err != nil {
			return fmt.Errorf("site.pricingPath: %w", err)
		}
	case KindErrorClassifier, KindModelMetadata:
		// No manifest section: these kinds are pure functions of their input.
	default:
		return unsupportedKindError(manifest.Kind)
	}
	return nil
}

func metaMatches(manifest Manifest, meta map[string]any) error {
	if got, _ := meta["id"].(string); got != manifest.ID {
		return fmt.Errorf("meta.id %q does not match manifest", got)
	}
	if got, _ := meta["kind"].(string); got != manifest.Kind {
		return fmt.Errorf("meta.kind %q does not match manifest", got)
	}
	if !metaVersion(meta["apiVersion"], int64(manifest.APIVersion)) {
		return fmt.Errorf("meta.apiVersion does not match manifest")
	}
	return nil
}

func metaVersion(value any, want int64) bool {
	switch typed := value.(type) {
	case int64:
		return typed == want
	case int:
		return int64(typed) == want
	case float64:
		return typed == float64(want)
	default:
		return false
	}
}

func hashSource(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func versionAllowed(constraint, current string) error {
	current = strings.TrimSpace(current)
	if current == "" || current == "dev" {
		return nil
	}
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil
	}
	for _, part := range strings.Fields(constraint) {
		if !strings.HasPrefix(part, ">=") {
			return fmt.Errorf("unsupported xlyra constraint %q", part)
		}
		if semverLess(current, strings.TrimPrefix(part, ">=")) {
			return fmt.Errorf("xlyra %s does not satisfy %s", current, constraint)
		}
	}
	return nil
}

func semverLess(current, minimum string) bool {
	left := semverParts(current)
	right := semverParts(minimum)
	for i := 0; i < 3; i++ {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}

func semverParts(value string) [3]int {
	value, _, _ = strings.Cut(value, "-")
	parts := strings.Split(value, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		number, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}
		}
		out[i] = number
	}
	return out
}

// CallDecodeRequest runs the decodeRequest hook once.
func (p *Plugin) CallDecodeRequest(ctx context.Context, endpointCtx ProtocolEndpointContext, payload map[string]any) (ProtocolDecodeResult, *ProtocolDecodeFailure, []LogEntry, int, error) {
	out, logs, dropped, err := p.callHook(ctx, "decodeRequest", protocolHookTimeout, 0, endpointCtx.asMap(), payload)
	if err != nil {
		return ProtocolDecodeResult{}, nil, logs, dropped, err
	}
	result, failure, decodeErr := decodeProtocolDecode(out)
	return result, failure, logs, dropped, decodeErr
}

// CallBuildRequest runs the buildRequest hook once.
func (p *Plugin) CallBuildRequest(ctx context.Context, buildCtx ProtocolBuildContext, payload map[string]any) (ProtocolBuiltRequest, []LogEntry, int, error) {
	out, logs, dropped, err := p.callHook(ctx, "buildRequest", protocolHookTimeout, 0, buildCtx.asMap(), payload)
	if err != nil {
		return ProtocolBuiltRequest{}, logs, dropped, err
	}
	built, decodeErr := decodeProtocolBuild(out)
	return built, logs, dropped, decodeErr
}

// HasHook reports whether the plugin exports the hook (optional hooks may be absent).
func (p *Plugin) HasHook(name string) bool {
	if p == nil || p.program == nil {
		return false
	}
	_, ok := p.program.hooks[name]
	return ok
}

// CallSignRequest runs the optional signRequest hook once.
func (p *Plugin) CallSignRequest(ctx context.Context, buildCtx ProtocolBuildContext, input ProtocolSignInput) (ProtocolSignResult, []LogEntry, int, error) {
	out, logs, dropped, err := p.callHook(ctx, HookSignRequest, protocolHookTimeout, 0, buildCtx.asMap(), signInputAsMap(input))
	if err != nil {
		return ProtocolSignResult{}, logs, dropped, err
	}
	signed, decodeErr := decodeProtocolSign(out)
	return signed, logs, dropped, decodeErr
}

// CallParseError runs the optional parseError hook once.
func (p *Plugin) CallParseError(ctx context.Context, buildCtx ProtocolBuildContext, input ProtocolErrorInput) (ProtocolErrorResult, []LogEntry, int, error) {
	out, logs, dropped, err := p.callHook(ctx, HookParseError, protocolHookTimeout, 0, buildCtx.asMap(), parseInputAsMap(input))
	if err != nil {
		return ProtocolErrorResult{}, logs, dropped, err
	}
	parsed, decodeErr := decodeProtocolError(out)
	return parsed, logs, dropped, decodeErr
}

// CallParseResponse runs the parseResponse hook once.
func (p *Plugin) CallParseResponse(ctx context.Context, buildCtx ProtocolBuildContext, input ProtocolParseInput) (ProtocolParsedResponse, []LogEntry, int, error) {
	out, logs, dropped, err := p.callHook(ctx, "parseResponse", protocolHookTimeout, 0, buildCtx.asMap(), parseInputAsMap(input))
	if err != nil {
		return ProtocolParsedResponse{}, logs, dropped, err
	}
	parsed, decodeErr := decodeProtocolParse(out)
	return parsed, logs, dropped, decodeErr
}

func (p *Plugin) callHook(ctx context.Context, hookName string, timeout time.Duration, step int, args ...any) (any, []LogEntry, int, error) {
	if p == nil || p.pool == nil {
		return nil, nil, 0, fmt.Errorf("plugin is not loaded")
	}
	hook, ok := p.program.hooks[hookName]
	if !ok {
		return nil, nil, 0, fmt.Errorf("%s hook is missing", hookName)
	}
	started := time.Now()
	session, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, 0, annotate(p, err, hookName, step, time.Since(started).Milliseconds())
	}
	out, logs, dropped, discard, callErr := session.call(ctx, timeout, hook, args)
	p.pool.Release(session, discard || fatalRuntime(callErr))
	duration := time.Since(started).Milliseconds()
	if callErr != nil {
		return nil, logs, dropped, annotate(p, callErr, hookName, step, duration)
	}
	return out, logs, dropped, nil
}

// HashSource is the sha256 used in a manifest, exposed for tests.
func HashSource(source string) string { return hashSource(source) }

// ParseManifest decodes a manifest JSON document.
func ParseManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
