package protocolspec

import (
	"reflect"
	"testing"
)

func TestResolveOpenCodeGoModelEndpointTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		want  []string
		ok    bool
	}{
		{model: "gpt-5.6-luna", want: []string{"openai-response"}, ok: true},
		{model: "minimax-m3", want: []string{"anthropic-messages"}, ok: true},
		{model: "qwen3.7-max", want: []string{"anthropic-messages"}, ok: true},
		{model: "grok-4.5", want: []string{"openai"}, ok: true},
		{model: "glm-5.2", want: []string{"openai"}, ok: true},
		{model: "kimi-k3", want: []string{"openai"}, ok: true},
		{model: "deepseek-v4-pro", want: []string{"openai"}, ok: true},
		{model: "mimo-v2.5-pro", want: []string{"openai"}, ok: true},
		{model: "longcat-2.5-preview-free", want: []string{"openai"}, ok: true},
		{model: "mimo-v2.6-flash-free", want: []string{"openai"}, ok: true},
		{model: "minimax-m3.1-flash-free", want: []string{"openai"}, ok: true},
		{model: "nemotron-3.5-lightning-free", want: []string{"openai"}, ok: true},
		{model: "nemotron-3.5-super-vl-free", want: []string{"openai"}, ok: true},
		{model: "jev-1.13-free", want: []string{"typesafe-systemone"}, ok: true},
		{model: "hy3-preview", want: []string{"openai"}, ok: true},
		{model: "unknown-model", want: []string{"openai"}, ok: false},
		{model: "big-pickle", want: []string{"openai"}, ok: false},
		{model: "laguna-s-2.1-free", want: []string{"openai"}, ok: false},
		{model: "ling-3.0-flash-free", want: []string{"openai"}, ok: false},
		{model: "ling-3.0-tiny-free", want: []string{"openai"}, ok: false},
		{model: "longcat-2.0-free", want: []string{"openai"}, ok: false},
		{model: "north-mini-code-free", want: []string{"openai"}, ok: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.model, func(t *testing.T) {
			t.Parallel()
			got, ok, err := ResolveModelEndpointTypes("opencode_go", tt.model)
			if err != nil {
				t.Fatalf("ResolveModelEndpointTypes returned error: %v", err)
			}
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveModelEndpointTypes = %#v, %v, want %#v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestIsOpenCodeZenBaseURL(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		url  string
		want bool
	}{
		{url: "https://opencode.ai/zen", want: true},
		{url: "https://opencode.ai/zen/go", want: true},
		{url: "https://opencode.ai/zen/v1", want: true},
		{url: "https://opencode.ai/v1", want: false},
		{url: "https://example.com/zen", want: false},
	} {
		if got := IsOpenCodeZenBaseURL(tc.url); got != tc.want {
			t.Errorf("IsOpenCodeZenBaseURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestProtocolSpecDataIsDefensivelyCopied(t *testing.T) {
	t.Parallel()

	first := Data()
	second := Data()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("embedded protocol spec data is empty")
	}
	first[0] = 'x'
	if second[0] == 'x' || Data()[0] == 'x' {
		t.Fatal("Data returned shared mutable storage")
	}
}

func TestCurrentOpenCodeGoCatalogModelsHaveSpecMapping(t *testing.T) {
	t.Parallel()

	models := []string{
		"minimax-m3", "minimax-m2.7", "minimax-m2.5",
		"kimi-k3", "kimi-k2.7-code", "kimi-k2.6", "kimi-k2.5",
		"glm-5.2", "glm-5.1", "glm-5",
		"deepseek-v4-pro", "deepseek-v4-flash",
		"qwen3.7-max", "qwen3.8-max", "qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus",
		"mimo-v2-pro", "mimo-v2-omni", "mimo-v2.5-pro", "mimo-v2.5",
		"longcat-2.5-preview-free", "mimo-v2.6-flash-free", "minimax-m3.1-flash-free",
		"nemotron-3.5-lightning-free", "nemotron-3.5-super-vl-free", "jev-1.13-free",
		"hy3", "hy3-preview", "gpt-5.6-luna", "grok-4.5",
	}
	for _, model := range models {
		endpointTypes, ok, err := ResolveModelEndpointTypes("opencode_go", model)
		if err != nil {
			t.Fatalf("resolve %q: %v", model, err)
		}
		if !ok || len(endpointTypes) != 1 {
			t.Fatalf("model %q endpoint types = %#v, matched=%v", model, endpointTypes, ok)
		}
	}
}
