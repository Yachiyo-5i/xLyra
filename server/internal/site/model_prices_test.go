package site

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestNormalizeModelPriceInputAcceptsTokensWithInputOnly(t *testing.T) {
	t.Parallel()

	input := 0.27
	got, err := normalizeModelPriceInput(ModelPriceInput{
		GroupName:   " vip ",
		BillingType: " TOKENS ",
		Currency:    " eur ",
		InputValue:  &input,
	})
	if err != nil {
		t.Fatalf("expected tokens price to be valid: %v", err)
	}
	if got.GroupName != "vip" {
		t.Fatalf("expected trimmed group, got %q", got.GroupName)
	}
	if got.BillingType != "tokens" {
		t.Fatalf("expected normalized billing type, got %q", got.BillingType)
	}
	if got.Currency != "EUR" {
		t.Fatalf("expected normalized currency, got %q", got.Currency)
	}

	output := 0.42
	got, err = normalizeModelPriceInput(ModelPriceInput{BillingType: "tokens", OutputValue: &output})
	if err != nil {
		t.Fatalf("output-only tokens price should be valid: %v", err)
	}
	if got.InputValue != nil || got.OutputValue == nil || *got.OutputValue != output {
		t.Fatalf("output-only tokens price = %#v", got)
	}
}

func TestNormalizeModelPriceInputDefaultsGroupBillingAndCurrency(t *testing.T) {
	t.Parallel()

	input := 0.27
	got, err := normalizeModelPriceInput(ModelPriceInput{InputValue: &input})
	if err != nil {
		t.Fatalf("expected defaulted tokens price to be valid: %v", err)
	}
	if got.GroupName != "default" || got.BillingType != "tokens" || got.Currency != "USD" {
		t.Fatalf("unexpected normalized defaults: %#v", got)
	}
}

func TestNormalizeModelPriceInputRejectsTokensWithoutPrices(t *testing.T) {
	t.Parallel()

	_, err := normalizeModelPriceInput(ModelPriceInput{BillingType: "tokens"})
	if !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected invalid model price, got %v", err)
	}
}

func TestNormalizeModelPriceInputRejectsInvalidBillingAndNegativeValues(t *testing.T) {
	t.Parallel()

	input := -0.1
	if _, err := normalizeModelPriceInput(ModelPriceInput{BillingType: "usage", InputValue: &input}); !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected invalid billing type error, got %v", err)
	}
	if _, err := normalizeModelPriceInput(ModelPriceInput{BillingType: "tokens", InputValue: &input}); !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected negative value error, got %v", err)
	}
	output := 0.42
	cacheInput := -0.01
	if _, err := normalizeModelPriceInput(ModelPriceInput{
		BillingType:     "tokens",
		OutputValue:     &output,
		CacheInputValue: &cacheInput,
	}); !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected negative cache input value error, got %v", err)
	}
	perRequest := -0.01
	if _, err := normalizeModelPriceInput(ModelPriceInput{
		BillingType:     "per_request",
		PerRequestValue: &perRequest,
	}); !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected negative per-request value error, got %v", err)
	}
}

func TestNormalizeModelPriceInputAcceptsPerRequestAndClearsTokenPrices(t *testing.T) {
	t.Parallel()

	input := 1.2
	perRequest := 0.02
	got, err := normalizeModelPriceInput(ModelPriceInput{
		BillingType:     "fixed",
		InputValue:      &input,
		PerRequestValue: &perRequest,
	})
	if err != nil {
		t.Fatalf("expected per-request price to be valid: %v", err)
	}
	if got.BillingType != "per_request" {
		t.Fatalf("expected per_request billing type, got %q", got.BillingType)
	}
	if got.InputValue != nil {
		t.Fatalf("expected token prices to be cleared for per_request, got %#v", got.InputValue)
	}
}

func TestNormalizeModelPriceInputRejectsPerRequestWithoutPrice(t *testing.T) {
	t.Parallel()

	_, err := normalizeModelPriceInput(ModelPriceInput{BillingType: "per_request"})
	if !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected invalid per-request price, got %v", err)
	}
}

func TestNormalizeModelPriceInputRejectsNegativeAudioOutputValue(t *testing.T) {
	t.Parallel()

	input := 0.60
	audio := -1.0
	if _, err := normalizeModelPriceInput(ModelPriceInput{
		BillingType:      "tokens",
		InputValue:       &input,
		AudioOutputValue: &audio,
	}); !errors.Is(err, ErrModelPriceInvalid) {
		t.Fatalf("expected negative audio output value error, got %v", err)
	}
}

