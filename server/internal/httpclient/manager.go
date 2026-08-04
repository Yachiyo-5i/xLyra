package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"

	"xlyra/server/internal/config"
)

// ssrfBlockPrivateNetworks, when set, additionally blocks loopback and private
// ranges. Off by default so self-hosted upstreams (localhost, LAN model servers)
// keep working; link-local/metadata is always blocked regardless.
var ssrfBlockPrivateNetworks = strings.EqualFold(strings.TrimSpace(os.Getenv("HTTP_CLIENT_BLOCK_PRIVATE_NETWORKS")), "true")

// ssrfGuardControl is a net.Dialer.Control hook that inspects the resolved IP
// just before connecting (so it also defeats DNS rebinding) and refuses to dial
// non-public addresses. Cloud metadata (169.254.169.254) and other link-local,
// unspecified and multicast targets are always blocked; loopback/private ranges
// are blocked only when HTTP_CLIENT_BLOCK_PRIVATE_NETWORKS is enabled.
func ssrfGuardControl(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("blocked dial to %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("blocked dial to non-ip address %q", host)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("blocked connection to non-public address %s", ip)
	}
	if ssrfBlockPrivateNetworks && (ip.IsLoopback() || ip.IsPrivate()) {
		return fmt.Errorf("blocked connection to private address %s", ip)
	}
	return nil
}

func NewGuardedDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control:   ssrfGuardControl,
	}
}

type ProxyProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type NetworkConfig struct {
	Proxies []ProxyProfile `json:"proxies"`
}

type Profile struct {
	ProxyID               string
	RequestTimeout        time.Duration
	NoRequestTimeout      bool
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
}

type Manager struct {
	confFile *config.ConfigFile
	mu       sync.Mutex
	clients  map[string]*http.Client
}

func NewManager(confFile *config.ConfigFile) *Manager {
	m := &Manager{
		confFile: confFile,
		clients:  map[string]*http.Client{},
	}
	if confFile != nil {
		confFile.OnChange(func(map[string]any) {
			m.CloseIdleConnections()
		})
	}
	return m
}

func DefaultProfile() Profile {
	return Profile{
		RequestTimeout:        300 * time.Second,
		ConnectTimeout:        30 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
		IdleConnTimeout:       90 * time.Second,
	}
}

func StreamingProfile(profile Profile) Profile {
	profile.NoRequestTimeout = true
	profile.RequestTimeout = 0
	return profile
}

func ReadNetworkConfig(confFile *config.ConfigFile) NetworkConfig {
	if confFile == nil {
		return NetworkConfig{Proxies: []ProxyProfile{}}
	}
	raw, ok := confFile.Get("network")
	if !ok || raw == nil {
		return NetworkConfig{Proxies: []ProxyProfile{}}
	}
	if cfg, ok := raw.(NetworkConfig); ok {
		return cfg
	}
	networkMap, ok := raw.(map[string]any)
	if !ok {
		return NetworkConfig{Proxies: []ProxyProfile{}}
	}
	proxies := []ProxyProfile{}
	switch proxiesRaw := networkMap["proxies"].(type) {
	case []ProxyProfile:
		proxies = append(proxies, proxiesRaw...)
	case []map[string]any:
		for _, proxyMap := range proxiesRaw {
			proxies = append(proxies, proxyProfileFromMap(proxyMap))
		}
	case []any:
		proxies = make([]ProxyProfile, 0, len(proxiesRaw))
		for _, item := range proxiesRaw {
			proxyMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			proxies = append(proxies, proxyProfileFromMap(proxyMap))
		}
	}
	return NetworkConfig{Proxies: proxies}
}

func proxyProfileFromMap(proxyMap map[string]any) ProxyProfile {
	return ProxyProfile{
		ID:   stringValue(proxyMap["id"]),
		Name: stringValue(proxyMap["name"]),
		Type: strings.ToLower(stringValue(proxyMap["type"])),
		URL:  stringValue(proxyMap["url"]),
	}
}

func ValidateNetworkConfig(cfg NetworkConfig) error {
	ids := map[string]struct{}{}
	names := map[string]struct{}{}
	for i, item := range cfg.Proxies {
		id := strings.TrimSpace(item.ID)
		name := strings.TrimSpace(item.Name)
		proxyType := strings.ToLower(strings.TrimSpace(item.Type))
		if id == "" {
			return fmt.Errorf("proxies[%d].id is required", i)
		}
		if name == "" {
			return fmt.Errorf("proxies[%d].name is required", i)
		}
		if _, ok := ids[id]; ok {
			return fmt.Errorf("proxy id %q is duplicated", id)
		}
		ids[id] = struct{}{}
		normalizedName := strings.ToLower(name)
		if _, ok := names[normalizedName]; ok {
			return fmt.Errorf("proxy name %q is duplicated", name)
		}
		names[normalizedName] = struct{}{}
		switch proxyType {
		case "http", "https", "socks5":
		default:
			return fmt.Errorf("proxy %q type must be one of http, https, socks5", id)
		}
		parsed, err := url.Parse(strings.TrimSpace(item.URL))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("proxy %q url is invalid", id)
		}
		if proxyType == "socks5" && parsed.Scheme != "socks5" {
			return fmt.Errorf("proxy %q url scheme must be socks5", id)
		}
		if proxyType != "socks5" && parsed.Scheme != "http" && parsed.Scheme != "https" {
			return fmt.Errorf("proxy %q url scheme must be http or https", id)
		}
	}
	return nil
}

