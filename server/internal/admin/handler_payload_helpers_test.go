package admin

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestPayloadMetadataNullableAndPointerValues(t *testing.T) {
	t.Parallel()

	nested := map[string]any{"enabled": true}
	if got, ok := metadataMap(map[string]any{"meta": nested}, "meta").(map[string]any); !ok || got["enabled"] != true {
		t.Fatalf("metadataMap = %#v, want nested map", got)
	}
	if metadataMap(map[string]any{"meta": "not-map"}, "meta") != nil {
		t.Fatal("metadataMap should return nil for non-map values")
	}
	if metadataMap(nil, "missing") != nil {
		t.Fatal("metadataMap should return nil for nil source")
	}

	if got := metadataString(map[string]any{"name": " value "}, "name"); got != " value " {
		t.Fatalf("metadataString = %#v, want original string", got)
	}
	if metadataString(map[string]any{"name": 12}, "name") != nil {
		t.Fatal("metadataString should return nil for non-string values")
	}

	if got := valueOrFallback("primary", "fallback"); got != "primary" {
		t.Fatalf("valueOrFallback should prefer non-nil value, got %#v", got)
	}
	if got := valueOrFallback(nil, " fallback "); got != " fallback " {
		t.Fatalf("valueOrFallback should preserve non-blank fallback string, got %#v", got)
	}
	if valueOrFallback(nil, " \t ") != nil {
		t.Fatal("valueOrFallback should collapse blank fallback strings to nil")
	}
	if got := valueOrFallback(nil, 7); got != 7 {
		t.Fatalf("valueOrFallback should return non-string fallback, got %#v", got)
	}

	now := payloadTimestamp(10, 30)
	if got := nullStringValue(sql.NullString{String: "hello", Valid: true}); got != "hello" {
		t.Fatalf("nullStringValue = %#v, want hello", got)
	}
	if nullStringValue(sql.NullString{String: "ignored"}) != nil {
		t.Fatal("invalid null string should return nil")
	}
	if got := nullBoolValue(sql.NullBool{Bool: false, Valid: true}); got != false {
		t.Fatalf("nullBoolValue = %#v, want false", got)
	}
	if nullBoolValue(sql.NullBool{Bool: true}) != nil {
		t.Fatal("invalid null bool should return nil")
	}
	if got := nullTimeValue(sql.NullTime{Time: now, Valid: true}); got != timeString(now) {
		t.Fatalf("nullTimeValue = %#v, want %s", got, timeString(now))
	}
	if nullTimeValue(sql.NullTime{Time: now}) != nil {
		t.Fatal("invalid null time should return nil")
	}
	if got := nullFloat64Value(sql.NullFloat64{Float64: 1.25, Valid: true}); got != 1.25 {
		t.Fatalf("nullFloat64Value = %#v, want 1.25", got)
	}
	if nullFloat64Value(sql.NullFloat64{Float64: 1.25}) != nil {
		t.Fatal("invalid null float should return nil")
	}
	if got := nullInt64Value(sql.NullInt64{Int64: 42, Valid: true}); got != int64(42) {
		t.Fatalf("nullInt64Value = %#v, want 42", got)
	}
	if nullInt64Value(sql.NullInt64{Int64: 42}) != nil {
		t.Fatal("invalid null int should return nil")
	}

	id := uuid.New()
	if got := nullUUIDValue(uuid.NullUUID{UUID: id, Valid: true}); got != id.String() {
		t.Fatalf("nullUUIDValue = %#v, want %s", got, id)
	}
	if nullUUIDValue(uuid.NullUUID{UUID: id}) != nil {
		t.Fatal("invalid null UUID should return nil")
	}

	text := "visible"
	if got := pointerStringValue(&text); got != "visible" {
		t.Fatalf("pointerStringValue = %#v, want visible", got)
	}
	if pointerStringValue(nil) != nil {
		t.Fatal("nil string pointer should return nil")
	}
	floatValue := 2.5
	if got := pointerFloat64Value(&floatValue); got != 2.5 {
		t.Fatalf("pointerFloat64Value = %#v, want 2.5", got)
	}
	if pointerFloat64Value(nil) != nil {
		t.Fatal("nil float pointer should return nil")
	}
	intValue := int64(9)
	if got := pointerInt64Value(&intValue); got != int64(9) {
		t.Fatalf("pointerInt64Value = %#v, want 9", got)
	}
	if pointerInt64Value(nil) != nil {
		t.Fatal("nil int pointer should return nil")
	}
	if got := timePtrValue(&now); got != timeString(now) {
		t.Fatalf("timePtrValue = %#v, want %s", got, timeString(now))
	}
	zero := time.Time{}
	if timePtrValue(&zero) != nil || timePtrValue(nil) != nil {
		t.Fatal("nil and zero time pointers should return nil")
	}
}

