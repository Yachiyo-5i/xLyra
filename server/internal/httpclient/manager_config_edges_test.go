package httpclient

import (
	"strings"
	"testing"

	"xlyra/server/internal/config"
)

func TestManagerClientReturnsProxyConstructionError(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set("network", map[string]any{
		"proxies": []any{
			map[string]any{
				"id":   "bad-proxy",
				"name": "Bad Proxy",
				"type": "ftp",
				"url":  "http://127.0.0.1:8080",
			},
		},
	}); err != nil {
		t.Fatalf("set network config: %v", err)
	}

	manager := NewManager(confFile)
	if _, err := manager.Client(Profile{ProxyID: "bad-proxy"}); err == nil || !strings.Contains(err.Error(), "unsupported proxy type") {
		t.Fatalf("expected unsupported proxy type error, got %v", err)
	}
}

func TestNilManagerCloseIdleConnectionsIsNoop(t *testing.T) {
	t.Parallel()

	(*Manager)(nil).CloseIdleConnections()
}

func TestReadNetworkConfigIgnoresNonStringProxyFields(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set("network", map[string]any{
		"proxies": []any{
			map[string]any{
				"id":   123,
				"name": true,
				"type": []string{"http"},
				"url":  nil,
			},
		},
	}); err != nil {
		t.Fatalf("set network config: %v", err)
	}

	got := ReadNetworkConfig(confFile)
	if len(got.Proxies) != 1 {
		t.Fatalf("expected one proxy entry, got %#v", got.Proxies)
	}
	if got.Proxies[0] != (ProxyProfile{}) {
		t.Fatalf("non-string fields should decode as zero proxy profile, got %#v", got.Proxies[0])
	}
}
