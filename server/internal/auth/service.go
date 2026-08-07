package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/credential"
	"xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

const (
	defaultSessionLifetime = 24 * time.Hour
	maxSessionLifetime     = 720 * time.Hour
	apiKeySecretBytes      = 32
	sessionTokenBytes      = 32
)

var ErrAlreadyInitialized = errors.New("admin bootstrap already completed")
var ErrTOTPRequired = errors.New("totp code is required")
var ErrTOTPInvalid = errors.New("totp code is invalid")
var ErrModelNotAllowed = errors.New("api key is not allowed to access model")
var ErrSiteNotAllowed = errors.New("api key is not allowed to access site")

type Service struct {
	db              *gorm.DB
	confFile        *config.ConfigFile
	masterKey       string
	admins          store.AdminRepository
	sessions        store.AdminSessionRepository
	accessTokens    store.AdminAccessTokenRepository
	auditLogs       store.AdminAuditLogRepository
	apiKeys         store.APIKeyRepository
	canonicalModels store.CanonicalModelRepository
	credentials     credential.Service
	timeZone        config.TimeZone
	loginGuard      *loginGuard
	totpReplay      *totpReplayGuard
}

// verifyTOTPForAdmin validates a code and rejects replays of a code already used
// by this admin within its validity window.
func (s *Service) verifyTOTPForAdmin(adminID uuid.UUID, secret string, code string) bool {
	counter, ok := matchTOTPCounter(secret, code, time.Now())
	if !ok {
		return false
	}
	return s.totpReplay.accept(adminID, counter)
}

type LoginResult struct {
	Token     string
	CSRFToken string
	ExpiresAt *time.Time
	Admin     store.Admin
	SessionID uuid.UUID
}

type AccessTokenResult struct {
	Token       string
	AccessToken store.AdminAccessToken
}

type AuditInput struct {
	Actor        AdminActor
	Action       string
	ResourceType string
	ResourceID   string
	RemoteAddr   string
	UserAgent    string
	RequestID    string
	Success      bool
	ErrorCode    string
	Metadata     any
}

type TOTPSetupResult struct {
	Secret     string
	OtpauthURL string
	Admin      store.Admin
}

type BootstrapStatus struct {
	Initialized bool
	AdminCount  int
}

type CreateAPIKeyResult struct {
	Key       string
	KeyPrefix string
	APIKey    store.APIKey
}

type CreateAPIKeyInput struct {
	Name                 string
	CustomKey            string
	ModelPolicy          string
	SiteModelIDs         []uuid.UUID
	SitePolicy           string
	SiteIDs              []uuid.UUID
	SiteGroupIDs         []uuid.UUID
	ModelRules           []store.APIKeyModelRule
	ImageToolBridge      *ImageToolBridgeInput
	QuotaLimit           *float64
	QuotaUnlimited       bool
	QuotaDailyLimit      *float64
	QuotaDailyUnlimited  *bool
	QuotaWeeklyLimit     *float64
	QuotaWeeklyUnlimited *bool
	ExpiresAt            *time.Time
	RateLimit            *RateLimitInput
}

type UpdateAPIKeyInput struct {
	Name                 string
	Status               string
	ModelPolicy          string
	SiteModelIDs         []uuid.UUID
	SitePolicy           string
	SiteIDs              []uuid.UUID
	SiteGroupIDs         []uuid.UUID
	ModelRules           []store.APIKeyModelRule
	ImageToolBridge      *ImageToolBridgeInput
	QuotaLimit           *float64
	QuotaUnlimited       bool
	QuotaDailyLimit      *float64
	QuotaDailyUnlimited  *bool
	QuotaWeeklyLimit     *float64
	QuotaWeeklyUnlimited *bool
	ExpiresAt            *time.Time
	RateLimit            *RateLimitInput
}

type APIKeyDetails struct {
	APIKey    store.APIKey
	Models    []store.APIKeySiteModelPermissionDetail
	Sites     []store.APIKeySitePermission
	Groups    []store.APIKeySiteGroupPermission
	RateLimit RateLimitInput
}

type IncreaseAPIKeyQuotaResult struct {
	APIKey      store.APIKey
	Amount      float64
	LimitBefore float64
}

type ResetAPIKeyQuotaResult struct {
	APIKey           store.APIKey
	Scopes           []string
	TotalUsedBefore  float64
	DailyUsedBefore  float64
	WeeklyUsedBefore float64
}

type ImageToolBridgeInput struct {
	Enabled  bool
	Model    string
	SiteID   *uuid.UUID
	MaxCalls *int
}

type RateLimitInput struct {
	Status   string
	RPMLimit *int64
	TPMLimit *int64
}

type APIKeyModelAccess struct {
	Allowed  bool
	APIKey   store.APIKey
	ModelKey string
}

type APIKeyAccessSets struct {
	APIKey              store.APIKey
	AllowedSiteIDs      []uuid.UUID
	AllowedSiteModelIDs []uuid.UUID
}

type APIKeyRouteAccess struct {
	Allowed             bool
	APIKey              store.APIKey
	RequestedModel      string
	ModelKey            string
	CanonicalModel      store.CanonicalModel
	AllowedSiteIDs      []uuid.UUID
	AllowedSiteModelIDs []uuid.UUID
}

func NewService(db *gorm.DB, masterKey string, confFiles ...*config.ConfigFile) *Service {
	var confFile *config.ConfigFile
	if len(confFiles) > 0 {
		confFile = confFiles[0]
	}
	return &Service{
		db:              db,
		confFile:        confFile,
		masterKey:       masterKey,
		admins:          store.NewAdminRepository(db),
		sessions:        store.NewAdminSessionRepository(db),
		accessTokens:    store.NewAdminAccessTokenRepository(db),
		auditLogs:       store.NewAdminAuditLogRepository(db),
		apiKeys:         store.NewAPIKeyRepository(db),
		canonicalModels: store.NewCanonicalModelRepository(db),
		credentials:     credential.NewService(masterKey),
		timeZone:        config.ResolveTimeZone(),
		loginGuard:      newLoginGuard(),
		totpReplay:      newTOTPReplayGuard(),
	}
}

func (s *Service) CreateAdmin(ctx context.Context, username string, password string, role string) (store.Admin, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if err := validateAdminPassword(username, password); err != nil {
		return store.Admin{}, err
	}

	passwordHash, err := crypto.HashPassword(password)
	if err != nil {
		return store.Admin{}, fmt.Errorf("hash password: %w", err)
	}

	if role == "" {
		role = "owner"
	}

	admin, err := s.admins.CreateInitial(ctx, store.CreateAdminParams{
		Username:     username,
		PasswordHash: passwordHash,
		Role:         role,
		Status:       "active",
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return store.Admin{}, ErrAlreadyInitialized
		}
		return store.Admin{}, err
	}
	return admin, nil
}

func (s *Service) BootstrapStatus(ctx context.Context) (BootstrapStatus, error) {
	count, err := s.admins.Count(ctx)
	if err != nil {
		return BootstrapStatus{}, err
	}

	return BootstrapStatus{
		Initialized: count > 0,
		AdminCount:  count,
	}, nil
}

func (s *Service) BootstrapAdmin(ctx context.Context, username string, password string, nickname string, avatar string, userAgent string, remoteAddr string) (LoginResult, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)

	if err := validateAdminPassword(username, password); err != nil {
		return LoginResult{}, err
	}

	passwordHash, err := crypto.HashPassword(password)
	if err != nil {
		return LoginResult{}, fmt.Errorf("hash password: %w", err)
	}

	admin, err := s.admins.CreateInitial(ctx, store.CreateAdminParams{
		Username:     username,
		Nickname:     strings.TrimSpace(nickname),
		Avatar:       strings.TrimSpace(avatar),
		PasswordHash: passwordHash,
		Role:         "owner",
		Status:       "active",
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return LoginResult{}, ErrAlreadyInitialized
		}
		return LoginResult{}, err
	}

	return s.issueAdminSession(ctx, admin, userAgent, remoteAddr)
}