func NormalizeNetworkConfig(cfg NetworkConfig) NetworkConfig {
	proxies := make([]ProxyProfile, 0, len(cfg.Proxies))
	for _, item := range cfg.Proxies {
		proxies = append(proxies, ProxyProfile{
			ID:   strings.TrimSpace(item.ID),
			Name: strings.TrimSpace(item.Name),
			Type: strings.ToLower(strings.TrimSpace(item.Type)),
			URL:  strings.TrimSpace(item.URL),
		})
	}
	return NetworkConfig{Proxies: proxies}
}

func (m *Manager) Client(profile Profile) (*http.Client, error) {
	if m == nil {
		return newHTTPClient(profile, nil)
	}
	profile = normalizeProfile(profile)
	var proxyProfile *ProxyProfile
	if strings.TrimSpace(profile.ProxyID) != "" {
		item, ok := m.proxyByID(profile.ProxyID)
		if !ok {
			return nil, fmt.Errorf("proxy %q was not found", profile.ProxyID)
		}
		proxyProfile = &item
	}
	key := cacheKey(profile, proxyProfile)

	m.mu.Lock()
	defer m.mu.Unlock()
	if client, ok := m.clients[key]; ok {
		return client, nil
	}
	client, err := newHTTPClient(profile, proxyProfile)
	if err != nil {
		return nil, err
	}
	m.clients[key] = client
	return client, nil
}

func (m *Manager) CloseIdleConnections() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.clients {
		client.CloseIdleConnections()
	}
	m.clients = map[string]*http.Client{}
}

func (m *Manager) proxyByID(id string) (ProxyProfile, bool) {
	id = strings.TrimSpace(id)
	for _, item := range ReadNetworkConfig(m.confFile).Proxies {
		if item.ID == id {
			return item, true
		}
	}
	return ProxyProfile{}, false
}

func normalizeProfile(profile Profile) Profile {
	defaults := DefaultProfile()
	if !profile.NoRequestTimeout && profile.RequestTimeout == 0 {
		profile.RequestTimeout = defaults.RequestTimeout
	}
	if profile.ConnectTimeout == 0 {
		profile.ConnectTimeout = defaults.ConnectTimeout
	}
	if profile.ResponseHeaderTimeout == 0 {
		profile.ResponseHeaderTimeout = defaults.ResponseHeaderTimeout
	}
	if profile.MaxIdleConns == 0 {
		profile.MaxIdleConns = defaults.MaxIdleConns
	}
	if profile.MaxIdleConnsPerHost == 0 {
		profile.MaxIdleConnsPerHost = defaults.MaxIdleConnsPerHost
	}
	if profile.MaxConnsPerHost == 0 {
		profile.MaxConnsPerHost = defaults.MaxConnsPerHost
	}
	if profile.IdleConnTimeout == 0 {
		profile.IdleConnTimeout = defaults.IdleConnTimeout
	}
	profile.ProxyID = strings.TrimSpace(profile.ProxyID)
	return profile
}

func newHTTPClient(profile Profile, proxyProfile *ProxyProfile) (*http.Client, error) {
	profile = normalizeProfile(profile)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = NewGuardedDialer(profile.ConnectTimeout).DialContext
	if proxyProfile != nil {
		parsed, err := url.Parse(strings.TrimSpace(proxyProfile.URL))
		if err != nil {
			return nil, fmt.Errorf("parse proxy url: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(proxyProfile.Type)) {
		case "http", "https":
			transport.Proxy = http.ProxyURL(parsed)
		case "socks5":
			dialer, err := proxy.SOCKS5("tcp", parsed.Host, proxyAuth(parsed), NewGuardedDialer(profile.ConnectTimeout))
			if err != nil {
				return nil, fmt.Errorf("create socks5 proxy dialer: %w", err)
			}
			transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
				type contextDialer interface {
					DialContext(context.Context, string, string) (net.Conn, error)
				}
				if ctxDialer, ok := dialer.(contextDialer); ok {
					return ctxDialer.DialContext(ctx, network, address)
				}
				connCh := make(chan net.Conn, 1)
				errCh := make(chan error, 1)
				go func() {
					conn, err := dialer.Dial(network, address)
					if err != nil {
						errCh <- err
						return
					}
					connCh <- conn
				}()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case err := <-errCh:
					return nil, err
				case conn := <-connCh:
					return conn, nil
				}
			}
		default:
			return nil, fmt.Errorf("unsupported proxy type %q", proxyProfile.Type)
		}
	}
	transport.MaxIdleConns = profile.MaxIdleConns
	transport.MaxIdleConnsPerHost = profile.MaxIdleConnsPerHost
	transport.MaxConnsPerHost = profile.MaxConnsPerHost
	transport.IdleConnTimeout = profile.IdleConnTimeout
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ExpectContinueTimeout = 1 * time.Second
	transport.ResponseHeaderTimeout = profile.ResponseHeaderTimeout

	return &http.Client{
		Timeout:   profile.RequestTimeout,
		Transport: transport,
	}, nil
}

func proxyAuth(parsed *url.URL) *proxy.Auth {
	if parsed == nil || parsed.User == nil {
		return nil
	}
	password, _ := parsed.User.Password()
	return &proxy.Auth{
		User:     parsed.User.Username(),
		Password: password,
	}
}

func cacheKey(profile Profile, proxyProfile *ProxyProfile) string {
	proxyPart := "direct"
	if proxyProfile != nil {
		proxyPart = strings.Join([]string{proxyProfile.ID, proxyProfile.Type, proxyProfile.URL}, "|")
	}
	return fmt.Sprintf("%s|%v|%s|%s|%d|%d|%d|%s|%s",
		profile.RequestTimeout,
		profile.NoRequestTimeout,
		profile.ConnectTimeout,
		profile.ResponseHeaderTimeout,
		profile.MaxIdleConns,
		profile.MaxIdleConnsPerHost,
		profile.MaxConnsPerHost,
		profile.IdleConnTimeout,
		proxyPart,
	)
}

func stringValue(value any) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
