package site

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestServiceWriteGuardsValidateBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "delete requires site id",
			call: func() error {
				return service.Delete(context.Background(), uuid.Nil)
			},
			wantErr: "site id is required",
		},
		{
			name: "update requires site id",
			call: func() error {
				_, _, err := service.Update(context.Background(), UpdateSiteParams{})
				return err
			},
			wantErr: "site id is required",
		},
		{
			name: "update rejects empty provided api key",
			call: func() error {
				_, _, err := service.Update(context.Background(), UpdateSiteParams{
					ID:       uuid.New(),
					Name:     "Primary",
					Slug:     "primary",
					SiteType: "openai",
					BaseURL:  "https://api.example.com",
					Credentials: []CredentialInput{{
						Type:   defaultCredentialType,
						Secret: " ",
					}},
				})
				return err
			},
			wantErr: "api key must not be empty when provided",
		},
		{
			name: "set enabled requires site id",
			call: func() error {
				_, err := service.SetEnabled(context.Background(), uuid.Nil, true)
				return err
			},
			wantErr: "site id is required",
		},
		{
			name: "create api key requires site id",
			call: func() error {
				_, err := service.CreateAPIKey(context.Background(), uuid.Nil, CreateAPIKeyInput{APIKey: "sk-test"})
				return err
			},
			wantErr: "site id is required",
		},
		{
			name: "create api key requires secret",
			call: func() error {
				_, err := service.CreateAPIKey(context.Background(), uuid.New(), CreateAPIKeyInput{APIKey: " \t\n "})
				return err
			},
			wantErr: "api key is required",
		},
		{
			name: "delete api key requires site id",
			call: func() error {
				return service.DeleteAPIKey(context.Background(), uuid.Nil, uuid.New())
			},
			wantErr: "site id is required",
		},
		{
			name: "delete api key requires credential id",
			call: func() error {
				return service.DeleteAPIKey(context.Background(), uuid.New(), uuid.Nil)
			},
			wantErr: "site credential id is required",
		},
		{
			name: "set api key model enabled requires model name",
			call: func() error {
				_, err := service.SetAPIKeyModelEnabled(context.Background(), uuid.New(), uuid.New(), " \t\n ", true)
				return err
			},
			wantErr: "model name is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			assertSiteErrorContains(t, tt.name, err, tt.wantErr)
		})
	}
}

func TestSetModelEnabledPropagatesUninitializedStore(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()

	model, err := service.SetModelEnabled(context.Background(), uuid.New(), uuid.New(), true)
	if model.ID != uuid.Nil {
		t.Fatalf("SetModelEnabled model = %#v, want zero value", model)
	}
	assertSiteErrorContains(t, "SetModelEnabled", err, "store is not initialized")
}

func TestStateReadMethodsPropagateRepositoryErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("state repository offline")
	service := siteServiceWithQueryError(t, queryErr)
	ctx := context.Background()

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "site state",
			call: func() error {
				_, err := service.SiteState(ctx, uuid.New())
				return err
			},
			wantErr: "get site state",
		},
		{
			name: "site states",
			call: func() error {
				_, err := service.SiteStates(ctx)
				return err
			},
			wantErr: "list site states",
		},
		{
			name: "api key states",
			call: func() error {
				_, err := service.APIKeyStates(ctx, uuid.New())
				return err
			},
			wantErr: "list site api key states",
		},
		{
			name: "api key models",
			call: func() error {
				_, err := service.APIKeyModels(ctx, uuid.New())
				return err
			},
			wantErr: "list site api key models by credential",
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

func TestAPIKeyMutationGuardsRejectCredentialFromOtherSite(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("stop after credential guard")
	siteID := uuid.New()
	credentialID := uuid.New()
	service := siteServiceWithCredentialLookupOnly(t, store.SiteCredential{
		ID:             credentialID,
		SiteID:         uuid.New(),
		CredentialType: defaultCredentialType,
	}, queryErr)

	for _, tt := range apiKeyMutationGuardCases(service, siteID, credentialID, map[string]any{"enabled": false}) {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			assertSiteErrorContains(t, tt.name, err, "site credential was not found")
			if errors.Is(err, queryErr) {
				t.Fatalf("%s error = %v, should stop before follow-up repository access", tt.name, err)
			}
		})
	}
}

func TestAPIKeyMutationGuardsRejectNonAPIKeyCredentialType(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("stop after credential type guard")
	siteID := uuid.New()
	credentialID := uuid.New()
	service := siteServiceWithCredentialLookupOnly(t, store.SiteCredential{
		ID:             credentialID,
		SiteID:         siteID,
		CredentialType: "oauth",
	}, queryErr)

	for _, tt := range apiKeyMutationGuardCases(service, siteID, credentialID, map[string]any{"name": "primary"}) {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			assertSiteErrorContains(t, tt.name, err, "site credential is not an api key")
			if errors.Is(err, queryErr) {
				t.Fatalf("%s error = %v, should stop before follow-up repository access", tt.name, err)
			}
		})
	}
}

type apiKeyMutationGuardCase struct {
	name string
	call func() error
}

func apiKeyMutationGuardCases(service *Service, siteID uuid.UUID, credentialID uuid.UUID, metaPatch map[string]any) []apiKeyMutationGuardCase {
	return []apiKeyMutationGuardCase{
		{
			name: "patch api key meta",
			call: func() error {
				_, err := service.PatchAPIKeyMeta(context.Background(), siteID, credentialID, metaPatch)
				return err
			},
		},
		{
			name: "set api key secret",
			call: func() error {
				_, err := service.SetAPIKeySecret(context.Background(), siteID, credentialID, "sk-new")
				return err
			},
		},
	}
}
