package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/store"
)

func TestHandleCodexCallbackMarksSessionFailedWhenExchangeFailsOffline(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	queryCount := 0
	var savedSession store.OAuthSession
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected codex callback query destination"))
			return
		}
		queryCount++
		*session = store.OAuthSession{
			ID:           sessionID,
			Provider:     codexProvider,
			State:        "codex-callback-state",
			Status:       "pending",
			RedirectURI:  codexRedirectURI,
			PKCEVerifier: "verifier",
			ExpiresAt:    time.Now().Add(time.Hour),
			Metadata:     store.JSON(`{"kept":"yes"}`),
		}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected codex callback save destination"))
			return
		}
		savedSession = *session
		tx.Statement.RowsAffected = 1
	})
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
			t.Fatalf("unexpected codex exchange request: %s %s", req.Method, req.URL.String())
		}
		return oauthHTTPResponse(http.StatusBadGateway, ` upstream down `), nil
	})}

	_, _, _, err := service.HandleCodexCallback(context.Background(), " codex-callback-state ", " code ")
	if err == nil || !strings.Contains(err.Error(), "codex token exchange returned 502: upstream down") {
		t.Fatalf("HandleCodexCallback error = %v, want exchange failure", err)
	}
	if queryCount != 2 {
		t.Fatalf("query count = %d, want get-by-state and complete get-by-id", queryCount)
	}
	if savedSession.ID != sessionID || savedSession.Status != "failed" || !savedSession.CompletedAt.Valid {
		t.Fatalf("saved session = %#v, want failed completed session", savedSession)
	}
	if string(savedSession.Metadata) != `{"kept":"yes"}` {
		t.Fatalf("saved metadata = %s, want original metadata", savedSession.Metadata)
	}
}

func TestHandleAntigravityCallbackMarksSessionFailedWhenUserInfoFailsOffline(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	queryCount := 0
	var savedSession store.OAuthSession
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected antigravity callback query destination"))
			return
		}
		queryCount++
		*session = store.OAuthSession{
			ID:          sessionID,
			Provider:    antigravityProvider,
			State:       "antigravity-callback-state",
			Status:      "pending",
			RedirectURI: antigravityRedirectURI,
			ExpiresAt:   time.Now().Add(time.Hour),
			Metadata:    store.JSON(`{"oauth_client_key":"antigravity-client"}`),
		}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected antigravity callback save destination"))
			return
		}
		savedSession = *session
		tx.Statement.RowsAffected = 1
	})
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case antigravityTokenURL:
			if req.Method != http.MethodPost {
				t.Fatalf("unexpected antigravity token method: %s", req.Method)
			}
			return oauthHTTPResponse(http.StatusOK, `{"access_token":"access-token","refresh_token":"refresh-token","token_type":"Bearer","expires_in":900}`), nil
		case antigravityUserInfoURL:
			if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("unexpected antigravity userinfo request: %s %#v", req.Method, req.Header)
			}
			return oauthHTTPResponse(http.StatusForbidden, ` forbidden `), nil
		default:
			t.Fatalf("unexpected antigravity callback URL: %s", req.URL.String())
			return nil, nil
		}
	})}

	_, _, _, err := service.HandleAntigravityCallback(context.Background(), "antigravity-callback-state", "code")
	if err == nil || !strings.Contains(err.Error(), "antigravity userinfo returned 403: forbidden") {
		t.Fatalf("HandleAntigravityCallback error = %v, want userinfo failure", err)
	}
	if queryCount != 2 {
		t.Fatalf("query count = %d, want get-by-state and complete get-by-id", queryCount)
	}
	if savedSession.ID != sessionID || savedSession.Status != "failed" || !savedSession.CompletedAt.Valid {
		t.Fatalf("saved session = %#v, want failed completed session", savedSession)
	}
}

