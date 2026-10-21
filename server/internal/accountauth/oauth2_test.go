package accountauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	leakAccess  = "ACCESS_LEAK_9f3c"
	leakRefresh = "REFRESH_LEAK_9f3c"
	leakID      = "ID_LEAK_9f3c"
	leakDevice  = "DEVICE_LEAK_9f3c"
	leakUser    = "USER_LEAK_9f3c"
	leakDesc    = "DESC_LEAK_9f3c"
	leakBody    = "BODY_LEAK_9f3c"
)

func TestStartDeviceCodeSuccessAndNormalization(t *testing.T) {
	t.Parallel()
	type want struct {
		interval  int
		expiresIn int
	}
	cases := []struct {
		name   string
		body   string
		status int
		want   want
		fail   bool
		leaks  bool
	}{
		{
			name: "defaults",
			body: `{"device_code":"dev-1","user_code":"USER-1","verification_uri":"__VERIFY__"}`,
			want: want{interval: 5, expiresIn: 600},
		},
		{
			name: "zero interval and expires",
			body: `{"device_code":"dev-1","user_code":"USER-1","verification_uri":"__VERIFY__","interval":0,"expires_in":0}`,
			want: want{interval: 5, expiresIn: 600},
		},
		{
			name: "negative expires defaults",
			body: `{"device_code":"dev-1","user_code":"USER-1","verification_uri":"__VERIFY__","interval":1,"expires_in":-4}`,
			want: want{interval: 1, expiresIn: 600},
		},
		{
			name: "clamps upper bounds",
			body: `{"device_code":"dev-1","user_code":"USER-1","verification_uri":"__VERIFY__","verification_uri_complete":"__COMPLETE__","interval":90,"expires_in":10000}`,
			want: want{interval: 60, expiresIn: 1800},
		},
		{
			name: "keeps in-range values",
			body: `{"device_code":"dev-1","user_code":"USER-1","verification_uri":"__VERIFY__","interval":60,"expires_in":1800}`,
			want: want{interval: 60, expiresIn: 1800},
		},
		{
			name:  "missing device code",
			body:  `{"user_code":"` + leakUser + `","verification_uri":"__VERIFY__","device_code":"  "}`,
			fail:  true,
			leaks: true,
		},
		{
			name:  "missing user code",
			body:  `{"device_code":"` + leakDevice + `","verification_uri":"__VERIFY__"}`,
			fail:  true,
			leaks: true,
		},
		{
			name:  "missing verification uri",
			body:  `{"device_code":"` + leakDevice + `","user_code":"` + leakUser + `"}`,
			fail:  true,
			leaks: true,
		},
		{
			name:  "negative interval",
			body:  `{"device_code":"` + leakDevice + `","user_code":"` + leakUser + `","verification_uri":"__VERIFY__","interval":-1}`,
			fail:  true,
			leaks: true,
		},
		{
			name:  "string interval",
			body:  `{"device_code":"` + leakDevice + `","user_code":"` + leakUser + `","verification_uri":"__VERIFY__","interval":"fast"}`,
			fail:  true,
			leaks: true,
		},
		{
			name:  "fractional interval",
			body:  `{"device_code":"` + leakDevice + `","user_code":"` + leakUser + `","verification_uri":"__VERIFY__","interval":1.5}`,
			fail:  true,
			leaks: true,
		},
		{
			name:  "fractional expires",
			body:  `{"device_code":"` + leakDevice + `","user_code":"` + leakUser + `","verification_uri":"__VERIFY__","expires_in":1.5}`,
			fail:  true,
			leaks: true,
		},
		{
			name:   "empty body",
			status: http.StatusOK,
			body:   `   `,
			fail:   true,
		},
		{
			name:   "not json",
			status: http.StatusOK,
			body:   leakBody + " is not json",
			fail:   true,
			leaks:  true,
		},
		{
			name:   "server error",
			status: http.StatusBadGateway,
			body:   `{"error":"invalid_grant","error_description":"` + leakDesc + `","access_token":"` + leakAccess + `"}`,
			fail:   true,
			leaks:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var hits atomic.Int32
			var mu sync.Mutex
			var form url.Values
			var header http.Header
			var verify, complete string
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				raw, _ := io.ReadAll(r.Body)
				parsed, _ := url.ParseQuery(string(raw))
				mu.Lock()
				form = parsed
				header = r.Header.Clone()
				mu.Unlock()
				status := tc.status
				if status == 0 {
					status = http.StatusOK
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				body := strings.ReplaceAll(tc.body, "__VERIFY__", verify)
				body = strings.ReplaceAll(body, "__COMPLETE__", complete)
				_, _ = io.WriteString(w, body)
			}))
			verify = srv.URL + "/verify"
			complete = srv.URL + "/verify?user_code=USER-1"
			t.Cleanup(srv.Close)
			cfg := baseConfig(t, srv)
			cfg.ExtraHeaders = map[string]string{"User-Agent": "pool-agent", "Accept": "text/plain"}
			start, err := StartDeviceCode(context.Background(), cfg)
			if tc.fail {
				if err == nil {
					t.Fatal("expected error")
				}
				if start != (DeviceStart{}) {
					t.Fatalf("failed start returned %+v", start)
				}
				if tc.leaks {
					assertNoLeak(t, err.Error(), leakAccess, leakRefresh, leakID, leakDevice, leakUser, leakDesc, leakBody)
				}
				if tc.status >= 500 && !IsTransient(err) {
					t.Fatalf("5xx error = %v, want transient", err)
				}
				if tc.status >= 500 && errors.Is(err, ErrReauthorizationRequired) {
					t.Fatal("5xx must not require reauthorization")
				}
				return
			}
			if err != nil {
				t.Fatalf("StartDeviceCode: %v", err)
			}
			if hits.Load() != 1 {
				t.Fatalf("hits = %d", hits.Load())
			}
			if start.DeviceCode != "dev-1" || start.UserCode != "USER-1" {
				t.Fatalf("codes = %s %s", start.DeviceCode, start.UserCode)
			}
			if start.VerificationURI != verify {
				t.Fatalf("verification_uri = %s", start.VerificationURI)
			}
			if start.Interval != tc.want.interval || start.ExpiresIn != tc.want.expiresIn {
				t.Fatalf("interval/expires = %d/%d, want %d/%d", start.Interval, start.ExpiresIn, tc.want.interval, tc.want.expiresIn)
			}
			mu.Lock()
			gotForm := form
			gotHeader := header.Clone()
			mu.Unlock()
			if gotForm.Get("client_id") != "client-1" || gotForm.Get("scope") != "openid profile" || gotForm.Get("grant_type") != "" {
				t.Fatalf("form = %v", gotForm)
			}
			if gotHeader.Get("Content-Type") != contentTypeForm || gotHeader.Get("Accept") != acceptJSON {
				t.Fatalf("content-type=%q accept=%q", gotHeader.Get("Content-Type"), gotHeader.Get("Accept"))
			}
			if gotHeader.Get("User-Agent") != "pool-agent" || gotHeader.Get("Authorization") != "" {
				t.Fatalf("user-agent=%q authorization=%q", gotHeader.Get("User-Agent"), gotHeader.Get("Authorization"))
			}
		})
	}
}

