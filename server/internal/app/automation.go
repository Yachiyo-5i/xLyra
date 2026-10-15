package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
	http *http.Client
}

const automationHTTPTimeout = 10 * time.Second

// automationDroppedHeaders are never sent, whatever a plugin asks for.
var automationDroppedHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true, "connection": true, "proxy-authorization": true,
}

// DoHTTP sends a request an automation plugin asked for. The plugin has already
// been held to the addresses the admin entered; here the request is checked
// once more and sent through the guarded client. Errors never carry the URL,
// which may hold a key.
func (h automationHost) DoHTTP(ctx context.Context, req jsplugin.HTTPRequest) (int, error) {
	target, err := url.Parse(req.URL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return 0, errors.New("the address must be an http(s) URL without credentials")
	}
	ctx, cancel := context.WithTimeout(ctx, automationHTTPTimeout)
	defer cancel()
	var body io.Reader
	if req.Body != "" && req.Method != http.MethodGet {
		body = strings.NewReader(req.Body)
	}
	request, err := http.NewRequestWithContext(ctx, req.Method, target.String(), body)
	if err != nil {
		return 0, errors.New("the request is invalid")
	}
	for name, value := range req.Headers {
		lower := strings.ToLower(name)
		if automationDroppedHeaders[lower] || strings.HasPrefix(lower, "x-forwarded-") {
			continue
		}
		request.Header.Set(name, value)
	}
	client := h.http
	if client == nil {
		client = http.DefaultClient
	}
	// The plugin was held to the addresses the admin entered, so a redirect may
	// not lead anywhere else either. Copying the client keeps the shared one as is.
	sameOrigin := *client
	sameOrigin.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if next.URL.Scheme != via[0].URL.Scheme || !strings.EqualFold(next.URL.Host, via[0].URL.Host) {
			return errors.New("the server redirected to another address")
		}
		return nil
	}
	response, err := sameOrigin.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, fmt.Errorf("the request failed: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	return response.StatusCode, nil
}

func (h automationHost) ResetAPIKeyQuota(ctx context.Context, id uuid.UUID, scopes []string) error {
	_, err := h.auth.ResetAPIKeyQuota(ctx, id, scopes)
	return err
}

// wireJSPluginAutomation connects the events xLyra emits to the plugin manager
// and starts the loop that runs the plugins and carries out their actions.
func wireJSPluginAutomation(oauth *oauthsvc.Service, manager *jsplugin.Manager, authService *auth.Service, httpClient *http.Client) {
	if manager == nil {
		return
	}
	if oauth != nil {
		oauth.SetQuotaSyncEmitter(func(ctx context.Context, tx *gorm.DB, connection store.OAuthConnection, previous, current map[string]any) error {
			return manager.EmitEvent(ctx, tx, jsplugin.EmittedEvent{
				Type:       "oauth.quota_synced",
				EntityType: jsplugin.EntityOAuthConnection,
				EntityID:   connection.ID.String(),
				Previous:   previous,
				Current:    current,
			})
		})
	}
	if authService != nil {
		manager.StartAutomation(context.Background(), automationHost{auth: authService, http: httpClient})
	}
}
