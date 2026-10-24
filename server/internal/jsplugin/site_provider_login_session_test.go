package jsplugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"xlyra/server/internal/httpclient"
)

func TestLoginSessionsRejectWhenFull(t *testing.T) {
	rt := pendingLoginRuntime(t)
	var kept string
	for i := 0; i < maxLoginSessions; i++ {
		session, err := rt.newSession("custom_poll", NewSecretBag(), nil, "https://api.example.cn")
		if err != nil {
			t.Fatal(err)
		}
		session.expiresAt = time.Now().Add(time.Hour)
		rt.store(session)
		if i == 0 {
			kept = session.id
		}
	}
	_, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if !errors.Is(err, ErrTooManyLoginSessions) || !strings.Contains(err.Error(), "too many login sessions") {
		t.Fatalf("over cap = %v", err)
	}
	rt.mu.Lock()
	n := len(rt.sessions)
	_, still := rt.sessions[kept]
	rt.mu.Unlock()
	if n != maxLoginSessions || !still {
		t.Fatalf("valid sessions = %d present=%v", n, still)
	}
	poll, err := rt.PollLogin(context.Background(), LoginSession{ID: kept})
	if err != nil || poll.Status != "pending" {
		t.Fatalf("existing session poll = %#v %v", poll, err)
	}

	rt.mu.Lock()
	for _, session := range rt.sessions {
		session.expiresAt = time.Now().Add(-time.Second)
	}
	rt.mu.Unlock()
	next, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatalf("login after expiry = %v", err)
	}
	rt.mu.Lock()
	_, keptStill := rt.sessions[kept]
	_, nextPresent := rt.sessions[next.ID]
	n = len(rt.sessions)
	rt.mu.Unlock()
	if keptStill || !nextPresent || n != 1 {
		t.Fatalf("after expiry sessions=%d kept=%v next=%v", n, keptStill, nextPresent)
	}
}

func TestExpiredLoginSessionIsRemovedAndWiped(t *testing.T) {
	rt := pendingLoginRuntime(t)
	first, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(&url.URL{Scheme: "https", Host: "api.example.cn"}, []*http.Cookie{{Name: "sid", Value: "COOKIE_LEAK_9f3c_pool"}})
	rt.mu.Lock()
	held := rt.sessions[first.ID]
	held.deviceCode = "DEVICE_LEAK_9f3c_pool"
	held.call.bag.Set("access_token", "ACCESS_LEAK_9f3c_pool")
	held.call.jar = jar
	held.expiresAt = time.Now().Add(-time.Second)
	rt.mu.Unlock()

	if _, err := rt.PollLogin(context.Background(), second); err != nil {
		t.Fatalf("poll live session: %v", err)
	}
	rt.mu.Lock()
	_, still := rt.sessions[first.ID]
	rt.mu.Unlock()
	if still || held.deviceCode != "" || held.call.bag != nil || held.call.jar != nil {
		t.Fatalf("expired session still=%v device=%q bag=%v jar=%v", still, held.deviceCode, held.call.bag != nil, held.call.jar != nil)
	}
}

func TestAccountTransportErrorHidesRequestURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		hide []string
	}{
		{
			name: "query token",
			raw:  "https://metadata.acme.example/latest?token=METADATA_TOKEN_9f3c",
			hide: []string{"METADATA_TOKEN_9f3c", "https://", "http://"},
		},
		{
			name: "userinfo",
			raw:  "https://user:s3cret@10.1.2.3:8080/latest?token=METADATA_TOKEN_9f3c",
			hide: []string{"s3cret", "METADATA_TOKEN_9f3c", "10.1.2.3", "https://", "http://"},
		},
		{
			name: "single quote",
			raw:  "https://idp.example/oauth/o'reilly?code=METADATA_TOKEN_9f3c",
			hide: []string{"METADATA_TOKEN_9f3c", "reilly", "https://", "http://"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plugin := mustPlugin(t, customSource(), customSection())
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return nil, &url.Error{
					Op:  req.Method,
					URL: tc.raw,
					Err: errors.New("blocked connection to non-public address 169.254.169.254"),
				}
			})}
			rt, err := NewAccountProviderRuntime(plugin, client)
			if err != nil {
				t.Fatal(err)
			}
			rt.hosts = []string{"api.example.cn"}
			_, err = rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
			if err == nil {
				t.Fatal("expected transport failure")
			}
			if !errors.Is(err, ErrAccountTransient) {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(err.Error(), "blocked connection") {
				t.Fatalf("error = %v", err)
			}
			for _, hidden := range tc.hide {
				if strings.Contains(err.Error(), hidden) {
					t.Fatalf("error = %v still has %q", err, hidden)
				}
			}
		})
	}
}