func TestStartDeviceCodeRejectsVerificationURL(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"device_code":"%s","user_code":"%s","verification_uri":"http://127.0.0.1/ok","verification_uri_complete":"http://evil.test/activate?user_code=%s"}`, leakDevice, leakUser, leakUser)
	}))
	t.Cleanup(srv.Close)
	host := hostname(t, srv.URL)
	cfg := baseConfig(t, srv)
	cfg.CheckURL = func(raw string) error {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		if !strings.EqualFold(parsed.Hostname(), host) {
			return fmt.Errorf("rejected %s", raw)
		}
		return nil
	}
	start, err := StartDeviceCode(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected verification url rejection")
	}
	if start != (DeviceStart{}) {
		t.Fatalf("returned addresses: %+v", start)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
	assertNoLeak(t, err.Error(), leakDevice, leakUser, "evil.test")
	if !strings.Contains(err.Error(), "verification url rejected") {
		t.Fatalf("error = %v", err)
	}
}

func TestStartDeviceCodeAllowsEmptyCompleteURI(t *testing.T) {
	t.Parallel()
	var verify string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"device_code":"dev","user_code":"usr","verification_uri":"%s"}`, verify)
	}))
	verify = srv.URL + "/verify"
	t.Cleanup(srv.Close)
	var checked []string
	cfg := baseConfig(t, srv)
	allow := cfg.CheckURL
	cfg.CheckURL = func(raw string) error {
		checked = append(checked, raw)
		if raw == "" {
			return errors.New("empty url")
		}
		return allow(raw)
	}
	start, err := StartDeviceCode(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartDeviceCode: %v", err)
	}
	if start.VerificationURIComplete != "" {
		t.Fatalf("complete = %q", start.VerificationURIComplete)
	}
	for _, raw := range checked {
		if raw == "" {
			t.Fatal("CheckURL was called with an empty url")
		}
	}
	if len(checked) != 2 {
		t.Fatalf("CheckURL calls = %v", checked)
	}
}

