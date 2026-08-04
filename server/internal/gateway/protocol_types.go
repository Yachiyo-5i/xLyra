package gateway

import (
	"context"
	"net/http"
	"time"

	routeengine "xlyra/server/internal/router"
)

type gatewayRequest struct {
	DownstreamPath    string
	DownstreamHeaders http.Header
	RequestedModel    string
	Stream            bool
	Diagnostic        bool
	Payload           map[string]any
	ContentType       string
	Canonical         *canonicalRequest
}

type gatewayEndpointAdapter interface {
	DownstreamPath() string
	RouteEndpointType() string
	DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure)
}

type gatewayProtocolAdapter interface {
	ProtocolName() string
	BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error)
	UpstreamPath(baseURL string) string
	TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error)
	ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error)
}

type gatewayProtocolBodyAdapter interface {
	BuildUpstreamBody(request gatewayRequest, payload map[string]any) ([]byte, string, error)
}

type gatewayProtocolHeaderAdapter interface {
	ApplyUpstreamHeaders(req *http.Request, upstreamKey string, accountID string, stream bool)
}

type upstreamProtocolResolver interface {
	Resolve(ctx context.Context, request gatewayRequest, candidate routeengine.Candidate) (gatewayProtocolAdapter, error)
}

type gatewayBufferedResponse struct {
	StatusCode     int
	ContentType    string
	Body           []byte
	Usage          gatewayUsage
	UpstreamBilled bool
}