func (s *Service) Login(ctx context.Context, username string, password string, totpCode string, userAgent string, remoteAddr string) (LoginResult, error) {
	attemptKey := loginAttemptKey(remoteAddr, username)
	if retry, locked := s.loginGuard.retryAfter(attemptKey); locked {
		return LoginResult{}, &LockoutError{RetryAfter: retry}
	}

	admin, err := s.admins.GetByUsername(ctx, strings.TrimSpace(username))
	if err != nil {
		return LoginResult{}, s.failedLogin(attemptKey, fmt.Errorf("invalid credentials"))
	}

	if admin.Status != "active" || !crypto.ComparePassword(admin.PasswordHash, password) {
		return LoginResult{}, s.failedLogin(attemptKey, fmt.Errorf("invalid credentials"))
	}
	if admin.TOTPEnabled {
		if strings.TrimSpace(totpCode) == "" {
			// Password is correct; the client is expected to resubmit with a code.
			// Do not count this as a brute-force failure.
			return LoginResult{}, ErrTOTPRequired
		}
		secret, err := s.credentials.Decrypt(admin.TOTPSecretEncrypted)
		if err != nil || !s.verifyTOTPForAdmin(admin.ID, secret, totpCode) {
			return LoginResult{}, s.failedLogin(attemptKey, ErrTOTPInvalid)
		}
	}

	s.loginGuard.reset(attemptKey)
	result, err := s.issueAdminSession(ctx, admin, userAgent, remoteAddr)
	if err != nil {
		return LoginResult{}, err
	}
	now := time.Now()
	_ = s.admins.UpdateLastLogin(ctx, admin.ID, now)
	result.Admin.LastLoginAt = sql.NullTime{Time: now, Valid: true}
	return result, nil
}

// failedLogin records a failed attempt for the guard key and returns a
// LockoutError once the attempt trips the lockout; otherwise it returns cause
// unchanged so the caller still sees the original reason.
func (s *Service) failedLogin(attemptKey string, cause error) error {
	if retry, locked := s.loginGuard.recordFailure(attemptKey); locked {
		return &LockoutError{RetryAfter: retry}
	}
	return cause
}

func (s *Service) GetAdminByID(ctx context.Context, id uuid.UUID) (store.Admin, error) {
	return s.admins.GetByID(ctx, id)
}

func (s *Service) issueAdminSession(ctx context.Context, admin store.Admin, userAgent string, remoteAddr string) (LoginResult, error) {
	token, err := newToken("xlyra_session_", sessionTokenBytes)
	if err != nil {
		return LoginResult{}, err
	}

	expiresAt := s.sessionExpiresAt(time.Now())
	session, err := s.sessions.Create(ctx, store.CreateAdminSessionParams{
		AdminID:          admin.ID,
		SessionTokenHash: hashToken(token),
		ExpiresAt:        expiresAt,
		IPAddress:        parseIP(remoteAddr),
		UserAgent:        userAgent,
	})
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{
		Token:     token,
		CSRFToken: s.CSRFTokenForSession(token),
		ExpiresAt: session.ExpiresAt,
		Admin:     admin,
		SessionID: session.ID,
	}, nil
}

func (s *Service) sessionLifetime() time.Duration {
	lifetime, _ := s.configuredSessionLifetime()
	return lifetime
}

func (s *Service) sessionExpiresAt(now time.Time) *time.Time {
	lifetime, neverExpires := s.configuredSessionLifetime()
	if neverExpires {
		return nil
	}
	expiresAt := now.Add(lifetime)
	return &expiresAt
}

func (s *Service) configuredSessionLifetime() (time.Duration, bool) {
	if s == nil || s.confFile == nil {
		return defaultSessionLifetime, false
	}
	general := config.ReadGeneralConfig(s.confFile)
	if general.Security.SessionLifetimeHours == 0 {
		return 0, true
	}
	lifetime := time.Duration(general.Security.SessionLifetimeHours) * time.Hour
	if lifetime <= 0 || lifetime > maxSessionLifetime {
		return defaultSessionLifetime, false
	}
	return lifetime, false
}

func (s *Service) ValidateAdminSession(ctx context.Context, token string) (store.AdminSession, error) {
	if token == "" {
		return store.AdminSession{}, fmt.Errorf("missing session token")
	}

	session, err := s.sessions.GetActiveByTokenHash(ctx, hashToken(token), time.Now())
	if err != nil {
		return store.AdminSession{}, fmt.Errorf("invalid session")
	}

	return session, nil
}

func (s *Service) ValidateAdminAccessToken(ctx context.Context, token string, userAgent string, remoteAddr string) (store.AdminAccessToken, error) {
	if strings.TrimSpace(token) == "" {
		return store.AdminAccessToken{}, fmt.Errorf("missing access token")
	}
	accessToken, err := s.accessTokens.GetActiveByTokenHash(ctx, hashToken(token))
	if err != nil {
		return store.AdminAccessToken{}, fmt.Errorf("invalid access token")
	}
	_ = s.accessTokens.TouchLastUsed(ctx, accessToken.ID, parseIP(remoteAddr), userAgent, time.Now())
	return accessToken, nil
}

func (s *Service) TouchAdminSession(ctx context.Context, sessionID uuid.UUID) {
	_ = s.sessions.TouchLastSeen(ctx, sessionID, time.Now(), 5*time.Minute)
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	return s.sessions.DeleteByTokenHash(ctx, hashToken(token))
}

func (s *Service) UpdateAdminProfile(ctx context.Context, adminID uuid.UUID, username string, nickname string, avatar string) (store.Admin, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return store.Admin{}, fmt.Errorf("username is required")
	}
	return s.admins.UpdateProfile(ctx, adminID, username, strings.TrimSpace(nickname), strings.TrimSpace(avatar))
}

func (s *Service) ChangeAdminPassword(ctx context.Context, adminID uuid.UUID, currentPassword string, newPassword string, keepSessionID uuid.UUID) (store.Admin, error) {
	admin, err := s.admins.GetByID(ctx, adminID)
	if err != nil {
		return store.Admin{}, err
	}
	if !crypto.ComparePassword(admin.PasswordHash, currentPassword) {
		return store.Admin{}, fmt.Errorf("current password is invalid")
	}
	if err := validateAdminPassword(admin.Username, newPassword); err != nil {
		return store.Admin{}, err
	}
	hash, err := crypto.HashPassword(strings.TrimSpace(newPassword))
	if err != nil {
		return store.Admin{}, fmt.Errorf("hash password: %w", err)
	}
	updated, err := s.admins.UpdatePasswordHash(ctx, admin.ID, hash)
	if err != nil {
		return store.Admin{}, err
	}
	if err := s.sessions.DeleteOthers(ctx, admin.ID, keepSessionID); err != nil {
		return store.Admin{}, err
	}
	return updated, nil
}

func (s *Service) ListAdminSessions(ctx context.Context, adminID uuid.UUID) ([]store.AdminSession, error) {
	return s.sessions.ListByAdmin(ctx, adminID, time.Now())
}

func (s *Service) DeleteAdminSession(ctx context.Context, adminID uuid.UUID, sessionID uuid.UUID) error {
	return s.sessions.DeleteByID(ctx, adminID, sessionID)
}

func (s *Service) DeleteOtherAdminSessions(ctx context.Context, adminID uuid.UUID, keepSessionID uuid.UUID) error {
	return s.sessions.DeleteOthers(ctx, adminID, keepSessionID)
}

func (s *Service) GetAdminAccessToken(ctx context.Context) (store.AdminAccessToken, error) {
	return s.accessTokens.Get(ctx)
}

func (s *Service) ReplaceAdminAccessToken(ctx context.Context, adminID uuid.UUID) (AccessTokenResult, error) {
	token, err := newAdminAccessToken()
	if err != nil {
		return AccessTokenResult{}, err
	}
	item, err := s.accessTokens.Replace(ctx, store.CreateAdminAccessTokenParams{
		AdminID:     adminID,
		TokenHash:   hashToken(token),
		MaskedToken: maskToken(token),
		Enabled:     true,
	})
	if err != nil {
		return AccessTokenResult{}, err
	}
	return AccessTokenResult{Token: token, AccessToken: item}, nil
}

func (s *Service) SetAdminAccessTokenEnabled(ctx context.Context, enabled bool) (store.AdminAccessToken, error) {
	return s.accessTokens.SetEnabled(ctx, enabled)
}

func (s *Service) DeleteAdminAccessToken(ctx context.Context) error {
	return s.accessTokens.Delete(ctx)
}

func (s *Service) SetupTOTP(ctx context.Context, adminID uuid.UUID) (TOTPSetupResult, error) {
	admin, err := s.admins.GetByID(ctx, adminID)
	if err != nil {
		return TOTPSetupResult{}, err
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return TOTPSetupResult{}, err
	}
	encrypted, _, err := s.credentials.Encrypt(secret)
	if err != nil {
		return TOTPSetupResult{}, err
	}
	admin, err = s.admins.SaveTOTPSetup(ctx, admin.ID, encrypted)
	if err != nil {
		return TOTPSetupResult{}, err
	}
	return TOTPSetupResult{
		Secret:     secret,
		OtpauthURL: totpURL("xLyra", admin.Username, secret),
		Admin:      admin,
	}, nil
}