func TestRedactRequestURL(t *testing.T) {
	const secret = "SECRET_VALUE_9f3c"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "uppercase scheme",
			in:   "GET HTTPS://metadata.acme.example/latest?token=" + secret + " done",
			want: "GET  done",
		},
		{
			name: "quoted url",
			in:   `upstream "https://idp.example/oauth?code=` + secret + `" failed`,
			want: `upstream "" failed`,
		},
		{
			name: "single quote url",
			in:   "fetch 'https://idp.example/oauth/o'reilly?code=" + secret + "' now",
			want: "fetch ' now",
		},
		{
			name: "userinfo",
			in:   "login user:s3cret@10.1.2.3:8080 failed",
			want: "login  failed",
		},
		{
			name: "ipv6",
			in:   "https://[::1]/latest?token=" + secret,
			want: "",
		},
		{
			name: "plain text",
			in:   "blocked connection to non-public address 169.254.169.254",
			want: "blocked connection to non-public address 169.254.169.254",
		},
		{
			name: "dial detail",
			in:   "dial tcp 169.254.169.254:80: connect: connection refused",
			want: "dial tcp 169.254.169.254:80: connect: connection refused",
		},
		{
			name: "two urls",
			in:   "see https://a.example/x?t=" + secret + " and http://b.example/y?t=" + secret,
			want: "see  and ",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactRequestURL(tc.in); got != tc.want {
				t.Fatalf("redactRequestURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	t.Run("secret bag", func(t *testing.T) {
		const token = "TOKEN_VALUE_9f3c"
		bag := NewSecretBag()
		bag.Set("access_token", token)
		err := accountTransportError(bag, errors.New("upstream said "+token+" dial tcp 169.254.169.254:80"))
		if !errors.Is(err, ErrAccountTransient) {
			t.Fatalf("error = %v", err)
		}
		got := err.Error()
		if strings.Contains(got, token) || !strings.Contains(got, "[redacted]") || !strings.Contains(got, "dial tcp 169.254.169.254:80") {
			t.Fatalf("error = %s", got)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func pendingLoginRuntime(t *testing.T) *AccountProviderRuntime {
	t.Helper()
	section := poolSection()
	section.Refresh = &SiteProviderRefresh{Kind: "none"}
	rt, err := NewAccountProviderRuntime(mustPlugin(t, pendingLoginSource(), section), nil)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func pendingLoginSource() string {
	return meta("acme.example", KindSiteProvider) + "\n" +
		"export function startLogin() {\n" +
		"  return { result: { kind: \"account_login\", status: \"pending\", verificationURL: \"https://api.example.cn/login\", userCode: \"USER-1\", interval: 1 } };\n" +
		"}\n" +
		"export function pollLogin() {\n" +
		"  return { result: { kind: \"account_login\", status: \"pending\", interval: 1 } };\n" +
		"}\n" +
		"export function summarizeAccount() {\n" +
		"  return { result: { kind: \"account_summary\", status: \"ok\" } };\n" +
		"}\n"
}

func TestExpiredLoginSessionReapedInBackground(t *testing.T) {
	prev := loginSessionReapInterval.Swap(int64(20 * time.Millisecond))
	t.Cleanup(func() { loginSessionReapInterval.Store(prev) })
	rt := pendingLoginRuntime(t)
	started, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	held := rt.sessions[started.ID]
	held.deviceCode = "DEVICE_LEAK_9f3c_pool"
	held.call.bag.Set("access_token", "ACCESS_LEAK_9f3c_pool")
	held.call.jar = jar
	held.expiresAt = time.Now().Add(-time.Second)
	rt.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for {
		rt.mu.Lock()
		_, still := rt.sessions[started.ID]
		rt.mu.Unlock()
		held.mu.Lock()
		wiped := held.deviceCode == "" && held.call.bag == nil && held.call.jar == nil
		held.mu.Unlock()
		if !still && wiped {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expired session still=%v wiped=%v", still, wiped)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLoginReapDuringPoll(t *testing.T) {
	rt := pendingLoginRuntime(t)
	started, err := rt.StartLogin(context.Background(), map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	held := rt.sessions[started.ID]
	held.deviceCode = "DEVICE_LEAK_9f3c_pool"
	held.call.bag.Set("access_token", "ACCESS_LEAK_9f3c_pool")
	rt.mu.Unlock()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	rt.observe = func(hook string, _ []any) {
		if hook != hookPollLogin {
			return
		}
		once.Do(func() { close(entered) })
		<-release
	}

	pollDone := make(chan error, 1)
	go func() {
		_, err := rt.PollLogin(context.Background(), started)
		pollDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("poll did not reach the hook")
	}

	reapDone := make(chan struct{})
	go func() {
		rt.reapLoginSessions(time.Now().Add(time.Hour))
		close(reapDone)
	}()
	select {
	case <-reapDone:
		t.Fatal("reap wiped a session while PollLogin held it")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)

	select {
	case err := <-pollDone:
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("poll deadlocked with session reap")
	}
	select {
	case <-reapDone:
	case <-time.After(5 * time.Second):
		t.Fatal("reap deadlocked with PollLogin")
	}
	rt.mu.Lock()
	_, still := rt.sessions[started.ID]
	rt.mu.Unlock()
	if still || held.deviceCode != "" || held.call.bag != nil {
		t.Fatalf("in-flight session still=%v device=%q bag=%v", still, held.deviceCode, held.call.bag != nil)
	}
}

func TestHookedClientKeepsAccountTimeoutAndDialGuard(t *testing.T) {
	section := poolSection()
	section.Refresh = &SiteProviderRefresh{Kind: "none"}
	transport := httpclient.NewGuardedTransport(time.Second)
	rt, err := NewAccountProviderRuntime(mustPlugin(t, pendingLoginSource(), section), &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	client := rt.hookedClient(nil, "https://api.example.cn")
	if client.Timeout != accountRequestTimeout || client.CheckRedirect == nil {
		t.Fatalf("timeout=%s redirect=%v", client.Timeout, client.CheckRedirect != nil)
	}
	dialer, ok := client.Transport.(*http.Transport)
	if !ok || dialer.DialContext == nil {
		t.Fatal("hooked client dropped the guarded dial")
	}
	const token = "METADATA_TOKEN_9f3c"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", "169.254.169.254:80")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("metadata dial connected")
	}
	if err == nil || !strings.Contains(err.Error(), "blocked connection") || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "https://") {
		t.Fatalf("dial = %v", err)
	}
}
