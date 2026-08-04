package oauth

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestStartOAuthFlowsRejectUnmarshalablePayloadsWhenRelaysAlreadyStartedOffline(t *testing.T) {
	ctx := context.Background()

	setCodexRelayStartedForCreateUpdateTest(t, true)
	setAntigravityRelayStartedForCreateUpdateTest(t, true)

	service := NewService(oauthStoreWithGorm(t, oauthGormWithCreate(t, func(tx *gorm.DB) {
		tx.AddError(errors.New("marshal guards should not create sessions"))
	})), "master-key")

	if _, err := service.StartCodexFlow(ctx, StartCodexFlowParams{
		PublicBaseURL: "https://backend.example.test",
		Metadata:      map[string]any{"bad": make(chan int)},
	}); err == nil || !strings.Contains(err.Error(), "marshal oauth metadata") {
		t.Fatalf("StartCodexFlow metadata marshal error = %v, want metadata marshal guard", err)
	}

	if _, err := service.StartAntigravityFlow(ctx, StartAntigravityFlowParams{
		PublicBaseURL: "https://backend.example.test",
		Site: PendingSite{
			GatewayConfig: []byte(`{`),
		},
	}); err == nil || !strings.Contains(err.Error(), "marshal oauth site payload") {
		t.Fatalf("StartAntigravityFlow site marshal error = %v, want site marshal guard", err)
	}
}

func TestCallbackRelaysReturnListenErrorsWhenPortsAreBusyOffline(t *testing.T) {
	setCodexRelayStartedForCreateUpdateTest(t, false)

	codexListener, err := net.Listen("tcp", ":1455")
	if err != nil {
		t.Fatalf("occupy codex relay port: %v", err)
	}
	defer codexListener.Close()

	if err := ensureCodexCallbackRelay(&store.Store{}); err == nil || !strings.Contains(err.Error(), "codex oauth callback relay failed to listen") {
		t.Fatalf("ensureCodexCallbackRelay error = %v, want listen failure", err)
	}

	setAntigravityRelayStartedForCreateUpdateTest(t, false)

	antigravityListener, err := net.Listen("tcp", ":1456")
	if err != nil {
		t.Fatalf("occupy antigravity relay port: %v", err)
	}
	defer antigravityListener.Close()

	if err := ensureAntigravityCallbackRelay(&store.Store{}); err == nil || !strings.Contains(err.Error(), "antigravity oauth callback relay failed to listen") {
		t.Fatalf("ensureAntigravityCallbackRelay error = %v, want listen failure", err)
	}
}

func TestHandleCodexCallbackRejectsExpiredSessionBeforeTokenExchangeOffline(t *testing.T) {
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		session, ok := tx.Statement.Dest.(*store.OAuthSession)
		if !ok {
			tx.AddError(errors.New("unexpected expired codex callback query destination"))
			return
		}
		*session = store.OAuthSession{
			ID:        uuid.New(),
			Provider:  codexProvider,
			State:     "expired-codex-session",
			Status:    "pending",
			ExpiresAt: time.Now().Add(-time.Minute),
		}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("expired codex callback should not save"))
	})

	if _, _, _, err := service.HandleCodexCallback(context.Background(), " expired-codex-session ", " code "); err == nil || !strings.Contains(err.Error(), "oauth session has expired") {
		t.Fatalf("HandleCodexCallback error = %v, want expired session guard", err)
	}
}

func TestMarkConnectionUnavailableBySiteIDSkipsNilStoreAndReturnsLookupErrorOffline(t *testing.T) {
	service := NewService(nil, "master-key")
	if err := service.MarkConnectionUnavailableBySiteID(context.Background(), uuid.New(), "boom"); err != nil {
		t.Fatalf("MarkConnectionUnavailableBySiteID nil store error = %v, want nil", err)
	}

	lookupErr := errors.New("site connection lookup stopped")
	service = oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		tx.AddError(lookupErr)
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("site lookup failure should not save"))
	})

	if err := service.MarkConnectionUnavailableBySiteID(context.Background(), uuid.New(), "boom"); !errors.Is(err, lookupErr) {
		t.Fatalf("MarkConnectionUnavailableBySiteID error = %v, want lookup error", err)
	}
}

func TestDisableSiteOnPermanentErrorOnlySavesEnabledBoundSiteOffline(t *testing.T) {
	siteID := uuid.New()
	queryCount := 0
	saveCount := 0
	var savedSite store.Site
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected permanent-error site query destination"))
			return
		}
		queryCount++
		*site = store.Site{ID: siteID, Name: "OAuth Site", Enabled: queryCount > 1}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		saveCount++
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected permanent-error site save destination"))
			return
		}
		savedSite = *site
		tx.Statement.RowsAffected = 1
	})

	service.disableSiteOnPermanentError(context.Background(), store.OAuthConnection{SiteID: &siteID}, "invalid_grant")
	if saveCount != 0 {
		t.Fatalf("save count after already-disabled site = %d, want no save", saveCount)
	}

	service.disableSiteOnPermanentError(context.Background(), store.OAuthConnection{SiteID: &siteID}, "codex token refresh returned 403: forbidden")
	if saveCount != 1 || savedSite.ID != siteID || savedSite.Enabled {
		t.Fatalf("saved site = %#v save count = %d, want enabled site disabled once", savedSite, saveCount)
	}
}

func setCodexRelayStartedForCreateUpdateTest(t *testing.T, started bool) {
	t.Helper()

	codexRelay.mu.Lock()
	originalDB := codexRelay.db
	originalServer := codexRelay.server
	originalStarted := codexRelay.started
	codexRelay.db = nil
	codexRelay.server = nil
	codexRelay.started = started
	codexRelay.mu.Unlock()

	t.Cleanup(func() {
		codexRelay.mu.Lock()
		codexRelay.db = originalDB
		codexRelay.server = originalServer
		codexRelay.started = originalStarted
		codexRelay.mu.Unlock()
	})
}

func setAntigravityRelayStartedForCreateUpdateTest(t *testing.T, started bool) {
	t.Helper()

	antigravityRelay.mu.Lock()
	originalDB := antigravityRelay.db
	originalServer := antigravityRelay.server
	originalStarted := antigravityRelay.started
	antigravityRelay.db = nil
	antigravityRelay.server = nil
	antigravityRelay.started = started
	antigravityRelay.mu.Unlock()

	t.Cleanup(func() {
		antigravityRelay.mu.Lock()
		antigravityRelay.db = originalDB
		antigravityRelay.server = originalServer
		antigravityRelay.started = originalStarted
		antigravityRelay.mu.Unlock()
	})
}
