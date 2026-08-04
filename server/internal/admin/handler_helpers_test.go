package admin

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
)

func TestSiteGatewayRequestToConfigPreservesFields(t *testing.T) {
	t.Parallel()

	if (*siteGatewayRequest)(nil).toSiteGatewayConfig() != nil {
		t.Fatal("nil gateway request should produce nil config")
	}

	timeout := 30
	impersonateCodex := true
	req := &siteGatewayRequest{
		RequestTimeoutMS:               &timeout,
		ResponsesToolPolicy:            "compatibility",
		DisabledResponsesTools:         []string{"image_generation"},
		ResponsesImageGenerationPolicy: "auto",
		ImpersonateCodexClient:         &impersonateCodex,
	}
	cfg := req.toSiteGatewayConfig()
	if cfg == nil || cfg.RequestTimeoutMS == nil || *cfg.RequestTimeoutMS != timeout {
		t.Fatalf("request timeout was not preserved: %#v", cfg)
	}
	if cfg.ResponsesToolPolicy != "compatibility" || len(cfg.DisabledResponsesTools) != 1 || cfg.DisabledResponsesTools[0] != "image_generation" {
		t.Fatalf("responses tool config was not preserved: %#v", cfg)
	}
	if cfg.ImpersonateCodexClient == nil || *cfg.ImpersonateCodexClient != true {
		t.Fatalf("impersonation flag was not preserved: %#v", cfg)
	}
}

func TestRouteUUIDParamHelpers(t *testing.T) {
	t.Parallel()

	type paramCase struct {
		name string
		key  string
		fn   func(http.ResponseWriter, *http.Request) (uuid.UUID, bool)
		code string
	}

	for _, tc := range []paramCase{
		{name: "model", key: "modelID", fn: modelIDParam, code: "invalid_model_id"},
		{name: "site_model", key: "siteModelID", fn: siteModelIDParam, code: "invalid_site_model_id"},
		{name: "alias", key: "aliasID", fn: aliasIDParam, code: "invalid_alias_id"},
		{name: "api_key", key: "apiKeyID", fn: apiKeyIDParam, code: "invalid_api_key_id"},
		{name: "site_group", key: "siteGroupID", fn: siteGroupIDParam, code: "invalid_site_group_id"},
		{name: "connection", key: "connectionID", fn: connectionIDParam, code: "invalid_connection_id"},
		{name: "request_log", key: "requestLogID", fn: requestLogIDParam, code: "invalid_request_log_id"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			id := uuid.New()
			req := adminRequestWithRouteParam(http.MethodGet, "/", "", tc.key, id.String())
			rec := adminRecorder()
			got, ok := tc.fn(rec, req)
			if !ok || got != id {
				t.Fatalf("valid %s param = %s ok=%v, want %s true", tc.key, got, ok, id)
			}

			req = adminRequestWithRouteParam(http.MethodGet, "/", "", tc.key, "bad-id")
			rec = adminRecorder()
			_, ok = tc.fn(rec, req)
			adminAssertParserError(t, rec, ok, tc.code)
		})
	}
}

func TestRouteSelectionRequestValidation(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	query, err := (routeSelectionRequest{
		ModelKey:            " gpt-5 ",
		Debug:               true,
		ExcludeSiteIDs:      []uuid.UUID{siteID},
		ExcludeSiteModelIDs: []uuid.UUID{siteModelID},
		Limit:               3,
		FailoverLimit:       2,
	}).toCandidateQuery()
	if err != nil {
		t.Fatalf("toCandidateQuery returned error: %v", err)
	}
	if query.ModelKey != " gpt-5 " || !query.Debug || query.Limit != 3 || query.FailoverLimit != 2 {
		t.Fatalf("unexpected query fields: %#v", query)
	}
	if len(query.ExcludeSiteIDs) != 1 || query.ExcludeSiteIDs[0] != siteID || len(query.ExcludeSiteModelIDs) != 1 || query.ExcludeSiteModelIDs[0] != siteModelID {
		t.Fatalf("unexpected exclusion ids: %#v", query)
	}

	for _, req := range []routeSelectionRequest{
		{},
		{ModelKey: "gpt-5", Limit: -1},
		{ModelKey: "gpt-5", FailoverLimit: -1},
	} {
		if _, err := req.toCandidateQuery(); err == nil {
			t.Fatalf("expected invalid route selection request to fail: %#v", req)
		}
	}
}

