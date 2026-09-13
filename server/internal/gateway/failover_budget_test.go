package gateway

import (
	"testing"
	"time"
)

func TestGatewayFailoverBudgetByRequestType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		request gatewayRequest
		want    time.Duration
	}{
		{name: "buffered", request: gatewayRequest{}, want: defaultGatewayFailoverBudget},
		{name: "streaming", request: gatewayRequest{Stream: true, DownstreamPath: gatewayEndpointChatCompletions}, want: defaultStreamingFailoverBudget},
		{name: "image", request: gatewayRequest{Stream: true, DownstreamPath: gatewayEndpointImagesGenerations}, want: defaultImageFailoverBudget},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gatewayFailoverBudget(tt.request); got != tt.want {
				t.Fatalf("budget = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestUpstreamModelUnavailableFailureMasksUpstreamDetails(t *testing.T) {
	t.Parallel()
	tests := []gatewayAttemptResult{
		{statusCode: 404, upstreamStatusCode: 404},
		{statusCode: 502, upstreamErrorCode: "model_not_found"},
		{statusCode: 502, errorMessage: "upstream model not found"},
	}
	for _, result := range tests {
		if !upstreamModelUnavailableFailure(result) {
			t.Fatalf("result %#v was not recognized as model unavailable", result)
		}
	}
	if upstreamModelUnavailableFailure(gatewayAttemptResult{statusCode: 500, errorMessage: "upstream overloaded"}) {
		t.Fatal("ordinary upstream failure was recognized as model unavailable")
	}
}
