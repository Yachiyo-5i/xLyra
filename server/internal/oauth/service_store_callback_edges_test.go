package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

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
	var saved store.OAuthConnection
	db := oauthGormWithQueryUpdate(t, func(tx *gorm.DB) {
		tx.AddError(errors.New("antigravity refresh should not query repository"))
	}, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected antigravity refresh save destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})
	repo := store.NewOAuthConnectionRepository(db)
	service.httpClient = &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != antigravityTokenURL {
			t.Fatalf("unexpected antigravity refresh request: %s %s", req.Method, req.URL.String())
		}
		return oauthHTTPResponse(http.StatusBadRequest, ` invalid_grant `), nil
	})}

	_, err = service.refreshAntigravityConnection(context.Background(), repo, connection)
	if err == nil || !strings.Contains(err.Error(), "antigravity token refresh returned 400: invalid_grant") {
		t.Fatalf("refreshAntigravityConnection error = %v, want refresh failure", err)
	}
	if saved.ID != connectionID || saved.Status != "reconnect_required" {
		t.Fatalf("saved connection = %#v, want reconnect_required", saved)
	}
	var meta map[string]any
	if err := json.Unmarshal(saved.Metadata, &meta); err != nil {
		t.Fatalf("decode saved metadata: %v", err)
	}
	if meta["oauth_client_key"] != "antigravity-client" || meta["last_error"] != "antigravity token refresh returned 400: invalid_grant" || strings.TrimSpace(stringFromAny(meta["last_error_at"])) == "" {
		t.Fatalf("saved metadata = %#v, want merged antigravity refresh error", meta)
	}
}
