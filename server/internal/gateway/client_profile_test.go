package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/site"
)

func TestUpstreamClientProfileFromSiteConfigAppliesOverrides(t *testing.T) {
	t.Parallel()

	cfg := &site.GatewayConfig{
		RequestTimeoutMS:        gatewayTestIntPtr(12_000),
		ConnectTimeoutMS:        gatewayTestIntPtr(3_000),
		ResponseHeaderTimeoutMS: gatewayTestIntPtr(4_000),
		MaxIdleConns:            gatewayTestIntPtr(101),
		MaxIdleConnsPerHost:     gatewayTestIntPtr(21),
		MaxConnsPerHost:         gatewayTestIntPtr(51),
		IdleConnTimeoutMS:       gatewayTestIntPtr(9_000),
	}

	profile := upstreamClientProfileFromSiteConfig(cfg, "proxy-a")
	if profile.ProxyID != "proxy-a" {
		t.Fatalf("ProxyID = %q, want proxy-a", profile.ProxyID)
	}
	if profile.RequestTimeout != 12*time.Second || profile.ConnectTimeout != 3*time.Second || profile.ResponseHeaderTimeout != 4*time.Second {
		t.Fatalf("unexpected timeout profile: %#v", profile)
	}
	if profile.MaxIdleConns != 101 || profile.MaxIdleConnsPerHost != 21 || profile.MaxConnsPerHost != 51 {
		t.Fatalf("unexpected connection pool profile: %#v", profile)
	}
	if profile.IdleConnTimeout != 9*time.Second {
		t.Fatalf("IdleConnTimeout = %s, want 9s", profile.IdleConnTimeout)
	}
}

func TestUpstreamClientProfileForStreamingRequestsUsesRequestDefaults(t *testing.T) {
	t.Parallel()

	streaming := upstreamClientProfileForRequest(nil, upstreamClientProfileRequest{Stream: true}, "")
	if !streaming.NoRequestTimeout || streaming.RequestTimeout != 0 {
		t.Fatalf("streaming profile should remove request timeout: %#v", streaming)
	}
	if streaming.ResponseHeaderTimeout != defaultStreamingResponseHeaderTimeout {
		t.Fatalf("streaming header timeout = %s, want %s", streaming.ResponseHeaderTimeout, defaultStreamingResponseHeaderTimeout)
	}

	imageStreaming := upstreamClientProfileForRequest(nil, upstreamClientProfileRequest{Stream: true, ImageGeneration: true}, "")
	if imageStreaming.ResponseHeaderTimeout != defaultImageGenerationResponseHeaderTimeout {
		t.Fatalf("image streaming header timeout = %s, want %s", imageStreaming.ResponseHeaderTimeout, defaultImageGenerationResponseHeaderTimeout)
	}

	customHeader := upstreamClientProfileForRequest(&site.GatewayConfig{
		ResponseHeaderTimeoutMS: gatewayTestIntPtr(7_000),
	}, upstreamClientProfileRequest{Stream: true, ImageGeneration: true}, "")
	if customHeader.ResponseHeaderTimeout != 7*time.Second {
		t.Fatalf("custom streaming header timeout = %s, want 7s", customHeader.ResponseHeaderTimeout)
	}

	nonStreaming := upstreamClientProfileForRequest(nil, upstreamClientProfileRequest{}, "proxy-b")
	defaultProfile := httpclient.DefaultProfile()
	if nonStreaming.NoRequestTimeout || nonStreaming.RequestTimeout != defaultProfile.RequestTimeout || nonStreaming.ProxyID != "proxy-b" {
		t.Fatalf("non-streaming profile = %#v, want default profile with proxy", nonStreaming)
	}
}

func TestDefaultResponseHeaderTimeoutForRequest(t *testing.T) {
	t.Parallel()

	if got := defaultResponseHeaderTimeoutForRequest(upstreamClientProfileRequest{}); got != defaultStreamingResponseHeaderTimeout {
		t.Fatalf("default response header timeout = %s, want %s", got, defaultStreamingResponseHeaderTimeout)
	}
	if got := defaultResponseHeaderTimeoutForRequest(upstreamClientProfileRequest{ImageGeneration: true}); got != defaultImageGenerationResponseHeaderTimeout {
		t.Fatalf("image response header timeout = %s, want %s", got, defaultImageGenerationResponseHeaderTimeout)
	}
}

func TestSiteGatewayConfigWithoutStoreReturnsEmptyConfig(t *testing.T) {
	t.Parallel()

	cfg, headers, proxyID, err := (Handler{}).siteGatewayConfig(context.Background(), uuid.New())

	if err != nil {
		t.Fatalf("siteGatewayConfig returned error: %v", err)
	}
	if cfg != nil || headers != nil || proxyID != "" {
		t.Fatalf("siteGatewayConfig = cfg=%#v headers=%#v proxyID=%q, want empty values", cfg, headers, proxyID)
	}
}

func TestUpstreamClientForSiteWithoutStoreUsesDefaultClient(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, "test-master-key")
	client, cfg, headers, err := handler.upstreamClientForSite(context.Background(), uuid.New(), upstreamClientProfileRequest{})

	if err != nil {
		t.Fatalf("upstreamClientForSite returned error: %v", err)
	}
	if client != handler.httpClient || client == nil {
		t.Fatalf("client = %#v, want handler default client %#v", client, handler.httpClient)
	}
	if cfg != nil || headers != nil {
		t.Fatalf("cfg=%#v headers=%#v, want nil nil", cfg, headers)
	}
}

func TestUpstreamClientForStreamingWithoutStoreUsesStreamingProfile(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, "test-master-key")
	client, cfg, headers, err := handler.upstreamClientForSite(context.Background(), uuid.New(), upstreamClientProfileRequest{Stream: true})

	if err != nil {
		t.Fatalf("upstreamClientForSite returned error: %v", err)
	}
	if client == nil {
		t.Fatal("streaming request should receive a client")
	}
	if client == handler.httpClient {
		t.Fatal("streaming request should use a streaming-profile client instead of the default client")
	}
	if client.Timeout != 0 {
		t.Fatalf("streaming client timeout = %s, want no request timeout", client.Timeout)
	}
	if cfg != nil || headers != nil {
		t.Fatalf("cfg=%#v headers=%#v, want nil nil", cfg, headers)
	}
}

func gatewayTestIntPtr(value int) *int {
	return &value
}