func TestPollDeviceCodeSequence(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var hits int
	var forms []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(raw))
		mu.Lock()
		hits++
		n := hits
		forms = append(forms, values)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch n {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"slow_down","interval":1}`)
		default:
			fmt.Fprintf(w, `{"access_token":"%s","refresh_token":"%s","id_token":"%s","token_type":"Bearer","scope":"openid","expires_in":60}`, leakAccess, leakRefresh, leakID)
		}
	}))
	t.Cleanup(srv.Close)
	cfg := baseConfig(t, srv)
	cfg.GrantType = ""
	before := time.Now()
	pending, err := PollDeviceCode(context.Background(), cfg, "device-1", 5)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if pending.Status != PollPending || pending.Interval != 5 || pending.Tokens != nil {
		t.Fatalf("pending = %+v", pending)
	}
	slow, err := PollDeviceCode(context.Background(), cfg, "device-1", pending.Interval)
	if err != nil {
		t.Fatalf("slow_down: %v", err)
	}
	if slow.Status != PollSlowDown || slow.Interval != 10 || slow.Tokens != nil {
		t.Fatalf("slow_down = %+v", slow)
	}
	done, err := PollDeviceCode(context.Background(), cfg, "device-1", slow.Interval)
	after := time.Now()
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if done.Status != PollComplete || done.Interval != 10 || done.Tokens == nil {
		t.Fatalf("complete = %+v", done.Status)
	}
	if done.Tokens.AccessToken != leakAccess || done.Tokens.RefreshToken != leakRefresh || done.Tokens.IDToken != leakID {
		t.Fatal("token fields mismatch")
	}
	if done.Tokens.TokenType != "Bearer" || done.Tokens.Scope != "openid" || done.Tokens.ExpiresIn != 60 {
		t.Fatalf("metadata = %+v", done.Tokens.TokenType)
	}
	if done.Tokens.ExpiresAt.Before(before.Add(60*time.Second)) || done.Tokens.ExpiresAt.After(after.Add(60*time.Second)) {
		t.Fatalf("expires_at = %s", done.Tokens.ExpiresAt)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(forms) != 3 {
		t.Fatalf("forms = %d", len(forms))
	}
	for _, form := range forms {
		if form.Get("grant_type") != defaultDeviceGrant || form.Get("device_code") != "device-1" || form.Get("client_id") != "client-1" {
			t.Fatalf("form = %v", form)
		}
		if form.Get("refresh_token") != "" || form.Get("scope") != "" {
			t.Fatalf("unexpected form fields = %v", form)
		}
	}
}

func TestPollDeviceCodeSlowDownCapsInterval(t *testing.T) {
	t.Parallel()
	srv := newJSONServer(t, http.StatusBadRequest, `{"error":"slow_down"}`)
	cfg := baseConfig(t, srv)
	cases := []struct {
		in, want int
	}{
		{in: 0, want: 10},
		{in: 5, want: 10},
		{in: 58, want: 60},
		{in: 60, want: 60},
		{in: 100, want: 60},
	}
	for _, tc := range cases {
		poll, err := PollDeviceCode(context.Background(), cfg, "device-1", tc.in)
		if err != nil {
			t.Fatalf("interval %d: %v", tc.in, err)
		}
		if poll.Status != PollSlowDown || poll.Interval != tc.want {
			t.Fatalf("interval %d -> %+v, want %d", tc.in, poll, tc.want)
		}
	}
	srvPending := newJSONServer(t, http.StatusBadRequest, `{"error":"authorization_pending"}`)
	cfg.TokenURL = srvPending.URL
	cfg.Client = srvPending.Client()
	cfg.CheckURL = allowHost(hostname(t, srvPending.URL))
	pending, err := PollDeviceCode(context.Background(), cfg, "device-1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != PollPending || pending.Interval != 60 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestPollDeviceCodeErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		status    int
		body      string
		sentinel  error
		transient bool
		code      string
	}{
		{name: "access denied", status: http.StatusBadRequest, body: `{"error":"access_denied","error_description":"` + leakDesc + `","device_code":"` + leakDevice + `"}`, sentinel: ErrAccessDenied, code: "access_denied"},
		{name: "expired", status: http.StatusBadRequest, body: `{"error":"expired_token","user_code":"` + leakUser + `"}`, sentinel: ErrDeviceCodeExpired, code: "expired_token"},
		{name: "unknown code", status: http.StatusBadRequest, body: `{"error":"server_error","error_description":"` + leakDesc + `"}`, code: "server_error"},
		{name: "unsafe code", status: http.StatusBadRequest, body: `{"error":"Not A Code","error_description":"` + leakDesc + `","access_token":"` + leakAccess + `"}`, code: "unknown"},
		{name: "not json", status: http.StatusBadRequest, body: leakBody + " still not json", code: "unknown"},
		{name: "empty", status: http.StatusBadRequest, body: "", code: "unknown"},
		{name: "empty 200", status: http.StatusOK, body: "", code: ""},
		{name: "server error", status: http.StatusServiceUnavailable, body: `{"error":"invalid_grant","error_description":"` + leakDesc + `","refresh_token":"` + leakRefresh + `"}`, transient: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newJSONServer(t, tc.status, tc.body)
			_, err := PollDeviceCode(context.Background(), baseConfig(t, srv), "device-1", 5)
			if err == nil {
				t.Fatal("expected error")
			}
			assertNoLeak(t, err.Error(), leakAccess, leakRefresh, leakID, leakDevice, leakUser, leakDesc, leakBody, "Not A Code")
			if tc.transient {
				if !IsTransient(err) {
					t.Fatalf("error = %v, want transient", err)
				}
				if errors.Is(err, ErrReauthorizationRequired) {
					t.Fatal("5xx must not require reauthorization")
				}
				return
			}
			if IsTransient(err) {
				t.Fatalf("error = %v, want a terminal error", err)
			}
			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Fatalf("error = %v, want %v", err, tc.sentinel)
			}
			if tc.code != "" {
				var oauthErr *OAuth2Error
				if !errors.As(err, &oauthErr) {
					t.Fatalf("error = %T %v, want *OAuth2Error", err, err)
				}
				if oauthErr.Code != tc.code || oauthErr.Status != tc.status {
					t.Fatalf("oauth error = %+v", oauthErr)
				}
			}
			if errors.Is(err, ErrReauthorizationRequired) {
				t.Fatal("poll must not require reauthorization")
			}
		})
	}
}

func TestRefreshOAuth2(t *testing.T) {
	t.Parallel()
	t.Run("success with rotated refresh token", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var form url.Values
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			parsed, _ := url.ParseQuery(string(raw))
			mu.Lock()
			form = parsed
			mu.Unlock()
			fmt.Fprintf(w, `{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","scope":"openid","expires_in":120}`)
		}))
		t.Cleanup(srv.Close)
		cfg := baseConfig(t, srv)
		cfg.GrantType = "custom-grant"
		before := time.Now()
		tokens, err := RefreshOAuth2(context.Background(), cfg, "old-refresh")
		after := time.Now()
		if err != nil {
			t.Fatalf("RefreshOAuth2: %v", err)
		}
		if tokens.AccessToken != "new-access" || tokens.RefreshToken != "new-refresh" || tokens.ExpiresIn != 120 {
			t.Fatal("unexpected tokens")
		}
		if tokens.ExpiresAt.Before(before.Add(120*time.Second)) || tokens.ExpiresAt.After(after.Add(120*time.Second)) {
			t.Fatalf("expires_at = %s", tokens.ExpiresAt)
		}
		mu.Lock()
		gotForm := form
		mu.Unlock()
		if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("refresh_token") != "old-refresh" || gotForm.Get("client_id") != "client-1" || gotForm.Get("scope") != "openid profile" {
			t.Fatalf("form = %v", gotForm)
		}
	})
	t.Run("success without a new refresh token", func(t *testing.T) {
		t.Parallel()
		srv := newJSONServer(t, http.StatusOK, `{"access_token":"new-access","expires_in":0,"token_type":"Bearer"}`)
		tokens, err := RefreshOAuth2(context.Background(), baseConfig(t, srv), "old-refresh")
		if err != nil {
			t.Fatalf("RefreshOAuth2: %v", err)
		}
		if tokens.RefreshToken != "" || tokens.AccessToken != "new-access" || tokens.ExpiresIn != 0 || !tokens.ExpiresAt.IsZero() {
			t.Fatal("missing refresh_token must stay empty and expiry must be zero")
		}
	})
	cases := []struct {
		name      string
		status    int
		body      string
		reauth    bool
		transient bool
		code      string
	}{
		{name: "invalid grant", status: http.StatusBadRequest, body: oauthLeak("invalid_grant"), reauth: true, code: "invalid_grant"},
		{name: "invalid client", status: http.StatusBadRequest, body: oauthLeak("invalid_client"), reauth: true, code: "invalid_client"},
		{name: "unauthorized client", status: http.StatusUnauthorized, body: oauthLeak("unauthorized_client"), reauth: true, code: "unauthorized_client"},
		{name: "uppercase grant", status: http.StatusBadRequest, body: oauthLeak("INVALID_GRANT"), reauth: true, code: "invalid_grant"},
		{name: "invalid token is not reauth", status: http.StatusUnauthorized, body: oauthLeak("invalid_token"), code: "invalid_token"},
		{name: "bare 401", status: http.StatusUnauthorized, body: `{"error_description":"` + leakDesc + `","access_token":"` + leakAccess + `"}`, code: "unknown"},
		{name: "server error", status: http.StatusBadGateway, body: oauthLeak("invalid_grant"), transient: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newJSONServer(t, tc.status, tc.body)
			_, err := RefreshOAuth2(context.Background(), baseConfig(t, srv), leakRefresh)
			if err == nil {
				t.Fatal("expected error")
			}
			assertNoLeak(t, err.Error(), leakAccess, leakRefresh, leakID, leakDevice, leakUser, leakDesc, leakBody)
			if tc.transient {
				if !IsTransient(err) || errors.Is(err, ErrReauthorizationRequired) {
					t.Fatalf("error = %v, want transient", err)
				}
				return
			}
			if IsTransient(err) {
				t.Fatalf("error = %v, want terminal", err)
			}
			if errors.Is(err, ErrReauthorizationRequired) != tc.reauth {
				t.Fatalf("reauth = %v, want %v (%v)", errors.Is(err, ErrReauthorizationRequired), tc.reauth, err)
			}
			var oauthErr *OAuth2Error
			if !errors.As(err, &oauthErr) {
				t.Fatalf("error = %T %v", err, err)
			}
			if oauthErr.Code != tc.code || oauthErr.Status != tc.status {
				t.Fatalf("oauth error = %+v", oauthErr)
			}
		})
	}
}