func TestRefreshCodexConnectionMarksReconnectRequiredOnRefreshFailureOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	bootstrap := NewService(nil, "master-key")
	encryptedRefresh, _, err := bootstrap.credentials.Encrypt("refresh-token")
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}
	connection := store.OAuthConnection{
		ID:                    connectionID,
		Provider:              codexProvider,
		Status:                "connected",
		Email:                 "user@example.com",
		EncryptedRefreshToken: encryptedRefresh,
		Metadata:              store.JSON(`{"token_mode":"oauth_refresh"}`),
	}
	var saved store.OAuthConnection
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected codex refresh query destination"))
			return
		}
		*item = connection
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected codex refresh save destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
			t.Fatalf("unexpected codex refresh request: %s %s", req.Method, req.URL.String())
		}
		return oauthHTTPResponse(http.StatusUnauthorized, ` invalid_grant `), nil
	})}

	_, err = service.RefreshCodexConnection(context.Background(), connectionID)
	if err == nil || !strings.Contains(err.Error(), "codex token refresh returned 401: invalid_grant") {
		t.Fatalf("RefreshCodexConnection error = %v, want refresh failure", err)
	}
	if saved.ID != connectionID || saved.Status != "reconnect_required" {
		t.Fatalf("saved connection = %#v, want reconnect_required", saved)
	}
	var meta map[string]any
	if err := json.Unmarshal(saved.Metadata, &meta); err != nil {
		t.Fatalf("decode saved metadata: %v", err)
	}
	if meta["token_mode"] != "oauth_refresh" || meta["last_error"] != "codex token refresh returned 401: invalid_grant" || strings.TrimSpace(stringFromAny(meta["last_error_at"])) == "" {
		t.Fatalf("saved metadata = %#v, want merged refresh error", meta)
	}
}

func TestRefreshAntigravityConnectionMarksReconnectRequiredOnRefreshFailureOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	service := NewService(nil, "master-key")
	encryptedRefresh, _, err := service.credentials.Encrypt("refresh-token")
	if err != nil {
		t.Fatalf("encrypt refresh token: %v", err)
	}
	connection := store.OAuthConnection{
		ID:                    connectionID,
		Provider:              antigravityProvider,
		Status:                "connected",
		EncryptedRefreshToken: encryptedRefresh,
		Metadata:              store.JSON(`{"oauth_client_key":"antigravity-client"}`),
	}
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
			t.Fatalf("unexpected antigravity refresh request: %s %s", req.Method, req.URL.String())
		}
		return oauthHTTPResponse(http.StatusBadRequest, ` invalid_grant `), nil
	})}

	_, err = service.refreshAntigravityConnection(context.Background(), store.OAuthConnectionRepository{}, connection)
	if err == nil || !strings.Contains(err.Error(), "antigravity token refresh returned 400: invalid_grant") {
		t.Fatalf("refreshAntigravityConnection error = %v, want refresh failure", err)
	}
	var fail *refreshFail
	if !errors.As(err, &fail) {
		t.Fatalf("refreshAntigravityConnection error = %v, want refreshFail carrying the reconnection state", err)
	}
	if fail.connection.ID != connectionID || fail.connection.Status != "reconnect_required" {
		t.Fatalf("refresh fail connection = %#v, want reconnect_required", fail.connection)
	}
	var meta map[string]any
	if err := json.Unmarshal(fail.connection.Metadata, &meta); err != nil {
		t.Fatalf("decode saved metadata: %v", err)
	}
	if meta["oauth_client_key"] != "antigravity-client" || meta["last_error"] != "antigravity token refresh returned 400: invalid_grant" || strings.TrimSpace(stringFromAny(meta["last_error_at"])) == "" {
		t.Fatalf("saved metadata = %#v, want merged antigravity refresh error", meta)
	}
}

