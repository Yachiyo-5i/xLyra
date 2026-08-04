package auth

import (
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestAdminSessionTokenFromRequestOnlyReadsCookie(t *testing.T) {
	t.Parallel()

	req := authTestRequest("GET", "/")
	req.Header.Set("Authorization", "Bearer xlyra_session_header")
	req.Header.Set("X-Admin-Session", "xlyra_session_legacy")
	if got := AdminSessionTokenFromRequest(req); got != "" {
		t.Fatalf("expected admin session headers to be ignored, got %q", got)
	}

	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: "xlyra_session_cookie"})
	if got := AdminSessionTokenFromRequest(req); got != "xlyra_session_cookie" {
		t.Fatalf("expected cookie session token, got %q", got)
	}

	req = authTestRequest("GET", "/")
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: " \txlyra_session_cookie\n "})
	if got := AdminSessionTokenFromRequest(req); got != "xlyra_session_cookie" {
		t.Fatalf("expected trimmed cookie session token, got %q", got)
	}
}

func TestAdminAndAPIKeyContextHelpersRoundTrip(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	sessionID := uuid.New()
	accessTokenID := uuid.New()
	session := store.AdminSession{ID: sessionID, AdminID: adminID}
	ctx := WithAdminSession(context.Background(), session)

	if got, ok := AdminIDFromContext(ctx); !ok || got != adminID {
		t.Fatalf("admin id = %s, %v; want %s true", got, ok, adminID)
	}
	if got, ok := AdminSessionFromContext(ctx); !ok || got.ID != sessionID || got.AdminID != adminID {
		t.Fatalf("admin session = %#v, %v", got, ok)
	}
	if actor, ok := AdminActorFromContext(ctx); !ok || actor.Type != "session" || actor.SessionID != sessionID || actor.AdminID != adminID {
		t.Fatalf("admin actor = %#v, %v", actor, ok)
	}

	ctx = WithAdminActor(context.Background(), AdminActor{Type: "access_token", AdminID: adminID, AccessTokenID: accessTokenID})
	if got, ok := AdminIDFromContext(ctx); !ok || got != adminID {
		t.Fatalf("actor admin id = %s, %v", got, ok)
	}
	if actor, ok := AdminActorFromContext(ctx); !ok || actor.Type != "access_token" || actor.AccessTokenID != accessTokenID {
		t.Fatalf("access token actor = %#v, %v", actor, ok)
	}

	ctx = WithAdminActor(context.Background(), AdminActor{Type: "system"})
	if _, ok := AdminIDFromContext(ctx); ok {
		t.Fatal("actor without admin id should not populate admin id context")
	}
	if actor, ok := AdminActorFromContext(ctx); !ok || actor.Type != "system" {
		t.Fatalf("system actor = %#v, %v", actor, ok)
	}

	apiKey := store.APIKey{ID: uuid.New(), Name: "prod-key"}
	ctx = WithAPIKey(context.Background(), apiKey)
	if got, ok := APIKeyIDFromContext(ctx); !ok || got != apiKey.ID {
		t.Fatalf("api key id = %s, %v; want %s true", got, ok, apiKey.ID)
	}
	if got, ok := APIKeyFromContext(ctx); !ok || got.ID != apiKey.ID || got.Name != "prod-key" {
		t.Fatalf("api key = %#v, %v", got, ok)
	}

	if _, ok := AdminIDFromContext(context.Background()); ok {
		t.Fatal("empty context should not contain admin id")
	}
}

func TestLogoutAndRecordAuditNoopBranchesDoNotRequireStore(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if err := service.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout with empty token should be a no-op: %v", err)
	}

	var nilService *Service
	nilService.RecordAudit(context.Background(), AuditInput{
		Actor:        AdminActor{Type: "system"},
		Action:       "auth.noop",
		ResourceType: "test",
		ResourceID:   "noop",
		Success:      true,
	})
}

