package adapter

import (
	"io"
	"net/http"
	"strings"
)

type adapterRoundTripFunc func(*http.Request) (*http.Response, error)

func (f adapterRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func adapterTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