func TestRefreshNetworkErrorIsTransient(t *testing.T) {
	t.Parallel()
	cfg := OAuth2Config{
		TokenURL: "http://127.0.0.1/token",
		ClientID: "client-1",
		CheckURL: allowHost("127.0.0.1"),
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
		})},
	}
	_, err := RefreshOAuth2(context.Background(), cfg, leakRefresh)
	if err == nil || !IsTransient(err) || errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("error = %v, want transient network error", err)
	}
	assertNoLeak(t, err.Error(), leakRefresh)
}

func TestCheckURLRejectsBeforeRequest(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	cfg := baseConfig(t, srv)
	cfg.CheckURL = func(string) error { return errors.New("blocked") }
	ctx := context.Background()
	if _, err := StartDeviceCode(ctx, cfg); err == nil {
		t.Fatal("expected device url rejection")
	}
	if _, err := PollDeviceCode(ctx, cfg, "device", 5); err == nil {
		t.Fatal("expected token url rejection")
	}
	if _, err := RefreshOAuth2(ctx, cfg, "refresh"); err == nil {
		t.Fatal("expected refresh url rejection")
	}
	cfg.CheckURL = nil
	if _, err := RefreshOAuth2(ctx, cfg, "refresh"); err == nil || !strings.Contains(err.Error(), "CheckURL") {
		t.Fatalf("nil CheckURL error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d, want 0", hits.Load())
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	t.Parallel()
	var hits2 atomic.Int32
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits2.Add(1)
		_, _ = io.WriteString(w, `{"access_token":"`+leakAccess+`"}`)
	}))
	t.Cleanup(srv2.Close)
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", srv2.URL+"/taken?access_token="+leakAccess+"&refresh_token="+leakRefresh+"&device_code="+leakDevice+"&user_code="+leakUser)
		w.WriteHeader(http.StatusFound)
		fmt.Fprintf(w, `{"error_description":"%s","access_token":"%s"}`, leakDesc, leakAccess)
	}))
	t.Cleanup(srv1.Close)
	control, err := http.Get(srv1.URL)
	if err != nil {
		t.Fatalf("control redirect: %v", err)
	}
	control.Body.Close()
	if hits2.Load() != 1 {
		t.Fatalf("control hits = %d, fixture did not follow redirects", hits2.Load())
	}
	cfg := OAuth2Config{
		DeviceCodeURL: srv1.URL,
		TokenURL:      srv1.URL,
		ClientID:      "client-1",
		CheckURL:      allowHost(hostname(t, srv1.URL)),
		Client:        srv1.Client(),
	}
	_, err = RefreshOAuth2(context.Background(), cfg, leakRefresh)
	if err == nil {
		t.Fatal("expected redirect to fail")
	}
	if hits2.Load() != 1 {
		t.Fatalf("engine followed redirect, hits = %d", hits2.Load())
	}
	assertNoLeak(t, err.Error(), leakAccess, leakRefresh, leakDevice, leakUser, leakDesc)
	var oauthErr *OAuth2Error
	if !errors.As(err, &oauthErr) || oauthErr.Status != http.StatusFound || oauthErr.Code != "unknown" {
		t.Fatalf("error = %v", err)
	}
	if IsTransient(err) || errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestExtraHeadersValidation(t *testing.T) {
	t.Parallel()
	forbidden := []string{
		"Authorization",
		"authorization",
		"Cookie",
		"Host",
		"Content-Length",
		"Transfer-Encoding",
		"Connection",
		"Proxy-Authorization",
		"X-Forwarded-For",
		"x-forwarded-host",
	}
	for _, name := range forbidden {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewOAuth2Config(OAuth2Config{ExtraHeaders: map[string]string{name: "Bearer " + leakAccess}})
			if err == nil {
				t.Fatal("expected header rejection")
			}
			assertNoLeak(t, err.Error(), leakAccess)
		})
	}
	t.Run("crlf value", func(t *testing.T) {
		t.Parallel()
		_, err := NewOAuth2Config(OAuth2Config{ExtraHeaders: map[string]string{"User-Agent": "agent\r\nX-Injected: " + leakAccess}})
		if err == nil {
			t.Fatal("expected value rejection")
		}
		assertNoLeak(t, err.Error(), leakAccess)
	})
	t.Run("invalid name", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"", "Bad Name", "Foo:Bar", "你好"} {
			if _, err := NewOAuth2Config(OAuth2Config{ExtraHeaders: map[string]string{name: "x"}}); err == nil {
				t.Fatalf("name %q was accepted", name)
			}
		}
	})
	t.Run("first call rejects a literal config", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
		}))
		t.Cleanup(srv.Close)
		cfg := baseConfig(t, srv)
		cfg.ExtraHeaders = map[string]string{"Cookie": "session=" + leakRefresh}
		_, err := StartDeviceCode(context.Background(), cfg)
		if err == nil {
			t.Fatal("expected header rejection")
		}
		if hits.Load() != 0 {
			t.Fatalf("hits = %d", hits.Load())
		}
		assertNoLeak(t, err.Error(), leakRefresh)
	})
	t.Run("constructor clones headers", func(t *testing.T) {
		t.Parallel()
		var agent atomic.Value
		var verify string
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			agent.Store(r.Header.Get("User-Agent"))
			fmt.Fprintf(w, `{"device_code":"dev","user_code":"usr","verification_uri":"%s"}`, verify)
		}))
		verify = srv.URL + "/verify"
		t.Cleanup(srv.Close)
		headers := map[string]string{"User-Agent": "pool-agent", "X-Client-Surface": "cli"}
		cfg, err := NewOAuth2Config(baseConfig(t, srv))
		if err != nil {
			t.Fatal(err)
		}
		cfg.ExtraHeaders = headers
		cfg, err = NewOAuth2Config(cfg)
		if err != nil {
			t.Fatal(err)
		}
		headers["User-Agent"] = "mutated"
		headers["Authorization"] = "Bearer " + leakAccess
		if _, err := StartDeviceCode(context.Background(), cfg); err != nil {
			t.Fatalf("StartDeviceCode: %v", err)
		}
		if agent.Load() != "pool-agent" {
			t.Fatalf("User-Agent = %v", agent.Load())
		}
	})
}

