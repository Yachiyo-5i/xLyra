package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestSummaryServiceUsesProvidedTimeZone(t *testing.T) {
	t.Parallel()

	tokyo := config.LoadTimeZone("Asia/Tokyo")
	got := summaryServiceTimeZone(tokyo)
	if got.Name != tokyo.Name || got.Location != tokyo.Location {
		t.Fatalf("summaryServiceTimeZone provided zone = %#v, want %#v", got, tokyo)
	}
}

func TestSummaryServiceStartupCheckPropagatesEnsureSummariesQueryError(t *testing.T) {
	t.Parallel()

	service, queryErr := usageSummaryServiceWithQueryError(t, "oldest detail lookup stopped")

	result, err := service.StartupCheck(context.Background(), time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC))
	if !errors.Is(err, queryErr) {
		t.Fatalf("StartupCheck error = %v, want wrapped query error", err)
	}
	assertSummaryMaintenanceError(t, "StartupCheck", result, err, "ensure request usage summaries")
}

func TestSummaryServiceEnsureSummariesZeroNowPropagatesQueryError(t *testing.T) {
	t.Parallel()

	service, queryErr := usageSummaryServiceWithQueryError(t, "zero-now oldest lookup stopped")

	result, err := service.ensureSummaries(context.Background(), time.Time{}, "startup")
	if !errors.Is(err, queryErr) {
		t.Fatalf("ensureSummaries error = %v, want wrapped query error", err)
	}
	assertSummaryMaintenanceError(t, "ensureSummaries", result, err, "ensure request usage summaries")
}
