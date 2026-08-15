package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
)

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
