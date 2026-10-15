package site

import (
	"context"
	"encoding/json"
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
	CashBalance     *float64 `json:"cash_balance,omitempty"`
	VoucherBalance  *float64 `json:"voucher_balance,omitempty"`
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
			result = s.probeQuota(ctx, client, probeType, item.SiteType, credential.CredentialType, item.BaseURL, secret)
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
	case "moonshot":
		if quotaProbeBaseURLOfficial(item.BaseURL, "api.moonshot.cn") {
			return QuotaProbeTypeMoonshot
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

func quotaProbeURL(baseURL string, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + path
}
