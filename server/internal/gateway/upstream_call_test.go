package gateway

import (
	"net/http"
	"testing"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
)

func TestJSProtocolAdapterUpstreamCall(t *testing.T) {
	plugin := &jsplugin.Plugin{
		Manifest: jsplugin.Manifest{
			Protocol: jsplugin.ProtocolSection{
				Method: "GET",
				Auth:   "header:X-Api-Key",
			},
		},
	}
	adapter := newJSProtocolAdapter(t.Context(), plugin, gatewayRequest{}, routeengine.Candidate{})
	req, err := newUpstreamHTTPRequest(t.Context(), adapter, "https://example.com/v1/test", []byte(`{}`))
	if err != nil {
		t.Fatalf("newUpstreamHTTPRequest: %v", err)
	}
	if req.Method != http.MethodGet {
		t.Fatalf("method = %q, want GET", req.Method)
	}
	applyUpstreamAuth(req, adapter, "secret")
	if req.Header.Get("X-Api-Key") != "secret" {
		t.Fatalf("auth header = %q", req.Header.Get("X-Api-Key"))
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("unexpected bearer auth")
	}
}
