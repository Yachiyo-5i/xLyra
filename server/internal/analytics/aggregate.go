package analytics

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

const (
	breakdownLimit = 20
	seriesTopN     = 9
	matrixTopN     = 10
)

type usageMetrics struct {
	requests               int64
	successCount           int64
	failureCount           int64
	promptTokens           int64
	completionTokens       int64
	cachedTokens           int64
	totalTokens            int64
	cost                   float64
	latencyCount           int64
	latencyTotalMS         int64
	latencyMaxMS           int64
	upstreamLatencyCount   int64
	upstreamLatencyTotalMS int64
}

func (m *usageMetrics) add(row store.RequestUsageDailySummary) {
	m.requests += row.RequestCount
	m.successCount += row.SuccessCount
	m.failureCount += row.FailureCount
	m.promptTokens += row.PromptTokens
	m.completionTokens += row.CompletionTokens
	m.cachedTokens += row.CachedTokens
	m.totalTokens += row.TotalTokens
	m.cost += row.EstimatedCost
	m.latencyCount += row.LatencyCount
	m.latencyTotalMS += row.LatencyTotalMS
	if row.LatencyMaxMS.Valid && row.LatencyMaxMS.Int64 > m.latencyMaxMS {
		m.latencyMaxMS = row.LatencyMaxMS.Int64
	}
	m.upstreamLatencyCount += row.UpstreamLatencyCount
	m.upstreamLatencyTotalMS += row.UpstreamLatencyTotalMS
}

func (m *usageMetrics) addMetrics(other usageMetrics) {
	m.requests += other.requests
	m.successCount += other.successCount
	m.failureCount += other.failureCount
	m.promptTokens += other.promptTokens
	m.completionTokens += other.completionTokens
	m.cachedTokens += other.cachedTokens
	m.totalTokens += other.totalTokens
	m.cost += other.cost
	m.latencyCount += other.latencyCount
	m.latencyTotalMS += other.latencyTotalMS
	if other.latencyMaxMS > m.latencyMaxMS {
		m.latencyMaxMS = other.latencyMaxMS
	}
	m.upstreamLatencyCount += other.upstreamLatencyCount
	m.upstreamLatencyTotalMS += other.upstreamLatencyTotalMS
}

func (m usageMetrics) avgLatencyMS() float64 {
	if m.latencyCount == 0 {
		return 0
	}
	return float64(m.latencyTotalMS) / float64(m.latencyCount)
}

func (m usageMetrics) avgUpstreamLatencyMS() float64 {
	if m.upstreamLatencyCount == 0 {
		return 0
	}
	return float64(m.upstreamLatencyTotalMS) / float64(m.upstreamLatencyCount)
}

func (m usageMetrics) successRate() *float64 {
	if m.requests == 0 {
		return nil
	}
	value := float64(m.successCount) / float64(m.requests)
	return &value
}

func (m usageMetrics) cacheHitRate() *float64 {
	if m.promptTokens == 0 {
		return nil
	}
	value := float64(m.cachedTokens) / float64(m.promptTokens)
	return &value
}

func normalizeCurrency(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultCurrency
	}
	return strings.ToUpper(value)
}

func currencyOverview(rows []store.RequestUsageDailySummary) ([]string, map[string]float64) {
	costByCurrency := map[string]float64{}
	for _, row := range rows {
		costByCurrency[normalizeCurrency(row.Currency)] += row.EstimatedCost
	}
	available := make([]string, 0, len(costByCurrency))
	for currency := range costByCurrency {
		available = append(available, currency)
	}
	sort.Strings(available)
	return available, costByCurrency
}

func pickDisplayCurrency(requested string, available []string, costByCurrency map[string]float64) string {
	if value := strings.ToUpper(strings.TrimSpace(requested)); value != "" {
		return value
	}
	display := ""
	for _, currency := range available {
		if display == "" || costByCurrency[currency] > costByCurrency[display] {
			display = currency
		}
	}
	if display == "" {
		display = defaultCurrency
	}
	return display
}