func TestRequireAPIKeyRejectsMissingKey(t *testing.T) {
	t.Parallel()

	service := &Service{}
	handler := service.RequireAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := authPerform(handler, authTestRequest(http.MethodPost, "/v1/chat/completions"))

	assertAuthErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestRequireAdminSessionRejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	service := &Service{}
	handler := service.RequireAdminSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := authPerform(handler, authTestRequest(http.MethodGet, "/api/v1/profile"))

	assertAuthErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestAPIKeyFromRequestPrefersBearerToken(t *testing.T) {
	t.Parallel()

	req := authTestRequest(http.MethodPost, "/v1/chat/completions")
	req.Header.Set("Authorization", "Bearer bearer-key")
	req.Header.Set("X-API-Key", "header-key")

	if got := apiKeyFromRequest(req); got != "bearer-key" {
		t.Fatalf("apiKeyFromRequest = %q, want bearer-key", got)
	}
}

func TestAPIKeyFromRequestFallsBackToXAPIKey(t *testing.T) {
	t.Parallel()

	req := authTestRequest(http.MethodPost, "/v1/chat/completions")
	req.Header.Set("Authorization", "Token ignored")
	req.Header.Set("X-API-Key", " header-key ")

	if got := apiKeyFromRequest(req); got != "header-key" {
		t.Fatalf("apiKeyFromRequest = %q, want header-key", got)
	}
}

func TestBearerTokenRequiresExactBearerPrefix(t *testing.T) {
	t.Parallel()

	if got := bearerToken("Bearer token-value"); got != "token-value" {
		t.Fatalf("bearerToken = %q, want token-value", got)
	}
	if got := bearerToken("Bearer \ttoken-value\n "); got != "token-value" {
		t.Fatalf("bearerToken with whitespace = %q, want token-value", got)
	}
	for _, value := range []string{"bearer token-value", "Token token-value", "Bearer", "Bearer    "} {
		if got := bearerToken(value); got != "" {
			t.Fatalf("bearerToken(%q) = %q, want empty", value, got)
		}
	}
}

func TestAPIKeyPrefixAndMaskToken(t *testing.T) {
	t.Parallel()

	shortGenerated := apiKeyPublicPrefix + "abc"
	if got := apiKeyPrefix(shortGenerated); got != shortGenerated {
		t.Fatalf("short generated prefix = %q, want %q", got, shortGenerated)
	}
	longGenerated := apiKeyPublicPrefix + strings.Repeat("z", apiKeySecretLength)
	if got := apiKeyPrefix(longGenerated); got != apiKeyPublicPrefix+strings.Repeat("z", 8) {
		t.Fatalf("long generated prefix = %q", got)
	}
	if got := apiKeyPrefix("short-custom"); got != "short-custom" {
		t.Fatalf("short custom prefix = %q", got)
	}
	if got := apiKeyPrefix("custom-key-value-with-long-tail"); got != "custom-key-value" {
		t.Fatalf("long custom prefix = %q", got)
	}

	if got := maskToken("short"); got != "****" {
		t.Fatalf("short mask = %q", got)
	}
	if got := maskToken("1234567890abcdef"); got != "1234567890...cdef" {
		t.Fatalf("long mask = %q", got)
	}
}

func TestNewTokenUsesPrefixAndURLSafeRandomPayload(t *testing.T) {
	t.Parallel()

	token, err := newToken("xlyra_session_", 24)
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	if !strings.HasPrefix(token, "xlyra_session_") {
		t.Fatalf("token prefix = %q", token)
	}
	raw := strings.TrimPrefix(token, "xlyra_session_")
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token payload should be raw URL base64: %v", err)
	}
	if len(decoded) != 24 {
		t.Fatalf("decoded token length = %d, want 24", len(decoded))
	}
}

func TestNewGatewayAPIKeyFormat(t *testing.T) {
	t.Parallel()

	key, err := newGatewayAPIKey()
	if err != nil {
		t.Fatalf("newGatewayAPIKey: %v", err)
	}
	if !strings.HasPrefix(key, apiKeyPublicPrefix) {
		t.Fatalf("generated key prefix = %q", key)
	}
	secret := strings.TrimPrefix(key, apiKeyPublicPrefix)
	if !validGeneratedAPIKeySecret(secret) {
		t.Fatalf("generated secret should pass validation: %q", secret)
	}
}

