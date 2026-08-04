package gateway

import (
	"testing"

	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
)

func codexImageResponsesRequest(payload map[string]any) gatewayRequest {
	return codexImageResponsesRequestWithModel("gpt-5.4", payload)
}

func codexImageResponsesRequestWithModel(model string, payload map[string]any) gatewayRequest {
	return gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		RequestedModel: model,
		Payload:        payload,
	}
}

func codexImageCompatibilityCandidate(disabledTools ...string) routeengine.Candidate {
	return routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ResponsesToolPolicy:    sitepkg.ResponsesToolPolicyCompatibility,
			DisabledResponsesTools: disabledTools,
		},
	}
}

func codexImageGenerationTool() map[string]any {
	return map[string]any{"type": "image_generation"}
}

func TestRequestForCodexImagePolicyStripsAdvertisedDisabledTool(t *testing.T) {
	t.Parallel()

	request := codexImageResponsesRequest(map[string]any{
		"model": "gpt-5.4",
		"input": "hello",
		"tools": []any{
			map[string]any{
				"type": "function",
				"name": "lookup",
			},
			codexImageGenerationTool(),
		},
	})
	candidate := codexImageCompatibilityCandidate(sitepkg.ResponsesHostedToolImageGeneration)

	rewritten, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentToolAdvertised)
	if !ok {
		t.Fatal("expected advertised tool request to remain routable")
	}
	if codexPayloadHasImageGenerationTool(rewritten.Payload) {
		t.Fatalf("expected image_generation tool to be stripped, got %#v", rewritten.Payload["tools"])
	}
	tools, _ := rewritten.Payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected one remaining tool, got %#v", rewritten.Payload["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if anyString(tool["type"]) != "function" {
		t.Fatalf("expected function tool to remain, got %#v", tool)
	}
}

func TestRequestForCodexImagePolicyRejectsExplicitDisabledTool(t *testing.T) {
	t.Parallel()

	request := codexImageResponsesRequest(map[string]any{
		"model":       "gpt-5.4",
		"input":       "draw a logo",
		"tool_choice": codexImageGenerationTool(),
	})
	candidate := codexImageCompatibilityCandidate(sitepkg.ResponsesHostedToolImageGeneration)

	_, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentForRequest(request))
	if ok {
		t.Fatal("expected explicit image generation request to skip disabled candidate")
	}
}

func TestRequestForCodexImagePolicyAllowsImageModelWhenToolStripped(t *testing.T) {
	t.Parallel()

	// A dedicated gpt-image-* model is served via the upstream image pipeline, so the
	// "strip image tool" config (which only removes the Responses hosted tool) must not
	// disqualify the site — otherwise a real gpt-image model would be treated as unroutable.
	request := codexImageResponsesRequestWithModel("gpt-image-2", map[string]any{"input": "draw a logo"})
	candidate := codexImageCompatibilityCandidate(sitepkg.ResponsesHostedToolImageGeneration)

	_, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentForRequest(request))
	if !ok {
		t.Fatal("expected dedicated gpt-image model request to route even when the hosted image tool is stripped")
	}
}

func TestRequestForCodexImagePolicyAllowsImagesEndpointWhenToolStripped(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		Payload:        map[string]any{"prompt": "draw a cat", "model": "gpt-image-2"},
	}
	candidate := codexImageCompatibilityCandidate(sitepkg.ResponsesHostedToolImageGeneration)

	_, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentForRequest(request))
	if !ok {
		t.Fatal("expected images generations endpoint to route even when the hosted image tool is stripped")
	}
}

func TestCodexImageIntentDetectsImagegenDirective(t *testing.T) {
	t.Parallel()

	request := codexImageResponsesRequest(map[string]any{
		"model": "gpt-5.4",
		"input": []any{
			map[string]any{"content": "please use $imagegen"},
		},
	})

	if got := codexImageIntentForRequest(request); got != codexImageIntentExplicit {
		t.Fatalf("codexImageIntentForRequest = %v, want %v", got, codexImageIntentExplicit)
	}
}

func TestCodexImageIntentDetectsEndpointModelAndAdvertisedTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request gatewayRequest
		want    codexImageGenerationIntent
	}{
		{
			name: "images endpoint",
			request: gatewayRequest{
				DownstreamPath: gatewayEndpointImagesGenerations,
				Payload:        map[string]any{"prompt": "draw"},
			},
			want: codexImageIntentModelEndpoint,
		},
		{
			name:    "image model",
			request: codexImageResponsesRequestWithModel("gpt-image-2", map[string]any{"input": "draw"}),
			want:    codexImageIntentModelEndpoint,
		},
		{
			name: "advertised image tool",
			request: codexImageResponsesRequest(map[string]any{
				"input": "hello",
				"tools": []any{codexImageGenerationTool()},
			}),
			want: codexImageIntentToolAdvertised,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := codexImageIntentForRequest(tt.request); got != tt.want {
				t.Fatalf("codexImageIntentForRequest = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestForCodexImagePolicyAllowsWhenToolIsNotDisabled(t *testing.T) {
	t.Parallel()

	request := codexImageResponsesRequest(map[string]any{
		"input": "hello",
		"tools": []any{codexImageGenerationTool()},
	})
	candidate := codexImageCompatibilityCandidate()

	rewritten, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentToolAdvertised)
	if !ok {
		t.Fatal("expected request to remain routable when image tool is allowed")
	}
	if !codexPayloadHasImageGenerationTool(rewritten.Payload) {
		t.Fatalf("expected image_generation tool to remain, got %#v", rewritten.Payload)
	}
}

func TestStripImageGenerationToolRemovesOnlyToolAndResetsToolChoice(t *testing.T) {
	t.Parallel()

	request := codexImageResponsesRequest(map[string]any{
		"model":       "gpt-5.4",
		"input":       "hello",
		"tools":       []any{codexImageGenerationTool()},
		"tool_choice": codexImageGenerationTool(),
	})

	rewritten := stripImageGenerationToolFromRequest(request)
	if _, ok := rewritten.Payload["tools"]; ok {
		t.Fatalf("tools should be removed when only image_generation was advertised, got %#v", rewritten.Payload["tools"])
	}
	if got := rewritten.Payload["tool_choice"]; got != "auto" {
		t.Fatalf("tool_choice = %#v, want auto", got)
	}
	if _, ok := request.Payload["tools"]; !ok {
		t.Fatal("original request payload should not be mutated")
	}
}

func TestCodexImageIntentDetectsEditsEndpoint(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointImagesEdits,
		Payload:        map[string]any{"prompt": "edit this"},
	}
	if got := codexImageIntentForRequest(request); got != codexImageIntentModelEndpoint {
		t.Fatalf("codexImageIntentForRequest = %v, want %v", got, codexImageIntentModelEndpoint)
	}
}

func TestRequestForCodexImagePolicyRejectsCodexEditsEndpoint(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointImagesEdits,
		Payload:        map[string]any{"prompt": "edit this", "model": "gpt-image-2"},
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: "codex"},
	}
	_, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentExplicit)
	if ok {
		t.Fatal("expected Codex edits endpoint to be rejected")
	}
}

func TestRequestForCodexImagePolicyAllowsCodexGenerationsEndpoint(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		Payload:        map[string]any{"prompt": "draw a cat", "model": "gpt-image-2"},
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{SiteType: "codex"},
	}
	_, ok := requestForCodexImagePolicy(request, candidate, codexImageIntentExplicit)
	if !ok {
		t.Fatal("expected Codex generations endpoint to be allowed")
	}
}
