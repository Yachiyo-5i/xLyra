package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/config"
)

func TestParseAnalyticsUsageParamsContributionFlag(t *testing.T) {
	t.Parallel()

	handler := Handler{timeZone: config.LoadTimeZone("UTC")}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?include_contributions=false", nil)
	params, ok := handler.parseAnalyticsUsageParams(recorder, request)
	if !ok || params.IncludeContributions == nil || *params.IncludeContributions {
		t.Fatalf("parseAnalyticsUsageParams = %#v, %t", params, ok)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/v1/analytics/usage?include_contributions=invalid", nil)
	if _, ok := handler.parseAnalyticsUsageParams(recorder, request); ok || recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid include_contributions status = %d, ok = %t", recorder.Code, ok)
	}
}