func TestValidateAdminPasswordRules(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{name: "valid", username: "admin", password: "Admin123", wantErr: false},
		{name: "blank username", username: " \t\n ", password: "Admin123", wantErr: true},
		{name: "too short", username: "admin", password: "Adm123", wantErr: true},
		{name: "no digit", username: "admin", password: "AdminOnly", wantErr: true},
		{name: "no letter", username: "admin", password: "12345678", wantErr: true},
		{name: "equals username", username: "Admin123", password: "Admin123", wantErr: true},
		{name: "equals username case insensitive", username: "Admin123", password: " admin123 ", wantErr: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateAdminPassword(tc.username, tc.password)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreateAdminRejectsInvalidPasswordBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.CreateAdmin(context.Background(), "admin", "short", "owner"); err == nil {
		t.Fatal("expected invalid admin password to be rejected")
	}
}

func TestBootstrapAdminRejectsInvalidPasswordBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.BootstrapAdmin(context.Background(), "admin", "short", "", "", "", ""); err == nil {
		t.Fatal("expected invalid bootstrap password to be rejected")
	}
}

func TestUpdateAdminProfileRejectsBlankUsernameBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.UpdateAdminProfile(context.Background(), uuid.New(), " \t\n ", "", ""); err == nil {
		t.Fatal("expected blank username to be rejected")
	}
}

func TestCreateAPIKeyRejectsBlankNameBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{Name: " \t\n "}, uuid.New()); err == nil {
		t.Fatal("expected blank api key name to be rejected")
	}
}

func TestCreateAPIKeyRejectsBlankModelMappingBeforeKeyGeneration(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		Name:       "prod",
		ModelRules: []store.APIKeyModelRule{{Pattern: "gpt-5", Target: " \t\n "}},
	}, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "model rule pattern and target must not be empty") {
		t.Fatalf("CreateAPIKey error = %v, want blank mapping rejection", err)
	}
}

func TestCreateAPIKeyRejectsBlankModelMappingKeyBeforeKeyGeneration(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		Name:       "prod",
		ModelRules: []store.APIKeyModelRule{{Pattern: " \t\n ", Target: "gpt-5"}},
	}, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "model rule pattern and target must not be empty") {
		t.Fatalf("CreateAPIKey error = %v, want blank mapping key rejection", err)
	}
}

func TestValidateAdminAndAPIKeyRejectMissingTokensBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.ValidateAdminSession(context.Background(), ""); err == nil || err.Error() != "missing session token" {
		t.Fatalf("ValidateAdminSession error = %v, want missing session token", err)
	}
	if _, err := service.ValidateAdminAccessToken(context.Background(), " \t\n ", "", ""); err == nil || err.Error() != "missing access token" {
		t.Fatalf("ValidateAdminAccessToken error = %v, want missing access token", err)
	}
	_, err := service.ValidateAPIKey(context.Background(), "")
	assertAuthErrorContains(t, "ValidateAPIKey", err, "missing api key")
}

func TestAPIKeyPlaintextRejectsUnavailableSecret(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, err := service.APIKeyPlaintext(store.APIKey{}); err == nil || !strings.Contains(err.Error(), "plaintext is unavailable") {
		t.Fatalf("APIKeyPlaintext error = %v, want unavailable plaintext", err)
	}
}

func TestAPIKeyPlaintextDecryptsStoredSecret(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	encrypted, _, err := service.credentials.Encrypt("xlyra-secret-value")
	if err != nil {
		t.Fatalf("encrypt api key secret: %v", err)
	}

	plaintext, err := service.APIKeyPlaintext(store.APIKey{
		EncryptedSecret: sql.NullString{String: encrypted, Valid: true},
	})
	if err != nil {
		t.Fatalf("APIKeyPlaintext returned error: %v", err)
	}
	if plaintext != "xlyra-secret-value" {
		t.Fatalf("APIKeyPlaintext = %q, want xlyra-secret-value", plaintext)
	}
}

func TestValidateModelRulesRejectsBlankRulesBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.validateModelRules(context.Background(), []store.APIKeyModelRule{
		{Pattern: "gpt-4o", Target: " \t\n "},
	}, "allow_all", nil)
	if err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("validateModelRules error = %v, want blank rule rejection", err)
	}
}