func TestHandleCodexCallbackWithProxyKeepsSubmittedProxyOnReturnedSite(t *testing.T) {
	t.Parallel()

	sessionID := uuid.New()
	siteID := uuid.New()
	sessionProxy := "session-proxy"
	sitePayload, err := json.Marshal(PendingSite{
		SiteID:  siteID,
		Name:    "Codex Site",
		ProxyID: &sessionProxy,
	})
	if err != nil {
		t.Fatalf("marshal site payload: %v", err)
	}
	session := store.OAuthSession{
		ID:           sessionID,
		Provider:     codexProvider,
		State:        "codex-proxy-state",
		Status:       "pending",
		RedirectURI:  codexRedirectURI,
		PKCEVerifier: "verifier",
		SitePayload:  sitePayload,
		ExpiresAt:    time.Now().Add(time.Hour),
		Metadata:     store.JSON(`{"kept":"yes"}`),
	}
	var created store.OAuthConnection
	service := oauthServiceWithCallbacks(t, oauthGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *store.OAuthSession:
				*dest = session
				tx.Statement.RowsAffected = 1
			case *store.OAuthConnection:
				tx.AddError(gorm.ErrRecordNotFound)
			default:
				tx.AddError(errors.New("unexpected codex proxy callback query destination"))
			}
		},
		create: func(tx *gorm.DB) {
			connection, ok := tx.Statement.Dest.(*store.OAuthConnection)
			if !ok {
				tx.AddError(errors.New("unexpected codex proxy callback create destination"))
				return
			}
			created = *connection
			tx.Statement.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*store.OAuthSession); !ok {
				tx.AddError(errors.New("unexpected codex proxy callback update destination"))
				return
			}
			tx.Statement.RowsAffected = 1
		},
	})

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set("network", map[string]any{
		"proxies": []any{
			map[string]any{
				"id":   "submitted-proxy",
				"name": "Submitted",
				"type": "http",
				"url":  "http://127.0.0.1:9",
			},
		},
	}); err != nil {
		t.Fatalf("set network config: %v", err)
	}
	manager := httpclient.NewManager(confFile)
	profile := httpclient.DefaultProfile()
	profile.ProxyID = "submitted-proxy"
	proxyClient, err := manager.Client(profile)
	if err != nil {
		t.Fatalf("proxy client: %v", err)
	}
	usedProxyClient := false
	idToken := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"email":"codex@example.com","https://api.openai.com/auth":{"chatgpt_account_id":"acct-1"}}`)) + ".sig"
	tokenBody, err := json.Marshal(map[string]any{
		"access_token":  "access",
		"refresh_token": "refresh",
		"id_token":      idToken,
		"token_type":    "Bearer",
		"scope":         "openid",
		"expires_in":    3600,
	})
	if err != nil {
		t.Fatalf("marshal token body: %v", err)
	}
	proxyClient.Transport = oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		usedProxyClient = true
		if req.Method != http.MethodPost || req.URL.String() != codexTokenURL {
			t.Fatalf("unexpected codex exchange request: %s %s", req.Method, req.URL.String())
		}
		return oauthHTTPResponse(http.StatusOK, string(tokenBody)), nil
	})
	service.httpClients = manager
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("token exchange used the direct client for %s", req.URL.String())
		return nil, errors.New("direct client")
	})}

	submitted := "  submitted-proxy  "
	_, connection, pendingSite, err := service.HandleCodexCallbackWithProxy(context.Background(), " codex-proxy-state ", " auth-code ", &submitted)
	if err != nil {
		t.Fatalf("HandleCodexCallbackWithProxy: %v", err)
	}
	if !usedProxyClient {
		t.Fatal("token exchange did not use the submitted proxy client")
	}
	if pendingSite.SiteID != siteID || pendingSite.Name != "Codex Site" {
		t.Fatalf("pending site = %#v, want site %s %q", pendingSite, siteID, "Codex Site")
	}
	if pendingSite.ProxyID == nil || *pendingSite.ProxyID != "submitted-proxy" {
		t.Fatalf("pending site proxy = %#v, want submitted-proxy", pendingSite.ProxyID)
	}
	if created.SiteID == nil || *created.SiteID != siteID || connection.Email != "codex@example.com" {
		t.Fatalf("created connection site = %#v email = %q, want site %s", created.SiteID, connection.Email, siteID)
	}
}
