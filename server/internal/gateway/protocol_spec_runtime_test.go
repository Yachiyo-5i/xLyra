package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
)

var runtimeProviderPathCases = []struct {
	name string
	path string
	ok   bool
}{
	{name: "chat completions", path: "/v1/chat/completions", ok: true},
	{name: "paas chat completions", path: "/api/paas/v4/chat/completions", ok: true},
	{name: "encoded dotdot", path: "/%2e%2e/x", ok: false},
	{name: "encoded dot", path: "/%2E/x", ok: false},
	{name: "encoded dotdot upper", path: "/%2E%2E/x", ok: false},
	{name: "dot segment", path: "/a/./b", ok: false},
	{name: "dotdot segment", path: "/a/../b", ok: false},
	{name: "double slash", path: "//x", ok: false},
	{name: "backslash", path: "/a\\b", ok: false},
	{name: "hash", path: "/a#b", ok: false},
	{name: "scheme", path: "/a://b", ok: false},
	{name: "encoded slash", path: "/a%2fb", ok: false},
	{name: "encoded slash upper", path: "/a%2Fb", ok: false},
	{name: "encoded backslash", path: "/a%5cb", ok: false},
	{name: "encoded backslash upper", path: "/a%5Cb", ok: false},
}

const legalRuntimeProviderSpec = `{
  "protocol": "openai_chat",
  "official_base_url": "https://api.example.com",
  "path": "/v1/chat/completions",
  "documentation": ["https://example.com/docs"],
  "request_params": {
    "supported": ["plugin_marker"],
    "unsupported": ["logit_bias"],
    "allowed_values": {"service_tier": ["auto"]},
    "defaults": {"temperature": 0.2},
    "fixed": {"top_p": 1},
    "forced": {"stream": true},
    "validation": {"max_tokens": {"type": "integer", "min": 1, "max": 16}}
  }
}`

func legalRuntimeProviderSpecJSON() json.RawMessage {
	return json.RawMessage(legalRuntimeProviderSpec)
}

func pluginCandidate(siteType string) routeengine.Candidate {
	return routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: siteType},
		Model: routeengine.CandidateModel{UpstreamName: "plugin-model"},
	}
}

func TestRuntimeProviderSpecAppliesPathURLAndRequestParams(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		" Acme.Example/Chat ": legalRuntimeProviderSpecJSON(),
	}); err != nil {
		t.Fatal(err)
	}

	candidate := pluginCandidate("acme.example/chat")
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate)
	if spec.Provider != "acme.example/chat" {
		t.Fatalf("provider = %q, want acme.example/chat", spec.Provider)
	}
	if spec.Path != "/v1/chat/completions" {
		t.Fatalf("path = %q", spec.Path)
	}
	if spec.OfficialBaseURL != "https://api.example.com" {
		t.Fatalf("official base URL = %q", spec.OfficialBaseURL)
	}
	if got := upstreamPathFromSpec("", spec, "/fallback"); got != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("upstream path = %q", got)
	}
	if !containsString(spec.Documentation, "https://example.com/docs") {
		t.Fatalf("documentation = %#v", spec.Documentation)
	}
	if !containsString(spec.RequestParams.Supported, "plugin_marker") {
		t.Fatalf("supported = %#v", spec.RequestParams.Supported)
	}
	if normalizeSpecKey(candidate.Site.SiteType) != normalizeSpecKey(spec.Provider) {
		t.Fatalf("site type %q != provider key %q", candidate.Site.SiteType, spec.Provider)
	}

	payload := map[string]any{
		"model":        "plugin-model",
		"logit_bias":   map[string]any{"1": 1},
		"service_tier": "nope",
		"top_p":        0.3,
		"stream":       false,
		"custom_field": "keep",
		"max_tokens":   8,
	}
	applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIChat, candidate)
	if _, ok := payload["logit_bias"]; ok {
		t.Fatalf("logit_bias should be removed, got %#v", payload)
	}
	if _, ok := payload["service_tier"]; ok {
		t.Fatalf("service_tier should be removed, got %#v", payload)
	}
	if payload["temperature"] != 0.2 {
		t.Fatalf("temperature = %#v, want 0.2", payload["temperature"])
	}
	if payload["top_p"] != float64(1) {
		t.Fatalf("top_p = %#v, want 1", payload["top_p"])
	}
	if payload["stream"] != true {
		t.Fatalf("stream = %#v, want true", payload["stream"])
	}
	if payload["custom_field"] != "keep" {
		t.Fatalf("custom_field = %#v, supported must not filter it", payload["custom_field"])
	}

	missingTopP := map[string]any{"model": "plugin-model"}
	applyRequestPolicyForCandidate(missingTopP, canonicalProtocolOpenAIChat, candidate)
	if _, ok := missingTopP["top_p"]; ok {
		t.Fatalf("fixed top_p should not be inserted, got %#v", missingTopP)
	}
	if missingTopP["temperature"] != 0.2 || missingTopP["stream"] != true {
		t.Fatalf("defaults and forced were not applied: %#v", missingTopP)
	}

	if err := validatePayloadParams(map[string]any{"max_tokens": float64(32)}, "openai_chat", candidate); err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("expected max_tokens validation error, got %v", err)
	}
	if err := validatePayloadParams(map[string]any{"max_tokens": float64(8)}, "openai_chat", candidate); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if got := OfficialBaseURLForProvider(" ACME.EXAMPLE/CHAT "); got != "https://api.example.com" {
		t.Fatalf("OfficialBaseURLForProvider = %q", got)
	}
}

