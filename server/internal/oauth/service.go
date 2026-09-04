package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	"xlyra/server/internal/credential"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/store"
)

type Service struct {
	db           *store.Store
	credentials  credential.Service
	httpClient   *http.Client
	httpClients  *httpclient.Manager
	masterKey    string
	refreshGroup singleflight.Group
	refreshLock  func(context.Context, uuid.UUID, func() (CodexConnection, error)) (CodexConnection, error)
}

type PendingSite struct {
	SiteID          uuid.UUID       `json:"site_id,omitempty"`
	Name            string          `json:"name,omitempty"`
	Slug            string          `json:"slug,omitempty"`
	BaseURL         string          `json:"base_url,omitempty"`
	Enabled         bool            `json:"enabled"`
	RoutingPriority *float64        `json:"routing_priority,omitempty"`
	ProxyID         *string         `json:"proxy_id,omitempty"`
	GatewayConfig   json.RawMessage `json:"gateway_config,omitempty"`
}

type StartCodexFlowParams struct {
	PublicBaseURL      string
	SuccessRedirectURL string
	FailureRedirectURL string
	Site               PendingSite
	Metadata           map[string]any
}

type StartCodexFlowResult struct {
	SessionID      uuid.UUID
	State          string
	AuthorizeURL   string
	CallbackURL    string
	CallbackPort   int
	RelayTargetURL string
	ExpiresAt      time.Time
}

type StartAntigravityFlowParams = StartCodexFlowParams

type StartAntigravityFlowResult = StartCodexFlowResult

type StartClaudeCodeFlowParams = StartCodexFlowParams

type StartClaudeCodeFlowResult = StartCodexFlowResult

type CodexConnection struct {
	Connection   store.OAuthConnection
	AccessToken  string
	RefreshToken string
	IDToken      string
	Claims       map[string]any
	Metadata     map[string]any
	AccountID    string
	Email        string
	PlanType     string
	Quota        map[string]any
	Models       []map[string]any
}

func NewService(db *store.Store, masterKey string, confFiles ...*config.ConfigFile) *Service {
	var confFile *config.ConfigFile
	if len(confFiles) > 0 {
		confFile = confFiles[0]
	}
	manager := httpclient.NewManager(confFile)
	client, _ := manager.Client(httpclient.DefaultProfile())
	service := &Service{
		db:          db,
		credentials: credential.NewService(masterKey),
		httpClient:  client,
		httpClients: manager,
		masterKey:   masterKey,
	}
	service.refreshLock = service.withRefreshConnectionLock
	return service
}

func (s *Service) DB() *store.Store {
	return s.db
}

func (s *Service) MasterKey() string {
	return s.masterKey
}

func (s *Service) StartCodexFlow(ctx context.Context, params StartCodexFlowParams) (StartCodexFlowResult, error) {
	if s.db == nil {
		return StartCodexFlowResult{}, fmt.Errorf("oauth store is not available")
	}
	verifier, err := newPKCEVerifier()
	if err != nil {
		return StartCodexFlowResult{}, err
	}
	state, err := newOAuthState()
	if err != nil {
		return StartCodexFlowResult{}, err
	}
	callbackBase := normalizeCallbackBaseURL(params.PublicBaseURL)
	if callbackBase == "" {
		return StartCodexFlowResult{}, fmt.Errorf("public_base_url must be a valid backend origin")
	}
	relayTargetURL := callbackBase + "/api/v1/oauth/providers/codex/callback"
	if err := ensureCodexCallbackRelay(s.db); err != nil {
		return StartCodexFlowResult{}, err
	}
	now := time.Now()
	expiresAt := now.Add(10 * time.Minute)
	sitePayload, err := json.Marshal(params.Site)
	if err != nil {
		return StartCodexFlowResult{}, fmt.Errorf("marshal oauth site payload: %w", err)
	}
	metadata := map[string]any{}
	for key, value := range params.Metadata {
		metadata[key] = value
	}
	metadata["public_base_url"] = callbackBase
	metadata["relay_target_url"] = relayTargetURL
	metadata["codex_redirect_uri"] = codexRedirectURI
	meta, err := json.Marshal(metadata)
	if err != nil {
		return StartCodexFlowResult{}, fmt.Errorf("marshal oauth metadata: %w", err)
	}
	session, err := store.NewOAuthSessionRepository(s.db.DB()).Create(ctx, store.CreateOAuthSessionParams{
		Provider:           codexProvider,
		State:              state,
		PKCEVerifier:       verifier,
		RedirectURI:        codexRedirectURI,
		SuccessRedirectURL: strings.TrimSpace(params.SuccessRedirectURL),
		FailureRedirectURL: strings.TrimSpace(params.FailureRedirectURL),
		SiteID:             params.Site.SiteID,
		SitePayload:        sitePayload,
		Status:             "pending",
		ExpiresAt:          expiresAt,
		Metadata:           meta,
	})
	if err != nil {
		return StartCodexFlowResult{}, err
	}
	return StartCodexFlowResult{
		SessionID:      session.ID,
		State:          state,
		AuthorizeURL:   codexAuthorizeLink(state, verifier, codexRedirectURI),
		CallbackURL:    codexRedirectURI,
		CallbackPort:   codexCallbackPort,
		RelayTargetURL: relayTargetURL,
		ExpiresAt:      expiresAt,
	}, nil
}

