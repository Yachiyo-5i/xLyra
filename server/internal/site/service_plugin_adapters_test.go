package site

import (
	"testing"

	"xlyra/server/internal/adapter"
)

func TestSetPluginAdapterModules(t *testing.T) {
	svc := NewService(nil, "test-master-key")
	module, err := adapter.NewGenericProvider(adapter.GenericProviderConfig{
		SiteType:        "acme.example/chat",
		DisplayName:     "Acme",
		OfficialBaseURL: "https://api.example.com",
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    []string{"api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPluginAdapterModules([]adapter.Module{module}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Capabilities("acme.example/chat"); len(got) == 0 {
		t.Fatal("dynamic module missing")
	}
	if err := svc.SetPluginAdapterModules(nil); err != nil {
		t.Fatal(err)
	}
	if got := svc.Capabilities("acme.example/chat"); len(got) != 0 {
		t.Fatalf("capabilities after clear = %#v", got)
	}
	var none *Service
	if err := none.SetPluginAdapterModules(nil); err == nil {
		t.Fatal("nil service accepted modules")
	}
}
