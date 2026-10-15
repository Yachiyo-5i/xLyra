package gateway

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
	"xlyra/server/internal/jsplugin"
)

type jsEndpointAdapter struct {
	plugin   *jsplugin.Plugin
	fallback gatewayEndpointAdapter
}

func newJSEndpointAdapter(plugin *jsplugin.Plugin, fallback gatewayEndpointAdapter) jsEndpointAdapter {
	return jsEndpointAdapter{plugin: plugin, fallback: fallback}
}

func (a jsEndpointAdapter) DownstreamPath() string {
	if path := strings.TrimSpace(a.plugin.Manifest.Protocol.DownstreamPath); path != "" {
		return path
	}
	if a.fallback != nil {
		return a.fallback.DownstreamPath()
	}
	return ""
}

func (a jsEndpointAdapter) RouteEndpointType() string {
	if endpoint := strings.TrimSpace(a.plugin.Manifest.Protocol.EndpointType); endpoint != "" {
		return endpoint
	}
	if a.fallback != nil {
		return a.fallback.RouteEndpointType()
	}
	return ""
}

func (a jsEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return gatewayRequest{}, decodeRequestFailure(err)
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var payload map[string]any
	if err := httpx.DecodeJSONBody(r, &payload); err != nil {
		return gatewayRequest{}, decodeRequestFailure(err)
	}
	if gatewayPayloadStreams(payload) {
		if a.fallback != nil {
			r.Body = io.NopCloser(bytes.NewReader(raw))
			return a.fallback.DecodeRequest(r)
		}
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "stream_not_supported",
			message: "streaming is not supported for this protocol",
			stage:   "validate",
		}
	}
	ctx := jsplugin.ProtocolEndpointContext{DownstreamPath: a.DownstreamPath()}
	decoded, failure, logs, dropped, callErr := a.plugin.CallDecodeRequest(r.Context(), ctx, payload)
	jsplugin.EmitLogs(r.Context(), a.plugin, "decodeRequest", 0, logs, dropped)
	if callErr != nil {
		return gatewayRequest{}, jsPluginChatFailure(callErr)
	}
	if failure != nil {
		return gatewayRequest{}, &chatFailure{
			status:  failure.Status,
			code:    failure.Code,
			message: failure.Message,
			stage:   "validate",
		}
	}
	if decoded.Model != "" {
		payload["model"] = decoded.Model
	}
	return gatewayRequest{
		DownstreamPath: a.DownstreamPath(),
		RequestedModel: decoded.Model,
		Stream:         false,
		Payload:        payload,
	}, nil
}

func gatewayPayloadStreams(payload map[string]any) bool {
	if value, ok := payload["stream"].(bool); ok && value {
		return true
	}
	if format, ok := payload["stream_format"].(string); ok && strings.EqualFold(strings.TrimSpace(format), "sse") {
		return true
	}
	return false
}

func jsPluginChatFailure(err error) *chatFailure {
	var call *jsplugin.CallError
	if errorAsCall(err, &call) {
		return &chatFailure{
			status:  http.StatusBadGateway,
			code:    call.Kind,
			message: call.Error(),
			stage:   "plugin",
		}
	}
	return &chatFailure{
		status:  http.StatusBadGateway,
		code:    "js_plugin_failed",
		message: err.Error(),
		stage:   "plugin",
	}
}
