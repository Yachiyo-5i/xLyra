package admin

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestDecodeJSONRejectsEmptyBody(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminTestRequest(http.MethodPost, "/api/v1/admin", "")
	rec := adminRecorder()

	var payload map[string]any
	ok := handler.decodeJSON(rec, req, &payload)
	adminAssertParserError(t, rec, ok, "invalid_json")
}

func TestParseUUIDListSkipsBlankValuesAndPreservesOrder(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")
	rec := adminRecorder()

	got, ok := parseUUIDList(rec, req, []string{" ", first.String(), "\t", second.String()}, "invalid_item_id", "item_id")

	adminRequireParserOK(t, rec, ok, "UUID list")
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("parsed UUIDs = %#v, want [%s %s]", got, first, second)
	}
}

func TestBulkModelPriceInputTrimsIDsAndKeepsPricing(t *testing.T) {
	t.Parallel()

	canonicalID := uuid.New()
	siteModelID := uuid.New()
	inputValue := 0.25
	outputValue := 1.5
	rec := adminRecorder()
	req := adminTestRequest(http.MethodPut, "/api/v1/model-prices/bulk", "")

	input, ok := (bulkModelPriceRequest{
		CanonicalModelID: " " + canonicalID.String() + " ",
		SiteModelIDs:     []string{"\t" + siteModelID.String() + "\n"},
		modelPriceRequest: modelPriceRequest{
			GroupName:   "default",
			BillingType: "tokens",
			Currency:    "USD",
			InputValue:  &inputValue,
			OutputValue: &outputValue,
			ManualNote:  "manual override",
		},
	}).toInput(rec, req)

	adminRequireParserOK(t, rec, ok, "bulk model price input")
	if input.CanonicalModelID != canonicalID {
		t.Fatalf("canonical model id = %s, want %s", input.CanonicalModelID, canonicalID)
	}
	if len(input.SiteModelIDs) != 1 || input.SiteModelIDs[0] != siteModelID {
		t.Fatalf("site model ids = %#v, want [%s]", input.SiteModelIDs, siteModelID)
	}
	if input.GroupName != "default" || input.BillingType != "tokens" || input.Currency != "USD" || input.ManualNote != "manual override" {
		t.Fatalf("pricing scalar fields were not preserved: %#v", input)
	}
	if input.InputValue == nil || *input.InputValue != inputValue || input.OutputValue == nil || *input.OutputValue != outputValue {
		t.Fatalf("pricing values were not preserved: %#v", input)
	}
}