func (s *Service) StartAntigravityFlow(ctx context.Context, params StartAntigravityFlowParams) (StartAntigravityFlowResult, error) {
	if s.db == nil {
		return StartAntigravityFlowResult{}, fmt.Errorf("oauth store is not available")
	}
	state, err := newOAuthState()
	if err != nil {
		return StartAntigravityFlowResult{}, err
	}
	callbackBase := normalizeCallbackBaseURL(params.PublicBaseURL)
	if callbackBase == "" {
		return StartAntigravityFlowResult{}, fmt.Errorf("public_base_url must be a valid backend origin")
	}
	relayTargetURL := callbackBase + "/api/v1/oauth/providers/antigravity/callback"
	if err := ensureAntigravityCallbackRelay(s.db); err != nil {
		return StartAntigravityFlowResult{}, err
	}
	now := time.Now()
	expiresAt := now.Add(10 * time.Minute)
	sitePayload, err := json.Marshal(params.Site)
	if err != nil {
		return StartAntigravityFlowResult{}, fmt.Errorf("marshal oauth site payload: %w", err)
	}
	metadata := map[string]any{}
	for key, value := range params.Metadata {
		metadata[key] = value
	}
	client := antigravityOAuthClientFromEnv(stringFromAny(metadata["oauth_client_key"]))
	metadata["public_base_url"] = callbackBase
	metadata["relay_target_url"] = relayTargetURL
	metadata["antigravity_redirect_uri"] = antigravityRedirectURI
	metadata["oauth_client_key"] = client.Key
	meta, err := json.Marshal(metadata)
	if err != nil {
		return StartAntigravityFlowResult{}, fmt.Errorf("marshal oauth metadata: %w", err)
	}
	session, err := store.NewOAuthSessionRepository(s.db.DB()).Create(ctx, store.CreateOAuthSessionParams{
		Provider:           antigravityProvider,
		State:              state,
		RedirectURI:        antigravityRedirectURI,
		SuccessRedirectURL: strings.TrimSpace(params.SuccessRedirectURL),
		FailureRedirectURL: strings.TrimSpace(params.FailureRedirectURL),
		SiteID:             params.Site.SiteID,
		SitePayload:        sitePayload,
		Status:             "pending",
		ExpiresAt:          expiresAt,
		Metadata:           meta,
	})
	if err != nil {
		return StartAntigravityFlowResult{}, err
	}
	return StartAntigravityFlowResult{
		SessionID:      session.ID,
		State:          state,
		AuthorizeURL:   antigravityAuthorizeLink(state, antigravityRedirectURI, client),
		CallbackURL:    antigravityRedirectURI,
		CallbackPort:   antigravityCallbackPort,
		RelayTargetURL: relayTargetURL,
		ExpiresAt:      expiresAt,
	}, nil
}

func (s *Service) StartClaudeCodeFlow(ctx context.Context, params StartClaudeCodeFlowParams) (StartClaudeCodeFlowResult, error) {
	if s.db == nil {
		return StartClaudeCodeFlowResult{}, fmt.Errorf("oauth store is not available")
	}
	verifier, err := newPKCEVerifier()
	if err != nil {
		return StartClaudeCodeFlowResult{}, err
	}
	state, err := newOAuthState()
	if err != nil {
		return StartClaudeCodeFlowResult{}, err
	}
	sitePayload, err := json.Marshal(params.Site)
	if err != nil {
		return StartClaudeCodeFlowResult{}, fmt.Errorf("marshal oauth site payload: %w", err)
	}
	metadata := map[string]any{}
	for key, value := range params.Metadata {
		metadata[key] = value
	}
	metadata["claude_code_redirect_uri"] = claudeCodeRedirectURI
	meta, err := json.Marshal(metadata)
	if err != nil {
		return StartClaudeCodeFlowResult{}, fmt.Errorf("marshal oauth metadata: %w", err)
	}
	expiresAt := time.Now().Add(10 * time.Minute)
	session, err := store.NewOAuthSessionRepository(s.db.DB()).Create(ctx, store.CreateOAuthSessionParams{
		Provider:           claudeCodeProvider,
		State:              state,
		PKCEVerifier:       verifier,
		RedirectURI:        claudeCodeRedirectURI,
		SuccessRedirectURL: strings.TrimSpace(params.SuccessRedirectURL),
		FailureRedirectURL: strings.TrimSpace(params.FailureRedirectURL),
		SiteID:             params.Site.SiteID,
		SitePayload:        sitePayload,
		Status:             "pending",
		ExpiresAt:          expiresAt,
		Metadata:           meta,
	})
	if err != nil {
		return StartClaudeCodeFlowResult{}, err
	}
	return StartClaudeCodeFlowResult{
		SessionID:    session.ID,
		State:        state,
		AuthorizeURL: claudeCodeAuthorizeLink(state, verifier),
		CallbackURL:  claudeCodeRedirectURI,
		ExpiresAt:    expiresAt,
	}, nil
}

