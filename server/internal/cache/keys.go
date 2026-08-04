package cache

import "strings"

const defaultPrefix = "xlyra"

func AdminSession(sessionID string) string {
	return join("admin_session", sessionID)
}

func APIKey(prefix string) string {
	return join("api_key", prefix)
}

func SiteModels(siteID string) string {
	return join("site", siteID, "models")
}

func SiteBalance(siteID string) string {
	return join("site", siteID, "balance")
}

func HealthCooldown(siteID string, siteModelID string) string {
	return join("health_cooldown", siteID, siteModelID)
}

func join(parts ...string) string {
	all := append([]string{defaultPrefix}, parts...)
	return strings.Join(all, ":")
}