func TestSetAPIKeyModelMappingsRejectsBlankMappingsBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.SetAPIKeyModelMappings(context.Background(), uuid.New(), []store.APIKeyModelRule{
		{Pattern: " \t\n ", Target: "gpt-5"},
	})
	if err == nil || !strings.Contains(err.Error(), "model rule pattern and target must not be empty") {
		t.Fatalf("SetAPIKeyModelMappings error = %v, want blank rule rejection", err)
	}
}

func TestResolveCanonicalModelRejectsBlankModelKeyBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	_, err := service.resolveCanonicalModel(context.Background(), " \t\n ")
	if err == nil || err.Error() != "model_key is required" {
		t.Fatalf("resolveCanonicalModel error = %v, want model_key is required", err)
	}
}

func TestNewAdminAccessTokenFormat(t *testing.T) {
	t.Parallel()

	token, err := newAdminAccessToken()
	if err != nil {
		t.Fatalf("new admin access token: %v", err)
	}
	if !strings.HasPrefix(token, "xlyra-admin-") {
		t.Fatalf("unexpected token prefix: %q", token)
	}
	if got := len(strings.TrimPrefix(token, "xlyra-admin-")); got != 16 {
		t.Fatalf("expected 16 random chars, got %d", got)
	}
}

func TestGeneratedAPIKeyAlias(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("a", apiKeySecretLength)
	alias, ok := generatedAPIKeyAlias(apiKeyCompatiblePrefix + secret)
	if !ok {
		t.Fatal("expected compatible generated api key alias")
	}
	if want := apiKeyPublicPrefix + secret; alias != want {
		t.Fatalf("alias = %q, want %q", alias, want)
	}
	if alias, ok := generatedAPIKeyAlias(" \t" + apiKeyCompatiblePrefix + secret + "\n "); !ok || alias != apiKeyPublicPrefix+secret {
		t.Fatalf("trimmed alias = %q, %v; want %q true", alias, ok, apiKeyPublicPrefix+secret)
	}

	for _, key := range []string{
		apiKeyCompatiblePrefix + strings.Repeat("a", apiKeySecretLength-1),
		apiKeyCompatiblePrefix + strings.Repeat("a", apiKeySecretLength+1),
		apiKeyCompatiblePrefix + strings.Repeat("_", apiKeySecretLength),
		apiKeyPublicPrefix + secret,
		"custom-memory-key",
	} {
		if alias, ok := generatedAPIKeyAlias(key); ok {
			t.Fatalf("expected %q not to produce alias, got %q", key, alias)
		}
	}
}

func TestAPIKeyStoragePrefixUsesStablePublicAndCustomPrefixes(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("a", apiKeySecretLength)
	generatedKey := apiKeyPublicPrefix + secret
	if got := apiKeyStoragePrefix(generatedKey, apiKeyGeneratedKind); got != "xlyra-aaaaaaaa" {
		t.Fatalf("generated api key storage prefix = %q, want xlyra-aaaaaaaa", got)
	}

	customKey := "team-prod-key"
	wantCustom := "custom-" + hashToken(customKey)[:12]
	if got := apiKeyStoragePrefix(customKey, apiKeyCustomKind); got != wantCustom {
		t.Fatalf("custom api key storage prefix = %q, want %q", got, wantCustom)
	}
}

func TestValidateCustomGatewayAPIKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"my-familiar-key", "sk-short", "xlyra-short", "ABC-xyz-123"} {
		if err := validateCustomGatewayAPIKey(key); err != nil {
			t.Fatalf("expected custom key %q to be accepted: %v", key, err)
		}
	}
	for _, key := range []string{"", " leading", "trailing ", "contains space", "under_score", "中文", "キー", "dot.key", "slash/key"} {
		if err := validateCustomGatewayAPIKey(key); err == nil {
			t.Fatalf("expected custom key %q to be rejected", key)
		}
	}
}

func TestCreateGatewayAPIKeyValueRejectsInvalidCustomKeyBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if _, _, err := service.createGatewayAPIKeyValue(context.Background(), "contains space"); err == nil {
		t.Fatal("expected invalid custom api key to be rejected")
	}
}

