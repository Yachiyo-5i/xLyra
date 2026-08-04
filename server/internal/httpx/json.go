package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrRequestBodyTooLarge is returned by DecodeJSONBody when the request body
// exceeds the configured size limit. Callers can detect it with errors.Is to
// respond with 413 instead of a generic 400.
var ErrRequestBodyTooLarge = errors.New("request body too large")

func DecodeJSONBody(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, ErrRequestBodyTooLarge) {
			return ErrRequestBodyTooLarge
		}
		if errors.Is(err, ErrRequestBodyEncoding) {
			return ErrRequestBodyEncoding
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return ErrRequestBodyTooLarge
		}
		return fmt.Errorf("decode json body: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode json body: request body must contain a single JSON value")
		}
		if errors.Is(err, ErrRequestBodyTooLarge) {
			return ErrRequestBodyTooLarge
		}
		if errors.Is(err, ErrRequestBodyEncoding) {
			return ErrRequestBodyEncoding
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return ErrRequestBodyTooLarge
		}
		return fmt.Errorf("decode json body: %w", err)
	}

	return nil
}