func TestPayloadJSONRawAndRequestLogMetadataParsing(t *testing.T) {
	t.Parallel()

	arrayValue, ok := jsonRaw([]byte(`[{"id":"a"}]`)).([]any)
	if !ok || len(arrayValue) != 1 {
		t.Fatalf("jsonRaw array = %#v, want decoded slice", arrayValue)
	}

	for _, raw := range [][]byte{nil, []byte{}, []byte(`{bad`)} {
		got, ok := jsonRaw(raw).(map[string]any)
		if !ok || len(got) != 0 {
			t.Fatalf("jsonRaw(%q) = %#v, want empty object", raw, got)
		}
	}

	meta := requestLogMetadata([]byte(`{"headers":{"x-request-id":"abc"},"retry":2}`))
	headers, ok := meta["headers"].(map[string]any)
	if !ok || headers["x-request-id"] != "abc" {
		t.Fatalf("requestLogMetadata nested object = %#v", meta)
	}
	if meta["retry"] != float64(2) {
		t.Fatalf("requestLogMetadata number = %#v, want decoded float64", meta["retry"])
	}
	if got := requestLogMetadata([]byte(`["not-object"]`)); len(got) != 0 {
		t.Fatalf("requestLogMetadata array = %#v, want empty map", got)
	}
}

