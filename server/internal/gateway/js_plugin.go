package gateway

import (
	"strings"
	"sync"

	"xlyra/server/internal/jsplugin"
)

var (
	jsBuiltinOnce sync.Once
	jsBuiltinReg  *jsplugin.Registry
	jsBuiltinErr  error
)

func jsBuiltinRegistry() (*jsplugin.Registry, error) {
	jsBuiltinOnce.Do(func() {
		jsBuiltinReg, jsBuiltinErr = jsplugin.LoadBuiltins()
	})
	return jsBuiltinReg, jsBuiltinErr
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
