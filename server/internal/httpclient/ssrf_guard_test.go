package httpclient

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewGuardedTransportBlocksMetadataAndAllowsLoopback(t *testing.T) {
	orig := ssrfBlockPrivateNetworks
	ssrfBlockPrivateNetworks = false
	t.Cleanup(func() { ssrfBlockPrivateNetworks = orig })

	transport := NewGuardedTransport(time.Second)
	if transport.Proxy == nil || transport.DialContext == nil {
		t.Fatal("guarded transport lost the default proxy or dial")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	const token = "METADATA_TOKEN_9f3c"
	conn, err := transport.DialContext(ctx, "tcp", "169.254.169.254:80")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("metadata dial connected")
	}
	if err == nil || !strings.Contains(err.Error(), "blocked connection") {
		t.Fatalf("metadata dial = %v", err)
	}
	if strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "http://") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("metadata dial error = %v", err)
	}
	conn, err = transport.DialContext(ctx, "tcp", "127.0.0.1:1")
	if conn != nil {
		_ = conn.Close()
	}
	if err != nil && strings.Contains(err.Error(), "blocked connection") {
		t.Fatalf("loopback dial blocked: %v", err)
	}
}

func TestNewGuardedDialerAppliesSSRFGuard(t *testing.T) {
	dialer := NewGuardedDialer(time.Second)
	if dialer.Control == nil {
		t.Fatal("NewGuardedDialer must install a Control hook")
	}
	if err := dialer.Control("tcp", "169.254.169.254:80", nil); err == nil {
		t.Fatal("guarded dialer must block cloud metadata address")
	}
	if err := dialer.Control("tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("guarded dialer must allow public address, got %v", err)
	}
}

func TestSSRFGuardControlBlocksNonPublicTargets(t *testing.T) {
	cases := []struct {
		address     string
		blockPriv   bool
		wantBlocked bool
	}{
		{"169.254.169.254:80", false, true}, // cloud metadata — always blocked
		{"[fe80::1]:443", false, true},      // link-local v6 — always blocked
		{"0.0.0.0:80", false, true},         // unspecified — always blocked
		{"8.8.8.8:443", false, false},       // public — allowed
		{"93.184.216.34:443", false, false}, // public — allowed
		{"127.0.0.1:8080", false, false},    // loopback allowed by default
		{"10.0.0.5:80", false, false},       // private allowed by default
		{"127.0.0.1:8080", true, true},      // loopback blocked when strict
		{"10.0.0.5:80", true, true},         // private blocked when strict
		{"192.168.1.10:80", true, true},     // private blocked when strict
		{"nonsense:80", false, true},        // non-ip host — blocked
	}

	orig := ssrfBlockPrivateNetworks
	defer func() { ssrfBlockPrivateNetworks = orig }()

	for _, tc := range cases {
		ssrfBlockPrivateNetworks = tc.blockPriv
		err := ssrfGuardControl("tcp", tc.address, nil)
		if (err != nil) != tc.wantBlocked {
			t.Fatalf("ssrfGuardControl(%q, blockPriv=%v) err=%v, wantBlocked=%v", tc.address, tc.blockPriv, err, tc.wantBlocked)
		}
	}
}
