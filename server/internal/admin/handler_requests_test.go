package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func TestRequestLogFiltersParseValidQuery(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()
	from := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	values := url.Values{}
	values.Set("success", "true")
	values.Set("hide_without_site", "true")
	values.Set("site_id", siteID.String())
	values.Set("api_key_id", apiKeyID.String())
	values.Set("search", " claude ")
	values.Set("model_key", " gpt-5 ")
	values.Set("error_type", " upstream ")
	values.Set("endpoint", " /v1/responses ")
	values.Set("request_id", " req_123 ")
	values.Set("created_from", from.Format(time.RFC3339Nano))
	values.Set("created_to", to.Format(time.RFC3339Nano))
	req := adminTestRequest(http.MethodGet, "/api/v1/requests?"+values.Encode(), "")
	rec := adminRecorder()

	query, ok := (Handler{}).requestLogFilters(rec, req)
	adminRequireParserOK(t, rec, ok, "request log filters")
	if query.Success == nil || *query.Success != true {
		t.Fatalf("success filter = %#v, want true", query.Success)
	}
	if query.SiteID == nil || *query.SiteID != siteID || query.APIKeyID == nil || *query.APIKeyID != apiKeyID {
		t.Fatalf("unexpected UUID filters: site=%v api_key=%v", query.SiteID, query.APIKeyID)
	}
	if query.Search != "claude" || query.ModelKey != "gpt-5" || query.ErrorType != "upstream" || query.Endpoint != "/v1/responses" || query.RequestID != "req_123" {
		t.Fatalf("unexpected text filters: %#v", query)
	}
	if !query.HideWithoutSite {
		t.Fatal("expected hide_without_site to be true")
	}
	if query.CreatedFrom == nil || !query.CreatedFrom.Equal(from) || query.CreatedTo == nil || !query.CreatedTo.Equal(to) {
		t.Fatalf("unexpected time filters: from=%v to=%v", query.CreatedFrom, query.CreatedTo)
	}
}

func TestRequestLogFiltersRejectInvalidBooleanFilters(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		target string
		code   string
	}{
		{name: "success", target: "/api/v1/requests?success=maybe", code: "invalid_success"},
		{name: "hide_without_site", target: "/api/v1/requests?hide_without_site=maybe", code: "invalid_hide_without_site"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodGet, tc.target, "")
			rec := adminRecorder()
			_, ok := (Handler{}).requestLogFilters(rec, req)
			adminAssertParserError(t, rec, ok, tc.code)
		})
	}
}

func TestRequestLogParentRequestIDPrefersTypedColumn(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{"parent_request_id": "legacy-parent"})
	if err != nil {
		t.Fatal(err)
	}
	item := store.RequestLogDetail{RequestLog: store.RequestLog{
		ParentRequestID: sql.NullString{String: "typed-parent", Valid: true},
		Metadata:        metadata,
	}}
	if got := requestLogParentRequestID(item); got != "typed-parent" {
		t.Fatalf("parent request id = %q, want typed-parent", got)
	}
}

func TestRequestLogSummaryReturnsUnsupportedPayloadForSearchFilter(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, nil, usage.NewService(nil), nil, nil, nil, nil, nil)
	rec := adminPerform(handler.RequestLogSummary, adminTestRequest(http.MethodGet, "/api/v1/requests/summary?search=claude", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Summary struct {
			TotalCost         any    `json:"total_cost"`
			PromptTokens      any    `json:"prompt_tokens"`
			CompletionTokens  any    `json:"completion_tokens"`
			TotalTokens       any    `json:"total_tokens"`
			CachedTokens      any    `json:"cached_tokens"`
			Currency          string `json:"currency"`
			Supported         bool   `json:"supported"`
			UnsupportedReason string `json:"unsupported_reason"`
		} `json:"summary"`
	}](t, rec)
	if body.Summary.Supported {
		t.Fatalf("expected unsupported summary, got %#v", body.Summary)
	}
	if body.Summary.UnsupportedReason != "search_filter" || body.Summary.Currency != "USD" {
		t.Fatalf("unexpected unsupported summary metadata: %#v", body.Summary)
	}
	if body.Summary.TotalCost != nil || body.Summary.PromptTokens != nil || body.Summary.CompletionTokens != nil || body.Summary.TotalTokens != nil || body.Summary.CachedTokens != nil {
		t.Fatalf("unsupported summary should null totals, got %#v", body.Summary)
	}
}

