package settings

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSystemProxyReportsTCPConnectFailure(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result := testSystemProxy(ctx, proxyConfig{
		ID:   "local",
		Name: "Local",
		Type: "http",
		URL:  "http://127.0.0.1:1",
	})

	if result.OK {
		t.Fatalf("proxy test unexpectedly succeeded: %+v", result)
	}
	if result.Stage != "tcp_connect" {
		t.Fatalf("stage = %q, want tcp_connect: %+v", result.Stage, result)
	}
	if strings.TrimSpace(result.Message) == "" {
		t.Fatalf("message should describe tcp connect failure: %+v", result)
	}
}
