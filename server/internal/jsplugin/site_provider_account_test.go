package jsplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xlyra/server/internal/accountauth"
)

const (
	leakAccess  = "ACCESS_LEAK_9f3c_pool"
	leakRefresh = "REFRESH_LEAK_9f3c_pool"
	leakCookie  = "COOKIE_LEAK_9f3c_pool"
	leakDevice  = "DEVICE_LEAK_9f3c_pool"
	leakNew     = "ACCESS_NEW_9f3c_pool"
)

func TestOAuthDeviceCodeLoginAndRefresh(t *testing.T) {
	logs := captureLogs(t)
	var polls int
	access := unsignedJWT(`{"sub":"user-7"}`)
	var gotClient, gotScope, gotID bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		switch r.URL.Path {
		case "/device":
			gotClient = r.Header.Get("X-Client") == "xlyra"
			gotID = r.Form.Get("client_id") == "grok-client"
			gotScope = r.Form.Get("scope") == "openid"
			fmt.Fprintf(w, `{"device_code":%q,"user_code":"USER-CODE","verification_uri":"http://%s/activate","interval":1,"expires_in":600}`, leakDevice, r.Host)
		case "/token":
			if r.Form.Get("grant_type") == "refresh_token" {
				if r.Form.Get("refresh_token") != leakRefresh {
					http.Error(w, "bad refresh", http.StatusBadRequest)
					return
				}
				fmt.Fprintf(w, `{"access_token":%q,"expires_in":30,"token_type":"bearer"}`, leakNew)
				return
			}
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
				return
			}
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":%q,"expires_in":120,"token_type":"bearer"}`, access, leakRefresh)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rt := oauthRuntime(t, srv)
	seen := &hookSeen{}
	rt.observe = seen.add
	started, err := rt.StartLogin(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !gotClient || !gotID || !gotScope {
		t.Fatalf("device request client=%v id=%v scope=%v", gotClient, gotID, gotScope)
	}
	if started.Status != "pending" || started.UserCode != "USER-CODE" || !strings.Contains(started.VerificationURL, "/activate") || started.Interval != 1 {
		t.Fatalf("start = %s url=%q code=%q interval=%d", started.Status, started.VerificationURL, started.UserCode, started.Interval)
	}
	pending, err := rt.PollLogin(context.Background(), started)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != "pending" || pending.Credential != nil {
		t.Fatalf("pending = %+v", pending)
	}
	complete, err := rt.PollLogin(context.Background(), started)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Status != "complete" || complete.Identity != "user-7" {
		t.Fatalf("complete = %s identity=%q", complete.Status, complete.Identity)
	}
	if complete.Credential["access_token"] != access || complete.Credential["refresh_token"] != leakRefresh {
		t.Fatalf("credential keys=%s", credentialKeys(complete.Credential))
	}
	refreshed, err := rt.Refresh(context.Background(), complete.Credential)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed["access_token"] != leakNew || refreshed["refresh_token"] != leakRefresh {
		t.Fatalf("refresh kept access=%v refresh=%v", refreshed["access_token"] == leakNew, refreshed["refresh_token"] == leakRefresh)
	}
	if len(seen.joined()) != 0 {
		t.Fatal("oauth login called a JS hook")
	}
	assertNoLeak(t, []string{logs.String()}, access, leakRefresh, leakNew, leakDevice)
}

func TestOAuthRefreshReauthAndTransient(t *testing.T) {
	logs := captureLogs(t)
	access := unsignedJWT(`{"sub":"user-7"}`)
	fields := map[string]string{"access_token": access, "refresh_token": leakRefresh}
	reauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/device" {
			fmt.Fprintf(w, `{"device_code":"dev","user_code":"USER-CODE","verification_uri":"http://%s/activate"}`, r.Host)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"invalid_grant","error_description":%q}`, leakRefresh)
	}))
	defer reauthSrv.Close()
	rt := oauthRuntime(t, reauthSrv)
	_, err := rt.Refresh(context.Background(), fields)
	if !errors.Is(err, accountauth.ErrReauthorizationRequired) || accountauth.IsTransient(err) {
		t.Fatalf("reauth = %v", err)
	}
	assertNoLeak(t, []string{err.Error(), logs.String()}, access, leakRefresh)

	transientSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":"upstream","access_token":%q,"refresh_token":%q}`, access, leakRefresh)
	}))
	defer transientSrv.Close()
	rt = oauthRuntime(t, transientSrv)
	_, err = rt.Refresh(context.Background(), fields)
	if !accountauth.IsTransient(err) || errors.Is(err, accountauth.ErrReauthorizationRequired) {
		t.Fatalf("transient = %v", err)
	}
	assertNoLeak(t, []string{err.Error(), logs.String()}, access, leakRefresh)
}