func TestNormalizeModelPriceInputRejectsAudioOutputValueWithoutPositiveInputValue(t *testing.T) {
	t.Parallel()

	audio := 1.0
	zero := 0.0
	for name, input := range map[string]*float64{
		"nil":  nil,
		"zero": &zero,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := normalizeModelPriceInput(ModelPriceInput{
				BillingType:      "tokens",
				InputValue:       input,
				AudioOutputValue: &audio,
			}); !errors.Is(err, ErrModelPriceInvalid) {
				t.Fatalf("expected missing positive input value error, got %v", err)
			}
		})
	}
}

func TestModelPriceStatusDistinguishesManualSyncedAndMissing(t *testing.T) {
	t.Parallel()

	synced := store.SiteModelPricing{
		Available:   true,
		BillingType: "tokens",
		InputValue:  sql.NullFloat64{Float64: 0.1, Valid: true},
	}
	if got := modelPriceStatus(&synced); got != "synced" {
		t.Fatalf("expected synced, got %q", got)
	}

	manual := synced
	manual.ManualOverride = true
	if got := modelPriceStatus(&manual); got != "manual" {
		t.Fatalf("expected manual, got %q", got)
	}

	missing := store.SiteModelPricing{Available: true, BillingType: "tokens"}
	if got := modelPriceStatus(&missing); got != "missing" {
		t.Fatalf("expected missing, got %q", got)
	}
}

func TestModelPriceCompleteRequiresAvailableSupportedPricingValues(t *testing.T) {
	t.Parallel()

	if modelPriceComplete(nil) {
		t.Fatal("nil pricing should be incomplete")
	}
	if modelPriceComplete(&store.SiteModelPricing{
		Available:   false,
		BillingType: "tokens",
		InputValue:  sql.NullFloat64{Float64: 0.1, Valid: true},
	}) {
		t.Fatal("unavailable pricing should be incomplete")
	}
	if !modelPriceComplete(&store.SiteModelPricing{
		Available:   true,
		BillingType: "tokens",
		OutputValue: sql.NullFloat64{Float64: 0, Valid: true},
	}) {
		t.Fatal("token pricing with zero output value should be complete")
	}

	pricing := store.SiteModelPricing{
		Available:       true,
		BillingType:     "fixed",
		PerRequestValue: sql.NullFloat64{Float64: 0, Valid: true},
	}
	if !modelPriceComplete(&pricing) {
		t.Fatal("expected fixed/per-request pricing with zero value to be complete")
	}
}

func TestModelPriceItemMarksNewAPIReadonly(t *testing.T) {
	t.Parallel()

	pricing := store.SiteModelPricing{
		Available:      true,
		BillingType:    "tokens",
		InputValue:     sql.NullFloat64{Float64: 0.1, Valid: true},
		ManualOverride: true,
	}
	item := modelPriceItem(
		store.Site{SiteType: "newapi"},
		store.SiteModel{UpstreamName: "gpt-test"},
		nil,
		&pricing,
	)

	if item.Editable {
		t.Fatal("expected newapi pricing to be readonly")
	}
	if item.EditReason == "" {
		t.Fatal("expected readonly reason")
	}
	if item.PricingStatus != "manual" {
		t.Fatalf("pricing status = %q, want manual", item.PricingStatus)
	}
}

func TestModelPriceListAddCountsStatuses(t *testing.T) {
	t.Parallel()

	list := ModelPriceList{}
	list.add(ModelPriceItem{PricingStatus: "missing"})
	list.add(ModelPriceItem{PricingStatus: "manual"})
	list.add(ModelPriceItem{PricingStatus: "synced"})

	if list.Count != 0 {
		t.Fatalf("raw add should not set filtered Count, got %d", list.Count)
	}
	if len(list.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(list.Items))
	}
	if list.MissingCount != 1 || list.PricedCount != 2 || list.ManualCount != 1 {
		t.Fatalf("unexpected counts: %+v", list)
	}
}

func TestModelPriceDisplayAndNullableHelpers(t *testing.T) {
	t.Parallel()

	canonical := store.CanonicalModel{ModelKey: "openai/gpt-5"}
	item := ModelPriceItem{
		Model:     store.SiteModel{UpstreamName: " GPT 5 "},
		Canonical: &canonical,
		Pricing:   &store.SiteModelPricing{GroupName: "vip"},
	}
	if got := modelPriceDisplayKey(item); got != "openai/gpt-5" {
		t.Fatalf("display key = %q, want canonical key", got)
	}
	if got := pricingGroupName(item.Pricing); got != "vip" {
		t.Fatalf("group name = %q, want vip", got)
	}
	if got := pricingGroupName(nil); got != "" {
		t.Fatalf("nil group name = %q, want empty", got)
	}

	item.Canonical = nil
	if got := modelPriceDisplayKey(item); got == "" || got == item.Model.UpstreamName {
		t.Fatalf("expected upstream name to be normalized, got %q", got)
	}

	value := 0.25
	nullable, ok := nullableFloatPtr(&value).(sql.NullFloat64)
	if !ok || !nullable.Valid || nullable.Float64 != value {
		t.Fatalf("nullable float = %#v, want valid %v", nullable, value)
	}
	if got := nullableFloatPtr(nil); got != nil {
		t.Fatalf("nil nullable float = %#v, want nil", got)
	}
}

