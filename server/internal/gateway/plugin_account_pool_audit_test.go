package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/jsplugin"
)

func TestPluginAccountPoolAllCooledHidesCredentials(t *testing.T) {
	transport := &pluginAccountTransport{}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true, selection: "quota_weighted"})
	const tokenA = "ACCESS-cooled-aaaa"
	const tokenB = "ACCESS-cooled-bbbb"
	handler, candidate, _ := pluginAccountForwardHandler(t, client, nil,
		newPluginForwardAccount(tokenA, "intl", 1000, pluginUsage(exhaustedEntry(time.Now().Add(time.Hour)))),
		newPluginForwardAccount(tokenB, "cn", 50, pluginUsage(exhaustedEntry(time.Now().Add(2*time.Hour)))),
	)
	result := forwardPluginAccount(handler, candidate, false)
	if result.success || result.errorType != "upstream_credential_unavailable" || len(transport.authorizations()) != 0 {
		t.Fatalf("success=%v type=%s auth=%v err=%s", result.success, result.errorType, transport.authorizations(), result.errorMessage)
	}
	if strings.Contains(result.errorMessage, tokenA) || strings.Contains(result.errorMessage, tokenB) {
		t.Fatalf("error leaked a credential: %s", result.errorMessage)
	}
}

func TestPluginUpstreamStreamOnlyPassesStreamingChatThrough(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		body, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"model\":\"plugin-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")
	}))
	defer server.Close()

	ClearRuntimeProviderSpecs()
	t.Cleanup(ClearRuntimeProviderSpecs)
	registerPluginAccountSite(t, pluginAccountSiteOptions{headers: true, streamOnly: true, regions: []jsplugin.SiteProviderRegion{{
		ID: "intl", BaseURL: server.URL,
	}}})
	if err := SetRuntimeProviderSpecs(map[string]json.RawMessage{
		pluginAccountSiteType: runtimeSpecJSON("openai_chat", "https://api.example.com", "/v1/chat/completions", ""),
	}); err != nil {
		t.Fatal(err)
	}
	account := newPluginForwardAccount("token-stream", "intl", 10, nil)
	handler, candidate, _ := pluginAccountForwardHandler(t, &http.Client{Timeout: 5 * time.Second}, nil, account)
	handler.clients = httpclient.NewManager(nil)
	candidate.Site.BaseURL = "http://127.0.0.1/dead"
	request := gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Stream:         true,
		Payload:        map[string]any{"model": "plugin-model", "messages": []any{}},
	}
	protocol := newOpenAIChatProtocolAdapter(request, candidate)
	rec := httptest.NewRecorder()
	result := handler.forwardGatewayRequest(context.Background(), rec, "req-plugin-sse-stream", 1, uuid.New(), uuid.New(), candidate, request, nil, protocol)
	written := rec.Body.String()
	if !result.success || !strings.Contains(written, "data:") || !strings.Contains(written, `"content":"ok"`) || strings.Contains(written, `"object":"chat.completion"`) {
		t.Fatalf("stream success=%v type=%s written=%s result=%s err=%s", result.success, result.errorType, written, result.body, result.errorMessage)
	}
	if !strings.Contains(string(body), `"stream":true`) || !strings.Contains(result.upstreamURL, server.URL) {
		t.Fatalf("upstream body=%s url=%s", body, result.upstreamURL)
	}
}