func TestTokensRedactSecrets(t *testing.T) {
	t.Parallel()
	tokens := OAuth2Tokens{
		AccessToken:  leakAccess,
		RefreshToken: leakRefresh,
		IDToken:      leakID,
		TokenType:    "Bearer",
		Scope:        "openid",
		ExpiresIn:    30,
		ExpiresAt:    time.Unix(1700000000, 0).UTC(),
	}
	encoded, err := json.Marshal(tokens)
	if err != nil {
		t.Fatal(err)
	}
	encodedPtr, err := json.Marshal(&tokens)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := struct{ Tokens OAuth2Tokens }{Tokens: tokens}
	texts := []string{
		tokens.String(),
		tokens.GoString(),
		fmt.Sprintf("%v", tokens),
		fmt.Sprintf("%+v", tokens),
		fmt.Sprintf("%#v", tokens),
		fmt.Sprintf("%s", tokens),
		fmt.Sprintf("%q", tokens),
		fmt.Sprintf("%v", &tokens),
		fmt.Sprintf("%+v", wrapper),
		string(encoded),
		string(encodedPtr),
	}
	for _, text := range texts {
		assertNoLeak(t, text, leakAccess, leakRefresh, leakID)
		if !strings.Contains(text, "Bearer") && !strings.Contains(text, "openid") {
			t.Fatalf("redacted text dropped non-secrets: %s", text)
		}
	}
	if !strings.Contains(string(encoded), `"access_token":"redacted"`) {
		t.Fatalf("json = %s", encoded)
	}
}