func TestOAuthVerificationURLRejected(t *testing.T) {
	access := unsignedJWT(`{"sub":"user-7"}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"device_code":%q,"user_code":"USER_LEAK_9f3c_pool","verification_uri":"http://evil.example/activate?access=%s"}`, leakDevice, access)
	}))
	defer srv.Close()
	rt := oauthRuntime(t, srv)
	started, err := rt.StartLogin(context.Background(), nil)
	if err == nil {
		t.Fatalf("start returned session %s", started.Status)
	}
	if started.VerificationURL != "" || started.ID != "" {
		t.Fatal("rejected start still returned a verification URL")
	}
	assertNoLeak(t, []string{err.Error()}, access, leakDevice, "USER_LEAK_9f3c_pool", "evil.example/activate")
}

func TestCustomPollLoginAndSummarize(t *testing.T) {
	logs := captureLogs(t)
	seen := &hookSeen{}
	var pollHeader, pollQuery, pollBody, pollCookie string
	var quotaHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/start":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Add("Set-Cookie", "sid="+leakCookie+"; Path=/")
			fmt.Fprintf(w, `{"data":{"accessToken":%q,"refreshToken":%q,"domain":"x.cn"}}`, leakAccess, leakRefresh)
		case "/login/poll":
			pollHeader = r.Header.Get("Authorization")
			pollQuery = r.URL.Query().Get("refresh")
			body, _ := io.ReadAll(r.Body)
			pollBody = string(body)
			pollCookie = r.Header.Get("Cookie")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		case "/quota":
			quotaHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"plan":"pro","remaining":3,"limit":10,"echo":%q}`, leakAccess)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rt := customRuntime(t, srv, "")
	rt.observe = seen.add
	started, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != "pending" || started.UserCode != "USER-1" || !strings.HasSuffix(started.VerificationURL, "/activate") {
		t.Fatalf("start = %s url=%q", started.Status, started.VerificationURL)
	}
	complete, err := rt.PollLogin(context.Background(), started)
	if err != nil {
		t.Fatal(err)
	}
	if complete.Status != "complete" || complete.Identity != "x.cn" {
		t.Fatalf("complete = %s identity=%q", complete.Status, complete.Identity)
	}
	if complete.Credential["access_token"] != leakAccess || complete.Credential["refresh_token"] != leakRefresh || complete.Credential["expires_at"] != "1700000000" || complete.Credential["domain"] != "x.cn" {
		t.Fatalf("credential keys=%s", credentialKeys(complete.Credential))
	}
	if pollHeader != "Bearer "+leakAccess || pollQuery != leakRefresh || !strings.Contains(pollBody, leakRefresh) {
		t.Fatalf("poll did not receive substituted secrets header=%q query=%q body=%q", pollHeader, pollQuery, pollBody)
	}
	if !strings.Contains(pollCookie, leakCookie) {
		t.Fatalf("cookie jar did not replay the login cookie: %q", pollCookie)
	}
	summary, err := rt.Summarize(context.Background(), AccountSnapshot{Fields: complete.Credential})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "ok" || summary.Remark != "pro" || len(summary.Entries) != 1 || summary.Entries[0].Label != "tokens" || summary.Entries[0].Remaining == nil || *summary.Entries[0].Remaining != 3 {
		t.Fatalf("summary = %+v", summary)
	}
	if quotaHeader != "Bearer "+leakAccess {
		t.Fatalf("quota authorization = %q", quotaHeader)
	}
	if !strings.Contains(seen.joined(), SecretRedactedPlaceholder) {
		t.Fatal("JS did not see the redacted placeholder")
	}
	assertNoLeak(t, []string{seen.joined(), logs.String()}, leakAccess, leakRefresh, leakCookie)
	again, err := rt.PollLogin(context.Background(), started)
	if !errors.Is(err, ErrLoginSessionNotFound) || again.Credential != nil {
		t.Fatalf("second poll = %v %s", err, again.Status)
	}
}

func TestCustomRefreshReauthAndTransient(t *testing.T) {
	logs := captureLogs(t)
	seen := &hookSeen{}
	mode := "ok"
	var refreshHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/refresh" {
			http.NotFound(w, r)
			return
		}
		switch mode {
		case "reauth":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"dead","access_token":%q}`, leakAccess)
		case "drop":
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
		default:
			refreshHeader = r.Header.Get("X-Refresh-Token")
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"data":{"accessToken":%q}}`, leakNew)
		}
	}))
	defer srv.Close()
	fields := map[string]string{
		"access_token": leakAccess, "refresh_token": leakRefresh,
		"expires_at": "1700000000", "domain": "x.cn", "realm": "cn",
	}
	rt := customRuntime(t, srv, "")
	rt.observe = seen.add
	refreshed, err := rt.Refresh(context.Background(), fields)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed["access_token"] != leakNew || refreshed["refresh_token"] != leakRefresh || refreshed["domain"] != "x.cn" {
		t.Fatalf("refresh keys=%s", credentialKeys(refreshed))
	}
	if refreshHeader != leakRefresh {
		t.Fatalf("refresh header = %q", refreshHeader)
	}

	mode = "reauth"
	_, err = rt.Refresh(context.Background(), fields)
	if !errors.Is(err, accountauth.ErrReauthorizationRequired) || errors.Is(err, ErrAccountTransient) {
		t.Fatalf("reauth = %v", err)
	}

	mode = "drop"
	_, err = rt.Refresh(context.Background(), fields)
	if !errors.Is(err, ErrAccountTransient) || errors.Is(err, accountauth.ErrReauthorizationRequired) {
		t.Fatalf("transient = %v", err)
	}
	assertNoLeak(t, []string{seen.joined(), logs.String(), err.Error()}, leakAccess, leakRefresh, leakNew)
}

func TestAccountPlaceholderAndHostRejected(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path == "/html" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>"+leakAccess+"</html>")
			return
		}
		if r.URL.Path == "/login/start" {
			w.Header().Set("Location", "http://203.0.113.9/activate?access="+leakAccess)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"accessToken":%q,"refreshToken":%q}}`, leakAccess, leakRefresh)
	}))
	defer srv.Close()

	cases := []struct {
		mode string
		want string
	}{
		{mode: "path", want: "path"},
		{mode: "header-name", want: "header name"},
		{mode: "query-key", want: "query key"},
		{mode: "body-key", want: "body key"},
		{mode: "undeclared", want: "not_declared"},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			before := hits
			rt := customRuntime(t, srv, "")
			_, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn", "mode": tc.mode})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if hits != before {
				t.Fatalf("rejected placeholder still dialed (%d hits)", hits-before)
			}
			assertNoLeak(t, []string{err.Error()}, leakAccess, leakRefresh)
		})
	}

	t.Run("non-json", func(t *testing.T) {
		rt := customRuntime(t, srv, "")
		seen := &hookSeen{}
		rt.observe = seen.add
		_, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn", "mode": "html"})
		if err == nil || !errors.Is(err, ErrSecretRedactNotJSON) {
			t.Fatalf("err = %v", err)
		}
		assertNoLeak(t, []string{err.Error(), seen.joined()}, leakAccess, leakRefresh)
	})

	t.Run("host", func(t *testing.T) {
		before := hits
		rt := customRuntime(t, srv, "")
		rt.hosts = []string{"api.example.cn"}
		_, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("err = %v", err)
		}
		if hits != before {
			t.Fatal("disallowed host was dialed")
		}
		assertNoLeak(t, []string{err.Error()}, leakAccess, leakRefresh)
	})

	t.Run("redirect", func(t *testing.T) {
		guard := &guardTransport{}
		rt := customRuntime(t, srv, "")
		rt.client = &http.Client{Transport: guard}
		_, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn", "mode": "redirect"})
		if err == nil || !strings.Contains(err.Error(), "redirect") {
			t.Fatalf("err = %v", err)
		}
		if host := guard.bad.Load(); host != nil {
			t.Fatalf("followed redirect to %v", host)
		}
		assertNoLeak(t, []string{err.Error()}, leakAccess, leakRefresh)
	})
}

func TestAccountLoginCancelAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/device" {
			fmt.Fprintf(w, `{"device_code":%q,"user_code":"USER-CODE","verification_uri":"http://%s/activate","interval":1,"expires_in":600}`, leakDevice, r.Host)
			return
		}
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
			fmt.Fprint(w, `{"error":"authorization_pending"}`)
		}
	}))
	defer srv.Close()
	rt := oauthRuntime(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rt.StartLogin(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start = %v", err)
	}
	started, err := rt.StartLogin(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err = rt.PollLogin(ctx, started)
	if !errors.Is(err, context.Canceled) || accountauth.IsTransient(err) {
		t.Fatalf("canceled poll = %v", err)
	}

	plugin := mustPlugin(t, timeoutSource(), customSection())
	slow := testRuntime(t, plugin, srv)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = slow.Summarize(ctx, AccountSnapshot{Fields: map[string]string{"access_token": leakAccess, "domain": "x.cn"}})
	if err == nil || !strings.Contains(err.Error(), KindTimeout) {
		t.Fatalf("timeout = %v", err)
	}
	assertNoLeak(t, []string{err.Error()}, leakAccess)
}

func TestAccountProviderConcurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/start":
			w.Header().Add("Set-Cookie", "sid="+leakCookie+"; Path=/")
			fmt.Fprintf(w, `{"data":{"accessToken":%q,"refreshToken":%q,"domain":"x.cn"}}`, leakAccess, leakRefresh)
		case "/login/poll":
			fmt.Fprint(w, `{"ok":true}`)
		case "/quota":
			if r.Header.Get("Authorization") != "Bearer "+leakAccess {
				http.Error(w, "missing", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, `{"plan":"pro","remaining":1,"limit":2,"echo":%q}`, leakAccess)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rt := customRuntime(t, srv, "")
	seen := &hookSeen{}
	rt.observe = seen.add
	var wg sync.WaitGroup
	errCh := make(chan error, 4)
	for i := 0; i < 2; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			started, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
			if err != nil {
				errCh <- err
				return
			}
			if _, err := rt.PollLogin(context.Background(), started); err != nil {
				errCh <- err
			}
		}()
		go func() {
			defer wg.Done()
			summary, err := rt.Summarize(context.Background(), AccountSnapshot{Fields: map[string]string{
				"access_token": leakAccess, "refresh_token": leakRefresh, "domain": "x.cn", "realm": "cn",
			}})
			if err != nil {
				errCh <- err
				return
			}
			if summary.Status != "ok" {
				errCh <- fmt.Errorf("status %s", summary.Status)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	assertNoLeak(t, []string{seen.joined()}, leakAccess, leakRefresh, leakCookie)
}

func TestAccountRuntimeRejectsAndMissingSession(t *testing.T) {
	source := meta("acme.example", KindSiteProvider)
	plugin, err := NewPlugin(siteProviderManifest(source), source, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAccountProviderRuntime(plugin, nil); err == nil {
		t.Fatal("api_key plugin was accepted")
	}
	srv := httptest.NewServer(http.HandlerFunc(http.NotFound))
	defer srv.Close()
	rt := oauthRuntime(t, srv)
	if _, err := rt.PollLogin(context.Background(), LoginSession{ID: "missing"}); !errors.Is(err, ErrLoginSessionNotFound) {
		t.Fatalf("missing = %v", err)
	}
	rt.spec.Refresh.Kind = "none"
	if _, err := rt.Refresh(context.Background(), map[string]string{"refresh_token": leakRefresh}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("refresh none = %v", err)
	}
	accountPlugin := mustPlugin(t, customSource(), customSection())
	if _, err := accountPlugin.CallSteppedHook(context.Background(), "startLogin", KindAccountLogin, ProbeContext{}, nil); err == nil || !strings.Contains(err.Error(), "not a stepped result kind") {
		t.Fatalf("stepped hook = %v", err)
	}
}

type hookSeen struct {
	mu    sync.Mutex
	blobs []string
}

func (s *hookSeen) add(hook string, args []any) {
	raw, err := json.Marshal(args)
	text := hook + ":"
	if err != nil {
		text += err.Error()
	} else {
		text += string(raw)
	}
	s.mu.Lock()
	s.blobs = append(s.blobs, text)
	s.mu.Unlock()
}

func (s *hookSeen) joined() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.blobs, "\n")
}

type guardTransport struct {
	bad atomicString
}

func (g *guardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	if host != "127.0.0.1" && host != "::1" {
		g.bad.Store(host)
		return nil, fmt.Errorf("blocked %s", host)
	}
	return http.DefaultTransport.RoundTrip(req)
}

type atomicString struct {
	mu    sync.Mutex
	value any
}

func (a *atomicString) Store(value any) {
	a.mu.Lock()
	a.value = value
	a.mu.Unlock()
}

func (a *atomicString) Load() any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.value
}

