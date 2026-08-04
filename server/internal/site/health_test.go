package site

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestBuildSiteHealthStateParamsHealthy(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	snapshotID := uuid.New()
	checkedAt := time.Date(2026, 6, 22, 12, 30, 0, 0, time.UTC)
	params := buildSiteHealthStateParams(siteID, store.HealthSnapshot{
		ID:        snapshotID,
		SiteID:    siteID,
		Success:   true,
		CheckedAt: checkedAt,
	}, []store.HealthSnapshot{
		{Success: true, LatencyMS: sql.NullInt64{Int64: 100, Valid: true}},
		{Success: true, LatencyMS: sql.NullInt64{Int64: 200, Valid: true}},
		{Success: true},
		{Success: true, LatencyMS: sql.NullInt64{Int64: 300, Valid: true}},
	})

	if params.SiteID != siteID {
		t.Fatalf("SiteID = %s, want %s", params.SiteID, siteID)
	}
	if params.Status != "healthy" {
		t.Fatalf("Status = %q, want healthy", params.Status)
	}
	if params.LastSnapshotID != snapshotID {
		t.Fatalf("LastSnapshotID = %s, want %s", params.LastSnapshotID, snapshotID)
	}
	if params.LastSuccessAt != checkedAt {
		t.Fatalf("LastSuccessAt = %#v, want %s", params.LastSuccessAt, checkedAt)
	}
	if params.LastFailureAt != nil {
		t.Fatalf("LastFailureAt = %#v, want nil", params.LastFailureAt)
	}
	if params.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0", params.ConsecutiveFailures)
	}
	if params.RecentSuccessRate != 1.0 {
		t.Fatalf("RecentSuccessRate = %#v, want 1.0", params.RecentSuccessRate)
	}
	if params.RecentAvgLatencyMS != int64(200) {
		t.Fatalf("RecentAvgLatencyMS = %#v, want 200", params.RecentAvgLatencyMS)
	}
	if params.CheckedAt != checkedAt {
		t.Fatalf("CheckedAt = %#v, want %s", params.CheckedAt, checkedAt)
	}
	if params.Message != "ok" {
		t.Fatalf("Message = %#v, want ok", params.Message)
	}

	metadata := siteMustJSONMap(t, params.Metadata)
	if metadata["recent_window"] != float64(4) {
		t.Fatalf("recent_window = %#v, want 4", metadata["recent_window"])
	}
}

func TestBuildSiteHealthStateParamsUnhealthyAfterConsecutiveFailures(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	checkedAt := time.Date(2026, 6, 22, 13, 0, 0, 0, time.UTC)
	params := buildSiteHealthStateParams(siteID, store.HealthSnapshot{
		ID:           uuid.New(),
		SiteID:       siteID,
		Success:      false,
		ErrorMessage: sql.NullString{String: "upstream timeout", Valid: true},
		CheckedAt:    checkedAt,
	}, []store.HealthSnapshot{
		{Success: false, LatencyMS: sql.NullInt64{Int64: 900, Valid: true}},
		{Success: false, LatencyMS: sql.NullInt64{Int64: 1100, Valid: true}},
		{Success: false},
		{Success: true, LatencyMS: sql.NullInt64{Int64: 500, Valid: true}},
	})

	if params.Status != "unhealthy" {
		t.Fatalf("Status = %q, want unhealthy", params.Status)
	}
	if params.LastSuccessAt != nil {
		t.Fatalf("LastSuccessAt = %#v, want nil", params.LastSuccessAt)
	}
	if params.LastFailureAt != checkedAt {
		t.Fatalf("LastFailureAt = %#v, want %s", params.LastFailureAt, checkedAt)
	}
	if params.ConsecutiveFailures != 3 {
		t.Fatalf("ConsecutiveFailures = %d, want 3", params.ConsecutiveFailures)
	}
	if params.RecentSuccessRate != 0.25 {
		t.Fatalf("RecentSuccessRate = %#v, want 0.25", params.RecentSuccessRate)
	}
	if params.RecentAvgLatencyMS != int64(833) {
		t.Fatalf("RecentAvgLatencyMS = %#v, want 833", params.RecentAvgLatencyMS)
	}
	if params.Message != "upstream timeout" {
		t.Fatalf("Message = %#v, want upstream timeout", params.Message)
	}
}