func TestNormalizeUUIDsDropsNilAndDeduplicates(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	got := normalizeUUIDs([]uuid.UUID{uuid.Nil, first, second, first, uuid.Nil})
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("normalizeUUIDs = %#v, want [%s %s]", got, first, second)
	}
}

func TestAPIKeyAccessHelpersReturnEarlyWhenNoRepositoryLookupIsNeeded(t *testing.T) {
	t.Parallel()

	service := &Service{}
	allowedSiteIDs, err := service.effectiveAllowedSiteIDs(context.Background(), store.APIKey{SitePolicy: "allow_all"})
	if err != nil {
		t.Fatalf("effectiveAllowedSiteIDs allow_all returned error: %v", err)
	}
	if allowedSiteIDs != nil {
		t.Fatalf("allow_all allowed sites = %#v, want nil", allowedSiteIDs)
	}

	filtered, err := service.filterSiteModelIDsBySites(context.Background(), nil, []uuid.UUID{uuid.New()})
	if err != nil || filtered != nil {
		t.Fatalf("empty site models filter = %#v, err=%v; want nil nil", filtered, err)
	}
	filtered, err = service.filterSiteModelIDsBySites(context.Background(), []uuid.UUID{uuid.New()}, nil)
	if err != nil || filtered != nil {
		t.Fatalf("empty allowed sites filter = %#v, err=%v; want nil nil", filtered, err)
	}

	siteModelIDs, err := service.normalizeExistingSiteModelIDs(context.Background(), []uuid.UUID{uuid.Nil, uuid.Nil})
	if err != nil {
		t.Fatalf("normalizeExistingSiteModelIDs nil-only returned error: %v", err)
	}
	if len(siteModelIDs) != 0 {
		t.Fatalf("nil-only site model IDs = %#v, want empty", siteModelIDs)
	}

	modelKeys, err := service.normalizeExistingModelKeys(context.Background(), nil)
	if err != nil {
		t.Fatalf("normalizeExistingModelKeys empty returned error: %v", err)
	}
	if len(modelKeys) != 0 {
		t.Fatalf("empty model keys = %#v, want empty", modelKeys)
	}

	if _, err := service.normalizeExistingModelKey(context.Background(), " \t\n "); err == nil || err.Error() != "model_key is required" {
		t.Fatalf("normalizeExistingModelKey blank error = %v, want model_key is required", err)
	}
}

func TestNormalizeAPIKeyPoliciesDefaultToAllowAll(t *testing.T) {
	t.Parallel()

	if got := normalizeModelPolicy(" allow_list "); got != "allow_list" {
		t.Fatalf("normalizeModelPolicy allow_list = %q", got)
	}
	if got := normalizeModelPolicy("deny_all"); got != "allow_all" {
		t.Fatalf("normalizeModelPolicy unknown = %q, want allow_all", got)
	}
	if got := normalizeSitePolicy(" allow_list "); got != "allow_list" {
		t.Fatalf("normalizeSitePolicy allow_list = %q", got)
	}
	if got := normalizeSitePolicy(""); got != "allow_all" {
		t.Fatalf("normalizeSitePolicy empty = %q, want allow_all", got)
	}
}

func TestRateLimitInputFromStoreNormalizesStatusAndLimits(t *testing.T) {
	t.Parallel()

	item := store.GatewayRateLimit{
		Status:   store.RateLimitStatusEnabled,
		RPMLimit: sql.NullInt64{Int64: 60, Valid: true},
		TPMLimit: sql.NullInt64{Int64: 6000, Valid: true},
	}
	got := rateLimitInputFromStore(item)
	if got.Status != store.RateLimitStatusEnabled {
		t.Fatalf("rate limit status = %q, want enabled", got.Status)
	}
	if got.RPMLimit == nil || *got.RPMLimit != 60 {
		t.Fatalf("rpm limit = %#v, want 60", got.RPMLimit)
	}
	if got.TPMLimit == nil || *got.TPMLimit != 6000 {
		t.Fatalf("tpm limit = %#v, want 6000", got.TPMLimit)
	}
}