func (s *Service) EnableTOTP(ctx context.Context, adminID uuid.UUID, code string) (store.Admin, error) {
	admin, err := s.admins.GetByID(ctx, adminID)
	if err != nil {
		return store.Admin{}, err
	}
	if admin.TOTPPendingSecretEncrypted == "" {
		return store.Admin{}, fmt.Errorf("totp setup is required")
	}
	secret, err := s.credentials.Decrypt(admin.TOTPPendingSecretEncrypted)
	if err != nil || !s.verifyTOTPForAdmin(admin.ID, secret, code) {
		return store.Admin{}, ErrTOTPInvalid
	}
	return s.admins.EnableTOTP(ctx, admin.ID)
}

func (s *Service) DisableTOTP(ctx context.Context, adminID uuid.UUID, currentPassword string, code string) (store.Admin, error) {
	admin, err := s.admins.GetByID(ctx, adminID)
	if err != nil {
		return store.Admin{}, err
	}
	if !crypto.ComparePassword(admin.PasswordHash, currentPassword) {
		return store.Admin{}, fmt.Errorf("current password is invalid")
	}
	if admin.TOTPEnabled {
		secret, err := s.credentials.Decrypt(admin.TOTPSecretEncrypted)
		if err != nil || !s.verifyTOTPForAdmin(admin.ID, secret, code) {
			return store.Admin{}, ErrTOTPInvalid
		}
	}
	return s.admins.DisableTOTP(ctx, admin.ID)
}

func (s *Service) RecordAudit(ctx context.Context, input AuditInput) {
	if s == nil {
		return
	}
	adminID := any(nil)
	if input.Actor.AdminID != uuid.Nil {
		adminID = input.Actor.AdminID
	}
	sessionID := any(nil)
	if input.Actor.SessionID != uuid.Nil {
		sessionID = input.Actor.SessionID
	}
	accessTokenID := any(nil)
	if input.Actor.AccessTokenID != uuid.Nil {
		accessTokenID = input.Actor.AccessTokenID
	}
	_, _ = s.auditLogs.Create(ctx, store.CreateAdminAuditLogParams{
		ActorType:      input.Actor.Type,
		AdminID:        adminID,
		AdminSessionID: sessionID,
		AccessTokenID:  accessTokenID,
		Action:         input.Action,
		ResourceType:   input.ResourceType,
		ResourceID:     input.ResourceID,
		IPAddress:      parseIP(input.RemoteAddr),
		UserAgent:      input.UserAgent,
		RequestID:      input.RequestID,
		Success:        input.Success,
		ErrorCode:      input.ErrorCode,
		Metadata:       input.Metadata,
	})
}

func (s *Service) ListAuditLogs(ctx context.Context, filters store.AdminAuditLogFilters) ([]store.AdminAuditLog, int64, error) {
	return s.auditLogs.List(ctx, filters)
}

func (s *Service) CreateAPIKey(ctx context.Context, input CreateAPIKeyInput, adminID uuid.UUID) (CreateAPIKeyResult, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return CreateAPIKeyResult{}, fmt.Errorf("api key name is required")
	}
	quotaUnlimited := input.QuotaUnlimited || zeroQuotaLimit(input.QuotaLimit)
	if err := validateAPIKeyQuota("quota_limit", input.QuotaLimit, quotaUnlimited, 0); err != nil {
		return CreateAPIKeyResult{}, err
	}
	dailyUnlimited := optionalBool(input.QuotaDailyUnlimited, input.QuotaDailyLimit == nil) || zeroQuotaLimit(input.QuotaDailyLimit)
	if !dailyUnlimited && input.QuotaDailyLimit == nil {
		return CreateAPIKeyResult{}, fmt.Errorf("quota_daily_limit is required when daily quota is limited")
	}
	if err := validateAPIKeyQuota("quota_daily_limit", input.QuotaDailyLimit, dailyUnlimited, 0); err != nil {
		return CreateAPIKeyResult{}, err
	}
	weeklyUnlimited := optionalBool(input.QuotaWeeklyUnlimited, input.QuotaWeeklyLimit == nil) || zeroQuotaLimit(input.QuotaWeeklyLimit)
	if !weeklyUnlimited && input.QuotaWeeklyLimit == nil {
		return CreateAPIKeyResult{}, fmt.Errorf("quota_weekly_limit is required when weekly quota is limited")
	}
	if err := validateAPIKeyQuota("quota_weekly_limit", input.QuotaWeeklyLimit, weeklyUnlimited, 0); err != nil {
		return CreateAPIKeyResult{}, err
	}

	modelPolicy := normalizeModelPolicy(input.ModelPolicy)
	siteModelIDs, err := s.normalizeExistingSiteModelIDs(ctx, input.SiteModelIDs)
	if err != nil {
		return CreateAPIKeyResult{}, err
	}
	sitePolicy := normalizeSitePolicy(input.SitePolicy)
	siteIDs := input.SiteIDs
	siteGroupIDs := normalizeUUIDs(input.SiteGroupIDs)

	modelRules, err := s.validateModelRules(ctx, input.ModelRules, modelPolicy, siteModelIDs)
	if err != nil {
		return CreateAPIKeyResult{}, err
	}
	imageToolBridge, err := s.normalizeImageToolBridge(ctx, input.ImageToolBridge)
	if err != nil {
		return CreateAPIKeyResult{}, err
	}

	key, keyKind, err := s.createGatewayAPIKeyValue(ctx, input.CustomKey)
	if err != nil {
		return CreateAPIKeyResult{}, err
	}

	encrypted, masked, err := s.credentials.Encrypt(key)
	if err != nil {
		return CreateAPIKeyResult{}, err
	}

	keyPrefix := apiKeyStoragePrefix(key, keyKind)
	var apiKey store.APIKey
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		apiKeyRepo := store.NewAPIKeyRepository(tx)
		created, err := apiKeyRepo.Create(ctx, store.CreateAPIKeyParams{
			Name:                 name,
			KeyPrefix:            keyPrefix,
			KeyHash:              hashToken(key),
			EncryptedSecret:      encrypted,
			MaskedKey:            masked,
			KeyKind:              keyKind,
			Scope:                "gateway",
			Status:               "active",
			ModelPolicy:          modelPolicy,
			SitePolicy:           sitePolicy,
			ModelMappings:        modelRules,
			ImageToolBridge:      imageToolBridge,
			QuotaLimit:           nullableFloat(input.QuotaLimit),
			QuotaUnlimited:       quotaUnlimited,
			QuotaDailyLimit:      nullableFloat(input.QuotaDailyLimit),
			QuotaDailyUnlimited:  dailyUnlimited,
			QuotaWeeklyLimit:     nullableFloat(input.QuotaWeeklyLimit),
			QuotaWeeklyUnlimited: weeklyUnlimited,
			ExpiresAt:            nullableTime(input.ExpiresAt),
			CreatedByAdminID:     adminID,
		})
		if err != nil {
			return err
		}
		apiKey = created
		accessRepo := store.NewAPIKeyAccessRepository(tx)
		if err := accessRepo.SetSiteModelPermissions(ctx, apiKey.ID, siteModelIDs); err != nil {
			return err
		}
		if err := accessRepo.SetSiteGroupPermissions(ctx, apiKey.ID, siteGroupIDs); err != nil {
			return err
		}
		if len(siteIDs) > 0 {
			if err := apiKeyRepo.SetSitePermissions(ctx, apiKey.ID, siteIDs); err != nil {
				return err
			}
		}
		if input.RateLimit != nil {
			if _, err := upsertAPIKeyRateLimit(ctx, tx, apiKey.ID, *input.RateLimit); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return CreateAPIKeyResult{}, err
	}

	return CreateAPIKeyResult{
		Key:       key,
		KeyPrefix: keyPrefix,
		APIKey:    apiKey,
	}, nil
}

