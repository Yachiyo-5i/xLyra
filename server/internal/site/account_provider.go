package site

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	oauthsvc "xlyra/server/internal/oauth"
)

const (
	accountLoginDeviceCode = "oauth2_device_code"
	accountLoginCustomPoll = "custom_poll"
)

var (
	ErrAccountImportUnsupported = errors.New("account import is not supported")
	ErrOAuthUnavailable         = errors.New("oauth service is not available")
)

type AccountProvider interface {
	List(ctx context.Context, siteID uuid.UUID) (items any, count int, err error)
	Update(ctx context.Context, siteID, credentialID uuid.UUID, enabled *bool) (any, error)
	SetModelEnabled(ctx context.Context, siteID, credentialID uuid.UUID, model string, enabled bool) (any, error)
	SetModelEndpoint(ctx context.Context, siteID, credentialID, siteModelID uuid.UUID, mode string, endpointTypes []string) (any, error)
	Delete(ctx context.Context, siteID, credentialID uuid.UUID) error
	Refresh(ctx context.Context, siteID, credentialID uuid.UUID) (any, error)
	RefreshAll(ctx context.Context, siteID uuid.UUID) (items []AccountRefreshResult, err error)
	StartLogin(ctx context.Context, siteID uuid.UUID, params AccountLoginParams) (AccountLoginStart, error)
	PollLogin(ctx context.Context, siteID uuid.UUID, session string, targetCredentialID *uuid.UUID) (AccountLoginPoll, error)
	Regions(ctx context.Context, siteID uuid.UUID) (items any, defaultID string, err error)
	Import(ctx context.Context, siteID uuid.UUID, files []AccountImportFile) (items any, err error)
}