func (s *Service) HandleClaudeCodeAuthorizationResult(ctx context.Context, sessionID uuid.UUID, authorizationResult string, proxyID *string) (store.OAuthSession, store.OAuthConnection, PendingSite, error) {
	if s.db == nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth store is not available")
	}
	if sessionID == uuid.Nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("session_id is required")
	}
	code, state, err := parseClaudeCodeAuthorizationResult(authorizationResult)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	sessionRepo := store.NewOAuthSessionRepository(s.db.DB())
	session, err := sessionRepo.GetByID(ctx, sessionID)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	if session.Provider != claudeCodeProvider {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session provider mismatch")
	}
	if session.Status != "pending" {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session is no longer pending")
	}
	if time.Now().After(session.ExpiresAt) {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session has expired")
	}
	if state != session.State {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth state mismatch")
	}
	var pendingSite PendingSite
	if len(session.SitePayload) > 0 {
		_ = json.Unmarshal(session.SitePayload, &pendingSite)
	}
	if proxyID != nil {
		trimmed := strings.TrimSpace(*proxyID)
		pendingSite.ProxyID = &trimmed
	}
	httpClient, err := s.httpClientForPendingSite(ctx, pendingSite)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	token, err := s.exchangeClaudeCode(ctx, code, state, session.PKCEVerifier, httpClient)
	if err != nil {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	profile, rawProfile, err := s.fetchClaudeCodeProfile(ctx, token.AccessToken, httpClient)
	if err != nil {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	email := firstNonEmptyString(profile.Account.Email, token.Account.EmailAddress)
	accountID := firstNonEmptyString(profile.Account.UUID, token.Account.UUID)
	organizationID := firstNonEmptyString(profile.Organization.UUID, token.Organization.UUID)
	if email == "" || accountID == "" {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("claude code profile is missing account identity")
	}
	accessEncrypted, accessMasked, err := s.credentials.Encrypt(token.AccessToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	refreshEncrypted, refreshMasked, err := s.credentials.Encrypt(token.RefreshToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	now := time.Now()
	connectionMeta := map[string]any{
		"provider":                claudeCodeProvider,
		"plan_type":               adapter.ClaudeCodePlanType(profile.Organization.OrganizationType, profile.Organization.RateLimitTier),
		"organization_id":         organizationID,
		"organization_type":       strings.TrimSpace(profile.Organization.OrganizationType),
		"rate_limit_tier":         strings.TrimSpace(profile.Organization.RateLimitTier),
		"seat_tier":               strings.TrimSpace(profile.Organization.SeatTier),
		"billing_type":            strings.TrimSpace(profile.Organization.BillingType),
		"has_extra_usage_enabled": profile.Organization.HasExtraUsageEnabled,
		"refreshable":             strings.TrimSpace(token.RefreshToken) != "",
		"token_mode":              "oauth_refresh",
		"profile_name":            strings.TrimSpace(profile.Account.DisplayName),
	}
	if token.RefreshTokenExpiresIn > 0 {
		connectionMeta["refresh_token_expires_at"] = now.Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	connectionMetaJSON, err := json.Marshal(connectionMeta)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("marshal claude code connection meta: %w", err)
	}
	var connection store.OAuthConnection
	err = s.db.WithinTx(ctx, func(tx store.Tx) error {
		connectionRepo := store.NewOAuthConnectionRepository(tx)
		connection, err = connectionRepo.UpsertByProviderEmail(ctx, store.UpsertOAuthConnectionParams{
			Provider:              claudeCodeProvider,
			SiteID:                pendingSite.SiteID,
			Status:                "connected",
			AccountID:             accountID,
			Email:                 email,
			EncryptedAccessToken:  accessEncrypted,
			MaskedAccessToken:     accessMasked,
			EncryptedRefreshToken: refreshEncrypted,
			MaskedRefreshToken:    refreshMasked,
			TokenType:             defaultTokenType(token.TokenType),
			Scopes:                strings.TrimSpace(token.Scope),
			ExpiresAt:             claudeCodeTokenExpiry(now, token.ExpiresIn),
			LastRefreshAt:         now,
			RawProfile:            jsonBytes(rawProfile),
			Metadata:              connectionMetaJSON,
		})
		if err != nil {
			return err
		}
		session, err = store.NewOAuthSessionRepository(tx).Complete(ctx, session.ID, "completed", session.Metadata)
		return err
	})
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	return session, connection, pendingSite, nil
}

// SessionByState resolves the pending OAuth session for a callback state. Error
// paths use it so redirects target the trusted redirect URL captured at
// authorize time rather than an attacker-supplied redirect_url query parameter.
func (s *Service) SessionByState(ctx context.Context, state string) (store.OAuthSession, error) {
	if s.db == nil {
		return store.OAuthSession{}, fmt.Errorf("oauth store is not available")
	}
	return store.NewOAuthSessionRepository(s.db.DB()).GetByState(ctx, strings.TrimSpace(state))
}

func (s *Service) HandleCodexCallback(ctx context.Context, state string, code string) (store.OAuthSession, store.OAuthConnection, PendingSite, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("state and code are required")
	}
	sessionRepo := store.NewOAuthSessionRepository(s.db.DB())
	connectionRepo := store.NewOAuthConnectionRepository(s.db.DB())
	session, err := sessionRepo.GetByState(ctx, strings.TrimSpace(state))
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	if session.Status != "pending" {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session is no longer pending")
	}
	if time.Now().After(session.ExpiresAt) {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session has expired")
	}
	tokenResp, err := s.exchangeCodexCode(ctx, strings.TrimSpace(code), session.RedirectURI, session.PKCEVerifier)
	if err != nil {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	claims, rawClaims, err := parseCodexIDToken(tokenResp.IDToken)
	if err != nil {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	accessEncrypted, accessMasked, err := s.credentials.Encrypt(tokenResp.AccessToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	refreshEncrypted, refreshMasked, err := s.credentials.Encrypt(tokenResp.RefreshToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	idEncrypted, idMasked, err := s.credentials.Encrypt(tokenResp.IDToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	connectionMeta := map[string]any{
		"plan_type":                         strings.TrimSpace(claims.AuthInfo.ChatGPTPlanType),
		"chatgpt_user_id":                   strings.TrimSpace(claims.AuthInfo.ChatGPTUserID),
		"user_id":                           strings.TrimSpace(claims.AuthInfo.UserID),
		"chatgpt_subscription_active_start": claims.AuthInfo.ChatGPTSubscriptionActiveStart,
		"chatgpt_subscription_active_until": claims.AuthInfo.ChatGPTSubscriptionActiveUntil,
	}
	connectionMetaJSON, err := json.Marshal(connectionMeta)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("marshal codex connection meta: %w", err)
	}
	var pendingSite PendingSite
	if len(session.SitePayload) > 0 {
		_ = json.Unmarshal(session.SitePayload, &pendingSite)
	}
	connection, err := connectionRepo.UpsertByProviderEmail(ctx, store.UpsertOAuthConnectionParams{
		Provider:              codexProvider,
		SiteID:                pendingSite.SiteID,
		Status:                "connected",
		AccountID:             strings.TrimSpace(claims.AuthInfo.ChatGPTAccountID),
		Email:                 strings.TrimSpace(claims.Email),
		EncryptedAccessToken:  accessEncrypted,
		MaskedAccessToken:     accessMasked,
		EncryptedRefreshToken: refreshEncrypted,
		MaskedRefreshToken:    refreshMasked,
		EncryptedIDToken:      idEncrypted,
		MaskedIDToken:         idMasked,
		TokenType:             defaultTokenType(tokenResp.TokenType),
		Scopes:                strings.TrimSpace(tokenResp.Scope),
		ExpiresAt:             codexTokenExpiry(time.Now(), tokenResp.ExpiresIn),
		LastRefreshAt:         time.Now(),
		RawProfile:            jsonBytes(rawClaims),
		Metadata:              connectionMetaJSON,
	})
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	session, err = sessionRepo.Complete(ctx, session.ID, "completed", session.Metadata)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	return session, connection, pendingSite, nil
}

func (s *Service) HandleAntigravityCallback(ctx context.Context, state string, code string) (store.OAuthSession, store.OAuthConnection, PendingSite, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("state and code are required")
	}
	sessionRepo := store.NewOAuthSessionRepository(s.db.DB())
	connectionRepo := store.NewOAuthConnectionRepository(s.db.DB())
	session, err := sessionRepo.GetByState(ctx, strings.TrimSpace(state))
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	if session.Provider != antigravityProvider {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session provider mismatch")
	}
	if session.Status != "pending" {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session is no longer pending")
	}
	if time.Now().After(session.ExpiresAt) {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("oauth session has expired")
	}
	sessionMeta := map[string]any{}
	if len(session.Metadata) > 0 {
		_ = json.Unmarshal(session.Metadata, &sessionMeta)
	}
	client := antigravityOAuthClientFromEnv(stringFromAny(sessionMeta["oauth_client_key"]))
	tokenResp, err := s.exchangeAntigravityCode(ctx, strings.TrimSpace(code), session.RedirectURI, client)
	if err != nil {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	userInfo, rawProfile, err := s.fetchAntigravityUserInfo(ctx, tokenResp.AccessToken, s.httpClient)
	if err != nil {
		_, _ = sessionRepo.Complete(ctx, session.ID, "failed", session.Metadata)
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	accessEncrypted, accessMasked, err := s.credentials.Encrypt(tokenResp.AccessToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	refreshEncrypted, refreshMasked, err := s.credentials.Encrypt(tokenResp.RefreshToken)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	connectionMeta := map[string]any{
		"provider":         antigravityProvider,
		"oauth_client_key": client.Key,
		"name":             strings.TrimSpace(userInfo.Name),
	}
	connectionMetaJSON, err := json.Marshal(connectionMeta)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, fmt.Errorf("marshal antigravity connection meta: %w", err)
	}
	var pendingSite PendingSite
	if len(session.SitePayload) > 0 {
		_ = json.Unmarshal(session.SitePayload, &pendingSite)
	}
	connection, err := connectionRepo.UpsertByProviderEmail(ctx, store.UpsertOAuthConnectionParams{
		Provider:              antigravityProvider,
		SiteID:                pendingSite.SiteID,
		Status:                "connected",
		AccountID:             firstNonEmptyString(userInfo.ID, userInfo.Email),
		Email:                 strings.TrimSpace(userInfo.Email),
		EncryptedAccessToken:  accessEncrypted,
		MaskedAccessToken:     accessMasked,
		EncryptedRefreshToken: refreshEncrypted,
		MaskedRefreshToken:    refreshMasked,
		TokenType:             defaultTokenType(tokenResp.TokenType),
		Scopes:                strings.TrimSpace(tokenResp.Scope),
		ExpiresAt:             antigravityTokenExpiry(time.Now(), tokenResp.ExpiresIn),
		LastRefreshAt:         time.Now(),
		RawProfile:            jsonBytes(rawProfile),
		Metadata:              connectionMetaJSON,
	})
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	session, err = sessionRepo.Complete(ctx, session.ID, "completed", session.Metadata)
	if err != nil {
		return store.OAuthSession{}, store.OAuthConnection{}, PendingSite{}, err
	}
	return session, connection, pendingSite, nil
}

func (s *Service) ListConnections(ctx context.Context) ([]store.OAuthConnection, error) {
	return store.NewOAuthConnectionRepository(s.db.DB()).List(ctx)
}

func (s *Service) ConnectionRecordByID(ctx context.Context, id uuid.UUID) (store.OAuthConnection, error) {
	return store.NewOAuthConnectionRepository(s.db.DB()).GetByID(ctx, id)
}

func (s *Service) ConnectionByID(ctx context.Context, id uuid.UUID) (CodexConnection, error) {
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetByID(ctx, id)
	if err != nil {
		return CodexConnection{}, err
	}
	return s.codexConnectionDetails(connection)
}

func (s *Service) ConnectionBySiteID(ctx context.Context, siteID uuid.UUID) (CodexConnection, error) {
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, siteID)
	if err != nil {
		return CodexConnection{}, err
	}
	return s.codexConnectionDetails(connection)
}

func (s *Service) EnsureCodexConnectionFresh(ctx context.Context, siteID uuid.UUID) (CodexConnection, error) {
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, siteID)
	if err != nil {
		return CodexConnection{}, err
	}
	if connection.Provider != codexProvider {
		return CodexConnection{}, fmt.Errorf("oauth connection provider %q does not support codex refresh", connection.Provider)
	}
	if !connection.ExpiresAt.Valid || time.Until(connection.ExpiresAt.Time) > codexRefreshLead {
		return s.codexConnectionDetails(connection)
	}
	return s.RefreshCodexConnection(ctx, connection.ID)
}

func (s *Service) EnsureAntigravityConnectionFresh(ctx context.Context, siteID uuid.UUID) (CodexConnection, error) {
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, siteID)
	if err != nil {
		return CodexConnection{}, err
	}
	if connection.Provider != antigravityProvider {
		return CodexConnection{}, fmt.Errorf("oauth connection provider %q does not support antigravity refresh", connection.Provider)
	}
	if !connection.ExpiresAt.Valid || time.Until(connection.ExpiresAt.Time) > antigravityRefreshLead {
		return s.codexConnectionDetails(connection)
	}
	return s.RefreshCodexConnection(ctx, connection.ID)
}

func (s *Service) EnsureClaudeCodeConnectionFresh(ctx context.Context, siteID uuid.UUID) (CodexConnection, error) {
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, siteID)
	if err != nil {
		return CodexConnection{}, err
	}
	if connection.Provider != claudeCodeProvider {
		return CodexConnection{}, fmt.Errorf("oauth connection provider %q does not support claude code refresh", connection.Provider)
	}
	if !connection.ExpiresAt.Valid || time.Until(connection.ExpiresAt.Time) > claudeCodeRefreshLead {
		return s.codexConnectionDetails(connection)
	}
	return s.RefreshCodexConnection(ctx, connection.ID)
}

// RefreshCodexConnection refreshes (and rotates) a connection's tokens.
// Concurrent callers for the same connection are collapsed via singleflight: the
// Codex/Antigravity refresh rotates refresh_token, so a second concurrent refresh
// would present an already-invalidated token, get invalid_grant, and wrongly
// disable a healthy site. Losers instead receive the winner's fresh result.
func (s *Service) RefreshCodexConnection(ctx context.Context, connectionID uuid.UUID) (CodexConnection, error) {
	result, err, _ := s.refreshGroup.Do(connectionID.String(), func() (any, error) {
		lock := s.withRefreshConnectionLock
		if s.refreshLock != nil {
			lock = s.refreshLock
		}
		return lock(ctx, connectionID, func() (CodexConnection, error) {
			return s.refreshConnectionOnce(ctx, connectionID)
		})
	})
	if err != nil {
		return CodexConnection{}, err
	}
	connection, _ := result.(CodexConnection)
	return connection, nil
}

func (s *Service) refreshConnectionOnce(ctx context.Context, connectionID uuid.UUID) (CodexConnection, error) {
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return CodexConnection{}, err
	}
	if connection.Provider == antigravityProvider {
		return s.refreshAntigravityConnection(ctx, repo, connection)
	}
	if connection.Provider == claudeCodeProvider {
		return s.refreshClaudeCodeConnection(ctx, repo, connection)
	}
	if connection.Provider != codexProvider {
		return CodexConnection{}, fmt.Errorf("oauth connection provider %q is not supported", connection.Provider)
	}
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
	httpClient, err := s.httpClientForConnection(ctx, connection)
	if err != nil {
		return CodexConnection{}, err
	}
	refreshed, err := s.refreshCodexTokens(ctx, refreshToken, httpClient)
	if err != nil {
		errMsg := err.Error()
		connection.Status = "reconnect_required"
		connection.Metadata = store.JSON(updateMetadataError(connection.Metadata, errMsg))
		_, _ = repo.Save(ctx, connection)
		s.disableSiteOnPermanentError(ctx, connection, errMsg)
		return CodexConnection{}, err
	}
	claims, rawClaims, err := parseCodexIDToken(refreshed.IDToken)
	if err != nil {
		return CodexConnection{}, err
	}
	connectionMeta := map[string]any{
		"plan_type":                         strings.TrimSpace(claims.AuthInfo.ChatGPTPlanType),
		"chatgpt_user_id":                   strings.TrimSpace(claims.AuthInfo.ChatGPTUserID),
		"user_id":                           strings.TrimSpace(claims.AuthInfo.UserID),
		"chatgpt_subscription_active_start": claims.AuthInfo.ChatGPTSubscriptionActiveStart,
		"chatgpt_subscription_active_until": claims.AuthInfo.ChatGPTSubscriptionActiveUntil,
		"refreshable":                       true,
		"token_mode":                        "oauth_refresh",
	}
	connection.Metadata = jsonBytes(connectionMeta)
	connection.RawProfile = jsonBytes(rawClaims)
	connection.Email = strings.TrimSpace(claims.Email)
	connection.AccountID = strings.TrimSpace(claims.AuthInfo.ChatGPTAccountID)
	connection.TokenType = defaultTokenType(refreshed.TokenType)
	connection.Scopes = strings.TrimSpace(refreshed.Scope)
	connection.Status = "connected"
	now := time.Now()
	expiresAt := codexTokenExpiry(now, refreshed.ExpiresIn)
	connection.LastRefreshAt = sql.NullTime{Time: now, Valid: true}
	connection.ExpiresAt = sql.NullTime{Time: expiresAt, Valid: !expiresAt.IsZero()}
	if connection.EncryptedAccessToken, connection.MaskedAccessToken, err = s.credentials.Encrypt(refreshed.AccessToken); err != nil {
		return CodexConnection{}, err
	}
	if connection.EncryptedRefreshToken, connection.MaskedRefreshToken, err = s.credentials.Encrypt(refreshed.RefreshToken); err != nil {
		return CodexConnection{}, err
	}
	if connection.EncryptedIDToken, connection.MaskedIDToken, err = s.credentials.Encrypt(refreshed.IDToken); err != nil {
		return CodexConnection{}, err
	}
	connection, err = repo.Save(ctx, connection)
	if err != nil {
		return CodexConnection{}, err
	}
	return s.codexConnectionDetails(connection)
}

func (s *Service) refreshClaudeCodeConnection(ctx context.Context, repo store.OAuthConnectionRepository, connection store.OAuthConnection) (CodexConnection, error) {
	if strings.TrimSpace(connection.EncryptedRefreshToken) == "" {
		if connection.ExpiresAt.Valid && !connection.ExpiresAt.Time.After(time.Now()) {
			return CodexConnection{}, fmt.Errorf("oauth connection has no refresh_token and access token has expired; sign in again")
		}
		return s.codexConnectionDetails(connection)
	}
	refreshToken, err := s.credentials.Decrypt(connection.EncryptedRefreshToken)
	if err != nil {
		return CodexConnection{}, err
	}
	httpClient, err := s.httpClientForConnection(ctx, connection)
	if err != nil {
		return CodexConnection{}, err
	}
	refreshed, err := s.refreshClaudeCodeTokens(ctx, refreshToken, connection.Scopes, httpClient)
	if err != nil {
		errMsg := err.Error()
		connection.Status = "reconnect_required"
		connection.Metadata = store.JSON(updateMetadataError(connection.Metadata, errMsg))
		_, _ = repo.Save(ctx, connection)
		s.disableSiteOnPermanentError(ctx, connection, errMsg)
		return CodexConnection{}, err
	}
	profile, rawProfile, err := s.fetchClaudeCodeProfile(ctx, refreshed.AccessToken, httpClient)
	if err != nil {
		return CodexConnection{}, err
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	meta["provider"] = claudeCodeProvider
	meta["plan_type"] = adapter.ClaudeCodePlanType(profile.Organization.OrganizationType, profile.Organization.RateLimitTier)
	meta["organization_id"] = profile.Organization.UUID
	meta["rate_limit_tier"] = profile.Organization.RateLimitTier
	meta["seat_tier"] = profile.Organization.SeatTier
	meta["billing_type"] = profile.Organization.BillingType
	meta["profile_name"] = strings.TrimSpace(profile.Account.DisplayName)
	meta["refreshable"] = true
	meta["token_mode"] = "oauth_refresh"
	delete(meta, "last_error")
	delete(meta, "last_error_at")
	now := time.Now()
	if refreshed.RefreshTokenExpiresIn > 0 {
		meta["refresh_token_expires_at"] = now.Add(time.Duration(refreshed.RefreshTokenExpiresIn) * time.Second).Format(time.RFC3339)
	}
	connection.Metadata = jsonBytes(meta)
	connection.RawProfile = jsonBytes(rawProfile)
	connection.Email = firstNonEmptyString(profile.Account.Email, refreshed.Account.EmailAddress, connection.Email)
	connection.AccountID = firstNonEmptyString(profile.Account.UUID, refreshed.Account.UUID, connection.AccountID)
	connection.TokenType = defaultTokenType(refreshed.TokenType)
	if strings.TrimSpace(refreshed.Scope) != "" {
		connection.Scopes = strings.TrimSpace(refreshed.Scope)
	}
	connection.Status = "connected"
	expiresAt := claudeCodeTokenExpiry(now, refreshed.ExpiresIn)
	connection.LastRefreshAt = sql.NullTime{Time: now, Valid: true}
	connection.ExpiresAt = sql.NullTime{Time: expiresAt, Valid: !expiresAt.IsZero()}
	if connection.EncryptedAccessToken, connection.MaskedAccessToken, err = s.credentials.Encrypt(refreshed.AccessToken); err != nil {
		return CodexConnection{}, err
	}
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		if connection.EncryptedRefreshToken, connection.MaskedRefreshToken, err = s.credentials.Encrypt(refreshed.RefreshToken); err != nil {
			return CodexConnection{}, err
		}
	}
	connection, err = repo.Save(ctx, connection)
	if err != nil {
		return CodexConnection{}, err
	}
	return s.codexConnectionDetails(connection)
}

func (s *Service) ConsumeCodexRateLimitResetCredit(ctx context.Context, connectionID uuid.UUID, idempotencyKey string, creditID string) (map[string]any, CodexConnection, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, CodexConnection{}, fmt.Errorf("idempotency_key is required")
	}
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return nil, CodexConnection{}, err
	}
	if connection.Provider != codexProvider {
		return nil, CodexConnection{}, fmt.Errorf("oauth connection provider %q does not support codex reset credits", connection.Provider)
	}
	details, err := s.codexConnectionDetails(connection)
	if err != nil {
		return nil, CodexConnection{}, err
	}
	siteConfig, err := s.codexAdapterSiteForConnection(ctx, connection)
	if err != nil {
		return nil, CodexConnection{}, err
	}
	module := adapter.NewCodex()
	result, err := module.ConsumeRateLimitResetCredit(ctx, siteConfig, adapter.SystemAuth{
		Provider:     codexProvider,
		ConnectionID: connection.ID,
		AccessToken:  details.AccessToken,
		RefreshToken: details.RefreshToken,
		IDToken:      details.IDToken,
		AccountID:    details.AccountID,
		Email:        details.Email,
		Metadata:     details.Metadata,
	}, idempotencyKey, creditID)
	if err != nil {
		return nil, CodexConnection{}, err
	}
	// A successful reset restores both quota windows immediately, so lift any quota
	// cooldown on this account's site right away instead of waiting for the original
	// window reset time to elapse. Best-effort: the upstream reset already succeeded.
	if outcome, _ := result["outcome"].(string); outcome == "reset" && connection.SiteID != nil && *connection.SiteID != uuid.Nil {
		_, _ = store.NewRouteCooldownRepository(s.db.DB()).ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
			SiteID:  *connection.SiteID,
			Reasons: store.CodexQuotaCooldownReasons(),
		})
	}
	updated, err := s.updateCodexConnectionSummary(ctx, connection, details, siteConfig)
	if err != nil {
		return nil, CodexConnection{}, fmt.Errorf("sync codex connection summary: %w", err)
	}
	return result, updated, nil
}