func (s *Service) createGatewayAPIKeyValue(ctx context.Context, customKey string) (string, string, error) {
	if customKey != "" {
		if err := validateCustomGatewayAPIKey(customKey); err != nil {
			return "", "", err
		}
		if exists, err := s.apiKeys.ExistsByHash(ctx, hashToken(customKey)); err != nil {
			return "", "", err
		} else if exists {
			return "", "", fmt.Errorf("api key already exists")
		}
		if generatedKey, ok := generatedAPIKeyAlias(customKey); ok {
			exists, err := s.apiKeys.ExistsGeneratedByHash(ctx, hashToken(generatedKey))
			if err != nil {
				return "", "", err
			}
			if exists {
				return "", "", fmt.Errorf("custom api key conflicts with an existing generated api key alias")
			}
		}
		return customKey, apiKeyCustomKind, nil
	}

	for attempts := 0; attempts < 10; attempts++ {
		key, err := newGatewayAPIKey()
		if err != nil {
			return "", "", err
		}
		if exists, err := s.apiKeys.ExistsByHash(ctx, hashToken(key)); err != nil {
			return "", "", err
		} else if exists {
			continue
		}
		compatible := apiKeyCompatiblePrefix + strings.TrimPrefix(key, apiKeyPublicPrefix)
		if exists, err := s.apiKeys.ExistsByHash(ctx, hashToken(compatible)); err != nil {
			return "", "", err
		} else if exists {
			continue
		}
		return key, apiKeyGeneratedKind, nil
	}

	return "", "", fmt.Errorf("generate unique api key: too many collisions")
}

func (s *Service) ListAPIKeys(ctx context.Context) ([]store.APIKey, error) {
	return s.apiKeys.List(ctx)
}

func (s *Service) ListAPIKeyDetails(ctx context.Context) ([]APIKeyDetails, error) {
	apiKeys, err := s.apiKeys.List(ctx)
	if err != nil {
		return nil, err
	}
	accessRepo := store.NewAPIKeyAccessRepository(s.db)
	models, err := accessRepo.ListAllSiteModelPermissionDetails(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := s.apiKeys.ListAllSitePermissions(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := accessRepo.ListAllSiteGroupPermissions(ctx)
	if err != nil {
		return nil, err
	}
	siteGroups, err := store.NewSiteGroupRepository(s.db).List(ctx)
	if err != nil {
		return nil, err
	}
	groupSites, err := store.NewSiteGroupRepository(s.db).ListAllGroupSites(ctx)
	if err != nil {
		return nil, err
	}
	rateLimits, err := store.NewGatewayRateLimitRepository(s.db).ListAPIKeyLimits(ctx)
	if err != nil {
		return nil, err
	}

	modelsByKey := make(map[uuid.UUID][]store.APIKeySiteModelPermissionDetail)
	for _, item := range models {
		modelsByKey[item.APIKeyID] = append(modelsByKey[item.APIKeyID], item)
	}
	sitesByKey := make(map[uuid.UUID][]store.APIKeySitePermission)
	for _, item := range sites {
		sitesByKey[item.APIKeyID] = append(sitesByKey[item.APIKeyID], item)
	}
	groupsByKey := make(map[uuid.UUID][]store.APIKeySiteGroupPermission)
	for _, item := range groups {
		groupsByKey[item.APIKeyID] = append(groupsByKey[item.APIKeyID], item)
	}
	enabledGroups := make(map[uuid.UUID]struct{})
	for _, item := range siteGroups {
		if item.Enabled {
			enabledGroups[item.ID] = struct{}{}
		}
	}
	sitesByGroup := make(map[uuid.UUID][]uuid.UUID)
	for _, item := range groupSites {
		sitesByGroup[item.GroupID] = append(sitesByGroup[item.GroupID], item.SiteID)
	}
	rateLimitsByKey := make(map[uuid.UUID]RateLimitInput)
	for _, item := range rateLimits {
		if item.APIKeyID.Valid {
			rateLimitsByKey[item.APIKeyID.UUID] = rateLimitInputFromStore(item)
		}
	}

	result := make([]APIKeyDetails, 0, len(apiKeys))
	for _, apiKey := range apiKeys {
		rateLimit := RateLimitInput{Status: store.RateLimitStatusDisabled}
		if item, ok := rateLimitsByKey[apiKey.ID]; ok {
			rateLimit = item
		}
		visibleModels := modelsByKey[apiKey.ID]
		if apiKey.SitePolicy == "allow_list" {
			allowedSites := make(map[uuid.UUID]struct{})
			for _, permission := range sitesByKey[apiKey.ID] {
				if permission.Enabled {
					allowedSites[permission.SiteID] = struct{}{}
				}
			}
			for _, permission := range groupsByKey[apiKey.ID] {
				if !permission.Enabled {
					continue
				}
				if _, ok := enabledGroups[permission.GroupID]; !ok {
					continue
				}
				for _, siteID := range sitesByGroup[permission.GroupID] {
					allowedSites[siteID] = struct{}{}
				}
			}
			visibleModels = make([]store.APIKeySiteModelPermissionDetail, 0, len(modelsByKey[apiKey.ID]))
			for _, model := range modelsByKey[apiKey.ID] {
				if _, ok := allowedSites[model.SiteID]; ok {
					visibleModels = append(visibleModels, model)
				}
			}
		}
		result = append(result, APIKeyDetails{
			APIKey:    apiKey,
			Models:    visibleModels,
			Sites:     sitesByKey[apiKey.ID],
			Groups:    groupsByKey[apiKey.ID],
			RateLimit: rateLimit,
		})
	}
	return result, nil
}

func (s *Service) GetAPIKey(ctx context.Context, id uuid.UUID) (store.APIKey, error) {
	return s.apiKeys.GetByID(ctx, id)
}

func (s *Service) UpdateAPIKey(ctx context.Context, id uuid.UUID, input UpdateAPIKeyInput) (store.APIKey, error) {
	current, err := s.apiKeys.GetByID(ctx, id)
	if err != nil {
		return store.APIKey{}, err
	}
	quotaUnlimited := input.QuotaUnlimited || zeroQuotaLimit(input.QuotaLimit)
	if err := validateAPIKeyQuota("quota_limit", input.QuotaLimit, quotaUnlimited, current.EffectiveTotalQuotaUsed()); err != nil {
		return store.APIKey{}, err
	}
	dailyLimit := input.QuotaDailyLimit
	if dailyLimit == nil && current.QuotaDailyLimit.Valid {
		value := current.QuotaDailyLimit.Float64
		dailyLimit = &value
	}
	dailyFallback := current.QuotaDailyUnlimited || (!current.QuotaDailyLimit.Valid && input.QuotaDailyUnlimited == nil)
	dailyUnlimited := optionalBool(input.QuotaDailyUnlimited, dailyFallback) || zeroQuotaLimit(dailyLimit)
	if !dailyUnlimited && dailyLimit == nil {
		return store.APIKey{}, fmt.Errorf("quota_daily_limit is required when daily quota is limited")
	}
	if err := validateAPIKeyQuota("quota_daily_limit", dailyLimit, dailyUnlimited, current.EffectiveDailyQuotaUsed(time.Now(), s.timeZone)); err != nil {
		return store.APIKey{}, err
	}
	weeklyLimit := input.QuotaWeeklyLimit
	if weeklyLimit == nil && current.QuotaWeeklyLimit.Valid {
		value := current.QuotaWeeklyLimit.Float64
		weeklyLimit = &value
	}
	weeklyFallback := current.QuotaWeeklyUnlimited || (!current.QuotaWeeklyLimit.Valid && input.QuotaWeeklyUnlimited == nil)
	weeklyUnlimited := optionalBool(input.QuotaWeeklyUnlimited, weeklyFallback) || zeroQuotaLimit(weeklyLimit)
	if !weeklyUnlimited && weeklyLimit == nil {
		return store.APIKey{}, fmt.Errorf("quota_weekly_limit is required when weekly quota is limited")
	}
	if err := validateAPIKeyQuota("quota_weekly_limit", weeklyLimit, weeklyUnlimited, current.EffectiveWeeklyQuotaUsed(time.Now(), s.timeZone)); err != nil {
		return store.APIKey{}, err
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = current.Name
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = current.Status
	}
	modelPolicy := normalizeModelPolicy(input.ModelPolicy)
	if input.ModelPolicy == "" {
		modelPolicy = current.ModelPolicy
	}
	sitePolicy := normalizeSitePolicy(input.SitePolicy)
	if input.SitePolicy == "" {
		sitePolicy = current.SitePolicy
	}
	var siteModelIDs []uuid.UUID
	if input.SiteModelIDs != nil {
		siteModelIDs, err = s.normalizeExistingSiteModelIDs(ctx, input.SiteModelIDs)
		if err != nil {
			return store.APIKey{}, err
		}
	}
	siteGroupIDs := input.SiteGroupIDs
	if siteGroupIDs != nil {
		siteGroupIDs = normalizeUUIDs(siteGroupIDs)
	}

	var modelRules any
	if input.ModelRules != nil {
		var allowedSiteModelIDs []uuid.UUID
		if normalizeModelPolicy(modelPolicy) == "allow_list" {
			if input.SiteModelIDs != nil {
				allowedSiteModelIDs = siteModelIDs
			} else {
				allowedSiteModelIDs, err = s.storedAllowedSiteModelIDs(ctx, id)
				if err != nil {
					return store.APIKey{}, err
				}
			}
		}
		validated, err := s.validateModelRules(ctx, input.ModelRules, modelPolicy, allowedSiteModelIDs)
		if err != nil {
			return store.APIKey{}, err
		}
		modelRules = validated
	}
	imageToolBridge, err := s.normalizeImageToolBridge(ctx, input.ImageToolBridge)
	if err != nil {
		return store.APIKey{}, err
	}

	var updated store.APIKey
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		updated, err = store.NewAPIKeyRepository(tx).Update(ctx, store.UpdateAPIKeyParams{
			ID:                   id,
			Name:                 name,
			Status:               status,
			ModelPolicy:          modelPolicy,
			SitePolicy:           sitePolicy,
			ModelMappings:        modelRules,
			ImageToolBridge:      imageToolBridge,
			QuotaLimit:           nullableFloat(input.QuotaLimit),
			QuotaUnlimited:       quotaUnlimited,
			QuotaDailyLimit:      nullableFloat(dailyLimit),
			QuotaDailyUnlimited:  dailyUnlimited,
			QuotaWeeklyLimit:     nullableFloat(weeklyLimit),
			QuotaWeeklyUnlimited: weeklyUnlimited,
			ExpiresAt:            nullableTime(input.ExpiresAt),
		})
		if err != nil {
			return err
		}
		accessRepo := store.NewAPIKeyAccessRepository(tx)
		if input.SiteModelIDs != nil {
			if err := accessRepo.SetSiteModelPermissions(ctx, id, siteModelIDs); err != nil {
				return err
			}
		}
		if input.SiteIDs != nil {
			if err := store.NewAPIKeyRepository(tx).SetSitePermissions(ctx, id, input.SiteIDs); err != nil {
				return err
			}
		}
		if input.SiteGroupIDs != nil {
			if err := accessRepo.SetSiteGroupPermissions(ctx, id, siteGroupIDs); err != nil {
				return err
			}
		}
		if input.RateLimit != nil {
			if _, err := upsertAPIKeyRateLimit(ctx, tx, id, *input.RateLimit); err != nil {
				return err
			}
		}
		return nil
	})
	return updated, err
}

