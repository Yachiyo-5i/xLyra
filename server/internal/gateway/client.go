package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

const (
	defaultStreamingResponseHeaderTimeout       = 60 * time.Second
	defaultImageGenerationResponseHeaderTimeout = 300 * time.Second
)

type upstreamClientProfileRequest struct {
	Stream          bool
	ImageGeneration bool
}

func upstreamClientProfileFromSiteConfig(cfg *site.GatewayConfig, proxyID string) httpclient.Profile {
	config := httpclient.DefaultProfile()
	config.ProxyID = proxyID
	if cfg == nil {
		return config
	}
	if cfg.RequestTimeoutMS != nil {
		config.RequestTimeout = time.Duration(*cfg.RequestTimeoutMS) * time.Millisecond
	}
	if cfg.ConnectTimeoutMS != nil {
		config.ConnectTimeout = time.Duration(*cfg.ConnectTimeoutMS) * time.Millisecond
	}
	if cfg.ResponseHeaderTimeoutMS != nil {
		config.ResponseHeaderTimeout = time.Duration(*cfg.ResponseHeaderTimeoutMS) * time.Millisecond
	}
	if cfg.MaxIdleConns != nil {
		config.MaxIdleConns = *cfg.MaxIdleConns
	}
	if cfg.MaxIdleConnsPerHost != nil {
		config.MaxIdleConnsPerHost = *cfg.MaxIdleConnsPerHost
	}
	if cfg.MaxConnsPerHost != nil {
		config.MaxConnsPerHost = *cfg.MaxConnsPerHost
	}
	if cfg.IdleConnTimeoutMS != nil {
		config.IdleConnTimeout = time.Duration(*cfg.IdleConnTimeoutMS) * time.Millisecond
	}
	return config
}

func upstreamClientProfileForRequest(cfg *site.GatewayConfig, request upstreamClientProfileRequest, proxyID string) httpclient.Profile {
	config := upstreamClientProfileFromSiteConfig(cfg, proxyID)
	if request.Stream {
		config = httpclient.StreamingProfile(config)
		if cfg == nil || cfg.ResponseHeaderTimeoutMS == nil {
			config.ResponseHeaderTimeout = defaultResponseHeaderTimeoutForRequest(request)
		}
	}
	return config
}

func defaultResponseHeaderTimeoutForRequest(request upstreamClientProfileRequest) time.Duration {
	if request.ImageGeneration {
		return defaultImageGenerationResponseHeaderTimeout
	}
	return defaultStreamingResponseHeaderTimeout
}

func (h Handler) siteGatewayConfig(ctx context.Context, siteID uuid.UUID) (*site.GatewayConfig, map[string]string, string, error) {
	if h.db == nil {
		return nil, nil, "", nil
	}

	item, err := store.NewSiteRepository(h.db.DB()).GetByID(ctx, siteID)
	if err != nil {
		return nil, nil, "", err
	}

	meta := map[string]any{}
	if len(item.Meta) > 0 {
		_ = json.Unmarshal(item.Meta, &meta)
	}
	proxyID, _ := meta["proxy_id"].(string)

	return site.GatewayConfigFromSiteMeta(item.Meta), site.RequestHeadersFromSiteMeta(item.Meta), proxyID, nil
}

func (h Handler) upstreamClientForSite(ctx context.Context, siteID uuid.UUID, request upstreamClientProfileRequest) (*http.Client, *site.GatewayConfig, map[string]string, error) {
	cfg, requestHeaders, proxyID, err := h.siteGatewayConfig(ctx, siteID)
	if err != nil {
		return h.httpClient, nil, nil, err
	}
	if cfg == nil && proxyID == "" && !request.Stream {
		return h.httpClient, nil, requestHeaders, nil
	}
	client, err := h.clients.Client(upstreamClientProfileForRequest(cfg, request, proxyID))
	if err != nil {
		return h.httpClient, nil, nil, err
	}
	return client, cfg, requestHeaders, nil
}
