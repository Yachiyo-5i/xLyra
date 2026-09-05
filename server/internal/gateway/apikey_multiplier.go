package gateway

import "context"

type apiKeyBillingMultiplierKey struct{}

// WithAPIKeyBillingMultiplier carries the local API key's billing multiplier
// through the request context so every downstream attempt route applies the
// same factor regardless of which path (direct, bridge, failover) reached it.
func WithAPIKeyBillingMultiplier(ctx context.Context, multiplier float64) context.Context {
	return context.WithValue(ctx, apiKeyBillingMultiplierKey{}, multiplier)
}

func apiKeyBillingMultiplierFromContext(ctx context.Context) float64 {
	if value, ok := ctx.Value(apiKeyBillingMultiplierKey{}).(float64); ok && value > 0 {
		return value
	}
	return 1
}
