package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

const (
	siteMetaQuotaProbeSummary   = "quota_probe_summary"
	QuotaProbeCredentialMetaKey = "quota_probe"

	quotaProbeRequestTimeout = 20 * time.Second
	quotaProbeBodyLimit      = 1 << 20
)

type QuotaProbeEntry struct {
	GrantedBalance  *float64 `json:"granted_balance,omitempty"`
	ToppedUpBalance *float64 `json:"topped_up_balance,omitempty"`
	Label           string   `json:"label"`
	Unit            string   `json:"unit,omitempty"`
	Remaining       *float64 `json:"remaining,omitempty"`
	Limit           *float64 `json:"limit,omitempty"`
	Used            *float64 `json:"used,omitempty"`
	Unlimited       bool     `json:"unlimited,omitempty"`
	ResetAt         *string  `json:"reset_at,omitempty"`
}

type QuotaProbeResult struct {
	IsAvailable *bool             `json:"is_available,omitempty"`
	Status      string            `json:"status"`
	Error       string            `json:"error,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Plan        string            `json:"plan,omitempty"`
	ExpiresAt   *string           `json:"expires_at,omitempty"`
	Entries     []QuotaProbeEntry `json:"entries,omitempty"`
	FetchedAt   time.Time         `json:"fetched_at"`
}

func (s *Service) runQuotaProbes(ctx context.Context, item store.Site) store.Site {
	probeType := QuotaProbeTypeFromConfig(GatewayConfigFromSiteMeta(item.Meta))
	if probeType == "" {
		probeType = defaultQuotaProbeTypeForSite(item)
	}
	if probeType == "" {
		return item
	}

	credentialRepo := store.NewSiteCredentialRepository(s.db.DB())
	credentials, err := credentialRepo.ListBySite(ctx, item.ID)
	if err != nil {
		return item
	}

	client := &http.Client{Timeout: quotaProbeRequestTimeout}
	summary := map[string]any{
		"probe_type": probeType,
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
	}
	okCount := 0
	probed := 0
	hasUnlimited := false
	var unlimitedUsedTotal *float64
	var minEntry *QuotaProbeEntry
	var summaryEntries []QuotaProbeEntry
	plan := ""
	var expiresAt *string

	for _, credential := range credentials {
		if !quotaProbeCredentialEligible(credential.CredentialType) {
			continue
		}
		probed++
		result := QuotaProbeResult{Status: "error", FetchedAt: time.Now().UTC()}
		secret, decryptErr := s.credentials.Decrypt(credential.EncryptedSecret)
		if decryptErr != nil {
			result.Error = "credential decrypt failed"
		} else {
			result = probeQuota(ctx, client, probeType, item.BaseURL, secret)
		}
		if probeType == QuotaProbeTypeSub2API && result.Status == "ok" {
			s.recoverSub2APISubscriptionCooldown(ctx, item.ID, credential.ID, result)
		}
		freshOK := result.Status == "ok"
		if result.Status != "ok" {
			result = preserveQuotaProbeResult(credential.Meta, result)
		}
		// 只对本次新鲜成功的探测同步冷却；失败时保留的旧数据不用于冷却判断
		if freshOK && (probeType == QuotaProbeTypeKimi || probeType == QuotaProbeTypeGLM) {
			s.syncCodingPlanQuotaCooldown(ctx, item.ID, credential.ID, result)
		}
		if result.Status == "ok" {
			okCount++
			// 取首个成功凭据的完整窗口明细作为展示数据（kimi_code 通常只有一把
			// key；多凭据时 minEntry 仍按所有凭据的最紧张窗口计算）
			if result.Plan != "" && plan == "" {
				plan = result.Plan
			}
			if result.ExpiresAt != nil && expiresAt == nil {
				expiresAt = result.ExpiresAt
			}
			if len(result.Entries) > 0 && summaryEntries == nil {
				summaryEntries = result.Entries
			}
			if entry, ok := quotaProbeSummaryEntry(probeType, result); ok {
				if minEntry == nil || *entry.Remaining < *minEntry.Remaining {
					copied := entry
					minEntry = &copied
				}
			} else if quotaProbeResultUnlimited(result) {
				hasUnlimited = true
				if used := quotaProbeResultUsed(result); used != nil {
					if unlimitedUsedTotal == nil {
						unlimitedUsedTotal = used
					} else {
						total := *unlimitedUsedTotal + *used
						unlimitedUsedTotal = &total
					}
				}
			}
		}
		meta := map[string]any{}
		if len(credential.Meta) > 0 {
			_ = json.Unmarshal(credential.Meta, &meta)
		}
		meta[QuotaProbeCredentialMetaKey] = result
		_, _ = updateCredentialMeta(ctx, credentialRepo, credential.ID, meta)
	}

	if probed == 0 {
		return item
	}
	summary["credential_count"] = probed
	summary["ok_count"] = okCount
	if okCount == 0 {
		summary["status"] = "error"
		preserveQuotaSummaryValues(siteMetaMap(item), summary)
	} else {
		summary["status"] = "ok"
	}
	if minEntry != nil {
		summary["remaining_min"] = *minEntry.Remaining
		summary["unit"] = minEntry.Unit
		if minEntry.Limit != nil {
			summary["limit"] = *minEntry.Limit
		}
		if minEntry.Used != nil {
			summary["used"] = *minEntry.Used
		}
	} else if hasUnlimited {
		summary["unlimited"] = true
		if unlimitedUsedTotal != nil {
			summary["used_total"] = *unlimitedUsedTotal
			summary["unit"] = "usd"
		}
	}
	if len(summaryEntries) > 0 {
		summary["entries"] = summaryEntries
	}
	if plan != "" {
		summary["plan"] = plan
	}
	if expiresAt != nil {
		summary["expires_at"] = *expiresAt
	}

	meta := siteMetaMap(item)
	meta[siteMetaQuotaProbeSummary] = summary
	updated, err := store.NewSiteRepository(s.db.DB()).UpdateMeta(ctx, item.ID, store.JSON(jsonBytes(meta)))
	if err != nil {
		return item
	}
	return updated
}

func (s *Service) recoverSub2APISubscriptionCooldown(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, result QuotaProbeResult) {
	if result.Status != "ok" || result.FetchedAt.IsZero() || siteID == uuid.Nil || credentialID == uuid.Nil {
		return
	}
	repo := store.NewRouteCooldownRepository(s.db.DB())
	items, err := repo.ListActiveByCredential(ctx, siteID, credentialID, []string{store.CooldownReasonUpstreamSubscriptionLimitExceeded}, time.Now())
	if err != nil {
		return
	}
	for _, item := range items {
		if item.CreatedAt.After(result.FetchedAt) {
			return
		}
	}
	for _, item := range items {
		metadata := map[string]any{}
		if json.Unmarshal(item.Metadata, &metadata) != nil {
			continue
		}
		limitWindow := anyString(metadata["limit_window"])
		entry, ok := sub2APISubscriptionQuotaEntry(result, limitWindow)
		if !ok {
			if sub2APISubscriptionQuotaRecovered(result, limitWindow) {
				_, _ = repo.ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
					SiteID:           siteID,
					SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
					Reasons:          []string{store.CooldownReasonUpstreamSubscriptionLimitExceeded},
				})
				return
			}
			continue
		}
		if entry.Remaining != nil && *entry.Remaining > 0 {
			_, _ = repo.ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
				SiteID:           siteID,
				SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
				Reasons:          []string{store.CooldownReasonUpstreamSubscriptionLimitExceeded},
			})
			return
		}
		if entry.Remaining == nil {
			continue
		}
		resetAt, ok := quotaProbeResetAt(result, limitWindow, time.Now())
		if !ok {
			continue
		}
		metadata["reset_at"] = resetAt.UTC().Format(time.RFC3339)
		encodedMetadata, err := json.Marshal(metadata)
		if err != nil {
			continue
		}
		if item.ActiveUntil.Equal(resetAt) && string(item.Metadata) == string(encodedMetadata) {
			continue
		}
		_ = repo.UpdateActiveUntil(ctx, item.ID, resetAt, store.JSON(encodedMetadata))
	}
}

func sub2APISubscriptionQuotaRecovered(result QuotaProbeResult, limitWindow string) bool {
	if result.Status != "ok" {
		return false
	}
	limitWindow = strings.ToLower(strings.TrimSpace(limitWindow))
	if limitWindow == "daily" || limitWindow == "weekly" || limitWindow == "monthly" {
		for _, entry := range result.Entries {
			if strings.EqualFold(strings.TrimSpace(entry.Label), limitWindow) {
				return entry.Remaining != nil && *entry.Remaining > 0
			}
		}
		return false
	}
	foundWindow := false
	for _, entry := range result.Entries {
		switch strings.ToLower(strings.TrimSpace(entry.Label)) {
		case "balance":
			if entry.Remaining == nil || *entry.Remaining <= 0 {
				return false
			}
		case "daily", "weekly", "monthly":
			if entry.Remaining == nil || *entry.Remaining <= 0 {
				return false
			}
			foundWindow = true
		}
	}
	return foundWindow
}

func sub2APISubscriptionQuotaEntry(result QuotaProbeResult, limitWindow string) (QuotaProbeEntry, bool) {
	limitWindow = strings.ToLower(strings.TrimSpace(limitWindow))
	if limitWindow != "daily" && limitWindow != "weekly" && limitWindow != "monthly" {
		return QuotaProbeEntry{}, false
	}
	for _, entry := range result.Entries {
		if strings.EqualFold(strings.TrimSpace(entry.Label), limitWindow) {
			return entry, true
		}
	}
	return QuotaProbeEntry{}, false
}

// syncCodingPlanQuotaCooldown 按 Kimi/GLM Coding Plan 探测结果冷却或解冻凭据：
// 5 小时或周额度耗尽（remaining 为 0）且带未来重置时间时，把凭据冷却到最晚的
// 重置时间，不在没额度的时候继续调用；所有窗口都有额度时解除探测冷却（窗口
// 提前重置或升档都能及时恢复）。只动探测自己设置的冷却，网关因鉴权/限流设置
// 的冷却不受影响。Best-effort。
func (s *Service) syncCodingPlanQuotaCooldown(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, result QuotaProbeResult) {
	if result.Status != "ok" || credentialID == uuid.Nil {
		return
	}
	repo := store.NewRouteCooldownRepository(s.db.DB())
	deadline, windows := codingPlanQuotaCooldownDeadline(result, time.Now())
	if deadline.IsZero() {
		_, _ = repo.ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
			SiteID:           siteID,
			SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
			Reasons:          []string{store.CooldownReasonCodingPlanQuotaExhausted},
		})
		return
	}
	_, _ = repo.Activate(ctx, store.ActivateRouteCooldownParams{
		SiteID:           siteID,
		SiteCredentialID: credentialID,
		Scope:            "credential",
		Source:           "quota_probe",
		Reason:           store.CooldownReasonCodingPlanQuotaExhausted,
		ActiveUntil:      deadline,
		Metadata: store.JSON(jsonBytes(map[string]any{
			"exhausted_windows": windows,
			"reset_at":          deadline.UTC().Format(time.RFC3339),
		})),
	})
}

// codingPlanQuotaCooldownDeadline 从探测结果里找出已耗尽的窗口（remaining 为 0 且
// 带未来重置时间），返回最晚的重置时间作为冷却截止——所有窗口都有额度时站点才
// 可用，所以要等到最后一个耗尽窗口重置。没有耗尽窗口时返回零值。
func codingPlanQuotaCooldownDeadline(result QuotaProbeResult, now time.Time) (time.Time, []string) {
	var deadline time.Time
	windows := []string{}
	for _, entry := range result.Entries {
		if entry.Label != "five_hour" && entry.Label != "weekly" {
			continue
		}
		if entry.Remaining == nil || *entry.Remaining > 0 || entry.ResetAt == nil {
			continue
		}
		resetAt, err := time.Parse(time.RFC3339, *entry.ResetAt)
		if err != nil || !resetAt.After(now) {
			continue
		}
		windows = append(windows, entry.Label)
		if resetAt.After(deadline) {
			deadline = resetAt
		}
	}
	return deadline, windows
}

func quotaProbeCredentialEligible(credentialType string) bool {
	return credentialType == "api_key" || strings.HasPrefix(credentialType, "api_key:")
}

// defaultQuotaProbeTypeForSite 给未显式配置 quota_probe 的站点类型提供默认探测。
// 只有指向官方站点的才默认开启：Kimi Code 官方站（api.kimi.com）、GLM Code 官方站
// （open.bigmodel.cn / api.z.ai）有 Coding Plan 额度接口，DeepSeek 官方站
// （api.deepseek.com）有余额接口；指向中转/镜像的站点不默认探测，避免对未实现
// 额度接口的第三方端点持续报错。
func defaultQuotaProbeTypeForSite(item store.Site) string {
	switch item.SiteType {
	case "kimi_code":
		if quotaProbeBaseURLOfficial(item.BaseURL, "api.kimi.com") {
			return QuotaProbeTypeKimi
		}
	case "glm_code":
		if quotaProbeBaseURLOfficial(item.BaseURL, "open.bigmodel.cn", "api.z.ai") {
			return QuotaProbeTypeGLM
		}
	case "deepseek":
		if quotaProbeBaseURLOfficial(item.BaseURL, "api.deepseek.com") {
			return QuotaProbeTypeDeepSeek
		}
	}
	return ""
}

// quotaProbeBaseURLOfficial 判断 base_url 是否指向官方站点之一。空 base_url
// 视为官方（各探测函数对空值本身也回退到官方地址）；无法解析出 host 的
// 非空 base_url 不视为官方。
func quotaProbeBaseURLOfficial(baseURL string, officialHosts ...string) bool {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return true
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	return slices.Contains(officialHosts, host)
}

func preserveQuotaProbeResult(credentialMeta store.JSON, result QuotaProbeResult) QuotaProbeResult {
	if len(credentialMeta) == 0 {
		return result
	}
	meta := map[string]json.RawMessage{}
	if err := json.Unmarshal(credentialMeta, &meta); err != nil {
		return result
	}
	raw, ok := meta[QuotaProbeCredentialMetaKey]
	if !ok {
		return result
	}
	var previous QuotaProbeResult
	if err := json.Unmarshal(raw, &previous); err != nil || len(previous.Entries) == 0 {
		return result
	}
	result.Kind = previous.Kind
	result.Plan = previous.Plan
	result.ExpiresAt = previous.ExpiresAt
	result.Entries = previous.Entries
	result.FetchedAt = previous.FetchedAt
	return result
}

// CredentialQuotaProbeResetAt returns the future reset for a specific quota
// window from the latest successful probe stored on a credential.
func CredentialQuotaProbeResetAt(credentialMeta store.JSON, limitWindow string, now time.Time) (time.Time, bool) {
	if len(credentialMeta) == 0 {
		return time.Time{}, false
	}
	meta := map[string]json.RawMessage{}
	if err := json.Unmarshal(credentialMeta, &meta); err != nil {
		return time.Time{}, false
	}
	raw, ok := meta[QuotaProbeCredentialMetaKey]
	if !ok {
		return time.Time{}, false
	}
	var result QuotaProbeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return time.Time{}, false
	}
	return quotaProbeResetAt(result, limitWindow, now)
}

func quotaProbeResetAt(result QuotaProbeResult, limitWindow string, now time.Time) (time.Time, bool) {
	if now.IsZero() {
		now = time.Now()
	}
	if !strings.EqualFold(strings.TrimSpace(result.Status), "ok") || result.FetchedAt.IsZero() || result.FetchedAt.After(now) {
		return time.Time{}, false
	}
	limitWindow = strings.ToLower(strings.TrimSpace(limitWindow))
	switch limitWindow {
	case "daily", "weekly", "monthly":
	default:
		return time.Time{}, false
	}
	for _, entry := range result.Entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Label), limitWindow) || entry.Remaining == nil || *entry.Remaining > 0 || entry.ResetAt == nil {
			continue
		}
		resetAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*entry.ResetAt))
		if err == nil && resetAt.After(now) {
			return resetAt, true
		}
	}
	return time.Time{}, false
}

func preserveQuotaSummaryValues(siteMeta map[string]any, summary map[string]any) {
	previous, ok := siteMeta[siteMetaQuotaProbeSummary].(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"remaining_min", "unit", "limit", "used", "unlimited", "used_total", "entries", "plan", "expires_at"} {
		if value, exists := previous[key]; exists {
			if _, taken := summary[key]; !taken {
				summary[key] = value
			}
		}
	}
}

func quotaProbeResultUnlimited(result QuotaProbeResult) bool {
	for _, entry := range result.Entries {
		if entry.Unlimited {
			return true
		}
	}
	return false
}

func quotaProbeResultUsed(result QuotaProbeResult) *float64 {
	for _, entry := range result.Entries {
		if entry.Unlimited && entry.Used != nil {
			return entry.Used
		}
	}
	return nil
}

func quotaProbePrimaryEntry(result QuotaProbeResult) (QuotaProbeEntry, bool) {
	for _, entry := range result.Entries {
		if entry.Unlimited {
			continue
		}
		if entry.Label == "balance" && entry.Remaining != nil {
			return entry, true
		}
	}
	var selected *QuotaProbeEntry
	for i, entry := range result.Entries {
		if entry.Unlimited || entry.Remaining == nil {
			continue
		}
		if selected == nil || *entry.Remaining < *selected.Remaining {
			copied := result.Entries[i]
			selected = &copied
		}
	}
	if selected != nil {
		return *selected, true
	}
	return QuotaProbeEntry{}, false
}

func quotaProbeSummaryEntry(probeType string, result QuotaProbeResult) (QuotaProbeEntry, bool) {
	if probeType == QuotaProbeTypeGLM {
		entries := make([]QuotaProbeEntry, 0, 2)
		for _, entry := range result.Entries {
			if entry.Label == "five_hour" || entry.Label == "weekly" {
				entries = append(entries, entry)
			}
		}
		result.Entries = entries
	}
	if probeType == QuotaProbeTypeSub2API {
		for _, entry := range result.Entries {
			if entry.Label == "balance" && !entry.Unlimited && entry.Remaining != nil {
				return entry, true
			}
		}
		return QuotaProbeEntry{}, false
	}
	return quotaProbePrimaryEntry(result)
}

func probeQuota(ctx context.Context, client *http.Client, probeType string, baseURL string, secret string) QuotaProbeResult {
	result := QuotaProbeResult{Status: "error", FetchedAt: time.Now().UTC()}
	var entries []QuotaProbeEntry
	var kind string
	var err error

	switch probeType {
	case QuotaProbeTypeSub2API:
		kind, entries, result.Plan, result.ExpiresAt, err = probeSub2APIQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeNewAPI:
		kind, entries, err = probeNewAPIQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeXLyra:
		kind, entries, err = probeXLyraQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeKimi:
		kind, entries, result.Plan, err = probeKimiQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeGLM:
		kind, entries, result.Plan, err = probeGLMQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeDeepSeek:
		kind, entries, result.IsAvailable, err = probeDeepSeekBalance(ctx, client, baseURL, secret)
	default:
		err = fmt.Errorf("unsupported quota probe type %q", probeType)
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if len(entries) == 0 {
		result.Error = "probe response did not contain quota data"
		return result
	}
	result.Status = "ok"
	result.Kind = kind
	result.Entries = entries
	return result
}

func probeSub2APIQuota(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, string, *string, error) {
	payload, err := quotaProbeGetJSON(ctx, client, quotaProbeURL(baseURL, "/v1/usage"), secret)
	if err != nil {
		return "", nil, "", nil, err
	}
	if valid, ok := payload["isValid"].(bool); ok && !valid {
		return "", nil, "", nil, fmt.Errorf("usage endpoint reported invalid API key")
	}

	entries := make([]QuotaProbeEntry, 0, 4)
	plan := strings.TrimSpace(anyString(payload["planName"]))
	var expiresAt *string
	planWindows := false
	if quota, ok := payload["quota"].(map[string]any); ok {
		entry := QuotaProbeEntry{Label: "balance", Unit: quotaProbeUnit(anyString(quota["unit"]), "usd")}
		entry.Limit = quotaProbeFloat(quota["limit"])
		entry.Used = quotaProbeFloat(quota["used"])
		entry.Remaining = quotaProbeFloat(quota["remaining"])
		if entry.Remaining != nil || entry.Limit != nil {
			entries = append(entries, entry)
		}
	} else if remaining := quotaProbeFloat(payload["remaining"]); remaining != nil {
		entries = append(entries, QuotaProbeEntry{Label: "balance", Unit: "usd", Remaining: remaining})
	}

	if subscription, ok := payload["subscription"].(map[string]any); ok {
		if value := strings.TrimSpace(anyString(subscription["expires_at"])); value != "" {
			expiresAt = &value
		}
		for _, window := range []struct {
			label string
			used  string
			limit string
		}{
			{label: "daily", used: "daily_usage_usd", limit: "daily_limit_usd"},
			{label: "weekly", used: "weekly_usage_usd", limit: "weekly_limit_usd"},
			{label: "monthly", used: "monthly_usage_usd", limit: "monthly_limit_usd"},
		} {
			if quota, ok := subscription[window.label].(map[string]any); ok {
				used := quotaProbeFloat(quota["percentage"])
				if used != nil {
					limit := 100.0
					remaining := limit - *used
					if remaining < 0 {
						remaining = 0
					}
					entry := QuotaProbeEntry{
						Label:     window.label,
						Unit:      "percent",
						Limit:     &limit,
						Used:      used,
						Remaining: &remaining,
					}
					if resetAt := strings.TrimSpace(anyString(quota["resets_at"])); resetAt != "" {
						entry.ResetAt = &resetAt
					}
					entries = append(entries, entry)
					planWindows = true
					continue
				}
			}
			limit := quotaProbeFloat(subscription[window.limit])
			used := quotaProbeFloat(subscription[window.used])
			if limit == nil || *limit <= 0 || used == nil {
				continue
			}
			remaining := *limit - *used
			entries = append(entries, QuotaProbeEntry{
				Label:     window.label,
				Unit:      "usd",
				Limit:     limit,
				Used:      used,
				Remaining: &remaining,
			})
		}
	}

	if rateLimits, ok := payload["rate_limits"].([]any); ok {
		for _, raw := range rateLimits {
			window, _ := raw.(map[string]any)
			label := strings.TrimSpace(anyString(window["window"]))
			if label == "" {
				continue
			}
			entry := QuotaProbeEntry{Label: label, Unit: "requests"}
			entry.Limit = quotaProbeFloat(window["limit"])
			entry.Used = quotaProbeFloat(window["used"])
			entry.Remaining = quotaProbeFloat(window["remaining"])
			if entry.Limit != nil {
				entries = append(entries, entry)
			}
		}
	}

	kind := "balance"
	if planWindows || plan != "" {
		kind = "subscription_plan"
	} else if len(entries) > 1 {
		kind = "mixed"
	}
	return kind, entries, plan, expiresAt, nil
}

const newAPIQuotaPerUnit = 500000

func probeNewAPIQuota(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, error) {
	kind, entries, err := probeNewAPITokenUsage(ctx, client, baseURL, secret)
	if err == nil {
		return kind, entries, nil
	}
	if ctx.Err() != nil {
		return "", nil, err
	}
	return probeNewAPIBilling(ctx, client, baseURL, secret)
}

func probeNewAPITokenUsage(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, error) {
	payload, err := quotaProbeGetJSON(ctx, client, quotaProbeURL(baseURL, "/api/usage/token"), secret)
	if err != nil {
		return "", nil, err
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return "", nil, fmt.Errorf("token usage endpoint did not return data")
	}

	entry := QuotaProbeEntry{Label: "balance", Unit: "usd"}
	if unlimited, _ := data["unlimited_quota"].(bool); unlimited {
		entry.Unlimited = true
		if used := quotaProbeFloat(data["total_used"]); used != nil {
			converted := *used / newAPIQuotaPerUnit
			entry.Used = &converted
		}
		return "unlimited", []QuotaProbeEntry{entry}, nil
	}
	for source, target := range map[string]**float64{
		"total_available": &entry.Remaining,
		"total_granted":   &entry.Limit,
		"total_used":      &entry.Used,
	} {
		if value := quotaProbeFloat(data[source]); value != nil {
			converted := *value / newAPIQuotaPerUnit
			*target = &converted
		}
	}
	if entry.Remaining == nil && entry.Limit == nil {
		return "", nil, fmt.Errorf("token usage endpoint did not return usable quota fields")
	}
	return "balance", []QuotaProbeEntry{entry}, nil
}

func probeNewAPIBilling(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, error) {
	subscription, err := quotaProbeGetJSONWithFallback(ctx, client, baseURL, secret,
		"/v1/dashboard/billing/subscription", "/dashboard/billing/subscription")
	if err != nil {
		return "", nil, err
	}
	limit := quotaProbeFloat(subscription["hard_limit_usd"])
	if limit != nil && *limit >= 100000000 {
		return "unlimited", []QuotaProbeEntry{{Label: "balance", Unit: "usd", Unlimited: true}}, nil
	}

	now := time.Now().UTC()
	usagePath := fmt.Sprintf("/v1/dashboard/billing/usage?start_date=%s&end_date=%s",
		now.AddDate(0, 0, -99).Format("2006-01-02"), now.AddDate(0, 0, 1).Format("2006-01-02"))
	usageFallback := strings.TrimPrefix(usagePath, "/v1")
	usage, err := quotaProbeGetJSONWithFallback(ctx, client, baseURL, secret, usagePath, usageFallback)
	if err != nil {
		return "", nil, err
	}

	entry := QuotaProbeEntry{Label: "balance", Unit: "usd", Limit: limit}
	if total := quotaProbeFloat(usage["total_usage"]); total != nil {
		used := *total / 100
		entry.Used = &used
		if limit != nil {
			remaining := *limit - used
			entry.Remaining = &remaining
		}
	}
	if entry.Limit == nil && entry.Used == nil {
		return "", nil, fmt.Errorf("billing endpoints did not return usable quota fields")
	}
	return "balance", []QuotaProbeEntry{entry}, nil
}

func probeXLyraQuota(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, error) {
	payload, err := quotaProbeGetJSON(ctx, client, quotaProbeURL(baseURL, "/v1/user/balance"), secret)
	if err != nil {
		return "", nil, err
	}

	entry := QuotaProbeEntry{Label: "balance", Unit: quotaProbeUnit(anyString(payload["unit"]), "usd")}
	unlimited, _ := payload["quota_unlimited"].(bool)
	if unlimited {
		entry.Used = quotaProbeFloat(payload["quota_used"])
		if entry.Used == nil {
			entry.Used = quotaProbeFloat(payload["quota_total_used"])
		}
	} else {
		entry.Used = quotaProbeFloat(payload["quota_total_used"])
		if entry.Used == nil {
			entry.Used = quotaProbeFloat(payload["quota_used"])
		}
	}
	if unlimited {
		entry.Unlimited = true
		return "unlimited", []QuotaProbeEntry{entry}, nil
	}
	entry.Limit = quotaProbeFloat(payload["quota_limit"])
	entry.Remaining = quotaProbeFloat(payload["balance"])
	if entry.Remaining == nil && entry.Limit == nil {
		return "", nil, fmt.Errorf("balance endpoint did not return usable quota fields")
	}
	return "balance", []QuotaProbeEntry{entry}, nil
}

// probeKimiQuota 查询 Kimi For Coding 的 Coding Plan 额度。
// 接口与 cc-switch 一致：GET {base}/v1/usages，Bearer 为推理用 API Key。
// 数值字段是 proto3 JSON 风格的字符串（如 "100"），由 quotaProbeFloat 兼容解析。
func probeKimiQuota(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, string, error) {
	payload, err := quotaProbeGetJSON(ctx, client, quotaProbeKimiUsagesURL(baseURL), secret)
	if err != nil {
		return "", nil, "", err
	}

	plan := ""
	region := ""
	if user, ok := payload["user"].(map[string]any); ok {
		region = anyString(user["region"])
		if membership, ok := user["membership"].(map[string]any); ok {
			plan = kimiMembershipPlanName(anyString(membership["level"]), region)
		}
	}

	entries := make([]QuotaProbeEntry, 0, 2)

	// limits[] 中的 5 小时滚动窗口（window.duration=300 分钟）。
	// 按 window 描述符识别而不是按下标，官方以后新增窗口也不会串位。
	for _, raw := range anySlice(payload["limits"]) {
		item, _ := raw.(map[string]any)
		window, _ := item["window"].(map[string]any)
		if !kimiWindowIsFiveHour(window) {
			continue
		}
		if entry, ok := kimiUsageDetailEntry("five_hour", item["detail"]); ok {
			entries = append(entries, entry)
		}
	}

	// usage 块为周额度（订阅日起 7 天滚动）。
	if entry, ok := kimiUsageDetailEntry("weekly", payload["usage"]); ok {
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return "", nil, plan, fmt.Errorf("usages endpoint did not contain quota data")
	}
	return "token_plan", entries, plan, nil
}

func quotaProbeKimiUsagesURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://api.kimi.com/coding"
	}
	// 容忍用户把 base_url 填成 .../coding/v1，避免拼出 /v1/v1/usages
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1/usages"
}

func kimiWindowIsFiveHour(window map[string]any) bool {
	if window == nil {
		return false
	}
	duration := quotaProbeFloat(window["duration"])
	if duration == nil {
		return false
	}
	unit := strings.ToUpper(anyString(window["timeUnit"]))
	switch {
	case strings.Contains(unit, "MINUTE"):
		return *duration == 300
	case strings.Contains(unit, "HOUR"):
		return *duration == 5
	case strings.Contains(unit, "SECOND"):
		return *duration == 18000
	default:
		return false
	}
}

func kimiUsageDetailEntry(label string, raw any) (QuotaProbeEntry, bool) {
	detail, ok := raw.(map[string]any)
	if !ok {
		return QuotaProbeEntry{}, false
	}
	entry := QuotaProbeEntry{Label: label, Unit: "percent"}
	entry.Limit = quotaProbeFloat(detail["limit"])
	entry.Used = quotaProbeFloat(detail["used"])
	entry.Remaining = quotaProbeFloat(detail["remaining"])
	if entry.Remaining == nil && entry.Limit != nil && entry.Used != nil {
		remaining := *entry.Limit - *entry.Used
		if remaining < 0 {
			remaining = 0
		}
		entry.Remaining = &remaining
	}
	if reset := strings.TrimSpace(anyString(detail["resetTime"])); reset != "" {
		entry.ResetAt = &reset
	}
	if entry.Remaining == nil && entry.Limit == nil {
		return QuotaProbeEntry{}, false
	}
	return entry, true
}

// kimiMembershipPlanName 把 usages 接口的 user.membership.level 枚举映射为
// 官方档位名（速度记号）。官方付费档从低到高：Andante / Moderato / Allegretto /
// Allegro（之上还有 Vivace）。LEVEL_BASIC 在中国区是 Andante，国际版是 Moderato，
// 靠 user.region 区分；未知枚举兜底为去前缀的可读形式，新档位不会静默丢失。
func kimiMembershipPlanName(level string, region string) string {
	level = strings.ToUpper(strings.TrimSpace(level))
	if level == "" {
		return ""
	}
	region = strings.ToUpper(strings.TrimSpace(region))
	switch level {
	case "LEVEL_BASIC":
		if region == "REGION_CN" || region == "CN" || region == "CHINA" {
			return "Andante"
		}
		return "Moderato"
	case "LEVEL_STANDARD":
		return "Moderato"
	case "LEVEL_INTERMEDIATE":
		return "Allegretto"
	case "LEVEL_ADVANCED":
		return "Allegro"
	case "LEVEL_PREMIUM":
		return "Vivace"
	default:
		name := strings.TrimPrefix(level, "LEVEL_")
		name = strings.ReplaceAll(name, "_", " ")
		if name == "" {
			return ""
		}
		return strings.ToUpper(name[:1]) + strings.ToLower(name[1:])
	}
}

// probeGLMQuota 查询 GLM Coding Plan 的额度。
// 接口：GET {origin}/api/monitor/usage/quota/limit，Bearer 为推理用 API Key，
// origin 取站点 base_url 的 scheme://host（open.bigmodel.cn 与 api.z.ai 同路径）。
// 套餐按制式分代返回不同 type（2026-07-30 积分制上线，旧套餐并存）：
//   - TOKENS_LIMIT：旧 Token 套餐，5 小时/周窗口，percentage 是已用百分比
//   - CREDIT_LIMIT：积分制套餐，5 小时/周窗口，usage/currentValue/remaining 为积分绝对值
//   - TIME_LIMIT：MCP 工具月度额度（unit=5 分钟×1），usageDetails 为各模型调用次数
// 窗口按 unit × number 换算分钟数识别：300 分钟 = 5 小时，10080 分钟 = 周。
func probeGLMQuota(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, string, error) {
	endpoint, err := quotaProbeGLMLimitURL(baseURL)
	if err != nil {
		return "", nil, "", err
	}
	payload, err := quotaProbeGetJSON(ctx, client, endpoint, secret)
	if err != nil {
		return "", nil, "", err
	}
	if code := quotaProbeFloat(payload["code"]); code == nil || *code != 200 {
		return "", nil, "", fmt.Errorf("quota limit endpoint returned error: %s", anyString(payload["msg"]))
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		return "", nil, "", fmt.Errorf("quota limit endpoint did not return data")
	}

	entries := make([]QuotaProbeEntry, 0, 3)
	// 同一窗口可能以不同单位编码出现两次（unit=3/number=5 与 unit=5/number=300
	// 都是 300 分钟），按 label 去重并保留剩余最少的一条，与 summary 取最紧张
	// 窗口的口径一致。
	entryIndex := map[string]int{}
	for _, raw := range anySlice(data["limits"]) {
		item, _ := raw.(map[string]any)
		switch strings.ToUpper(anyString(item["type"])) {
		case "TOKENS_LIMIT", "CREDIT_LIMIT":
			label := glmWindowLabel(item)
			if label == "" {
				continue
			}
			entry, ok := glmLimitEntry(label, item)
			if !ok {
				continue
			}
			if at, dup := entryIndex[label]; dup {
				if entry.Remaining != nil && *entry.Remaining < *entries[at].Remaining {
					entries[at] = entry
				}
				continue
			}
			entryIndex[label] = len(entries)
			entries = append(entries, entry)
		case "TIME_LIMIT":
			// MCP 月度额度（usage/currentValue/remaining 是调用次数，非金额），
			// 按次数转百分比折算进 monthly label。
			entry, ok := glmMCPMonthlyEntry(item)
			if !ok {
				continue
			}
			if at, dup := entryIndex[entry.Label]; dup {
				if entry.Remaining != nil && *entry.Remaining < *entries[at].Remaining {
					entries[at] = entry
				}
				continue
			}
			entryIndex[entry.Label] = len(entries)
			entries = append(entries, entry)
		}
	}
	_, hasFiveHour := entryIndex["five_hour"]
	_, hasWeekly := entryIndex["weekly"]
	if !hasFiveHour && !hasWeekly {
		return "", nil, "", fmt.Errorf("quota limit endpoint did not contain token quota data")
	}
	return "token_plan", entries, glmPlanName(anyString(data["level"])), nil
}

// glmWindowLabel 按窗口时长识别 TOKENS_LIMIT 窗口：300 分钟 = 5 小时，10080 分钟 = 周。
// unit 是时间单位编码（1=天 3=小时 5=分钟 6=周），窗口分钟数 = number × unit 换算值。
// 未知窗口（如日/月）跳过；官方新增窗口时给 glmUnitMinutes 加映射即可。
var glmUnitMinutes = map[int]int{1: 1440, 3: 60, 5: 1, 6: 10080}

func glmWindowLabel(item map[string]any) string {
	unit := quotaProbeFloat(item["unit"])
	number := quotaProbeFloat(item["number"])
	if unit == nil || number == nil || *number <= 0 {
		return ""
	}
	minutes, ok := glmUnitMinutes[int(*unit)]
	if !ok {
		return ""
	}
	switch minutes * int(*number) {
	case 300:
		return "five_hour"
	case 10080:
		return "weekly"
	}
	return ""
}

// glmLimitEntry 把一条 TOKENS_LIMIT / CREDIT_LIMIT 折算成百分比 entry
// （limit 恒为 100），与 kimi 的 entries 同构，前端按 five_hour/weekly label 渲染。
// 积分制 CREDIT_LIMIT 的 usage/currentValue/remaining 是积分绝对值：优先按
// currentValue/usage 换算精确百分比——上游 percentage 是服务端取整（如
// 1438/12000 记为 11 而非 11.98），有舍入误差，仅在缺少绝对值时作兜底。
func glmLimitEntry(label string, item map[string]any) (QuotaProbeEntry, bool) {
	usage := quotaProbeFloat(item["usage"])
	current := quotaProbeFloat(item["currentValue"])
	if usage != nil && *usage > 0 && current != nil {
		used := clampQuotaPercent(*current / *usage * 100)
		return glmQuotaPercentEntry(label, used, item), true
	}
	used := quotaProbeFloat(item["percentage"])
	if used == nil {
		return QuotaProbeEntry{}, false
	}
	return glmQuotaPercentEntry(label, *used, item), true
}

// glmMCPMonthlyEntry 把 TIME_LIMIT（MCP 工具月度调用次数额度）折算成 monthly
// 百分比 entry；次数口径转百分比，避免被前端当金额单位展示。
func glmMCPMonthlyEntry(item map[string]any) (QuotaProbeEntry, bool) {
	usage := quotaProbeFloat(item["usage"])
	current := quotaProbeFloat(item["currentValue"])
	if usage != nil && *usage > 0 && current != nil {
		used := clampQuotaPercent(*current / *usage * 100)
		return glmQuotaPercentEntry("monthly", used, item), true
	}
	used := quotaProbeFloat(item["percentage"])
	if used == nil {
		return QuotaProbeEntry{}, false
	}
	return glmQuotaPercentEntry("monthly", *used, item), true
}

func glmQuotaPercentEntry(label string, used float64, item map[string]any) QuotaProbeEntry {
	clamped := clampQuotaPercent(used)
	limit := 100.0
	remaining := 100 - clamped
	entry := QuotaProbeEntry{Label: label, Unit: "percent", Limit: &limit, Used: &clamped, Remaining: &remaining}
	if reset := quotaProbeFloat(item["nextResetTime"]); reset != nil {
		resetAt := time.UnixMilli(int64(*reset)).UTC().Format(time.RFC3339)
		entry.ResetAt = &resetAt
	}
	return entry
}

func clampQuotaPercent(value float64) float64 {
	return min(100, max(0, value))
}

// glmPlanName 把 data.level（如 "pro"）转为档位名（"Pro"）。
func glmPlanName(level string) string {
	runes := []rune(strings.TrimSpace(level))
	if len(runes) == 0 {
		return ""
	}
	return strings.ToUpper(string(runes[:1])) + strings.ToLower(string(runes[1:]))
}

func probeDeepSeekBalance(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, *bool, error) {
	payload, err := quotaProbeGetJSON(ctx, client, deepSeekBalanceBaseURL(baseURL)+"/user/balance", secret)
	if err != nil {
		return "", nil, nil, err
	}
	var available *bool
	if value, ok := payload["is_available"].(bool); ok {
		available = &value
	}
	entries := make([]QuotaProbeEntry, 0, 2)
	for _, raw := range anySlice(payload["balance_infos"]) {
		item, _ := raw.(map[string]any)
		entry := QuotaProbeEntry{
			Label:           "balance",
			Unit:            quotaProbeUnit(anyString(item["currency"]), "cny"),
			Remaining:       quotaProbeFloat(item["total_balance"]),
			GrantedBalance:  quotaProbeFloat(item["granted_balance"]),
			ToppedUpBalance: quotaProbeFloat(item["topped_up_balance"]),
		}
		if entry.Remaining != nil {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return "", nil, nil, fmt.Errorf("balance endpoint did not contain balance data")
	}
	return "balance", entries, available, nil
}

// deepSeekBalanceBaseURL 返回余额接口的 base：DeepSeek 的 /user/balance 不在
// /v1 命名空间下，base_url 以 /v1 结尾时剥掉。
func deepSeekBalanceBaseURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return "https://api.deepseek.com"
	}
	if strings.HasSuffix(strings.ToLower(trimmed), "/v1") {
		return trimmed[:len(trimmed)-len("/v1")]
	}
	return trimmed
}

func quotaProbeGLMLimitURL(baseURL string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = "https://open.bigmodel.cn"
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base URL %q for GLM quota probe", baseURL)
	}
	return parsed.Scheme + "://" + parsed.Host + "/api/monitor/usage/quota/limit", nil
}

func anySlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	return nil
}

func quotaProbeGetJSONWithFallback(ctx context.Context, client *http.Client, baseURL string, secret string, path string, fallbackPath string) (map[string]any, error) {
	payload, err := quotaProbeGetJSON(ctx, client, quotaProbeURL(baseURL, path), secret)
	if err == nil {
		return payload, nil
	}
	if fallbackPath == "" || fallbackPath == path || !quotaProbeShouldFallback(err) {
		return nil, err
	}
	return quotaProbeGetJSON(ctx, client, quotaProbeURL(baseURL, fallbackPath), secret)
}

type quotaProbeHTTPError struct {
	StatusCode int
	Message    string
}

func (e *quotaProbeHTTPError) Error() string {
	return e.Message
}

func quotaProbeShouldFallback(err error) bool {
	httpErr, ok := err.(*quotaProbeHTTPError)
	return ok && (httpErr.StatusCode == http.StatusNotFound || httpErr.StatusCode == http.StatusMethodNotAllowed)
}

func quotaProbeGetJSON(ctx context.Context, client *http.Client, url string, secret string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, quotaProbeBodyLimit))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 256 {
			message = message[:256]
		}
		return nil, &quotaProbeHTTPError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, message),
		}
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON response")
	}
	return payload, nil
}

func quotaProbeURL(baseURL string, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + path
}

func quotaProbeUnit(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func quotaProbeFloat(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case int:
		result := float64(typed)
		return &result
	case int64:
		result := float64(typed)
		return &result
	case json.Number:
		if parsed, err := typed.Float64(); err == nil {
			return &parsed
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		var parsed float64
		if _, err := fmt.Sscanf(trimmed, "%f", &parsed); err == nil {
			return &parsed
		}
	}
	return nil
}