func filterRowsByCurrency(rows []store.RequestUsageDailySummary, currency string) []store.RequestUsageDailySummary {
	filtered := make([]store.RequestUsageDailySummary, 0, len(rows))
	for _, row := range rows {
		if normalizeCurrency(row.Currency) == currency {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func buildTotals(rows []store.RequestUsageDailySummary, previousRows []store.RequestUsageDailySummary, costByCurrency map[string]float64, timeZone config.TimeZone, prevFrom time.Time, prevTo time.Time) UsageTotals {
	var current usageMetrics
	for _, row := range rows {
		current.add(row)
	}
	var previous usageMetrics
	for _, row := range previousRows {
		previous.add(row)
	}
	return UsageTotals{
		Requests:             current.requests,
		SuccessCount:         current.successCount,
		FailureCount:         current.failureCount,
		SuccessRate:          current.successRate(),
		PromptTokens:         current.promptTokens,
		CompletionTokens:     current.completionTokens,
		CachedTokens:         current.cachedTokens,
		TotalTokens:          current.totalTokens,
		CacheHitRate:         current.cacheHitRate(),
		Cost:                 current.cost,
		CostByCurrency:       costByCurrency,
		AvgLatencyMS:         current.avgLatencyMS(),
		MaxLatencyMS:         current.latencyMaxMS,
		AvgUpstreamLatencyMS: current.avgUpstreamLatencyMS(),
		PreviousPeriod: PreviousPeriod{
			From:         timeZone.Format(prevFrom, "2006-01-02"),
			To:           timeZone.Format(prevTo, "2006-01-02"),
			Requests:     previous.requests,
			SuccessRate:  previous.successRate(),
			TotalTokens:  previous.totalTokens,
			Cost:         previous.cost,
			AvgLatencyMS: previous.avgLatencyMS(),
		},
	}
}

type dimensionRef struct {
	key   string
	id    *string
	label string
}

func normalizeSummaryValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == summaryNoneKey {
		return ""
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func uuidStringPtr(value uuid.NullUUID) *string {
	if !value.Valid || value.UUID == uuid.Nil {
		return nil
	}
	text := value.UUID.String()
	return &text
}

func siteDimension(row store.RequestUsageDailySummary) dimensionRef {
	if id := uuidStringPtr(row.SiteID); id != nil {
		return dimensionRef{
			key:   *id,
			id:    id,
			label: firstNonEmpty(normalizeSummaryValue(row.SiteName), normalizeSummaryValue(row.SiteSlug), unknownLabel),
		}
	}
	key := normalizeSummaryValue(row.SiteKey)
	if key == "" {
		key = unknownLabel
	}
	return dimensionRef{
		key:   key,
		label: firstNonEmpty(normalizeSummaryValue(row.SiteName), normalizeSummaryValue(row.SiteSlug), unknownLabel),
	}
}

func modelDimension(row store.RequestUsageDailySummary) dimensionRef {
	key := normalizeSummaryValue(row.CanonicalModelKey)
	if key == "" {
		key = unknownLabel
	}
	return dimensionRef{key: key, id: uuidStringPtr(row.CanonicalModelID), label: key}
}

func siteModelDimension(row store.RequestUsageDailySummary) dimensionRef {
	key := normalizeSummaryValue(row.SiteModelKey)
	if id := uuidStringPtr(row.SiteModelID); id != nil {
		key = *id
	}
	if key == "" {
		key = unknownLabel
	}
	return dimensionRef{
		key:   key,
		id:    uuidStringPtr(row.SiteModelID),
		label: firstNonEmpty(normalizeSummaryValue(row.UpstreamModelName), unknownLabel),
	}
}

func apiKeyDimension(row store.RequestUsageDailySummary, apiKeys map[uuid.UUID]store.APIKey) dimensionRef {
	id := uuidStringPtr(row.APIKeyID)
	if id == nil {
		return dimensionRef{key: unknownLabel, label: unknownLabel}
	}
	label := normalizeSummaryValue(row.APIKeyName)
	if apiKey, ok := apiKeys[row.APIKeyID.UUID]; ok {
		if name := strings.TrimSpace(apiKey.Name); name != "" {
			label = name
		}
	}
	if label == "" {
		label = unknownLabel
	}
	return dimensionRef{key: *id, id: id, label: label}
}

func endpointDimension(row store.RequestUsageDailySummary) dimensionRef {
	key := normalizeSummaryValue(row.Endpoint)
	if key == "" {
		key = unknownLabel
	}
	return dimensionRef{key: key, label: key}
}

func errorTypeDimension(row store.RequestUsageDailySummary) dimensionRef {
	key := normalizeSummaryValue(row.ErrorType)
	if key == "" {
		key = unknownLabel
	}
	return dimensionRef{key: key, label: key}
}

type breakdownAggregate struct {
	ref     dimensionRef
	metrics usageMetrics
}

func addBreakdownAggregate(aggregates map[string]*breakdownAggregate, ref dimensionRef, row store.RequestUsageDailySummary) {
	aggregate := aggregates[ref.key]
	if aggregate == nil {
		aggregate = &breakdownAggregate{ref: ref}
		aggregates[ref.key] = aggregate
	}
	if aggregate.ref.label == unknownLabel && ref.label != unknownLabel {
		aggregate.ref.label = ref.label
	}
	if aggregate.ref.id == nil && ref.id != nil {
		aggregate.ref.id = ref.id
	}
	aggregate.metrics.add(row)
}

func breakdownItems(aggregates map[string]*breakdownAggregate) []BreakdownItem {
	rows := make([]breakdownAggregate, 0, len(aggregates))
	for _, aggregate := range aggregates {
		rows = append(rows, *aggregate)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].metrics.cost != rows[j].metrics.cost {
			return rows[i].metrics.cost > rows[j].metrics.cost
		}
		if rows[i].metrics.requests != rows[j].metrics.requests {
			return rows[i].metrics.requests > rows[j].metrics.requests
		}
		return rows[i].ref.label < rows[j].ref.label
	})
	if len(rows) > breakdownLimit {
		rows = rows[:breakdownLimit]
	}
	items := make([]BreakdownItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, BreakdownItem{
			Key:              row.ref.key,
			ID:               row.ref.id,
			Label:            row.ref.label,
			Requests:         row.metrics.requests,
			SuccessCount:     row.metrics.successCount,
			FailureCount:     row.metrics.failureCount,
			SuccessRate:      row.metrics.successRate(),
			PromptTokens:     row.metrics.promptTokens,
			CompletionTokens: row.metrics.completionTokens,
			CachedTokens:     row.metrics.cachedTokens,
			TotalTokens:      row.metrics.totalTokens,
			Cost:             row.metrics.cost,
			AvgLatencyMS:     row.metrics.avgLatencyMS(),
			MaxLatencyMS:     row.metrics.latencyMaxMS,
		})
	}
	return items
}

type matrixAggregate struct {
	site        dimensionRef
	model       dimensionRef
	requests    int64
	totalTokens int64
	cost        float64
}

func topDimensionKeys(costByKey map[string]float64, labelByKey map[string]string, limit int) map[string]bool {
	type entry struct {
		key   string
		cost  float64
		label string
	}
	entries := make([]entry, 0, len(costByKey))
	for key, cost := range costByKey {
		entries = append(entries, entry{key: key, cost: cost, label: labelByKey[key]})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].cost != entries[j].cost {
			return entries[i].cost > entries[j].cost
		}
		return entries[i].label < entries[j].label
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	result := map[string]bool{}
	for _, item := range entries {
		result[item.key] = true
	}
	return result
}

func buildBreakdowns(rows []store.RequestUsageDailySummary, apiKeys map[uuid.UUID]store.APIKey) UsageBreakdowns {
	bySite := map[string]*breakdownAggregate{}
	byModel := map[string]*breakdownAggregate{}
	byAPIKey := map[string]*breakdownAggregate{}
	matrixCells := map[string]*matrixAggregate{}
	siteCost := map[string]float64{}
	modelCost := map[string]float64{}
	siteLabel := map[string]string{}
	modelLabel := map[string]string{}

	for _, row := range rows {
		site := siteDimension(row)
		model := modelDimension(row)
		addBreakdownAggregate(bySite, site, row)
		addBreakdownAggregate(byModel, model, row)
		addBreakdownAggregate(byAPIKey, apiKeyDimension(row, apiKeys), row)
		siteCost[site.key] += row.EstimatedCost
		modelCost[model.key] += row.EstimatedCost
		if _, ok := siteLabel[site.key]; !ok {
			siteLabel[site.key] = site.label
		}
		if _, ok := modelLabel[model.key]; !ok {
			modelLabel[model.key] = model.label
		}
		cellKey := site.key + "\x00" + model.key
		cell := matrixCells[cellKey]
		if cell == nil {
			cell = &matrixAggregate{site: site, model: model}
			matrixCells[cellKey] = cell
		}
		cell.requests += row.RequestCount
		cell.totalTokens += row.TotalTokens
		cell.cost += row.EstimatedCost
	}

	topSites := topDimensionKeys(siteCost, siteLabel, matrixTopN)
	topModels := topDimensionKeys(modelCost, modelLabel, matrixTopN)
	cells := make([]matrixAggregate, 0, len(matrixCells))
	for _, cell := range matrixCells {
		if !topSites[cell.site.key] || !topModels[cell.model.key] {
			continue
		}
		cells = append(cells, *cell)
	}
	sort.SliceStable(cells, func(i, j int) bool {
		if cells[i].cost != cells[j].cost {
			return cells[i].cost > cells[j].cost
		}
		if cells[i].requests != cells[j].requests {
			return cells[i].requests > cells[j].requests
		}
		if cells[i].site.label != cells[j].site.label {
			return cells[i].site.label < cells[j].site.label
		}
		return cells[i].model.label < cells[j].model.label
	})
	matrix := make([]MatrixItem, 0, len(cells))
	for _, cell := range cells {
		matrix = append(matrix, MatrixItem{
			Site:        MatrixDimension{Key: cell.site.key, ID: cell.site.id, Label: cell.site.label},
			Model:       MatrixDimension{Key: cell.model.key, ID: cell.model.id, Label: cell.model.label},
			Requests:    cell.requests,
			TotalTokens: cell.totalTokens,
			Cost:        cell.cost,
		})
	}

	return UsageBreakdowns{
		Site:   breakdownItems(bySite),
		Model:  breakdownItems(byModel),
		APIKey: breakdownItems(byAPIKey),
		Matrix: matrix,
	}
}

type seriesAggregate struct {
	ref      dimensionRef
	cost     float64
	requests int64
	points   map[string]*usageMetrics
}

func seriesDimensionFunc(groupBy string, apiKeys map[uuid.UUID]store.APIKey) func(store.RequestUsageDailySummary) dimensionRef {
	switch groupBy {
	case GroupBySite:
		return siteDimension
	case GroupBySiteModel:
		return siteModelDimension
	case GroupByAPIKey:
		return func(row store.RequestUsageDailySummary) dimensionRef { return apiKeyDimension(row, apiKeys) }
	case GroupByEndpoint:
		return endpointDimension
	case GroupByErrorType:
		return errorTypeDimension
	default:
		return modelDimension
	}
}

func buildSeries(rows []store.RequestUsageDailySummary, groupBy string, apiKeys map[uuid.UUID]store.APIKey, timeZone config.TimeZone, granularity string) []UsageSeries {
	dateFormat := "2006-01-02"
	if granularity == "hour" {
		dateFormat = "2006-01-02 15:00"
	}
	if groupBy == GroupByNone {
		points := map[string]*usageMetrics{}
		for _, row := range rows {
			addSeriesPoint(points, timeZone, row, dateFormat)
		}
		return []UsageSeries{{
			Key:    totalSeriesKey,
			Label:  totalSeriesLabel,
			Points: seriesPoints(points),
		}}
	}

	dimensionOf := seriesDimensionFunc(groupBy, apiKeys)
	members := map[string]*seriesAggregate{}
	for _, row := range rows {
		ref := dimensionOf(row)
		member := members[ref.key]
		if member == nil {
			member = &seriesAggregate{ref: ref, points: map[string]*usageMetrics{}}
			members[ref.key] = member
		}
		if member.ref.label == unknownLabel && ref.label != unknownLabel {
			member.ref.label = ref.label
		}
		member.cost += row.EstimatedCost
		member.requests += row.RequestCount
		addSeriesPoint(member.points, timeZone, row, dateFormat)
	}

	ordered := make([]seriesAggregate, 0, len(members))
	for _, member := range members {
		ordered = append(ordered, *member)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].cost != ordered[j].cost {
			return ordered[i].cost > ordered[j].cost
		}
		if ordered[i].requests != ordered[j].requests {
			return ordered[i].requests > ordered[j].requests
		}
		return ordered[i].ref.label < ordered[j].ref.label
	})

	series := make([]UsageSeries, 0, len(ordered)+1)
	kept := ordered
	if len(kept) > seriesTopN {
		kept = ordered[:seriesTopN]
	}
	for _, member := range kept {
		series = append(series, UsageSeries{
			Key:    member.ref.key,
			ID:     member.ref.id,
			Label:  member.ref.label,
			Points: seriesPoints(member.points),
		})
	}
	if len(ordered) > seriesTopN {
		otherPoints := map[string]*usageMetrics{}
		for _, member := range ordered[seriesTopN:] {
			for date, metrics := range member.points {
				point := otherPoints[date]
				if point == nil {
					point = &usageMetrics{}
					otherPoints[date] = point
				}
				point.addMetrics(*metrics)
			}
		}
		series = append(series, UsageSeries{
			Key:    otherSeriesKey,
			Label:  otherSeriesLabel,
			Points: seriesPoints(otherPoints),
		})
	}
	return series
}

