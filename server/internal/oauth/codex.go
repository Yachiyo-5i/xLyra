package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	codexProvider              = "codex"
	codexDefaultBackendBaseURL = "https://chatgpt.com/backend-api"
	codexAuthorizeURL          = "https://auth.openai.com/oauth/authorize"
	codexTokenURL              = "https://auth.openai.com/oauth/token"
	codexClientID              = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexScope                 = "openid email profile offline_access"
	codexCallbackPort          = 1455
	codexRedirectURI           = "http://localhost:1455/auth/callback"
	codexSuccessHTML           = `<html><head><meta charset="utf-8"><title>Codex login complete</title></head><body><h1>Codex login complete</h1><p>You can close this window.</p></body></html>`
	codexRefreshLead           = 5 * time.Minute
	codexRateLimitUsagePath    = "/wham/usage"
	codexModelsPrimaryPath     = "/codex/models"
	codexModelsFallbackPath    = "/v1/models"
)

type codexTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}

type codexJWTClaims struct {
	Email         string             `json:"email"`
	EmailVerified bool               `json:"email_verified"`
	Exp           int64              `json:"exp"`
	AuthInfo      codexJWTClaimsAuth `json:"https://api.openai.com/auth"`
}

type codexJWTClaimsAuth struct {
	ChatGPTAccountID               string `json:"chatgpt_account_id"`
	ChatGPTPlanType                string `json:"chatgpt_plan_type"`
	ChatGPTSubscriptionActiveStart any    `json:"chatgpt_subscription_active_start"`
	ChatGPTSubscriptionActiveUntil any    `json:"chatgpt_subscription_active_until"`
	ChatGPTUserID                  string `json:"chatgpt_user_id"`
	UserID                         string `json:"user_id"`
}

type codexUsageWindow struct {
	Name              string `json:"name"`
	UsedPercent       int    `json:"used_percent"`
	RemainingPercent  int    `json:"remaining_percent"`
	ResetAt           any    `json:"reset_at"`
	LimitWindowSecond int    `json:"limit_window_seconds"`
}

type codexUsageSnapshot struct {
	PlanType string            `json:"plan_type,omitempty"`
	FiveHour *codexUsageWindow `json:"five_hour,omitempty"`
	Weekly   *codexUsageWindow `json:"weekly,omitempty"`
	Credits  any               `json:"credits,omitempty"`
	Raw      map[string]any    `json:"raw,omitempty"`
	Meta     map[string]any    `json:"meta,omitempty"`
}

type codexModelResponse struct {
	Data []map[string]any `json:"data"`
}

func newPKCEVerifier() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate pkce verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newOAuthState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate oauth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func codexChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func codexAuthorizeLink(state string, verifier string, redirectURI string) string {
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", codexClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", codexScope)
	query.Set("code_challenge", codexChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("prompt", "login")
	query.Set("id_token_add_organizations", "true")
	query.Set("codex_cli_simplified_flow", "true")
	return codexAuthorizeURL + "?" + query.Encode()
}

func (s *Service) exchangeCodexCode(ctx context.Context, code string, redirectURI string, verifier string) (codexTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", codexClientID)
	form.Set("code_verifier", verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return codexTokenResponse{}, fmt.Errorf("create codex token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return codexTokenResponse{}, fmt.Errorf("exchange codex code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return codexTokenResponse{}, fmt.Errorf("codex token exchange returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp codexTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return codexTokenResponse{}, fmt.Errorf("decode codex token exchange: %w", err)
	}
	return tokenResp, nil
}

func (s *Service) refreshCodexTokens(ctx context.Context, refreshToken string, httpClient *http.Client) (codexTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", codexClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return codexTokenResponse{}, fmt.Errorf("create codex refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return codexTokenResponse{}, fmt.Errorf("refresh codex token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return codexTokenResponse{}, fmt.Errorf("codex token refresh returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp codexTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return codexTokenResponse{}, fmt.Errorf("decode codex token refresh: %w", err)
	}
	return tokenResp, nil
}

func parseCodexIDToken(idToken string) (codexJWTClaims, map[string]any, error) {
	parts := strings.Split(strings.TrimSpace(idToken), ".")
	if len(parts) != 3 {
		return codexJWTClaims{}, nil, fmt.Errorf("invalid id_token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return codexJWTClaims{}, nil, fmt.Errorf("decode id_token payload: %w", err)
	}
	var claims codexJWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return codexJWTClaims{}, nil, fmt.Errorf("unmarshal id_token payload: %w", err)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return codexJWTClaims{}, nil, fmt.Errorf("unmarshal id_token raw payload: %w", err)
	}
	return claims, raw, nil
}

func codexTokenExpiry(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}