func TestRequestLogPayloadFallsBackToMetadataSnapshot(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"site_id":                 "site-deleted-id",
		"site_name":               "Codex_xcha_0419",
		"site_slug":               "codex-n45pjx",
		"site_type":               "codex",
		"site_model_id":           "site-model-deleted-id",
		"upstream_model":          "gpt-5.4",
		"site_model_display_name": "GPT 5.4",
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:        uuid.New(),
			RequestID: "req_123",
			Endpoint:  "/v1/responses",
			Success:   true,
			Metadata:  metadata,
		},
	}, false)

	site, _ := payload["site"].(map[string]any)
	model, _ := payload["model"].(map[string]any)
	if site["name"] != "Codex_xcha_0419" || site["slug"] != "codex-n45pjx" || site["site_type"] != "codex" {
		t.Fatalf("unexpected site snapshot fallback: %#v", site)
	}
	if model["site_model_id"] != "site-model-deleted-id" || model["upstream_model"] != "gpt-5.4" || model["display_name"] != "GPT 5.4" {
		t.Fatalf("unexpected model snapshot fallback: %#v", model)
	}
}

func TestRequestLogPayloadIncludesFastBillingCostCalculation(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"billing_mode": "fast",
		"cost_calculation": map[string]any{
			"service_tier":        "fast",
			"billing_mode":        "fast",
			"base_estimated_cost": 0.5,
			"cost_multiplier":     2.5,
			"estimated_cost":      1.25,
			"currency":            "USD",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:        uuid.New(),
			RequestID: "req_fast",
			Endpoint:  "/v1/responses",
			Success:   true,
			Metadata:  metadata,
		},
		EstimatedCost: sql.NullFloat64{Float64: 1.25, Valid: true},
		UsageCurrency: sql.NullString{String: "USD", Valid: true},
	}, false)

	calculation, ok := payload["cost_calculation"].(map[string]any)
	if !ok {
		t.Fatalf("expected cost_calculation payload, got %T", payload["cost_calculation"])
	}
	if calculation["billing_mode"] != "fast" || calculation["service_tier"] != "fast" {
		t.Fatalf("unexpected fast metadata: %#v", calculation)
	}
	if calculation["cost_multiplier"] != 2.5 || calculation["estimated_cost"] != 1.25 {
		t.Fatalf("unexpected cost calculation: %#v", calculation)
	}
}

func TestRequestLogPayloadPreservesFailedLongContextBilling(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"pricing": map[string]any{
			"input_value":  10.0,
			"output_value": 45.0,
			"currency":     "USD",
		},
		"cost_calculation": map[string]any{
			"long_context":                   true,
			"long_context_threshold_tokens":  272000,
			"long_context_input_multiplier":  2.0,
			"long_context_output_multiplier": 1.5,
			"prompt_tokens":                  300000,
			"completion_tokens":              10000,
			"estimated_cost":                 1.65,
			"currency":                       "USD",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:        uuid.New(),
			RequestID: "req_failed_long_context",
			Endpoint:  "/v1/responses",
			Success:   false,
			Metadata:  metadata,
		},
		UsagePromptTokens:     sql.NullInt64{Int64: 300000, Valid: true},
		UsageCompletionTokens: sql.NullInt64{Int64: 10000, Valid: true},
		UsageTotalTokens:      sql.NullInt64{Int64: 310000, Valid: true},
		EstimatedCost:         sql.NullFloat64{Float64: 1.65, Valid: true},
		UsageCurrency:         sql.NullString{String: "USD", Valid: true},
	}, false)

	calculation, ok := payload["cost_calculation"].(map[string]any)
	if !ok || calculation["long_context"] != true || calculation["long_context_threshold_tokens"] != float64(272000) {
		t.Fatalf("failed long-context calculation = %#v", payload["cost_calculation"])
	}
	pricing, ok := payload["pricing"].(map[string]any)
	if !ok || pricing["input_value"] != 10.0 || pricing["output_value"] != 45.0 {
		t.Fatalf("failed long-context pricing = %#v", payload["pricing"])
	}
	usagePayload, ok := payload["usage"].(map[string]any)
	if !ok || usagePayload["estimated_cost"] != 1.65 || payload["success"] != false {
		t.Fatalf("failed long-context usage = %#v", payload)
	}
}

