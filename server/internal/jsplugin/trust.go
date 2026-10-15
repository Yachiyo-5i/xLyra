package jsplugin

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

// TrustStatus describes whether a package signer is trusted for enable.
type TrustStatus string

const (
	TrustTrusted         TrustStatus = "trusted"
	TrustUntrustedSigner TrustStatus = "untrusted_signer"
	TrustUnsigned        TrustStatus = "unsigned"
)

// ConfirmRequiredError is returned when enable needs confirm_untrusted.
type ConfirmRequiredError struct {
	Trust TrustStatus
}

func (e *ConfirmRequiredError) Error() string {
	return "enable requires confirm_untrusted"
}

func (m *Manager) TrustStatus(ctx context.Context, signerFingerprint string) (TrustStatus, error) {
	if strings.TrimSpace(signerFingerprint) == "" {
		return TrustUnsigned, nil
	}
	ok, err := m.repo.IsTrustedFingerprint(ctx, signerFingerprint)
	if err != nil {
		return "", err
	}
	if ok {
		return TrustTrusted, nil
	}
	return TrustUntrustedSigner, nil
}

func (m *Manager) ListTrustedKeys(ctx context.Context) ([]store.JSPluginTrustedKey, error) {
	return m.repo.ListTrustedKeys(ctx)
}

const maxTrustedKeyNameRunes = 64

func (m *Manager) AddTrustedKey(ctx context.Context, adminID, name, publicKeyInput string) (store.JSPluginTrustedKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.JSPluginTrustedKey{}, &InvalidTrustedKeyError{Err: fmt.Errorf("name is required")}
	}
	if utf8.RuneCountInString(name) > maxTrustedKeyNameRunes {
		return store.JSPluginTrustedKey{}, &InvalidTrustedKeyError{Err: fmt.Errorf("name must be at most %d characters", maxTrustedKeyNameRunes)}
	}
	pub, err := ParseEd25519PublicKey(publicKeyInput)
	if err != nil {
		return store.JSPluginTrustedKey{}, &InvalidTrustedKeyError{Err: err}
	}
	fingerprint := PublicKeyFingerprint(pub)
	if _, err := m.repo.GetTrustedKeyByFingerprint(ctx, fingerprint); err == nil {
		return store.JSPluginTrustedKey{}, ErrTrustedKeyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.JSPluginTrustedKey{}, err
	}
	row := store.JSPluginTrustedKey{
		Name:        name,
		PublicKey:   base64.StdEncoding.EncodeToString(pub),
		Fingerprint: fingerprint,
	}
	if adminID != "" {
		if id, err := uuid.Parse(adminID); err == nil {
			row.CreatedBy = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	if err := m.repo.CreateTrustedKey(ctx, &row); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return store.JSPluginTrustedKey{}, ErrTrustedKeyExists
		}
		return store.JSPluginTrustedKey{}, err
	}
	return row, nil
}

func (m *Manager) DeleteTrustedKey(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return gorm.ErrRecordNotFound
	}
	return m.repo.DeleteTrustedKey(ctx, parsed)
}

// ParseEd25519PublicKey accepts standard or URL-safe base64, with or without padding,
// and tolerates the `public_key=` prefix printed by `xlyra-plugin keygen`.
func ParseEd25519PublicKey(input string) ([]byte, error) {
	value := strings.TrimSpace(input)
	value = strings.TrimPrefix(value, "public_key=")
	value = strings.Join(strings.Fields(value), "")
	if value == "" {
		return nil, fmt.Errorf("public_key is required")
	}
	var pub []byte
	var decodeErr error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		pub, decodeErr = enc.DecodeString(value)
		if decodeErr == nil {
			break
		}
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("public_key is not valid base64")
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public_key must decode to %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	return pub, nil
}
