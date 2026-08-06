package site

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	Label     string   `json:"label"`
	Unit      string   `json:"unit,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	Limit     *float64 `json:"limit,omitempty"`
	Used      *float64 `json:"used,omitempty"`
	Unlimited bool     `json:"unlimited,omitempty"`
	ResetAt   *string  `json:"reset_at,omitempty"`
}

type QuotaProbeResult struct {
	Status    string            `json:"status"`
	Error     string            `json:"error,omitempty"`
	Kind      string            `json:"kind,omitempty"`
	Plan      string            `json:"plan,omitempty"`
	Entries   []QuotaProbeEntry `json:"entries,omitempty"`
	FetchedAt time.Time         `json:"fetched_at"`
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
		if result.Status != "ok" {
			result = preserveQuotaProbeResult(credential.Meta, result)
		}
		if result.Status == "ok" {
			okCount++
			// 取首个成功凭据的完整窗口明细作为展示数据（kimi_code 通常只有一把
			// key；多凭据时 minEntry 仍按所有凭据的最紧张窗口计算）
			if result.Plan != "" && plan == "" {
				plan = result.Plan
			}
			if len(result.Entries) > 0 && summaryEntries == nil {
				summaryEntries = result.Entries
			}
			if entry, ok := quotaProbePrimaryEntry(result); ok {
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
		if !sub2APISubscriptionQuotaRecovered(result, anyString(metadata["limit_window"])) {
			continue
		}
		_, _ = repo.ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
			SiteID:           siteID,
			SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
			Reasons:          []string{store.CooldownReasonUpstreamSubscriptionLimitExceeded},
		})
		return
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

func quotaProbeCredentialEligible(credentialType string) bool {
	return credentialType == "api_key" || strings.HasPrefix(credentialType, "api_key:")
}

// defaultQuotaProbeTypeForSite 给未显式配置 quota_probe 的站点类型提供默认探测。
// Kimi Code 官方站（api.kimi.com/coding）有 Coding Plan 额度接口，无需用户配置。
func defaultQuotaProbeTypeForSite(item store.Site) string {
	if item.SiteType == "kimi_code" {
		return QuotaProbeTypeKimi
	}
	return ""
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
	result.Entries = previous.Entries
	result.FetchedAt = previous.FetchedAt
	return result
}

func preserveQuotaSummaryValues(siteMeta map[string]any, summary map[string]any) {
	previous, ok := siteMeta[siteMetaQuotaProbeSummary].(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"remaining_min", "unit", "limit", "used", "unlimited", "used_total", "entries", "plan"} {
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

func probeQuota(ctx context.Context, client *http.Client, probeType string, baseURL string, secret string) QuotaProbeResult {
	result := QuotaProbeResult{Status: "error", FetchedAt: time.Now().UTC()}
	var entries []QuotaProbeEntry
	var kind string
	var err error

	switch probeType {
	case QuotaProbeTypeSub2API:
		kind, entries, err = probeSub2APIQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeNewAPI:
		kind, entries, err = probeNewAPIQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeXLyra:
		kind, entries, err = probeXLyraQuota(ctx, client, baseURL, secret)
	case QuotaProbeTypeKimi:
		kind, entries, result.Plan, err = probeKimiQuota(ctx, client, baseURL, secret)
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

func probeSub2APIQuota(ctx context.Context, client *http.Client, baseURL string, secret string) (string, []QuotaProbeEntry, error) {
	payload, err := quotaProbeGetJSON(ctx, client, quotaProbeURL(baseURL, "/v1/usage"), secret)
	if err != nil {
		return "", nil, err
	}

	entries := make([]QuotaProbeEntry, 0, 4)
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
		for _, window := range []struct {
			label string
			used  string
			limit string
		}{
			{label: "daily", used: "daily_usage_usd", limit: "daily_limit_usd"},
			{label: "weekly", used: "weekly_usage_usd", limit: "weekly_limit_usd"},
			{label: "monthly", used: "monthly_usage_usd", limit: "monthly_limit_usd"},
		} {
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
	if len(entries) > 1 {
		kind = "mixed"
	}
	return kind, entries, nil
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
	entry.Used = quotaProbeFloat(payload["quota_used"])
	if unlimited, _ := payload["quota_unlimited"].(bool); unlimited {
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
