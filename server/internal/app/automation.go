package app

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/jsplugin"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

// automationHost lets automation plugins' actions reach xLyra's own services.
type automationHost struct {
	auth *auth.Service
}

func (h automationHost) ResetAPIKeyQuota(ctx context.Context, id uuid.UUID, scopes []string) error {
	_, err := h.auth.ResetAPIKeyQuota(ctx, id, scopes)
	return err
}

// wireJSPluginAutomation connects the events xLyra emits to the plugin manager
// and starts the loop that runs the plugins and carries out their actions.
func wireJSPluginAutomation(oauth *oauthsvc.Service, manager *jsplugin.Manager, authService *auth.Service) {
	if manager == nil {
		return
	}
	if oauth != nil {
		oauth.SetQuotaSyncEmitter(func(ctx context.Context, tx *gorm.DB, connection store.OAuthConnection, previous, current map[string]any) error {
			return manager.EmitEvent(ctx, tx, jsplugin.EmittedEvent{
				Type: "oauth.quota_synced",
				Subject: jsplugin.AutomationSubject{
					Type:     "oauth_connection",
					ID:       connection.ID.String(),
					Provider: connection.Provider,
					Label:    connection.Email,
				},
				Previous: previous,
				Current:  current,
			})
		})
	}
	if authService != nil {
		manager.StartAutomation(context.Background(), automationHost{auth: authService})
	}
}
