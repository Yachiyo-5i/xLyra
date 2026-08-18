package analytics

type UsageAnalytics struct {
	Meta                UsageMeta               `json:"meta"`
	Totals              UsageTotals             `json:"totals"`
	Breakdowns          UsageBreakdowns         `json:"breakdowns"`
	Series              []UsageSeries           `json:"series"`
	APIKeyContributions []DailyAPIKeyUsagePoint `json:"api_key_contributions"`
}

type AnalyticsOptions struct {
	Sites   []AnalyticsOption `json:"sites"`
	APIKeys []AnalyticsOption `json:"api_keys"`
}

type AnalyticsOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type APIKeyContributions struct {
	GeneratedAt string                  `json:"generated_at"`
	From        string                  `json:"from"`
	To          string                  `json:"to"`
	Points      []DailyAPIKeyUsagePoint `json:"points"`
}

type UsageDataset struct {
	Meta     UsageDatasetMeta `json:"meta"`
	Current  []UsageFact      `json:"current"`
	Previous []UsageFact      `json:"previous"`
}

type UsageDatasetMeta struct {
	From         string  `json:"from"`
	To           string  `json:"to"`
	PreviousFrom string  `json:"previous_from"`
	PreviousTo   string  `json:"previous_to"`
	Days         int     `json:"days"`
	Timezone     string  `json:"timezone"`
	GeneratedAt  string  `json:"generated_at"`
	DataFrom     *string `json:"data_from"`
	Granularity  string  `json:"granularity"`
	FactCount    int     `json:"fact_count"`
	FactLimit    int     `json:"fact_limit"`
}

type UsageFact struct {
	Date                   string  `json:"date"`
	SiteKey                string  `json:"site_key"`
	SiteID                 *string `json:"site_id"`
	SiteLabel              string  `json:"site_label"`
	ModelKey               string  `json:"model_key"`
	ModelID                *string `json:"model_id"`
	ModelLabel             string  `json:"model_label"`
	APIKeyKey              string  `json:"api_key_key"`
	APIKeyID               *string `json:"api_key_id"`
	APIKeyLabel            string  `json:"api_key_label"`
	Currency               string  `json:"currency"`
	Requests               int64   `json:"requests"`
	SuccessCount           int64   `json:"success_count"`
	FailureCount           int64   `json:"failure_count"`
	PromptTokens           int64   `json:"prompt_tokens"`
	CompletionTokens       int64   `json:"completion_tokens"`
	CachedTokens           int64   `json:"cached_tokens"`
	TotalTokens            int64   `json:"total_tokens"`
	Cost                   float64 `json:"cost"`
	LatencyCount           int64   `json:"latency_count"`
	LatencyTotalMS         int64   `json:"latency_total_ms"`
	LatencyMaxMS           int64   `json:"latency_max_ms"`
	UpstreamLatencyCount   int64   `json:"upstream_latency_count"`
	UpstreamLatencyTotalMS int64   `json:"upstream_latency_total_ms"`
}

type UsageMeta struct {
	From                string       `json:"from"`
	To                  string       `json:"to"`
	Days                int          `json:"days"`
	Timezone            string       `json:"timezone"`
	GeneratedAt         string       `json:"generated_at"`
	GroupBy             string       `json:"group_by"`
	Currency            string       `json:"currency"`
	AvailableCurrencies []string     `json:"available_currencies"`
	Filters             UsageFilters `json:"filters"`
	DataFrom            *string      `json:"data_from"`
	Granularity         string       `json:"granularity"`
}

type UsageFilters struct {
	SiteIDs   []string `json:"site_ids"`
	ModelKeys []string `json:"model_keys"`
	APIKeyIDs []string `json:"api_key_ids"`
	Success   *bool    `json:"success"`
}

type UsageTotals struct {
	Requests             int64              `json:"requests"`
	SuccessCount         int64              `json:"success_count"`
	FailureCount         int64              `json:"failure_count"`
	SuccessRate          *float64           `json:"success_rate"`
	PromptTokens         int64              `json:"prompt_tokens"`
	CompletionTokens     int64              `json:"completion_tokens"`
	CachedTokens         int64              `json:"cached_tokens"`
	TotalTokens          int64              `json:"total_tokens"`
	CacheHitRate         *float64           `json:"cache_hit_rate"`
	Cost                 float64            `json:"cost"`
	CostByCurrency       map[string]float64 `json:"cost_by_currency"`
	AvgLatencyMS         float64            `json:"avg_latency_ms"`
	MaxLatencyMS         int64              `json:"max_latency_ms"`
	AvgUpstreamLatencyMS float64            `json:"avg_upstream_latency_ms"`
	PreviousPeriod       PreviousPeriod     `json:"previous_period"`
}

type PreviousPeriod struct {
	From         string   `json:"from"`
	To           string   `json:"to"`
	Requests     int64    `json:"requests"`
	SuccessRate  *float64 `json:"success_rate"`
	TotalTokens  int64    `json:"total_tokens"`
	Cost         float64  `json:"cost"`
	AvgLatencyMS float64  `json:"avg_latency_ms"`
}

type UsageBreakdowns struct {
	Site   []BreakdownItem `json:"site"`
	Model  []BreakdownItem `json:"model"`
	APIKey []BreakdownItem `json:"api_key"`
	Matrix []MatrixItem    `json:"matrix"`
}

type BreakdownItem struct {
	Key              string   `json:"key"`
	ID               *string  `json:"id"`
	Label            string   `json:"label"`
	Requests         int64    `json:"requests"`
	SuccessCount     int64    `json:"success_count"`
	FailureCount     int64    `json:"failure_count"`
	SuccessRate      *float64 `json:"success_rate"`
	PromptTokens     int64    `json:"prompt_tokens"`
	CompletionTokens int64    `json:"completion_tokens"`
	CachedTokens     int64    `json:"cached_tokens"`
	TotalTokens      int64    `json:"total_tokens"`
	Cost             float64  `json:"cost"`
	AvgLatencyMS     float64  `json:"avg_latency_ms"`
	MaxLatencyMS     int64    `json:"max_latency_ms"`
}

type MatrixDimension struct {
	Key   string  `json:"key"`
	ID    *string `json:"id"`
	Label string  `json:"label"`
}

type MatrixItem struct {
	Site        MatrixDimension `json:"site"`
	Model       MatrixDimension `json:"model"`
	Requests    int64           `json:"requests"`
	TotalTokens int64           `json:"total_tokens"`
	Cost        float64         `json:"cost"`
}

type UsageSeries struct {
	Key    string             `json:"key"`
	ID     *string            `json:"id"`
	Label  string             `json:"label"`
	Points []UsageSeriesPoint `json:"points"`
}

type UsageSeriesPoint struct {
	Date             string  `json:"date"`
	Requests         int64   `json:"requests"`
	SuccessCount     int64   `json:"success_count"`
	FailureCount     int64   `json:"failure_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	Cost             float64 `json:"cost"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
	MaxLatencyMS     int64   `json:"max_latency_ms"`
}

// DailyAPIKeyUsagePoint 是按天聚合的单个 API Key 用量点，用于热力图。
type DailyAPIKeyUsagePoint struct {
	Date        string  `json:"date"`
	APIKeyID    string  `json:"api_key_id"`
	APIKeyName  string  `json:"api_key_name"`
	TotalTokens int64   `json:"total_tokens"`
	Cost        float64 `json:"cost"`
	Currency    string  `json:"currency"`
}
