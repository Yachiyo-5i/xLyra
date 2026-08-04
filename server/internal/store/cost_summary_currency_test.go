package store

import "testing"

func TestRequestUsageCostSummarySingleCurrencyUnchanged(t *testing.T) {
	t.Parallel()

	var s RequestUsageCostSummary
	s.AddFloat(0.10, "USD")
	s.AddFloat(0.20, "USD")
	s.AddFloat(0.05, "") // blank defaults to USD
	if s.Currency != "USD" {
		t.Fatalf("currency = %q, want USD", s.Currency)
	}
	if got := s.TotalCost; got < 0.349 || got > 0.351 {
		t.Fatalf("single-currency total = %v, want ~0.35", got)
	}
}

func TestRequestUsageCostSummaryMixedCurrencyStaysConsistent(t *testing.T) {
	t.Parallel()

	var s RequestUsageCostSummary
	s.AddFloat(1, "USD")
	s.AddFloat(2, "EUR")
	s.AddFloat(3, "EUR")
	// EUR is the first non-default currency → summary currency; TotalCost is the
	// EUR total (5), not 1+2+3 across currencies.
	if s.Currency != "EUR" || s.TotalCost != 5 {
		t.Fatalf("summary = %+v, want EUR total 5", s)
	}
	if s.CostByCurrency["USD"] != 1 || s.CostByCurrency["EUR"] != 5 {
		t.Fatalf("breakdown = %#v", s.CostByCurrency)
	}
}