func (s *Service) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	return s.apiKeys.Delete(ctx, id)
}

func (s *Service) IncreaseAPIKeyQuota(ctx context.Context, id uuid.UUID, amount float64) (IncreaseAPIKeyQuotaResult, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return IncreaseAPIKeyQuotaResult{}, fmt.Errorf("amount must be a finite number greater than 0")
	}
	apiKey, err := s.apiKeys.IncreaseQuotaLimit(ctx, id, amount)
	if err != nil {
		return IncreaseAPIKeyQuotaResult{}, err
	}
	return IncreaseAPIKeyQuotaResult{APIKey: apiKey, Amount: amount, LimitBefore: apiKey.QuotaLimit.Float64 - amount}, nil
}

func (s *Service) ResetAPIKeyQuota(ctx context.Context, id uuid.UUID, scopes []string) (ResetAPIKeyQuotaResult, error) {
	resetTotal := false
	resetDaily := false
	resetWeekly := false
	normalized := make([]string, 0, 3)
	for _, scope := range scopes {
		switch strings.TrimSpace(scope) {
		case "total":
			if !resetTotal {
				resetTotal = true
				normalized = append(normalized, "total")
			}
		case "daily":
			if !resetDaily {
				resetDaily = true
				normalized = append(normalized, "daily")
			}
		case "weekly":
			if !resetWeekly {
				resetWeekly = true
				normalized = append(normalized, "weekly")
			}
		default:
			return ResetAPIKeyQuotaResult{}, fmt.Errorf("scopes may only contain total, daily, or weekly")
		}
	}
	if !resetTotal && !resetDaily && !resetWeekly {
		return ResetAPIKeyQuotaResult{}, fmt.Errorf("at least one quota reset scope is required")
	}
	reset, err := s.apiKeys.ResetQuota(ctx, id, resetTotal, resetDaily, resetWeekly, time.Now(), s.timeZone)
	if err != nil {
		return ResetAPIKeyQuotaResult{}, err
	}
	return ResetAPIKeyQuotaResult{
		APIKey:           reset.APIKey,
		Scopes:           normalized,
		TotalUsedBefore:  reset.TotalUsedBefore,
		DailyUsedBefore:  reset.DailyUsedBefore,
		WeeklyUsedBefore: reset.WeeklyUsedBefore,
	}, nil
}

func (s *Service) APIKeyPlaintext(apiKey store.APIKey) (string, error) {
	if !apiKey.EncryptedSecret.Valid || strings.TrimSpace(apiKey.EncryptedSecret.String) == "" {
		return "", fmt.Errorf("api key plaintext is unavailable")
	}
	return s.credentials.Decrypt(apiKey.EncryptedSecret.String)
}

func (s *Service) SetAPIKeySiteModels(ctx context.Context, id uuid.UUID, siteModelIDs []uuid.UUID, modelPolicy string) (store.APIKey, []store.APIKeySiteModelPermissionDetail, error) {
	normalized, err := s.normalizeExistingSiteModelIDs(ctx, siteModelIDs)
	if err != nil {
		return store.APIKey{}, nil, err
	}
	var current store.APIKey
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		apiKeyRepo := store.NewAPIKeyRepository(tx)
		if err := store.NewAPIKeyAccessRepository(tx).SetSiteModelPermissions(ctx, id, normalized); err != nil {
			return err
		}
		var err error
		current, err = apiKeyRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(modelPolicy) != "" && normalizeModelPolicy(modelPolicy) != current.ModelPolicy {
			current, err = apiKeyRepo.Update(ctx, store.UpdateAPIKeyParams{
				ID:                   id,
				Name:                 current.Name,
				Status:               current.Status,
				ModelPolicy:          normalizeModelPolicy(modelPolicy),
				SitePolicy:           current.SitePolicy,
				ModelMappings:        current.ModelMappings,
				QuotaLimit:           nullFloatAsAny(current.QuotaLimit),
				QuotaUnlimited:       current.QuotaUnlimited,
				QuotaDailyLimit:      nullFloatAsAny(current.QuotaDailyLimit),
				QuotaDailyUnlimited:  current.QuotaDailyUnlimited,
				QuotaWeeklyLimit:     nullFloatAsAny(current.QuotaWeeklyLimit),
				QuotaWeeklyUnlimited: current.QuotaWeeklyUnlimited,
				ExpiresAt:            timePtrAsAny(current.ExpiresAt),
			})
			return err
		}
		return nil
	})
	if err != nil {
		return store.APIKey{}, nil, err
	}
	models, err := store.NewAPIKeyAccessRepository(s.db).ListSiteModelPermissionDetails(ctx, id)
	if err != nil {
		return store.APIKey{}, nil, err
	}
	return current, models, nil
}

func (s *Service) APIKeySiteModels(ctx context.Context, id uuid.UUID) ([]store.APIKeySiteModelPermissionDetail, error) {
	return store.NewAPIKeyAccessRepository(s.db).ListSiteModelPermissionDetails(ctx, id)
}

func (s *Service) APIKeyEffectiveSiteModels(ctx context.Context, apiKey store.APIKey) ([]store.APIKeySiteModelPermissionDetail, error) {
	models, err := s.APIKeySiteModels(ctx, apiKey.ID)
	if err != nil || apiKey.SitePolicy != "allow_list" {
		return models, err
	}

	allowedSiteIDs, err := s.effectiveAllowedSiteIDs(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	if len(allowedSiteIDs) == 0 {
		return []store.APIKeySiteModelPermissionDetail{}, nil
	}

	allowedSites := make(map[uuid.UUID]struct{}, len(allowedSiteIDs))
	for _, siteID := range allowedSiteIDs {
		allowedSites[siteID] = struct{}{}
	}
	filtered := make([]store.APIKeySiteModelPermissionDetail, 0, len(models))
	for _, model := range models {
		if _, ok := allowedSites[model.SiteID]; ok {
			filtered = append(filtered, model)
		}
	}
	return filtered, nil
}

func (s *Service) SetAPIKeySiteGroups(ctx context.Context, id uuid.UUID, groupIDs []uuid.UUID) (store.APIKey, []store.APIKeySiteGroupPermission, error) {
	normalized := normalizeUUIDs(groupIDs)
	var current store.APIKey
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := store.NewAPIKeyAccessRepository(tx).SetSiteGroupPermissions(ctx, id, normalized); err != nil {
			return err
		}
		var err error
		current, err = store.NewAPIKeyRepository(tx).GetByID(ctx, id)
		return err
	})
	if err != nil {
		return store.APIKey{}, nil, err
	}
	groups, err := store.NewAPIKeyAccessRepository(s.db).ListSiteGroupPermissions(ctx, id)
	if err != nil {
		return store.APIKey{}, nil, err
	}
	return current, groups, nil
}

