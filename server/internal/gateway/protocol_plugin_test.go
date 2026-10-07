package gateway

import (
	"net/http"
	"testing"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
)

func TestJSProtocolAdapterSystemOne(t *testing.T) {
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
		Model: routeengine.CandidateModel{UpstreamName: "jev-preview", SupportedEndpointTypes: []string{"typesafe-systemone"}},
	}

	adapter := newJSProtocolAdapter(t.Context(), plugin, request, candidate)

	payload, err := adapter.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload: %v", err)
	}
	if payload["model"] != "jev-preview" {
		t.Fatalf("model = %#v, want jev-preview", payload["model"])
	}
	if got := adapter.UpstreamPath(candidate.Site.BaseURL); got != "https://api.typesafe.ai/v1/systemone" {
		t.Fatalf("UpstreamPath = %q", got)
	}

	body := []byte(`{"usage":{"input_tokens":394,"output_tokens":68}}`)
	resp, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse: %v", err)
	}
	if resp.Usage.PromptTokens != 394 || resp.Usage.CompletionTokens != 68 || resp.Usage.TotalTokens != 462 {
		t.Fatalf("usage = %#v", resp.Usage)
	}
}
