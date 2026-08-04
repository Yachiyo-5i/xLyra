package gateway

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func TestPortalRequestItemHidesDisabledDimensions(t *testing.T) {
	t.Parallel()

	item := store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:         uuid.New(),
			RequestID:  "req-1",
			Endpoint:   "/v1/chat/completions",
			StatusCode: 200,
			Success:    true,
			LatencyMS:  sql.NullInt64{Int64: 120, Valid: true},
			Metadata:   store.JSON(`{"upstream_url":"https://upstream.example/v1"}`),
		},
		SiteName:              sql.NullString{String: "secret-site", Valid: true},
		SiteModelUpstreamName: sql.NullString{String: "gpt-x", Valid: true},
		EstimatedCost:         sql.NullFloat64{Float64: 0.5, Valid: true},
	}

	dims := config.PortalDimensions{
		Site:     false,
		Model:    true,
		Tokens:   false,
		Cost:     true,
		Latency:  false,
		Endpoint: false,
		Upstream: false,
		Error:    true,
	}

	payload := portalRequestItem(item, dims)

	for _, hidden := range []string{"site", "usage", "latency_ms", "endpoint", "upstream"} {
		if _, ok := payload[hidden]; ok {
			t.Fatalf("expected %q to be hidden, payload=%#v", hidden, payload)
		}
	}
	if _, ok := payload["model"]; !ok {
		t.Fatal("model dimension should be present")
	}
	if _, ok := payload["cost"]; !ok {
		t.Fatal("cost dimension should be present")
	}
}

func TestPortalRequestItemIncludesEnabledDimensions(t *testing.T) {
	t.Parallel()

	item := store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:         uuid.New(),
			RequestID:  "req-2",
			Endpoint:   "/v1/chat/completions",
			StatusCode: 500,
			Success:    false,
			Metadata:   store.JSON(`{"upstream_url":"https://upstream.example/v1","upstream_path":"/chat"}`),
			ErrorType:  sql.NullString{String: "upstream_error", Valid: true},
		},
		SiteName: sql.NullString{String: "site-a", Valid: true},
	}

	dims := config.PortalDimensions{Site: true, Endpoint: true, Upstream: true, Error: true}
	payload := portalRequestItem(item, dims)

	site, ok := payload["site"].(map[string]any)
	if !ok || site["name"] != "site-a" {
		t.Fatalf("expected site name in payload, got %#v", payload["site"])
	}
	upstream, ok := payload["upstream"].(map[string]any)
	if !ok || upstream["url"] != "https://upstream.example/v1" {
		t.Fatalf("expected upstream url in payload, got %#v", payload["upstream"])
	}
	if payload["error_type"] != "upstream_error" {
		t.Fatalf("expected error_type for failed request, got %#v", payload["error_type"])
	}
}

func TestPortalUsageBucketRespectsDimensions(t *testing.T) {
	t.Parallel()

	bucket := usage.KeyDailyUsage{Date: "2026-07-13", Requests: 5, Success: 4, TotalTokens: 100, Cost: 0.25}

	withoutCost := portalUsageBucket(bucket, config.PortalDimensions{Tokens: true, Cost: false})
	if _, ok := withoutCost["cost"]; ok {
		t.Fatal("cost should be hidden when dimension disabled")
	}
	if _, ok := withoutCost["total_tokens"]; !ok {
		t.Fatal("tokens should be present when dimension enabled")
	}

	minimal := portalUsageBucket(bucket, config.PortalDimensions{})
	if _, ok := minimal["total_tokens"]; ok {
		t.Fatal("tokens should be hidden when dimension disabled")
	}
	if minimal["requests"] == nil {
		t.Fatal("requests count should always be present")
	}
}
