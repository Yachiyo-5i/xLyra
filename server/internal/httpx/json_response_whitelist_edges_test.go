package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"xlyra/server/internal/config"
)

func TestDecodeJSONBodyRejectsMalformedAndEmptyBodies(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"ok":`},
		{name: "empty", body: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			var payload map[string]any
			err := DecodeJSONBody(req, &payload)
			if err == nil {
				t.Fatal("expected decode error")
			}
			if !strings.Contains(err.Error(), "decode json body:") {
				t.Fatalf("error = %q, want decode json body prefix", err.Error())
			}
		})
	}
}

func TestJSONWritesStatusContentTypeAndBody(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()

	JSON(rec, http.StatusAccepted, map[string]string{"status": "queued"})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "queued" {
		t.Fatalf("body = %#v", body)
	}
}

func TestIPWhitelistAllowsRequestsWhenDisabled(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.DefaultGeneralConfig()
	cfg.IPWhitelist.Enabled = false
	cfg.IPWhitelist.Entries = []string{"10.0.0.0/24"}
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(cfg)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	handler := IPWhitelist(confFile)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-a-valid-ip"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestRequestAddrParsesPlainHostPortAndIPv4MappedAddresses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{name: "plain address", remoteAddr: "  192.0.2.10  ", want: "192.0.2.10"},
		{name: "host port", remoteAddr: "192.0.2.11:443", want: "192.0.2.11"},
		{name: "mapped host port", remoteAddr: "[::ffff:192.0.2.12]:443", want: "192.0.2.12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr

			got, ok := requestAddr(req)

			if !ok {
				t.Fatal("expected remote address to parse")
			}
			if got.String() != tc.want {
				t.Fatalf("addr = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRequestAddrRejectsInvalidRemoteAddr(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "not-a-valid-ip"

	if got, ok := requestAddr(req); ok {
		t.Fatalf("expected invalid remote address to be rejected, got %s", got)
	}
}

func TestAddrAllowedTrimsEntriesIgnoresInvalidAndUnmapsIPv4(t *testing.T) {
	t.Parallel()

	addr := netip.MustParseAddr("192.0.2.10")

	if !addrAllowed(addr, []string{" ", "not-a-prefix", " ::ffff:192.0.2.10 "}) {
		t.Fatal("expected exact IPv4-mapped entry to allow address")
	}
	if addrAllowed(addr, []string{"not-a-prefix", "198.51.100.0/24"}) {
		t.Fatal("expected invalid and non-matching entries to reject address")
	}
}