func TestSetRuntimeProviderSpecsAcceptsCanonicalProtocols(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	for _, protocol := range []string{"openai_chat", "openai_responses", "anthropic_messages", "google_gemini", " OpenAI_Chat "} {
		raw := json.RawMessage(fmt.Sprintf(
			`{"protocol":%q,"official_base_url":"https://api.example.com","path":"/v1/chat/completions"}`,
			protocol,
		))
		if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{"acme.example/chat": raw}); err != nil {
			t.Fatalf("protocol %q: %v", protocol, err)
		}
		spec := effectiveProtocolSpec(canonicalProtocol(normalizeSpecKey(protocol)), pluginCandidate("acme.example/chat"))
		if spec.Path != "/v1/chat/completions" || spec.OfficialBaseURL != "https://api.example.com" {
			t.Fatalf("protocol %q spec = path %q url %q", protocol, spec.Path, spec.OfficialBaseURL)
		}
	}
}

func TestSetRuntimeProviderSpecsReplacesSnapshot(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	first := runtimeSpecJSON("openai_chat", "https://one.example", "/v1/chat/completions", "")
	second := runtimeSpecJSON("openai_responses", "https://two.example", "/v1/responses", "")
	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		"acme.example/one": first,
		"acme.example/two": second,
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		"acme.example/two": second,
	}); err != nil {
		t.Fatal(err)
	}
	if got := OfficialBaseURLForProvider("acme.example/one"); got != "" {
		t.Fatalf("replaced key still visible: %q", got)
	}
	if got := OfficialBaseURLForProvider("acme.example/two"); got != "https://two.example" {
		t.Fatalf("remaining key = %q", got)
	}
	if err := SetRuntimeProviderSpecs(nil); err != nil {
		t.Fatal(err)
	}
	if got := OfficialBaseURLForProvider("acme.example/two"); got != "" {
		t.Fatalf("nil set left %q", got)
	}
}

func TestClearRuntimeProviderSpecs(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		"acme.example/chat": legalRuntimeProviderSpecJSON(),
	}); err != nil {
		t.Fatal(err)
	}
	ClearRuntimeProviderSpecs()

	candidate := pluginCandidate("acme.example/chat")
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate)
	if spec.Path != "" || spec.OfficialBaseURL != "" {
		t.Fatalf("cleared spec path=%q url=%q", spec.Path, spec.OfficialBaseURL)
	}
	if got := OfficialBaseURLForProvider("acme.example/chat"); got != "" {
		t.Fatalf("cleared official URL = %q", got)
	}
	payload := map[string]any{"model": "plugin-model", "logit_bias": true}
	applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIChat, candidate)
	if _, ok := payload["logit_bias"]; !ok {
		t.Fatal("cleared spec still removed logit_bias")
	}
}