type AccountRefreshResult struct {
	CredentialID uuid.UUID `json:"credential_id"`
	Account      any       `json:"account,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type AccountLoginStart struct {
	Kind                    string    `json:"kind"`
	Session                 string    `json:"session"`
	UserCode                string    `json:"user_code,omitempty"`
	VerificationURI         string    `json:"verification_uri,omitempty"`
	VerificationURIComplete string    `json:"verification_uri_complete,omitempty"`
	AuthURL                 string    `json:"auth_url,omitempty"`
	Interval                int       `json:"interval"`
	ExpiresAt               time.Time `json:"expires_at"`
	Region                  string    `json:"region,omitempty"`
}

type AccountLoginPoll struct {
	Status   string `json:"status"`
	Interval int    `json:"interval,omitempty"`
	Account  any    `json:"account,omitempty"`
}

type AccountLoginParams struct {
	Region string `json:"region"`
}

type AccountImportFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type AccountRegion struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	LabelEN string `json:"label_en"`
}

func (s *Service) AccountProviderFor(siteType string) (AccountProvider, bool) {
	desc, ok := LookupSiteType(siteType)
	if !ok || desc.Accounts == nil {
		return nil, false
	}
	switch desc.SiteType {
	case "grok":
		return grokAccountAPI{s: s}, true
	default:
		return s.pluginAccountProvider(desc.SiteType)
	}
}

func accountRefreshResult[T any](id uuid.UUID, account *T, errText string) AccountRefreshResult {
	result := AccountRefreshResult{CredentialID: id, Error: errText}
	if account != nil {
		result.Account = account
	}
	return result
}

func (s *Service) accountOAuth() (*oauthsvc.Service, error) {
	if s == nil || s.oauth == nil {
		return nil, ErrOAuthUnavailable
	}
	return s.oauth, nil
}

type grokAccountAPI struct {
	s *Service
}

func (p grokAccountAPI) List(ctx context.Context, siteID uuid.UUID) (any, int, error) {
	accounts, err := p.s.ListGrokAccounts(ctx, siteID)
	if err != nil {
		return nil, 0, err
	}
	if accounts == nil {
		accounts = []GrokAccount{}
	}
	return accounts, len(accounts), nil
}

func (p grokAccountAPI) Update(ctx context.Context, siteID, credentialID uuid.UUID, enabled *bool) (any, error) {
	account, err := p.s.UpdateGrokAccount(ctx, siteID, credentialID, GrokAccountUpdate{Enabled: enabled})
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p grokAccountAPI) SetModelEnabled(ctx context.Context, siteID, credentialID uuid.UUID, model string, enabled bool) (any, error) {
	account, err := p.s.SetGrokAccountModelEnabled(ctx, siteID, credentialID, model, enabled)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p grokAccountAPI) SetModelEndpoint(ctx context.Context, siteID, credentialID, siteModelID uuid.UUID, mode string, endpointTypes []string) (any, error) {
	account, err := p.s.SetGrokAccountModelEndpoint(ctx, siteID, credentialID, siteModelID, mode, endpointTypes)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p grokAccountAPI) Delete(ctx context.Context, siteID, credentialID uuid.UUID) error {
	return p.s.DeleteGrokAccount(ctx, siteID, credentialID)
}

func (p grokAccountAPI) Refresh(ctx context.Context, siteID, credentialID uuid.UUID) (any, error) {
	account, err := p.s.RefreshGrokAccount(ctx, siteID, credentialID)
	if err != nil {
		return nil, err
	}
	return account, nil
}

func (p grokAccountAPI) RefreshAll(ctx context.Context, siteID uuid.UUID) ([]AccountRefreshResult, error) {
	items, err := p.s.RefreshGrokAccounts(ctx, siteID)
	if err != nil {
		return nil, err
	}
	results := make([]AccountRefreshResult, 0, len(items))
	for _, item := range items {
		results = append(results, accountRefreshResult(item.CredentialID, item.Account, item.Error))
	}
	return results, nil
}

func (p grokAccountAPI) StartLogin(ctx context.Context, siteID uuid.UUID, _ AccountLoginParams) (AccountLoginStart, error) {
	if err := p.s.requireGrokSite(ctx, siteID); err != nil {
		return AccountLoginStart{}, err
	}
	oauth, err := p.s.accountOAuth()
	if err != nil {
		return AccountLoginStart{}, err
	}
	auth, err := oauth.StartGrokDeviceAuth(ctx, siteID)
	if err != nil {
		return AccountLoginStart{}, err
	}
	return AccountLoginStart{
		Kind:                    accountLoginDeviceCode,
		Session:                 auth.DeviceCode,
		UserCode:                auth.UserCode,
		VerificationURI:         auth.VerificationURI,
		VerificationURIComplete: auth.VerificationURIComplete,
		Interval:                auth.Interval,
		ExpiresAt:               auth.ExpiresAt,
	}, nil
}

func (p grokAccountAPI) PollLogin(ctx context.Context, siteID uuid.UUID, session string, targetCredentialID *uuid.UUID) (AccountLoginPoll, error) {
	if err := p.s.requireGrokSite(ctx, siteID); err != nil {
		return AccountLoginPoll{}, err
	}
	session = strings.TrimSpace(session)
	if session == "" {
		return AccountLoginPoll{}, fmt.Errorf("session is required")
	}
	oauth, err := p.s.accountOAuth()
	if err != nil {
		return AccountLoginPoll{}, err
	}
	tokens, err := oauth.PollGrokDeviceToken(ctx, siteID, session)
	if err != nil {
		switch {
		case errors.Is(err, oauthsvc.ErrGrokAuthorizationPending):
			return AccountLoginPoll{Status: "pending"}, nil
		case errors.Is(err, oauthsvc.ErrGrokSlowDown):
			return AccountLoginPoll{Status: "slow_down"}, nil
		default:
			return AccountLoginPoll{}, err
		}
	}
	account, err := p.s.CompleteGrokDeviceLogin(ctx, siteID, tokens, targetCredentialID)
	if err != nil {
		return AccountLoginPoll{}, err
	}
	if refreshed, refreshErr := p.s.RefreshGrokAccount(ctx, siteID, account.CredentialID); refreshErr == nil {
		account = refreshed
	}
	return AccountLoginPoll{Status: "complete", Account: account}, nil
}

func (p grokAccountAPI) Regions(context.Context, uuid.UUID) (any, string, error) {
	return []AccountRegion{}, "", nil
}

func (p grokAccountAPI) Import(context.Context, uuid.UUID, []AccountImportFile) (any, error) {
	return nil, ErrAccountImportUnsupported
}
