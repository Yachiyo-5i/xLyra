package gateway

import (
	"testing"

	"xlyra/server/internal/config"
	oauthsvc "xlyra/server/internal/oauth"
)

func TestNewHandlerInstallsOAuthServiceWhenNil(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, "test-master-key")
	if handler.oauth == nil {
		t.Fatal("NewHandler oauth = nil, want a service")
	}

	withTimeZone := NewHandlerWithTimeZone(nil, nil, nil, nil, "test-master-key", config.ResolveTimeZone())
	if withTimeZone.oauth == nil {
		t.Fatal("NewHandlerWithTimeZone oauth = nil, want a service")
	}

	withNil := NewHandlerWithOAuthService(nil, nil, nil, nil, "test-master-key", config.ResolveTimeZone(), nil)
	if withNil.oauth == nil {
		t.Fatal("NewHandlerWithOAuthService nil service = nil, want a service")
	}

	service := oauthsvc.NewService(nil, "test-master-key")
	withService := NewHandlerWithOAuthService(nil, nil, nil, nil, "test-master-key", config.ResolveTimeZone(), service)
	if withService.oauth != service {
		t.Fatal("NewHandlerWithOAuthService stored a different oauth service")
	}
}
