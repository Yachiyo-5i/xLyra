package site

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestReadMethodsPropagateRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site repository query stopped")
	service := siteServiceWithQueryError(t, queryErr)
	ctx := context.Background()
	siteID := uuid.New()

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "list sites",
			call: func() error {
				_, err := service.List(ctx)
				return err
			},
			wantErr: "list sites",
		},
		{
			name: "list with deleted request sites",
			call: func() error {
				_, err := service.ListWithDeletedRequestSites(ctx)
				return err
			},
			wantErr: "list sites",
		},
		{
			name: "list site groups",
			call: func() error {
				_, err := service.ListSiteGroups(ctx)
				return err
			},
			wantErr: "list site groups",
		},
		{
			name: "get site group",
			call: func() error {
				_, err := service.GetSiteGroup(ctx, uuid.New())
				return err
			},
			wantErr: "get site group",
		},
		{
			name: "site group sites",
			call: func() error {
				_, err := service.SiteGroupSites(ctx, uuid.New())
				return err
			},
			wantErr: "list site group sites",
		},
		{
			name: "list models",
			call: func() error {
				_, err := service.ListModels(ctx, siteID)
				return err
			},
			wantErr: "list site models",
		},
		{
			name: "site pricing groups",
			call: func() error {
				_, err := service.SitePricingGroups(ctx, siteID)
				return err
			},
			wantErr: "list site pricing groups",
		},
		{
			name: "site model pricings",
			call: func() error {
				_, err := service.SiteModelPricings(ctx, siteID)
				return err
			},
			wantErr: "list site model pricings",
		},
		{
			name: "all site model pricings",
			call: func() error {
				_, err := service.AllSiteModelPricings(ctx)
				return err
			},
			wantErr: "list all site model pricings",
		},
		{
			name: "api keys",
			call: func() error {
				_, err := service.APIKeys(ctx, siteID)
				return err
			},
			wantErr: "list site credentials",
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

func TestCredentialAndSystemAuthPropagateInitialSiteError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site lookup stopped")
	service := siteServiceWithQueryError(t, queryErr)
	ctx := context.Background()
	siteID := uuid.New()

	if site, credential, secret, err := service.Credential(ctx, siteID); site.ID != uuid.Nil || credential.ID != uuid.Nil || secret != "" {
		t.Fatalf("Credential = %#v %#v %q %v, want zero values", site, credential, secret, err)
	} else {
		assertSiteQueryError(t, "Credential", err, queryErr)
	}
	if auth, err := service.SystemAuth(ctx, siteID); auth.AccessToken != "" {
		t.Fatalf("SystemAuth = %#v %v, want zero auth", auth, err)
	} else {
		assertSiteQueryError(t, "SystemAuth", err, queryErr)
	}
	if apiKey, err := service.APIKey(ctx, siteID); apiKey != "" {
		t.Fatalf("APIKey = %q %v, want empty api key", apiKey, err)
	} else {
		assertSiteQueryError(t, "APIKey", err, queryErr)
	}
}
