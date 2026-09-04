package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"xlyra/server/internal/codexversion"
)

const (
	codexBackendDefaultBaseURL = "https://chatgpt.com/backend-api"
	codexOriginURL             = "https://chatgpt.com"
	codexRefererURL            = "https://chatgpt.com/codex"
)

// codexUserAgent mirrors the real Codex CLI user agent, with the client
// version resolved at runtime so it tracks the latest published release
// instead of a pinned build-time constant.
func codexUserAgent() string {
	return "codex_cli_rs/" + codexversion.Version() + " (Mac OS 26.0; arm64) vscode/1.99.3"
}

// CodexUserAgent returns the runtime user agent used for outbound Codex
// requests. It is exported so the gateway can reuse the same identity.
func CodexUserAgent() string { return codexUserAgent() }

type Codex struct {
	client *http.Client
}

type codexUpstreamHTTPError struct {
	statusCode  int
	contentType string
	body        string
}

func (e codexUpstreamHTTPError) Error() string {
	return fmt.Sprintf("codex upstream returned %d: %s", e.statusCode, strings.TrimSpace(e.body))
}

func NewCodex() Codex {
	return Codex{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (a Codex) SiteTypes() []string {
	return []string{"codex"}
}

func (a Codex) Capabilities() []Capability {
	return []Capability{
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityListAPIKeys,
		CapabilitySummarizeAPIKey,
		CapabilityFetchUserSummary,
		CapabilityFetchBalance,
		CapabilityFetchMetadata,
	}
}

func (a Codex) ValidateSystemCredentials(ctx context.Context, site SiteConfig, auth SystemAuth) error {
	_, err := a.fetchUsage(ctx, site, auth.AccessToken, auth.AccountID)
	if err != nil {
		return err
	}
	_, err = a.ListModelsWithAuth(ctx, site, auth)
	return err
}

func (a Codex) ValidateCredentials(ctx context.Context, site SiteConfig, apiKey string) error {
	accountID := stringFromMap(site.Meta, "oauth_account_id")
	_, err := a.fetchUsage(ctx, site, apiKey, accountID)
	if err != nil {
		return err
	}
	_, err = a.ListModels(ctx, site, apiKey)
	return err
}

func (a Codex) ListModelsWithAuth(ctx context.Context, site SiteConfig, auth SystemAuth) ([]Model, error) {
	return a.fetchModels(ctx, site, auth.AccessToken, auth.AccountID)
}

func (a Codex) ListModels(ctx context.Context, site SiteConfig, apiKey string) ([]Model, error) {
	return a.fetchModels(ctx, site, apiKey, stringFromMap(site.Meta, "oauth_account_id"))
}

func (a Codex) ListAPIKeys(ctx context.Context, site SiteConfig, auth SystemAuth) ([]APIKey, error) {
	return []APIKey{{
		ID:        1,
		Name:      defaultCodexAPIKeyName(auth.Email),
		Status:    "connected",
		Key:       auth.AccessToken,
		MaskedKey: "",
		Raw: map[string]any{
			"provider":   "codex",
			"account_id": auth.AccountID,
			"email":      auth.Email,
			"plan_type":  stringFromMap(auth.Metadata, "plan_type"),
		},
	}}, nil
}

func (a Codex) SummarizeAPIKey(ctx context.Context, site SiteConfig, apiKey string) (APIKeySummary, error) {
	accountID := stringFromMap(site.Meta, "oauth_account_id")
	usage, err := a.fetchUsage(ctx, site, apiKey, accountID)
	if err != nil {
		if codexOAuthAuthError(err) {
			return APIKeySummary{}, err
		}
		usage = codexUsageError(err)
	}
	models, err := a.fetchModels(ctx, site, apiKey, accountID)
	if err != nil {
		return APIKeySummary{}, err
	}
	return APIKeySummary{
		Usage:  usage,
		Models: models,
		Raw:    usage,
	}, nil
}

func (a Codex) FetchUserSummary(ctx context.Context, site SiteConfig, auth SystemAuth) (UserSummary, error) {
	usage, err := a.fetchUsage(ctx, site, auth.AccessToken, auth.AccountID)
	if err != nil {
		if codexOAuthAuthError(err) {
			return UserSummary{}, err
		}
		usage = codexUsageError(err)
	}
	planType := firstNonEmptyString(stringFromMap(usage, "plan_type"), stringFromMap(auth.Metadata, "plan_type"))
	models, err := a.fetchModels(ctx, site, auth.AccessToken, auth.AccountID)
	if err != nil {
		return UserSummary{}, err
	}
	modelItems := make([]map[string]any, 0, len(models))
	for _, model := range models {
		raw, _ := model.Capabilities["raw"].(map[string]any)
		if raw == nil {
			raw = map[string]any{"id": model.UpstreamName}
		}
		modelItems = append(modelItems, raw)
	}
	// gpt-image-2 is not issued by the codex /codex/models endpoint for this
	// account, but the gateway still routes explicit image requests to codex. It
	// must stay in the site model catalog for that routing to keep working, so it
	// is re-appended after every successful model sync (otherwise MarkUnavailable-
	// Except would mark it unavailable). It is a routing capability, not a model
	// the account was issued, and is labelled as such.
	if len(models) > 0 && !hasUpstreamModel(models, codexImageSlug) {
		modelItems = append(modelItems, codexImageRouteModel())
	}
	user := map[string]any{
		"provider":   "codex",
		"email":      auth.Email,
		"account_id": auth.AccountID,
		"plan_type":  planType,
		"quota":      usage,
	}
	if value := auth.Metadata["chatgpt_subscription_active_start"]; value != nil {
		user["chatgpt_subscription_active_start"] = value
	}
	if value := auth.Metadata["chatgpt_subscription_active_until"]; value != nil {
		user["chatgpt_subscription_active_until"] = value
	}
	return UserSummary{
		User: user,
		APIKeys: map[string]any{
			"count": 1,
			"mode":  "oauth_bearer",
		},
		UserModels: map[string]any{
			"data": modelItems,
		},
	}, nil
}

func (a Codex) FetchBalance(ctx context.Context, site SiteConfig, auth SystemAuth) (BalanceSnapshot, error) {
	usage, err := a.fetchUsage(ctx, site, auth.AccessToken, auth.AccountID)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	return BalanceSnapshot{Raw: usage}, nil
}

func (a Codex) FetchMetadata(ctx context.Context, site SiteConfig, auth SystemAuth) (MetadataSnapshot, error) {
	usage, err := a.fetchUsage(ctx, site, auth.AccessToken, auth.AccountID)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	models, err := a.fetchModels(ctx, site, auth.AccessToken, auth.AccountID)
	if err != nil {
		return MetadataSnapshot{}, err
	}
	return MetadataSnapshot{Raw: map[string]any{
		"quota":  usage,
		"models": models,
	}}, nil
}

func (a Codex) fetchModels(ctx context.Context, site SiteConfig, accessToken string, accountID string) ([]Model, error) {
	endpoints := codexModelEndpoints(site.BaseURL)
	var payload map[string]any
	var err error
	var lastErr error
	for _, endpoint := range endpoints {
		payload, err = a.getJSON(ctx, site, endpoint, accessToken, accountID)
		if err != nil {
			lastErr = err
			continue
		}
		if len(codexModelsFromItems(extractCodexModelItems(payload))) > 0 {
			lastErr = nil
			break
		}
	}
	models := codexModelsFromItems(extractCodexModelItems(payload))
	if len(models) == 0 {
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("codex upstream returned no models")
	}
	return models, nil
}

func codexModelsFromItems(items []map[string]any) []Model {
	sorted := make([]map[string]any, 0, len(items))
	current := codexversion.Version()
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(item["visibility"])), "hide") {
			continue
		}
		minimal := strings.TrimSpace(stringFromAny(item["minimal_client_version"]))
		if minimal != "" && versionGreaterThan(minimal, current) {
			continue
		}
		sorted = append(sorted, item)
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		return intFromAny(sorted[i]["priority"]) < intFromAny(sorted[j]["priority"])
	})

	models := make([]Model, 0, len(sorted))
	for _, item := range sorted {
		id := firstNonEmptyString(
			strings.TrimSpace(stringFromAny(item["slug"])),
			strings.TrimSpace(stringFromAny(item["id"])),
		)
		if id == "" {
			continue
		}
		displayName := firstNonEmptyString(stringFromAny(item["display_name"]), stringFromAny(item["name"]), id)
		capabilities := map[string]any{
			"source":    "codex",
			"available": valueOrDefault(item["available"], true),
			"raw":       item,
		}
		if endpoints := stringSliceFromAny(item["supported_endpoint_types"]); len(endpoints) > 0 {
			capabilities["supported_endpoint_types"] = endpoints
		} else {
			capabilities["supported_endpoint_types"] = []string{"openai", "openai-response"}
		}
		models = append(models, Model{
			UpstreamName: id,
			DisplayName:  displayName,
			Capabilities: capabilities,
		})
	}
	return models
}

