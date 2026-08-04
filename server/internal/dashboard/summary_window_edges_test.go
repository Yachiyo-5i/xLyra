package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestDailyModelSummariesRespectWindowBoundariesAndDefaults(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(3, time.Date(2026, 6, 6, 15, 30, 0, 0, time.UTC), timeZone)
	modelID := uuid.New()
	service := NewService(nil, timeZone)
	rows := []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.Add(-time.Nanosecond), CanonicalModelKey: "too-old", EstimatedCost: 99, Success: true, SuccessCount: 99},
		{BucketStart: window.RangeStart, CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CanonicalModelKey: "gpt-5", EstimatedCost: 1.5, Success: true, SuccessCount: 2},
		{BucketStart: window.RangeStart.Add(2 * time.Hour), CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true}, CanonicalModelKey: "gpt-5", EstimatedCost: 2.5, Success: true, SuccessCount: 3},
		{BucketStart: window.RangeStart, CanonicalModelKey: "", EstimatedCost: 6, Currency: "EUR", Success: false, SuccessCount: 7},
		{BucketStart: window.TodayStart, CanonicalModelKey: summaryNoneKey, EstimatedCost: 4, Success: true, SuccessCount: 5},
	}

	costs, err := service.dailyModelCostFromSummaries(window, rows)
	if err != nil {
		t.Fatalf("daily model cost: %v", err)
	}
	if len(costs) != 3 {
		t.Fatalf("expected three cost points, got %#v", costs)
	}
	if costs[0].Date != "2026-06-04" || costs[0].ModelKey != "unknown" || costs[0].Cost != 6 || costs[0].Currency != "EUR" {
		t.Fatalf("expected unknown EUR cost first for range start day, got %#v", costs[0])
	}
	if costs[1].Date != "2026-06-04" || costs[1].ModelID == nil || *costs[1].ModelID != modelID.String() || costs[1].Cost != 4 || costs[1].Currency != "USD" {
		t.Fatalf("expected gpt-5 default-currency aggregate second, got %#v", costs[1])
	}
	if costs[2].Date != "2026-06-06" || costs[2].ModelKey != "unknown" || costs[2].Cost != 4 {
		t.Fatalf("expected today none-key cost normalized to unknown, got %#v", costs[2])
	}

	requests, err := service.dailyModelRequestsFromSummaries(window, rows)
	if err != nil {
		t.Fatalf("daily model requests: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("expected two request points, got %#v", requests)
	}
	if requests[0].Date != "2026-06-04" || requests[0].ModelKey != "gpt-5" || requests[0].RequestCount != 5 {
		t.Fatalf("expected successful gpt-5 requests at range boundary, got %#v", requests[0])
	}
	if requests[1].Date != "2026-06-06" || requests[1].ModelKey != "unknown" || requests[1].RequestCount != 5 {
		t.Fatalf("expected today none-key requests normalized to unknown, got %#v", requests[1])
	}
}

func TestDailyAPIKeyUsageDefaultsAndSortsBoundedRows(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(2, time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC), timeZone)
	namedID := uuid.New()
	blankNameID := uuid.New()
	items := dailyAPIKeyUsageFromSummaries(window, []store.RequestUsageDailySummary{
		{BucketStart: window.RangeStart.Add(-time.Nanosecond), APIKeyID: uuid.NullUUID{UUID: namedID, Valid: true}, APIKeyName: "Too Old", TotalTokens: 900, EstimatedCost: 9},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: blankNameID, Valid: true}, TotalTokens: 20, EstimatedCost: 0.2},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: namedID, Valid: true}, APIKeyName: "Build", TotalTokens: 15, EstimatedCost: 0.15, Currency: "EUR"},
		{BucketStart: window.RangeStart, APIKeyID: uuid.NullUUID{UUID: namedID, Valid: true}, APIKeyName: "Build", TotalTokens: 15, EstimatedCost: 0.15, Currency: "EUR"},
		{BucketStart: window.TodayStart, APIKeyID: uuid.NullUUID{UUID: namedID, Valid: true}, APIKeyName: "Build", TotalTokens: 100, EstimatedCost: 1},
		{BucketStart: window.TodayStart, APIKeyName: "Missing ID", TotalTokens: 500, EstimatedCost: 5},
	}, nil)

	if len(items) != 3 {
		t.Fatalf("expected three API key usage points, got %#v", items)
	}
	if items[0].Date != "2026-06-05" || items[0].APIKeyID != namedID.String() || items[0].APIKeyName != "Build" || items[0].TotalTokens != 30 || items[0].Cost != 0.3 || items[0].Currency != "EUR" {
		t.Fatalf("expected named EUR aggregate first on range start day, got %#v", items[0])
	}
	if items[1].Date != "2026-06-05" || items[1].APIKeyID != blankNameID.String() || items[1].APIKeyName != blankNameID.String() || items[1].TotalTokens != 20 || items[1].Currency != "USD" {
		t.Fatalf("expected blank name to fall back to key id with default currency, got %#v", items[1])
	}
	if items[2].Date != "2026-06-06" || items[2].APIKeyID != namedID.String() || items[2].TotalTokens != 100 || items[2].Currency != "USD" {
		t.Fatalf("expected today row after older date groups, got %#v", items[2])
	}
}