func (s *Service) ListCodexRateLimitResetCredits(ctx context.Context, connectionID uuid.UUID) (adapter.CodexRateLimitResetCredits, error) {
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return adapter.CodexRateLimitResetCredits{}, err
	}
	if connection.Provider != codexProvider {
		return adapter.CodexRateLimitResetCredits{}, fmt.Errorf("oauth connection provider %q does not support codex reset credits", connection.Provider)
	}
	details, err := s.codexConnectionDetails(connection)
	if err != nil {
		return adapter.CodexRateLimitResetCredits{}, err
	}
	siteConfig, err := s.codexAdapterSiteForConnection(ctx, connection)
	if err != nil {
		return adapter.CodexRateLimitResetCredits{}, err
	}
	module := adapter.NewCodex()
	return module.ListRateLimitResetCredits(ctx, siteConfig, adapter.SystemAuth{
		Provider:     codexProvider,
		ConnectionID: connection.ID,
		AccessToken:  details.AccessToken,
		RefreshToken: details.RefreshToken,
		IDToken:      details.IDToken,
		AccountID:    details.AccountID,
		Email:        details.Email,
		Metadata:     details.Metadata,
	})
}

func (s *Service) updateCodexConnectionSummary(ctx context.Context, connection store.OAuthConnection, details CodexConnection, siteConfig adapter.SiteConfig) (CodexConnection, error) {
	module := adapter.NewCodex()
	summary, err := module.FetchUserSummary(ctx, siteConfig, adapter.SystemAuth{
		Provider:     codexProvider,
		ConnectionID: connection.ID,
		AccessToken:  details.AccessToken,
		RefreshToken: details.RefreshToken,
		IDToken:      details.IDToken,
		AccountID:    details.AccountID,
		Email:        details.Email,
		Metadata:     details.Metadata,
	})
	if err != nil {
		return CodexConnection{}, err
	}
	patch := map[string]any{
		"quota":  quotaFromCodexUserSummary(summary),
		"models": modelsFromCodexUserSummary(summary),
	}
	for key, value := range metadataFromCodexUserSummary(summary) {
		patch[key] = value
	}
	if err := s.UpdateConnectionSync(ctx, connection.ID, patch); err != nil {
		return CodexConnection{}, err
	}
	updated, err := s.ConnectionByID(ctx, connection.ID)
	if err != nil {
		return CodexConnection{}, err
	}
	return updated, nil
}

