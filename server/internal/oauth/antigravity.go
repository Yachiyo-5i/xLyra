package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"xlyra/server/internal/store"
)

const (
	antigravityProvider              = "antigravity"
	antigravityDefaultBackendBaseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	antigravityAuthorizeURL          = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityTokenURL              = "https://oauth2.googleapis.com/token"
	antigravityUserInfoURL           = "https://www.googleapis.com/oauth2/v2/userinfo"
	antigravityDefaultClientID       = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	antigravityDefaultClientSecret   = "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"
	antigravityDefaultClientKey      = "antigravity_enterprise"
	antigravityCallbackPort          = 1456
	antigravityRedirectURI           = "http://localhost:1456/oauth/antigravity/callback"
	antigravitySuccessHTML           = `<html><head><meta charset="utf-8"><title>Antigravity login complete</title></head><body><h1>Antigravity login complete</h1><p>You can close this window.</p></body></html>`
	antigravityRefreshLead           = 15 * time.Minute
	antigravityUserAgent             = "antigravity/1.0.0 xLyra/1.0"
)

var antigravityScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

type antigravityOAuthClient struct {
	Key          string
	ClientID     string
	ClientSecret string
}

type antigravityTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}

type antigravityUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
}

func antigravityOAuthClientFromEnv(clientKey string) antigravityOAuthClient {
	key := strings.TrimSpace(clientKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ANTIGRAVITY_OAUTH_CLIENT_KEY"))
	}
	if key == "" {
		key = antigravityDefaultClientKey
	}
	return antigravityOAuthClient{
		Key:          key,
		ClientID:     firstNonEmptyString(os.Getenv("ANTIGRAVITY_OAUTH_CLIENT_ID"), antigravityDefaultClientID),
		ClientSecret: firstNonEmptyString(os.Getenv("ANTIGRAVITY_OAUTH_CLIENT_SECRET"), antigravityDefaultClientSecret),
	}
}

func antigravityAuthorizeLink(state string, redirectURI string, client antigravityOAuthClient) string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", client.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", strings.Join(antigravityScopes, " "))
	query.Set("access_type", "offline")
	query.Set("prompt", "consent")
	query.Set("include_granted_scopes", "true")
	query.Set("state", state)
	return antigravityAuthorizeURL + "?" + query.Encode()
}

func (s *Service) exchangeAntigravityCode(ctx context.Context, code string, redirectURI string, client antigravityOAuthClient) (antigravityTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", client.ClientID)
	form.Set("client_secret", client.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return antigravityTokenResponse{}, fmt.Errorf("create antigravity token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return antigravityTokenResponse{}, fmt.Errorf("exchange antigravity code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return antigravityTokenResponse{}, fmt.Errorf("antigravity token exchange returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp antigravityTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return antigravityTokenResponse{}, fmt.Errorf("decode antigravity token exchange: %w", err)
	}
	if strings.TrimSpace(tokenResp.RefreshToken) == "" {
		return antigravityTokenResponse{}, fmt.Errorf("google did not return refresh_token; revoke prior app authorization and retry")
	}
	return tokenResp, nil
}

func (s *Service) refreshAntigravityTokens(ctx context.Context, refreshToken string, client antigravityOAuthClient, httpClient *http.Client) (antigravityTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", client.ClientID)
	form.Set("client_secret", client.ClientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return antigravityTokenResponse{}, fmt.Errorf("create antigravity refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", antigravityUserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return antigravityTokenResponse{}, fmt.Errorf("refresh antigravity token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return antigravityTokenResponse{}, fmt.Errorf("antigravity token refresh returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp antigravityTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return antigravityTokenResponse{}, fmt.Errorf("decode antigravity token refresh: %w", err)
	}
	return tokenResp, nil
}

func (s *Service) fetchAntigravityUserInfo(ctx context.Context, accessToken string, httpClient *http.Client) (antigravityUserInfo, map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, antigravityUserInfoURL, nil)
	if err != nil {
		return antigravityUserInfo{}, nil, fmt.Errorf("create antigravity userinfo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", antigravityUserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return antigravityUserInfo{}, nil, fmt.Errorf("fetch antigravity userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return antigravityUserInfo{}, nil, fmt.Errorf("antigravity userinfo returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return antigravityUserInfo{}, nil, fmt.Errorf("read antigravity userinfo: %w", err)
	}
	var info antigravityUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return antigravityUserInfo{}, nil, fmt.Errorf("decode antigravity userinfo: %w", err)
	}
	raw := map[string]any{}
	_ = json.Unmarshal(body, &raw)
	return info, raw, nil
}

func antigravityTokenExpiry(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}

func (s *Service) refreshAntigravityConnection(ctx context.Context, repo store.OAuthConnectionRepository, connection store.OAuthConnection) (CodexConnection, error) {
	if strings.TrimSpace(connection.EncryptedRefreshToken) == "" {
		if connection.ExpiresAt.Valid && !connection.ExpiresAt.Time.After(time.Now()) {
			return CodexConnection{}, fmt.Errorf("oauth connection has no refresh_token and access token has expired; re-import or sign in again")
		}
		return s.codexConnectionDetails(connection)
	}
	refreshToken, err := s.credentials.Decrypt(connection.EncryptedRefreshToken)
	if err != nil {
		return CodexConnection{}, err
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	client := antigravityOAuthClientFromEnv(stringFromAny(meta["oauth_client_key"]))
	httpClient, err := s.httpClientForConnection(ctx, connection)
	if err != nil {
		return CodexConnection{}, err
	}
	refreshed, err := s.refreshAntigravityTokens(ctx, refreshToken, client, httpClient)
	if err != nil {
		errMsg := err.Error()
		connection.Status = "reconnect_required"
		connection.Metadata = store.JSON(updateMetadataError(connection.Metadata, errMsg))
		_, _ = repo.Save(ctx, connection)
		s.disableSiteOnPermanentError(ctx, connection, errMsg)
		return CodexConnection{}, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = refreshToken
	}
	userInfo, rawProfile, err := s.fetchAntigravityUserInfo(ctx, refreshed.AccessToken, httpClient)
	if err != nil {
		return CodexConnection{}, err
	}
	now := time.Now()
	expiresAt := antigravityTokenExpiry(now, refreshed.ExpiresIn)
	meta["oauth_client_key"] = client.Key
	meta["name"] = strings.TrimSpace(userInfo.Name)
	meta["provider"] = antigravityProvider
	connection.Metadata = jsonBytes(meta)
	connection.RawProfile = jsonBytes(rawProfile)
	connection.Email = strings.TrimSpace(userInfo.Email)
	connection.AccountID = firstNonEmptyString(userInfo.ID, userInfo.Email)
	connection.TokenType = defaultTokenType(refreshed.TokenType)
	connection.Scopes = strings.TrimSpace(refreshed.Scope)
	connection.Status = "connected"
	connection.LastRefreshAt = sql.NullTime{Time: now, Valid: true}
	connection.ExpiresAt = sql.NullTime{Time: expiresAt, Valid: !expiresAt.IsZero()}
	if connection.EncryptedAccessToken, connection.MaskedAccessToken, err = s.credentials.Encrypt(refreshed.AccessToken); err != nil {
		return CodexConnection{}, err
	}
	if connection.EncryptedRefreshToken, connection.MaskedRefreshToken, err = s.credentials.Encrypt(refreshed.RefreshToken); err != nil {
		return CodexConnection{}, err
	}
	connection, err = repo.Save(ctx, connection)
	if err != nil {
		return CodexConnection{}, err
	}
	return s.codexConnectionDetails(connection)
}
