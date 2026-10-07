package gateway

import (
	"bytes"
	"context"
	"net/http"
	"strings"
)

// gatewayUpstreamCallAdapter overrides method and auth for an upstream request.
type gatewayUpstreamCallAdapter interface {
	UpstreamMethod() string
	ApplyUpstreamAuth(req *http.Request, upstreamKey string)
}

func upstreamHTTPMethod(protocol gatewayProtocolAdapter) string {
	if call, ok := protocol.(gatewayUpstreamCallAdapter); ok {
		if method := strings.TrimSpace(call.UpstreamMethod()); method != "" {
			return method
		}
	}
	return http.MethodPost
}

func newUpstreamHTTPRequest(ctx context.Context, protocol gatewayProtocolAdapter, endpoint string, body []byte) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, upstreamHTTPMethod(protocol), endpoint, bytes.NewReader(body))
}

func setUpstreamAccept(req *http.Request, upstreamStream bool) {
	if upstreamStream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

func applyUpstreamAuth(req *http.Request, protocol gatewayProtocolAdapter, upstreamKey string) {
	if call, ok := protocol.(gatewayUpstreamCallAdapter); ok {
		call.ApplyUpstreamAuth(req, upstreamKey)
		return
	}
	req.Header.Set("Authorization", "Bearer "+upstreamKey)
}
