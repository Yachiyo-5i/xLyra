package httpx

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

func WithRequestIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestID := middleware.GetReqID(r.Context()); requestID != "" {
			w.Header().Set("X-Request-ID", requestID)
		}

		next.ServeHTTP(w, r)
	})
}

// LimitRequestBody caps the size of the request body to maxBytes. A declared
// Content-Length over the limit is rejected up front with 413 without reading
// the body; otherwise the body is wrapped with http.MaxBytesReader so an
// oversized (chunked or under-declared) stream is truncated and surfaced as an
// error during decoding rather than being buffered into memory. maxBytes <= 0
// disables the limit.
func LimitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxBytes > 0 {
				if r.ContentLength > maxBytes {
					Error(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "request body exceeds the maximum allowed size")
					return
				}
				if r.Body != nil {
					r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
