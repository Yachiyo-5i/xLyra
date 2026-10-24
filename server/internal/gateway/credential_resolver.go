package gateway

import (
	"context"
	"errors"

	"github.com/google/uuid"

	oauthsvc "xlyra/server/internal/oauth"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

// PluginAccountRefresher renews one plugin account-pool credential.
// siteID and credentialID identify the stored bundle. fields is the bag this
// request already decrypted. The returned bag is what this request uses.
// A successful refresh must be written back. A dead grant must match
// accountauth.ErrReauthorizationRequired.
type PluginAccountRefresher interface {
	Refresh(ctx context.Context, siteID, credentialID uuid.UUID, siteType string, fields map[string]string) (map[string]string, error)
}

// gatewayOAuth is the oauth surface the gateway calls. *oauthsvc.Service
// implements it; tests supply a fake.
type gatewayOAuth interface {
	EnsureCodexConnectionFresh(ctx context.Context, siteID uuid.UUID) (oauthsvc.CodexConnection, error)
	EnsureAntigravityConnectionFresh(ctx context.Context, siteID uuid.UUID) (oauthsvc.CodexConnection, error)
	EnsureClaudeCodeConnectionFresh(ctx context.Context, siteID uuid.UUID) (oauthsvc.CodexConnection, error)
	EnsureGrokAccessToken(ctx context.Context, credentialID uuid.UUID) (string, error)
	MarkConnectionUnavailableBySiteID(ctx context.Context, siteID uuid.UUID, errMsg string) error
}

type resolvedUpstreamCredential struct {
	Key       string
	AccountID string
	// BaseURL overrides the site address. Empty keeps the site address.
	BaseURL string
	// HeaderVars supplies {{cred.*}} values for plugin request header templates.
	// It is not passed to gatewayProtocolHeaderAdapter. Nil means the site has
	// no credential resolver: an API-key plugin site fills cred.api_key from
	// the decrypted upstream key.
	HeaderVars map[string]string
}

type credentialResolveFailure struct {
	ErrorType         string
	Err               error
	TryNextCredential bool
	MarkReauth        bool
}

type credentialResolver interface {
	Resolve(ctx context.Context, h Handler, candidate routeengine.Candidate, credential store.GatewayCredential, key string, accountID string) (resolvedUpstreamCredential, *credentialResolveFailure)
}

type oauthConnectionRefresher func(gatewayOAuth, context.Context, uuid.UUID) (oauthsvc.CodexConnection, error)

type oauthConnectionCredentialResolver struct {
	errorType string
	refresh   oauthConnectionRefresher
}

func (r oauthConnectionCredentialResolver) Resolve(ctx context.Context, h Handler, candidate routeengine.Candidate, credential store.GatewayCredential, key string, accountID string) (resolvedUpstreamCredential, *credentialResolveFailure) {
	connection, err := r.refresh(h.oauth, ctx, candidate.Site.ID)
	if err != nil {
		return resolvedUpstreamCredential{}, &credentialResolveFailure{
			ErrorType:         r.errorType,
			Err:               err,
			TryNextCredential: false,
		}
	}
	return resolvedUpstreamCredential{Key: connection.AccessToken, AccountID: connection.AccountID}, nil
}

type grokCredentialResolver struct{}

func (grokCredentialResolver) Resolve(ctx context.Context, h Handler, candidate routeengine.Candidate, credential store.GatewayCredential, key string, accountID string) (resolvedUpstreamCredential, *credentialResolveFailure) {
	accessToken, err := h.oauth.EnsureGrokAccessToken(ctx, credential.Credential.ID)
	if err != nil {
		return resolvedUpstreamCredential{}, &credentialResolveFailure{
			ErrorType:         "grok_oauth_refresh_failed",
			Err:               err,
			TryNextCredential: true,
			MarkReauth:        errors.Is(err, oauthsvc.ErrGrokReauthorizationRequired),
		}
	}
	return resolvedUpstreamCredential{Key: accessToken, AccountID: accountID}, nil
}

func credentialResolverForSite(siteType string) credentialResolver {
	switch {
	case isCodexSite(siteType):
		return oauthConnectionCredentialResolver{errorType: "codex_oauth_refresh_failed", refresh: gatewayOAuth.EnsureCodexConnectionFresh}
	case isAntigravitySite(siteType):
		return oauthConnectionCredentialResolver{errorType: "antigravity_oauth_refresh_failed", refresh: gatewayOAuth.EnsureAntigravityConnectionFresh}
	case isClaudeCodeSite(siteType):
		return oauthConnectionCredentialResolver{errorType: "claude_code_oauth_refresh_failed", refresh: gatewayOAuth.EnsureClaudeCodeConnectionFresh}
	case isGrokSite(siteType):
		return grokCredentialResolver{}
	default:
		return nil
	}
}

// credentialResolverFor keeps the built-in site resolvers and adds the plugin
// account-pool resolver only for account:<siteType>:<uuid> on a registered pool.
func credentialResolverFor(siteType, credentialType string) credentialResolver {
	if resolver := credentialResolverForSite(siteType); resolver != nil {
		return resolver
	}
	if pluginAccountPoolCredential(siteType, credentialType) {
		return pluginAccountCredentialResolver{}
	}
	return nil
}
