package adapter

import "net/http"

func httpClientForSite(site SiteConfig, fallback *http.Client) *http.Client {
	if site.Client != nil {
		return site.Client
	}
	if fallback != nil {
		return fallback
	}
	return http.DefaultClient
}