func TestBuildSiteHealthStateParamsDegradedAndUnknownWindows(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	checkedAt := time.Date(2026, 6, 22, 14, 0, 0, 0, time.UTC)
	degraded := buildSiteHealthStateParams(siteID, store.HealthSnapshot{
		ID:           uuid.New(),
		SiteID:       siteID,
		Success:      false,
		ErrorMessage: sql.NullString{String: "single failure", Valid: true},
		CheckedAt:    checkedAt,
	}, []store.HealthSnapshot{
		{Success: false, LatencyMS: sql.NullInt64{Int64: 120, Valid: true}},
		{Success: true, LatencyMS: sql.NullInt64{Int64: 80, Valid: true}},
		{Success: true},
	})

	if degraded.Status != "degraded" {
		t.Fatalf("single recent failure status = %q, want degraded", degraded.Status)
	}
	if degraded.ConsecutiveFailures != 1 {
		t.Fatalf("single recent failure count = %d, want 1", degraded.ConsecutiveFailures)
	}
	if degraded.RecentSuccessRate != float64(2)/float64(3) {
		t.Fatalf("success rate = %#v, want 2/3", degraded.RecentSuccessRate)
	}
	if degraded.RecentAvgLatencyMS != int64(100) {
		t.Fatalf("avg latency = %#v, want 100", degraded.RecentAvgLatencyMS)
	}
	if degraded.Message != "single failure" {
		t.Fatalf("message = %#v, want single failure", degraded.Message)
	}

	lowSuccessRate := buildSiteHealthStateParams(siteID, store.HealthSnapshot{
		ID:        uuid.New(),
		SiteID:    siteID,
		Success:   true,
		CheckedAt: checkedAt,
	}, []store.HealthSnapshot{
		{Success: true, LatencyMS: sql.NullInt64{Int64: 10, Valid: true}},
		{Success: false, LatencyMS: sql.NullInt64{Int64: 20, Valid: true}},
		{Success: false, LatencyMS: sql.NullInt64{Int64: 30, Valid: true}},
	})
	if lowSuccessRate.Status != "degraded" {
		t.Fatalf("low success rate status = %q, want degraded", lowSuccessRate.Status)
	}
	if lowSuccessRate.ConsecutiveFailures != 0 {
		t.Fatalf("low success rate consecutive failures = %d, want 0", lowSuccessRate.ConsecutiveFailures)
	}
	if lowSuccessRate.RecentSuccessRate != float64(1)/float64(3) {
		t.Fatalf("low success rate = %#v, want 1/3", lowSuccessRate.RecentSuccessRate)
	}

	unknown := buildSiteHealthStateParams(siteID, store.HealthSnapshot{
		ID:        uuid.New(),
		SiteID:    siteID,
		Success:   true,
		CheckedAt: checkedAt,
	}, nil)
	if unknown.Status != "unknown" {
		t.Fatalf("empty recent window status = %q, want unknown", unknown.Status)
	}
	if unknown.RecentSuccessRate != nil || unknown.RecentAvgLatencyMS != nil {
		t.Fatalf("empty recent window metrics = %#v/%#v, want nil/nil", unknown.RecentSuccessRate, unknown.RecentAvgLatencyMS)
	}
}

func TestUnknownSiteHealthState(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	state := unknownSiteHealthState(siteID)
	if state.SiteID != siteID {
		t.Fatalf("SiteID = %s, want %s", state.SiteID, siteID)
	}
	if state.Status != "unknown" {
		t.Fatalf("Status = %q, want unknown", state.Status)
	}
	if string(state.Metadata) != `{}` {
		t.Fatalf("Metadata = %s, want {}", state.Metadata)
	}
}

func TestNullableHealthString(t *testing.T) {
	t.Parallel()

	if got := nullableHealthString(" \t\n"); got != nil {
		t.Fatalf("blank string = %#v, want nil", got)
	}
	if got := nullableHealthString(" ok "); got != " ok " {
		t.Fatalf("nonblank string = %#v, want original value", got)
	}
}

func TestSiteHealthSnapshotsForStateIgnoresLegacyUnsupportedProbeFailures(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	siteID := uuid.New()
	recent := []store.HealthSnapshot{
		{
			SiteID:    siteID,
			Success:   false,
			Endpoint:  "detect",
			Method:    "GET",
			ErrorType: sql.NullString{String: "unsupported_health_check", Valid: true},
			CheckedAt: time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC),
			Metadata:  store.JSON(`{"site_type":"opencode_go"}`),
		},
		{
			SiteID:    siteID,
			Success:   false,
			Endpoint:  "GET /v1/models",
			Method:    "GET",
			ErrorType: sql.NullString{String: "unsupported_health_check", Valid: true},
			Metadata:  store.JSON(`{"site_type":"opencode_go"}`),
		},
		{
			SiteID:    siteID,
			Success:   false,
			Endpoint:  "detect",
			Method:    "GET",
			ErrorType: sql.NullString{String: "unsupported_health_check", Valid: true},
			Metadata:  store.JSON(`{"site_type":"other"}`),
		},
		{SiteID: siteID, Success: true},
	}

	filtered := service.siteHealthSnapshotsForState(store.Site{ID: siteID, SiteType: "opencode_go"}, recent)
	if len(filtered) != 3 || filtered[0].Endpoint != "GET /v1/models" {
		t.Fatalf("filtered health snapshots = %#v, want matching legacy failure removed only", filtered)
	}

	unchanged := service.siteHealthSnapshotsForState(store.Site{ID: siteID, SiteType: "openai"}, recent)
	if len(unchanged) != len(recent) {
		t.Fatalf("non-probe site snapshots = %#v, want unchanged history", unchanged)
	}
}
