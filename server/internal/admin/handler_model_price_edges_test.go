package admin

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/site"
)

func TestWriteModelPriceErrorMapsKnownErrors(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminTestRequest(http.MethodPatch, "/api/v1/model-prices/id", "")
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "readonly", err: site.ErrModelPriceReadonly, status: http.StatusForbidden, code: "pricing_readonly"},
		{name: "canonical mismatch", err: site.ErrModelPriceCanonicalMismatch, status: http.StatusBadRequest, code: "canonical_model_mismatch"},
		{name: "invalid", err: site.ErrModelPriceInvalid, status: http.StatusBadRequest, code: "invalid_model_price"},
		{name: "wrapped invalid", err: errors.Join(errors.New("context"), site.ErrModelPriceInvalid), status: http.StatusBadRequest, code: "invalid_model_price"},
		{name: "unknown", err: errors.New("boom"), status: http.StatusInternalServerError, code: "model_price_update_failed"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := adminRecorder()
			handler.writeModelPriceError(rec, req, tt.err)

			assertAdminErrorCode(t, rec, tt.status, tt.code)
		})
	}
}

func TestModelPriceFiltersFromRequestNormalizesValidFilters(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	canonicalModelID := uuid.New()
	req := adminTestRequest(http.MethodGet, "/api/v1/model-prices?q=%20gpt%20&site_type=%20openai%20&provider=%20OpenAI%20&model_key=%20gpt-5%20&billing_type=%20TOKENS%20&pricing_status=%20MISSING%20&site_id="+siteID.String()+"&canonical_model_id="+canonicalModelID.String(), "")
	rec := adminRecorder()

	filters, ok := modelPriceFiltersFromRequest(rec, req)

	adminRequireParserOK(t, rec, ok, "model price filters")
	if filters.Q != "gpt" || filters.SiteType != "openai" || filters.Provider != "OpenAI" || filters.ModelKey != "gpt-5" {
		t.Fatalf("unexpected string filters: %#v", filters)
	}
	if filters.BillingType != "tokens" || filters.PricingStatus != "missing" {
		t.Fatalf("unexpected normalized filters: %#v", filters)
	}
	if filters.SiteID != siteID || filters.CanonicalModelID != canonicalModelID {
		t.Fatalf("unexpected UUID filters: %#v", filters)
	}
}

func TestModelPriceFiltersFromRequestRejectsInvalidCanonicalModelID(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/model-prices?canonical_model_id=bad-id", "")
	rec := adminRecorder()

	_, ok := modelPriceFiltersFromRequest(rec, req)

	adminAssertParserError(t, rec, ok, "invalid_canonical_model_id")
}

func TestListModelPricesRejectsInvalidCanonicalModelIDBeforeServiceQuery(t *testing.T) {
	t.Parallel()

	handler := Handler{sites: adminSiteService()}
	rec := adminPerform(handler.ListModelPrices, adminTestRequest(http.MethodGet, "/api/v1/model-prices?canonical_model_id=bad-id", ""))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_canonical_model_id")
}

func TestBulkUpdateModelPricesRejectsInvalidCanonicalModelIDBeforeServiceQuery(t *testing.T) {
	t.Parallel()

	handler := Handler{sites: adminSiteService()}
	rec := adminPerform(handler.BulkUpdateModelPrices, adminTestRequest(http.MethodPut, "/api/v1/model-prices/bulk", `{"canonical_model_id":"bad-id","site_model_ids":[]}`))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_canonical_model_id")
}
