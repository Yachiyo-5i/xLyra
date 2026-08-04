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
		{model: "hy3-preview", want: []string{"openai"}, ok: true},
		{model: "unknown-model", ok: false},
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
