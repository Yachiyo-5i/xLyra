package gateway

import (
	"strings"

	"xlyra/server/internal/jsplugin"
)

func jsBuiltinRegistry() (*jsplugin.Registry, error) {
	registry := jsplugin.DefaultCatalog().Registry()
	if registry == nil {
		if err := jsplugin.DefaultCatalog().InitBuiltins(); err != nil {
			return nil, err
		}
		registry = jsplugin.DefaultCatalog().Registry()
	}
	return registry, nil
}

func gatewayRequestStreams(request gatewayRequest) bool {
	if request.Stream {
		return true
	}
	if value, ok := request.Payload["stream"].(bool); ok && value {
		return true
	}
	if format, ok := request.Payload["stream_format"].(string); ok && strings.EqualFold(strings.TrimSpace(format), "sse") {
		return true
	}
	return false
}
