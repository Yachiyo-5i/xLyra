package adapter

import (
	"strings"
	"sync"
	"testing"
)

type taggedModule struct {
	id        string
	siteTypes []string
}

func (m taggedModule) SiteTypes() []string {
	return m.siteTypes
}

func TestSetDynamicModulesReplacesSnapshot(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	module := taggedModule{id: "dyn", siteTypes: []string{" ACME.EXAMPLE/Chat ", "acme.example/embed"}}
	if err := registry.SetDynamicModules([]Module{module}); err != nil {
		t.Fatal(err)
	}

	for _, siteType := range []string{"acme.example/chat", "ACME.EXAMPLE/CHAT", "acme.example/embed"} {
		got, ok := registry.ModuleForSiteType(siteType)
		if !ok {
			t.Fatalf("ModuleForSiteType(%q) not found", siteType)
		}
		if got.(taggedModule).id != "dyn" {
			t.Fatalf("ModuleForSiteType(%q) = %#v", siteType, got)
		}
	}

	modules := registry.Modules()
	if len(modules) != 14 {
		t.Fatalf("module count = %d, want 14", len(modules))
	}
	if modules[len(modules)-1].(taggedModule).id != "dyn" {
		t.Fatalf("last module = %#v", modules[len(modules)-1])
	}
	modules[len(modules)-1] = nil
	if registry.Modules()[len(modules)-1] == nil {
		t.Fatal("Modules should return a copy")
	}

	if err := registry.SetDynamicModules(nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.ModuleForSiteType("acme.example/chat"); ok {
		t.Fatal("empty snapshot should remove dynamic modules")
	}
	if len(registry.Modules()) != 13 {
		t.Fatalf("module count after empty snapshot = %d, want 13", len(registry.Modules()))
	}
}

func TestSetDynamicModulesRejectsWithoutReplacingSnapshot(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	kept := taggedModule{id: "kept", siteTypes: []string{"acme.example/chat"}}
	if err := registry.SetDynamicModules([]Module{kept}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		modules []Module
		want    string
	}{
		{name: "nil module", modules: []Module{nil}, want: "nil"},
		{name: "no site type", modules: []Module{taggedModule{id: "empty"}}, want: "no site type"},
		{name: "missing slash", modules: []Module{taggedModule{id: "builtin", siteTypes: []string{"openai"}}}, want: `must contain "/"`},
		{name: "blank site type", modules: []Module{taggedModule{id: "blank", siteTypes: []string{"  "}}}, want: `must contain "/"`},
		{name: "builtin conflict", modules: []Module{taggedModule{id: "conflict", siteTypes: []string{"acme.example/static"}}}, want: "conflicts"},
		{name: "duplicate", modules: []Module{
			taggedModule{id: "one", siteTypes: []string{"plugin.example/chat"}},
			taggedModule{id: "two", siteTypes: []string{" Plugin.Example/Chat "}},
		}, want: "duplicate"},
		{name: "duplicate on one module", modules: []Module{taggedModule{id: "same", siteTypes: []string{"plugin.example/chat", "plugin.example/chat"}}}, want: "duplicate"},
	}
	registry.Register(taggedModule{id: "static", siteTypes: []string{"acme.example/static"}})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.SetDynamicModules(tc.modules)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			got, ok := registry.ModuleForSiteType("acme.example/chat")
			if !ok || got.(taggedModule).id != "kept" {
				t.Fatalf("snapshot changed: %#v ok=%v", got, ok)
			}
			if _, ok := registry.ModuleForSiteType("plugin.example/chat"); ok {
				t.Fatal("rejected module was published")
			}
		})
	}
}

func TestDynamicModulesBuiltinWinsAndCopiesShareSnapshot(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.SetDynamicModules([]Module{taggedModule{id: "dyn", siteTypes: []string{"acme.example/chat"}}}); err != nil {
		t.Fatal(err)
	}
	registry.Register(taggedModule{id: "static", siteTypes: []string{"acme.example/chat"}})
	got, ok := registry.ModuleForSiteType("acme.example/chat")
	if !ok || got.(taggedModule).id != "static" {
		t.Fatalf("builtin priority = %#v ok=%v", got, ok)
	}
	if _, ok := registry.ModuleForSiteType("openai"); !ok {
		t.Fatal("built-in openai lookup missing")
	}

	shared := registry
	next := taggedModule{id: "next", siteTypes: []string{"plugin.example/other"}}
	if err := shared.SetDynamicModules([]Module{next}); err != nil {
		t.Fatal(err)
	}
	got, ok = registry.ModuleForSiteType("plugin.example/other")
	if !ok || got.(taggedModule).id != "next" {
		t.Fatalf("copy did not share snapshot: %#v ok=%v", got, ok)
	}
	shared.ClearDynamicModules()
	if _, ok := registry.ModuleForSiteType("plugin.example/other"); ok {
		t.Fatal("clear on a copy did not drop the shared snapshot")
	}

	other := NewRegistry()
	if err := other.SetDynamicModules([]Module{next}); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.ModuleForSiteType("plugin.example/other"); ok {
		t.Fatal("dynamic module leaked into another registry")
	}
	if _, ok := other.ModuleForSiteType("plugin.example/other"); !ok {
		t.Fatal("other registry missing its dynamic module")
	}
}

func TestDynamicModulesZeroRegistryAndNilReceiver(t *testing.T) {
	t.Parallel()

	var registry Registry
	if _, ok := registry.ModuleForSiteType("acme.example/chat"); ok {
		t.Fatal("zero registry returned a module")
	}
	if len(registry.Modules()) != 0 {
		t.Fatalf("zero registry modules = %d", len(registry.Modules()))
	}
	module := taggedModule{id: "dyn", siteTypes: []string{"acme.example/chat"}}
	if err := registry.SetDynamicModules([]Module{module}); err != nil {
		t.Fatal(err)
	}
	if got, ok := registry.ModuleForSiteType("acme.example/chat"); !ok || got.(taggedModule).id != "dyn" {
		t.Fatalf("zero registry lookup = %#v ok=%v", got, ok)
	}

	var nilRegistry *Registry
	if err := nilRegistry.SetDynamicModules([]Module{module}); err == nil {
		t.Fatal("nil registry accepted dynamic modules")
	}
	nilRegistry.ClearDynamicModules()
}

func TestDynamicModulesConcurrentSetAndLookup(t *testing.T) {
	registry := NewRegistry()
	first := taggedModule{id: "first", siteTypes: []string{"acme.example/chat"}}
	second := taggedModule{id: "second", siteTypes: []string{"plugin.example/other"}}
	if err := registry.SetDynamicModules([]Module{first}); err != nil {
		t.Fatal(err)
	}
	shared := registry

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for n := 0; n < 200; n++ {
				switch {
				case i%2 == 0:
					_, _ = shared.ModuleForSiteType("acme.example/chat")
					_, _ = registry.ModuleForSiteType("plugin.example/other")
					_ = registry.Modules()
				case n%2 == 0:
					_ = registry.SetDynamicModules([]Module{first, second})
				default:
					shared.ClearDynamicModules()
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
}
