package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/store"
)

const (
	grokDeviceCodeURL = "https://auth.x.ai/oauth2/device/code"
	grokTokenURL      = "https://auth.x.ai/oauth2/token"
	grokClientID      = "b1a00492-073a-47ea-816f-4c329264a828"
	grokScope         = "openid profile email offline_access grok-cli:access api:access"
	grokDeviceGrant   = "urn:ietf:params:oauth:grant-type:device_code"
	grokRefreshLead   = 5 * time.Minute
)

type GrokDeviceAuth struct {
	DeviceCode              string    `json:"device_code"`
	UserCode                string    `json:"user_code"`
	VerificationURI         string    `json:"verification_uri"`
	VerificationURIComplete string    `json:"verification_uri_complete"`
	Interval                int       `json:"interval"`
	ExpiresAt               time.Time `json:"expires_at"`
}

type GrokTokens struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresAt    time.Time
	Claims       map[string]any
	Email        string
	Tier         string
	TeamID       string
	PrincipalID  string
}

type grokTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func grokRequestHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-grok-client-surface", adapter.GrokClientSurface)
	req.Header.Set("x-grok-client-version", adapter.GrokClientVersion)
	req.Header.Set("User-Agent", adapter.GrokUserAgent)
}

// grokHTTPClient returns the HTTP client to use for grok auth requests bound to
// the given site. When the site configures a proxy (proxy_id in its meta), every
// grok request must carry that proxy; a configured-but-unusable proxy is a hard
// error rather than a silent direct connection. Sites without a proxy (or an
// unresolved siteID) fall back to the default client.
func (s *Service) grokHTTPClient(ctx context.Context, siteID uuid.UUID) (*http.Client, error) {
	if siteID == uuid.Nil || s.db == nil {
		return s.httpClient, nil
	}
	site, err := store.NewSiteRepository(s.db.DB()).GetByID(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("load grok site for proxy: %w", err)
	}
	meta := map[string]any{}
	if len(site.Meta) > 0 {
		_ = json.Unmarshal(site.Meta, &meta)
	}
	proxyID := ""
	if value, ok := meta["proxy_id"].(string); ok {
		proxyID = strings.TrimSpace(value)
	}
	if proxyID == "" {
		return s.httpClient, nil
	}
	profile := httpclient.DefaultProfile()
	profile.ProxyID = proxyID
	client, err := s.httpClients.Client(profile)
	if err != nil {
		return nil, fmt.Errorf("build grok proxy client: %w", err)
	}
	if client == nil {
		return s.httpClient, nil
	}
	return client, nil
}

func (s *Service) StartGrokDeviceAuth(ctx context.Context, siteID uuid.UUID) (GrokDeviceAuth, error) {
	client, err := s.grokHTTPClient(ctx, siteID)
	if err != nil {
		return GrokDeviceAuth{}, err
	}
	form := url.Values{}
	form.Set("client_id", grokClientID)
	form.Set("scope", grokScope)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grokDeviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return GrokDeviceAuth{}, fmt.Errorf("create grok device code request: %w", err)
	}
	grokRequestHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return GrokDeviceAuth{}, fmt.Errorf("request grok device code: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GrokDeviceAuth{}, fmt.Errorf("grok device code returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return GrokDeviceAuth{}, fmt.Errorf("decode grok device code: %w", err)
	}
	if strings.TrimSpace(decoded.DeviceCode) == "" || strings.TrimSpace(decoded.UserCode) == "" {
		return GrokDeviceAuth{}, fmt.Errorf("grok device code response was incomplete")
	}
	interval := decoded.Interval
	if interval <= 0 {
		interval = 5
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	if decoded.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second)
	}
	return GrokDeviceAuth{
		DeviceCode:              decoded.DeviceCode,
		UserCode:                decoded.UserCode,
		VerificationURI:         decoded.VerificationURI,
		VerificationURIComplete: decoded.VerificationURIComplete,
		Interval:                interval,
		ExpiresAt:               expiresAt,
	}, nil
}

