package site

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestSiteHealthReadAPIsWrapRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("health repository offline")
	service := siteServiceWithQueryError(t, queryErr)
	ctx := context.Background()
	siteID := uuid.New()

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "site health states",
			call: func() error {
				_, err := service.SiteHealthStates(ctx)
				return err
			},
			wantErr: "list site health states",
		},
		{
			name: "site health history stops on site lookup",
			call: func() error {
				_, err := service.SiteHealthHistory(ctx, siteID, 3, "scheduler")
				return err
			},
			wantErr: "get site",
		},
		{
			name: "site health hourly stops on site lookup",
			call: func() error {
				_, err := service.SiteHealthHourly(ctx, siteID, 3)
				return err
			},
			wantErr: "get site",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			assertSiteWrappedQueryError(t, tt.name, err, queryErr, tt.wantErr)
		})
	}
}

func TestSiteHealthHistoryAndHourlyWrapSnapshotQueryErrors(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	queryErr := errors.New("snapshot query offline")
	service := siteServiceWithSnapshotQueryFailureAfterSiteLookup(t, siteID, queryErr)

	_, err := service.SiteHealthHistory(context.Background(), siteID, 5, "scheduler")
	assertSiteWrappedQueryError(t, "SiteHealthHistory", err, queryErr, "list site health snapshots by source")
	_, err = service.SiteHealthHourly(context.Background(), siteID, 5)
	assertSiteWrappedQueryError(t, "SiteHealthHourly", err, queryErr, "list site health hourly buckets")
}

func siteServiceWithSnapshotQueryFailureAfterSiteLookup(t *testing.T, siteID uuid.UUID, queryErr error) *Service {
	t.Helper()

	return siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{
				ID:       siteID,
				Name:     "Primary",
				Slug:     "primary",
				SiteType: "openai",
				BaseURL:  "https://api.example.com",
				Status:   "active",
				Enabled:  true,
			}
			tx.RowsAffected = 1
		default:
			tx.AddError(queryErr)
		}
	})
}