func TestRequestLogPayloadMarksFailoverAttempt(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"attempt":            2,
		"credential_attempt": 1,
		"credential_total":   2,
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req:2", Metadata: metadata},
	}, false)
	if payload["attempt"] != 2 || payload["credential_total"] != 2 || payload["failover"] != true {
		t.Fatalf("unexpected failover fields: %#v", payload)
	}
}

func TestRequestLogPayloadProjectsFailoverDiagnostics(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"attempt":                     1,
		"credential_attempt":          1,
		"credential_total":            2,
		"next_credential_available":   true,
		"should_try_next_credential":  false,
		"failover_action":             "stop",
		"stream_started":              true,
		"stream_completed":            false,
		"stream_received_done":        false,
		"stream_incomplete":           true,
		"stream_end_reason":           "upstream_stream_read_failed",
		"pre_output_events_buffered":  1,
		"pre_output_failure_deferred": false,
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req_diagnostic", Success: false, Metadata: metadata},
	}, false)

	for key, want := range map[string]any{
		"next_credential_available":   true,
		"should_try_next_credential":  false,
		"failover_action":             "stop",
		"stream_started":              true,
		"stream_completed":            false,
		"stream_received_done":        false,
		"stream_incomplete":           true,
		"stream_end_reason":           "upstream_stream_read_failed",
		"pre_output_events_buffered":  int64(1),
		"pre_output_failure_deferred": false,
	} {
		if got := payload[key]; got != want {
			t.Fatalf("%s = %#v, want %#v", key, got, want)
		}
	}
}