var ErrGrokAuthorizationPending = fmt.Errorf("grok authorization pending")

var ErrGrokSlowDown = fmt.Errorf("grok slow down")

var ErrGrokReauthorizationRequired = errors.New("grok reauthorization required")

func (s *Service) PollGrokDeviceToken(ctx context.Context, siteID uuid.UUID, deviceCode string) (GrokTokens, error) {
	client, err := s.grokHTTPClient(ctx, siteID)
	if err != nil {
		return GrokTokens{}, err
	}
	form := url.Values{}
	form.Set("grant_type", grokDeviceGrant)
	form.Set("device_code", strings.TrimSpace(deviceCode))
	form.Set("client_id", grokClientID)
	return s.grokTokenRequest(ctx, client, form)
}

func (s *Service) RefreshGrokTokens(ctx context.Context, siteID uuid.UUID, refreshToken string) (GrokTokens, error) {
	client, err := s.grokHTTPClient(ctx, siteID)
	if err != nil {
		return GrokTokens{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", strings.TrimSpace(refreshToken))
	form.Set("client_id", grokClientID)
	form.Set("scope", grokScope)
	return s.grokTokenRequest(ctx, client, form)
}

func (s *Service) grokTokenRequest(ctx context.Context, client *http.Client, form url.Values) (GrokTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grokTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return GrokTokens{}, fmt.Errorf("create grok token request: %w", err)
	}
	grokRequestHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return GrokTokens{}, fmt.Errorf("grok token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var decoded grokTokenResponse
	_ = json.Unmarshal(body, &decoded)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch strings.TrimSpace(decoded.Error) {
		case "authorization_pending":
			return GrokTokens{}, ErrGrokAuthorizationPending
		case "slow_down":
			return GrokTokens{}, ErrGrokSlowDown
		}
		msg := strings.TrimSpace(decoded.ErrorDesc)
		if msg == "" {
			msg = strings.TrimSpace(decoded.Error)
		}
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if form.Get("grant_type") == "refresh_token" && (resp.StatusCode == http.StatusUnauthorized || slices.Contains([]string{"invalid_grant", "invalid_token", "unauthorized_client"}, strings.TrimSpace(decoded.Error))) {
			return GrokTokens{}, fmt.Errorf("%w: %s", ErrGrokReauthorizationRequired, msg)
		}
		return GrokTokens{}, fmt.Errorf("grok token request returned %d: %s", resp.StatusCode, msg)
	}
	if strings.TrimSpace(decoded.AccessToken) == "" {
		return GrokTokens{}, fmt.Errorf("grok token response missing access_token")
	}
	tokens := GrokTokens{
		AccessToken:  decoded.AccessToken,
		RefreshToken: decoded.RefreshToken,
		TokenType:    decoded.TokenType,
		Scope:        decoded.Scope,
		ExpiresAt:    grokTokenExpiry(time.Now(), decoded.ExpiresIn),
	}
	claims, _ := parseGrokAccessClaims(decoded.AccessToken)
	if claims != nil {
		tokens.Claims = claims
		tokens.Email = grokClaimString(claims, "email")
		tokens.Tier = grokClaimString(claims, "tier")
		tokens.PrincipalID, tokens.TeamID = GrokTokenIdentity(decoded.AccessToken)
		if tokens.ExpiresAt.IsZero() {
			if exp, ok := claims["exp"].(float64); ok && exp > 0 {
				tokens.ExpiresAt = time.Unix(int64(exp), 0)
			}
		}
	}
	return tokens, nil
}

func GrokTokenIdentity(token string) (string, string) {
	claims, err := parseGrokAccessClaims(token)
	if err != nil {
		return "", ""
	}
	principalID := grokClaimString(claims, "principal_id")
	if principalID == "" {
		principalID = grokClaimString(claims, "sub")
	}
	return principalID, grokClaimString(claims, "team_id")
}

func parseGrokAccessClaims(token string) (map[string]any, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid grok access token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode grok access token payload: %w", err)
	}
	claims := map[string]any{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal grok access token payload: %w", err)
	}
	return claims, nil
}

func grokClaimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	if value, ok := claims[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func grokTokenExpiry(now time.Time, expiresIn int) time.Time {
	if expiresIn <= 0 {
		return time.Time{}
	}
	return now.Add(time.Duration(expiresIn) * time.Second)
}

type GrokCredentialBundle struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	TokenType    string `json:"token_type,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func GrokBundleFromTokens(t GrokTokens) GrokCredentialBundle {
	exp := int64(0)
	if !t.ExpiresAt.IsZero() {
		exp = t.ExpiresAt.Unix()
	}
	return GrokCredentialBundle{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    exp,
		TokenType:    t.TokenType,
		Scope:        t.Scope,
	}
}

func EncodeGrokBundle(b GrokCredentialBundle) (string, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return "", fmt.Errorf("encode grok credential bundle: %w", err)
	}
	return string(raw), nil
}

func DecodeGrokBundle(secret string) (GrokCredentialBundle, error) {
	var bundle GrokCredentialBundle
	if err := json.Unmarshal([]byte(strings.TrimSpace(secret)), &bundle); err != nil {
		return GrokCredentialBundle{}, fmt.Errorf("decode grok credential bundle: %w", err)
	}
	return bundle, nil
}

func (s *Service) EnsureGrokAccessToken(ctx context.Context, credentialID uuid.UUID) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("oauth store is not available")
	}
	repo := store.NewSiteCredentialRepository(s.db.DB())
	cred, err := repo.GetByID(ctx, credentialID)
	if err != nil {
		return "", fmt.Errorf("load grok credential: %w", err)
	}
	decrypted, err := s.credentials.Decrypt(cred.EncryptedSecret)
	if err != nil {
		return "", fmt.Errorf("decrypt grok credential: %w", err)
	}
	bundle, err := DecodeGrokBundle(decrypted)
	if err != nil {
		return "", err
	}
	if bundle.AccessToken != "" && grokBundleFresh(bundle) {
		return bundle.AccessToken, nil
	}
	if strings.TrimSpace(bundle.RefreshToken) == "" {
		if strings.TrimSpace(bundle.AccessToken) == "" {
			return "", fmt.Errorf("grok credential is missing tokens")
		}
		return "", fmt.Errorf("%w: access token is expired or has unknown expiry and no refresh token is available", ErrGrokReauthorizationRequired)
	}
	result, err, _ := s.refreshGroup.Do("grok:"+credentialID.String(), func() (any, error) {
		return s.refreshGrokCredentialOnce(ctx, cred.ID, cred.SiteID, bundle)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

func grokBundleFresh(bundle GrokCredentialBundle) bool {
	expiresAt := bundle.ExpiresAt
	if expiresAt <= 0 {
		claims, err := parseGrokAccessClaims(bundle.AccessToken)
		if err != nil {
			return false
		}
		if exp, ok := claims["exp"].(float64); ok {
			expiresAt = int64(exp)
		}
		if expiresAt <= 0 {
			return false
		}
	}
	return time.Until(time.Unix(expiresAt, 0)) > grokRefreshLead
}

func (s *Service) refreshGrokCredentialOnce(ctx context.Context, credentialID uuid.UUID, siteID uuid.UUID, current GrokCredentialBundle) (string, error) {
	tokens, err := s.RefreshGrokTokens(ctx, siteID, current.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh grok token: %w", err)
	}
	next := GrokBundleFromTokens(tokens)
	if strings.TrimSpace(next.RefreshToken) == "" {
		next.RefreshToken = current.RefreshToken
	}
	encoded, err := EncodeGrokBundle(next)
	if err != nil {
		return "", err
	}
	encrypted, _, err := s.credentials.Encrypt(encoded)
	if err != nil {
		return "", fmt.Errorf("encrypt grok credential: %w", err)
	}
	if _, err := store.NewSiteCredentialRepository(s.db.DB()).UpdateSecret(ctx, credentialID, encrypted); err != nil {
		return "", err
	}
	return next.AccessToken, nil
}