func (s *Service) APIKeySiteGroups(ctx context.Context, id uuid.UUID) ([]store.APIKeySiteGroupPermission, error) {
	return store.NewAPIKeyAccessRepository(s.db).ListSiteGroupPermissions(ctx, id)
}

func (s *Service) APIKeyRateLimit(ctx context.Context, id uuid.UUID) (RateLimitInput, error) {
	item, err := store.NewGatewayRateLimitRepository(s.db).GetByAPIKey(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return RateLimitInput{Status: store.RateLimitStatusDisabled}, nil
	}
	if err != nil {
		return RateLimitInput{}, err
	}
	return rateLimitInputFromStore(item), nil
}

func (s *Service) ResolveAPIKeyAccessSets(ctx context.Context, apiKeyID uuid.UUID) (APIKeyAccessSets, error) {
	apiKey, err := s.apiKeys.GetByID(ctx, apiKeyID)
	if err != nil {
		return APIKeyAccessSets{}, err
	}

	allowedSiteIDs, err := s.effectiveAllowedSiteIDs(ctx, apiKey)
	if err != nil {
		return APIKeyAccessSets{}, err
	}

	var allowedSiteModelIDs []uuid.UUID
	if apiKey.ModelPolicy == "allow_list" {
		allowedSiteModelIDs, err = store.NewAPIKeyAccessRepository(s.db).EnabledSiteModelIDs(ctx, apiKey.ID)
		if err != nil {
			return APIKeyAccessSets{}, err
		}
		if apiKey.SitePolicy == "allow_list" {
			allowedSiteModelIDs, err = s.filterSiteModelIDsBySites(ctx, allowedSiteModelIDs, allowedSiteIDs)
			if err != nil {
				return APIKeyAccessSets{}, err
			}
		}
	}

	return APIKeyAccessSets{
		APIKey:              apiKey,
		AllowedSiteIDs:      allowedSiteIDs,
		AllowedSiteModelIDs: allowedSiteModelIDs,
	}, nil
}

func (s *Service) ResolveAPIKeyRouteAccess(ctx context.Context, apiKeyID uuid.UUID, modelKey string) (APIKeyRouteAccess, error) {
	apiKey, err := s.apiKeys.GetByID(ctx, apiKeyID)
	if err != nil {
		return APIKeyRouteAccess{}, err
	}

	canonical, err := s.resolveCanonicalModel(ctx, modelKey)
	if err != nil {
		return APIKeyRouteAccess{}, err
	}

	allowedSiteIDs, err := s.effectiveAllowedSiteIDs(ctx, apiKey)
	if err != nil {
		return APIKeyRouteAccess{}, err
	}
	if apiKey.SitePolicy == "allow_list" && len(allowedSiteIDs) == 0 {
		return APIKeyRouteAccess{
			Allowed:        false,
			APIKey:         apiKey,
			RequestedModel: modelKey,
			ModelKey:       canonical.ModelKey,
			CanonicalModel: canonical,
		}, ErrSiteNotAllowed
	}

	allowedSiteModelIDs := []uuid.UUID(nil)
	if apiKey.ModelPolicy == "allow_list" {
		allowedSiteModelIDs, err = s.allowedSiteModelIDsForCanonical(ctx, apiKey.ID, canonical.ID, allowedSiteIDs)
		if err != nil {
			return APIKeyRouteAccess{}, err
		}
		if len(allowedSiteModelIDs) == 0 {
			return APIKeyRouteAccess{
				Allowed:        false,
				APIKey:         apiKey,
				RequestedModel: modelKey,
				ModelKey:       canonical.ModelKey,
				CanonicalModel: canonical,
				AllowedSiteIDs: allowedSiteIDs,
			}, ErrModelNotAllowed
		}
	}

	return APIKeyRouteAccess{
		Allowed:             true,
		APIKey:              apiKey,
		RequestedModel:      modelKey,
		ModelKey:            canonical.ModelKey,
		CanonicalModel:      canonical,
		AllowedSiteIDs:      allowedSiteIDs,
		AllowedSiteModelIDs: allowedSiteModelIDs,
	}, nil
}

func (s *Service) CheckModelAccess(ctx context.Context, apiKeyID uuid.UUID, modelKey string) (APIKeyModelAccess, error) {
	result, err := s.ResolveAPIKeyRouteAccess(ctx, apiKeyID, modelKey)
	if err != nil {
		return APIKeyModelAccess{Allowed: false, APIKey: result.APIKey, ModelKey: result.ModelKey}, err
	}
	return APIKeyModelAccess{Allowed: result.Allowed, APIKey: result.APIKey, ModelKey: result.ModelKey}, nil
}

func (s *Service) CheckSiteAccess(ctx context.Context, apiKeyID uuid.UUID, siteID uuid.UUID) (bool, error) {
	apiKey, err := s.apiKeys.GetByID(ctx, apiKeyID)
	if err != nil {
		return false, err
	}

	if apiKey.SitePolicy == "allow_all" {
		return true, nil
	}

	allowedSiteIDs, err := s.effectiveAllowedSiteIDs(ctx, apiKey)
	if err != nil {
		return false, err
	}
	for _, allowedSiteID := range allowedSiteIDs {
		if allowedSiteID == siteID {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) AllowedSiteIDs(ctx context.Context, apiKeyID uuid.UUID) ([]uuid.UUID, error) {
	apiKey, err := s.apiKeys.GetByID(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	if apiKey.SitePolicy == "allow_all" {
		return nil, nil
	}
	return s.effectiveAllowedSiteIDs(ctx, apiKey)
}

func (s *Service) APIKeySites(ctx context.Context, id uuid.UUID) ([]store.APIKeySitePermission, error) {
	return s.apiKeys.ListSitePermissions(ctx, id)
}

func (s *Service) SetAPIKeySites(ctx context.Context, id uuid.UUID, siteIDs []uuid.UUID, sitePolicy string) (store.APIKey, []store.APIKeySitePermission, error) {
	var current store.APIKey
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		apiKeyRepo := store.NewAPIKeyRepository(tx)
		if err := apiKeyRepo.SetSitePermissions(ctx, id, siteIDs); err != nil {
			return err
		}
		var err error
		current, err = apiKeyRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(sitePolicy) != "" && normalizeSitePolicy(sitePolicy) != current.SitePolicy {
			current, err = apiKeyRepo.Update(ctx, store.UpdateAPIKeyParams{
				ID:                   id,
				Name:                 current.Name,
				Status:               current.Status,
				ModelPolicy:          current.ModelPolicy,
				SitePolicy:           normalizeSitePolicy(sitePolicy),
				QuotaLimit:           nullFloatAsAny(current.QuotaLimit),
				QuotaUnlimited:       current.QuotaUnlimited,
				QuotaDailyLimit:      nullFloatAsAny(current.QuotaDailyLimit),
				QuotaDailyUnlimited:  current.QuotaDailyUnlimited,
				QuotaWeeklyLimit:     nullFloatAsAny(current.QuotaWeeklyLimit),
				QuotaWeeklyUnlimited: current.QuotaWeeklyUnlimited,
				ExpiresAt:            timePtrAsAny(current.ExpiresAt),
			})
			return err
		}
		return nil
	})
	if err != nil {
		return store.APIKey{}, nil, err
	}
	sites, err := s.apiKeys.ListSitePermissions(ctx, id)
	if err != nil {
		return store.APIKey{}, nil, err
	}
	return current, sites, nil
}

func (s *Service) SetAPIKeyModelMappings(ctx context.Context, id uuid.UUID, rules []store.APIKeyModelRule) (store.APIKey, error) {
	if err := validateModelRuleShapes(rules); err != nil {
		return store.APIKey{}, err
	}
	current, err := s.apiKeys.GetByID(ctx, id)
	if err != nil {
		return store.APIKey{}, err
	}
	var allowedSiteModelIDs []uuid.UUID
	if normalizeModelPolicy(current.ModelPolicy) == "allow_list" {
		allowedSiteModelIDs, err = s.storedAllowedSiteModelIDs(ctx, id)
		if err != nil {
			return store.APIKey{}, err
		}
	}
	modelRules, err := s.validateModelRules(ctx, rules, current.ModelPolicy, allowedSiteModelIDs)
	if err != nil {
		return store.APIKey{}, err
	}
	return s.apiKeys.Update(ctx, store.UpdateAPIKeyParams{
		ID:                   id,
		Name:                 current.Name,
		Status:               current.Status,
		ModelPolicy:          current.ModelPolicy,
		SitePolicy:           current.SitePolicy,
		ModelMappings:        modelRules,
		QuotaLimit:           nullFloatAsAny(current.QuotaLimit),
		QuotaUnlimited:       current.QuotaUnlimited,
		QuotaDailyLimit:      nullFloatAsAny(current.QuotaDailyLimit),
		QuotaDailyUnlimited:  current.QuotaDailyUnlimited,
		QuotaWeeklyLimit:     nullFloatAsAny(current.QuotaWeeklyLimit),
		QuotaWeeklyUnlimited: current.QuotaWeeklyUnlimited,
		ExpiresAt:            timePtrAsAny(current.ExpiresAt),
	})
}

const imageToolBridgeMaxCallsLimit = 10

func (s *Service) normalizeImageToolBridge(ctx context.Context, input *ImageToolBridgeInput) (any, error) {
	if input == nil {
		return nil, nil
	}
	if !input.Enabled {
		return map[string]any{}, nil
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		return nil, fmt.Errorf("image tool bridge model is required when the bridge is enabled")
	}
	if _, err := s.canonicalModels.GetByKey(ctx, model); err != nil {
		return nil, fmt.Errorf("image tool bridge model %q is not a valid canonical model", model)
	}
	if input.SiteID != nil && *input.SiteID != uuid.Nil {
		if _, err := store.NewSiteRepository(s.db).GetByID(ctx, *input.SiteID); err != nil {
			return nil, fmt.Errorf("image tool bridge site is not a valid site")
		}
	}
	maxCalls := store.DefaultImageBridgeMaxCalls
	if input.MaxCalls != nil {
		if *input.MaxCalls < 1 || *input.MaxCalls > imageToolBridgeMaxCallsLimit {
			return nil, fmt.Errorf("image tool bridge max calls must be between 1 and %d", imageToolBridgeMaxCallsLimit)
		}
		maxCalls = *input.MaxCalls
	}
	cfg := store.APIKeyImageBridge{
		Enabled:  true,
		Model:    model,
		MaxCalls: maxCalls,
	}
	if input.SiteID != nil && *input.SiteID != uuid.Nil {
		cfg.SiteID = input.SiteID
	}
	return cfg, nil
}

func validateModelRuleShapes(rules []store.APIKeyModelRule) error {
	seen := map[string]struct{}{}
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		target := strings.TrimSpace(rule.Target)
		if pattern == "" || target == "" {
			return fmt.Errorf("model rule pattern and target must not be empty")
		}
		identity := modelRulePatternIdentity(pattern)
		if identity == "" {
			return fmt.Errorf("model rule pattern %q is not valid", pattern)
		}
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("duplicate model rule pattern %q", pattern)
		}
		seen[identity] = struct{}{}
	}
	return nil
}