func captureLogs(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func assertNoLeak(t *testing.T, texts []string, secrets ...string) {
	t.Helper()
	for _, text := range texts {
		for _, secret := range secrets {
			if secret != "" && strings.Contains(text, secret) {
				t.Fatalf("leaked secret %q", secret)
			}
		}
	}
}

func oauthRuntime(t *testing.T, srv *httptest.Server) *AccountProviderRuntime {
	t.Helper()
	source := poolSource("acme.example", "summarizeAccount")
	return testRuntime(t, mustPlugin(t, source, grokSection()), srv)
}

func customRuntime(t *testing.T, srv *httptest.Server, source string) *AccountProviderRuntime {
	t.Helper()
	if source == "" {
		source = customSource()
	}
	return testRuntime(t, mustPlugin(t, source, customSection()), srv)
}

func testRuntime(t *testing.T, plugin *Plugin, srv *httptest.Server) *AccountProviderRuntime {
	t.Helper()
	client := srv.Client()
	rt, err := NewAccountProviderRuntime(plugin, client)
	if err != nil {
		t.Fatal(err)
	}
	rt.hosts = []string{"127.0.0.1"}
	if len(rt.spec.Regions) > 0 {
		rt.spec.Regions[0].BaseURL = srv.URL
	}
	if rt.spec.Login.DeviceCodeURL != "" {
		rt.spec.Login.DeviceCodeURL = srv.URL + "/device"
		rt.spec.Login.TokenURL = srv.URL + "/token"
	}
	if rt.spec.Refresh.TokenURL != "" || rt.spec.Refresh.Kind == "oauth2_refresh" {
		rt.spec.Refresh.TokenURL = srv.URL + "/token"
	}
	return rt
}

func mustPlugin(t *testing.T, source string, section SiteProviderSection) *Plugin {
	t.Helper()
	plugin, err := NewPlugin(poolManifest(source, section), source, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	return plugin
}

func customSection() SiteProviderSection {
	section := poolSection()
	section.Credential.Identity = Sources("field.domain")
	section.Import.FieldMap["domain"] = []string{"domain"}
	return section
}

func customSource() string {
	return meta("acme.example", KindSiteProvider) + `
function leaked(ctx, steps) {
  const blob = JSON.stringify(ctx) + "\n" + JSON.stringify(steps);
  log.info(blob);
  return blob.indexOf("ACCESS_LEAK_") >= 0 || blob.indexOf("REFRESH_LEAK_") >= 0 || blob.indexOf("COOKIE_LEAK_") >= 0;
}
export function startLogin(ctx, steps) {
  if (leaked(ctx, steps)) return { error: "saw-secret" };
  const mode = (ctx.params && ctx.params.mode) || "";
  if (steps.length === 0) {
    if (mode === "path") return { request: { method: "GET", path: "/{{secret.access_token}}" } };
    if (mode === "header-name") return { request: { method: "GET", path: "/login/start", headers: { "{{secret.access_token}}": "x" } } };
    if (mode === "query-key") return { request: { method: "GET", path: "/login/start", query: { "{{secret.access_token}}": "x" } } };
    if (mode === "body-key") return { request: { method: "POST", path: "/login/start", body: { "{{secret.access_token}}": "x" } } };
    if (mode === "undeclared") return { request: { method: "POST", path: "/login/start", body: { token: "{{secret.not_declared}}" } } };
    if (mode === "html") return { request: { method: "GET", path: "/html" } };
    if (mode === "redirect") return { request: { method: "GET", path: "/login/start" } };
    return { request: { method: "POST", path: "/login/start", body: { region: (ctx.params && ctx.params.region) || "" } } };
  }
  return { result: { kind: "account_login", status: "pending", verificationURL: ctx.baseURL + "/activate", userCode: "USER-1", interval: 1 } };
}
export function pollLogin(ctx, steps) {
  if (leaked(ctx, steps)) return { error: "saw-secret" };
  const last = steps.length ? steps[steps.length - 1] : null;
  if (!last || last.request.path !== "/login/poll") {
    return { request: { method: "POST", path: "/login/poll", headers: { Authorization: "Bearer {{secret.access_token}}" }, query: { refresh: "{{secret.refresh_token}}" }, body: { refresh: "{{secret.refresh_token}}" } } };
  }
  return { result: { kind: "account_login", status: "complete", credential: { access_token: "{{secret.access_token}}", refresh_token: "{{secret.refresh_token}}", expires_at: "1700000000", domain: "x.cn", realm: "cn" } } };
}
export function refreshTokens(ctx, steps) {
  if (leaked(ctx, steps)) return { error: "saw-secret" };
  if (steps.length === 0) {
    return { request: { method: "POST", path: "/refresh", headers: { "X-Refresh-Token": "{{secret.refresh_token}}" } } };
  }
  if (steps[0].response.status === 401) {
    return { result: { kind: "account_refresh", status: "reauth" } };
  }
  return { result: { kind: "account_refresh", status: "ok", credential: { access_token: "{{secret.access_token}}", refresh_token: "{{secret.refresh_token}}", expires_at: "1700001000", domain: (ctx.fields && ctx.fields.domain) || "", realm: (ctx.fields && ctx.fields.realm) || "" } } };
}
export function summarizeAccount(ctx, steps) {
  if (leaked(ctx, steps)) return { error: "saw-secret" };
  if (steps.length === 0) {
    return { request: { method: "GET", path: "/quota", headers: { Authorization: "Bearer {{secret.access_token}}" } } };
  }
  const row = (steps[0].response && steps[0].response.json) || {};
  return { result: { kind: "account_summary", status: "ok", remark: row.plan || "ok", entries: [{ label: "tokens", remaining: row.remaining, limit: row.limit }] } };
}
`
}

func timeoutSource() string {
	return meta("acme.example", KindSiteProvider) + `
export function startLogin() { return { error: "no" }; }
export function pollLogin() { return { error: "no" }; }
export function refreshTokens() { return { error: "no" }; }
export function summarizeAccount() { while (true) {} }
`
}

func unsignedJWT(payload string) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc([]byte(payload)) + ".sig"
}

func TestCustomPollPendingThenComplete(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/start":
			fmt.Fprint(w, `{"ok":true}`)
		case "/poll":
			polls++
			if polls == 1 {
				fmt.Fprint(w, `{"code":11217}`)
				return
			}
			fmt.Fprintf(w, `{"data":{"accessToken":%q,"refreshToken":%q}}`, "tok-a", "tok-r")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	source := meta("acme.example", KindSiteProvider) + `
export function startLogin(ctx, steps) {
  if (steps.length === 0) return { request: { method: "POST", path: "/start", body: {} } };
  return { result: { kind: "account_login", status: "pending", verificationURL: ctx.baseURL + "/activate", interval: 3 } };
}
export function pollLogin(ctx, steps) {
  let token = null;
  for (const step of steps) {
    if (step.request && step.request.path === "/poll") token = step;
  }
  if (!token) return { request: { method: "GET", path: "/poll" } };
  const code = token.response && token.response.json && token.response.json.code;
  if (code === 11217) return { result: { kind: "account_login", status: "pending", interval: 3 } };
  return { result: { kind: "account_login", status: "complete", credential: { access_token: "{{secret.access_token}}", refresh_token: "{{secret.refresh_token}}", domain: "x.cn", realm: "cn", expires_at: "1700000000" } } };
}
export function refreshTokens() { return { error: "no" }; }
export function summarizeAccount() { return { result: { kind: "account_summary", status: "ok", entries: [] } }; }
`
	rt := customRuntime(t, srv, source)
	started, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := rt.PollLogin(context.Background(), started)
	if err != nil || pending.Status != "pending" || polls != 1 {
		t.Fatalf("pending = %s polls=%d err=%v", pending.Status, polls, err)
	}
	done, err := rt.PollLogin(context.Background(), started)
	if err != nil || done.Status != "complete" || polls != 2 || done.Credential["access_token"] != "tok-a" {
		t.Fatalf("complete = %s polls=%d err=%v", done.Status, polls, err)
	}
}

func TestCustomPollNonJSONStaysPending(t *testing.T) {
	const leak = "ACCESS_LEAK_pending_body"
	logs := captureLogs(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		case "/poll":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, leak)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>"+leak+"</html>")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	source := meta("acme.example", KindSiteProvider) + `
export function startLogin(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/html", onNonJSON: "pending" } };
  return { result: { kind: "account_login", status: "pending", interval: 1 } };
}
export function pollLogin(ctx, steps) {
  let token = null;
  for (const step of steps) {
    if (step.request && step.request.path === "/poll") token = step;
  }
  if (!token) return { request: { method: "GET", path: "/poll", onNonJSON: "pending" } };
  return { error: "poll saw a body" };
}
export function refreshTokens() { return { error: "no" }; }
export function summarizeAccount() { return { result: { kind: "account_summary", status: "ok", entries: [] } }; }
`
	rt := customRuntime(t, srv, source)
	if _, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"}); err == nil || !errors.Is(err, ErrSecretRedactNotJSON) || strings.Contains(err.Error(), leak) {
		t.Fatalf("start ignored onNonJSON: %v", err)
	}
	source = strings.Replace(source, `path: "/html", onNonJSON: "pending"`, `path: "/start"`, 1)
	rt = customRuntime(t, srv, source)
	started, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := rt.PollLogin(context.Background(), started)
	if err != nil || pending.Status != "pending" {
		t.Fatalf("poll = %+v err=%v", pending, err)
	}
	if strings.Contains(logs.String(), leak) || strings.Contains(pending.String(), leak) {
		t.Fatal("non-JSON poll body reached the host result or logs")
	}
}

func TestRefreshCustomFollowsRealm(t *testing.T) {
	saw := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/refresh" {
			http.NotFound(w, r)
			return
		}
		saw = true
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"accessToken":"next-token"}}`)
	}))
	defer srv.Close()
	rt := customRuntime(t, srv, "")
	rt.spec.Regions = []SiteProviderRegion{
		{ID: "cn", BaseURL: "https://api.example.cn"},
		{ID: "intl", BaseURL: srv.URL, Realms: []string{"international"}},
	}
	rt.spec.DefaultRegion = "cn"
	fields := map[string]string{
		"access_token": unsignedJWT(`{"sub":"user-1"}`), "refresh_token": "r-1",
		"domain": "x.cn", "realm": "international",
	}
	if _, err := rt.Refresh(context.Background(), fields); err != nil {
		t.Fatal(err)
	}
	if !saw {
		t.Fatal("refresh did not use the realm region")
	}
}

