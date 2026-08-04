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

func TestChannelSplitRowsPropagatesSummaryListRepositoryError(t *testing.T) {
	t.Parallel()

	service, queryErr := usageServiceWithQueryError(t, "summary list stopped")
	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)

	rows, err := service.channelSplitRows(context.Background(), []uuid.UUID{uuid.New()}, channelSplitWindow{
		RangeStart:        &from,
		RangeEndExclusive: &to,
	}, config.LoadTimeZone("UTC"))
	if rows != nil {
		t.Fatalf("channelSplitRows rows = %#v, want nil on error", rows)
	}
	if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "list request usage summaries") {
		t.Fatalf("channelSplitRows error = %v, want wrapped summary list error", err)
	}
}

func TestChannelSplitAPIKeysPropagatesRepositoryListError(t *testing.T) {
	t.Parallel()

	service, queryErr := usageServiceWithQueryError(t, "api key list stopped")

	apiKeys, err := service.channelSplitAPIKeys(context.Background(), []uuid.UUID{uuid.New()})
	if apiKeys != nil {
		t.Fatalf("channelSplitAPIKeys result = %#v, want nil on error", apiKeys)
	}
	if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "list api keys by ids") {
		t.Fatalf("channelSplitAPIKeys error = %v, want wrapped api key list error", err)
	}
}

func TestRequestSummaryPropagatesSummaryAndDetailRepositoryErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)

	t.Run("summary rows", func(t *testing.T) {
		t.Parallel()

		service, queryErr := usageServiceWithQueryError(t, "summary cost stopped")
		from := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		to := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)

		result, err := service.RequestSummary(context.Background(), RequestQuery{
			CreatedFrom: &from,
			CreatedTo:   &to,
		}, now)
		if result != (RequestSummaryResult{}) {
			t.Fatalf("RequestSummary result = %#v, want zero on summary error", result)
		}
		if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "request usage cost summary") {
			t.Fatalf("RequestSummary error = %v, want wrapped summary cost error", err)
		}
	})

	t.Run("detail rows", func(t *testing.T) {
		t.Parallel()

		service, queryErr := usageServiceWithQueryError(t, "detail cost stopped")
		from := time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC)
		to := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)

		result, err := service.RequestSummary(context.Background(), RequestQuery{
			CreatedFrom: &from,
			CreatedTo:   &to,
		}, now)
		if result != (RequestSummaryResult{}) {
			t.Fatalf("RequestSummary result = %#v, want zero on detail error", result)
		}
		if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "list request logs for cost summary") {
			t.Fatalf("RequestSummary error = %v, want wrapped detail cost error", err)
		}
	})
}

func TestUsageServiceEntryPointsPropagateRepositoryErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		call    func(*Service) error
		message string
	}{
		{
			name: "list requests",
			call: func(service *Service) error {
				_, err := service.ListRequests(context.Background(), RequestQuery{Limit: 1})
				return err
			},
			message: "count request logs",
		},
		{
			name: "list requests page",
			call: func(service *Service) error {
				_, err := service.ListRequestsPage(context.Background(), RequestQuery{Page: 1, PageSize: 1})
				return err
			},
			message: "count request logs",
		},
		{
			name: "get request",
			call: func(service *Service) error {
				_, err := service.GetRequest(context.Background(), uuid.New())
				return err
			},
			message: "get request log detail",
		},
		{
			name: "recent rate usage",
			call: func(service *Service) error {
				_, err := service.RecentRateUsage(context.Background(), time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC))
				return err
			},
			message: "recent request rate usage",
		},
		{
			name: "usage summary by site",
			call: func(service *Service) error {
				_, err := service.UsageSummaryBySite(context.Background(), nil)
				return err
			},
			message: "summarize usage by site",
		},
		{
			name: "usage summary for site",
			call: func(service *Service) error {
				_, _, err := service.UsageSummaryForSite(context.Background(), uuid.New(), nil)
				return err
			},
			message: "summarize usage by site",
		},
		{
			name: "channel split site lookup",
			call: func(service *Service) error {
				siteID := uuid.New()
				_, err := service.ChannelSplit(context.Background(), ChannelSplitQuery{SiteID: &siteID}, time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC))
				return err
			},
			message: "list sites by ids",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			service, queryErr := usageServiceWithQueryError(t, tc.name+" stopped")

			err := tc.call(service)
			if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("%s error = %v, want wrapped %q error", tc.name, err, tc.message)
			}
		})
	}
}

func TestUsageServiceDryRunReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	service := usageServiceWithDryRunGorm(t)

	recent, err := service.RecentRateUsage(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("RecentRateUsage empty dry run: %v", err)
	}
	if recent.RPM != 0 || recent.TPM != 0 {
		t.Fatalf("RecentRateUsage empty dry run = %#v, want zero summary", recent)
	}

	after := time.Date(2026, 6, 22, 15, 30, 0, 0, time.UTC)
	row, found, err := service.UsageSummaryForSite(context.Background(), uuid.New(), &after)
	if err != nil {
		t.Fatalf("UsageSummaryForSite empty dry run: %v", err)
	}
	if found || row.SiteID != uuid.Nil {
		t.Fatalf("UsageSummaryForSite empty dry run = %#v found=%v, want zero false", row, found)
	}
}
