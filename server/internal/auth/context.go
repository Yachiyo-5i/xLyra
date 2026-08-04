package auth

import (
	"context"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type contextKey string

const (
	adminIDKey      contextKey = "admin_id"
	adminSessionKey contextKey = "admin_session"
	adminActorKey   contextKey = "admin_actor"
	apiKeyIDKey     contextKey = "api_key_id"
	apiKeyKey       contextKey = "api_key"
)

type AdminActor struct {
	Type          string
	AdminID       uuid.UUID
	SessionID     uuid.UUID
	AccessTokenID uuid.UUID
}

func WithAdminID(ctx context.Context, adminID uuid.UUID) context.Context {
	return context.WithValue(ctx, adminIDKey, adminID)
}

func AdminIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	adminID, ok := ctx.Value(adminIDKey).(uuid.UUID)
	return adminID, ok
}

func WithAdminSession(ctx context.Context, session store.AdminSession) context.Context {
	ctx = WithAdminID(ctx, session.AdminID)
	ctx = WithAdminActor(ctx, AdminActor{Type: "session", AdminID: session.AdminID, SessionID: session.ID})
	return context.WithValue(ctx, adminSessionKey, session)
}

func AdminSessionFromContext(ctx context.Context) (store.AdminSession, bool) {
	session, ok := ctx.Value(adminSessionKey).(store.AdminSession)
	return session, ok
}

func WithAdminActor(ctx context.Context, actor AdminActor) context.Context {
	if actor.AdminID != uuid.Nil {
		ctx = WithAdminID(ctx, actor.AdminID)
	}
	return context.WithValue(ctx, adminActorKey, actor)
}

func AdminActorFromContext(ctx context.Context) (AdminActor, bool) {
	actor, ok := ctx.Value(adminActorKey).(AdminActor)
	return actor, ok
}

func WithAPIKeyID(ctx context.Context, apiKeyID uuid.UUID) context.Context {
	return context.WithValue(ctx, apiKeyIDKey, apiKeyID)
}

func APIKeyIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	apiKeyID, ok := ctx.Value(apiKeyIDKey).(uuid.UUID)
	return apiKeyID, ok
}

func WithAPIKey(ctx context.Context, apiKey store.APIKey) context.Context {
	ctx = WithAPIKeyID(ctx, apiKey.ID)
	return context.WithValue(ctx, apiKeyKey, apiKey)
}

func APIKeyFromContext(ctx context.Context) (store.APIKey, bool) {
	apiKey, ok := ctx.Value(apiKeyKey).(store.APIKey)
	return apiKey, ok
}