func (s *Service) codexAdapterSiteForConnection(ctx context.Context, connection store.OAuthConnection) (adapter.SiteConfig, error) {
	config := adapter.SiteConfig{
		SiteType: codexProvider,
		BaseURL:  codexDefaultBackendBaseURL,
		Meta: map[string]any{
			"oauth_account_id": connection.AccountID,
		},
	}
	if connection.SiteID == nil || *connection.SiteID == uuid.Nil {
		config.Client = s.httpClient
		return config, nil
	}
	site, err := store.NewSiteRepository(s.db.DB()).GetByID(ctx, *connection.SiteID)
	if err != nil {
		return adapter.SiteConfig{}, err
	}
	meta := map[string]any{}
	if len(site.Meta) > 0 {
		_ = json.Unmarshal(site.Meta, &meta)
	}
	config.ID = site.ID.String()
	config.Name = site.Name
	config.SiteType = site.SiteType
	config.BaseURL = strings.TrimSpace(site.BaseURL)
	if config.BaseURL == "" {
		config.BaseURL = codexDefaultBackendBaseURL
	}
	config.Meta = meta
	config.Client, err = s.httpClientForConnection(ctx, connection)
	if err != nil {
		return adapter.SiteConfig{}, err
	}
	return config, nil
}

func (s *Service) UpdateConnectionSync(ctx context.Context, connectionID uuid.UUID, metadataPatch map[string]any) error {
	if connectionID == uuid.Nil {
		return nil
	}
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	for key, value := range metadataPatch {
		meta[key] = value
	}
	connection.Metadata = jsonBytes(meta)
	connection.LastSyncAt = sql.NullTime{Time: time.Now(), Valid: true}
	_, err = repo.Save(ctx, connection)
	return err
}

