package site

import (
	"sync"
	"testing"
)

func TestSiteTypeAvailableAndProviderStatus(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{
		{
			Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"api.example.com"},
		},
		{
			Descriptor: SiteTypeDescriptor{SiteType: "paused.example/chat", DisplayName: "Paused"},
			PluginID:   "paused.example",
			Enabled:    false,
			Hosts:      []string{"api.paused.example"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		siteType  string
		available bool
		status    string
	}{
		{name: "builtin", siteType: "openai", available: true, status: ""},
		{name: "builtin case and space", siteType: " OpenAI ", available: true, status: ""},
		{name: "unknown builtin-shaped", siteType: "not-a-plugin", available: true, status: ""},
		{name: "empty", siteType: "  ", available: true, status: ""},
		{name: "enabled plugin", siteType: "acme.example/chat", available: true, status: "enabled"},
		{name: "enabled plugin case and space", siteType: " ACME.EXAMPLE/Chat ", available: true, status: "enabled"},
		{name: "disabled plugin", siteType: "paused.example/chat", available: false, status: "unavailable"},
		{name: "disabled plugin case", siteType: " Paused.Example/Chat ", available: false, status: "unavailable"},
		{name: "unknown plugin", siteType: "missing.example/chat", available: false, status: "unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SiteTypeAvailable(tc.siteType); got != tc.available {
				t.Fatalf("SiteTypeAvailable(%q) = %v, want %v", tc.siteType, got, tc.available)
			}
			if got := ProviderStatus(tc.siteType); got != tc.status {
				t.Fatalf("ProviderStatus(%q) = %q, want %q", tc.siteType, got, tc.status)
			}
		})
	}
}

func TestSiteTypeAvailableConcurrentSnapshotSwap(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	enabled := []PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.com"},
	}}
	disabled := []PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    false,
		Hosts:      []string{"api.example.com"},
	}}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			items := enabled
			if i%2 == 1 {
				items = disabled
			}
			if err := SetPluginSiteTypes(items); err != nil {
				t.Errorf("SetPluginSiteTypes: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = SiteTypeAvailable(" ACME.EXAMPLE/Chat ")
			_ = ProviderStatus("acme.example/chat")
			if !SiteTypeAvailable("openai") || ProviderStatus("openai") != "" {
				t.Error("builtin availability changed during a snapshot swap")
				return
			}
		}
	}()
	wg.Wait()
}