func TestResponseBodyLimit(t *testing.T) {
	t.Parallel()
	exact := sizedJSON(t, maxBodyBytes)
	over := strings.Repeat("A", maxBodyBytes+1)
	t.Run("exact limit", func(t *testing.T) {
		t.Parallel()
		srv := newJSONServer(t, http.StatusOK, exact)
		tokens, err := RefreshOAuth2(context.Background(), baseConfig(t, srv), "refresh")
		if err != nil {
			t.Fatalf("RefreshOAuth2: %v", err)
		}
		if tokens.AccessToken != "sized-token" {
			t.Fatal("expected the exact-limit body to parse")
		}
	})
	t.Run("over limit", func(t *testing.T) {
		t.Parallel()
		srv := newJSONServer(t, http.StatusOK, over)
		_, err := RefreshOAuth2(context.Background(), baseConfig(t, srv), leakRefresh)
		if !errors.Is(err, errResponseTooLarge) {
			t.Fatalf("error = %v", err)
		}
		assertNoLeak(t, err.Error(), leakRefresh, "AAA", "sized-token")
		if IsTransient(err) {
			t.Fatal("oversized 200 is not transient")
		}
	})
}

func TestContextCancel(t *testing.T) {
	t.Parallel()
	t.Run("already canceled", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
		}))
		t.Cleanup(srv.Close)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := RefreshOAuth2(ctx, baseConfig(t, srv), leakRefresh)
		if !errors.Is(err, context.Canceled) || IsTransient(err) {
			t.Fatalf("error = %v", err)
		}
		if hits.Load() != 0 {
			t.Fatalf("hits = %d", hits.Load())
		}
		assertNoLeak(t, err.Error(), leakRefresh)
	})
	t.Run("canceled in flight", func(t *testing.T) {
		t.Parallel()
		entered := make(chan struct{})
		cfg := OAuth2Config{
			TokenURL: "https://auth.example/token",
			ClientID: "client-1",
			CheckURL: allowHost("auth.example"),
			Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				close(entered)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})},
		}
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			_, err := RefreshOAuth2(ctx, cfg, leakRefresh)
			errCh <- err
		}()
		<-entered
		cancel()
		err := <-errCh
		if !errors.Is(err, context.Canceled) || IsTransient(err) {
			t.Fatalf("error = %v", err)
		}
		assertNoLeak(t, err.Error(), leakRefresh)
	})
}

