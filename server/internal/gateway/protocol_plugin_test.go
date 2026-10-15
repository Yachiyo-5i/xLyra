package gateway

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
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

const signedPluginSource = `export const meta = { apiVersion: 1, id: "acme.signed", kind: "protocol" };
export function decodeRequest(ctx, payload) { return { model: payload.model }; }
export function buildRequest(ctx, payload) { return { path: "/v1/run", payload: { model: payload.model } }; }
export function parseResponse(ctx, resp) { return { passthrough: true }; }
export function signRequest(ctx, req) {
  return { stringToSign: req.method + " " + req.url + " " + req.body, algorithm: "hmac-sha256", header: "X-Signature", headers: { "X-Stamp": "42" } };
}
export function parseError(ctx, resp) {
  return { body: "upstream said: " + resp.json.msg };
}
`

func signedTestAdapter(t *testing.T) *jsProtocolAdapter {
	t.Helper()
	manifest := jsplugin.Manifest{
		ID: "acme.signed", Name: "signed", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindProtocol,
		Protocol: jsplugin.ProtocolSection{
			Name: "signed", DownstreamPath: "/v1/plugins/signed/run", EndpointType: "plugin:acme.signed",
			Auth: "none", DefaultBaseURL: "https://api.example.com",
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(signedPluginSource)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, signedPluginSource, nil, 1)
	if err != nil {
		t.Fatalf("NewPlugin: %v", err)
	}
	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "custom", BaseURL: "https://api.example.com"}}
	return newJSProtocolAdapter(t.Context(), plugin, gatewayRequest{Payload: map[string]any{"model": "m"}}, candidate)
}

func TestJSProtocolAdapterSignsWithTheCredential(t *testing.T) {
	t.Parallel()
	adapter := signedTestAdapter(t)
	body := []byte(`{"model":"m"}`)
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/v1/run", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	const key = "sk-secret-credential"
	if err := signUpstreamRequest(req, adapter, body, key); err != nil {
		t.Fatalf("sign: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(`POST https://api.example.com/v1/run {"model":"m"}`))
	if got, want := req.Header.Get("X-Signature"), hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Fatalf("X-Signature = %q, want %q", got, want)
	}
	if req.Header.Get("X-Stamp") != "42" {
		t.Fatalf("X-Stamp = %q", req.Header.Get("X-Stamp"))
	}
	for name, values := range req.Header {
		for _, value := range values {
			if strings.Contains(value, key) {
				t.Fatalf("credential leaked into header %s", name)
			}
		}
	}
}

func TestJSProtocolAdapterSignRefusesLargeBody(t *testing.T) {
	t.Parallel()
	adapter := signedTestAdapter(t)
	req, _ := http.NewRequest(http.MethodPost, "https://api.example.com/v1/run", nil)
	err := signUpstreamRequest(req, adapter, make([]byte, jsplugin.MaxSignBodyBytes+1), "k")
	if err == nil {
		t.Fatal("large body was signed")
	}
}

func TestJSProtocolAdapterRewritesUpstreamError(t *testing.T) {
	t.Parallel()
	adapter := signedTestAdapter(t)
	resp, err := adapter.TransformBufferedResponse(http.StatusTooManyRequests, http.Header{"Content-Type": {"application/json"}}, []byte(`{"msg":"slow down"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, the upstream status must be kept", resp.StatusCode)
	}
	if string(resp.Body) != "upstream said: slow down" {
		t.Fatalf("body = %q", resp.Body)
	}
	// A hook that throws leaves the upstream response alone.
	broken, err := adapter.TransformBufferedResponse(http.StatusBadGateway, http.Header{}, []byte(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	if string(broken.Body) != "not json" || broken.StatusCode != http.StatusBadGateway {
		t.Fatalf("fallback = %#v", broken)
	}
}

func TestPlainProtocolAdapterDoesNotSign(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodPost, "https://x.example/v1", nil)
	if err := signUpstreamRequest(req, openAIChatProtocolAdapter{}, nil, "k"); err != nil {
		t.Fatal(err)
	}
	if len(req.Header) != 0 {
		t.Fatalf("headers = %v", req.Header)
	}
}
