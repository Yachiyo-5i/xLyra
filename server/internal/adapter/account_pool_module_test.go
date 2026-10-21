package adapter

import (
	"context"
	"testing"
)

func TestNewAccountPoolModule(t *testing.T) {
	t.Parallel()
	module, err := NewAccountPoolModule(" ACME.EXAMPLE/Pool ", "https://api.example.cn/")
	if err != nil {
		t.Fatal(err)
	}
	if got := module.SiteTypes(); len(got) != 1 || got[0] != "acme.example/pool" {
		t.Fatalf("site types = %#v", got)
	}
	provider, ok := AsDefaultBaseURLProvider(module)
	if !ok || provider.DefaultBaseURL() != "https://api.example.cn" {
		t.Fatalf("base = %v %q", ok, provider.DefaultBaseURL())
	}
	if caps := ModuleCapabilities(module); len(caps) != 0 {
		t.Fatalf("capabilities = %#v", caps)
	}
	if _, ok := module.(CredentialValidator); ok {
		t.Fatal("validates credentials")
	}
	if _, ok := module.(interface {
		ListModels(context.Context, SiteConfig, string) ([]Model, error)
	}); ok {
		t.Fatal("lists models")
	}
	if _, err := NewAccountPoolModule("openai", "https://api.example.cn"); err == nil {
		t.Fatal("built-in site type was accepted")
	}
	if _, err := NewAccountPoolModule("acme.example/pool", " "); err == nil {
		t.Fatal("empty base url was accepted")
	}
}
