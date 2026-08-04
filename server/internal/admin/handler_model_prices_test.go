package admin

import (
	"database/sql"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestListModelPricesRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListModelPrices, adminTestRequest(http.MethodGet, "/api/v1/model-prices", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}

func TestUpdateSiteModelPricingRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminRequestWithRouteParam(http.MethodPut, "/api/v1/model-prices/site-model-id", "", "siteModelID", uuid.New().String())
	rec := adminPerform(handler.UpdateSiteModelPricing, req)

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}

func TestUpdateSiteModelPricingRejectsInvalidSiteModelID(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPut, "/api/v1/model-prices/site-model-id", "", "siteModelID", "bad-id")
	rec := adminPerform(handler.UpdateSiteModelPricing, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_model_id")
}

func TestUpdateSiteModelPricingRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPut, "/api/v1/model-prices/site-model-id", `{"group_name":`, "siteModelID", uuid.New().String())
	rec := adminPerform(handler.UpdateSiteModelPricing, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}

func TestBulkUpdateModelPricesRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.BulkUpdateModelPrices, adminTestRequest(http.MethodPut, "/api/v1/model-prices/bulk", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}

func TestBulkUpdateModelPricesRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	rec := adminPerform(handler.BulkUpdateModelPrices, adminTestRequest(http.MethodPut, "/api/v1/model-prices/bulk", `{"canonical_model_id":`))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}

func TestModelPriceFiltersRejectInvalidSiteID(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/model-prices?site_id=not-a-uuid", "")
	rec := adminRecorder()

	_, ok := modelPriceFiltersFromRequest(rec, req)

	adminAssertParserError(t, rec, ok, "invalid_site_id")
}

func TestModelPriceRequestToInputPreservesFields(t *testing.T) {
	t.Parallel()

	inputValue := 1.25
	outputValue := 2.5
	cacheInputValue := 0.25
	perRequestValue := 0.01
	input := (modelPriceRequest{
		GroupName:       "default",
		BillingType:     "tokens",
		Currency:        "USD",
		InputValue:      &inputValue,
		OutputValue:     &outputValue,
		CacheInputValue: &cacheInputValue,
		PerRequestValue: &perRequestValue,
		ManualNote:      "manual note",
	}).toInput()

	if input.GroupName != "default" || input.BillingType != "tokens" || input.Currency != "USD" || input.ManualNote != "manual note" {
		t.Fatalf("scalar fields were not preserved: %#v", input)
	}
	if input.InputValue != &inputValue || input.OutputValue != &outputValue || input.CacheInputValue != &cacheInputValue || input.PerRequestValue != &perRequestValue {
		t.Fatalf("pointer fields were not preserved: %#v", input)
	}
}