func TestSetRuntimeProviderSpecsRejectsInvalidWithoutReplacingSnapshot(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		"acme.example/chat": legalRuntimeProviderSpecJSON(),
	}); err != nil {
		t.Fatal(err)
	}
	before := runtimeProviderSpecs.Load()
	if before == nil {
		t.Fatal("expected snapshot")
	}

	legal := legalRuntimeProviderSpecJSON()
	tests := []struct {
		name  string
		items map[string]json.RawMessage
		want  string
	}{
		{
			name:  "key without slash",
			items: map[string]json.RawMessage{"deepseek": legal, "acme.example/other": legal},
			want:  `must contain "/"`,
		},
		{
			name: "alternate_protocols",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat/completions",
				`,"alternate_protocols":{"anthropic_messages":{"protocol":"anthropic_messages","official_only":true}}`,
			)},
			want: "alternate_protocols",
		},
		{
			name: "protocol_request_params",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat/completions",
				`,"protocol_request_params":{"openai_chat":{"defaults":{"max_tokens":1}}}`,
			)},
			want: "protocol_request_params",
		},
		{
			name: "unknown field models",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat/completions",
				`,"models":{"gpt":{}}`,
			)},
			want: "models",
		},
		{
			name: "reasoning",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat/completions",
				`,"reasoning":{"thinking":"metadata"}`,
			)},
			want: "reasoning",
		},
		{
			name: "request_params unknown field",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat/completions",
				`,"request_params":{"unsupported_with_image_generation":["temperature"]}`,
			)},
			want: "unsupported_with_image_generation",
		},
		{
			name: "non-https official base url",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "http://api.example.com", "/v1/chat/completions", "",
			)},
			want: "official_base_url",
		},
		{
			name: "userinfo",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://user:pass@api.example.com", "/v1/chat/completions", "",
			)},
			want: "userinfo",
		},
		{
			name: "query",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com/v1?x=1", "/v1/chat/completions", "",
			)},
			want: "query",
		},
		{
			name: "fragment",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com/v1#frag", "/v1/chat/completions", "",
			)},
			want: "fragment",
		},
		{
			name: "path dotdot",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/../chat", "",
			)},
			want: "..",
		},
		{
			name: "path scheme",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/https://evil", "",
			)},
			want: "://",
		},
		{
			name: "path backslash",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1\\chat", "",
			)},
			want: "path",
		},
		{
			name: "path hash",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat#frag", "",
			)},
			want: "path",
		},
		{
			name: "path missing slash",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "v1/chat/completions", "",
			)},
			want: "path",
		},
		{
			name: "path double slash",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "//v1/chat", "",
			)},
			want: "path",
		},
		{
			name: "unknown protocol",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"codex_responses", "https://api.example.com", "/v1/chat/completions", "",
			)},
			want: "codex_responses",
		},
		{
			name: "duplicate key",
			items: map[string]json.RawMessage{
				"Acme.Example/Chat": legal,
				"acme.example/chat": legal,
			},
			want: "duplicate",
		},
		{
			name:  "empty key",
			items: map[string]json.RawMessage{"": legal},
			want:  "empty",
		},
		{
			name:  "blank key",
			items: map[string]json.RawMessage{"  ": legal},
			want:  "empty",
		},
		{
			name: "request_params conflict",
			items: map[string]json.RawMessage{"acme.example/chat": runtimeSpecJSON(
				"openai_chat", "https://api.example.com", "/v1/chat/completions",
				`,"request_params":{"defaults":{"temperature":0.2},"fixed":{"temperature":1}}`,
			)},
			want: "fixed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetRuntimeProviderSpecs(tt.items)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.want)
			}
			if runtimeProviderSpecs.Load() != before {
				t.Fatal("snapshot changed")
			}
			if got := OfficialBaseURLForProvider("acme.example/chat"); got != "https://api.example.com" {
				t.Fatalf("override = %q", got)
			}
			if got := OfficialBaseURLForProvider("acme.example/other"); got != "" {
				t.Fatalf("rejected key became visible: %q", got)
			}
		})
	}
}

