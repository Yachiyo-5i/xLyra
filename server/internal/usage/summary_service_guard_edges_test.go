package usage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
)

func TestSummaryServiceNilStartupCheckReturnsStoreGuard(t *testing.T) {
	t.Parallel()

	var service *SummaryService
	result, err := service.StartupCheck(context.Background(), time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC))
	assertSummaryMaintenanceError(t, "StartupCheck", result, err, "usage summary store is not initialized")
}

func TestSummaryServiceNilDailyMaintenanceReturnsStoreGuard(t *testing.T) {
	t.Parallel()

	var service *SummaryService
	result, err := service.DailyMaintenance(context.Background(), time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC))
	assertSummaryMaintenanceError(t, "DailyMaintenance", result, err, "usage summary store is not initialized")
}

func TestSummaryServiceEnsureSummariesMissingDBReturnsStoreGuard(t *testing.T) {
	t.Parallel()

	service := NewSummaryService(nil, nil, config.LoadTimeZone("UTC"))
	result, err := service.ensureSummaries(context.Background(), time.Time{}, "test")
	assertSummaryMaintenanceError(t, "ensureSummaries", result, err, "usage summary store is not initialized")
}

func TestSummaryServiceTimeZoneFallbacksResolveLocation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		zone config.TimeZone
	}{
		{name: "none"},
		{name: "missing location", zone: config.TimeZone{Name: "UTC"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got config.TimeZone
			if tc.name == "none" {
				got = summaryServiceTimeZone()
			} else {
				got = summaryServiceTimeZone(tc.zone)
			}
			if got.Location == nil || strings.TrimSpace(got.Name) == "" {
				t.Fatalf("summaryServiceTimeZone(%#v) = %#v, want resolved timezone", tc.zone, got)
			}
		})
	}
}

func TestChannelSplitEmptyInputsShortCircuitBeforeDB(t *testing.T) {
	t.Parallel()

	service := usageUTCService()
	target, err := service.channelSplitTarget(context.Background(), ChannelSplitQuery{})
	if !errors.Is(err, ErrInvalidChannelSplitQuery) {
		t.Fatalf("channelSplitTarget error = %v, want ErrInvalidChannelSplitQuery", err)
	}
	if len(target.Sites) != 0 || target.OAuthConnectionID != nil {
		t.Fatalf("channelSplitTarget result = %#v, want empty target", target)
	}

	apiKeys, err := service.channelSplitAPIKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("channelSplitAPIKeys empty input: %v", err)
	}
	if len(apiKeys) != 0 {
		t.Fatalf("channelSplitAPIKeys empty input = %#v, want empty map", apiKeys)
	}
	apiKeys, err = service.channelSplitAPIKeys(context.Background(), []uuid.UUID{})
	if err != nil {
		t.Fatalf("channelSplitAPIKeys empty slice: %v", err)
	}
	if len(apiKeys) != 0 {
		t.Fatalf("channelSplitAPIKeys empty slice = %#v, want empty map", apiKeys)
	}
}
