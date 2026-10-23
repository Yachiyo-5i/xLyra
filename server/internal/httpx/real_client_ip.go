package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

func RealClientIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip := realClientIP(r); ip != "" {
			r.RemoteAddr = ip
		}
		next.ServeHTTP(w, r)
	})
}

func realClientIP(r *http.Request) string {
	candidates := []string{
		r.Header.Get("CF-Connecting-IP"),
		r.Header.Get("True-Client-IP"),
		r.Header.Get("X-Real-IP"),
		firstForwardedIP(r.Header.Get("X-Forwarded-For")),
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		addr, err := netip.ParseAddr(candidate)
		if err != nil || !addr.IsValid() {
			continue
		}
		return candidate
	}
	return ""
}

func firstForwardedIP(value string) string {
	if value == "" {
		return ""
	}
	first, _, _ := strings.Cut(value, ",")
	return first
}