func (s *Service) MarkConnectionAccessTokenOnly(ctx context.Context, connectionID uuid.UUID) error {
	if connectionID == uuid.Nil {
		return nil
	}
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return err
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	meta["refreshable"] = false
	meta["token_mode"] = "access_token_only"
	meta["refresh_warning"] = importAccessTokenOnlyWarning
	delete(meta, "last_error")
	delete(meta, "last_error_at")
	connection.Metadata = jsonBytes(meta)
	connection.Status = "connected"
	connection.EncryptedRefreshToken = ""
	connection.MaskedRefreshToken = ""
	_, err = repo.Save(ctx, connection)
	return err
}

func (s *Service) MarkConnectionUnavailable(ctx context.Context, connectionID uuid.UUID, errMsg string) error {
	if connectionID == uuid.Nil {
		return nil
	}
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return err
	}
	connection.Status = "reconnect_required"
	connection.Metadata = store.JSON(updateMetadataError(connection.Metadata, errMsg))
	if _, err = repo.Save(ctx, connection); err != nil {
		return err
	}
	if connection.SiteID != nil && *connection.SiteID != uuid.Nil {
		_, _ = store.NewSiteRepository(s.db.DB()).SetEnabled(ctx, *connection.SiteID, false)
	}
	return nil
}

