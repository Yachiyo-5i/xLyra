package config

import "fmt"

const PortalConfigPath = "global.portal"

type PortalDimensions struct {
	Site     bool `json:"site"`
	Model    bool `json:"model"`
	Tokens   bool `json:"tokens"`
	Cost     bool `json:"cost"`
	Latency  bool `json:"latency"`
	Endpoint bool `json:"endpoint"`
	Upstream bool `json:"upstream"`
	Error    bool `json:"error"`
}

type PortalConfig struct {
	Enabled            bool             `json:"enabled"`
	ShowRequests       bool             `json:"show_requests"`
	ShowSummary        bool             `json:"show_summary"`
	SummaryDays        int              `json:"summary_days"`
	RequestPageSizeMax int              `json:"request_page_size_max"`
	Dimensions         PortalDimensions `json:"dimensions"`
}

func DefaultPortalConfig() PortalConfig {
	return PortalConfig{
		Enabled:            false,
		ShowRequests:       true,
		ShowSummary:        true,
		SummaryDays:        14,
		RequestPageSizeMax: 50,
		Dimensions: PortalDimensions{
			Site:     false,
			Model:    true,
			Tokens:   true,
			Cost:     true,
			Latency:  true,
			Endpoint: true,
			Upstream: false,
			Error:    true,
		},
	}
}

func ReadPortalConfig(confFile *ConfigFile) PortalConfig {
	if confFile == nil {
		return DefaultPortalConfig()
	}
	raw, ok := confFile.Get(PortalConfigPath)
	if !ok || raw == nil {
		return DefaultPortalConfig()
	}
	return PortalConfigFromRaw(raw)
}

func PortalConfigFromRaw(raw any) PortalConfig {
	cfg := DefaultPortalConfig()
	root, ok := raw.(map[string]any)
	if !ok {
		return cfg
	}

	cfg.Enabled = boolFromMap(root, "enabled", cfg.Enabled)
	cfg.ShowRequests = boolFromMap(root, "show_requests", cfg.ShowRequests)
	cfg.ShowSummary = boolFromMap(root, "show_summary", cfg.ShowSummary)
	cfg.SummaryDays = intFromMap(root, "summary_days", cfg.SummaryDays)
	cfg.RequestPageSizeMax = intFromMap(root, "request_page_size_max", cfg.RequestPageSizeMax)

	if dimensions, ok := root["dimensions"].(map[string]any); ok {
		cfg.Dimensions.Site = boolFromMap(dimensions, "site", cfg.Dimensions.Site)
		cfg.Dimensions.Model = boolFromMap(dimensions, "model", cfg.Dimensions.Model)
		cfg.Dimensions.Tokens = boolFromMap(dimensions, "tokens", cfg.Dimensions.Tokens)
		cfg.Dimensions.Cost = boolFromMap(dimensions, "cost", cfg.Dimensions.Cost)
		cfg.Dimensions.Latency = boolFromMap(dimensions, "latency", cfg.Dimensions.Latency)
		cfg.Dimensions.Endpoint = boolFromMap(dimensions, "endpoint", cfg.Dimensions.Endpoint)
		cfg.Dimensions.Upstream = boolFromMap(dimensions, "upstream", cfg.Dimensions.Upstream)
		cfg.Dimensions.Error = boolFromMap(dimensions, "error", cfg.Dimensions.Error)
	}

	return NormalizePortalConfig(cfg)
}

func NormalizePortalConfig(cfg PortalConfig) PortalConfig {
	defaults := DefaultPortalConfig()
	if cfg.SummaryDays == 0 {
		cfg.SummaryDays = defaults.SummaryDays
	}
	if cfg.RequestPageSizeMax == 0 {
		cfg.RequestPageSizeMax = defaults.RequestPageSizeMax
	}
	return cfg
}

func ValidatePortalConfig(cfg PortalConfig) error {
	if cfg.SummaryDays < 1 || cfg.SummaryDays > 90 {
		return fmt.Errorf("summary_days must be between 1 and 90")
	}
	if cfg.RequestPageSizeMax < 1 || cfg.RequestPageSizeMax > 200 {
		return fmt.Errorf("request_page_size_max must be between 1 and 200")
	}
	return nil
}

func PortalConfigToMap(cfg PortalConfig) map[string]any {
	return map[string]any{
		"enabled":               cfg.Enabled,
		"show_requests":         cfg.ShowRequests,
		"show_summary":          cfg.ShowSummary,
		"summary_days":          cfg.SummaryDays,
		"request_page_size_max": cfg.RequestPageSizeMax,
		"dimensions": map[string]any{
			"site":     cfg.Dimensions.Site,
			"model":    cfg.Dimensions.Model,
			"tokens":   cfg.Dimensions.Tokens,
			"cost":     cfg.Dimensions.Cost,
			"latency":  cfg.Dimensions.Latency,
			"endpoint": cfg.Dimensions.Endpoint,
			"upstream": cfg.Dimensions.Upstream,
			"error":    cfg.Dimensions.Error,
		},
	}
}
