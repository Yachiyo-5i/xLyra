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

// gatewayUpstreamSigner signs the finished upstream request. It runs after every
// header is in place, so the signature covers what is actually sent. The key is
// only handed to Go code.
type gatewayUpstreamSigner interface {
	SignUpstreamRequest(req *http.Request, body []byte, upstreamKey string) error
}

// signUpstreamRequest is a no-op for protocols that do not sign.
func signUpstreamRequest(req *http.Request, protocol gatewayProtocolAdapter, body []byte, upstreamKey string) error {
	if signer, ok := protocol.(gatewayUpstreamSigner); ok {
		return signer.SignUpstreamRequest(req, body, upstreamKey)
	}
	return nil
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
