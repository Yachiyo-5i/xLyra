package site

import "testing"

func TestNormalizeGatewayConfigRejectsNonPositiveValues(t *testing.T) {
	t.Parallel()

	value := 0
	_, err := NormalizeGatewayConfig(&GatewayConfig{
		RequestTimeoutMS: &value,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	cfg, err := NormalizeGatewayConfig(nil)
	if err != nil {
		t.Fatalf("NormalizeGatewayConfig(nil) error = %v", err)
	}
	if cfg != nil {
		t.Fatalf("NormalizeGatewayConfig(nil) = %#v, want nil", cfg)
	}

	_, err = MergeSiteGatewayConfig([]byte(`{"gateway":{"request_timeout_ms":1000}}`), &GatewayConfig{
		ConnectTimeoutMS: &value,
	})
	if err == nil {
		t.Fatal("expected merge validation error")
	}
}

func TestMergeSiteGatewayConfigPreservesExistingFields(t *testing.T) {
	t.Parallel()

	timeout := 45000
	maxConcurrency := 8
	maxModelConcurrency := 4
	maxCredentialConcurrency := 2
	raw, err := MergeSiteGatewayConfig([]byte(`{"notes":"keep"}`), &GatewayConfig{
		RequestTimeoutMS:         &timeout,
		MaxConcurrency:           &maxConcurrency,
		MaxModelConcurrency:      &maxModelConcurrency,
		MaxCredentialConcurrency: &maxCredentialConcurrency,
	})
	if err != nil {
		t.Fatalf("merge gateway config: %v", err)
	}

	cfg := GatewayConfigFromSiteMeta(raw)
	if cfg == nil || cfg.RequestTimeoutMS == nil || *cfg.RequestTimeoutMS != timeout {
		t.Fatalf("expected request_timeout_ms %d, got %#v", timeout, cfg)
	}
	if cfg == nil || cfg.MaxConcurrency == nil || *cfg.MaxConcurrency != maxConcurrency {
		t.Fatalf("expected max_concurrency %d, got %#v", maxConcurrency, cfg)
	}
	if cfg == nil || cfg.MaxModelConcurrency == nil || *cfg.MaxModelConcurrency != maxModelConcurrency {
		t.Fatalf("expected max_model_concurrency %d, got %#v", maxModelConcurrency, cfg)
	}
	if cfg == nil || cfg.MaxCredentialConcurrency == nil || *cfg.MaxCredentialConcurrency != maxCredentialConcurrency {
		t.Fatalf("expected max_credential_concurrency %d, got %#v", maxCredentialConcurrency, cfg)
	}
	if string(raw) == "{}" {
		t.Fatalf("expected merged meta, got %s", raw)
	}
}

func TestNormalizeGatewayConfigMapsLegacyResponsesImagePolicy(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeGatewayConfig(&GatewayConfig{
		ResponsesImageGenerationPolicy: "strip_auto_tool",
	})
	if err != nil {
		t.Fatalf("normalize gateway config: %v", err)
	}
	if cfg.ResponsesToolPolicy != ResponsesToolPolicyCompatibility {
		t.Fatalf("ResponsesToolPolicy = %q, want %q", cfg.ResponsesToolPolicy, ResponsesToolPolicyCompatibility)
	}
	if !ResponsesToolDisabled(cfg, ResponsesHostedToolImageGeneration) {
		t.Fatalf("expected %s to be disabled", ResponsesHostedToolImageGeneration)
	}
	if cfg.ResponsesImageGenerationPolicy != "" {
		t.Fatalf("expected legacy policy to be cleared, got %q", cfg.ResponsesImageGenerationPolicy)
	}
}

func TestMergeSiteGatewayConfigClearsDisabledResponsesTools(t *testing.T) {
	t.Parallel()

	raw, err := MergeSiteGatewayConfig([]byte(`{"gateway":{"responses_tool_policy":"compatibility","disabled_responses_tools":["image_generation"]}}`), &GatewayConfig{
		ResponsesToolPolicy:    ResponsesToolPolicyPassthrough,
		DisabledResponsesTools: []string{},
	})
	if err != nil {
		t.Fatalf("merge gateway config: %v", err)
	}

	cfg := GatewayConfigFromSiteMeta(raw)
	if cfg == nil {
		t.Fatal("expected gateway config")
	}
	if ResponsesToolDisabled(cfg, ResponsesHostedToolImageGeneration) {
		t.Fatalf("expected %s to be enabled after clearing disabled tools", ResponsesHostedToolImageGeneration)
	}
}

func TestResponsesToolDisabledRequiresCompatibilityAndKnownTool(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		cfg  *GatewayConfig
		tool string
		want bool
	}{
		{
			name: "nil config",
			cfg:  nil,
			tool: ResponsesHostedToolImageGeneration,
			want: false,
		},
		{
			name: "blank tool",
			cfg: &GatewayConfig{
				ResponsesToolPolicy: ResponsesToolPolicyCompatibility,
			},
			tool: " ",
			want: false,
		},
		{
			name: "compatibility disables configured tool",
			cfg: &GatewayConfig{
				ResponsesToolPolicy:    ResponsesToolPolicyCompatibility,
				DisabledResponsesTools: []string{ResponsesHostedToolImageGeneration},
			},
			tool: ResponsesHostedToolImageGeneration,
			want: true,
		},
		{
			name: "passthrough ignores disabled list",
			cfg: &GatewayConfig{
				ResponsesToolPolicy:    ResponsesToolPolicyPassthrough,
				DisabledResponsesTools: []string{ResponsesHostedToolImageGeneration},
			},
			tool: ResponsesHostedToolImageGeneration,
			want: false,
		},
		{
			name: "unknown policy normalizes to passthrough",
			cfg: &GatewayConfig{
				ResponsesToolPolicy:    "future-policy",
				DisabledResponsesTools: []string{ResponsesHostedToolImageGeneration},
			},
			tool: ResponsesHostedToolImageGeneration,
			want: false,
		},
		{
			name: "unknown tool is never disabled",
			cfg: &GatewayConfig{
				ResponsesToolPolicy:    ResponsesToolPolicyCompatibility,
				DisabledResponsesTools: []string{ResponsesHostedToolImageGeneration, "future_tool"},
			},
			tool: "future_tool",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ResponsesToolDisabled(tc.cfg, tc.tool); got != tc.want {
				t.Fatalf("ResponsesToolDisabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeSiteGatewayConfigUpdatesClientImpersonation(t *testing.T) {
	t.Parallel()

	raw, err := MergeSiteGatewayConfig([]byte(`{"gateway":{"impersonate_codex_client":true,"impersonate_claude_code_client":true}}`), &GatewayConfig{
		ImpersonateCodexClient:      testBool(false),
		ImpersonateClaudeCodeClient: testBool(true),
	})
	if err != nil {
		t.Fatalf("merge gateway config: %v", err)
	}

	cfg := GatewayConfigFromSiteMeta(raw)
	if cfg == nil {
		t.Fatal("expected gateway config")
	}
	if cfg.ImpersonateCodexClient == nil || *cfg.ImpersonateCodexClient {
		t.Fatalf("ImpersonateCodexClient = %#v, want false", cfg.ImpersonateCodexClient)
	}
	if cfg.ImpersonateClaudeCodeClient == nil || !*cfg.ImpersonateClaudeCodeClient {
		t.Fatalf("ImpersonateClaudeCodeClient = %#v, want true", cfg.ImpersonateClaudeCodeClient)
	}
}

func TestMergeSiteGatewayConfigUpdatesConnectionPoolFields(t *testing.T) {
	t.Parallel()

	requestTimeout := 1000
	connectTimeout := 2000
	responseHeaderTimeout := 3000
	maxIdle := 10
	maxIdlePerHost := 5
	maxConnsPerHost := 3
	idleConnTimeout := 4000

	raw, err := MergeSiteGatewayConfig([]byte(`{"gateway":{"request_timeout_ms":9000,"responses_tool_policy":"compatibility","disabled_responses_tools":["image_generation"]}}`), &GatewayConfig{
		RequestTimeoutMS:        &requestTimeout,
		ConnectTimeoutMS:        &connectTimeout,
		ResponseHeaderTimeoutMS: &responseHeaderTimeout,
		MaxIdleConns:            &maxIdle,
		MaxIdleConnsPerHost:     &maxIdlePerHost,
		MaxConnsPerHost:         &maxConnsPerHost,
		IdleConnTimeoutMS:       &idleConnTimeout,
		ResponsesToolPolicy:     "passthrough",
	})
	if err != nil {
		t.Fatalf("merge gateway config: %v", err)
	}

	cfg := GatewayConfigFromSiteMeta(raw)
	if cfg == nil {
		t.Fatal("expected gateway config")
	}
	if cfg.RequestTimeoutMS == nil || *cfg.RequestTimeoutMS != requestTimeout {
		t.Fatalf("request timeout = %#v, want %d", cfg.RequestTimeoutMS, requestTimeout)
	}
	if cfg.ConnectTimeoutMS == nil || *cfg.ConnectTimeoutMS != connectTimeout {
		t.Fatalf("connect timeout = %#v, want %d", cfg.ConnectTimeoutMS, connectTimeout)
	}
	if cfg.ResponseHeaderTimeoutMS == nil || *cfg.ResponseHeaderTimeoutMS != responseHeaderTimeout {
		t.Fatalf("response header timeout = %#v, want %d", cfg.ResponseHeaderTimeoutMS, responseHeaderTimeout)
	}
	if cfg.MaxIdleConns == nil || *cfg.MaxIdleConns != maxIdle {
		t.Fatalf("max idle conns = %#v, want %d", cfg.MaxIdleConns, maxIdle)
	}
	if cfg.MaxIdleConnsPerHost == nil || *cfg.MaxIdleConnsPerHost != maxIdlePerHost {
		t.Fatalf("max idle conns per host = %#v, want %d", cfg.MaxIdleConnsPerHost, maxIdlePerHost)
	}
	if cfg.MaxConnsPerHost == nil || *cfg.MaxConnsPerHost != maxConnsPerHost {
		t.Fatalf("max conns per host = %#v, want %d", cfg.MaxConnsPerHost, maxConnsPerHost)
	}
	if cfg.IdleConnTimeoutMS == nil || *cfg.IdleConnTimeoutMS != idleConnTimeout {
		t.Fatalf("idle conn timeout = %#v, want %d", cfg.IdleConnTimeoutMS, idleConnTimeout)
	}
	if cfg.ResponsesToolPolicy != ResponsesToolPolicyPassthrough {
		t.Fatalf("responses tool policy = %q, want passthrough", cfg.ResponsesToolPolicy)
	}
	if len(cfg.DisabledResponsesTools) != 1 || cfg.DisabledResponsesTools[0] != ResponsesHostedToolImageGeneration {
		t.Fatalf("disabled tools should be preserved without an explicit patch, got %#v", cfg.DisabledResponsesTools)
	}
}

func TestRequestHeadersFromSiteMetaParsesHeaderEntries(t *testing.T) {
	t.Parallel()

	headers := RequestHeadersFromSiteMeta([]byte(`{
		"request_headers":[
			{"key":"X-Trace","value":"enabled"},
			{"key":"","value":"ignored"},
			{"key":"X-Mode","value":""}
		]
	}`))

	if len(headers) != 2 {
		t.Fatalf("expected two headers, got %#v", headers)
	}
	if headers["X-Trace"] != "enabled" {
		t.Fatalf("X-Trace = %q, want enabled", headers["X-Trace"])
	}
	if value, ok := headers["X-Mode"]; !ok || value != "" {
		t.Fatalf("X-Mode = %q, %v; want empty value preserved", value, ok)
	}

	headers = RequestHeadersFromSiteMeta([]byte(`{
		"request_headers":[
			{"key":"X-Trace","value":"first"},
			{"key":"","value":"ignored"},
			{"key":"X-Trace","value":"second"}
		]
	}`))
	if len(headers) != 1 {
		t.Fatalf("duplicate headers = %#v, want one collapsed header", headers)
	}
	if headers["X-Trace"] != "second" {
		t.Fatalf("duplicate X-Trace = %q, want second", headers["X-Trace"])
	}
}

func TestRequestHeadersFromSiteMetaReturnsNilForInvalidOrEmptyMeta(t *testing.T) {
	t.Parallel()

	for _, raw := range [][]byte{
		nil,
		[]byte(`not json`),
		[]byte(`{"request_headers":null}`),
		[]byte(`{"request_headers":[{"key":"","value":"ignored"}]}`),
		[]byte(`{"request_headers":{"key":"X-Trace"}}`),
	} {
		if got := RequestHeadersFromSiteMeta(raw); got != nil {
			t.Fatalf("RequestHeadersFromSiteMeta(%s) = %#v, want nil", raw, got)
		}
	}
}

func testBool(value bool) *bool {
	return &value
}