func TestPayloadIconURLMappingsNormalizeKnownProviders(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		siteType string
		want     string
	}{
		{name: "trim and lowercase", siteType: " OpenAI ", want: "/brand-icons/openai.png"},
		{name: "opencode go icon", siteType: "OPENCODE_GO", want: "/brand-icons/opencode.png"},
		{name: "codex oauth icon", siteType: "codex", want: "/oauth-icons/codex.svg"},
		{name: "antigravity oauth icon", siteType: "antigravity", want: "/oauth-icons/antigravity.png"},
		{name: "kimi moonshot icon", siteType: "KIMI_CODE", want: "/brand-icons/moonshot.png"},
		{name: "unknown", siteType: "unknown", want: ""},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := siteTypeIconURL(tc.siteType); got != tc.want {
				t.Fatalf("siteTypeIconURL(%q) = %q, want %q", tc.siteType, got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		provider string
		want     string
	}{
		{provider: "xai", want: "/brand-icons/xai.png"},
		{provider: "qwen", want: "/brand-icons/qwen.png"},
		{provider: "moonshot", want: "/brand-icons/moonshot.png"},
		{provider: "bytedance", want: "/brand-icons/bytedance-dark.png"},
		{provider: "vidu", want: "/brand-icons/vidu-dark.png"},
		{provider: "stepfun", want: "/brand-icons/stepfun-dark.png"},
		{provider: "alibaba", want: "/brand-icons/alibaba-dark.png"},
		{provider: "kuaishou", want: "/brand-icons/kuaishou.svg"},
		{provider: "baai", want: "/brand-icons/baai-dark.png"},
		{provider: "flux", want: "/brand-icons/flux-dark.png"},
		{provider: "hunyuan", want: "/brand-icons/hunyuan-dark.png"},
		{provider: "sensenova", want: "/brand-icons/sensenova-dark.png"},
		{provider: " openai ", want: ""},
	} {
		tc := tc
		t.Run("provider_"+tc.provider, func(t *testing.T) {
			t.Parallel()

			if got := providerIconURL(tc.provider); got != tc.want {
				t.Fatalf("providerIconURL(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

func TestQuotaHelpersPreferRawTokenQuotaAndHandleUnavailableShapes(t *testing.T) {
	t.Parallel()

	if usageHasQuotaData(nil) || usageHasQuotaData(map[string]any{"data": "invalid"}) {
		t.Fatal("usageHasQuotaData should reject nil and invalid data shapes")
	}
	if !usageHasQuotaData(map[string]any{"weekly": map[string]any{}}) {
		t.Fatal("usageHasQuotaData should accept weekly quota summaries")
	}
	if !tokenQuotaDataAvailable(map[string]any{"unlimited_quota": true}) {
		t.Fatal("unlimited quota should count as available token quota data")
	}
	if tokenQuotaDataAvailable(map[string]any{"name": "missing quota"}) {
		t.Fatal("missing remain/used and non-unlimited quota should be unavailable")
	}

	meta := map[string]any{
		"name":         "meta",
		"remain_quota": 1,
		"used_quota":   2,
	}
	raw := map[string]any{
		"name":            "raw",
		"remain_quota":    10,
		"used_quota":      3,
		"unlimited_quota": true,
		"model_limits":    []any{"gpt-5"},
	}
	usage := usageFromCredentialMeta(meta, raw)
	data := usage["data"].(map[string]any)
	want := map[string]any{
		"object":               "token_usage",
		"name":                 "raw",
		"total_granted":        13,
		"total_used":           3,
		"total_available":      10,
		"unlimited_quota":      true,
		"model_limits":         []any{"gpt-5"},
		"model_limits_enabled": nil,
		"expires_at":           nil,
	}
	if usage["success"] != true || !reflect.DeepEqual(data, want) {
		t.Fatalf("usageFromCredentialMeta data = %#v, want %#v", data, want)
	}
}

func TestSitePayloadPreservesNonObjectMetadataWithoutDerivedFields(t *testing.T) {
	t.Parallel()

	now := payloadTimestamp(10, 30)
	siteID := uuid.New()
	payload := sitePayload(store.Site{
		ID:              siteID,
		Name:            "Array Meta",
		Slug:            "array-meta",
		SiteType:        "newapi",
		BaseURL:         "https://newapi.example.com",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 3.5,
		Meta:            store.JSON(`[{"key":"value"}]`),
		CreatedAt:       now,
		UpdatedAt:       now,
	})

	if payload["id"] != siteID.String() || payload["icon_url"] != "/brand-icons/newapi.png" {
		t.Fatalf("site identity/icon payload = %#v", payload)
	}
	meta, ok := payload["meta"].([]any)
	if !ok || len(meta) != 1 {
		t.Fatalf("site meta = %#v, want decoded array", payload["meta"])
	}
	if _, ok := payload["proxy_id"]; ok {
		t.Fatalf("non-object meta should not expose proxy_id: %#v", payload)
	}
	if _, ok := payload["request_headers"]; ok {
		t.Fatalf("non-object meta should not expose request_headers: %#v", payload)
	}
	if _, ok := payload["oauth_account"]; ok {
		t.Fatalf("non-object meta should not expose oauth_account: %#v", payload)
	}
	if payload["created_at"] != timeString(now) || payload["updated_at"] != timeString(now) {
		t.Fatalf("site timestamps = %#v/%#v", payload["created_at"], payload["updated_at"])
	}
}

func TestSiteStateAndHealthPayloadsHandleMissingNullableData(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	now := payloadTimestamp(11, 0)
	statePayload := siteStatePayload(store.SiteState{
		SiteID:            siteID,
		SyncStatus:        "pending",
		ValidationMessage: sql.NullString{String: "ignored"},
		RawStatus:         store.JSON(`{bad`),
		UserSummary:       store.JSON(`[{"user":"a"}]`),
		UpdatedAt:         now,
	})

	if statePayload["message"] != nil || statePayload["failure_class"] != nil || statePayload["validation_ok"] != nil || statePayload["validation_message"] != nil {
		t.Fatalf("invalid nullable state fields should be nil: %#v", statePayload)
	}
	rawStatus, ok := statePayload["raw_status"].(map[string]any)
	if !ok || len(rawStatus) != 0 {
		t.Fatalf("invalid raw_status = %#v, want empty object", statePayload["raw_status"])
	}
	userSummary, ok := statePayload["user_summary"].([]any)
	if !ok || len(userSummary) != 1 {
		t.Fatalf("user_summary = %#v, want decoded array", statePayload["user_summary"])
	}

	healthPayload := siteHealthResultPayload(sitepkg.HealthResult{
		Site:  store.Site{ID: siteID, SiteType: "unknown", CreatedAt: now, UpdatedAt: now},
		State: store.SiteHealthState{SiteID: siteID, Status: "unknown"},
	})
	if _, ok := healthPayload["snapshot"]; ok {
		t.Fatalf("empty snapshot should be omitted: %#v", healthPayload)
	}
	if _, ok := healthPayload["ok"]; ok {
		t.Fatalf("ok should be omitted when no snapshot is present: %#v", healthPayload)
	}
	if healthPayload["meta"].(map[string]any)["recent_count"] != 0 {
		t.Fatalf("health meta = %#v, want zero recent count", healthPayload["meta"])
	}
	recent, ok := healthPayload["recent"].([]map[string]any)
	if !ok || len(recent) != 0 {
		t.Fatalf("recent = %#v, want empty payload slice", healthPayload["recent"])
	}

	snapshotID := uuid.New()
	modelID := uuid.New()
	snapshot := healthSnapshotPayload(store.HealthSnapshot{
		ID:          snapshotID,
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		Scope:       "model",
		Source:      "scheduled",
		Method:      "POST",
		Success:     true,
		CheckedAt:   now,
		Metadata:    store.JSON(`{bad`),
	})
	if snapshot["site_model_id"] != modelID.String() {
		t.Fatalf("snapshot site_model_id = %#v, want %s", snapshot["site_model_id"], modelID)
	}
	if snapshot["status_code"] != nil || snapshot["latency_ms"] != nil || snapshot["error_type"] != nil || snapshot["error_message"] != nil {
		t.Fatalf("invalid nullable snapshot fields should be nil: %#v", snapshot)
	}
	metadata, ok := snapshot["metadata"].(map[string]any)
	if !ok || len(metadata) != 0 {
		t.Fatalf("invalid snapshot metadata = %#v, want empty object", snapshot["metadata"])
	}
}

func TestSiteStatePayloadIncludesFailureClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  string
		message string
		want    string
	}{
		{
			name:    "quota",
			status:  "partial",
			message: `upstream returned 401: {"error":{"code":"api_key_daily_quota_exhausted"}}`,
			want:    "limited",
		},
		{
			name:    "invalid credential",
			status:  "failed",
			message: `upstream returned 401: {"error":{"code":"invalid_api_key"}}`,
			want:    "credential_invalid",
		},
		{
			name:    "unknown failure",
			status:  "failed",
			message: "upstream returned 404: endpoint not found",
			want:    "unknown",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := store.SiteState{
				SyncStatus:  tc.status,
				SyncMessage: sql.NullString{String: tc.message, Valid: true},
			}
			if got := statePayloadFailureClass(siteStatePayload(state)); got != tc.want {
				t.Fatalf("failure_class = %q, want %q", got, tc.want)
			}
		})
	}

	synced := siteStatePayload(store.SiteState{
		SyncStatus:  "synced",
		SyncMessage: sql.NullString{String: "fresh", Valid: true},
	})
	if synced["failure_class"] != nil {
		t.Fatalf("synced state failure_class = %#v, want nil", synced["failure_class"])
	}
}

func statePayloadFailureClass(payload map[string]any) string {
	value, _ := payload["failure_class"].(string)
	return value
}

func TestSiteModelAndPricingPayloadsDecodeOptionalFields(t *testing.T) {
	t.Parallel()

	now := payloadTimestamp(12, 0)
	siteID := uuid.New()
	modelID := uuid.New()
	modelPayloads := siteModelPayloads([]store.SiteModel{
		{
			ID:           modelID,
			SiteID:       siteID,
			UpstreamName: "claude-sonnet-4",
			DisplayName:  "Claude Sonnet 4",
			Capabilities: store.JSON(`["tools","vision"]`),
			Status:       "active",
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	})
	if len(modelPayloads) != 1 || modelPayloads[0]["id"] != modelID.String() {
		t.Fatalf("siteModelPayloads = %#v", modelPayloads)
	}
	capabilities, ok := modelPayloads[0]["capabilities"].([]any)
	if !ok || len(capabilities) != 2 {
		t.Fatalf("model capabilities = %#v, want decoded array", modelPayloads[0]["capabilities"])
	}
	if len(siteModelPayloads(nil)) != 0 {
		t.Fatal("nil model slice should produce an empty payload slice")
	}

	groupID := uuid.New()
	group := sitePricingGroupPayload(store.SitePricingGroup{
		ID:        groupID,
		SiteID:    siteID,
		GroupName: "vip",
		Ratio:     1.5,
		IsAuto:    false,
		Available: false,
		Raw:       store.JSON(`{bad`),
		CreatedAt: now,
		UpdatedAt: now,
	})
	if group["id"] != groupID.String() || group["display_name"] != nil || group["last_synced_at"] != nil {
		t.Fatalf("pricing group nullable fields = %#v", group)
	}
	raw, ok := group["raw"].(map[string]any)
	if !ok || len(raw) != 0 {
		t.Fatalf("pricing group raw = %#v, want empty object", group["raw"])
	}
	if len(sitePricingGroupPayloads(nil)) != 0 {
		t.Fatal("nil pricing group slice should produce an empty payload slice")
	}

	derived := siteModelPricingDerivedValues(store.SiteModelPricing{
		InputValue:           sql.NullFloat64{Float64: 10, Valid: true},
		OutputValue:          sql.NullFloat64{Float64: 20, Valid: true},
		PerRequestValue:      sql.NullFloat64{Float64: 0.02, Valid: true},
		CacheRatio:           sql.NullFloat64{Float64: 0.25, Valid: true},
		AudioRatio:           sql.NullFloat64{Float64: 2, Valid: true},
		AudioCompletionRatio: sql.NullFloat64{},
	})
	if derived.CacheInputValue != 2.5 {
		t.Fatalf("cache input derived value = %#v, want 2.5", derived.CacheInputValue)
	}
	if derived.CreateCacheInputValue != nil || derived.ImageInputValue != nil {
		t.Fatalf("missing ratios should produce nil derived values: %#v", derived)
	}
	if derived.AudioOutputValue != float64(20) {
		t.Fatalf("audio output value with missing completion ratio should default completion to 1 (input*audio_ratio): %#v", derived.AudioOutputValue)
	}
	calculation := derived.Calculation
	if calculation["input"].(map[string]any)["value"] != float64(10) || calculation["output"].(map[string]any)["value"] != float64(20) {
		t.Fatalf("calculation base values = %#v", calculation)
	}
	if calculation["audio_output"].(map[string]any)["value"] != float64(20) {
		t.Fatalf("audio_output calculation should default completion ratio to 1 when invalid: %#v", calculation["audio_output"])
	}
}

func TestPayloadCollectionsAndNumbersNormalizeInputs(t *testing.T) {
	t.Parallel()

	names := siteModelNames([]store.SiteModel{
		{DisplayName: "  Display Name  ", UpstreamName: "upstream-a"},
		{DisplayName: "\t", UpstreamName: "  upstream-b  "},
		{DisplayName: " ", UpstreamName: " "},
	})
	if !reflect.DeepEqual(names, []string{"Display Name", "upstream-b"}) {
		t.Fatalf("siteModelNames = %#v", names)
	}

	if got := apiKeySummaryModelIDs(nil); len(got) != 0 {
		t.Fatalf("nil summary models = %#v, want empty", got)
	}
	if got := apiKeySummaryModelIDs(map[string]any{"data": "not-list"}); len(got) != 0 {
		t.Fatalf("non-list summary models = %#v, want empty", got)
	}
	ids := apiKeySummaryModelIDs(map[string]any{"data": []any{
		map[string]any{"id": " model-a "},
		map[string]any{"id": json.Number("12")},
		map[string]any{"name": "missing-id"},
	}})
	if !reflect.DeepEqual(ids, []string{" model-a "}) {
		t.Fatalf("summary model ids = %#v", ids)
	}

	set := stringSetFromAny([]string{" a ", "", "b", "a"})
	if _, ok := set["a"]; !ok {
		t.Fatalf("string set missing a: %#v", set)
	}
	if _, ok := set["b"]; !ok || len(set) != 2 {
		t.Fatalf("string set = %#v, want a and b only", set)
	}
	anySet := stringSetFromAny([]any{" x ", 12, "y"})
	if _, ok := anySet["x"]; !ok {
		t.Fatalf("any set missing x: %#v", anySet)
	}
	if _, ok := anySet["y"]; !ok || len(anySet) != 2 {
		t.Fatalf("any set = %#v, want x and y only", anySet)
	}
	if got := stringSetFromAny("unsupported"); len(got) != 0 {
		t.Fatalf("unsupported set source = %#v, want empty", got)
	}

	if !metaValuesEqual(int64(3), json.Number("3")) {
		t.Fatal("numeric metadata values should compare equal across numeric types")
	}
	if metaValuesEqual(json.Number("bad"), 0) {
		t.Fatal("invalid json.Number should not compare equal to zero")
	}
	if !metaValuesEqual([]string{"a"}, []string{"a"}) {
		t.Fatal("non-numeric metadata values should fall back to DeepEqual")
	}

	for _, tc := range []struct {
		value any
		want  float64
		ok    bool
	}{
		{value: int(1), want: 1, ok: true},
		{value: int64(2), want: 2, ok: true},
		{value: float64(3.5), want: 3.5, ok: true},
		{value: json.Number("4.25"), want: 4.25, ok: true},
		{value: json.Number("bad"), ok: false},
		{value: "5", ok: false},
	} {
		got, ok := numberAsFloat(tc.value)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("numberAsFloat(%#v) = %v %v, want %v %v", tc.value, got, ok, tc.want, tc.ok)
		}
	}

	headers := requestHeadersFromInput([]siteRequestHeader{
		{Key: " X-Test ", Value: "one"},
		{Key: "X-Test", Value: "two"},
		{Key: " ", Value: "ignored"},
		{Key: "X-Empty", Value: ""},
	})
	if len(headers) != 2 || headers["X-Test"] != "two" || headers["X-Empty"] != "" {
		t.Fatalf("requestHeadersFromInput = %#v", headers)
	}
}

func payloadTimestamp(hour int, minute int) time.Time {
	return time.Date(2026, 6, 22, hour, minute, 0, 0, time.UTC)
}