func TestModelPriceItemPayloadBuildsExplicitOutput(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	canonicalID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	item := site.ModelPriceItem{
		Site: store.Site{
			ID:       siteID,
			Name:     "Primary Site",
			Slug:     "primary-site",
			SiteType: "openai",
			Enabled:  true,
		},
		Model: store.SiteModel{
			ID:           siteModelID,
			CanonicalID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
			UpstreamName: "gpt-5-mini",
			DisplayName:  "GPT-5 mini",
			Status:       "available",
		},
		Canonical: &store.CanonicalModel{
			ModelKey:    "openai/gpt-5-mini",
			DisplayName: "GPT-5 mini canonical",
			Provider:    "openai",
			Category:    "chat",
		},
		Editable:      true,
		EditReason:    "   ",
		PricingStatus: "missing",
	}

	got := modelPriceItemPayload(item)
	want := map[string]any{
		"site_model_id": siteModelID.String(),
		"pricing_id":    nil,
		"site": map[string]any{
			"id":      siteID.String(),
			"name":    "Primary Site",
			"slug":    "primary-site",
			"type":    "openai",
			"enabled": true,
		},
		"model": map[string]any{
			"site_model_id":          siteModelID.String(),
			"upstream_model_name":    "gpt-5-mini",
			"display_name":           "GPT-5 mini",
			"status":                 "available",
			"canonical_model_id":     canonicalID.String(),
			"canonical_model_key":    "openai/gpt-5-mini",
			"canonical_display_name": "GPT-5 mini canonical",
			"provider":               "openai",
			"category":               "chat",
		},
		"pricing":        nil,
		"editable":       true,
		"edit_reason":    nil,
		"pricing_status": "missing",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestModelPriceItemPayloadBuildsNilCanonicalOutput(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteModelID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	item := site.ModelPriceItem{
		Site: store.Site{
			ID:       siteID,
			Name:     "Readonly Site",
			Slug:     "readonly-site",
			SiteType: "newapi",
		},
		Model: store.SiteModel{
			ID:           siteModelID,
			UpstreamName: "vendor-model",
			DisplayName:  "Vendor Model",
			Status:       "available",
		},
		Editable:      false,
		EditReason:    "newapi pricing is synced from upstream",
		PricingStatus: "missing",
	}

	got := modelPriceItemPayload(item)
	want := map[string]any{
		"site_model_id": siteModelID.String(),
		"pricing_id":    nil,
		"site": map[string]any{
			"id":      siteID.String(),
			"name":    "Readonly Site",
			"slug":    "readonly-site",
			"type":    "newapi",
			"enabled": false,
		},
		"model": map[string]any{
			"site_model_id":          siteModelID.String(),
			"upstream_model_name":    "vendor-model",
			"display_name":           "Vendor Model",
			"status":                 "available",
			"canonical_model_id":     nil,
			"canonical_model_key":    nil,
			"canonical_display_name": nil,
			"provider":               nil,
			"category":               nil,
		},
		"pricing":        nil,
		"editable":       false,
		"edit_reason":    "newapi pricing is synced from upstream",
		"pricing_status": "missing",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestModelPriceSitePayloadBuildsExplicitOutput(t *testing.T) {
	t.Parallel()

	siteID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	item := site.ModelPriceItem{
		Site: store.Site{
			ID:       siteID,
			Name:     "Primary Site",
			Slug:     "primary-site",
			SiteType: "openai",
			Enabled:  true,
		},
	}

	got := modelPriceSitePayload(item)
	want := map[string]any{
		"id":      siteID.String(),
		"name":    "Primary Site",
		"slug":    "primary-site",
		"type":    "openai",
		"enabled": true,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestModelPricePricingPayloadBuildsExplicitOutput(t *testing.T) {
	previous := adminTimeZone()
	setAdminTimeZone(config.LoadTimeZone("UTC"))
	t.Cleanup(func() { setAdminTimeZone(previous) })

	pricingID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	item := site.ModelPriceItem{
		Pricing: &store.SiteModelPricing{
			ID:               pricingID,
			GroupName:        "vip",
			BillingType:      "tokens",
			QuotaType:        0,
			Currency:         "USD",
			GroupRatio:       1.25,
			InputValue:       sql.NullFloat64{Float64: 8, Valid: true},
			OutputValue:      sql.NullFloat64{Float64: 16, Valid: true},
			CacheRatio:       sql.NullFloat64{Float64: 0.25, Valid: true},
			CreateCacheRatio: sql.NullFloat64{Float64: 0.5, Valid: true},
			PricingSource:    "manual",
			ManualOverride:   true,
			ManualUpdatedAt:  sql.NullTime{Time: time.Date(2026, 6, 22, 1, 2, 3, 0, time.UTC), Valid: true},
			ManualNote:       sql.NullString{String: "manual note", Valid: true},
			Available:        true,
			LastSyncedAt:     sql.NullTime{Time: time.Date(2026, 6, 22, 2, 3, 4, 0, time.UTC), Valid: true},
			CreatedAt:        time.Date(2026, 6, 20, 11, 22, 33, 0, time.UTC),
			UpdatedAt:        time.Date(2026, 6, 21, 12, 23, 34, 0, time.UTC),
		},
	}

	got := modelPricePricingPayload(item)
	want := map[string]any{
		"id":                          pricingID.String(),
		"group_name":                  "vip",
		"billing_type":                "tokens",
		"quota_type":                  0,
		"currency":                    "USD",
		"group_ratio":                 1.25,
		"input_value":                 8.0,
		"output_value":                16.0,
		"cache_ratio":                 0.25,
		"cache_input_value":           2.0,
		"create_cache_ratio":          0.5,
		"create_cache_input_value":    4.0,
		"create_cache_1h_ratio":       nil,
		"create_cache_1h_input_value": nil,
		"audio_output_value":          nil,
		"per_request_value":           nil,
		"pricing_source":              "manual",
		"manual_override":             true,
		"manual_updated_at":           "2026-06-22T01:02:03Z",
		"manual_note":                 "manual note",
		"available":                   true,
		"last_synced_at":              "2026-06-22T02:03:04Z",
		"created_at":                  "2026-06-20T11:22:33Z",
		"updated_at":                  "2026-06-21T12:23:34Z",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestModelPricePricingPayloadReturnsNilWhenMissing(t *testing.T) {
	t.Parallel()

	if got := modelPricePricingPayload(site.ModelPriceItem{}); got != nil {
		t.Fatalf("payload = %#v, want nil", got)
	}
}

func TestModelPricePricingIDValueReturnsPricingIDOrNil(t *testing.T) {
	t.Parallel()

	pricingID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	item := site.ModelPriceItem{Pricing: &store.SiteModelPricing{ID: pricingID}}

	if got := pricingIDValue(item); got != pricingID.String() {
		t.Fatalf("pricing ID = %#v, want %q", got, pricingID.String())
	}
	if got := pricingIDValue(site.ModelPriceItem{}); got != nil {
		t.Fatalf("missing pricing ID = %#v, want nil", got)
	}
}

func TestModelPriceEmptyToNilTrimsOnlyForBlankDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  any
	}{
		{name: "empty", value: "", want: nil},
		{name: "whitespace", value: " \t\n ", want: nil},
		{name: "keeps original nonblank", value: " manual note ", want: " manual note "},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := modelPriceEmptyToNil(tt.value); got != tt.want {
				t.Fatalf("emptyToNil(%q) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}
}

func TestBulkModelPriceInputRejectsInvalidSiteModelID(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPut, "/api/v1/model-prices/bulk", "")
	rec := adminRecorder()
	payload := bulkModelPriceRequest{
		CanonicalModelID: "11111111-1111-1111-1111-111111111111",
		SiteModelIDs:     []string{"22222222-2222-2222-2222-222222222222", "bad-id"},
		modelPriceRequest: modelPriceRequest{
			GroupName:   "default",
			BillingType: "token",
			Currency:    "USD",
		},
	}

	_, ok := payload.toInput(rec, req)

	adminAssertParserError(t, rec, ok, "invalid_site_model_id")
}

func TestBulkModelPriceInputNormalizesValidUUIDs(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPut, "/api/v1/model-prices/bulk", "")
	rec := adminRecorder()
	inputValue := 1.25
	outputValue := 2.5
	payload := bulkModelPriceRequest{
		CanonicalModelID: "11111111-1111-1111-1111-111111111111",
		SiteModelIDs:     []string{" 22222222-2222-2222-2222-222222222222 "},
		modelPriceRequest: modelPriceRequest{
			GroupName:   "default",
			BillingType: "token",
			Currency:    "USD",
			InputValue:  &inputValue,
			OutputValue: &outputValue,
		},
	}

	input, ok := payload.toInput(rec, req)

	adminRequireParserOK(t, rec, ok, "bulk model price input")
	if input.CanonicalModelID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected canonical model id: %s", input.CanonicalModelID)
	}
	if len(input.SiteModelIDs) != 1 || input.SiteModelIDs[0].String() != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("unexpected site model ids: %#v", input.SiteModelIDs)
	}
	if input.GroupName != "default" || input.BillingType != "token" || input.Currency != "USD" {
		t.Fatalf("unexpected model price fields: %#v", input.ModelPriceInput)
	}
	if input.InputValue == nil || *input.InputValue != inputValue || input.OutputValue == nil || *input.OutputValue != outputValue {
		t.Fatalf("unexpected price values: %#v", input.ModelPriceInput)
	}
}