func TestRequestDeadlineIsCapped(t *testing.T) {
	t.Parallel()
	var until atomic.Int64
	cfg := OAuth2Config{
		TokenURL: "https://auth.example/token",
		ClientID: "client-1",
		CheckURL: allowHost("auth.example"),
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			if !ok {
				return nil, errors.New("missing deadline")
			}
			until.Store(int64(time.Until(deadline)))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"access_token":"ok","expires_in":1}`)),
				Request:    req,
			}, nil
		})},
	}
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := RefreshOAuth2(parent, cfg, "refresh"); err != nil {
		t.Fatalf("RefreshOAuth2: %v", err)
	}
	got := time.Duration(until.Load())
	if got <= 0 || got > requestTimeout+time.Second {
		t.Fatalf("deadline remaining = %s, want about %s", got, requestTimeout)
	}
}

func TestConcurrentConfigUse(t *testing.T) {
	var verify string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/device") {
			fmt.Fprintf(w, `{"device_code":"dev","user_code":"usr","verification_uri":"%s","interval":5,"expires_in":600}`, verify)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"tok","refresh_token":"ref","expires_in":30,"token_type":"Bearer"}`)
	}))
	verify = srv.URL + "/verify"
	t.Cleanup(srv.Close)
	cfg := baseConfig(t, srv)
	cfg.ExtraHeaders = map[string]string{"User-Agent": "pool-agent", "X-Client-Surface": "cli"}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			if _, err := StartDeviceCode(context.Background(), cfg); err != nil {
				t.Errorf("start: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			poll, err := PollDeviceCode(context.Background(), cfg, "dev", 5)
			if err != nil || poll.Tokens == nil || poll.Tokens.AccessToken != "tok" {
				t.Errorf("poll: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			tokens, err := RefreshOAuth2(context.Background(), cfg, "ref")
			if err != nil || tokens.AccessToken != "tok" {
				t.Errorf("refresh: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestScopeOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		parsed, _ := url.ParseQuery(string(raw))
		mu.Lock()
		form = parsed
		mu.Unlock()
		_, _ = io.WriteString(w, `{"access_token":"ok"}`)
	}))
	t.Cleanup(srv.Close)
	cfg := baseConfig(t, srv)
	cfg.Scope = "  "
	if _, err := RefreshOAuth2(context.Background(), cfg, "refresh"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotForm := form
	mu.Unlock()
	if gotForm.Get("scope") != "" || gotForm.Has("scope") {
		t.Fatalf("scope = %q", gotForm.Get("scope"))
	}
}

func TestCustomDeviceGrant(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		parsed, _ := url.ParseQuery(string(raw))
		mu.Lock()
		form = parsed
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
	}))
	t.Cleanup(srv.Close)
	cfg := baseConfig(t, srv)
	cfg.GrantType = "urn:example:device"
	if _, err := PollDeviceCode(context.Background(), cfg, "device", 5); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotForm := form
	mu.Unlock()
	if gotForm.Get("grant_type") != "urn:example:device" {
		t.Fatalf("grant = %q", gotForm.Get("grant_type"))
	}
}