func TestSummaryDateAndUptimeHourKeyUseProvidedLocations(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone(config.DefaultTimeZone)
	window := newTimeWindow(1, time.Date(2026, 6, 6, 8, 0, 0, 0, timeZone.Location), timeZone)
	lateUTC := time.Date(2026, 6, 5, 16, 30, 0, 0, time.UTC)
	if got := summaryDate(lateUTC, window); got != "2026-06-06" {
		t.Fatalf("expected summary date in dashboard timezone, got %q", got)
	}

	shanghaiHour := lateUTC.In(timeZone.Location).Truncate(time.Hour)
	if got := uptimeHourKey(shanghaiHour); got != "2026-06-06 00:00:00" {
		t.Fatalf("expected uptime key to preserve local hour, got %q", got)
	}
	if got := uptimeHourKey(lateUTC.Truncate(time.Hour)); got != "2026-06-05 16:00:00" {
		t.Fatalf("expected uptime key to preserve UTC hour when UTC time is provided, got %q", got)
	}
}

func TestTimeClausesUseTypedGORMExpressions(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)

	gte := timeGteClause("created_at", start)
	if len(gte.Exprs) != 1 {
		t.Fatalf("expected one gte expression, got %#v", gte.Exprs)
	}
	gteExpr, ok := gte.Exprs[0].(clause.Gte)
	if !ok {
		t.Fatalf("expected clause.Gte, got %T", gte.Exprs[0])
	}
	if gteExpr.Column != (clause.Column{Name: "created_at"}) || !gteExpr.Value.(time.Time).Equal(start) {
		t.Fatalf("unexpected gte expression: %#v", gteExpr)
	}

	ranged := timeRangeClause("checked_at", start, end)
	if len(ranged.Exprs) != 2 {
		t.Fatalf("expected two range expressions, got %#v", ranged.Exprs)
	}
	rangeStart, ok := ranged.Exprs[0].(clause.Gte)
	if !ok {
		t.Fatalf("expected range start clause.Gte, got %T", ranged.Exprs[0])
	}
	rangeEnd, ok := ranged.Exprs[1].(clause.Lt)
	if !ok {
		t.Fatalf("expected range end clause.Lt, got %T", ranged.Exprs[1])
	}
	if rangeStart.Column != (clause.Column{Name: "checked_at"}) || !rangeStart.Value.(time.Time).Equal(start) {
		t.Fatalf("unexpected range start expression: %#v", rangeStart)
	}
	if rangeEnd.Column != (clause.Column{Name: "checked_at"}) || !rangeEnd.Value.(time.Time).Equal(end) {
		t.Fatalf("unexpected range end expression: %#v", rangeEnd)
	}
}

func TestUptimeBucketPayloadIdleShape(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	window := newTimeWindow(1, time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC), timeZone)
	payload := uptimeBucket{hour: window.TodayStart.Add(3 * time.Hour)}.payload(window)
	if payload.Hour != "2026-06-06T03:00:00Z" || payload.Status != "idle" {
		t.Fatalf("unexpected idle uptime payload: %#v", payload)
	}
	if payload.SuccessRate != nil || payload.SuccessCount != 0 || payload.FailureCount != 0 || payload.TotalCount != 0 {
		t.Fatalf("expected zero-count idle payload without success rate, got %#v", payload)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal uptime payload: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal uptime payload: %v", err)
	}
	if _, ok := decoded["success_rate"]; !ok || decoded["success_rate"] != nil {
		t.Fatalf("expected success_rate JSON key with null value, got %s", raw)
	}
}
