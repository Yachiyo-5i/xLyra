package site

import "testing"

func TestNormalizeQuotaProbeTypeAllowsPluginSelector(t *testing.T) {
	t.Parallel()
	got, err := NormalizeQuotaProbeType("plugin:com.example.foo-quota")
	if err != nil || got != "plugin:com.example.foo-quota" {
		t.Fatalf("NormalizeQuotaProbeType = %q, err = %v", got, err)
	}
	if _, err := NormalizeQuotaProbeType("plugin:"); err == nil {
		t.Fatal("expected empty plugin id to fail")
	}
}