func TestBuiltinProviderSpecsStayStableAcrossRuntimeOverrides(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	deepseek := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: " DeepSeek ", BaseURL: "https://api.deepseek.com"},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-chat"},
	}
	zhipu := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "zhipu"},
		Model: routeengine.CandidateModel{UpstreamName: "glm-4.5-air"},
	}
	before := builtinSpecSnapshot(t, deepseek, zhipu)

	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		"acme.example/chat": legalRuntimeProviderSpecJSON(),
	}); err != nil {
		t.Fatal(err)
	}
	if got := builtinSpecSnapshot(t, deepseek, zhipu); got != before {
		t.Fatal("built-in specs changed after Set")
	}
	ClearRuntimeProviderSpecs()
	if got := builtinSpecSnapshot(t, deepseek, zhipu); got != before {
		t.Fatal("built-in specs changed after Clear")
	}
}

func TestBuiltinProviderSpecWinsOverRuntimeKey(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	before := OfficialBaseURLForProvider("deepseek")
	if before == "" {
		t.Fatal("expected deepseek official base URL")
	}
	runtimeProviderSpecs.Store(&runtimeProviderSpecSnapshot{
		providers: map[string]providerSpecDefinition{
			"deepseek": {Protocol: "openai_chat", OfficialBaseURL: "https://evil.example", Path: "/evil"},
		},
	})
	if got := OfficialBaseURLForProvider("deepseek"); got != before {
		t.Fatalf("builtin URL = %q, want %q", got, before)
	}
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: "deepseek"},
	})
	if spec.OfficialBaseURL != before || spec.Path == "/evil" {
		t.Fatalf("effective spec used the override: %#v", spec)
	}
}

func TestOfficialOnlyMatchesPluginSiteTypeKey(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	// Set rejects alternate_protocols. Seed the read path directly so the
	// official_only comparison is what decides the match.
	const siteType = "acme.example/chat"
	runtimeProviderSpecs.Store(&runtimeProviderSpecSnapshot{
		providers: map[string]providerSpecDefinition{
			siteType: {
				Protocol:        "openai_chat",
				OfficialBaseURL: "https://api.example.com",
				Path:            "/v1/chat/completions",
				AlternateProtocols: map[string]alternateProtocolDefinition{
					"anthropic_messages": {
						Protocol:     "anthropic_messages",
						BaseURL:      "https://api.example.com",
						Path:         "/v1/messages",
						OfficialOnly: true,
					},
				},
			},
		},
	})

	alt, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: " Acme.Example/Chat "},
	})
	if !ok {
		t.Fatal("official_only should match when the site type equals the provider key")
	}
	if alt.Path != "/v1/messages" || !alt.OfficialOnly {
		t.Fatalf("alternate = %#v", alt)
	}
	if _, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: "other.example/chat"},
	}); ok {
		t.Fatal("a different plugin site type must not use this alternate protocol")
	}
	if _, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: "newapi"},
	}); ok {
		t.Fatal("generic site type must not match official_only")
	}
}

