package site

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type accountLoginProvider[T any, A any] interface {
	name() string
	requireSite(ctx context.Context, siteID uuid.UUID) error
	isCredential(credentialType string) bool
	credentialPrefix() string
	normalize(tokens T) (T, error)
	encodeSecret(tokens T) (string, error)
	identity(tokens T) string
	metaIdentity(meta map[string]any) string
	secretIdentity(secret string) string
	applyMeta(meta map[string]any, tokens T)
	maskedLabel(tokens T) string
	snapshot(ctx context.Context, credential store.SiteCredential) A
}

type accountLogin[T any, A any] struct {
	s *Service
	p accountLoginProvider[T, A]
}

func (l accountLogin[T, A]) complete(ctx context.Context, siteID uuid.UUID, tokens T, targetCredentialID *uuid.UUID) (A, error) {
	var zero A
	if err := l.p.requireSite(ctx, siteID); err != nil {
		return zero, err
	}
	tokens, err := l.p.normalize(tokens)
	if err != nil {
		return zero, err
	}
	defer l.s.siteLocks.lock(siteID)()
	secret, err := l.p.encodeSecret(tokens)
	if err != nil {
		return zero, err
	}
	encrypted, _, err := l.s.credentials.Encrypt(secret)
	if err != nil {
		return zero, err
	}
	var credential store.SiteCredential
	err = l.s.db.WithinTx(ctx, func(tx store.Tx) error {
		credentialRepo := store.NewSiteCredentialRepository(tx)
		credentials, err := credentialRepo.ListBySite(ctx, siteID)
		if err != nil {
			return err
		}
		existing, found, err := l.matchExisting(credentials, siteID, targetCredentialID, tokens)
		if err != nil {
			return err
		}
		meta := map[string]any{}
		credentialType := l.p.credentialPrefix() + uuid.NewString()
		if found {
			meta = accountMetaMap(existing.Meta)
			credentialType = existing.CredentialType
		}
		meta["enabled"] = true
		delete(meta, "reauth_required")
		l.p.applyMeta(meta, tokens)
		encodedMeta, err := json.Marshal(meta)
		if err != nil {
			return fmt.Errorf("marshal %s account meta: %w", l.p.name(), err)
		}
		credential, err = credentialRepo.Upsert(ctx, store.UpsertSiteCredentialParams{
			SiteID:          siteID,
			CredentialType:  credentialType,
			EncryptedSecret: encrypted,
			MaskedSecret:    l.p.maskedLabel(tokens),
			Meta:            encodedMeta,
		})
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if err := store.NewRouteCooldownRepository(tx).DeleteBySiteCredential(ctx, siteID, credential.ID); err != nil {
			return err
		}
		states, err := store.NewSiteAPIKeyStateRepository(tx).ListBySite(ctx, siteID)
		if err != nil {
			return err
		}
		for _, state := range states {
			if state.SiteCredentialID == credential.ID {
				_, err = store.NewSiteAPIKeyStateRepository(tx).RecoverPending(ctx, credential.ID)
				return err
			}
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return l.p.snapshot(ctx, credential), nil
}

func (l accountLogin[T, A]) matchExisting(credentials []store.SiteCredential, siteID uuid.UUID, targetCredentialID *uuid.UUID, tokens T) (store.SiteCredential, bool, error) {
	if targetCredentialID != nil && *targetCredentialID != uuid.Nil {
		for _, credential := range credentials {
			if credential.ID == *targetCredentialID && credential.SiteID == siteID && l.p.isCredential(credential.CredentialType) {
				return credential, true, nil
			}
		}
		return store.SiteCredential{}, false, fmt.Errorf("%s account was not found", l.p.name())
	}
	wanted := l.p.identity(tokens)
	if wanted == "" {
		return store.SiteCredential{}, false, nil
	}
	for _, credential := range credentials {
		if !l.p.isCredential(credential.CredentialType) {
			continue
		}
		identity := l.p.metaIdentity(accountMetaMap(credential.Meta))
		if identity == "" {
			if decrypted, err := l.s.credentials.Decrypt(credential.EncryptedSecret); err == nil {
				identity = l.p.secretIdentity(decrypted)
			}
		}
		if identity == wanted {
			return credential, true, nil
		}
	}
	return store.SiteCredential{}, false, nil
}

func accountMaskedLabel(email string, accessToken string) string {
	if trimmed := strings.TrimSpace(email); trimmed != "" {
		return trimmed
	}
	token := strings.TrimSpace(accessToken)
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "…" + token[len(token)-4:]
}
