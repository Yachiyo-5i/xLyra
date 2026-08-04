package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHealthCreateSnapshotBuildsDefaultsOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	captured := storeCaptureCreate[HealthSnapshot](t, db, "health snapshot", nil)

	siteID := uuid.New()
	siteModelID := uuid.New()
	checkedAt := time.Date(2026, 6, 23, 9, 30, 0, 0, time.UTC)
	item, err := NewHealthRepository(db).CreateSnapshot(t.Context(), CreateHealthSnapshotParams{
		SiteID:       siteID,
		SiteModelID:  siteModelID,
		Endpoint:     "/health",
		Success:      true,
		StatusCode:   int32(204),
		LatencyMS:    int64(17),
		ErrorType:    " ",
		ErrorMessage: "ok",
		CheckedAt:    checkedAt,
	})
	if err != nil {
		t.Fatalf("CreateSnapshot returned error: %v", err)
	}

	if item.SiteID != siteID || captured.SiteID != siteID {
		t.Fatalf("site id was not preserved: item=%#v captured=%#v", item, *captured)
	}
	if captured.Scope != "site" || captured.Source != "manual" || captured.Method != "GET" {
		t.Fatalf("snapshot defaults = scope %q source %q method %q", captured.Scope, captured.Source, captured.Method)
	}
	if !captured.SiteModelID.Valid || captured.SiteModelID.UUID != siteModelID {
		t.Fatalf("site model id = %#v, want valid %s", captured.SiteModelID, siteModelID)
	}
	if !captured.StatusCode.Valid || captured.StatusCode.Int64 != 204 {
		t.Fatalf("status code = %#v, want 204", captured.StatusCode)
	}
	if !captured.LatencyMS.Valid || captured.LatencyMS.Int64 != 17 {
		t.Fatalf("latency = %#v, want 17", captured.LatencyMS)
	}
	if !captured.ErrorType.Valid || captured.ErrorType.String != " " {
		t.Fatalf("error type should keep provided string value, got %#v", captured.ErrorType)
	}
	if string(captured.Metadata) != "{}" {
		t.Fatalf("metadata = %s, want {}", captured.Metadata)
	}
	if !captured.CheckedAt.Equal(checkedAt) {
		t.Fatalf("checked at = %s, want %s", captured.CheckedAt, checkedAt)
	}
}

func TestHealthCreateSnapshotWrapsCallbackError(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	createErr := errors.New("health create stopped")
	storeCreateError(t, db, createErr)

	_, err := NewHealthRepository(db).CreateSnapshot(t.Context(), CreateHealthSnapshotParams{
		SiteID: uuid.New(),
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("CreateSnapshot error = %v, want wrapped callback error", err)
	}
}

func TestHealthUpsertSiteStateCreatesAfterMissingStateOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeQueryRecordNotFound(t, db)
	captured := storeCaptureCreate[SiteHealthState](t, db, "site health state", nil)

	siteID := uuid.New()
	snapshotID := uuid.New()
	checkedAt := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	state, err := NewHealthRepository(db).UpsertSiteState(t.Context(), UpsertSiteHealthStateParams{
		SiteID:              siteID,
		LastSnapshotID:      snapshotID,
		LastSuccessAt:       checkedAt,
		ConsecutiveFailures: 3,
		RecentSuccessRate:   float32(0.75),
		RecentAvgLatencyMS:  int(120),
		CheckedAt:           &checkedAt,
		Message:             "degraded",
	})
	if err != nil {
		t.Fatalf("UpsertSiteState returned error: %v", err)
	}

	if state.SiteID != siteID || captured.SiteID != siteID {
		t.Fatalf("site id was not preserved: state=%#v captured=%#v", state, *captured)
	}
	if captured.Status != "unknown" {
		t.Fatalf("blank status should default to unknown, got %q", captured.Status)
	}
	if !captured.LastSnapshotID.Valid || captured.LastSnapshotID.UUID != snapshotID {
		t.Fatalf("last snapshot id = %#v, want valid %s", captured.LastSnapshotID, snapshotID)
	}
	if !captured.LastSuccessAt.Valid || !captured.LastSuccessAt.Time.Equal(checkedAt) {
		t.Fatalf("last success = %#v, want %s", captured.LastSuccessAt, checkedAt)
	}
	if captured.LastFailureAt.Valid {
		t.Fatalf("last failure = %#v, want invalid", captured.LastFailureAt)
	}
	if captured.ConsecutiveFailures != 3 {
		t.Fatalf("consecutive failures = %d, want 3", captured.ConsecutiveFailures)
	}
	if !captured.RecentSuccessRate.Valid || captured.RecentSuccessRate.Float64 != float64(float32(0.75)) {
		t.Fatalf("recent success rate = %#v, want 0.75", captured.RecentSuccessRate)
	}
	if !captured.RecentAvgLatencyMS.Valid || captured.RecentAvgLatencyMS.Int64 != 120 {
		t.Fatalf("recent latency = %#v, want 120", captured.RecentAvgLatencyMS)
	}
	if !captured.Message.Valid || captured.Message.String != "degraded" {
		t.Fatalf("message = %#v, want degraded", captured.Message)
	}
	if string(captured.Metadata) != "{}" {
		t.Fatalf("metadata = %s, want {}", captured.Metadata)
	}
}