func TestRuntimeProviderSpecsConcurrentReadSetClear(t *testing.T) {
	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)

	deepseekURL := OfficialBaseURLForProvider("deepseek")
	zhipuURL := OfficialBaseURLForProvider("zhipu")
	if deepseekURL == "" || zhipuURL == "" {
		t.Fatal("expected built-in official base URLs")
	}
	specFor := func(baseURL, requestPath string) json.RawMessage {
		return runtimeSpecJSON("openai_chat", baseURL, requestPath, "")
	}
	left := map[string]json.RawMessage{"acme.example/one": specFor("https://one.example", "/v1/chat/completions")}
	right := map[string]json.RawMessage{"acme.example/two": specFor("https://two.example", "/v1/responses")}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	errCh := make(chan string, 1)
	report := func(format string, args ...any) {
		select {
		case errCh <- fmt.Sprintf(format, args...):
		default:
		}
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				snap := runtimeProviderSpecs.Load()
				if snap != nil {
					one := strings.TrimSpace(snap.providers["acme.example/one"].OfficialBaseURL)
					two := strings.TrimSpace(snap.providers["acme.example/two"].OfficialBaseURL)
					switch {
					case one == "https://one.example" && two == "":
					case two == "https://two.example" && one == "":
					case one == "" && two == "":
					default:
						report("inconsistent snapshot one=%q two=%q", one, two)
						return
					}
				}
				if got := OfficialBaseURLForProvider("deepseek"); got != deepseekURL {
					report("deepseek official base URL = %q", got)
					return
				}
				if got := OfficialBaseURLForProvider("zhipu"); got != zhipuURL {
					report("zhipu official base URL = %q", got)
					return
				}
				spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, pluginCandidate("acme.example/one"))
				if spec.OfficialBaseURL != "" && spec.OfficialBaseURL != "https://one.example" {
					report("effective spec URL = %q", spec.OfficialBaseURL)
					return
				}
				if spec.OfficialBaseURL == "https://one.example" && spec.Path != "/v1/chat/completions" {
					report("partial spec path %q", spec.Path)
					return
				}
				for key := range spec.RequestParams.Defaults {
					_ = spec.RequestParams.Defaults[key]
				}
				_ = applyRequestPolicyForCandidate(map[string]any{"model": "glm-4.5-air", "temperature": 0.2}, canonicalProtocolOpenAIChat, routeengine.Candidate{
					Site:  routeengine.CandidateSite{SiteType: " Zhipu "},
					Model: routeengine.CandidateModel{UpstreamName: "glm-4.5-air"},
				})
			}
		}()
	}

	for i := 0; i < 100; i++ {
		items := left
		if i%2 == 1 {
			items = right
		}
		if err := SetRuntimeProviderSpecs(items); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
		if i%5 == 4 {
			ClearRuntimeProviderSpecs()
		}
	}
	close(stop)
	wg.Wait()
	select {
	case msg := <-errCh:
		t.Fatal(msg)
	default:
	}
}

func builtinSpecSnapshot(t *testing.T, deepseek routeengine.Candidate, zhipu routeengine.Candidate) string {
	t.Helper()
	var b strings.Builder
	for _, protocol := range []canonicalProtocol{canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses} {
		for _, candidate := range []routeengine.Candidate{deepseek, zhipu} {
			raw, err := json.Marshal(effectiveProtocolSpec(protocol, candidate))
			if err != nil {
				t.Fatal(err)
			}
			b.Write(raw)
			b.WriteByte('\n')
		}
	}
	for _, provider := range []string{"deepseek", " DeepSeek ", "zhipu", "Zhipu"} {
		b.WriteString(OfficialBaseURLForProvider(provider))
		b.WriteByte('\n')
	}
	for _, model := range []string{"deepseek-chat", "deepseek-v4-pro", "glm-4.5", "glm-5.2"} {
		raw, err := json.Marshal(ReasoningEffortSpecForModel(model))
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	alt, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "deepseek", BaseURL: "https://api.deepseek.com"},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	})
	if !ok || !alt.OfficialOnly {
		t.Fatal("expected deepseek official_only alternate")
	}
	raw, err := json.Marshal(alt)
	if err != nil {
		t.Fatal(err)
	}
	b.Write(raw)
	return b.String()
}

func TestValidateRuntimeProviderPath(t *testing.T) {
	for _, tt := range runtimeProviderPathCases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuntimeProviderPath(tt.path)
			if tt.ok && err != nil {
				t.Fatalf("path %q: %v", tt.path, err)
			}
			if !tt.ok && err == nil {
				t.Fatalf("path %q was accepted", tt.path)
			}
		})
	}
}

func TestValidateRuntimeProviderPathAgreesWithJSPlugin(t *testing.T) {
	const base = "https://api.example.com"
	for _, tt := range runtimeProviderPathCases {
		t.Run(tt.name, func(t *testing.T) {
			_, pluginErr := jsplugin.ResolveProbeURL(base, tt.path, nil)
			runtimeErr := validateRuntimeProviderPath(tt.path)
			if (pluginErr == nil) != (runtimeErr == nil) {
				t.Fatalf("path %q plugin=%v runtime=%v", tt.path, pluginErr, runtimeErr)
			}
		})
	}
}

func runtimeSpecJSON(protocol string, baseURL string, requestPath string, extra string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(
		`{"protocol":%q,"official_base_url":%q,"path":%q%s}`,
		protocol, baseURL, requestPath, extra,
	))
}