func TestMissingModelPriceFilterExcludesNewAPI(t *testing.T) {
	t.Parallel()

	item := ModelPriceItem{
		Site:          store.Site{ID: uuid.New(), SiteType: "newapi"},
		Model:         store.SiteModel{ID: uuid.New(), UpstreamName: "gpt-test"},
		PricingStatus: "missing",
	}
	if modelPriceMatchesFilters(item, ModelPriceFilters{PricingStatus: "missing"}) {
		t.Fatal("expected missing filter to exclude newapi rows")
	}

	item.Site.SiteType = "openai"
	if !modelPriceMatchesFilters(item, ModelPriceFilters{PricingStatus: "missing"}) {
		t.Fatal("expected missing filter to include non-newapi rows")
	}
}

func TestModelPriceMatchesIdentityAndTextFilters(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	canonicalID := uuid.New()
	item := ModelPriceItem{
		Site: store.Site{
			ID:       siteID,
			Name:     "Primary OpenAI",
			SiteType: "openai",
		},
		Model: store.SiteModel{
			ID:           uuid.New(),
			UpstreamName: "gpt-4o-mini",
			DisplayName:  "GPT 4o Mini",
		},
		Canonical: &store.CanonicalModel{
			ID:       canonicalID,
			ModelKey: "openai/gpt-4o-mini",
			Provider: "openai",
		},
		Pricing: &store.SiteModelPricing{
			BillingType: "tokens",
			Available:   true,
			InputValue:  sql.NullFloat64{Float64: 0.1, Valid: true},
		},
		PricingStatus: "synced",
	}

	matching := []ModelPriceFilters{
		{SiteID: siteID},
		{SiteType: "openai"},
		{Provider: "openai"},
		{CanonicalModelID: canonicalID},
		{ModelKey: " OPENAI/GPT-4O-MINI "},
		{ModelKey: " GPT-4O-MINI "},
		{BillingType: "tokens"},
		{PricingStatus: "priced"},
		{PricingStatus: "synced"},
		{Q: "4o mini"},
		{Q: "primary"},
	}
	for _, filters := range matching {
		if !modelPriceMatchesFilters(item, filters) {
			t.Fatalf("expected filters %#v to match item", filters)
		}
	}

	notMatching := []ModelPriceFilters{
		{SiteID: uuid.New()},
		{SiteType: "anthropic"},
		{Provider: "anthropic"},
		{CanonicalModelID: uuid.New()},
		{ModelKey: "openai/gpt-5"},
		{BillingType: "per_request"},
		{PricingStatus: "manual"},
		{Q: "claude"},
	}
	for _, filters := range notMatching {
		if modelPriceMatchesFilters(item, filters) {
			t.Fatalf("expected filters %#v not to match item", filters)
		}
	}
}

func TestModelPriceMatchesFiltersHandlesMissingCanonicalAndPricing(t *testing.T) {
	t.Parallel()

	item := ModelPriceItem{
		Site:          store.Site{ID: uuid.New(), Name: "Local", SiteType: "openai"},
		Model:         store.SiteModel{ID: uuid.New(), UpstreamName: "custom-model"},
		PricingStatus: "missing",
	}

	if !modelPriceMatchesFilters(item, ModelPriceFilters{ModelKey: "custom-model"}) {
		t.Fatal("expected upstream model key to match without canonical model")
	}
	if modelPriceMatchesFilters(item, ModelPriceFilters{Provider: "openai"}) {
		t.Fatal("expected provider filter to reject rows without canonical model")
	}
	if modelPriceMatchesFilters(item, ModelPriceFilters{CanonicalModelID: uuid.New()}) {
		t.Fatal("expected canonical filter to reject rows without canonical model")
	}
	if modelPriceMatchesFilters(item, ModelPriceFilters{BillingType: "tokens"}) {
		t.Fatal("expected billing type filter to reject rows without pricing")
	}
	if modelPriceMatchesFilters(item, ModelPriceFilters{PricingStatus: "priced"}) {
		t.Fatal("expected priced filter to reject missing pricing")
	}
}

func TestModelPriceListAddCountsTreatsUnknownStatusAsPriced(t *testing.T) {
	t.Parallel()

	list := ModelPriceList{}
	list.addCounts(ModelPriceItem{PricingStatus: "synced"})
	list.addCounts(ModelPriceItem{PricingStatus: "other"})

	if list.PricedCount != 2 || list.MissingCount != 0 || list.ManualCount != 0 {
		t.Fatalf("unexpected counts for priced-like statuses: %+v", list)
	}
}