func TestAccountListModelsKeepsTokenOutOfJS(t *testing.T) {
	logs := captureLogs(t)
	const domain = "tenant.example"
	const marker = "BODY_MARKER_9f3c"
	mode := "ok"
	hits := 0
	var auth, gotDomain string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		auth = r.Header.Get("Authorization")
		gotDomain = r.Header.Get("X-Domain")
		if mode == "unauth" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"%s"}`, marker)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":["m1"]}`)
	}))
	defer srv.Close()

	section := poolSection()
	section.Models.PerAccount = true
	source := poolSource("acme.example", "summarizeAccount", "startLogin", "pollLogin", "refreshTokens") + `
export function listModels(ctx, steps) {
  const account = (ctx && ctx.account) || {};
  log.info("account-domain=" + (account.domain || "") + " region=" + (account.region || ""));
  if (steps.length === 0) {
    if (account.domain === "evil") return { request: { method: "GET", path: "https://evil.example/models" } };
    const headers = { Authorization: "Bearer {{secret.access_token}}" };
    if (account.domain) headers["X-Domain"] = account.domain;
    return { request: { method: "GET", path: "/models", headers: headers } };
  }
  const body = steps[0] && steps[0].response ? String(steps[0].response.body) : "";
  if (body.indexOf("BODY_MARKER_9f3c") >= 0) return { error: "saw-body" };
  return { result: { models: [{ name: "m1", displayName: "M1" }] } };
}
`
	rt := testRuntime(t, mustPlugin(t, source, section), srv)
	seen := &hookSeen{}
	rt.observe = seen.add
	fields := map[string]string{
		"access_token": leakAccess, "refresh_token": leakRefresh,
		"domain": domain, "realm": "cn",
	}
	entries, err := rt.ListModels(context.Background(), fields)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "m1" || entries[0].DisplayName != "M1" {
		t.Fatalf("models = %+v", entries)
	}
	if auth != "Bearer "+leakAccess || gotDomain != domain || hits != 1 {
		t.Fatalf("auth=%q domain=%q hits=%d", auth, gotDomain, hits)
	}
	if !strings.Contains(seen.joined(), domain) || !strings.Contains(seen.joined(), `"region":"cn"`) {
		t.Fatalf("account context = %s", seen.joined())
	}
	assertNoLeak(t, []string{logs.String(), seen.joined(), fmt.Sprint(err)}, leakAccess, leakRefresh)

	before := hits
	_, err = rt.ListModels(context.Background(), map[string]string{
		"access_token": leakAccess, "domain": "evil", "realm": "cn",
	})
	if err == nil || hits != before {
		t.Fatalf("evil url err=%v hits=%d before=%d", err, hits, before)
	}
	assertNoLeak(t, []string{logs.String(), seen.joined(), err.Error()}, leakAccess, leakRefresh)

	mode = "unauth"
	_, err = rt.ListModels(context.Background(), fields)
	if !errors.Is(err, accountauth.ErrReauthorizationRequired) {
		t.Fatalf("401 = %v", err)
	}
	if strings.Contains(seen.joined(), marker) || strings.Contains(logs.String(), marker) || strings.Contains(err.Error(), marker) {
		t.Fatal("401 body was shown to JS or the error")
	}
	assertNoLeak(t, []string{logs.String(), seen.joined(), err.Error()}, leakAccess, leakRefresh)
}
