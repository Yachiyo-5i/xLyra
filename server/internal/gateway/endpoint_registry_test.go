package gateway

import (
	"testing"

	"xlyra/server/internal/jsplugin"
)

func TestDownstreamEndpointRegistryIncludesSystemOne(t *testing.T) {
	if err := jsplugin.DefaultCatalog().InitBuiltins(); err != nil {
		t.Fatalf("InitBuiltins: %v", err)
	}
	if !downstreamEndpointUsesTPM(gatewayEndpointTypeSafeSystemOne) {
		t.Fatalf("systemone should use TPM estimates")
	}
	if got := downstreamProtocolFromPath(gatewayEndpointChatCompletions); got != string(canonicalProtocolOpenAIChat) {
		t.Fatalf("downstreamProtocolFromPath = %q", got)
	}
}