func TestRequestLogFailoverTracePayloadGroupsCredentialRetriesByChannel(t *testing.T) {
	t.Parallel()

	metadata := func(attempt int, site string, success bool, status int, errorType string, credentialID string, credentialName string) store.JSON {
		value, err := json.Marshal(map[string]any{
			"attempt":              attempt,
			"parent_request_id":    "req-parent",
			"site_name":            site,
			"site_type":            "openai",
			"upstream_status_code": status,
			"credential_masked":    "sk-...secret",
			"credential_id":        credentialID,
			"credential_name":      credentialName,
			"credential_attempt":   attempt,
			"credential_total":     2,
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	attempts := []store.RequestLogDetail{
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-parent:1:first", Success: false, StatusCode: http.StatusBadGateway, ErrorType: sql.NullString{String: "upstream_timeout", Valid: true}, Metadata: metadata(1, "Default", false, http.StatusGatewayTimeout, "upstream_timeout", "credential-a", "Primary")}},
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-parent:1:second", Success: false, StatusCode: http.StatusBadGateway, ErrorType: sql.NullString{String: "upstream_credential_limited", Valid: true}, Metadata: metadata(1, "Default", false, http.StatusTooManyRequests, "upstream_credential_limited", "credential-b", "Backup")}},
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-parent:2:middle", Success: false, StatusCode: http.StatusBadGateway, ErrorType: sql.NullString{String: "upstream_transport_error", Valid: true}, Metadata: metadata(2, "Fallback A", false, http.StatusBadGateway, "upstream_transport_error", "credential-c", "Fallback A key")}},
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-parent:3:final", Success: true, StatusCode: http.StatusOK, Metadata: metadata(3, "Fallback B", true, http.StatusOK, "", "credential-d", "Fallback B key")}},
	}

	trace := requestLogFailoverTracePayload(attempts)
	if trace == nil {
		t.Fatal("expected failover trace")
	}
	defaultChannel, _ := trace["default_channel"].(map[string]any)
	defaultSite, _ := defaultChannel["site"].(map[string]any)
	if defaultChannel["success"] != false || defaultChannel["error_type"] != "upstream_credential_limited" || defaultChannel["upstream_status_code"] != http.StatusTooManyRequests {
		t.Fatalf("unexpected default channel: %#v", defaultChannel)
	}
	if defaultSite["name"] != "Default" || defaultSite["site_type"] != "openai" {
		t.Fatalf("unexpected default channel site: %#v", defaultSite)
	}
	if _, ok := defaultChannel["credential"]; ok {
		t.Fatalf("channel detail must not expose credential data: %#v", defaultChannel)
	}
	intermediate, _ := trace["intermediate_channels"].([]map[string]any)
	if len(intermediate) != 1 || intermediate[0]["error_type"] != "upstream_transport_error" {
		t.Fatalf("unexpected intermediate channels: %#v", intermediate)
	}
	finalChannel, _ := trace["final_channel"].(map[string]any)
	finalSite, _ := finalChannel["site"].(map[string]any)
	if finalChannel["success"] != true || finalSite["name"] != "Fallback B" {
		t.Fatalf("unexpected final channel: %#v", finalChannel)
	}
	credentialAttempts, ok := trace["credential_attempts"].([]map[string]any)
	if !ok || len(credentialAttempts) != len(attempts) {
		t.Fatalf("unexpected credential attempts: %#v", trace["credential_attempts"])
	}
	firstCredential, _ := credentialAttempts[0]["credential"].(map[string]any)
	if firstCredential["id"] != "credential-a" || firstCredential["name"] != "Primary" || credentialAttempts[0]["success"] != false {
		t.Fatalf("unexpected first credential attempt: %#v", credentialAttempts[0])
	}
	lastCredential, _ := credentialAttempts[len(credentialAttempts)-1]["credential"].(map[string]any)
	if lastCredential["id"] != "credential-d" || lastCredential["name"] != "Fallback B key" || credentialAttempts[len(credentialAttempts)-1]["success"] != true {
		t.Fatalf("unexpected last credential attempt: %#v", credentialAttempts[len(credentialAttempts)-1])
	}
	encoded, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "credential_masked") || strings.Contains(string(encoded), "masked_secret") || strings.Contains(string(encoded), "sk-...secret") {
		t.Fatalf("credential trace leaked secret material: %s", encoded)
	}
}

func TestRequestLogFailoverTracePayloadKeepsSameChannelCredentialChain(t *testing.T) {
	t.Parallel()

	metadata := func(id, name string, success bool, attempt int) store.JSON {
		value, err := json.Marshal(map[string]any{
			"parent_request_id":  "req-same-channel",
			"site_id":            "site-default",
			"site_name":          "Default",
			"site_type":          "openai",
			"credential_id":      id,
			"credential_name":    name,
			"credential_attempt": attempt,
			"credential_total":   2,
			"credential_masked":  "sk-...hidden",
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	trace := requestLogFailoverTracePayload([]store.RequestLogDetail{
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-same-channel:1", Success: false, StatusCode: http.StatusBadGateway, ErrorType: sql.NullString{String: "upstream_credential_limited", Valid: true}, Metadata: metadata("credential-a", "Primary", false, 1)}},
		{RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req-same-channel:2", Success: true, StatusCode: http.StatusOK, Metadata: metadata("credential-b", "Backup", true, 2)}},
	})
	if trace == nil {
		t.Fatal("expected same-channel credential trace")
	}
	credentialAttempts, ok := trace["credential_attempts"].([]map[string]any)
	if !ok || len(credentialAttempts) != 2 {
		t.Fatalf("unexpected same-channel credential attempts: %#v", trace["credential_attempts"])
	}
	if credentialAttempts[0]["success"] != false || credentialAttempts[1]["success"] != true {
		t.Fatalf("credential attempt outcomes = %#v", credentialAttempts)
	}
}

func TestRequestLogFailoverTracePayloadOmitsTerminalChannelWhenNoSwitchOccurs(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"parent_request_id":    "req-parent",
		"site_name":            "Default",
		"site_type":            "openai",
		"upstream_status_code": http.StatusTooManyRequests,
	})
	if err != nil {
		t.Fatal(err)
	}

	trace := requestLogFailoverTracePayload([]store.RequestLogDetail{
		{
			RequestLog: store.RequestLog{
				ID:         uuid.New(),
				RequestID:  "req-parent:1",
				Success:    false,
				StatusCode: http.StatusBadGateway,
				ErrorType:  sql.NullString{String: "upstream_credential_limited", Valid: true},
				Metadata:   metadata,
			},
		},
	})
	if trace != nil {
		t.Fatalf("unexpected failover trace without a switch: %#v", trace)
	}
}

func TestRequestLogPayloadProjectsTimingReasoningAndSecretFreeCredential(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"first_byte_latency": 37,
		"reasoning_effort":   "high",
		"credential_id":      "credential-fallback",
		"credential_name":    "Fallback name",
		"credential_masked":  "sk-...fallback",
		"credential": map[string]any{
			"id":            "credential-123",
			"name":          "Production key",
			"masked_secret": "sk-...123",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{
			ID:        uuid.New(),
			RequestID: "req_reasoning",
			Metadata:  metadata,
		},
	}, true)
	if got := payload["first_byte_latency_ms"]; got != int64(37) {
		t.Fatalf("first_byte_latency_ms = %#v, want 37", got)
	}
	if got := payload["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}
	credential, ok := payload["credential"].(map[string]any)
	if !ok || credential["id"] != "credential-123" || credential["name"] != "Production key" {
		t.Fatalf("unexpected credential projection: %#v", payload["credential"])
	}
	if _, ok := credential["masked_secret"]; ok {
		t.Fatalf("credential projection must not include masked secret: %#v", credential)
	}
	responseMetadata, ok := payload["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata payload = %#v", payload["metadata"])
	}
	if _, ok := responseMetadata["credential_masked"]; ok {
		t.Fatalf("metadata must not expose credential_masked: %#v", responseMetadata)
	}
	responseCredential, ok := responseMetadata["credential"].(map[string]any)
	if !ok {
		t.Fatalf("metadata credential = %#v", responseMetadata["credential"])
	}
	if _, ok := responseCredential["masked_secret"]; ok {
		t.Fatalf("metadata must not expose credential masked_secret: %#v", responseCredential)
	}
}

func TestRequestLogPayloadOmitsUnknownFirstByteLatencyAndCredential(t *testing.T) {
	t.Parallel()

	metadata, err := json.Marshal(map[string]any{
		"first_byte_latency": 0,
		"reasoning_effort":   42,
		"credential":         map[string]any{"masked_secret": "sk-...123"},
	})
	if err != nil {
		t.Fatal(err)
	}

	payload := requestLogPayload(store.RequestLogDetail{
		RequestLog: store.RequestLog{ID: uuid.New(), RequestID: "req_legacy", Metadata: metadata},
	}, false)
	if payload["first_byte_latency_ms"] != nil {
		t.Fatalf("unknown first byte latency = %#v, want nil", payload["first_byte_latency_ms"])
	}
	if payload["reasoning_effort"] != nil {
		t.Fatalf("invalid reasoning effort = %#v, want nil", payload["reasoning_effort"])
	}
	if payload["credential"] != nil {
		t.Fatalf("credential without id/name = %#v, want nil", payload["credential"])
	}
}