func (s *Service) MarkConnectionUnavailableBySiteID(ctx context.Context, siteID uuid.UUID, errMsg string) error {
	if siteID == uuid.Nil || s.db == nil {
		return nil
	}
	connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetBySiteID(ctx, siteID)
	if err != nil {
		return err
	}
	return s.MarkConnectionUnavailable(ctx, connection.ID, errMsg)
}

func (s *Service) BindConnectionSite(ctx context.Context, connectionID uuid.UUID, siteID uuid.UUID) error {
	repo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := repo.GetByID(ctx, connectionID)
	if err != nil {
		return err
	}
	if siteID == uuid.Nil {
		connection.SiteID = nil
	} else {
		boundID := siteID
		connection.SiteID = &boundID
	}
	_, err = repo.Save(ctx, connection)
	return err
}

func (s *Service) httpClientForConnection(ctx context.Context, connection store.OAuthConnection) (*http.Client, error) {
	if connection.SiteID == nil || *connection.SiteID == uuid.Nil || s.db == nil {
		return s.httpClient, nil
	}
	site, err := store.NewSiteRepository(s.db.DB()).GetByID(ctx, *connection.SiteID)
	if err != nil {
		return nil, err
	}
	meta := map[string]any{}
	if len(site.Meta) > 0 {
		_ = json.Unmarshal(site.Meta, &meta)
	}
	profile := httpclient.DefaultProfile()
	if proxyID, ok := meta["proxy_id"].(string); ok {
		profile.ProxyID = strings.TrimSpace(proxyID)
	}
	return s.httpClients.Client(profile)
}

func (s *Service) httpClientForPendingSite(_ context.Context, pending PendingSite) (*http.Client, error) {
	profile := httpclient.DefaultProfile()
	if pending.ProxyID != nil {
		profile.ProxyID = strings.TrimSpace(*pending.ProxyID)
	}
	if profile.ProxyID == "" {
		return s.httpClient, nil
	}
	return s.httpClients.Client(profile)
}

