package auth

import (
	"context"
	"encoding/base32"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
)

func TestNewServiceUsesOptionalConfigFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	confFile, err := config.LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	general := config.DefaultGeneralConfig()
	general.Security.SessionLifetimeHours = 2
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(general)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	service := NewService(nil, "test-master-key", confFile)
	if service.confFile != confFile {
		t.Fatal("NewService should keep the optional config file")
	}
	if got := service.sessionLifetime(); got != 2*time.Hour {
		t.Fatalf("sessionLifetime = %s, want 2h", got)
	}
}

func TestCreateAPIKeyRejectsInvalidCustomKeyAfterNameNormalization(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		Name:      " \tprod key\n ",
		CustomKey: "bad custom key",
	}, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "custom api key may only contain") {
		t.Fatalf("CreateAPIKey error = %v, want custom key validation error", err)
	}
}

func TestNewTOTPSecretIsBase32NoPaddingTwentyBytes(t *testing.T) {
	t.Parallel()

	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatalf("newTOTPSecret: %v", err)
	}
	if strings.Contains(secret, "=") {
		t.Fatalf("secret should not contain base32 padding: %q", secret)
	}
	if strings.ToUpper(secret) != secret {
		t.Fatalf("secret should use uppercase base32 characters: %q", secret)
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if len(decoded) != 20 {
		t.Fatalf("decoded secret length = %d, want 20", len(decoded))
	}
}

func TestVerifyTOTPAcceptsTrimmedLowercaseSecretAndAdjacentWindow(t *testing.T) {
	t.Parallel()

	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	secretBytes := mustDecodeTOTPSecret(t, secret)
	counter := now.Unix() / totpPeriodSeconds

	previousCode := totpCode(secretBytes, counter-1)
	if !verifyTOTP(" \t"+strings.ToLower(secret)+"\n ", " "+previousCode+"\n", now) {
		t.Fatal("expected previous-window code with normalized secret and code whitespace to verify")
	}

	nextCode := totpCode(secretBytes, counter+1)
	if !verifyTOTP(secret, nextCode, now) {
		t.Fatal("expected next-window code to verify")
	}
}