func oauthLeak(code string) string {
	return fmt.Sprintf(`{"error":"%s","error_description":"%s access_token=%s refresh_token=%s device_code=%s user_code=%s","access_token":"%s","refresh_token":"%s"}`,
		code, leakDesc, leakAccess, leakRefresh, leakDevice, leakUser, leakAccess, leakRefresh)
}

func assertNoLeak(t *testing.T, text string, leaks ...string) {
	t.Helper()
	for _, leak := range leaks {
		if leak != "" && strings.Contains(text, leak) {
			t.Fatalf("text leaked %q", leak)
		}
	}
}

func baseConfig(t *testing.T, srv *httptest.Server) OAuth2Config {
	t.Helper()
	return OAuth2Config{
		DeviceCodeURL: srv.URL + "/device",
		TokenURL:      srv.URL + "/token",
		ClientID:      "client-1",
		Scope:         "openid profile",
		CheckURL:      allowHost(hostname(t, srv.URL)),
		Client:        srv.Client(),
	}
}

func allowHost(host string) func(string) error {
	return func(raw string) error {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return errors.New("scheme rejected")
		}
		if !strings.EqualFold(parsed.Hostname(), host) {
			return errors.New("host rejected")
		}
		return nil
	}
}

func hostname(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Hostname()
}

func newJSONServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sizedJSON(t *testing.T, size int) string {
	t.Helper()
	prefix := `{"access_token":"sized-token","pad":"`
	suffix := `"}`
	if size < len(prefix)+len(suffix) {
		t.Fatalf("size %d is too small", size)
	}
	return prefix + strings.Repeat("b", size-len(prefix)-len(suffix)) + suffix
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