func (s *Service) codexConnectionDetails(connection store.OAuthConnection) (CodexConnection, error) {
	accessToken, err := s.credentials.Decrypt(connection.EncryptedAccessToken)
	if err != nil {
		return CodexConnection{}, err
	}
	refreshToken := ""
	if strings.TrimSpace(connection.EncryptedRefreshToken) != "" {
		refreshToken, err = s.credentials.Decrypt(connection.EncryptedRefreshToken)
		if err != nil {
			return CodexConnection{}, err
		}
	}
	idToken := ""
	if strings.TrimSpace(connection.EncryptedIDToken) != "" {
		idToken, err = s.credentials.Decrypt(connection.EncryptedIDToken)
		if err != nil {
			return CodexConnection{}, err
		}
	}
	claims := map[string]any{}
	if len(connection.RawProfile) > 0 {
		_ = json.Unmarshal(connection.RawProfile, &claims)
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	planType, _ := meta["plan_type"].(string)
	quota, _ := meta["quota"].(map[string]any)
	if connection.Provider == claudeCodeProvider {
		if derived := adapter.ClaudeCodePlanType(stringFromAny(meta["organization_type"]), stringFromAny(meta["rate_limit_tier"])); derived != "" {
			planType = derived
		}
		quota = claudeCodeQuotaForDetails(quota)
	}
	models := normalizeCodexModelSnapshots(mapsFromAny(meta["models"]))
	return CodexConnection{
		Connection:   connection,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		IDToken:      idToken,
		Claims:       claims,
		Metadata:     meta,
		AccountID:    connection.AccountID,
		Email:        connection.Email,
		PlanType:     strings.TrimSpace(planType),
		Quota:        quota,
		Models:       models,
	}, nil
}

func claudeCodeQuotaForDetails(quota map[string]any) map[string]any {
	if len(quota) == 0 {
		return quota
	}
	if quotaType, _ := quota["type"].(string); strings.EqualFold(strings.TrimSpace(quotaType), "claude_code") {
		// Re-adapt from the embedded raw payload so stored summaries pick up
		// adapter improvements (e.g. newly exposed model-scoped windows)
		// without waiting for the next upstream refresh.
		raw, ok := quota["raw"].(map[string]any)
		if !ok || len(raw) == 0 {
			return quota
		}
		adapted := adapter.ClaudeCodeQuotaSummary(raw)
		if len(adapted) == 0 {
			return quota
		}
		merged := make(map[string]any, len(quota)+len(adapted))
		for key, value := range quota {
			merged[key] = value
		}
		for _, key := range []string{"five_hour", "weekly", "models", "spend", "extra_usage"} {
			delete(merged, key)
		}
		for key, value := range adapted {
			merged[key] = value
		}
		return merged
	}
	if _, ok := quota["limits"]; ok {
		return adapter.ClaudeCodeQuotaSummary(quota)
	}
	if fiveHour, ok := quota["five_hour"].(map[string]any); ok {
		if _, ok := fiveHour["utilization"]; ok {
			return adapter.ClaudeCodeQuotaSummary(quota)
		}
	}
	return quota
}

func normalizeCallbackBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ""
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.TrimRight(parsed.String(), "/")
}

func defaultTokenType(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Bearer"
	}
	return strings.TrimSpace(value)
}

func updateMetadataError(existing store.JSON, errMsg string) []byte {
	meta := map[string]any{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &meta)
	}
	meta["last_error"] = errMsg
	meta["last_error_at"] = time.Now().UTC().Format(time.RFC3339)
	encoded, _ := json.Marshal(meta)
	return encoded
}

func isPermanentAuthError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	if strings.Contains(lower, "refresh_token_reused") || strings.Contains(lower, "invalid_grant") {
		return true
	}
	return messageContainsHTTPAuthCode(lower)
}

func messageContainsHTTPAuthCode(message string) bool {
	for _, prefix := range []string{
		"returned ", "status ", "status=", "status_code ", "http ", "responded with ",
	} {
		remaining := message
		for {
			idx := strings.Index(remaining, prefix)
			if idx < 0 {
				break
			}
			codeStart := idx + len(prefix)
			remaining = remaining[codeStart:]
			if len(remaining) < 3 {
				break
			}
			c1, c2 := remaining[1], remaining[2]
			if c1 < '0' || c1 > '9' || c2 < '0' || c2 > '9' {
				continue
			}
			if len(remaining) > 3 && remaining[3] >= '0' && remaining[3] <= '9' {
				continue
			}
			code := remaining[:3]
			if code == "401" || code == "403" {
				return true
			}
		}
	}
	return false
}

func (s *Service) disableSiteOnPermanentError(ctx context.Context, connection store.OAuthConnection, errMsg string) {
	if !isPermanentAuthError(errMsg) {
		return
	}
	if connection.SiteID == nil {
		return
	}
	siteRepo := store.NewSiteRepository(s.db.DB())
	existing, err := siteRepo.GetByID(ctx, *connection.SiteID)
	if err != nil {
		return
	}
	if !existing.Enabled {
		return
	}
	existing.Enabled = false
	_ = s.db.DB().WithContext(ctx).Save(&existing).Error
}

func jsonBytes(value any) store.JSON {
	if value == nil {
		return store.JSON([]byte(`{}`))
	}
	data, err := json.Marshal(value)
	if err != nil {
		return store.JSON([]byte(`{}`))
	}
	return store.JSON(data)
}

func quotaFromCodexUserSummary(summary adapter.UserSummary) any {
	user, _ := summary.User.(map[string]any)
	if user == nil {
		return nil
	}
	return user["quota"]
}

func modelsFromCodexUserSummary(summary adapter.UserSummary) any {
	models, _ := summary.UserModels.(map[string]any)
	if models == nil {
		return nil
	}
	return models["data"]
}

func metadataFromCodexUserSummary(summary adapter.UserSummary) map[string]any {
	result := map[string]any{}
	user, _ := summary.User.(map[string]any)
	if user == nil {
		return result
	}
	for _, key := range []string{"plan_type", "chatgpt_subscription_active_start", "chatgpt_subscription_active_until"} {
		if value := user[key]; value != nil {
			result[key] = value
		}
	}
	return result
}

func mapsFromAny(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		parsed, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, parsed)
	}
	return result
}

func normalizeCodexModelSnapshots(items []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		model := map[string]any{}
		for key, value := range item {
			model[key] = value
		}
		id := strings.TrimSpace(stringFromAny(model["id"]))
		name := firstNonEmptyString(
			stringFromAny(model["slug"]),
			stringFromAny(model["upstream_model_name"]),
			stringFromAny(model["name"]),
			stringFromAny(model["model"]),
		)
		if looksLikeUUID(id) && name != "" {
			model["site_model_id"] = id
			model["id"] = name
		}
		if strings.TrimSpace(stringFromAny(model["id"])) == "" && name != "" {
			model["id"] = name
		}
		if _, ok := model["upstream_model_name"]; !ok && name != "" {
			model["upstream_model_name"] = name
		}
		if _, ok := model["display_name"]; !ok {
			if display := stringFromAny(model["display"]); display != "" {
				model["display_name"] = display
			}
		}
		if _, ok := model["enabled"]; !ok {
			status := strings.TrimSpace(stringFromAny(model["status"]))
			model["enabled"] = status == "" || status == "active"
		}
		result = append(result, model)
	}
	return result
}

func looksLikeUUID(value string) bool {
	if _, err := uuid.Parse(strings.TrimSpace(value)); err == nil {
		return true
	}
	return false
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