func TestRateLimitInputFromStoreDefaultsUnknownStatusToDisabled(t *testing.T) {
	t.Parallel()

	got := rateLimitInputFromStore(store.GatewayRateLimit{Status: "paused"})
	if got.Status != store.RateLimitStatusDisabled {
		t.Fatalf("rate limit status = %q, want disabled", got.Status)
	}
	if got.RPMLimit != nil || got.TPMLimit != nil {
		t.Fatalf("expected empty limits, got %#v", got)
	}
}

func TestSessionLifetimeReadsGeneralConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	confFile, err := config.LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	general := config.DefaultGeneralConfig()
	general.Security.SessionLifetimeHours = 168
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(general)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	service := &Service{confFile: confFile}
	if got := service.sessionLifetime(); got != 168*time.Hour {
		t.Fatalf("sessionLifetime = %s, want 168h", got)
	}
}

func TestSessionLifetimeDefaultsTo24Hours(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if got := service.sessionLifetime(); got != defaultSessionLifetime {
		t.Fatalf("sessionLifetime = %s, want %s", got, defaultSessionLifetime)
	}
}

func TestSessionLifetimeZeroNeverExpires(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	confFile, err := config.LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	general := config.DefaultGeneralConfig()
	general.Security.SessionLifetimeHours = 0
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(general)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	service := &Service{confFile: confFile}
	if got := service.sessionExpiresAt(time.Unix(1_700_000_000, 0)); got != nil {
		t.Fatalf("sessionExpiresAt = %v, want nil", got)
	}
}

func TestSessionExpiresAtAppliesConfiguredLifetime(t *testing.T) {
	t.Parallel()

	service := &Service{}
	now := time.Unix(1_700_000_000, 0)
	expiresAt := service.sessionExpiresAt(now)
	if expiresAt == nil {
		t.Fatal("sessionExpiresAt = nil, want default expiry")
	}
	if want := now.Add(defaultSessionLifetime); !expiresAt.Equal(want) {
		t.Fatalf("sessionExpiresAt = %s, want %s", expiresAt, want)
	}
}

func TestSessionLifetimeInvalidConfigDefaultsTo24Hours(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	confFile, err := config.LoadConfigFile(dir)
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	general := config.DefaultGeneralConfig()
	general.Security.SessionLifetimeHours = 721
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(general)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	service := &Service{confFile: confFile}
	if got := service.sessionLifetime(); got != defaultSessionLifetime {
		t.Fatalf("sessionLifetime = %s, want %s", got, defaultSessionLifetime)
	}
}

func TestNullableAndPointerHelpersForAPIKeyPersistence(t *testing.T) {
	t.Parallel()

	amount := 12.5
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	limit := int64(60)

	if got := nullableFloat(&amount); got != amount {
		t.Fatalf("nullableFloat = %#v, want %v", got, amount)
	}
	if got := nullableFloat(nil); got != nil {
		t.Fatalf("nullableFloat nil = %#v", got)
	}
	if got := nullableTime(&now); got != now {
		t.Fatalf("nullableTime = %#v, want %v", got, now)
	}
	zero := time.Time{}
	if got := nullableTime(&zero); got != nil {
		t.Fatalf("nullable zero time = %#v", got)
	}
	if got := nullFloatAsAny(sql.NullFloat64{Float64: amount, Valid: true}); got != amount {
		t.Fatalf("nullFloatAsAny = %#v", got)
	}
	if got := nullFloatAsAny(sql.NullFloat64{}); got != nil {
		t.Fatalf("nullFloatAsAny invalid = %#v", got)
	}
	if got := timePtrAsAny(&now); got != now {
		t.Fatalf("timePtrAsAny = %#v", got)
	}
	if got := timePtrAsAny(&zero); got != nil {
		t.Fatalf("timePtrAsAny zero = %#v", got)
	}
	if got := int64PtrAsAny(&limit); got != limit {
		t.Fatalf("int64PtrAsAny = %#v", got)
	}
	if got := int64PtrAsAny(nil); got != nil {
		t.Fatalf("int64PtrAsAny nil = %#v", got)
	}
}

