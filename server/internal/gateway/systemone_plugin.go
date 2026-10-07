package gateway

import (
	"context"
	"fmt"
	"net/http"

	routeengine "xlyra/server/internal/router"
)

func builtinSystemOneProtocol(ctx context.Context, request gatewayRequest, candidate routeengine.Candidate) (gatewayProtocolAdapter, error) {
	if gatewayRequestStreams(request) {
		return nil, fmt.Errorf("streaming is not supported for systemone")
	}
	registry, err := jsBuiltinRegistry()
	if err != nil {
		return nil, err
	}
	plugin, ok := registry.ByProtocolName("typesafe_systemone")
	if !ok {
		return nil, fmt.Errorf("typesafe_systemone plugin is not loaded")
	}
	return newJSProtocolAdapter(ctx, plugin, request, candidate), nil
}

func (h Handler) typeSafeSystemOneEndpoint() gatewayEndpointAdapter {
	registry, err := jsBuiltinRegistry()
	if err != nil {
		return unavailableEndpointAdapter{message: err.Error()}
	}
	plugin, ok := registry.ByProtocolName("typesafe_systemone")
	if !ok {
		return unavailableEndpointAdapter{message: "typesafe_systemone plugin is not loaded"}
	}
	return newJSEndpointAdapter(plugin, nil)
}

type unavailableEndpointAdapter struct {
	message string
}

func (a unavailableEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointTypeSafeSystemOne
}

func (a unavailableEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeTypeSafeSystemOne
}

func (a unavailableEndpointAdapter) DecodeRequest(_ *http.Request) (gatewayRequest, *chatFailure) {
	return gatewayRequest{}, &chatFailure{
		status:  http.StatusServiceUnavailable,
		code:    "js_plugin_unavailable",
		message: a.message,
		stage:   "plugin",
	}
}
