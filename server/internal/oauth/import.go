package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/credential"
	"xlyra/server/internal/store"
)

type ImportService struct {
	db          *store.Store
	credentials credential.Service
	oauthSvc    *Service
	proxyID     string
}

type Sub2APIExport struct {
	ExportedAt string           `json:"exported_at"`
	Accounts   []Sub2APIAccount `json:"accounts"`
}

// CPAExport represents the CPA flat single-account JSON format.
type CPAExport struct {
	Type         string `json:"type"`
	AccountID    string `json:"account_id"`
	Email        string `json:"email"`
	ProjectID    string `json:"project_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Expired      string `json:"expired"`
	ExpiresIn    int    `json:"expires_in"`
	Timestamp    int64  `json:"timestamp"`
	Disabled     bool   `json:"disabled"`
	Note         string `json:"note"`
	Priority     int    `json:"priority"`
}

type Sub2APIAccount struct {
	Name        string             `json:"name"`
	Platform    string             `json:"platform"`
	Type        string             `json:"type"`
	Credentials Sub2APICredentials `json:"credentials"`
	Extra       map[string]any     `json:"extra"`
}

type Sub2APICredentials struct {
	AccessToken      string            `json:"access_token"`
	RefreshToken     string            `json:"refresh_token"`
	IDToken          string            `json:"id_token"`
	Email            string            `json:"email"`
	ChatGPTAccountID string            `json:"chatgpt_account_id"`
	ChatGPTUserID    string            `json:"chatgpt_user_id"`
	ClientID         string            `json:"client_id"`
	ExpiresAt        importExpiresAt   `json:"expires_at"`
	PlanType         string            `json:"plan_type"`
	OrganizationID   string            `json:"organization_id"`
	ModelMapping     map[string]string `json:"model_mapping"`
}

type importExpiresAt struct {
	Time time.Time
}

func (v *importExpiresAt) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" || raw == `""` {
		v.Time = time.Time{}
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		parsed, err := parseImportExpiresAtString(text)
		if err != nil {
			return err
		}
		v.Time = parsed
		return nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("parse expires_at: %w", err)
	}
	v.Time = importUnixTime(seconds)
	return nil
}

func parseImportExpiresAtString(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse expires_at: %w", err)
	}
	return importUnixTime(seconds), nil
}

func importUnixTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	// Some exporters use Unix milliseconds while older Sub2API exports use seconds.
	if value > 1_000_000_000_000 {
		return time.UnixMilli(value)
	}
	return time.Unix(value, 0)
}

// ChatGPTTokenExport represents a single ChatGPT token export JSON format.
type ChatGPTTokenExport struct {
	AuthMode     string              `json:"auth_mode"`
	LastRefresh  string              `json:"last_refresh"`
	OpenAIAPIKey *string             `json:"OPENAI_API_KEY"`
	Tokens       ChatGPTTokenDetails `json:"tokens"`
}

type ChatGPTTokenDetails struct {
	AccessToken  string `json:"access_token"`
	AccountID    string `json:"account_id"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
}

type ImportAccountResult struct {
	Email        string `json:"email"`
	Provider     string `json:"provider"`
	ConnectionID string `json:"connection_id,omitempty"`
	SiteID       string `json:"site_id,omitempty"`
	SiteName     string `json:"site_name,omitempty"`
	Status       string `json:"status"`
	RefreshOK    *bool  `json:"refresh_ok,omitempty"`
	Refreshable  *bool  `json:"refreshable,omitempty"`
	TokenMode    string `json:"token_mode,omitempty"`
	Warning      string `json:"warning,omitempty"`
	Error        string `json:"error,omitempty"`
}

type ImportResult struct {
	Items []ImportAccountResult `json:"items"`
	Meta  ImportMeta            `json:"meta"`
}