func (s *Service) validateModelRules(ctx context.Context, rules []store.APIKeyModelRule, modelPolicy string, allowedSiteModelIDs []uuid.UUID) ([]store.APIKeyModelRule, error) {
	if len(rules) == 0 {
		return []store.APIKeyModelRule{}, nil
	}
	if err := validateModelRuleShapes(rules); err != nil {
		return nil, err
	}
	var allowedTargets map[string]struct{}
	if normalizeModelPolicy(modelPolicy) == "allow_list" {
		var err error
		allowedTargets, err = s.canonicalKeysForSiteModelIDs(ctx, allowedSiteModelIDs)
		if err != nil {
			return nil, err
		}
	}
	normalized := make([]store.APIKeyModelRule, 0, len(rules))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		target := strings.TrimSpace(rule.Target)
		canonical, err := s.canonicalModels.GetByKey(ctx, target)
		if err != nil {
			return nil, fmt.Errorf("mapped model %q is not a valid canonical model", target)
		}
		if allowedTargets != nil {
			if _, ok := allowedTargets[canonical.ModelKey]; !ok {
				return nil, fmt.Errorf("mapped model %q is not in the api key model allow list", target)
			}
		}
		normalized = append(normalized, store.APIKeyModelRule{
			Pattern: pattern,
			Target:  canonical.ModelKey,
			Mode:    store.NormalizeAPIKeyModelRuleMode(rule.Mode),
		})
	}
	return normalized, nil
}

func modelRulePatternIdentity(pattern string) string {
	if pattern == store.APIKeyModelRuleCatchAll {
		return "*"
	}
	if strings.HasSuffix(pattern, "*") {
		rawPrefix := strings.TrimSuffix(pattern, "*")
		if strings.Contains(rawPrefix, "*") {
			return ""
		}
		prefix := catalog.NormalizeModelKeyPrefix(rawPrefix)
		if prefix == "" {
			return ""
		}
		return "wildcard:" + prefix
	}
	if strings.Contains(pattern, "*") {
		return ""
	}
	normalized := catalog.NormalizeModelKey(pattern)
	if normalized == "" {
		return ""
	}
	return "exact:" + normalized
}

func (s *Service) canonicalKeysForSiteModelIDs(ctx context.Context, siteModelIDs []uuid.UUID) (map[string]struct{}, error) {
	keys := map[string]struct{}{}
	if len(siteModelIDs) == 0 {
		return keys, nil
	}
	siteModels := store.NewSiteModelRepository(s.db)
	canonicalIDs := map[uuid.UUID]struct{}{}
	for _, siteModelID := range siteModelIDs {
		if siteModelID == uuid.Nil {
			continue
		}
		model, err := siteModels.GetByID(ctx, siteModelID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, fmt.Errorf("resolve allow list site model: %w", err)
		}
		if model.CanonicalID.Valid {
			canonicalIDs[model.CanonicalID.UUID] = struct{}{}
		}
	}
	for canonicalID := range canonicalIDs {
		canonical, err := s.canonicalModels.GetByID(ctx, canonicalID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, fmt.Errorf("resolve allow list canonical model: %w", err)
		}
		keys[canonical.ModelKey] = struct{}{}
	}
	return keys, nil
}

func (s *Service) storedAllowedSiteModelIDs(ctx context.Context, apiKeyID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := store.NewAPIKeyAccessRepository(s.db).EnabledSiteModelIDs(ctx, apiKeyID)
	if err != nil {
		return nil, fmt.Errorf("resolve allow list site model ids: %w", err)
	}
	return ids, nil
}

func (s *Service) ConsumeQuota(ctx context.Context, apiKeyID uuid.UUID, amount float64) (store.APIKey, error) {
	if amount <= 0 {
		return s.apiKeys.GetByID(ctx, apiKeyID)
	}
	return s.apiKeys.AddUsage(ctx, apiKeyID, amount)
}

func (s *Service) ValidateAPIKey(ctx context.Context, key string) (store.APIKey, error) {
	if key == "" {
		return store.APIKey{}, fmt.Errorf("missing api key")
	}

	apiKey, err := s.apiKeys.GetActiveByHash(ctx, hashToken(key), time.Now())
	if err == nil {
		return apiKey, nil
	}
	var quotaErr *store.APIKeyQuotaExceededError
	if errors.As(err, &quotaErr) {
		return store.APIKey{}, quotaErr
	}

	if generatedKey, ok := generatedAPIKeyAlias(key); ok {
		apiKey, err = s.apiKeys.GetActiveGeneratedByHash(ctx, hashToken(generatedKey), time.Now())
		if err == nil {
			return apiKey, nil
		}
		if errors.As(err, &quotaErr) {
			return store.APIKey{}, quotaErr
		}
	}

	return store.APIKey{}, fmt.Errorf("invalid api key")
}

func (s *Service) ValidateAPIKeyIdentity(ctx context.Context, key string) (store.APIKey, error) {
	if key == "" {
		return store.APIKey{}, fmt.Errorf("missing api key")
	}

	apiKey, err := s.apiKeys.GetActiveIdentityByHash(ctx, hashToken(key), time.Now())
	if err == nil {
		return apiKey, nil
	}

	if generatedKey, ok := generatedAPIKeyAlias(key); ok {
		apiKey, err = s.apiKeys.GetActiveGeneratedIdentityByHash(ctx, hashToken(generatedKey), time.Now())
		if err == nil {
			return apiKey, nil
		}
	}

	return store.APIKey{}, fmt.Errorf("invalid api key")
}

