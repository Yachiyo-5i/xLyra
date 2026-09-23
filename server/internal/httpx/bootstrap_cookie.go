package httpx

import (
	"net/http"
	"strings"
)

const BootstrapInitializedCookie = "xlyra_admin_initialized"

func SetBootstrapInitializedCookie(w http.ResponseWriter, r *http.Request, initialized bool) {
	value := "0"
	if initialized {
		value = "1"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     BootstrapInitializedCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: false,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func requestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on")
}