func TestRateLimitStatusNormalization(t *testing.T) {
	t.Parallel()

	if got := normalizeRateLimitStatus(" " + store.RateLimitStatusEnabled + " "); got != store.RateLimitStatusEnabled {
		t.Fatalf("enabled status = %q", got)
	}
	if got := normalizeRateLimitStatus(""); got != store.RateLimitStatusDisabled {
		t.Fatalf("empty status = %q, want disabled", got)
	}
	if got := normalizeRateLimitStatus("paused"); got != "" {
		t.Fatalf("unknown status = %q, want empty", got)
	}
}

func TestUpsertAPIKeyRateLimitRejectsInvalidInputBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	_, err := upsertAPIKeyRateLimit(context.Background(), nil, uuid.New(), RateLimitInput{Status: "paused"})
	if err == nil || !strings.Contains(err.Error(), "rate_limit.status") {
		t.Fatalf("invalid status error = %v", err)
	}

	rpm := int64(0)
	_, err = upsertAPIKeyRateLimit(context.Background(), nil, uuid.New(), RateLimitInput{Status: store.RateLimitStatusEnabled, RPMLimit: &rpm})
	if err == nil || !strings.Contains(err.Error(), "rpm_limit") {
		t.Fatalf("invalid rpm error = %v", err)
	}

	tpm := int64(-1)
	_, err = upsertAPIKeyRateLimit(context.Background(), nil, uuid.New(), RateLimitInput{Status: store.RateLimitStatusEnabled, TPMLimit: &tpm})
	if err == nil || !strings.Contains(err.Error(), "tpm_limit") {
		t.Fatalf("invalid tpm error = %v", err)
	}
}

func TestParseIPHandlesHostPortAndInvalidAddress(t *testing.T) {
	t.Parallel()

	if got := parseIP("127.0.0.1:8080"); got == nil || got.String() != "127.0.0.1" {
		t.Fatalf("parse host:port = %v", got)
	}
	if got := parseIP("::1"); got == nil || got.String() != "::1" {
		t.Fatalf("parse ipv6 = %v", got)
	}
	if got := parseIP("not an ip"); got != nil {
		t.Fatalf("invalid ip = %v, want nil", got)
	}
}

func TestTOTPURLIncludesIssuerAndAccount(t *testing.T) {
	t.Parallel()

	secret := "JBSWY3DPEHPK3PXP"
	got := totpURL("xLyra", "admin@example.com", secret)
	if !strings.HasPrefix(got, "otpauth://totp/") {
		t.Fatalf("unexpected totp url: %s", got)
	}
	if !strings.Contains(got, "secret="+secret) || !strings.Contains(got, "issuer=xLyra") || !strings.Contains(got, "algorithm=SHA1") {
		t.Fatalf("totp url missing expected query fields: %s", got)
	}
}

func TestTOTPVerifyAcceptsGeneratedCode(t *testing.T) {
	t.Parallel()

	secret, err := newTOTPSecret()
	if err != nil {
		t.Fatalf("new totp secret: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	secretBytes := mustDecodeTOTPSecret(t, secret)
	code := totpCode(secretBytes, now.Unix()/totpPeriodSeconds)
	if !verifyTOTP(secret, code, now) {
		t.Fatal("expected generated TOTP code to verify")
	}
}

func TestVerifyTOTPRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	secretBytes := mustDecodeTOTPSecret(t, secret)
	validCode := totpCode(secretBytes, now.Unix()/totpPeriodSeconds)
	wrongCode := "000000"
	if wrongCode == validCode {
		wrongCode = "111111"
	}

	for _, tc := range []struct {
		name   string
		secret string
		code   string
	}{
		{name: "too short", secret: secret, code: validCode[:5]},
		{name: "too long", secret: secret, code: validCode + "0"},
		{name: "non digit", secret: secret, code: validCode[:5] + "x"},
		{name: "bad base32 secret", secret: "not-base32", code: validCode},
		{name: "wrong code", secret: secret, code: wrongCode},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if verifyTOTP(tc.secret, tc.code, now) {
				t.Fatal("expected TOTP verification to reject invalid input")
			}
		})
	}
}

func mustDecodeTOTPSecret(t *testing.T, secret string) []byte {
	t.Helper()
	secretBytes, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return secretBytes
}