func (a Codex) fetchUsage(ctx context.Context, site SiteConfig, accessToken string, accountID string) (map[string]any, error) {
	endpoints := codexUsageEndpoints(site.BaseURL)
	var payload map[string]any
	var err error
	for _, endpoint := range endpoints {
		payload, err = a.getJSON(ctx, site, endpoint, accessToken, accountID)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return normalizeCodexUsage(payload), nil
}

type CodexRateLimitResetCredit struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	ResetType       string `json:"reset_type,omitempty"`
	Title           string `json:"title,omitempty"`
	Description     string `json:"description,omitempty"`
	GrantedAt       string `json:"granted_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RedeemStartedAt string `json:"redeem_started_at,omitempty"`
	RedeemedAt      string `json:"redeemed_at,omitempty"`
	ProfileUserID   string `json:"profile_user_id,omitempty"`
	ProfileImageURL string `json:"profile_image_url,omitempty"`
}

type CodexRateLimitResetCredits struct {
	Credits          []CodexRateLimitResetCredit `json:"credits"`
	AvailableCount   int                         `json:"available_count"`
	TotalEarnedCount int                         `json:"total_earned_count"`
}

func (a Codex) ListRateLimitResetCredits(ctx context.Context, site SiteConfig, auth SystemAuth) (CodexRateLimitResetCredits, error) {
	endpoints := codexRateLimitResetCreditsEndpoints(site.BaseURL)
	var (
		payload map[string]any
		lastErr error
	)
	for _, endpoint := range endpoints {
		payload, lastErr = a.getJSON(ctx, site, endpoint, auth.AccessToken, auth.AccountID)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return CodexRateLimitResetCredits{}, lastErr
	}
	return normalizeCodexRateLimitResetCreditsList(payload), nil
}

func normalizeCodexRateLimitResetCreditsList(payload map[string]any) CodexRateLimitResetCredits {
	out := CodexRateLimitResetCredits{Credits: []CodexRateLimitResetCredit{}}
	if payload == nil {
		return out
	}
	out.AvailableCount = intFromAny(firstNonNil(payload["available_count"], payload["availableCount"]))
	if out.AvailableCount < 0 {
		out.AvailableCount = 0
	}
	out.TotalEarnedCount = intFromAny(firstNonNil(payload["total_earned_count"], payload["totalEarnedCount"]))
	if out.TotalEarnedCount < 0 {
		out.TotalEarnedCount = 0
	}
	items, ok := payload["credits"].([]any)
	if !ok {
		return out
	}
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		credit := CodexRateLimitResetCredit{
			ID:              stringFromAny(firstNonNil(raw["id"], raw["credit_id"])),
			Status:          stringFromAny(raw["status"]),
			ResetType:       stringFromAny(raw["reset_type"]),
			Title:           stringFromAny(raw["title"]),
			Description:     stringFromAny(raw["description"]),
			GrantedAt:       stringFromAny(firstNonNil(raw["granted_at"], raw["grantedAt"])),
			ExpiresAt:       stringFromAny(firstNonNil(raw["expires_at"], raw["expiresAt"])),
			RedeemStartedAt: stringFromAny(firstNonNil(raw["redeem_started_at"], raw["redeemStartedAt"])),
			RedeemedAt:      stringFromAny(firstNonNil(raw["redeemed_at"], raw["redeemedAt"])),
			ProfileUserID:   stringFromAny(firstNonNil(raw["profile_user_id"], raw["profileUserId"])),
			ProfileImageURL: stringFromAny(firstNonNil(raw["profile_image_url"], raw["profileImageUrl"])),
		}
		if credit.ID == "" {
			continue
		}
		out.Credits = append(out.Credits, credit)
	}
	return out
}

func (a Codex) ConsumeRateLimitResetCredit(ctx context.Context, site SiteConfig, auth SystemAuth, idempotencyKey string, creditID string) (map[string]any, error) {
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		return nil, fmt.Errorf("idempotency_key is required")
	}
	request := map[string]any{
		"redeem_request_id": key,
	}
	if trimmed := strings.TrimSpace(creditID); trimmed != "" {
		request["credit_id"] = trimmed
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("marshal codex reset credit request: %w", err)
	}
	endpoints := codexResetCreditConsumeEndpoints(site.BaseURL)
	var (
		payload map[string]any
		lastErr error
	)
	for _, endpoint := range endpoints {
		payload, lastErr = a.postJSON(ctx, site, endpoint, auth.AccessToken, auth.AccountID, body)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	outcome := codexResetCreditOutcome(payload)
	if outcome == "" {
		return nil, fmt.Errorf("codex reset credit response missing outcome")
	}
	payload["outcome"] = outcome
	return payload, nil
}

func codexResetCreditOutcome(payload map[string]any) string {
	if outcome := strings.TrimSpace(stringFromAny(payload["outcome"])); outcome != "" {
		return outcome
	}
	switch strings.TrimSpace(stringFromAny(payload["code"])) {
	case "reset":
		return "reset"
	case "nothing_to_reset":
		return "nothingToReset"
	case "no_credit":
		return "noCredit"
	case "already_redeemed":
		return "alreadyRedeemed"
	default:
		return ""
	}
}

func (a Codex) getJSON(ctx context.Context, site SiteConfig, endpoint string, accessToken string, accountID string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create codex request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Origin", codexOriginURL)
	req.Header.Set("Referer", codexRefererURL)
	req.Header.Set("User-Agent", codexUserAgent())
	if trimmed := strings.TrimSpace(accountID); trimmed != "" {
		req.Header.Set("ChatGPT-Account-Id", trimmed)
	}
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call codex upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, codexUpstreamHTTPError{
			statusCode:  resp.StatusCode,
			contentType: resp.Header.Get("Content-Type"),
			body:        string(body),
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read codex response: %w", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err == nil {
		return payload, nil
	}
	var items []map[string]any
	if err := json.Unmarshal(body, &items); err == nil {
		return map[string]any{"data": items}, nil
	}
	return nil, fmt.Errorf("decode codex response failed")
}

func (a Codex) postJSON(ctx context.Context, site SiteConfig, endpoint string, accessToken string, accountID string, body []byte) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create codex request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Origin", codexOriginURL)
	req.Header.Set("Referer", codexRefererURL)
	req.Header.Set("User-Agent", codexUserAgent())
	if trimmed := strings.TrimSpace(accountID); trimmed != "" {
		req.Header.Set("ChatGPT-Account-Id", trimmed)
	}
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("call codex upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, codexUpstreamHTTPError{
			statusCode:  resp.StatusCode,
			contentType: resp.Header.Get("Content-Type"),
			body:        string(body),
		}
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read codex response: %w", err)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(responseBody, &payload); err != nil {
		return nil, fmt.Errorf("decode codex response failed")
	}
	return payload, nil
}

func normalizeCodexUsage(payload map[string]any) map[string]any {
	out := map[string]any{
		"raw": payload,
	}
	planType, _ := payload["plan_type"].(string)
	if strings.TrimSpace(planType) != "" {
		out["plan_type"] = strings.TrimSpace(planType)
	}
	limits, _ := payload["rate_limit"].(map[string]any)
	if len(limits) == 0 {
		limits, _ = payload["rate_limits"].(map[string]any)
	}
	if allowed, ok := limits["allowed"].(bool); ok {
		out["allowed"] = allowed
	}
	if limitReached, ok := limits["limit_reached"].(bool); ok {
		out["limit_reached"] = limitReached
	}
	for key, value := range limits {
		window, ok := value.(map[string]any)
		if !ok {
			continue
		}
		windowPayload := codexUsageWindowPayload(key, window)
		switch windowPayload["window"] {
		case "five_hour":
			out["five_hour"] = windowPayload
		case "weekly":
			out["weekly"] = windowPayload
		default:
			out[key] = windowPayload
		}
	}
	if credits, ok := payload["credits"]; ok {
		out["credits"] = credits
	}
	if resetCredits := normalizeCodexRateLimitResetCredits(firstNonNil(payload["rate_limit_reset_credits"], payload["rateLimitResetCredits"])); resetCredits != nil {
		out["reset_credits"] = resetCredits
	}
	if email := stringFromAny(payload["email"]); email != "" {
		out["email"] = email
	}
	if accountID := firstNonEmptyString(stringFromAny(payload["account_id"]), stringFromAny(payload["chatgpt_account_id"])); accountID != "" {
		out["account_id"] = accountID
	}
	if allowed, ok := payload["allowed"].(bool); ok {
		out["allowed"] = allowed
	}
	if limitReached, ok := payload["limit_reached"].(bool); ok {
		out["limit_reached"] = limitReached
	}
	if message := codexUsageMessage(payload); message != "" {
		out["message"] = message
	}
	if out["five_hour"] == nil && out["weekly"] == nil {
		for _, window := range collectCodexQuotaWindows(payload) {
			windowPayload := codexUsageWindowPayload(firstNonEmptyString(stringFromAny(window["name"]), stringFromAny(window["window"])), window)
			switch windowPayload["window"] {
			case "five_hour":
				if out["five_hour"] == nil {
					out["five_hour"] = windowPayload
				}
			case "weekly":
				if out["weekly"] == nil {
					out["weekly"] = windowPayload
				}
			}
		}
	}
	out["available"] = codexUsageAvailable(out)
	return out
}

func normalizeCodexRateLimitResetCredits(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	available := intFromAny(firstNonNil(raw["available_count"], raw["availableCount"], raw["count"]))
	if available < 0 {
		available = 0
	}
	return map[string]any{
		"available_count": available,
	}
}

func codexUsageError(err error) map[string]any {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return map[string]any{
		"available": false,
		"success":   false,
		"message":   message,
		"error":     message,
	}
}

func codexOAuthAuthError(err error) bool {
	if err == nil {
		return false
	}
	var upstreamErr codexUpstreamHTTPError
	if errors.As(err, &upstreamErr) && codexTransientHTML403(upstreamErr.statusCode, upstreamErr.contentType, upstreamErr.body) {
		return false
	}
	message := strings.ToLower(err.Error())
	if codexTransientHTML403Message(message) {
		return false
	}
	return strings.Contains(message, "returned 401") ||
		strings.Contains(message, "returned 403") ||
		strings.Contains(message, "token_invalidated") ||
		strings.Contains(message, "invalid_grant")
}

func codexTransientHTML403(statusCode int, contentType string, body string) bool {
	if statusCode != http.StatusForbidden {
		return false
	}
	lowerBody := strings.ToLower(strings.TrimSpace(body))
	if !strings.Contains(lowerBody, "<html") {
		return false
	}
	return codexHTMLRefreshChallenge(lowerBody)
}

func codexTransientHTML403Message(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if !strings.Contains(lower, "codex upstream returned 403") || !strings.Contains(lower, "<html") {
		return false
	}
	return codexHTMLRefreshChallenge(lower)
}

func codexHTMLRefreshChallenge(text string) bool {
	return strings.Contains(text, `http-equiv="refresh"`) ||
		strings.Contains(text, `http-equiv='refresh'`) ||
		strings.Contains(text, "http-equiv=refresh") ||
		(strings.Contains(text, "<meta") && strings.Contains(text, "content=\"360\""))
}

func codexUsageWindowPayload(name string, value map[string]any) map[string]any {
	windowSeconds := intFromAny(value["limit_window_seconds"])
	usedPercent := intFromAny(value["used_percent"])
	remainingPercent := intFromAny(value["remaining_percent"])
	if raw, present := value["remaining_percent"]; (!present || raw == nil) && usedPercent >= 0 && usedPercent <= 100 {
		remainingPercent = 100 - usedPercent
	}
	resetAt := normalizeResetAtValue(firstNonNil(
		value["resets_at"],
		value["resetsAt"],
		value["resetAt"],
		value["reset_at"],
		value["nextResetAt"],
	))
	windowName := "window"
	switch {
	case windowSeconds == 18000:
		windowName = "five_hour"
	case windowSeconds == 604800:
		windowName = "weekly"
	case strings.Contains(strings.ToLower(name), "primary"):
		windowName = "five_hour"
	case strings.Contains(strings.ToLower(name), "secondary"):
		windowName = "weekly"
	}
	return map[string]any{
		"limit":                value["limit"],
		"used":                 value["used"],
		"remaining":            value["remaining"],
		"name":                 name,
		"window":               windowName,
		"used_percent":         usedPercent,
		"remaining_percent":    remainingPercent,
		"reset_at":             resetAt,
		"limit_window_seconds": windowSeconds,
	}
}

func codexUsageAvailable(payload map[string]any) bool {
	if allowed, ok := payload["allowed"].(bool); ok && !allowed {
		return false
	}
	if limitReached, ok := payload["limit_reached"].(bool); ok && limitReached {
		return false
	}
	for _, key := range []string{"five_hour", "weekly"} {
		window, _ := payload[key].(map[string]any)
		if len(window) == 0 {
			continue
		}
		if remaining := intFromAny(window["remaining_percent"]); remaining <= 0 {
			return false
		}
	}
	return true
}

func extractCodexModelItems(payload map[string]any) []map[string]any {
	if payload == nil {
		return nil
	}
	if items, ok := payload["data"].([]any); ok {
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			raw, ok := item.(map[string]any)
			if ok {
				result = append(result, raw)
			}
		}
		return result
	}
	if items, ok := payload["models"].([]any); ok {
		result := make([]map[string]any, 0, len(items))
		for _, item := range items {
			raw, ok := item.(map[string]any)
			if ok {
				result = append(result, raw)
			}
		}
		return result
	}
	return nil
}

// codexImageSlug is the image model the proxy exposes for Codex image routing.
// It is not issued by /codex/models but is required to keep explicit image
// requests routable to the Codex site.
const codexImageSlug = "gpt-image-2"

// codexImageRouteModel returns the catalog entry that keeps gpt-image-2 routable
// to Codex. It represents a routing capability rather than a model the account
// was issued, so it is labelled distinctly from the upstream models and carries
// only the image endpoint type.
func codexImageRouteModel() map[string]any {
	return map[string]any{
		"slug":                     codexImageSlug,
		"object":                   "model",
		"created":                  int64(1704067200),
		"owned_by":                 "openai",
		"type":                     "openai",
		"display_name":             "GPT Image 2",
		"version":                  codexImageSlug,
		"priority":                 1000,
		"visibility":               "list",
		"supported_endpoint_types": []string{"openai-image"},
		"available":                true,
		"source":                   "codex_image_route",
	}
}

func hasUpstreamModel(models []Model, upstreamName string) bool {
	if strings.TrimSpace(upstreamName) == "" {
		return false
	}
	for _, model := range models {
		if strings.EqualFold(strings.TrimSpace(model.UpstreamName), strings.TrimSpace(upstreamName)) {
			return true
		}
	}
	return false
}

// versionGreaterThan reports whether a > b for dotted numeric versions. It is
// used to drop models whose minimal_client_version is above the running client
// version, mirroring the Codex CLI client-side gate.
func versionGreaterThan(a string, b string) bool {
	ai, okA := parseVersion(a)
	bi, okB := parseVersion(b)
	if !okA || !okB {
		return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b)) > 0
	}
	switch {
	case ai[0] != bi[0]:
		return ai[0] > bi[0]
	case ai[1] != bi[1]:
		return ai[1] > bi[1]
	default:
		return ai[2] > bi[2]
	}
}

func parseVersion(value string) ([3]int, bool) {
	parts := strings.Split(strings.TrimSpace(value), ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for index, part := range parts {
		number, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return [3]int{}, false
		}
		out[index] = number
	}
	return out, true
}

func normalizeCodexBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return codexBackendDefaultBaseURL
	}
	if strings.HasSuffix(baseURL, "/codex") {
		return strings.TrimSuffix(baseURL, "/codex")
	}
	return baseURL
}

func codexUsageEndpoints(baseURL string) []string {
	base := normalizeCodexBaseURL(baseURL)
	origin := codexOriginFromBaseURL(base)
	return uniqueStrings(
		base+"/wham/usage",
		base+"/api/codex/usage",
		origin+"/api/codex/usage",
		origin+"/backend-api/codex/usage",
	)
}

func codexResponsesEndpoints(baseURL string) []string {
	base := normalizeCodexBaseURL(baseURL)
	origin := codexOriginFromBaseURL(base)
	return uniqueStrings(
		base+"/wham/responses",
		base+"/api/codex/responses",
		origin+"/api/codex/responses",
		origin+"/backend-api/codex/responses",
	)
}

func codexResetCreditConsumeEndpoints(baseURL string) []string {
	base := normalizeCodexBaseURL(baseURL)
	origin := codexOriginFromBaseURL(base)
	return uniqueStrings(
		base+"/wham/rate-limit-reset-credits/consume",
		base+"/api/codex/rate-limit-reset-credits/consume",
		origin+"/api/codex/rate-limit-reset-credits/consume",
		origin+"/backend-api/codex/rate-limit-reset-credits/consume",
	)
}

func codexRateLimitResetCreditsEndpoints(baseURL string) []string {
	base := normalizeCodexBaseURL(baseURL)
	origin := codexOriginFromBaseURL(base)
	return uniqueStrings(
		base+"/wham/rate-limit-reset-credits",
		base+"/api/codex/rate-limit-reset-credits",
		origin+"/api/codex/rate-limit-reset-credits",
		origin+"/backend-api/codex/rate-limit-reset-credits",
	)
}

func codexModelEndpoints(baseURL string) []string {
	base := normalizeCodexBaseURL(baseURL)
	origin := codexOriginFromBaseURL(base)
	return uniqueStrings(
		codexWithClientVersion(base+codexModelsPrimaryPath()),
		codexWithClientVersion(base+codexModelsFallbackPath()),
		codexWithClientVersion(origin+"/backend-api"+codexModelsPrimaryPath()),
		codexWithClientVersion(origin+"/backend-api"+codexModelsFallbackPath()),
	)
}

func codexWithClientVersion(endpoint string) string {
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "client_version=" + codexversion.Version()
}

func codexOriginFromBaseURL(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return codexOriginURL
	}
	return parsed.Scheme + "://" + parsed.Host
}

func codexModelsPrimaryPath() string {
	return "/codex/models"
}

func codexModelsFallbackPath() string {
	return "/v1/models"
}

func defaultCodexAPIKeyName(email string) string {
	if strings.TrimSpace(email) != "" {
		return strings.TrimSpace(email)
	}
	return "Codex OAuth"
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func stringFromMap(source map[string]any, key string) string {
	if source == nil {
		return ""
	}
	return stringFromAny(source[key])
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func valueOrDefault(value any, fallback bool) bool {
	parsed, ok := value.(bool)
	if ok {
		return parsed
	}
	return fallback
}

func uniqueStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeResetAtValue(value any) any {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return int64(v)
		}
	case int64:
		if v > 0 {
			return v
		}
	case float64:
		if v > 0 {
			return int64(v)
		}
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.Unix()
		}
		return text
	}
	return nil
}

func codexUsageMessage(payload any) string {
	switch value := payload.(type) {
	case map[string]any:
		for _, key := range []string{"message", "detail", "error", "status_message"} {
			if text := stringFromAny(value[key]); text != "" {
				return text
			}
		}
		for _, nested := range value {
			if text := codexUsageMessage(nested); text != "" {
				return text
			}
		}
	case []any:
		for _, nested := range value {
			if text := codexUsageMessage(nested); text != "" {
				return text
			}
		}
	}
	return ""
}

func collectCodexQuotaWindows(payload any) []map[string]any {
	switch value := payload.(type) {
	case map[string]any:
		result := make([]map[string]any, 0)
		if quota := codexQuotaWindow(value); len(quota) > 0 {
			result = append(result, quota)
		}
		for _, nested := range value {
			result = append(result, collectCodexQuotaWindows(nested)...)
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(value))
		for _, nested := range value {
			result = append(result, collectCodexQuotaWindows(nested)...)
		}
		return result
	default:
		return nil
	}
}

func codexQuotaWindow(value map[string]any) map[string]any {
	windowMinutes := intFromAny(
		firstNonNil(
			value["windowDurationMins"],
			value["window_duration_mins"],
			value["windowMinutes"],
			value["window_minutes"],
			value["window_duration_minutes"],
		),
	)
	windowSeconds := intFromAny(value["limit_window_seconds"])
	if windowSeconds <= 0 && windowMinutes > 0 {
		windowSeconds = windowMinutes * 60
	}
	limit := intFromAny(firstNonNil(value["limit"], value["max"], value["quota"]))
	used := intFromAny(firstNonNil(value["used"], value["usage"], value["count"], value["consumed"]))
	remaining := intFromAny(firstNonNil(value["remaining"], value["remainingCount"], value["remaining_count"], value["available"]))
	resetAt := normalizeResetAtValue(firstNonNil(value["resets_at"], value["resetsAt"], value["resetAt"], value["reset_at"], value["nextResetAt"]))
	if windowMinutes == 0 && windowSeconds == 0 && limit == 0 && used == 0 && remaining == 0 && resetAt == nil {
		return nil
	}
	if windowSeconds == 0 {
		switch {
		case limit > 0 && limit <= 400:
			windowSeconds = 18000
		case remaining > 0 && remaining <= 400:
			windowSeconds = 18000
		default:
			windowSeconds = 604800
		}
	}
	if remaining == 0 && limit > 0 && used > 0 && limit >= used {
		remaining = limit - used
	}
	if used == 0 && limit > 0 && remaining > 0 && limit >= remaining {
		used = limit - remaining
	}
	window := map[string]any{
		"limit_window_seconds": windowSeconds,
		"reset_at":             resetAt,
	}
	if limit > 0 {
		window["limit"] = limit
	}
	if used > 0 {
		window["used"] = used
	}
	if remaining > 0 {
		window["remaining"] = remaining
		window["remaining_percent"] = remaining
	}
	return window
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