func TestOptionalQueryAndPointerHelpers(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	now := time.Date(2026, 6, 22, 10, 30, 0, 123_000_000, time.UTC)
	req := adminTestRequest(http.MethodGet, "/api/v1/requests?site_id="+id.String()+"&created_from="+now.Format(time.RFC3339Nano), "")
	rec := adminRecorder()

	gotID, ok := optionalUUIDQuery(rec, req, "site_id")
	if !ok || gotID == nil || *gotID != id {
		t.Fatalf("optional UUID = %v ok=%v, want %s true", gotID, ok, id)
	}
	gotTime, ok := optionalTimeQuery(rec, req, "created_from")
	if !ok || gotTime == nil || !gotTime.Equal(now) {
		t.Fatalf("optional time = %v ok=%v, want %s true", gotTime, ok, now)
	}
	if got := uuidPtrString(gotID); got != id.String() {
		t.Fatalf("uuidPtrString = %#v, want %s", got, id)
	}
	if got := timePtrString(gotTime); got != adminTimeZone().Format(now, time.RFC3339Nano) {
		t.Fatalf("timePtrString = %#v", got)
	}

	emptyReq := adminTestRequest(http.MethodGet, "/api/v1/requests", "")
	if got, ok := optionalUUIDQuery(adminRecorder(), emptyReq, "site_id"); !ok || got != nil {
		t.Fatalf("empty optional UUID = %v ok=%v, want nil true", got, ok)
	}
	if got, ok := optionalTimeQuery(adminRecorder(), emptyReq, "created_from"); !ok || got != nil {
		t.Fatalf("empty optional time = %v ok=%v, want nil true", got, ok)
	}
	if uuidPtrString(nil) != nil || timePtrString(nil) != nil {
		t.Fatal("nil pointer helpers should return nil")
	}
}

func TestOptionalQueryHelpersRejectInvalidValues(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/requests?site_id=bad-id", "")
	rec := adminRecorder()
	_, ok := optionalUUIDQuery(rec, req, "site_id")
	adminAssertParserError(t, rec, ok, "invalid_site_id")

	req = adminTestRequest(http.MethodGet, "/api/v1/requests?created_from=tomorrow", "")
	rec = adminRecorder()
	_, ok = optionalTimeQuery(rec, req, "created_from")
	adminAssertParserError(t, rec, ok, "invalid_created_from")
}

func TestScalarPayloadHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	if timeValue(time.Time{}) != nil {
		t.Fatal("zero timeValue should be nil")
	}
	if got := timeValue(now); got != timeString(now) {
		t.Fatalf("timeValue = %#v, want %s", got, timeString(now))
	}
	if got := emptyStringAsNil(" value "); got != " value " {
		t.Fatalf("emptyStringAsNil should preserve non-empty string, got %#v", got)
	}
	if emptyStringAsNil(" ") != nil {
		t.Fatal("blank string should become nil")
	}

	base := sql.NullFloat64{Float64: 2, Valid: true}
	ratioA := sql.NullFloat64{Float64: 3, Valid: true}
	ratioB := sql.NullFloat64{Float64: 4, Valid: true}
	if got := scaledNullFloat(base, ratioA); got != float64(6) {
		t.Fatalf("scaledNullFloat = %#v, want 6", got)
	}
	if got := chainedScaledNullFloat(base, ratioA, ratioB); got != float64(24) {
		t.Fatalf("chainedScaledNullFloat = %#v, want 24", got)
	}
	if scaledNullFloat(sql.NullFloat64{}, ratioA) != nil || chainedScaledNullFloat(base, sql.NullFloat64{}, ratioB) != nil {
		t.Fatal("invalid scale inputs should return nil")
	}
}

func TestAdminTimeZoneOverrideAndJSONHelpers(t *testing.T) {
	shanghai := config.LoadTimeZone("Asia/Shanghai")
	previous := adminTimeZone()
	setAdminTimeZone(shanghai)
	t.Cleanup(func() { setAdminTimeZone(previous) })

	now := time.Date(2026, 6, 22, 2, 30, 0, 0, time.UTC)
	if got := timeString(now); got != "2026-06-22T10:30:00+08:00" {
		t.Fatalf("timeString with admin timezone = %q", got)
	}
	if got := adminTimeZone(config.LoadTimeZone("UTC")).Name; got != "UTC" {
		t.Fatalf("explicit admin timezone = %q, want UTC", got)
	}

	if got := metadataString(map[string]any{"name": " value "}, "name"); got != " value " {
		t.Fatalf("metadataString = %#v", got)
	}
	if metadataString(map[string]any{"name": " "}, "name") != nil {
		t.Fatal("blank metadata string should be nil")
	}
	if got := requestLogMetadata([]byte(`{"error":"boom"}`)); got["error"] != "boom" {
		t.Fatalf("request log metadata = %#v", got)
	}
	if len(requestLogMetadata([]byte(`[]`))) != 0 {
		t.Fatal("non-object request log metadata should return empty map")
	}
}

func TestNewHandlerUsesExplicitTimeZoneAndSetAdminTimeZoneFallback(t *testing.T) {
	previous := adminTimeZone()
	t.Cleanup(func() { setAdminTimeZone(previous) })

	utc := config.LoadTimeZone("UTC")
	handler := NewHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, utc)
	if handler.timeZone.Name != "UTC" {
		t.Fatalf("handler timezone = %q, want UTC", handler.timeZone.Name)
	}
	if got := adminTimeZone().Name; got != "UTC" {
		t.Fatalf("admin timezone = %q, want UTC", got)
	}

	setAdminTimeZone(config.TimeZone{})
	if adminTimeZone().Location == nil || adminTimeZone().Name == "" {
		t.Fatalf("setAdminTimeZone fallback produced invalid timezone: %#v", adminTimeZone())
	}
}
