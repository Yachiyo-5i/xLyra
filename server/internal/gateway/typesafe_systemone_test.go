package gateway

import (
	"net/http"
	"testing"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
)

func systemOneEndpoint(t *testing.T) gatewayEndpointAdapter {
	adapter := (Handler{}).typeSafeSystemOneEndpoint()
	if _, ok := adapter.(unavailableEndpointAdapter); ok {
		t.Fatal("typesafe_systemone builtin is not loaded")
	}
	return adapter
}

func systemOneProtocol(t *testing.T, request gatewayRequest, candidate routeengine.Candidate) gatewayProtocolAdapter {
	protocol, err := builtinSystemOneProtocol(t.Context(), request, candidate)
	if err != nil {
		t.Fatalf("builtinSystemOneProtocol: %v", err)
	}
	return protocol
}

func TestTypeSafeSystemOneEndpointAdapterDecodesRequest(t *testing.T) {
	t.Parallel()

	request := requireDecodedEndpointRequest(t, systemOneEndpoint(t), `{"model":" jev-latest ","state":"hi","questions":{"q":{"type":"noul","instructions":"urgent?"}}}`, gatewayEndpointTypeSafeSystemOne, "jev-latest")
	if request.Stream {
		t.Fatal("expected stream=false")
	}
	if _, ok := request.Payload["questions"].(map[string]any); !ok {
		t.Fatalf("payload questions = %#v, want object", request.Payload["questions"])
	}
}

func TestTypeSafeSystemOneEndpointAdapterRejectsInvalidJSONAndMissingModel(t *testing.T) {
	t.Parallel()

	adapter := systemOneEndpoint(t)
	assertEndpointDecodeFailure(t, "invalid JSON", adapter, `{`, "invalid_json", "decode")
	assertEndpointDecodeFailure(t, "missing model", adapter, `{"state":"hi"}`, "invalid_model", "validate")
}

func TestTypeSafeSystemOneResolverSelectsJSAdapter(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{DownstreamPath: gatewayEndpointTypeSafeSystemOne, RequestedModel: "jev-latest", Payload: map[string]any{"model": "jev-latest"}}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "typesafe", BaseURL: "https://api.typesafe.ai/"},
		Model: routeengine.CandidateModel{UpstreamName: "jev-preview", SupportedEndpointTypes: []string{"typesafe-systemone"}},
	}
	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), request, candidate)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if _, ok := protocol.(*jsProtocolAdapter); !ok {
		t.Fatalf("protocol = %T, want *jsProtocolAdapter", protocol)
	}
	if !credentialSupportsAdapter([]string{"typesafe-systemone"}, protocol) {
		t.Fatal("systemone credential must support the passthrough adapter")
	}
	if credentialSupportsAdapter([]string{"openai"}, protocol) {
		t.Fatal("chat credential must not support the passthrough adapter")
	}

	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "jev-preview" || request.Payload["model"] != "jev-latest" {
		t.Fatalf("upstream model = %#v, downstream model = %#v", payload["model"], request.Payload["model"])
	}
}

func TestTypeSafeSystemOneResolverRejectsChatModels(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{DownstreamPath: gatewayEndpointTypeSafeSystemOne, RequestedModel: "gpt-5.5"}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.5", SupportedEndpointTypes: []string{"openai", "openai-response"}},
	}
	if _, err := (openAIProtocolResolver{}).Resolve(t.Context(), request, candidate); err == nil {
		t.Fatal("expected chat model to be rejected for systemone")
	}
	chatRequest := gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}
	if endpointTypesAllowRequest(chatRequest, []string{"typesafe-systemone"}) {
		t.Fatal("systemone model must not serve chat completions")
	}
}

func TestTypeSafeSystemOneUpstreamPath(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointTypeSafeSystemOne,
		RequestedModel: "jev-latest",
		Payload:        map[string]any{"model": "jev-latest", "state": "hi"},
	}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "typesafe", BaseURL: "https://api.typesafe.ai/"},
		Model: routeengine.CandidateModel{UpstreamName: "jev-preview"},
	}
	protocol := systemOneProtocol(t, request, candidate)
	if got := protocol.UpstreamPath("https://api.typesafe.ai/"); got != "https://api.typesafe.ai/v1/systemone" {
		t.Fatalf("UpstreamPath = %q", got)
	}
	if got := protocol.UpstreamPath(""); got != "https://api.typesafe.ai/v1/systemone" {
		t.Fatalf("UpstreamPath(empty) = %q", got)
	}
}

func TestTypeSafeSystemOneTransformBufferedResponseParsesUsage(t *testing.T) {
	t.Parallel()

	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatalf("load builtins: %v", err)
	}
	plugin, ok := registry.ByProtocolName("typesafe_systemone")
	if !ok {
		t.Fatal("missing typesafe_systemone builtin")
	}
	request := gatewayRequest{
		DownstreamPath: gatewayEndpointTypeSafeSystemOne,
		RequestedModel: "jev-latest",
		Payload:        map[string]any{"model": "jev-latest", "state": "hi"},
	}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "typesafe", BaseURL: "https://api.typesafe.ai/"},
		Model: routeengine.CandidateModel{UpstreamName: "jev-preview"},
	}
	adapter := newJSProtocolAdapter(t.Context(), plugin, request, candidate)
	_, err = adapter.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload: %v", err)
	}

	body := []byte(`{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":0.92}},"usage":{"input_tokens":394,"output_tokens":68}}`)
	response, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if string(response.Body) != string(body) {
		t.Fatalf("body = %s, want passthrough", response.Body)
	}
	if response.ContentType != "application/json" {
		t.Fatalf("content type = %q", response.ContentType)
	}
	if response.Usage.PromptTokens != 394 || response.Usage.CompletionTokens != 68 || response.Usage.TotalTokens != 462 {
		t.Fatalf("usage = %#v", response.Usage)
	}

	errorBody := []byte(`{"detail":"validation failed"}`)
	failed, err := adapter.TransformBufferedResponse(http.StatusUnprocessableEntity, http.Header{"Content-Type": {"application/json"}}, errorBody)
	if err != nil {
		t.Fatalf("TransformBufferedResponse(422) returned error: %v", err)
	}
	if failed.StatusCode != http.StatusUnprocessableEntity || failed.Usage.TotalTokens != 0 {
		t.Fatalf("failed response = %#v", failed)
	}
}
