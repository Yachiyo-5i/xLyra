package gateway

import "time"

const (
	defaultGatewayFailoverBudget   = 300 * time.Second
	defaultStreamingFailoverBudget = 90 * time.Second
	defaultImageFailoverBudget     = 180 * time.Second
)

func gatewayFailoverBudget(request gatewayRequest) time.Duration {
	if request.Stream {
		if request.DownstreamPath == gatewayEndpointImagesGenerations || request.DownstreamPath == gatewayEndpointImagesEdits {
			return defaultImageFailoverBudget
		}
		return defaultStreamingFailoverBudget
	}
	return defaultGatewayFailoverBudget
}