func addSeriesPoint(points map[string]*usageMetrics, timeZone config.TimeZone, row store.RequestUsageDailySummary, dateFormat string) {
	date := timeZone.Format(row.BucketStart, dateFormat)
	point := points[date]
	if point == nil {
		point = &usageMetrics{}
		points[date] = point
	}
	point.add(row)
}

func seriesPoints(points map[string]*usageMetrics) []UsageSeriesPoint {
	dates := make([]string, 0, len(points))
	for date := range points {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	result := make([]UsageSeriesPoint, 0, len(dates))
	for _, date := range dates {
		metrics := points[date]
		result = append(result, UsageSeriesPoint{
			Date:             date,
			Requests:         metrics.requests,
			SuccessCount:     metrics.successCount,
			FailureCount:     metrics.failureCount,
			PromptTokens:     metrics.promptTokens,
			CompletionTokens: metrics.completionTokens,
			CachedTokens:     metrics.cachedTokens,
			TotalTokens:      metrics.totalTokens,
			Cost:             metrics.cost,
			AvgLatencyMS:     metrics.avgLatencyMS(),
			MaxLatencyMS:     metrics.latencyMaxMS,
		})
	}
	return result
}

// buildAPIKeyContributions 生成按天×API Key 聚合的时序数据，用于热力图（365 天窗口）。
func buildAPIKeyContributions(
	rows []store.RequestUsageDailySummary,
	apiKeys map[uuid.UUID]store.APIKey,
	timeZone config.TimeZone,
	rangeFrom time.Time,
) []DailyAPIKeyUsagePoint {
	type aggregate struct {
		date       string
		apiKeyID   uuid.UUID
		apiKeyName string
		tokens     int64
		cost       float64
		currency   string
	}
	byKey := map[string]*aggregate{}
	for _, row := range rows {
		if !row.APIKeyID.Valid || row.BucketStart.Before(rangeFrom) {
			continue
		}
		date := timeZone.Format(row.BucketStart, "2006-01-02")
		apiKeyID := row.APIKeyID.UUID
		currency := strings.TrimSpace(row.Currency)
		if currency == "" {
			currency = defaultCurrency
		}
		currency = strings.ToUpper(currency)
		key := date + "\x00" + apiKeyID.String() + "\x00" + currency
		item := byKey[key]
		if item == nil {
			name := strings.TrimSpace(row.APIKeyName)
			if apiKey, ok := apiKeys[apiKeyID]; ok {
				if n := strings.TrimSpace(apiKey.Name); n != "" {
					name = n
				}
			}
			if name == "" {
				name = unknownLabel
			}
			item = &aggregate{
				date:       date,
				apiKeyID:   apiKeyID,
				apiKeyName: name,
				currency:   currency,
			}
			byKey[key] = item
		}
		item.tokens += row.TotalTokens
		item.cost += row.EstimatedCost
	}
	out := make([]DailyAPIKeyUsagePoint, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, DailyAPIKeyUsagePoint{
			Date:        item.date,
			APIKeyID:    item.apiKeyID.String(),
			APIKeyName:  item.apiKeyName,
			TotalTokens: item.tokens,
			Cost:        item.cost,
			Currency:    item.currency,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].TotalTokens > out[j].TotalTokens
	})
	return out
}