func (s *Service) effectiveAllowedSiteIDs(ctx context.Context, apiKey store.APIKey) ([]uuid.UUID, error) {
	if apiKey.SitePolicy == "allow_all" {
		return nil, nil
	}

	result := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]struct{}{}
	directIDs, err := s.apiKeys.AllowedSiteIDs(ctx, apiKey.ID)
	if err != nil {
		return nil, err
	}
	for _, siteID := range directIDs {
		if siteID == uuid.Nil {
			continue
		}
		if _, ok := seen[siteID]; ok {
			continue
		}
		seen[siteID] = struct{}{}
		result = append(result, siteID)
	}

	groupPermissions, err := store.NewAPIKeyAccessRepository(s.db).ListSiteGroupPermissions(ctx, apiKey.ID)
	if err != nil {
		return nil, err
	}
	groupIDs := make([]uuid.UUID, 0, len(groupPermissions))
	for _, permission := range groupPermissions {
		if permission.Enabled {
			groupIDs = append(groupIDs, permission.GroupID)
		}
	}
	groupSiteIDs, err := store.NewAPIKeyAccessRepository(s.db).EnabledSiteIDsForGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	for _, siteID := range groupSiteIDs {
		if siteID == uuid.Nil {
			continue
		}
		if _, ok := seen[siteID]; ok {
			continue
		}
		seen[siteID] = struct{}{}
		result = append(result, siteID)
	}

	return result, nil
}

func (s *Service) allowedSiteModelIDsForCanonical(ctx context.Context, apiKeyID uuid.UUID, canonicalModelID uuid.UUID, allowedSiteIDs []uuid.UUID) ([]uuid.UUID, error) {
	ids, err := store.NewAPIKeyAccessRepository(s.db).EnabledSiteModelIDsForCanonical(ctx, apiKeyID, canonicalModelID)
	if err != nil {
		return nil, err
	}
	if len(allowedSiteIDs) == 0 {
		return ids, nil
	}

	return s.filterSiteModelIDsBySites(ctx, ids, allowedSiteIDs)
}

func (s *Service) filterSiteModelIDsBySites(ctx context.Context, siteModelIDs []uuid.UUID, allowedSiteIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(siteModelIDs) == 0 || len(allowedSiteIDs) == 0 {
		return nil, nil
	}
	allowedSites := map[uuid.UUID]struct{}{}
	for _, siteID := range allowedSiteIDs {
		allowedSites[siteID] = struct{}{}
	}
	siteModels := store.NewSiteModelRepository(s.db)
	result := make([]uuid.UUID, 0, len(siteModelIDs))
	for _, id := range siteModelIDs {
		siteModel, err := siteModels.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if _, ok := allowedSites[siteModel.SiteID]; ok {
			result = append(result, id)
		}
	}
	return result, nil
}

func (s *Service) normalizeExistingSiteModelIDs(ctx context.Context, siteModelIDs []uuid.UUID) ([]uuid.UUID, error) {
	result := make([]uuid.UUID, 0, len(siteModelIDs))
	seen := map[uuid.UUID]struct{}{}
	repo := store.NewSiteModelRepository(s.db)
	for _, siteModelID := range siteModelIDs {
		if siteModelID == uuid.Nil {
			continue
		}
		if _, ok := seen[siteModelID]; ok {
			continue
		}
		if _, err := repo.GetByID(ctx, siteModelID); err != nil {
			return nil, fmt.Errorf("site_model_id %s was not found", siteModelID)
		}
		seen[siteModelID] = struct{}{}
		result = append(result, siteModelID)
	}
	return result, nil
}

func (s *Service) resolveCanonicalModel(ctx context.Context, modelKey string) (store.CanonicalModel, error) {
	normalized := catalog.CanonicalModelKeyFromUpstream(modelKey)
	if normalized == "" {
		normalized = catalog.NormalizeModelKey(modelKey)
	}
	if normalized == "" {
		return store.CanonicalModel{}, fmt.Errorf("model_key is required")
	}

	canonical, err := s.canonicalModels.GetByKey(ctx, normalized)
	if err == nil {
		return canonical, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.CanonicalModel{}, err
	}

	canonical, err = s.canonicalModels.GetByNormalizedAlias(ctx, normalized)
	if err != nil {
		return store.CanonicalModel{}, fmt.Errorf("canonical model %q was not found", modelKey)
	}
	return canonical, nil
}

func (s *Service) normalizeExistingModelKeys(ctx context.Context, modelKeys []string) ([]string, error) {
	result := make([]string, 0, len(modelKeys))
	seen := map[string]struct{}{}
	for _, item := range modelKeys {
		normalized, err := s.normalizeExistingModelKey(ctx, item)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func normalizeUUIDs(items []uuid.UUID) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(items))
	seen := map[uuid.UUID]struct{}{}
	for _, item := range items {
		if item == uuid.Nil {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func (s *Service) normalizeExistingModelKey(ctx context.Context, modelKey string) (string, error) {
	normalized := catalog.NormalizeModelKey(modelKey)
	if normalized == "" {
		return "", fmt.Errorf("model_key is required")
	}
	if _, err := s.canonicalModels.GetByKey(ctx, normalized); err != nil {
		return "", fmt.Errorf("canonical model %q was not found", normalized)
	}
	return normalized, nil
}

func normalizeModelPolicy(value string) string {
	switch strings.TrimSpace(value) {
	case "allow_list":
		return "allow_list"
	default:
		return "allow_all"
	}
}

func normalizeSitePolicy(value string) string {
	switch strings.TrimSpace(value) {
	case "allow_list":
		return "allow_list"
	default:
		return "allow_all"
	}
}

func validateAPIKeyQuota(field string, limit *float64, unlimited bool, used float64) error {
	if unlimited || limit == nil {
		return nil
	}
	if math.IsNaN(*limit) || math.IsInf(*limit, 0) || *limit < 0 {
		return fmt.Errorf("%s must be a finite number greater than or equal to 0", field)
	}
	if *limit < used {
		if field == "quota_limit" {
			return fmt.Errorf("quota_limit must be greater than or equal to quota_total_used")
		}
		return fmt.Errorf("%s must be greater than or equal to current used quota", field)
	}
	return nil
}

func optionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func zeroQuotaLimit(value *float64) bool {
	return value != nil && *value == 0
}

func upsertAPIKeyRateLimit(ctx context.Context, tx *gorm.DB, apiKeyID uuid.UUID, input RateLimitInput) (store.GatewayRateLimit, error) {
	status := normalizeRateLimitStatus(input.Status)
	if status == "" {
		return store.GatewayRateLimit{}, fmt.Errorf("rate_limit.status must be enabled or disabled")
	}
	if input.RPMLimit != nil && *input.RPMLimit <= 0 {
		return store.GatewayRateLimit{}, fmt.Errorf("rate_limit.rpm_limit must be greater than 0")
	}
	if input.TPMLimit != nil && *input.TPMLimit <= 0 {
		return store.GatewayRateLimit{}, fmt.Errorf("rate_limit.tpm_limit must be greater than 0")
	}
	return store.NewGatewayRateLimitRepository(tx).Upsert(ctx, store.UpsertGatewayRateLimitParams{
		Scope:    store.RateLimitScopeAPIKey,
		APIKeyID: apiKeyID,
		Status:   status,
		RPMLimit: int64PtrAsAny(input.RPMLimit),
		TPMLimit: int64PtrAsAny(input.TPMLimit),
	})
}

func rateLimitInputFromStore(item store.GatewayRateLimit) RateLimitInput {
	status := normalizeRateLimitStatus(item.Status)
	if status == "" {
		status = store.RateLimitStatusDisabled
	}
	return RateLimitInput{
		Status:   status,
		RPMLimit: nullInt64Ptr(item.RPMLimit),
		TPMLimit: nullInt64Ptr(item.TPMLimit),
	}
}

func normalizeRateLimitStatus(value string) string {
	switch strings.TrimSpace(value) {
	case store.RateLimitStatusEnabled:
		return store.RateLimitStatusEnabled
	case store.RateLimitStatusDisabled, "":
		return store.RateLimitStatusDisabled
	default:
		return ""
	}
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func int64PtrAsAny(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}

func nullFloatAsAny(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}

func timePtrAsAny(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}

func validateAdminPassword(username string, password string) error {
	username = strings.TrimSpace(strings.ToLower(username))
	password = strings.TrimSpace(password)
	if username == "" {
		return fmt.Errorf("username is required")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if strings.EqualFold(password, username) {
		return fmt.Errorf("password must not equal username")
	}
	hasLetter := false
	hasDigit := false
	for _, r := range password {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			hasLetter = true
		}
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("password must contain letters and numbers")
	}
	return nil
}

func parseIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	return net.ParseIP(host)
}