type ImportMeta struct {
	Total     int `json:"total"`
	Accepted  int `json:"accepted"`
	Queued    int `json:"queued"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type importOAuthAccountInput struct {
	Email        string
	Provider     string
	AccountID    string
	AccessToken  string
	RefreshToken string
	IDToken      string
	Status       string
	SiteEnabled  bool
	ExpiresAt    time.Time
	RawProfile   map[string]any
	Metadata     map[string]any
}

func NewImportService(db *store.Store, masterKey string, oauthSvc *Service) *ImportService {
	return &ImportService{
		db:          db,
		credentials: credential.NewService(masterKey),
		oauthSvc:    oauthSvc,
	}
}

func (s *ImportService) WithProxyID(proxyID string) *ImportService {
	s.proxyID = strings.TrimSpace(proxyID)
	return s
}

func (s *ImportService) ImportAccounts(ctx context.Context, export Sub2APIExport, doRefresh bool) ImportResult {
	result := ImportResult{
		Items: make([]ImportAccountResult, 0, len(export.Accounts)),
		Meta: ImportMeta{
			Total: len(export.Accounts),
		},
	}

	for _, account := range export.Accounts {
		item := s.importOne(ctx, account, doRefresh)
		result.Items = append(result.Items, item)
		if item.Status == "queued" {
			result.Meta.Accepted++
			result.Meta.Queued++
			result.Meta.Succeeded++
		} else {
			result.Meta.Failed++
		}
	}

	return result
}

func (s *ImportService) importOne(ctx context.Context, account Sub2APIAccount, doRefresh bool) ImportAccountResult {
	cred := account.Credentials
	email := strings.TrimSpace(cred.Email)
	if email == "" {
		email = strings.TrimSpace(account.Name)
	}

	provider := platformToProvider(account.Platform)
	if provider == "" {
		return ImportAccountResult{
			Email:  email,
			Status: "failed",
			Error:  fmt.Sprintf("unsupported platform %q", account.Platform),
		}
	}

	if strings.TrimSpace(cred.AccessToken) == "" {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    "access_token is required",
		}
	}

	rawProfile, claimsMeta := decodeIDToken(cred.IDToken)
	if email == "" {
		email = stringFromImportMap(rawProfile, "email")
	}

	accountID := firstNonEmptyString(strings.TrimSpace(cred.ChatGPTAccountID), stringFromImportMap(claimsMeta, "chatgpt_account_id"))
	if strings.TrimSpace(cred.IDToken) == "" {
		if missing := missingSub2APIFallbackMetadata(account, email, accountID); len(missing) > 0 {
			return ImportAccountResult{
				Email:    email,
				Provider: provider,
				Status:   "failed",
				Error:    "id_token is missing and required metadata is incomplete: missing " + strings.Join(missing, ", "),
			}
		}
	}
	if accountID == "" {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    "chatgpt_account_id is required",
		}
	}

	connectionMeta := map[string]any{
		"plan_type":       strings.TrimSpace(cred.PlanType),
		"chatgpt_user_id": strings.TrimSpace(cred.ChatGPTUserID),
	}
	mergeIDTokenMetadata(connectionMeta, claimsMeta)

	var expiresAt time.Time
	if !cred.ExpiresAt.Time.IsZero() {
		expiresAt = cred.ExpiresAt.Time
	}

	return s.importOAuthAccount(ctx, importOAuthAccountInput{
		Email:        email,
		Provider:     provider,
		AccountID:    accountID,
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		IDToken:      cred.IDToken,
		Status:       "pending_sync",
		SiteEnabled:  false,
		ExpiresAt:    expiresAt,
		RawProfile:   rawProfile,
		Metadata:     connectionMeta,
	}, doRefresh)
}

func (s *ImportService) importOAuthAccount(ctx context.Context, input importOAuthAccountInput, doRefresh bool) ImportAccountResult {
	email := strings.TrimSpace(input.Email)
	provider := strings.TrimSpace(input.Provider)
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "connected"
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}

	accessEncrypted, accessMasked, err := s.credentials.Encrypt(input.AccessToken)
	if err != nil {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    fmt.Sprintf("encrypt access_token: %v", err),
		}
	}

	var refreshEncrypted, refreshMasked string
	refreshable := importTokenRefreshable(input.RefreshToken)
	if refreshable {
		refreshEncrypted, refreshMasked, err = s.credentials.Encrypt(input.RefreshToken)
		if err != nil {
			return ImportAccountResult{
				Email:    email,
				Provider: provider,
				Status:   "failed",
				Error:    fmt.Sprintf("encrypt refresh_token: %v", err),
			}
		}
	}

	var idEncrypted, idMasked string
	if strings.TrimSpace(input.IDToken) != "" {
		idEncrypted, idMasked, err = s.credentials.Encrypt(input.IDToken)
		if err != nil {
			return ImportAccountResult{
				Email:    email,
				Provider: provider,
				Status:   "failed",
				Error:    fmt.Sprintf("encrypt id_token: %v", err),
			}
		}
	}

	rawProfileJSON := store.JSON([]byte("{}"))
	if input.RawProfile != nil {
		if encoded, err := json.Marshal(input.RawProfile); err == nil {
			rawProfileJSON = store.JSON(encoded)
		}
	}
	applyImportTokenMetadata(metadata, input.RefreshToken)
	connectionMetaJSON, err := json.Marshal(metadata)
	if err != nil {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    fmt.Sprintf("marshal metadata: %v", err),
		}
	}

	connectionRepo := store.NewOAuthConnectionRepository(s.db.DB())
	connection, err := connectionRepo.UpsertByProviderEmail(ctx, store.UpsertOAuthConnectionParams{
		Provider:              provider,
		SiteID:                uuid.Nil,
		Status:                status,
		AccountID:             strings.TrimSpace(input.AccountID),
		Email:                 email,
		EncryptedAccessToken:  accessEncrypted,
		MaskedAccessToken:     accessMasked,
		EncryptedRefreshToken: refreshEncrypted,
		MaskedRefreshToken:    refreshMasked,
		EncryptedIDToken:      idEncrypted,
		MaskedIDToken:         idMasked,
		TokenType:             "Bearer",
		Scopes:                "openid email profile offline_access",
		ExpiresAt:             input.ExpiresAt,
		LastRefreshAt:         time.Now(),
		RawProfile:            rawProfileJSON,
		Metadata:              store.JSON(connectionMetaJSON),
	})
	if err != nil {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    fmt.Sprintf("upsert connection: %v", err),
		}
	}
	connection.CreatedAt = time.Now()

	siteService := store.NewSiteRepository(s.db.DB())
	siteID, siteName, err := s.resolveImportedOAuthSite(ctx, siteService, connection, provider, email, input.SiteEnabled)
	if err != nil {
		return ImportAccountResult{
			Email:        email,
			Provider:     provider,
			ConnectionID: connection.ID.String(),
			Status:       "failed",
			Error:        err.Error(),
		}
	}
	if status == "pending_sync" {
		_, _ = siteService.SetEnabled(ctx, siteID, false)
	}

	_, err = connectionRepo.Save(ctx, store.OAuthConnection{
		ID:                    connection.ID,
		Provider:              connection.Provider,
		SiteID:                &siteID,
		Status:                connection.Status,
		AccountID:             connection.AccountID,
		Email:                 connection.Email,
		EncryptedAccessToken:  connection.EncryptedAccessToken,
		MaskedAccessToken:     connection.MaskedAccessToken,
		EncryptedRefreshToken: connection.EncryptedRefreshToken,
		MaskedRefreshToken:    connection.MaskedRefreshToken,
		EncryptedIDToken:      connection.EncryptedIDToken,
		MaskedIDToken:         connection.MaskedIDToken,
		TokenType:             connection.TokenType,
		Scopes:                connection.Scopes,
		ExpiresAt:             connection.ExpiresAt,
		LastRefreshAt:         connection.LastRefreshAt,
		RawProfile:            connection.RawProfile,
		Metadata:              connection.Metadata,
		CreatedAt:             connection.CreatedAt,
	})
	if err != nil {
		return ImportAccountResult{
			Email:        email,
			Provider:     provider,
			ConnectionID: connection.ID.String(),
			SiteID:       siteID.String(),
			SiteName:     siteName,
			Status:       "failed",
			Error:        fmt.Sprintf("bind site: %v", err),
		}
	}

	s.updateImportedSiteMeta(ctx, siteService, siteID, provider, connection.ID, strings.TrimSpace(input.AccountID), email, metadata)

	result := ImportAccountResult{
		Email:        email,
		Provider:     provider,
		ConnectionID: connection.ID.String(),
		SiteID:       siteID.String(),
		SiteName:     siteName,
		Status:       importResultStatus(status),
		Refreshable:  boolPtr(refreshable),
		TokenMode:    importTokenMode(input.RefreshToken),
		Warning:      importRefreshWarning(input.RefreshToken),
	}
	if doRefresh && status == "connected" && refreshable {
		refreshOK := s.refreshImportedConnection(ctx, connection.ID)
		result.RefreshOK = &refreshOK
	}
	return result
}

func (s *ImportService) resolveImportedOAuthSite(ctx context.Context, siteService store.SiteRepository, connection store.OAuthConnection, provider string, email string, enabled bool) (uuid.UUID, string, error) {
	if connection.SiteID != nil && *connection.SiteID != uuid.Nil {
		existing, err := siteService.GetByID(ctx, *connection.SiteID)
		if err != nil {
			return uuid.Nil, "", fmt.Errorf("get bound site: %v", err)
		}
		return existing.ID, existing.Name, nil
	}
	if existing, err := siteService.FindOAuthSite(ctx, provider, strings.TrimSpace(connection.AccountID), strings.TrimSpace(email)); err == nil && existing.ID != uuid.Nil {
		return existing.ID, existing.Name, nil
	} else if err != nil && !store.IsRecordNotFound(err) {
		return uuid.Nil, "", err
	}

	var lastName string
	for range 5 {
		siteName, err := ImportedSiteName(provider, email)
		if err != nil {
			return uuid.Nil, "", err
		}
		lastName = siteName
		siteSlug := SiteSlugFromName(siteName)
		if _, err := siteService.FindBySlug(ctx, siteSlug); err == nil {
			continue
		} else if err != nil && !store.IsRecordNotFound(err) {
			return uuid.Nil, "", err
		}
		created, createErr := siteService.Create(ctx, store.CreateSiteParams{
			Name:            siteName,
			Slug:            siteSlug,
			SiteType:        provider,
			BaseURL:         oauthImportedDefaultBaseURL(provider),
			Status:          "active",
			Enabled:         enabled,
			RoutingPriority: 1.0,
			Meta:            importSiteProxyMeta(s.proxyID),
		})
		if createErr != nil {
			return uuid.Nil, "", fmt.Errorf("create site: %v", createErr)
		}
		return created.ID, created.Name, nil
	}
	return uuid.Nil, "", fmt.Errorf("generate unique site slug for %q: too many collisions", lastName)
}

func (s *ImportService) updateImportedSiteMeta(ctx context.Context, siteService store.SiteRepository, siteID uuid.UUID, provider string, connectionID uuid.UUID, accountID string, email string, connectionMeta map[string]any) {
	existing, _ := siteService.GetByID(ctx, siteID)
	if existing.ID == uuid.Nil {
		return
	}
	meta := map[string]any{}
	if len(existing.Meta) > 0 {
		_ = json.Unmarshal(existing.Meta, &meta)
	}
	for _, key := range []string{"oauth_provider", "oauth_connection_id", "oauth_account_id", "oauth_email", "oauth_plan_type", "oauth_subscription_tier", "oauth_project_id"} {
		delete(meta, key)
	}
	siteMeta := map[string]any{
		"oauth_provider":      provider,
		"oauth_connection_id": connectionID.String(),
		"oauth_account_id":    accountID,
		"oauth_email":         email,
	}
	if planType, ok := connectionMeta["plan_type"].(string); ok && strings.TrimSpace(planType) != "" {
		siteMeta["oauth_plan_type"] = strings.TrimSpace(planType)
	}
	if strings.TrimSpace(s.proxyID) != "" {
		siteMeta["proxy_id"] = strings.TrimSpace(s.proxyID)
	}
	for k, v := range siteMeta {
		meta[k] = v
	}
	mergedJSON, _ := json.Marshal(meta)
	existing.Meta = store.JSON(mergedJSON)
	_ = s.db.DB().WithContext(ctx).Save(&existing).Error
}

func importSiteProxyMeta(proxyID string) store.JSON {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return nil
	}
	encoded, _ := json.Marshal(map[string]any{"proxy_id": proxyID})
	return store.JSON(encoded)
}

// DetectAndImport auto-detects the JSON format and imports accordingly.
// If the JSON has an "accounts" field, it's Sub2API batch format; otherwise CPA format.
func (s *ImportService) DetectAndImport(ctx context.Context, data []byte, doRefresh bool) ImportResult {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return ImportResult{
			Items: []ImportAccountResult{{Status: "failed", Error: "invalid JSON: " + err.Error()}},
			Meta:  ImportMeta{Total: 1, Failed: 1},
		}
	}

	if _, ok := probe["accounts"]; ok {
		var export Sub2APIExport
		if err := json.Unmarshal(data, &export); err != nil {
			return ImportResult{
				Items: []ImportAccountResult{{Status: "failed", Error: "parse Sub2API format: " + err.Error()}},
				Meta:  ImportMeta{Total: 1, Failed: 1},
			}
		}
		if len(export.Accounts) == 0 {
			return ImportResult{
				Items: []ImportAccountResult{{Status: "failed", Error: "file contains no accounts"}},
				Meta:  ImportMeta{Total: 1, Failed: 1},
			}
		}
		return s.ImportAccounts(ctx, export, doRefresh)
	}

	if _, ok := probe["tokens"]; ok {
		var export ChatGPTTokenExport
		if err := json.Unmarshal(data, &export); err != nil {
			return ImportResult{
				Items: []ImportAccountResult{{Status: "failed", Error: "parse ChatGPT token format: " + err.Error()}},
				Meta:  ImportMeta{Total: 1, Failed: 1},
			}
		}
		return s.ImportChatGPTTokenAccounts(ctx, export, doRefresh)
	}

	var single CPAExport
	if err := json.Unmarshal(data, &single); err != nil {
		return ImportResult{
			Items: []ImportAccountResult{{Status: "failed", Error: "parse CPA format: " + err.Error()}},
			Meta:  ImportMeta{Total: 1, Failed: 1},
		}
	}
	return s.ImportCPAAccounts(ctx, single, doRefresh)
}

func (s *ImportService) ImportCPAAccounts(ctx context.Context, account CPAExport, doRefresh bool) ImportResult {
	item := s.importCPAAccount(ctx, account, doRefresh)
	result := ImportResult{
		Items: []ImportAccountResult{item},
		Meta:  ImportMeta{Total: 1},
	}
	if item.Status == "queued" {
		result.Meta.Accepted = 1
		result.Meta.Queued = 1
		result.Meta.Succeeded = 1
	} else {
		result.Meta.Failed = 1
	}
	return result
}

func (s *ImportService) ImportChatGPTTokenAccounts(ctx context.Context, export ChatGPTTokenExport, doRefresh bool) ImportResult {
	item := s.importChatGPTAccount(ctx, export, doRefresh)
	result := ImportResult{
		Items: []ImportAccountResult{item},
		Meta:  ImportMeta{Total: 1},
	}
	if item.Status == "queued" {
		result.Meta.Accepted = 1
		result.Meta.Queued = 1
		result.Meta.Succeeded = 1
	} else {
		result.Meta.Failed = 1
	}
	return result
}

func (s *ImportService) importChatGPTAccount(ctx context.Context, export ChatGPTTokenExport, doRefresh bool) ImportAccountResult {
	provider := "codex"

	tokens := export.Tokens
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return ImportAccountResult{
			Provider: provider,
			Status:   "failed",
			Error:    "access_token is required",
		}
	}

	accountID := strings.TrimSpace(tokens.AccountID)
	if accountID == "" {
		return ImportAccountResult{
			Provider: provider,
			Status:   "failed",
			Error:    "account_id is required",
		}
	}

	rawProfile, claimsMeta := decodeIDToken(tokens.IDToken)

	email := ""
	if rawProfile != nil {
		if e, ok := rawProfile["email"].(string); ok {
			email = strings.TrimSpace(e)
		}
	}
	if strings.TrimSpace(tokens.IDToken) == "" {
		if missing := missingFlatTokenFallbackMetadata(email, accountID, "", "", time.Time{}); len(missing) > 0 {
			return ImportAccountResult{
				Email:    email,
				Provider: provider,
				Status:   "failed",
				Error:    "id_token is missing and required metadata is incomplete: missing " + strings.Join(missing, ", "),
			}
		}
	}

	connectionMeta := map[string]any{
		"auth_mode": strings.TrimSpace(export.AuthMode),
	}
	if strings.TrimSpace(export.LastRefresh) != "" {
		connectionMeta["last_refresh"] = strings.TrimSpace(export.LastRefresh)
	}
	mergeIDTokenMetadata(connectionMeta, claimsMeta)

	var expiresAt time.Time
	if strings.TrimSpace(export.LastRefresh) != "" {
		if t, err := time.Parse(time.RFC3339, export.LastRefresh); err == nil {
			expiresAt = t
		}
	}

	return s.importOAuthAccount(ctx, importOAuthAccountInput{
		Email:        email,
		Provider:     provider,
		AccountID:    accountID,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		IDToken:      tokens.IDToken,
		Status:       "pending_sync",
		SiteEnabled:  false,
		ExpiresAt:    expiresAt,
		RawProfile:   rawProfile,
		Metadata:     connectionMeta,
	}, doRefresh)
}

func (s *ImportService) importCPAAccount(ctx context.Context, account CPAExport, doRefresh bool) ImportAccountResult {
	email := strings.TrimSpace(account.Email)
	provider := strings.TrimSpace(strings.ToLower(account.Type))

	switch provider {
	case "openai", "codex":
		provider = "codex"
	case "google", "antigravity":
		provider = "antigravity"
	default:
		return ImportAccountResult{
			Email:  email,
			Status: "failed",
			Error:  fmt.Sprintf("unsupported type %q", account.Type),
		}
	}

	if strings.TrimSpace(account.AccessToken) == "" {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    "access_token is required",
		}
	}

	accountID := cpaAccountID(provider, account)
	if accountID == "" {
		return ImportAccountResult{
			Email:    email,
			Provider: provider,
			Status:   "failed",
			Error:    "account_id is required",
		}
	}

	var expiresAt time.Time
	if account.Expired != "" {
		if t, err := time.Parse(time.RFC3339, account.Expired); err == nil {
			expiresAt = t
		}
	}

	rawProfile, claimsMeta := decodeIDToken(account.IDToken)
	if rawProfile == nil {
		rawProfile = cpaRawProfile(account)
	}
	if provider == codexProvider && strings.TrimSpace(account.IDToken) == "" {
		if missing := missingFlatTokenFallbackMetadata(email, accountID, "", "", expiresAt); len(missing) > 0 {
			return ImportAccountResult{
				Email:    email,
				Provider: provider,
				Status:   "failed",
				Error:    "id_token is missing and required metadata is incomplete: missing " + strings.Join(missing, ", "),
			}
		}
	}

	connectionMeta := map[string]any{}
	mergeIDTokenMetadata(connectionMeta, claimsMeta)
	if strings.TrimSpace(account.Note) != "" {
		connectionMeta["note"] = strings.TrimSpace(account.Note)
	}
	if strings.TrimSpace(account.ProjectID) != "" {
		connectionMeta["project_id"] = strings.TrimSpace(account.ProjectID)
	}
	if account.ExpiresIn > 0 {
		connectionMeta["expires_in"] = account.ExpiresIn
	}
	if account.Timestamp > 0 {
		connectionMeta["import_timestamp"] = account.Timestamp
	}
	if account.Priority > 0 {
		connectionMeta["priority"] = account.Priority
	}

	status := "pending_sync"
	if account.Disabled {
		status = "disabled"
	}

	return s.importOAuthAccount(ctx, importOAuthAccountInput{
		Email:        email,
		Provider:     provider,
		AccountID:    accountID,
		AccessToken:  account.AccessToken,
		RefreshToken: account.RefreshToken,
		IDToken:      account.IDToken,
		Status:       status,
		SiteEnabled:  false,
		ExpiresAt:    expiresAt,
		RawProfile:   rawProfile,
		Metadata:     connectionMeta,
	}, doRefresh)
}

func (s *ImportService) refreshImportedConnection(ctx context.Context, connectionID uuid.UUID) bool {
	if s.oauthSvc == nil || connectionID == uuid.Nil {
		return false
	}
	_, err := s.oauthSvc.RefreshCodexConnection(ctx, connectionID)
	return err == nil
}

func platformToProvider(platform string) string {
	switch strings.TrimSpace(strings.ToLower(platform)) {
	case "openai", "codex":
		return "codex"
	case "google", "antigravity":
		return "antigravity"
	default:
		return ""
	}
}

const importAccessTokenOnlyWarning = "不可自动续期，access token 过期后需要重新导入或重新登录"

func missingSub2APIFallbackMetadata(account Sub2APIAccount, email string, accountID string) []string {
	missing := make([]string, 0, 2)
	if strings.TrimSpace(email) == "" {
		missing = append(missing, "email")
	}
	if strings.TrimSpace(accountID) == "" {
		missing = append(missing, "chatgpt_account_id")
	}
	return missing
}

func missingFlatTokenFallbackMetadata(email string, accountID string, planType string, userID string, expiresAt time.Time) []string {
	missing := make([]string, 0, 5)
	if strings.TrimSpace(email) == "" {
		missing = append(missing, "email")
	}
	if strings.TrimSpace(accountID) == "" {
		missing = append(missing, "account_id")
	}
	if strings.TrimSpace(planType) == "" {
		missing = append(missing, "plan_type")
	}
	if strings.TrimSpace(userID) == "" {
		missing = append(missing, "user_id")
	}
	if expiresAt.IsZero() {
		missing = append(missing, "expires_at")
	}
	return missing
}

func stringFromImportMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	return stringFromAny(values[key])
}

func importTokenRefreshable(refreshToken string) bool {
	return strings.TrimSpace(refreshToken) != ""
}

func importTokenMode(refreshToken string) string {
	if importTokenRefreshable(refreshToken) {
		return "oauth_refresh"
	}
	return "access_token_only"
}

func importRefreshWarning(refreshToken string) string {
	if importTokenRefreshable(refreshToken) {
		return ""
	}
	return importAccessTokenOnlyWarning
}

func importResultStatus(connectionStatus string) string {
	switch strings.TrimSpace(connectionStatus) {
	case "pending_sync", "connected":
		return "queued"
	case "disabled":
		return "failed"
	default:
		return "failed"
	}
}

func cpaAccountID(provider string, account CPAExport) string {
	if accountID := strings.TrimSpace(account.AccountID); accountID != "" {
		return accountID
	}
	if provider == antigravityProvider {
		return firstNonEmptyString(account.Email, account.ProjectID)
	}
	return ""
}

func cpaRawProfile(account CPAExport) map[string]any {
	profile := map[string]any{}
	if email := strings.TrimSpace(account.Email); email != "" {
		profile["email"] = email
	}
	if projectID := strings.TrimSpace(account.ProjectID); projectID != "" {
		profile["project_id"] = projectID
	}
	if len(profile) == 0 {
		return nil
	}
	return profile
}

func applyImportTokenMetadata(meta map[string]any, refreshToken string) {
	if meta == nil {
		return
	}
	refreshable := importTokenRefreshable(refreshToken)
	meta["refreshable"] = refreshable
	meta["token_mode"] = importTokenMode(refreshToken)
	if refreshable {
		delete(meta, "refresh_warning")
		return
	}
	meta["refresh_warning"] = importAccessTokenOnlyWarning
}

func boolPtr(value bool) *bool {
	return &value
}

func mergeIDTokenMetadata(meta map[string]any, claimsMeta map[string]any) {
	if meta == nil {
		return
	}
	if planType, ok := claimsMeta["plan_type"].(string); ok && strings.TrimSpace(planType) != "" {
		meta["plan_type"] = strings.TrimSpace(planType)
	}
	if userID, ok := claimsMeta["chatgpt_user_id"].(string); ok && strings.TrimSpace(userID) != "" {
		meta["chatgpt_user_id"] = strings.TrimSpace(userID)
	}
	if subStart, ok := claimsMeta["chatgpt_subscription_active_start"]; ok {
		meta["chatgpt_subscription_active_start"] = subStart
	}
	if subUntil, ok := claimsMeta["chatgpt_subscription_active_until"]; ok {
		meta["chatgpt_subscription_active_until"] = subUntil
	}
}

func safeAccountIDPrefix(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if len(accountID) <= 8 {
		return accountID
	}
	return accountID[:8]
}

func oauthImportedDefaultBaseURL(provider string) string {
	switch strings.TrimSpace(strings.ToLower(provider)) {
	case "antigravity":
		return "https://daily-cloudcode-pa.sandbox.googleapis.com"
	default:
		return "https://chatgpt.com/backend-api"
	}
}

func decodeIDToken(idToken string) (map[string]any, map[string]any) {
	trimmed := strings.TrimSpace(idToken)
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return nil, nil
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, nil
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, nil
	}

	meta := map[string]any{}
	if authInfo, ok := raw["https://api.openai.com/auth"].(map[string]any); ok {
		if v, ok := authInfo["chatgpt_plan_type"]; ok {
			meta["plan_type"] = v
		}
		if v, ok := authInfo["chatgpt_user_id"]; ok {
			meta["chatgpt_user_id"] = v
		}
		if v, ok := authInfo["chatgpt_account_id"]; ok {
			meta["chatgpt_account_id"] = v
		}
		if v, ok := authInfo["chatgpt_subscription_active_start"]; ok {
			meta["chatgpt_subscription_active_start"] = v
		}
		if v, ok := authInfo["chatgpt_subscription_active_until"]; ok {
			meta["chatgpt_subscription_active_until"] = v
		}
	}

	return raw, meta
}
