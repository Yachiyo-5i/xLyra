package config

func Defaults() map[string]any {
	return map[string]any{
		"global": map[string]any{
			"general": map[string]any{
				"tasks": map[string]any{
					"site_refresh_cron":   "0 */15 * * *",
					"newapi_checkin_cron": "0 9 * * *",
				},
				"ip_whitelist": map[string]any{
					"enabled": false,
					"entries": []string{},
				},
				"log": map[string]any{
					"level":           "info",
					"cleanup_enabled": true,
					"retention_days":  30,
				},
				"data": map[string]any{
					"request_detail_cleanup_enabled": true,
					"request_detail_retention_days":  90,
				},
				"security": map[string]any{
					"session_lifetime_hours": 24,
				},
			},
		},
		"network": map[string]any{
			"proxies": []map[string]any{},
		},
		"notification": map[string]any{},
	}
}
