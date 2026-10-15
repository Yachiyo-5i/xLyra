package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xlyra/server/internal/jsplugin"
)

func TestDoHTTPSendsTheRequestAndKeepsTheKeyOutOfErrors(t *testing.T) {
	var method, path, body, contentType, host string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		method, path, body, contentType, host = r.Method, r.URL.Path, string(raw), r.Header.Get("Content-Type"), r.Header.Get("X-Forwarded-For")
		if strings.Contains(r.URL.Path, "bad") {
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	host2 := automationHost{http: server.Client()}

	status, err := host2.DoHTTP(context.Background(), jsplugin.HTTPRequest{
		Method: "POST", URL: server.URL + "/devicekey", Body: `{"a":1}`,
		Headers: map[string]string{"Content-Type": "application/json", "X-Forwarded-For": "1.2.3.4", "Host": "evil"},
	})
	if err != nil || status != 200 {
		t.Fatalf("status = %d err = %v", status, err)
	}
	if method != "POST" || path != "/devicekey" || body != `{"a":1}` || contentType != "application/json" || host != "" {
		t.Fatalf("method=%q path=%q body=%q type=%q forwarded=%q", method, path, body, contentType, host)
	}

	if status, err := host2.DoHTTP(context.Background(), jsplugin.HTTPRequest{Method: "GET", URL: server.URL + "/badkey", Body: "ignored"}); err != nil || status != 400 {
		t.Fatalf("an error status is for the caller to judge: status = %d err = %v", status, err)
	}
	server.Close()
	if _, err := host2.DoHTTP(context.Background(), jsplugin.HTTPRequest{Method: "POST", URL: server.URL + "/secretkey"}); err == nil || strings.Contains(err.Error(), "secretkey") {
		t.Fatalf("a network failure leaked the key: err = %v", err)
	}
	for _, bad := range []string{"", "ftp://x/y", "api.day.app/key", "https://user:pw@host/key"} {
		if _, err := host2.DoHTTP(context.Background(), jsplugin.HTTPRequest{Method: "POST", URL: bad}); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestDoHTTPDoesNotFollowRedirectsToAnotherAddress(t *testing.T) {
	var elsewhereHits int
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhereHits++ }))
	defer elsewhere.Close()
	var finalBody string
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, elsewhere.URL+"/stolen", http.StatusTemporaryRedirect)
		case "/inside":
			http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
		case "/final":
			raw, _ := io.ReadAll(r.Body)
			finalBody = string(raw)
		}
	}))
	defer home.Close()
	host := automationHost{http: home.Client()}

	_, err := host.DoHTTP(context.Background(), jsplugin.HTTPRequest{Method: "POST", URL: home.URL + "/away", Body: "secret"})
	if err == nil || strings.Contains(err.Error(), "stolen") || strings.Contains(err.Error(), elsewhere.URL) {
		t.Fatalf("a redirect to another address: err = %v", err)
	}
	if elsewhereHits != 0 {
		t.Fatalf("the request followed the redirect and reached the other address %d time(s)", elsewhereHits)
	}

	status, err := host.DoHTTP(context.Background(), jsplugin.HTTPRequest{Method: "POST", URL: home.URL + "/inside", Body: "kept"})
	if err != nil || status != 200 || finalBody != "kept" {
		t.Fatalf("a redirect inside the same address: status = %d err = %v body = %q", status, err, finalBody)
	}
}
