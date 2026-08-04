package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
)

const (
	claudeCodeProvider              = "claude_code"
	claudeCodeDefaultBackendBaseURL = "https://api.anthropic.com"
	claudeCodeAuthorizeURL          = "https://claude.com/cai/oauth/authorize"
	claudeCodeTokenURL              = "https://platform.claude.com/v1/oauth/token"
	claudeCodeProfileURL            = "https://api.anthropic.com/api/oauth/profile"
	claudeCodeClientID              = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	claudeCodeRedirectURI           = "https://platform.claude.com/oauth/code/callback"
	claudeCodeRefreshLead           = 5 * time.Minute
)

var claudeCodeScopes = []string{
	"org:create_api_key",
	"user:profile",
	"user:inference",
	"user:sessions:claude_code",
	"user:mcp_servers",
	"user:file_upload",
}

type claudeCodeTokenResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	TokenType             string `json:"token_type"`
	Scope                 string `json:"scope"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Account               struct {
		UUID         string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account"`
	Organization struct {
		UUID string `json:"uuid"`
	} `json:"organization"`
}

type claudeCodeProfile struct {
	Account struct {
		UUID        string `json:"uuid"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
	} `json:"account"`
	Organization struct {
		UUID                        string         `json:"uuid"`
		OrganizationType            string         `json:"organization_type"`
		RateLimitTier               string         `json:"rate_limit_tier"`
		SeatTier                    string         `json:"seat_tier"`
		BillingType                 string         `json:"billing_type"`
		HasExtraUsageEnabled        bool           `json:"has_extra_usage_enabled"`
		ClaudeCodeOnboardingFlags   map[string]any `json:"cc_onboarding_flags"`
		ClaudeCodeTrialEndsAt       any            `json:"claude_code_trial_ends_at"`
		ClaudeCodeTrialDurationDays any            `json:"claude_code_trial_duration_days"`
		SubscriptionCreatedAt       string         `json:"subscription_created_at"`
	} `json:"organization"`
}

func claudeCodeAuthorizeLink(state string, verifier string) string {
	query := url.Values{}
	query.Set("code", "true")
	query.Set("client_id", claudeCodeClientID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", claudeCodeRedirectURI)
	query.Set("scope", strings.Join(claudeCodeScopes, " "))
	query.Set("code_challenge", codexChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	return claudeCodeAuthorizeURL + "?" + query.Encode()
}

func parseClaudeCodeAuthorizationResult(raw string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), "#", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("authorization_result must contain code#state")
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func (s *Service) exchangeClaudeCode(ctx context.Context, code string, state string, verifier string, httpClient *http.Client) (claudeCodeTokenResponse, error) {
	payload := map[string]any{
		"grant_type":    "authorization_code",
		"code":          strings.TrimSpace(code),
		"redirect_uri":  claudeCodeRedirectURI,
		"client_id":     claudeCodeClientID,
		"code_verifier": verifier,
		"state":         strings.TrimSpace(state),
	}
	return s.claudeCodeTokenRequest(ctx, payload, httpClient, "exchange claude code authorization")
}

func (s *Service) refreshClaudeCodeTokens(ctx context.Context, refreshToken string, scopes string, httpClient *http.Client) (claudeCodeTokenResponse, error) {
	scope := strings.TrimSpace(scopes)
	if scope == "" {
		scope = strings.Join(claudeCodeScopes, " ")
	}
	payload := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": strings.TrimSpace(refreshToken),
		"client_id":     claudeCodeClientID,
		"scope":         scope,
	}
	return s.claudeCodeTokenRequest(ctx, payload, httpClient, "refresh claude code token")
}

func (s *Service) claudeCodeTokenRequest(ctx context.Context, payload map[string]any, httpClient *http.Client, action string) (claudeCodeTokenResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return claudeCodeTokenResponse{}, fmt.Errorf("%s: %w", action, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, claudeCodeTokenURL, strings.NewReader(string(body)))
	if err != nil {
		return claudeCodeTokenResponse{}, fmt.Errorf("%s: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", adapter.ClaudeCodeUserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return claudeCodeTokenResponse{}, fmt.Errorf("%s: %w", action, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return claudeCodeTokenResponse{}, fmt.Errorf("%s returned %d: %s", action, resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result claudeCodeTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return claudeCodeTokenResponse{}, fmt.Errorf("decode claude code token response: %w", err)
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return claudeCodeTokenResponse{}, fmt.Errorf("claude code token response missing access_token")
	}
	return result, nil
}

func (s *Service) fetchClaudeCodeProfile(ctx context.Context, accessToken string, httpClient *http.Client) (claudeCodeProfile, map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeCodeProfileURL, nil)
	if err != nil {
		return claudeCodeProfile{}, nil, fmt.Errorf("create claude code profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("User-Agent", adapter.ClaudeCodeUserAgent)
	req.Header.Set("X-App", "cli")
	req.Header.Set("anthropic-beta", adapter.ClaudeCodeOAuthBeta)
	req.Header.Set("anthropic-version", adapter.ClaudeCodeAPIVersion)
	resp, err := httpClient.Do(req)
	if err != nil {
		return claudeCodeProfile{}, nil, fmt.Errorf("fetch claude code profile: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return claudeCodeProfile{}, nil, fmt.Errorf("claude code profile returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return claudeCodeProfile{}, nil, fmt.Errorf("decode claude code profile: %w", err)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return claudeCodeProfile{}, nil, fmt.Errorf("marshal claude code profile: %w", err)
	}
	var profile claudeCodeProfile
	if err := json.Unmarshal(encoded, &profile); err != nil {
		return claudeCodeProfile{}, nil, fmt.Errorf("unmarshal claude code profile: %w", err)
	}
	return profile, raw, nil
}

func claudeCodeTokenExpiry(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}
