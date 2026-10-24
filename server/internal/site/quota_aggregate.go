package site

import (
	"math"
	"strings"
	"time"
)

// QuotaAggregateSummary is the site-level rollup of per-credential quota entries.
// A nil numeric field was missing on every included entry. It is not stored as zero.
type QuotaAggregateSummary struct {
	Remaining *float64
	Limit     *float64
	Used      *float64
	Unit      string
	ResetAt   *string
}

// AggregateQuota rolls entries up by mode.
// sum adds each finite, non-negative remaining, limit, and used separately, and
// its resetAt is the earliest future time. min keeps the smallest finite
// non-negative remaining plus that entry's limit and used; its resetAt is that
// entry's own resetAt when the time is still in the future. Equal remainings
// prefer the earlier future resetAt. none, any other mode, and an empty list
// return ok false.
func AggregateQuota(mode string, items []QuotaProbeEntry) (QuotaAggregateSummary, bool) {
	switch mode {
	case "sum":
		if len(items) == 0 {
			return QuotaAggregateSummary{}, false
		}
		return sumQuotaEntries(items, time.Now()), true
	case "min":
		if len(items) == 0 {
			return QuotaAggregateSummary{}, false
		}
		return minQuotaEntries(items, time.Now())
	default:
		return QuotaAggregateSummary{}, false
	}
}

func sumQuotaEntries(items []QuotaProbeEntry, now time.Time) QuotaAggregateSummary {
	return QuotaAggregateSummary{
		Remaining: sumQuotaField(items, func(item QuotaProbeEntry) *float64 { return item.Remaining }),
		Limit:     sumQuotaField(items, func(item QuotaProbeEntry) *float64 { return item.Limit }),
		Used:      sumQuotaField(items, func(item QuotaProbeEntry) *float64 { return item.Used }),
		Unit:      quotaAggregateUnit(items),
		ResetAt:   earliestFutureReset(items, now),
	}
}

func minQuotaEntries(items []QuotaProbeEntry, now time.Time) (QuotaAggregateSummary, bool) {
	selected := -1
	var selectedRemaining float64
	var selectedReset time.Time
	selectedHasReset := false
	for i := range items {
		remaining, ok := quotaFinite(items[i].Remaining)
		if !ok {
			continue
		}
		reset, hasReset := futureResetTime(items[i], now)
		if selected < 0 || remaining < selectedRemaining || (remaining == selectedRemaining && earlierFutureReset(hasReset, reset, selectedHasReset, selectedReset)) {
			selected = i
			selectedRemaining = remaining
			selectedReset = reset
			selectedHasReset = hasReset
		}
	}
	if selected < 0 {
		return QuotaAggregateSummary{}, false
	}
	item := items[selected]
	summary := QuotaAggregateSummary{
		Remaining: floatPointer(selectedRemaining),
		Limit:     finitePointer(item.Limit),
		Used:      finitePointer(item.Used),
		Unit:      item.Unit,
	}
	if selectedHasReset && item.ResetAt != nil {
		raw := strings.TrimSpace(*item.ResetAt)
		summary.ResetAt = &raw
	}
	return summary, true
}

func earlierFutureReset(hasReset bool, reset time.Time, selectedHasReset bool, selectedReset time.Time) bool {
	if hasReset && !selectedHasReset {
		return true
	}
	return hasReset && selectedHasReset && reset.Before(selectedReset)
}

func sumQuotaField(items []QuotaProbeEntry, field func(QuotaProbeEntry) *float64) *float64 {
	var total float64
	seen := false
	for _, item := range items {
		number, ok := quotaFinite(field(item))
		if !ok {
			continue
		}
		total += number
		seen = true
	}
	if !seen {
		return nil
	}
	return floatPointer(total)
}

func quotaAggregateUnit(items []QuotaProbeEntry) string {
	unit := ""
	for _, item := range items {
		if !quotaEntryHasNumber(item) || item.Unit == "" {
			continue
		}
		if unit == "" {
			unit = item.Unit
			continue
		}
		if item.Unit != unit {
			return ""
		}
	}
	return unit
}

func quotaEntryHasNumber(item QuotaProbeEntry) bool {
	if _, ok := quotaFinite(item.Remaining); ok {
		return true
	}
	if _, ok := quotaFinite(item.Limit); ok {
		return true
	}
	_, ok := quotaFinite(item.Used)
	return ok
}

func earliestFutureReset(items []QuotaProbeEntry, now time.Time) *string {
	var best time.Time
	raw := ""
	found := false
	for _, item := range items {
		parsed, ok := futureResetTime(item, now)
		if !ok || item.ResetAt == nil {
			continue
		}
		if !found || parsed.Before(best) {
			best = parsed
			raw = strings.TrimSpace(*item.ResetAt)
			found = true
		}
	}
	if !found {
		return nil
	}
	return &raw
}

func futureResetTime(item QuotaProbeEntry, now time.Time) (time.Time, bool) {
	if item.ResetAt == nil {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(*item.ResetAt)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil || !parsed.After(now) {
		return time.Time{}, false
	}
	return parsed, true
}

func quotaFinite(value *float64) (float64, bool) {
	if value == nil {
		return 0, false
	}
	number := *value
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
		return 0, false
	}
	return number, true
}

func finitePointer(value *float64) *float64 {
	number, ok := quotaFinite(value)
	if !ok {
		return nil
	}
	return floatPointer(number)
}

func floatPointer(number float64) *float64 {
	return &number
}

// explicitPluginQuotaAggregate is the manifest mode for an API-key plugin site.
// Empty means the probe summary stays on the historical minimum. Account-pool
// sites keep their mode on Pool and are not redirected here.
func explicitPluginQuotaAggregate(siteType string) string {
	item, ok := LookupPluginSiteType(siteType)
	if !ok || item.Pool != nil {
		return ""
	}
	switch item.QuotaAggregate {
	case "sum", "min", "none":
		return item.QuotaAggregate
	default:
		return ""
	}
}
