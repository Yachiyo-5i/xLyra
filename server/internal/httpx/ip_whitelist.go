package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"xlyra/server/internal/config"
)

func IPWhitelist(confFile *config.ConfigFile) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			general := config.ReadGeneralConfig(confFile)
			if !general.IPWhitelist.Enabled {
				next.ServeHTTP(w, r)
				return
			}
			addr, ok := requestAddr(r)
			if !ok || !addrAllowed(addr, general.IPWhitelist.Entries) {
				Error(w, r, http.StatusForbidden, "ip_not_allowed", "request source ip is not allowed")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestAddr(r *http.Request) (netip.Addr, bool) {
	value := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func addrAllowed(addr netip.Addr, entries []string) bool {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		allowedAddr, err := netip.ParseAddr(entry)
		if err == nil && allowedAddr.Unmap() == addr {
			return true
		}
	}
	return false
}
