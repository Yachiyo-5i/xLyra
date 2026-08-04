package dashboard

import (
	"testing"
	"time"

	"xlyra/server/internal/store"
)

func TestAttentionRankDefaultsStayLast(t *testing.T) {
	t.Parallel()

	if got := attentionSeverityRank("critical"); got != 0 {
		t.Fatalf("critical severity rank = %d, want 0", got)
	}
	if got := attentionSeverityRank("unknown"); got != 3 {
		t.Fatalf("unknown severity rank = %d, want 3", got)
	}

	if got := attentionTypeRank("no_route_candidates"); got != 0 {
		t.Fatalf("no_route_candidates type rank = %d, want 0", got)
	}
	if got := attentionTypeRank("not_registered"); got != 99 {
		t.Fatalf("unknown type rank = %d, want 99", got)
	}
}

func TestQuotaPayloadRejectsEmptyOrMissingQuota(t *testing.T) {
	t.Parallel()

	for _, raw := range []store.JSON{
		nil,
		store.JSON(`{}`),
		store.JSON(`{"user":{}}`),
		store.JSON(`{"user":{"quota":{}}}`),
		store.JSON(`{"quota":{}}`),
		store.JSON(`not-json`),
	} {
		if got := epaperCodexQuotaPayload(raw); got != nil {
			t.Fatalf("epaperCodexQuotaPayload(%s) = %#v, want nil", raw, got)
		}
	}
}

func TestPercentileContHandlesEmptySingleAndEndpointPercentiles(t *testing.T) {
	t.Parallel()

	if got := percentileCont(nil, 0.95); got != 0 {
		t.Fatalf("empty percentile = %v, want 0", got)
	}
	if got := percentileCont([]float64{12.5}, 0.95); got != 12.5 {
		t.Fatalf("single percentile = %v, want 12.5", got)
	}

	values := []float64{30, 10, 20}
	if got := percentileCont(append([]float64(nil), values...), 0); got != 10 {
		t.Fatalf("p0 = %v, want 10", got)
	}
	if got := percentileCont(append([]float64(nil), values...), 1); got != 30 {
		t.Fatalf("p1 = %v, want 30", got)
	}
	if got := percentileCont([]float64{40, 10, 30, 20}, 0.95); got != 38.5 {
		t.Fatalf("p95 = %v, want 38.5", got)
	}
}

func TestUnixSecondsPointerHandlesPrimitiveValues(t *testing.T) {
	t.Parallel()

	if got := unixSecondsPtrFromAny(nil); got != nil {
		t.Fatalf("nil unix seconds = %#v, want nil", got)
	}
	if got := unixSecondsPtrFromAny(int(123)); got == nil || *got != 123 {
		t.Fatalf("int unix seconds = %#v, want 123", got)
	}
	if got := unixSecondsPtrFromAny(int64(456)); got == nil || *got != 456 {
		t.Fatalf("int64 unix seconds = %#v, want 456", got)
	}
	if got := unixSecondsPtrFromAny(float64(789.6)); got == nil || *got != 790 {
		t.Fatalf("float unix seconds = %#v, want 790", got)
	}
	if got := unixSecondsPtrFromAny(time.Time{}); got != nil {
		t.Fatalf("unsupported unix seconds = %#v, want nil", got)
	}
}
